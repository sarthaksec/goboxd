package types

type RunRequest struct {
	Language       string `json:"language"`
	Source         string `json:"source"`
	SourceFilename string `json:"source_filename,omitempty"`
	Tests          []Test `json:"tests,omitempty"`
}

type Test struct {
	Input string `json:"input,omitempty"`
}

type RunResponse struct {
	Status string       `json:"status"`
	Build  BuildResult  `json:"build"`
	Tests  []TestResult `json:"tests"`
}

type BuildResult struct {
	Success bool   `json:"success"`
	Output  string `json:"output"`
}

type TestResult struct {
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}
