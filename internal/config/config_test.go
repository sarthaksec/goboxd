package config

import (
	"testing"
)

func TestIsFlagAllowed(t *testing.T) {
	allowlist := []string{
		"-O0",
		"-O1",
		"-O2",
		"-O3",
		"-Wall",
		"-Wextra",
		"-std=*",
	}

	tests := []struct {
		flag    string
		allowed bool
	}{
		{"-O2", true},
		{"-O3", true},
		{"-Wall", true},
		{"-std=c++17", true},
		{"-std=c++20", true},
		{"-std=", true},
		{"-O4", false},
		{"-x", false},
		{"-fplugin=malicious", false},
		{"-B/tmp", false},
	}

	for _, tt := range tests {
		t.Run(tt.flag, func(t *testing.T) {
			got := IsFlagAllowed(tt.flag, allowlist)
			if got != tt.allowed {
				t.Errorf("IsFlagAllowed(%q) = %v; want %v", tt.flag, got, tt.allowed)
			}
		})
	}
}
