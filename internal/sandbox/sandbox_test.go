package sandbox

import (
	"testing"
)

func TestValidateFilename(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"solution.py", false},
		{"solution.cpp", false},
		{"solution", false},
		{"", false},
		{"../solution.py", true},
		{"..", true},
		{"/solution.py", true},
		{"\\solution.py", true},
		{"solution.py\x00", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateFilename(tt.name)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateFilename(%q) error = %v, wantErr = %v", tt.name, err, tt.wantErr)
			}
		})
	}
}

func TestCompareOutput(t *testing.T) {
	tests := []struct {
		actual   string
		expected string
		want     string
	}{
		{"hello", "hello", "accepted"},
		{"hello\n", "hello", "output_whitespace_mismatch"},
		{"hello  \r\n", "hello", "output_whitespace_mismatch"},
		{"hello \nworld", "hello \nworld", "accepted"},
		{"hello \nworld\n", "hello \nworld", "output_whitespace_mismatch"},
		{"hello world", "hello world  ", "output_whitespace_mismatch"},
		{"hello", "world", "wrong_output"},
		{"hello", "hello world", "wrong_output"},
	}

	for _, tt := range tests {
		t.Run(tt.actual+"_vs_"+tt.expected, func(t *testing.T) {
			got := compareOutput(tt.actual, tt.expected)
			if got != tt.want {
				t.Errorf("compareOutput(%q, %q) = %q; want %q", tt.actual, tt.expected, got, tt.want)
			}
		})
	}
}
