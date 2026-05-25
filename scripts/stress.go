package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"
)

type RunResult struct {
	Duration   time.Duration
	StatusCode int
	RespStatus string
	Err        error
}

func main() {
	targetURL := flag.String("target", "http://localhost:8080/run", "Target execution API URL")
	payloadPath := flag.String("payload", "test_py3.json", "Path to the request payload JSON file")
	concurrency := flag.Int("concurrency", 10, "Number of concurrent workers (ignored if -batch is set)")
	duration := flag.Duration("duration", 5*time.Second, "Duration for each test run (e.g., 5s, 10s)")
	batchMode := flag.Bool("batch", false, "Run full benchmark suite sequentially for p1, p10, p20, p50, and p100 users")
	flag.Parse()

	// Read and validate payload
	payloadData, err := os.ReadFile(*payloadPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading payload file: %v\n", err)
		os.Exit(1)
	}

	var jsonCheck map[string]interface{}
	if err := json.Unmarshal(payloadData, &jsonCheck); err != nil {
		fmt.Fprintf(os.Stderr, "Payload file is not valid JSON: %v\n", err)
		os.Exit(1)
	}

	lang, _ := jsonCheck["language"].(string)
	fmt.Printf("🔥 Goboxd Stress Tester Starting\n")
	fmt.Printf("===============================\n")
	fmt.Printf("Target Endpoint: %s\n", *targetURL)
	fmt.Printf("Payload Source : %s (Language: %s)\n", *payloadPath, lang)
	fmt.Printf("Run Duration   : %v\n\n", *duration)

	if *batchMode {
		concurrencies := []int{1, 10, 20, 50, 100}
		fmt.Println("🚀 Running Full Batch Concurrency Suite (p1 -> p100)...")
		fmt.Println("---------------------------------------------------------------------------------------------------------")
		fmt.Printf("%-12s | %-12s | %-12s | %-12s | %-10s | %-10s | %-10s | %-10s\n",
			"Concurrency", "Total Req", "RPS", "Success Rate", "Min Lat", "P50 Lat", "P90 Lat", "P99 Lat")
		fmt.Println("---------------------------------------------------------------------------------------------------------")

		for _, c := range concurrencies {
			stats := runStressTest(*targetURL, payloadData, c, *duration)
			printRow(c, stats)
			// Small cooldown sleep between benchmarks to allow OS resources/chroots to clear
			time.Sleep(1 * time.Second)
		}
		fmt.Println("---------------------------------------------------------------------------------------------------------")
	} else {
		fmt.Printf("🚀 Running Single Test (Concurrency: %d)...\n", *concurrency)
		stats := runStressTest(*targetURL, payloadData, *concurrency, *duration)

		fmt.Println("\n📊 Run Results Summary:")
		fmt.Println("--------------------------------------------------")
		fmt.Printf("Concurrency (Workers)  : %d\n", *concurrency)
		fmt.Printf("Total Requests Sent    : %d\n", stats.TotalRequests)
		fmt.Printf("Successful Responses   : %d (HTTP 200 and accepted)\n", stats.SuccessCount)
		fmt.Printf("HTTP Errors (Non-200)  : %d\n", stats.HttpErrorCount)
		fmt.Printf("Sandbox Failures       : %d\n", stats.SandboxFailureCount)
		fmt.Printf("Overall Success Rate   : %.2f%%\n", stats.SuccessRate)
		fmt.Printf("Throughput (RPS)       : %.2f req/sec\n", stats.RPS)
		fmt.Println("--------------------------------------------------")
		fmt.Printf("Latency Minimum        : %v\n", stats.Min)
		fmt.Printf("Latency P50 (Median)   : %v\n", stats.P50)
		fmt.Printf("Latency P90            : %v\n", stats.P90)
		fmt.Printf("Latency P99            : %v\n", stats.P99)
		fmt.Printf("Latency Maximum        : %v\n", stats.Max)
		fmt.Printf("Latency Average (Mean) : %v\n", stats.Mean)
		fmt.Println("--------------------------------------------------")
	}
}

