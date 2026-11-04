package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func readToolJSONObjectFlag(name string, value string, invocation ...*Context) (map[string]any, error) {
	raw, err := readToolJSONValueFlag(name, value, invocation...)
	if err != nil {
		return nil, err
	}
	object, ok := raw.(map[string]any)
	if !ok {
		return nil, errors.New("json must be object")
	}
	return object, nil
}

func readToolJSONValueFlag(name string, value string, invocation ...*Context) (any, error) {
	if strings.TrimSpace(value) == "" {
		return nil, errors.New("missing " + name + " value")
	}
	trimmed := strings.TrimSpace(value)
	if trimmed != "-" && !strings.HasPrefix(trimmed, "file://") && !strings.HasPrefix(trimmed, "{") {
		return nil, errors.New(name + " must use file://, -, or inline JSON object")
	}
	return inputInvocation(invocation).readJSONValueMaybeFile(trimmed)
}

func loadToolExecContext(value string, invocation ...*Context) (toolExecContext, error) {
	valueJSON, err := readToolJSONValueFlag("--context", value, invocation...)
	if err != nil {
		return toolExecContext{}, err
	}
	raw, ok := valueJSON.(map[string]any)
	if !ok {
		return toolExecContext{}, &toolContextValidationError{Path: "$", Reason: "must be an object"}
	}
	if err := validateToolExecContext(raw); err != nil {
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
	if err := ctx.applyExecutionControls(cfg); err != nil {
		return err
	}
	resolved, err := resolveRuntimeSelectors(runtimeSelectorSet{
		GlobalProfile:      ctx.Profile,
		GlobalSecretsFile:  ctx.GlobalSecretsFile,
		ContextProfile:     cfg.Profile,
		ContextSecretsFile: cfg.SecretsFile,
	})
	if err != nil {
		return err
	}
	region := strings.TrimSpace(cfg.Region)
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if region != "" && strings.TrimSpace(ctx.RuntimeRegion) != "" && region != strings.TrimSpace(ctx.RuntimeRegion) {
		return &toolContextValidationError{Path: "$.region", Reason: "conflicts with global --region"}
	}
	if endpoint != "" && strings.TrimSpace(ctx.RuntimeEndpoint) != "" && endpoint != strings.TrimSpace(ctx.RuntimeEndpoint) {
		return &toolContextValidationError{Path: "$.endpoint", Reason: "conflicts with global --endpoint"}
	}
	invalidate := ctx.Profile != resolved.Profile
	if strings.TrimSpace(resolved.SecretsFile) != "" {
		if err := loadSecretsFile(resolved.SecretsFile); err != nil {
			return err
		}
		ctx.forceStaticAuth = true
		invalidate = true
	}
	ctx.Profile = resolved.Profile
	if region != "" && ctx.RuntimeRegion != region {
		ctx.RuntimeRegion = region
		invalidate = true
	}
	if endpoint != "" && ctx.RuntimeEndpoint != endpoint {
		ctx.RuntimeEndpoint = endpoint
		invalidate = true
	}
	if invalidate {
		ctx.invalidateProfileCache()
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
		if dir, ok := outputCfg["dir"].(string); ok && strings.TrimSpace(dir) != "" {
			out.OutputDir = strings.TrimSpace(dir)
		}
		if path, ok := outputCfg["path"].(string); ok && strings.TrimSpace(path) != "" {
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
		if !value {
			ctx.TraceDir = ""
			return
		}
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
			ctx.TraceDir = ""
			return
		}
		if dir, ok := value["dir"].(string); ok && strings.TrimSpace(dir) != "" {
			ctx.TraceDir = strings.TrimSpace(dir)
		}
		if redact, ok := value["redact"].(string); ok && strings.TrimSpace(redact) != "" {
			ctx.TraceRedact = normalizeTraceRedactValue(strings.TrimSpace(redact))
		}
		if strings.TrimSpace(ctx.TraceDir) == "" {
			ctx.TraceDir = filepath.Join(os.TempDir(), "volclog-tool-trace")
		}
	}
}
