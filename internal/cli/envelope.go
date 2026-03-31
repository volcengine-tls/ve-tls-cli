package cli

import (
	"os"
	"strings"

	"volclog/internal/output"
)

func isAPIEnvelopeCandidate(group string, out any) bool {
	if strings.TrimSpace(group) != "api" {
		return false
	}
	_, isString := out.(string)
	return !isString
}

func buildAPIEnvelope(ctx *Context, out any, outputMode string, outputFile string, format output.Format) (map[string]any, error) {
	summary := map[string]any{
		"outputMode": outputMode,
		"dryRun":     ctx.DryRun,
	}
	if strings.TrimSpace(ctx.TracePath) != "" {
		summary["tracePath"] = ctx.TracePath
	}
	env := map[string]any{
		"status":    "success",
		"action":    normalizeAPIAction(ctx.Action),
		"requestId": strings.TrimSpace(ctx.RequestID),
		"summary":   summary,
		"artifacts": []map[string]any{},
		"error":     nil,
	}
	if outputMode == "file" {
		p, err := writeOutputFile(outputFile, "api", out, format)
		if err != nil {
			return nil, err
		}
		artifact := map[string]any{
			"path":   p,
			"format": string(format),
		}
		if fi, err := os.Stat(p); err == nil {
			artifact["sizeBytes"] = fi.Size()
		}
		env["artifacts"] = []map[string]any{artifact}
		return env, nil
	}
	env["data"] = out
	return env, nil
}

func normalizeAPIAction(action string) string {
	a := strings.TrimSpace(action)
	if a == "" {
		return "api.call"
	}
	return a
}
