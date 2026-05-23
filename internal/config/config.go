package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type LimitConfig struct {
	WallTimeS    int `yaml:"wall_time_s"`
	MemoryKb     int `yaml:"memory_kb"`
	MaxProcesses int `yaml:"max_processes"`
}

type CommandConfig struct {
	Cmd           string       `yaml:"cmd"`
	Args          []string     `yaml:"args,omitempty"`
	Limits        LimitConfig  `yaml:"limits"`
	FlagAllowlist []string     `yaml:"flag_allowlist,omitempty"`
}

type LanguageConfig struct {
	ID             string         `yaml:"id"`
	Name           string         `yaml:"name"`
	SourceFilename string         `yaml:"source_filename"`
	Artifact       string         `yaml:"artifact,omitempty"`
	Build          *CommandConfig `yaml:"build,omitempty"`
	Run            CommandConfig  `yaml:"run"`
}

type Registry struct {
	Languages []LanguageConfig `yaml:"languages"`
}

var registry *Registry

func LoadConfig(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read language config: %w", err)
	}

	var reg Registry
	if err := yaml.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal language config: %w", err)
	}

	registry = &reg
	return registry, nil
}

func GetRegistry() *Registry {
	return registry
}

func GetLanguage(id string) (*LanguageConfig, bool) {
	if registry == nil {
		return nil, false
	}
	for i := range registry.Languages {
		if registry.Languages[i].ID == id {
			return &registry.Languages[i], true
		}
	}
	return nil, false
}

func IsFlagAllowed(flag string, allowlist []string) bool {
	for _, pattern := range allowlist {
		if strings.HasSuffix(pattern, "*") {
			prefix := strings.TrimSuffix(pattern, "*")
			if strings.HasPrefix(flag, prefix) {
				return true
			}
		} else {
			if flag == pattern {
				return true
			}
		}
	}
	return false
}
