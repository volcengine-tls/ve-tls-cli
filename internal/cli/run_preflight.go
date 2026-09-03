package cli

import (
	"errors"
	"io"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/output"
)

func preflightRunInvocation(invocation *runInvocation, stdout, stderr io.Writer) (int, bool) {
	ctx := invocation.ctx
	group := invocation.group
	rest := invocation.rest
	flags := invocation.flags
	outputMode := invocation.outputMode

	// login/logout/sso and configure sso freeze stdout to their exact JSON result
	// shape. Reject any global option that would rewrite, divert, or wrap that
	// result before any side effect runs (and before the generic file preflight
	// can misclassify --output-mode file as a filesystem error). This must happen
	// before the adapter factory is built so no login/configure-sso side effect
	// runs when stdout would be diverted.
	if isInteractiveAuthCommand(group, rest) {
		if err := rejectFrozenOutputOptions(ctx); err != nil {
			return writeRunError(ctx, stdout, stderr, group, rest, err, "usage", "login/logout/sso/configure-sso writes only JSON to stdout; remove output/file/filter/trace flags", outputMode, 1), true
		}
	}
	if filter := strings.TrimSpace(ctx.Filter); filter != "" {
		if err := output.Validate(filter); err != nil {
			return writeRunError(ctx, stdout, stderr, group, rest, err, "decode", "invalid --jmes-filter", outputMode, 3), true
		}
	}

	if outputMode != "stdout" && outputMode != "file" {
		return writeRunError(ctx, stdout, stderr, group, rest, errors.New("unsupported output-mode: "+flags.OutputMode), "usage", "invalid --output-mode", outputMode, 1), true
	}
	if outputMode == "file" && strings.TrimSpace(flags.Filter) != "" && filterTargetsEnvelope(group, rest) {
		return writeRunError(ctx, stdout, stderr, group, rest, errors.New("--jmes-filter cannot be combined with file delivery"), "incompatible_flags", "remove --jmes-filter or use stdout delivery", outputMode, 1), true
	}
	if rejectsOutputFileForGroup(group) && strings.TrimSpace(flags.OutputFile) != "" {
		return writeRunError(ctx, stdout, stderr, group, rest, errors.New("--output-file is not supported for "+strings.TrimSpace(group)), "usage", "use output_dir-based delivery instead", outputMode, 1), true
	}
	if err := ctx.validateDryRunScope(group, rest); err != nil {
		return writeRunError(ctx, stdout, stderr, group, rest, err, "usage", "invalid --dry-run scope", outputMode, 1), true
	}
	if outputMode == "file" {
		if err := preflightOutputFilePath(flags.OutputFile, ctx.OutputDir, group, knownFileDeliveryFormat(group, rest, invocation.format)); err != nil {
			return writeRunError(ctx, stdout, stderr, group, rest, err, "filesystem", "provide a writable --output-dir or check the local file path and permissions", outputMode, 2), true
		}
	}
	if ctx.GlobalSecretsFile != "" {
		if err := preflightGlobalSecretsFile(group, ctx.Profile, ctx.GlobalSecretsFile); err != nil {
			if emitsStructuredEnvelope(group, rest) {
				return writeStructuredError(stdout, stderr, err, "", 0, group, buildAPIErrorEnvelope(ctx, group, err, outputMode)), true
			}
			hint := "use exactly one runtime selector: --profile or --secrets-file"
			if rejectsSecretsFileForGroup(group) {
				hint = "--secrets-file cannot be combined with interactive login commands"
			}
			writeCLIError(stderr, err, "", 0, "validation", hint)
			return 1, true
		}
		if !defersSecretsResolutionToCommand(group, rest) {
			if err := loadSecretsFile(ctx.GlobalSecretsFile); err != nil {
				if emitsStructuredEnvelope(group, rest) {
					return writeStructuredError(stdout, stderr, err, "", 0, group, buildAPIErrorEnvelope(ctx, group, err, outputMode)), true
				}
				writeCLIError(stderr, err, "", 0, "config", "failed to load --secrets-file")
				return 2, true
			}
			ctx.forceStaticAuth = true
		}
	}
	return 0, false
}
