package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"goboxd/internal/config"
	"goboxd/internal/types"
)

type SafeBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func NewSafeBuffer(limit int) *SafeBuffer {
	return &SafeBuffer{limit: limit}
}

func (s *SafeBuffer) Write(p []byte) (n int, err error) {
	if s.buf.Len() >= s.limit {
		s.truncated = true
		return len(p), nil
	}
	remaining := s.limit - s.buf.Len()
	if len(p) > remaining {
		s.buf.Write(p[:remaining])
		s.truncated = true
		return len(p), nil
	}
	return s.buf.Write(p)
}

func (s *SafeBuffer) String() string {
	str := s.buf.String()
	if s.truncated {
		str += "\n[--- TRUNCATED ---]\n"
	}
	return str
}

func ValidateFilename(name string) error {
	if name == "" {
		return nil
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, "..") || strings.Contains(name, "\x00") {
		return fmt.Errorf("filename must be a single path component")
	}
	return nil
}

func SweepOrphanJails() {
	tempDir := os.TempDir()
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		log.Printf("Failed to read temp dir for sweep: %v", err)
		return
	}

	now := time.Now()
	threshold := 5 * time.Minute

	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "goboxd-jail-") {
			fullPath := filepath.Join(tempDir, entry.Name())
			info, err := entry.Info()
			if err != nil {
				continue
			}
			if now.Sub(info.ModTime()) > threshold {
				log.Printf("Sweeping orphaned jail directory: %s", fullPath)
				os.RemoveAll(fullPath)
			}
		}
	}
}

var executionSemaphore = make(chan struct{}, 16)

func Execute(lang *config.LanguageConfig, req *types.RunRequest) (*types.RunResponse, error) {
	executionSemaphore <- struct{}{}
	defer func() { <-executionSemaphore }()

	jailDir, err := os.MkdirTemp("", "goboxd-jail-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create jail dir: %w", err)
	}
	defer os.RemoveAll(jailDir)

	if err := os.Chmod(jailDir, 0777); err != nil {
		return nil, fmt.Errorf("failed to chmod jail dir: %w", err)
	}

	sourceFilename := req.SourceFilename
	if sourceFilename == "" {
		sourceFilename = lang.SourceFilename
	}
	if err := ValidateFilename(sourceFilename); err != nil {
		return nil, err
	}

	srcPath := filepath.Join(jailDir, sourceFilename)
	if err := os.WriteFile(srcPath, []byte(req.Source), 0644); err != nil {
		return nil, fmt.Errorf("failed to write source file: %w", err)
	}

	artifactFilename := req.ArtifactFilename
	if artifactFilename == "" {
		artifactFilename = lang.Artifact
	}
	if err := ValidateFilename(artifactFilename); err != nil {
		return nil, err
	}

	placeholders := map[string]string{
		"{{source}}":   sourceFilename,
		"{{artifact}}": artifactFilename,
	}

	resp := &types.RunResponse{
		Status: "accepted",
		Tests:  make([]types.TestResult, len(req.Tests)),
	}

	if lang.Build != nil {
		limits := lang.Build.Limits
		if req.Build != nil && req.Build.Limits != nil {
			
			if req.Build.Limits.WallTimeS > 0 {
				limits.WallTimeS = req.Build.Limits.WallTimeS
			}
			if req.Build.Limits.MemoryKb > 0 {
				limits.MemoryKb = req.Build.Limits.MemoryKb
			}
			if req.Build.Limits.MaxProcesses > 0 {
				limits.MaxProcesses = req.Build.Limits.MaxProcesses
			}
		}

		var flags []string
		if req.Build != nil {
			flags = req.Build.Flags
		}

		buildArgs := expandArgs(lang.Build.Args, placeholders, flags)

		nsjailArgs := buildNsjailArgs(jailDir, limits, lang.Build.Cmd, buildArgs)

		startTime := time.Now()
		buildOut, buildErr, statusStr, compileErr := runNsjailCmd(nsjailArgs, nil, limits.WallTimeS)
		durationMs := time.Since(startTime).Milliseconds()

		resp.Build = types.BuildResult{
			Status:     "ok",
			Stdout:     buildOut,
			Stderr:     buildErr,
			DurationMs: durationMs,
		}

		if compileErr != nil {
			resp.Build.Status = "internal_error"
			resp.Status = "internal_error"
			for i := range resp.Tests {
				resp.Tests[i] = types.TestResult{Status: "not_executed"}
			}
			return resp, nil
		}

		if statusStr != "accepted" {
			resp.Build.Status = "failed"
			resp.Status = "build_failed"
			for i := range resp.Tests {
				resp.Tests[i] = types.TestResult{Status: "not_executed"}
			}
			return resp, nil
		}
	} else {
		
		resp.Build = types.BuildResult{
			Status: "ok",
		}
	}

	limits := lang.Run.Limits
	if req.Run != nil && req.Run.Limits != nil {
		if req.Run.Limits.WallTimeS > 0 {
			limits.WallTimeS = req.Run.Limits.WallTimeS
		}
		if req.Run.Limits.MemoryKb > 0 {
			limits.MemoryKb = req.Run.Limits.MemoryKb
		}
		if req.Run.Limits.MaxProcesses > 0 {
			limits.MaxProcesses = req.Run.Limits.MaxProcesses
		}
	}

	var runFlags []string
	if req.Run != nil {
		runFlags = req.Run.Flags
	}

	runArgs := expandArgs(lang.Run.Args, placeholders, runFlags)

	firstNonAccepted := ""

	for i, test := range req.Tests {
		nsjailArgs := buildNsjailArgs(jailDir, limits, lang.Run.Cmd, runArgs)

		startTime := time.Now()
		testOut, testErr, statusStr, testRunErr := runNsjailCmd(nsjailArgs, []byte(test.Stdin), limits.WallTimeS)
		durationMs := time.Since(startTime).Milliseconds()

		peakMemoryKb := 0
		if testRunErr == nil {
			
		}

		result := types.TestResult{
			Status:       statusStr,
			Stdout:       testOut,
			Stderr:       testErr,
			DurationMs:   durationMs,
			MemoryPeakKb: peakMemoryKb,
		}

		if testRunErr != nil {
			result.Status = "internal_error"
		} else if statusStr == "accepted" {
			
			result.Status = compareOutput(testOut, test.ExpectedStdout)
		}

		resp.Tests[i] = result

		if result.Status != "accepted" && firstNonAccepted == "" {
			firstNonAccepted = result.Status
		}
	}

	if firstNonAccepted != "" {
		resp.Status = firstNonAccepted
	}

	return resp, nil
}

