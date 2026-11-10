package cli

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/execution"
	"github.com/volcengine-tls/ve-tls-cli/internal/jsonx"
	"github.com/volcengine-tls/ve-tls-cli/internal/output"
)

func completenessValue(value any) *execution.Completeness {
	if state, ok := value.(*execution.Completeness); ok {
		return state
	}
	if value == nil {
		return nil
	}
	raw, err := marshalOutputBytes(value, output.FormatJSON)
	if err != nil {
		return nil
	}
	var state execution.Completeness
	if jsonx.Unmarshal(raw, &state) != nil {
		return nil
	}
	return &state
}

// Keep partial collection results in a self-describing envelope artifact when
// file delivery was requested. Successful export data files use their own
// completeness sidecars instead.
func prepareIncompleteDelivery(ctx *Context, group string, env map[string]any) (map[string]any, error) {
	summary, _ := env["summary"].(map[string]any)
	state := completenessValue(summary["completeness"])
	if state == nil || state.Complete || state.ItemCount == 0 || env["data"] == nil {
		return env, nil
	}
	if _, ok := firstArtifactPath(env["artifacts"]); ok {
		return env, nil
	}
	if ctx.OutputMode != "file" {
		if strings.TrimSpace(ctx.OutputDir) == "" || ctx.OutputModeExplicit || strings.TrimSpace(ctx.Filter) != "" {
			return env, nil
		}
		size, err := estimateOutputBytes(env, output.FormatJSON)
		if err != nil || size <= toolExecAutoArtifactByteLimit {
			return env, nil
		}
	}
	path, err := resolveOutputFilePath(ctx.OutputFile, ctx.OutputDir, group, output.FormatJSON)
	if err != nil {
		return env, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return env, err
	}
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	file, err := os.CreateTemp(filepath.Dir(path), "."+base+".partial-*.json")
	if err != nil {
		return env, err
	}
	path = file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return env, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(path)
		}
	}()
	full := cloneOutcomeMap(env)
	fullSummary := cloneOutcomeMap(summary)
	fullSummary["deliveryMode"] = "file_partial"
	full["summary"] = fullSummary
	artifact := fileArtifactDescriptor(fileArtifactOutput{Path: path, Format: output.FormatJSON, Completeness: state})
	full["artifacts"] = []map[string]any{artifact}
	lastSize := -1
	for i := 0; i < 8; i++ {
		if lastSize >= 0 {
			artifact["sizeBytes"] = lastSize
		}
		if err := updateEnvelopeTotalBytes(full); err != nil {
			return env, err
		}
		encoded, err := marshalOutputBytes(full, output.FormatJSON)
		if err != nil {
			return env, err
		}
		if len(encoded) == lastSize {
			if err := writeFilteredOutputFile(path, encoded); err != nil {
				return env, err
			}
			keep = true
			env["artifacts"] = full["artifacts"]
			env["data"] = map[string]any{"itemCount": state.ItemCount, "partialResult": "see artifact"}
			summary["deliveryMode"] = "file_partial"
			_ = updateEnvelopeTotalBytes(env)
			return env, nil
		}
		lastSize = len(encoded)
	}
	return env, &outputFailureError{Stage: "artifact", Outcome: currentRemoteOutcome(ctx, nil), Exit: 2}
}
