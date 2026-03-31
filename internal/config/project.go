package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type ProjectConfig struct {
	Region         string `json:"region,omitempty"`
	Endpoint       string `json:"endpoint,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
	Output         string `json:"output,omitempty"`
	OutputMode     string `json:"output_mode,omitempty"`
	TraceRedact    string `json:"trace_redact,omitempty"`
	HintsFile      string `json:"hints_file,omitempty"`
}

func LoadProjectConfig(wd string) (ProjectConfig, string, error) {
	base := strings.TrimSpace(wd)
	if base == "" {
		return ProjectConfig{}, "", errors.New("empty working directory")
	}
	p := filepath.Join(base, ".volclog", "cli.config.json")
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return ProjectConfig{}, p, nil
		}
		return ProjectConfig{}, "", err
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		return ProjectConfig{}, "", err
	}
	for k := range raw {
		lk := strings.ToLower(strings.TrimSpace(k))
		switch lk {
		case "access_key_id", "secret_access_key", "security_token", "ak", "sk", "token":
			return ProjectConfig{}, "", errors.New("project config must not contain credentials")
		}
	}
	var cfg ProjectConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		return ProjectConfig{}, "", err
	}
	cfg.Region = strings.TrimSpace(cfg.Region)
	cfg.Endpoint = strings.TrimSpace(cfg.Endpoint)
	cfg.Output = strings.TrimSpace(cfg.Output)
	cfg.OutputMode = strings.TrimSpace(cfg.OutputMode)
	cfg.TraceRedact = strings.TrimSpace(cfg.TraceRedact)
	cfg.HintsFile = strings.TrimSpace(cfg.HintsFile)
	return cfg, p, nil
}
