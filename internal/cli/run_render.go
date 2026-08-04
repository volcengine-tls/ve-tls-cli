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

	if ctx.FormatOverride != "" {
		format = ctx.FormatOverride
		ctx.Format = format
	}
	if err != nil {
		if ue, ok := asUsageError(err); ok {
			_, _ = stdout.Write([]byte(ue.Text))
			return ue.ExitCode
		}
		if emitsStructuredEnvelope(group, rest) {
			if strings.TrimSpace(ctx.TraceDir) != "" {
				_ = ctx.initTrace()
			}
			env := buildAPIErrorEnvelope(ctx, group, err, outputMode)
			if ctx.Filter != "" && filterTargetsEnvelope(group, rest) {
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
					writeCLIError(stderr, err2, ctx.RequestID, ctx.StatusCode, "decode", "output write failed")
					return 3
				}
				_, code := classifyError(err, ctx.RequestID, ctx.StatusCode, group)
				return code
			}
			return writeStructuredError(stdout, stderr, err, ctx.RequestID, ctx.StatusCode, group, env)
		}
		return writeStructuredError(stdout, stderr, err, ctx.RequestID, ctx.StatusCode, group, nil)
	}
	applyEnvelopeFilter := filterTargetsEnvelope(group, rest)
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
			writeCLIError(stderr, errors.New("unsupported output: table"), ctx.RequestID, ctx.StatusCode, "usage", hint)
			return 1
		}
		if outputMode == "file" {
			p, err := writeOutputFileToDir(flags.OutputFile, ctx.OutputDir, group, out, format)
			if err != nil {
				writeCLIError(stderr, err, ctx.RequestID, ctx.StatusCode, "filesystem", "provide a writable --output-dir or check the local file path and permissions")
				return 2
			}
			_, _ = stdout.Write([]byte(p + "\n"))
			return exitCode
		}
		if err := output.Write(stdout, out, format); err != nil {
			writeCLIError(stderr, err, ctx.RequestID, ctx.StatusCode, "decode", "output write failed")
			return 3
		}
		return exitCode
	}
	if s, ok := out.(string); ok {
		if outputMode == "file" {
			p, err := writeTextFileToDir(flags.OutputFile, ctx.OutputDir, group, s)
			if err != nil {
				writeCLIError(stderr, err, ctx.RequestID, ctx.StatusCode, "filesystem", "provide a writable --output-dir or check the local file path and permissions")
				return 2
			}
			_, _ = stdout.Write([]byte(p + "\n"))
			return exitCode
		}
		_, _ = stdout.Write([]byte(s))
		return exitCode
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
			kind := "decode"
			hint := "output write failed"
			code := 3
			if outputMode == "file" {
				kind = "filesystem"
				hint = "provide a writable --output-dir or check the local file path and permissions"
				code = 2
			}
			writeCLIError(stderr, err, ctx.RequestID, ctx.StatusCode, kind, hint)
			return code
		}
		if ctx.Filter != "" && applyEnvelopeFilter {
			filtered, err := applyEnvelopeFilterResult(env, ctx.Filter)
			if err != nil {
				return writeFilterApplicationError(ctx, stdout, stderr, group, rest, env, outputMode, err)
			}
			if err := output.Write(stdout, filtered, output.FormatJSON); err != nil {
				writeCLIError(stderr, err, ctx.RequestID, ctx.StatusCode, "decode", "output write failed")
				return 3
			}
			return exitCode
		}
		if notice, ok := fileDeliveryNoticeForOutput(env, group, rest); ok {
			_, _ = stdout.Write([]byte(notice))
			return exitCode
		}
		if err := output.Write(stdout, env, output.FormatJSON); err != nil {
			writeCLIError(stderr, err, ctx.RequestID, ctx.StatusCode, "decode", "output write failed")
			return 3
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
		_, _ = stdout.Write([]byte(notice))
		return exitCode
	}
	if outputMode == "file" {
		p, err := writeOutputFileToDir(flags.OutputFile, ctx.OutputDir, group, out, format)
		if err != nil {
			writeCLIError(stderr, err, ctx.RequestID, ctx.StatusCode, "filesystem", "provide a writable --output-dir or check the local file path and permissions")
			return 2
		}
		_, _ = stdout.Write([]byte(p + "\n"))
		return exitCode
	}
	if err := output.Write(stdout, out, format); err != nil {
		writeCLIError(stderr, err, ctx.RequestID, ctx.StatusCode, "decode", "output write failed")
		return 3
	}
	return exitCode
}
