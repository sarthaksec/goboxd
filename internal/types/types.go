package types

type LimitsSpec struct {
	WallTimeS    int `json:"wall_time_s"`
	MemoryKb     int `json:"memory_kb"`
	MaxProcesses int `json:"max_processes"`
}

type BuildSpec struct {
	Limits *LimitsSpec `json:"limits,omitempty"`
	Flags  []string    `json:"flags,omitempty"`
}

type RunSpec struct {
	Limits *LimitsSpec `json:"limits,omitempty"`
	Flags  []string    `json:"flags,omitempty"`
}

type Test struct {
	Stdin          string `json:"stdin"`
	ExpectedStdout string `json:"expected_stdout"`
}

type RunRequest struct {
	Language         string     `json:"language"`
	Source           string     `json:"source"`
	SourceFilename   string     `json:"source_filename,omitempty"`
	ArtifactFilename string     `json:"artifact_filename,omitempty"`
	Build            *BuildSpec `json:"build,omitempty"`
	Run              *RunSpec   `json:"run,omitempty"`
	Tests            []Test     `json:"tests"`
}

type BuildResult struct {
	Status     string `json:"status"` // ok, failed, internal_error
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	DurationMs int64  `json:"duration_ms"`
}

type TestResult struct {
	Status       string `json:"status"` // accepted, wrong_output, output_whitespace_mismatch, time_exceeded, memory_exceeded, runtime_error, not_executed, internal_error
	Stdout       string `json:"stdout"`
	Stderr       string `json:"stderr"`
	DurationMs   int64  `json:"duration_ms"`
	MemoryPeakKb int    `json:"memory_peak_kb,omitempty"`
}

type RunResponse struct {
	Status string       `json:"status"` // top-level status
	Build  BuildResult  `json:"build"`
	Tests  []TestResult `json:"tests"`
}
