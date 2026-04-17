package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/util"
)

type toolExecContext struct {
	Profile        string         `json:"profile"`
	SecretsFile    string         `json:"secrets_file"`
	Region         string         `json:"region"`
	Endpoint       string         `json:"endpoint"`
	Trace          any            `json:"trace"`
	Execution      map[string]any `json:"execution"`
	ContractDigest string         `json:"contract_digest"`
}

type toolExecutionOptions struct {
	DryRun       bool
	Artifact     bool
	ArtifactPath string
	OutputDir    string
	Projection   string
	PageAll      bool
}

func readToolJSONObjectFlag(name string, value string) (map[string]any, error) {
	if strings.TrimSpace(value) == "" {
		return nil, errors.New("missing " + name + " value")
	}
	trimmed := strings.TrimSpace(value)
	if trimmed != "-" && !strings.HasPrefix(trimmed, "file://") && !strings.HasPrefix(trimmed, "{") {
		return nil, errors.New(name + " must use file://, -, or inline JSON object")
	}
	return util.ReadJSONObjectMaybeFile(trimmed)
}

func loadToolExecContext(value string) (toolExecContext, error) {
	raw, err := readToolJSONObjectFlag("--context", value)
	if err != nil {
		return toolExecContext{}, err
	}
	ctx := toolExecContext{}
	if v, ok := raw["profile"].(string); ok {
		ctx.Profile = strings.TrimSpace(v)
	}
	if v, ok := raw["secrets_file"].(string); ok {
		ctx.SecretsFile = strings.TrimSpace(v)
	}
	if v, ok := raw["region"].(string); ok {
		ctx.Region = strings.TrimSpace(v)
	}
	if v, ok := raw["endpoint"].(string); ok {
		ctx.Endpoint = strings.TrimSpace(v)
	}
	ctx.Trace = raw["trace"]
	if v, ok := raw["contract_digest"].(string); ok {
		ctx.ContractDigest = strings.TrimSpace(v)
	}
	if exec, ok := raw["execution"].(map[string]any); ok {
		ctx.Execution = exec
	} else {
		ctx.Execution = map[string]any{}
	}
	return ctx, nil
}

func applyToolExecContext(ctx *Context, cfg toolExecContext) error {
	globalProfile := strings.TrimSpace(ctx.Profile)
	contextProfile := strings.TrimSpace(cfg.Profile)
	if contextProfile != "" {
		if globalProfile != "" && globalProfile != contextProfile {
			return errors.New("conflicting profile selectors: global --profile=" + globalProfile + " conflicts with context.profile=" + contextProfile)
		}
		ctx.Profile = contextProfile
	}
	if strings.TrimSpace(cfg.SecretsFile) != "" {
		if err := loadSecretsFile(cfg.SecretsFile); err != nil {
			return err
		}
	}
	if strings.TrimSpace(cfg.Region) != "" {
		ctx.defaults.Region = strings.TrimSpace(cfg.Region)
	}
	if strings.TrimSpace(cfg.Endpoint) != "" {
		ctx.defaults.Endpoint = strings.TrimSpace(cfg.Endpoint)
	}
	applyToolTraceConfig(ctx, cfg.Trace)
	return nil
}

func resolveToolExecutionOptions(cfg toolExecContext) toolExecutionOptions {
	out := toolExecutionOptions{}
	exec := cfg.Execution
	if exec == nil {
		exec = map[string]any{}
	}
	if v, ok := exec["dry_run"].(bool); ok {
		out.DryRun = v
	}
	switch v := exec["artifact"].(type) {
	case bool:
		out.Artifact = v
	case string:
		out.Artifact = strings.TrimSpace(v) != ""
		out.ArtifactPath = strings.TrimSpace(v)
	case map[string]any:
		out.Artifact = true
		if p, ok := v["path"].(string); ok {
			out.ArtifactPath = strings.TrimSpace(p)
		}
		if dir, ok := v["dir"].(string); ok {
			out.OutputDir = strings.TrimSpace(dir)
		}
	}
	if mode, ok := exec["output_mode"].(string); ok {
		switch strings.ToLower(strings.TrimSpace(mode)) {
		case "artifact", "file":
			out.Artifact = true
		}
	}
	if outputCfg, ok := exec["output"].(map[string]any); ok {
		if mode, ok := outputCfg["mode"].(string); ok {
			switch strings.ToLower(strings.TrimSpace(mode)) {
			case "artifact", "file":
				out.Artifact = true
			}
		}
		if dir, ok := outputCfg["dir"].(string); ok {
			out.OutputDir = strings.TrimSpace(dir)
		}
		if path, ok := outputCfg["path"].(string); ok {
			out.ArtifactPath = strings.TrimSpace(path)
		}
	}
	out.Projection = resolveToolProjection(exec["projection"])
	if page, ok := exec["page"].(map[string]any); ok {
		if all, ok := page["all"].(bool); ok {
			out.PageAll = all
		}
	}
	if all, ok := exec["page_all"].(bool); ok {
		out.PageAll = all
	}
	return out
}

func resolveToolProjection(v any) string {
	switch value := v.(type) {
	case string:
		return strings.TrimSpace(value)
	case map[string]any:
		if jmes, ok := value["jmes"].(string); ok {
			return strings.TrimSpace(jmes)
		}
	case []any:
		for _, item := range value {
			if expr := resolveToolProjection(item); expr != "" {
				return expr
			}
		}
	case []string:
		for _, item := range value {
			if expr := strings.TrimSpace(item); expr != "" {
				return expr
			}
		}
	}
	return ""
}

func applyToolTraceConfig(ctx *Context, trace any) {
	switch value := trace.(type) {
	case bool:
		if value && strings.TrimSpace(ctx.TraceDir) == "" {
			ctx.TraceDir = filepath.Join(os.TempDir(), "volclog-tool-trace")
		}
	case string:
		if dir := strings.TrimSpace(value); dir != "" {
			ctx.TraceDir = dir
		}
	case map[string]any:
		enabled := true
		if flag, ok := value["enabled"].(bool); ok {
			enabled = flag
		}
		if !enabled {
			return
		}
		if dir, ok := value["dir"].(string); ok && strings.TrimSpace(dir) != "" {
			ctx.TraceDir = strings.TrimSpace(dir)
		}
		if redact, ok := value["redact"].(string); ok && strings.TrimSpace(redact) != "" {
			ctx.TraceRedact = strings.TrimSpace(redact)
		}
		if strings.TrimSpace(ctx.TraceDir) == "" {
			ctx.TraceDir = filepath.Join(os.TempDir(), "volclog-tool-trace")
		}
	}
}
