package cli

import (
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/output"
)

func isAPIEnvelopeCandidate(group string, out any) bool {
	g := strings.TrimSpace(group)
	if !isEnvelopeGroup(g) {
		return false
	}
	if g == "raw" {
		_, isString := out.(string)
		return !isString
	}
	return true
}

func isEnvelopeGroup(group string) bool {
	switch strings.TrimSpace(group) {
	case "raw", "project", "topic", "metric-topic", "index", "log", "assistant":
		return true
	default:
		return false
	}
}

func buildAPIEnvelope(ctx *Context, group string, out any, outputMode string, outputFile string, format output.Format) (map[string]any, error) {
	deliveryMode := "stdout"
	if outputMode == "file" {
		deliveryMode = "file_forced"
	}
	env := newAPISuccessEnvelope(ctx, group, out, outputMode, deliveryMode, nil)
	if outputMode == "file" {
		if fileOut, ok := out.(fileArtifactOutput); ok {
			env["artifacts"] = []map[string]any{buildOutputArtifact(fileOut.Path, fileOut.Format)}
			if err := updateEnvelopeTotalBytes(env); err != nil {
				return nil, err
			}
			return env, nil
		}
		p, err := resolveOutputFilePath(outputFile, ctx.OutputDir, group, output.FormatJSON)
		if err != nil {
			return nil, err
		}
		if err := materializeEnvelopeFile(p, env); err != nil {
			return nil, err
		}
		return env, nil
	}
	if err := updateEnvelopeTotalBytes(env); err != nil {
		return nil, err
	}
	return env, nil
}

func newAPISuccessEnvelope(ctx *Context, group string, data any, outputMode string, deliveryMode string, artifacts []map[string]any) map[string]any {
	if artifacts == nil {
		artifacts = []map[string]any{}
	}
	summary := map[string]any{
		"outputMode":   outputMode,
		"deliveryMode": deliveryMode,
		"dryRun":       ctx.DryRun,
		"itemCount":    envelopeItemCount(data),
		"totalBytes":   0,
	}
	if strings.TrimSpace(ctx.TracePath) != "" {
		summary["tracePath"] = ctx.TracePath
	}
	return map[string]any{
		"status":    "success",
		"action":    normalizeAPIAction(ctx.Action, group),
		"requestId": strings.TrimSpace(ctx.RequestID),
		"summary":   summary,
		"artifacts": artifacts,
		"data":      data,
		"error":     nil,
	}
}

func updateEnvelopeTotalBytes(env map[string]any) error {
	if env == nil {
		return nil
	}
	summary, _ := env["summary"].(map[string]any)
	if summary == nil {
		return nil
	}
	prev := -1
	for i := 0; i < 3; i++ {
		size, err := estimateOutputBytes(env, output.FormatJSON)
		if err != nil {
			return err
		}
		summary["totalBytes"] = size
		if size == prev {
			return nil
		}
		prev = size
	}
	return nil
}

func envelopeItemCount(v any) int {
	switch vv := v.(type) {
	case nil:
		return 0
	case []any:
		return len(vv)
	case []map[string]any:
		return len(vv)
	case map[string]any:
		for _, key := range []string{"items", "Items", "Logs", "Projects", "Topics", "Rows", "Results"} {
			if n, ok := envelopeSliceLen(vv[key]); ok {
				return n
			}
		}
		return 0
	default:
		return 0
	}
}

func envelopeSliceLen(v any) (int, bool) {
	switch vv := v.(type) {
	case []any:
		return len(vv), true
	case []map[string]any:
		return len(vv), true
	default:
		return 0, false
	}
}

func buildAPIErrorEnvelope(ctx *Context, group string, err error, outputMode string) map[string]any {
	requestID := strings.TrimSpace(ctx.RequestID)
	statusCode := ctx.StatusCode
	kind := "unknown"
	hint := ""
	source := "cli"
	code := "CLIError"
	if err != nil {
		p, _ := classifyError(err, ctx.RequestID, ctx.StatusCode, group)
		if strings.TrimSpace(p.RequestID) != "" {
			requestID = strings.TrimSpace(p.RequestID)
		}
		if p.StatusCode != 0 {
			statusCode = p.StatusCode
		}
		if strings.TrimSpace(p.Kind) != "" {
			kind = strings.TrimSpace(p.Kind)
		}
		if strings.TrimSpace(p.Hint) != "" {
			hint = strings.TrimSpace(p.Hint)
		}
	}
	summary := map[string]any{
		"outputMode":   outputMode,
		"deliveryMode": "stdout",
		"dryRun":       ctx.DryRun,
		"itemCount":    0,
		"totalBytes":   0,
	}
	if strings.TrimSpace(ctx.TracePath) != "" {
		summary["tracePath"] = ctx.TracePath
	}
	message := ""
	var details map[string]any
	if err != nil {
		message = err.Error()
		if he, ok := isHTTPError(err); ok {
			source = "upstream"
			code = "HTTPError"
			upstreamCode, upstreamMessage, upstreamDetails := parseHTTPErrorPayload(he)
			if upstreamCode != "" {
				code = upstreamCode
			}
			if upstreamMessage != "" {
				message = upstreamMessage
			}
			if upstreamDetails != nil {
				details = upstreamDetails
			}
		}
	}
	errObj := map[string]any{
		"source":     source,
		"code":       code,
		"message":    message,
		"requestId":  requestID,
		"statusCode": statusCode,
		"kind":       kind,
		"hint":       hint,
	}
	if details != nil {
		errObj["details"] = details
	}
	return map[string]any{
		"status":    "failed",
		"action":    normalizeAPIAction(ctx.Action, group),
		"requestId": requestID,
		"summary":   summary,
		"artifacts": []map[string]any{},
		"data":      nil,
		"error":     errObj,
	}
}

func normalizeAPIAction(action string, group string) string {
	a := strings.TrimSpace(action)
	if a == "" {
		if strings.TrimSpace(group) == "" {
			return "api.call"
		}
		return strings.TrimSpace(group)
	}
	return a
}