func expandArgs(configuredArgs []string, placeholders map[string]string, flags []string) []string {
	var result []string
	for _, arg := range configuredArgs {
		if arg == "{{flags}}" {
			result = append(result, flags...)
		} else {
			val := arg
			for placeholder, replacement := range placeholders {
				val = strings.ReplaceAll(val, placeholder, replacement)
			}
			result = append(result, val)
		}
	}
	return result
}

func buildNsjailArgs(jailDir string, limits config.LimitConfig, targetCmd string, targetArgs []string) []string {
	
	memMb := limits.MemoryKb / 1024
	if memMb <= 0 {
		memMb = 100 
	}

	args := []string{
		"-q",                 
		"--log", "/dev/null", 
		"--mode", "o",        
		"--chroot", jailDir,
		"-B", jailDir + ":/", 
		"--user", "nobody",
		"--group", "nogroup",
		"-R", "/bin",
		"-R", "/usr",
		"-R", "/lib",
		"-R", "/lib64",
		"-R", "/etc",
		"-E", "PATH=/usr/bin:/bin",
		"--time_limit", fmt.Sprintf("%d", limits.WallTimeS),
		"--rlimit_as", fmt.Sprintf("%d", memMb),
		"--rlimit_nproc", fmt.Sprintf("%d", limits.MaxProcesses),
		"--",
		targetCmd,
	}
	args = append(args, targetArgs...)
	return args
}

func runNsjailCmd(nsjailArgs []string, stdin []byte, timeoutS int) (string, string, string, error) {
	
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutS)*time.Second+500*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(ctx, "nsjail", nsjailArgs...)

	if len(stdin) > 0 {
		cmd.Stdin = bytes.NewReader(stdin)
	}

	stdoutBuf := NewSafeBuffer(256 * 1024) 
	stderrBuf := NewSafeBuffer(256 * 1024)

	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf

	err := cmd.Run()

	durationMs := int(0) 

	statusStr := "accepted"
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				if status.Signaled() {
					sig := status.Signal()
					if sig == syscall.SIGKILL || sig == syscall.Signal(24) {
						statusStr = "time_exceeded"
					} else {
						statusStr = "runtime_error"
					}
				} else {
					code := status.ExitStatus()
					if code == 137 {
						
						statusStr = "time_exceeded"
					} else if code != 0 {
						statusStr = "runtime_error"
					}
				}
			} else {
				statusStr = "runtime_error"
			}
		} else {
			if ctx.Err() == context.DeadlineExceeded {
				statusStr = "time_exceeded"
			} else {
				
				return "", "", "internal_error", err
			}
		}
	}

	_ = durationMs

	return stdoutBuf.String(), stderrBuf.String(), statusStr, nil
}

func compareOutput(actual, expected string) string {
	if actual == expected {
		return "accepted"
	}
	
	actualTrim := strings.TrimRight(actual, " \t\r\n")
	expectedTrim := strings.TrimRight(expected, " \t\r\n")

	if actualTrim == expectedTrim {
		return "output_whitespace_mismatch"
	}
	return "wrong_output"
}
