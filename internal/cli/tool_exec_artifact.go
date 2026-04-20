package cli

import (
	"bytes"
	"errors"
	"sort"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/output"
)

var toolExecAutoArtifactByteLimit = 16 * 1024

const (
	toolExecPreviewItemLimit = 3
	toolExecPreviewDepth     = 3
)

func finalizeToolExecEnvelope(ctx *Context, result any, env map[string]any, options toolExecutionOptions) (map[string]any, error) {
	if !shouldAutoArtifactToolExec(ctx, options) {
		return env, nil
	}
	size, err := estimateOutputBytes(env, ctx.Format)
	if err != nil {
		return env, nil
	}
	if size <= toolExecAutoArtifactByteLimit {
		return env, nil
	}
	filePath, err := resolveOutputFilePath("", ctx.OutputDir, "tool", output.FormatJSON)
	if err != nil {
		if errors.Is(err, errMissingWritableOutputDir) {
			return nil, errors.New("result too large for stdout; specify --output-dir <writable-dir> to allow automatic file delivery")
		}
		return nil, err
	}
	fileArtifact := []map[string]any{{
		"path":   filePath,
		"format": string(output.FormatJSON),
	}}
	fileEnv := newAPISuccessEnvelope(ctx, "tool", result, ctx.OutputMode, "file_auto", fileArtifact)
	if err := materializeEnvelopeFile(filePath, fileEnv); err != nil {
		return nil, err
	}
	summary, _ := env["summary"].(map[string]any)
	if summary == nil {
		summary = map[string]any{}
		env["summary"] = summary
	}
	summary["deliveryMode"] = "file_auto"
	summary["truncated"] = true
	summary["autoArtifact"] = true
	summary["fullBytes"] = size
	summary["hint"] = "full result written to artifact; use execution.projection or execution.artifact for deterministic control"
	summary["totalBytes"] = fileEnv["summary"].(map[string]any)["totalBytes"]
	env["artifacts"] = fileEnv["artifacts"]
	env["data"] = buildToolExecPreview(result)
	return env, nil
}

func shouldAutoArtifactToolExec(ctx *Context, options toolExecutionOptions) bool {
	if ctx == nil {
		return false
	}
	if ctx.OutputMode != "stdout" || ctx.OutputModeExplicit {
		return false
	}
	if ctx.Format != output.FormatJSON {
		return false
	}
	if strings.TrimSpace(ctx.Filter) != "" {
		return false
	}
	if options.Artifact || strings.TrimSpace(options.Projection) != "" {
		return false
	}
	return true
}

func estimateOutputBytes(v any, format output.Format) (int, error) {
	var buf bytes.Buffer
	if err := output.Write(&buf, v, format); err != nil {
		return 0, err
	}
	return buf.Len(), nil
}

func buildToolExecPreview(v any) any {
	preview, _ := buildToolExecPreviewValue(v, toolExecPreviewItemLimit, toolExecPreviewDepth)
	if mm, ok := preview.(map[string]any); ok {
		mm["omitted"] = map[string]any{
			"full_result": "see artifact",
		}
		return mm
	}
	return map[string]any{
		"preview": preview,
		"omitted": map[string]any{
			"full_result": "see artifact",
		},
	}
}

func buildToolExecPreviewValue(v any, itemLimit int, depth int) (any, bool) {
	if depth <= 0 {
		return v, false
	}
	switch vv := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(vv))
		for key := range vv {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out := make(map[string]any, len(vv))
		truncated := false
		for _, key := range keys {
			child, childTruncated := buildToolExecPreviewValue(vv[key], itemLimit, depth-1)
			out[key] = child
			truncated = truncated || childTruncated
		}
		return out, truncated
	case []any:
		if len(vv) <= itemLimit {
			out := make([]any, 0, len(vv))
			truncated := false
			for _, item := range vv {
				child, childTruncated := buildToolExecPreviewValue(item, itemLimit, depth-1)
				out = append(out, child)
				truncated = truncated || childTruncated
			}
			return out, truncated
		}
		out := make([]any, 0, itemLimit+1)
		for _, item := range vv[:itemLimit] {
			child, _ := buildToolExecPreviewValue(item, itemLimit, depth-1)
			out = append(out, child)
		}
		out = append(out, "... omitted, see artifact")
		return out, true
	default:
		return v, false
	}
}
