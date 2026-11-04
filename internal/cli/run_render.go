package cli

import (
	"errors"
	"io"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/output"
)

func renderRunResult(invocation *runInvocation, stdout, stderr io.Writer, out any, exitCode int, err error) int {
	ctx := invocation.ctx
	group := invocation.group
	rest := invocation.rest
	flags := invocation.flags
	format := invocation.format
	outputMode := invocation.outputMode
	var deliveredEnvelope map[string]any
	if (group == "tool" || group == "workflow") && len(rest) > 0 && rest[0] == "exec" {
		deliveredEnvelope, _ = structuredEnvelopeOutput(out)
	}
	failDelivery := func(err error, stage string, code int) int {
		return writeDeliveryFailure(ctx, stdout, stderr, group, err, stage, code, deliveredEnvelope)
	}
	failStdout := func(err error) int { return writeDeliveryFailureToStderr(ctx, stderr, group, err, deliveredEnvelope) }
	writeNotice := func(text string) int {
		if err := writeOutputNotice(stdout, text); err != nil {
			return failStdout(err)
		}
		return exitCode
	}
	if strings.TrimSpace(ctx.Filter) != "" && filterTargetsEnvelope(group, rest) && ctx.OutputMode == "file" {
		outputMode = ctx.OutputMode
	}

	if ctx.FormatOverride != "" {
		format = ctx.FormatOverride
		ctx.Format = format
	}
	if err != nil {
		if ue, ok := asUsageError(err); ok {
			_, _ = stdout.Write([]byte(ue.Text))
			return ue.ExitCode
		}
		if emitsStructuredEnvelope(group, rest) || ctx.Completeness != nil {
			if strings.TrimSpace(ctx.TraceDir) != "" {
				_ = ctx.initTrace()
			}
			env := buildAPIErrorEnvelope(ctx, group, err, outputMode)
			if prepared, deliveryErr := prepareIncompleteDelivery(ctx, group, env); deliveryErr != nil {
				failed := buildAPIErrorEnvelope(ctx, group, outputFailure(ctx, deliveryErr, "artifact", 2), outputMode)
				failed["data"] = env["data"]
				return writeStructuredError(stdout, stderr, outputFailure(ctx, deliveryErr, "artifact", 2), ctx.RequestID, ctx.StatusCode, group, failed)
			} else {
				env = prepared
			}
			if ctx.Filter != "" && filterTargetsEnvelope(group, rest) && outputMode != "file" {
				filtered, err2 := applyEnvelopeFilterResult(env, ctx.Filter)
				if err2 != nil {
					appendEnvelopeWarning(env, map[string]any{
						"kind":    "filter_no_value",
						"message": err2.Error(),
						"policy":  "soft",
					})
					return writeStructuredError(stdout, stderr, err, ctx.RequestID, ctx.StatusCode, group, env)
				}
				if err2 := output.Write(stdout, filtered, output.FormatJSON); err2 != nil {
					return failStdout(err2)
				}
				_, code := classifyError(err, ctx.RequestID, ctx.StatusCode, group)
				return code
			}
			return writeStructuredError(stdout, stderr, err, ctx.RequestID, ctx.StatusCode, group, env)
		}
		return writeStructuredError(stdout, stderr, err, ctx.RequestID, ctx.StatusCode, group, nil)
	}
	applyEnvelopeFilter := filterTargetsEnvelope(group, rest)
	if strings.TrimSpace(ctx.Filter) != "" && applyEnvelopeFilter && outputMode == "file" {
		env, ok := structuredEnvelopeOutput(out)
		// Raw service data must always be wrapped locally; do not trust a
		// response that happens to resemble a CLI envelope or artifact path.
		if group == "raw" || !ok {
			var err error
			env, err = buildAPIEnvelope(ctx, group, out, outputMode, ctx.OutputFile, format)
			if err != nil {
				return failDelivery(err, "artifact", 2)
			}
		}
		return renderFilteredEnvelopeFile(ctx, stdout, stderr, group, rest, env, exitCode)
	}
	if ctx.Filter != "" && !applyEnvelopeFilter {
		filterTarget := out
		out, err = output.ApplyFilter(out, ctx.Filter)
		if err != nil {
			return writeFilterApplicationError(ctx, stdout, stderr, group, rest, filterTarget, outputMode, err)
		}
	}
	if format == output.FormatTable {
		if !supportsTableOutput(ctx) {
			hint := "table is only supported for project/topic/metric-topic list|get, index get, and log search"
			if currentEdition() == cliEditionVolclog {
				hint = "table is not supported on the default volclog agent path; use --output json|jsonl"
			}
			if currentRemoteOutcome(ctx, nil) != "not_sent" {
				return failDelivery(errors.New("unsupported output: table"), "format", 3)
			}
			writeCLIError(stderr, errors.New("unsupported output: table"), ctx.RequestID, ctx.StatusCode, "usage", hint)
			return 1
		}
		if outputMode == "file" {
			p, err := writeOutputFileToDir(flags.OutputFile, ctx.OutputDir, group, out, format)
			if err != nil {
				return failDelivery(err, "artifact", 2)
			}
			deliveredEnvelope = map[string]any{"artifacts": []map[string]any{buildOutputArtifact(p, format)}}
			return writeNotice(p + "\n")
		}
		if err := output.Write(stdout, out, format); err != nil {
			return failStdout(err)
		}
		return exitCode
	}
	if s, ok := out.(string); ok {
		if outputMode == "file" {
			p, err := writeTextFileToDir(flags.OutputFile, ctx.OutputDir, group, s)
			if err != nil {
				return failDelivery(err, "artifact", 2)
			}
			deliveredEnvelope = map[string]any{"artifacts": []map[string]any{buildOutputArtifact(p, format)}}
			return writeNotice(p + "\n")
		}
		return writeNotice(s)
	}
	if strings.TrimSpace(ctx.TraceDir) != "" {
		_ = ctx.initTrace()
		if strings.TrimSpace(ctx.TracePath) != "" && !isAPIEnvelopeCandidate(group, out) {
			out = attachMeta(out, ctx.TracePath)
		}
	}
	if isAPIEnvelopeCandidate(group, out) {
		env, err := buildAPIEnvelope(ctx, group, out, outputMode, flags.OutputFile, format)
		if err != nil {
			code := 3
			if outputMode == "file" {
				code = 2
			}
			return failDelivery(err, "artifact", code)
		}
		deliveredEnvelope = env
		if ctx.Filter != "" && applyEnvelopeFilter {
			filtered, err := applyEnvelopeFilterResult(env, ctx.Filter)
			if err != nil {
				return writeFilterApplicationError(ctx, stdout, stderr, group, rest, env, outputMode, err)
			}
			if err := output.Write(stdout, filtered, output.FormatJSON); err != nil {
				return failStdout(err)
			}
			return exitCode
		}
		if notice, ok := fileDeliveryNoticeForOutput(env, group, rest); ok {
			return writeNotice(notice)
		}
		if err := output.Write(stdout, env, output.FormatJSON); err != nil {
			return failStdout(err)
		}
		return exitCode
	}
	if ctx.Filter != "" && applyEnvelopeFilter {
		filterTarget := out
		out, err = applyEnvelopeFilterResult(out, ctx.Filter)
		if err != nil {
			return writeFilterApplicationError(ctx, stdout, stderr, group, rest, filterTarget, outputMode, err)
		}
	}
	if notice, ok := fileDeliveryNoticeForOutput(out, group, rest); ok {
		return writeNotice(notice)
	}
	if outputMode == "file" {
		p, err := writeOutputFileToDir(flags.OutputFile, ctx.OutputDir, group, out, format)
		if err != nil {
			return failDelivery(err, "artifact", 2)
		}
		deliveredEnvelope = map[string]any{"artifacts": []map[string]any{buildOutputArtifact(p, format)}}
		return writeNotice(p + "\n")
	}
	if err := output.Write(stdout, out, format); err != nil {
		return failStdout(err)
	}
	return exitCode
}