type AggregatedStats struct {
	TotalRequests       int
	SuccessCount        int
	HttpErrorCount      int
	SandboxFailureCount int
	SuccessRate         float64
	RPS                 float64
	Min, Max, Mean      time.Duration
	P50, P90, P99       time.Duration
}

func runStressTest(url string, payload []byte, concurrency int, duration time.Duration) AggregatedStats {
	resultsChan := make(chan RunResult, 50000)
	var wg sync.WaitGroup

	stopChan := make(chan struct{})

	// Create dedicated HTTP client to reuse connections
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        concurrency * 2,
			MaxIdleConnsPerHost: concurrency * 2,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	startTime := time.Now()

	// Spawn concurrent workers
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stopChan:
					return
				default:
					reqStart := time.Now()
					req, err := http.NewRequest("POST", url, bytes.NewReader(payload))
					if err != nil {
						resultsChan <- RunResult{Err: err}
						continue
					}
					req.Header.Set("Content-Type", "application/json")

					resp, err := client.Do(req)
					elapsed := time.Since(reqStart)

					if err != nil {
						resultsChan <- RunResult{Duration: elapsed, Err: err}
						continue
					}

					// Read full body to reuse keep-alive connection
					body, err := io.ReadAll(resp.Body)
					resp.Body.Close()

					if err != nil {
						resultsChan <- RunResult{Duration: elapsed, StatusCode: resp.StatusCode, Err: err}
						continue
					}

					var responseData struct {
						Status string `json:"status"`
					}

					respStatus := ""
					if resp.StatusCode == http.StatusOK {
						_ = json.Unmarshal(body, &responseData)
						respStatus = responseData.Status
					}

					resultsChan <- RunResult{
						Duration:   elapsed,
						StatusCode: resp.StatusCode,
						RespStatus: respStatus,
					}
				}
			}
		}()
	}

	// Wait for duration then stop all workers
	time.Sleep(duration)
	close(stopChan)
	wg.Wait()
	close(resultsChan)

	actualDuration := time.Since(startTime)

	// Collect metrics
	var latencies []time.Duration
	var totalReq, successCount, httpErrCount, sandboxErrCount int

	for res := range resultsChan {
		totalReq++
		if res.Err != nil {
			httpErrCount++
			continue
		}
		if res.StatusCode != http.StatusOK {
			httpErrCount++
			continue
		}

		// If HTTP OK, verify sandbox status
		if res.RespStatus == "accepted" {
			successCount++
		} else {
			sandboxErrCount++
		}
		latencies = append(latencies, res.Duration)
	}

	stats := AggregatedStats{
		TotalRequests:       totalReq,
		SuccessCount:        successCount,
		HttpErrorCount:      httpErrCount,
		SandboxFailureCount: sandboxErrCount,
	}

	if totalReq > 0 {
		stats.SuccessRate = (float64(successCount) / float64(totalReq)) * 100
		stats.RPS = float64(totalReq) / actualDuration.Seconds()
	}

	if len(latencies) > 0 {
		sort.Slice(latencies, func(i, j int) bool {
			return latencies[i] < latencies[j]
		})

		var sum time.Duration
		for _, l := range latencies {
			sum += l
		}

		stats.Min = latencies[0]
		stats.Max = latencies[len(latencies)-1]
		stats.Mean = sum / time.Duration(len(latencies))
		stats.P50 = latencies[int(float64(len(latencies))*0.50)]
		stats.P90 = latencies[int(float64(len(latencies))*0.90)]
		stats.P99 = latencies[int(float64(len(latencies))*0.99)]
	}

	return stats
}

func printRow(concurrency int, stats AggregatedStats) {
	fmt.Printf("p%-11d | %-12d | %-12.2f | %-11.2f%% | %-10v | %-10v | %-10v | %-10v\n",
		concurrency,
		stats.TotalRequests,
		stats.RPS,
		stats.SuccessRate,
		roundDuration(stats.Min),
		roundDuration(stats.P50),
		roundDuration(stats.P90),
		roundDuration(stats.P99),
	)
}

func roundDuration(d time.Duration) time.Duration {
	return d.Round(time.Millisecond)
}
