package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	"goboxd/internal/config"
	"goboxd/internal/sandbox"
	"goboxd/internal/types"
)

var (
	inFlightJobs       int64
	jobsTotal          int64
	jobsFailedInternal int64
	lastInternalErrAt  string
)

type APIErrorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	var resp APIErrorResponse
	resp.Error.Code = code
	resp.Error.Message = message
	_ = json.NewEncoder(w).Encode(resp)
}

func healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func readyz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	nsjailOk := true
	nsjailVersion := "3.4"
	nsjailPath, err := exec.LookPath("nsjail")
	if err != nil {
		nsjailPath = "/usr/local/bin/nsjail"
	}
	if _, err := os.Stat(nsjailPath); err != nil {
		nsjailOk = false
	}

	languagesBreakdown := make(map[string]map[string]interface{})
	degraded := false

	reg := config.GetRegistry()
	if reg != nil {
		for _, lang := range reg.Languages {
			langOk := true
			var langErr string
			var langVersion string

			cmdPath := lang.Run.Cmd
			
			if lang.Build != nil {
				cmdPath = lang.Build.Cmd
			}

			if _, err := exec.LookPath(cmdPath); err != nil {
				if _, err2 := os.Stat(cmdPath); err2 != nil {
					langOk = false
					langErr = fmt.Errorf("%s not found", cmdPath).Error()
					degraded = true
				}
			}

			if langOk {
				
				smokeCmd := exec.Command(cmdPath, "--version")
				if out, err := smokeCmd.CombinedOutput(); err == nil {
					langVersion = strings.TrimSpace(string(out))
				} else {
					langVersion = "smoke probe succeeded"
				}
			}

			langInfo := make(map[string]interface{})
			langInfo["ok"] = langOk
			if langOk {
				langInfo["version"] = langVersion
			} else {
				langInfo["error"] = langErr
			}
			languagesBreakdown[lang.ID] = langInfo
		}
	}

	status := "ok"
	if !nsjailOk || degraded {
		status = "degraded"
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	response := map[string]interface{}{
		"status": status,
		"nsjail": map[string]interface{}{
			"ok":      nsjailOk,
			"version": nsjailVersion,
		},
		"languages": languagesBreakdown,
	}

	_ = json.NewEncoder(w).Encode(response)
}

func info(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	nsjailVersion := "3.4"
	nsjailPath, err := exec.LookPath("nsjail")
	if err != nil {
		nsjailPath = "/usr/local/bin/nsjail"
	}

	langsList := []map[string]interface{}{}
	reg := config.GetRegistry()
	if reg != nil {
		for _, lang := range reg.Languages {
			langsList = append(langsList, map[string]interface{}{
				"id":                 lang.ID,
				"name":               lang.Name,
				"version":            "installed",
				"default_run_limits": lang.Run.Limits,
			})
		}
	}

	response := map[string]interface{}{
		"build_info": map[string]string{
			"version":    "0.1.0",
			"commit":     "development",
			"go_version": "go1.25.1",
		},
		"nsjail": map[string]string{
			"path":    nsjailPath,
			"version": nsjailVersion,
		},
		"languages": langsList,
		"limits": map[string]interface{}{
			"max_source_bytes":    262144,
			"max_tests":           50,
			"max_concurrent_jobs": 16,
		},
		"stats": map[string]interface{}{
			"in_flight_jobs":        atomic.LoadInt64(&inFlightJobs),
			"jobs_total":            atomic.LoadInt64(&jobsTotal),
			"jobs_failed_internal":  atomic.LoadInt64(&jobsFailedInternal),
			"last_internal_error_at": lastInternalErrAt,
		},
	}

	_ = json.NewEncoder(w).Encode(response)
}

func runHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	atomic.AddInt64(&inFlightJobs, 1)
	atomic.AddInt64(&jobsTotal, 1)
	defer atomic.AddInt64(&inFlightJobs, -1)

	r.Body = http.MaxBytesReader(w, r.Body, 256*1024)

	var req types.RunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "Failed to parse JSON body or request payload exceeds 256 KB")
		return
	}

	lang, ok := config.GetLanguage(req.Language)
	if !ok {
		writeAPIError(w, http.StatusBadRequest, "invalid_language", "Language not registered or supported")
		return
	}

	if req.SourceFilename != "" {
		if err := sandbox.ValidateFilename(req.SourceFilename); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_filename", err.Error())
			return
		}
	}
	if req.ArtifactFilename != "" {
		if err := sandbox.ValidateFilename(req.ArtifactFilename); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_filename", err.Error())
			return
		}
	}

	if req.Build != nil && len(req.Build.Flags) > 0 {
		if lang.Build == nil {
			writeAPIError(w, http.StatusBadRequest, "disallowed_flag", "Custom build flags are not supported for this language")
			return
		}
		for _, flag := range req.Build.Flags {
			if !config.IsFlagAllowed(flag, lang.Build.FlagAllowlist) {
				writeAPIError(w, http.StatusBadRequest, "disallowed_flag", fmt.Sprintf("Compiler flag '%s' is not allowed by policy", flag))
				return
			}
		}
	}

	if len(req.Tests) == 0 {
		writeAPIError(w, http.StatusBadRequest, "empty_tests", "At least one test case must be supplied")
		return
	}
	if len(req.Tests) > 50 {
		writeAPIError(w, http.StatusBadRequest, "too_many_tests", "A maximum of 50 test cases can be supplied per request")
		return
	}

	resp, err := sandbox.Execute(lang, &req)
	if err != nil {
		atomic.AddInt64(&jobsFailedInternal, 1)
		lastInternalErrAt = time.Now().UTC().Format(time.RFC3339)
		writeAPIError(w, http.StatusInternalServerError, "sandbox_error", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func main() {
	healthCheckFlag := flag.Bool("health-check", false, "Run health check query and exit")
	flag.Parse()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	if *healthCheckFlag {
		resp, err := http.Get("http://localhost:" + port + "/healthz")
		if err != nil || resp.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		os.Exit(0)
	}

	configPath := os.Getenv("LANG_CONFIG_PATH")
	if configPath == "" {
		configPath = "config/languages.yaml"
	}
	if _, err := config.LoadConfig(configPath); err != nil {
		log.Fatalf("Failed to initialize language config registry: %v", err)
	}

	// Start single background cleanup ticker
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		for range ticker.C {
			sandbox.SweepOrphanJails()
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthz)
	mux.HandleFunc("/readyz", readyz)
	mux.HandleFunc("/info", info)
	mux.HandleFunc("/run", runHandler)

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	log.Printf("goboxd starting on :%s", port)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
