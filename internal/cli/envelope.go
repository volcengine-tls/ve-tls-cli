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
	summary := map[string]any{
		"outputMode": outputMode,
		"dryRun":     ctx.DryRun,
	}
	if strings.TrimSpace(ctx.TracePath) != "" {
		summary["tracePath"] = ctx.TracePath
	}
	env := map[string]any{
		"status":    "success",
		"action":    normalizeAPIAction(ctx.Action, group),
		"requestId": strings.TrimSpace(ctx.RequestID),
		"summary":   summary,
		"artifacts": []map[string]any{},
		"error":     nil,
	}
	if outputMode == "file" {
		if fileOut, ok := out.(fileArtifactOutput); ok {
			env["artifacts"] = []map[string]any{buildOutputArtifact(fileOut.Path, fileOut.Format)}
			return env, nil
		}
		p, err := writeOutputFileToDir(outputFile, ctx.OutputDir, group, out, format)
		if err != nil {
			return nil, err
		}
		env["artifacts"] = []map[string]any{buildOutputArtifact(p, format)}
		return env, nil
	}
	env["data"] = out
	return env, nil
}

func buildAPIErrorEnvelope(ctx *Context, group string, err error, outputMode string) map[string]any {
	requestID := strings.TrimSpace(ctx.RequestID)
	statusCode := ctx.StatusCode
	kind := "unknown"
	hint := ""
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
		"outputMode": outputMode,
		"dryRun":     ctx.DryRun,
	}
	if strings.TrimSpace(ctx.TracePath) != "" {
		summary["tracePath"] = ctx.TracePath
	}
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	return map[string]any{
		"status":    "failed",
		"action":    normalizeAPIAction(ctx.Action, group),
		"requestId": requestID,
		"summary":   summary,
		"artifacts": []map[string]any{},
		"data":      nil,
		"error": map[string]any{
			"errorCode":    "CLIError",
			"errorMessage": errMsg,
			"requestId":    requestID,
			"statusCode":   statusCode,
			"kind":         kind,
			"hint":         hint,
		},
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
