package sandbox

import (
	"goboxd/internal/types"
	"os"
	"os/exec"
	"path/filepath"
	// "github.com/thesouldev/goboxd/internal/types"
)

func ExecutePython(source string) (*types.RunResponse, error) {
	// Create temporary jail directory
	jailDir, err := os.MkdirTemp("", "goboxd-jail-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(jailDir)
	srcPath := filepath.Join(jailDir, "solution.py")
	if err := os.WriteFile(srcPath, []byte(source), 0644); err != nil {
		return nil, err
	}

	// 3rd try running python in nsjail lmao
	cmd := exec.Command("nsjail",
		"--mode", "o",
		"--chroot", jailDir,
		"--user", "nobody",
		"--group", "nogroup",
		"--time_limit", "5",
		"--rlimit_as", "512",
		"--", "/usr/bin/python3", "/solution.py",
	)

	output, err := cmd.CombinedOutput()

	resp := &types.RunResponse{
		Status: "200",
		Build:  types.BuildResult{Success: true},
	}

	if err != nil {
		resp.Tests = []types.TestResult{{
			Success: false,
			Error:   err.Error(),
			Output:  string(output),
		}}
	} else {
		resp.Tests = []types.TestResult{{
			Success: true,
			Output:  string(output),
		}}
	}

	return resp, nil
}
