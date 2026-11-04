package cli

import (
	"errors"
	"io"
	"os"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/config"
	"github.com/volcengine-tls/ve-tls-cli/internal/output"
	"github.com/volcengine-tls/ve-tls-cli/internal/version"
)

type runInvocation struct {
	group      string
	rest       []string
	flags      GlobalFlags
	format     output.Format
	outputMode string
	ctx        *Context
}

func prepareRunInvocation(args []string, stdout, stderr io.Writer) (*runInvocation, int, bool) {
	if len(args) == 0 {
		if defaultAgentErrorProtocol("", nil) {
			code := writeInvocationFailure(stdout, stderr, "cli.invoke", nil, errors.New("missing command"), "usage", "run 'volclog --help' to inspect commands", 1)
			return nil, code, true
		}
		_, _ = stderr.Write([]byte(usageText()))
		return nil, 1, true
	}

	group, rest, flags, ok := parseGlobal(args)
	if !ok {
		if defaultAgentErrorProtocol("", nil) {
			code := writeInvocationFailure(stdout, stderr, "cli.invoke", nil, errors.New("invalid global flags or missing flag value"), "usage", "run 'volclog --help' to inspect global flags", 1)
			return nil, code, true
		}
		_, _ = stderr.Write([]byte(usageText()))
		return nil, 1, true
	}
	if allowsTrailingGlobalsForGroup(group) {
		tail, merged, ok := extractTrailingGlobals(rest, flags, allowsTrailingDryRun(group, rest))
		if !ok {
			if defaultAgentErrorProtocol(group, rest) {
				code := writeInvocationFailure(stdout, stderr, group, rest, errors.New("missing global flag value"), "usage", "run 'volclog --help' to inspect global flags", 1)
				return nil, code, true
			}
			_, _ = stderr.Write([]byte(usageText()))
			return nil, 1, true
		}
		rest, flags = tail, merged
	} else {
		tail, merged, ok := extractTrailingInvocationControls(rest, flags)
		if !ok {
			code := writeInvocationFailure(stdout, stderr, group, rest, errors.New("missing invocation control value"), "usage", "check --timeout", 1)
			return nil, code, true
		}
		rest, flags = tail, merged
	}
	if flags.ShowHelp {
		if currentEdition() == cliEditionVolclog {
			if err := writeOutputNotice(stdout, usageText()); err != nil {
				env := buildAPIErrorEnvelope(nil, "cli.invoke", err, "stdout")
				return nil, writeBrokenStdout(stderr, err, env), true
			}
		} else {
			_, _ = stdout.Write([]byte(usageText()))
		}
		return nil, 0, true
	}
	if flags.ShowVersion {
		text := "volclog " + version.Version + "\n"
		if defaultAgentErrorProtocol(group, rest) {
			if err := writeOutputNotice(stdout, text); err != nil {
				env := buildAPIErrorEnvelope(nil, "version", err, "stdout")
				return nil, writeBrokenStdout(stderr, err, env), true
			}
		} else {
			_, _ = stdout.Write([]byte(text))
		}
		return nil, 0, true
	}
	if group == "version" {
		if len(rest) != 0 {
			err := errors.New("version does not accept arguments")
			if defaultAgentErrorProtocol(group, rest) {
				code := writeInvocationFailure(stdout, stderr, group, rest, err, "usage", "use 'volclog version'", 1)
				return nil, code, true
			}
			writeCLIError(stderr, err, "", 0, "usage", "use 'volclog version'")
			return nil, 1, true
		}
		versionStdout := stdout
		var observed *invocationStdoutWriter
		if defaultAgentErrorProtocol(group, rest) {
			observed = &invocationStdoutWriter{writer: stdout}
			versionStdout = observed
		}
		if err := writeVersionInfo(versionStdout); err != nil {
			if observed != nil {
				if observed.err != nil {
					env := buildAPIErrorEnvelope(nil, group, err, "stdout")
					return nil, writeBrokenStdout(stderr, observed.err, env), true
				}
				code := writeInvocationFailure(stdout, stderr, group, rest, err, "internal", "failed to build version information", 3)
				return nil, code, true
			}
			writeCLIError(stderr, err, "", 0, "internal", "failed to build version information")
			return nil, 3, true
		}
		return nil, 0, true
	}

	wd, err := os.Getwd()
	if err != nil {
		if defaultAgentErrorProtocol(group, rest) {
			code := writeInvocationFailure(stdout, stderr, group, rest, err, "config", "failed to get working directory", 2)
			return nil, code, true
		}
		writeCLIError(stderr, err, "", 0, "config", "failed to get working directory")
		return nil, 2, true
	}
	projectCfg, _, err := config.LoadProjectConfig(wd)
	if err != nil {
		if defaultAgentErrorProtocol(group, rest) {
			code := writeInvocationFailure(stdout, stderr, group, rest, err, "config", "failed to load project config", 2)
			return nil, code, true
		}
		writeCLIError(stderr, err, "", 0, "config", "failed to load project config")
		return nil, 2, true
	}
	outputExplicit := strings.TrimSpace(flags.Output) != ""
	if strings.TrimSpace(flags.Output) == "" && strings.TrimSpace(projectCfg.Output) != "" {
		flags.Output = projectCfg.Output
	}
	if strings.TrimSpace(flags.OutputMode) == "" && strings.TrimSpace(projectCfg.OutputMode) != "" {
		flags.OutputMode = projectCfg.OutputMode
	}
	if strings.TrimSpace(flags.TraceRedact) == "" && strings.TrimSpace(projectCfg.TraceRedact) != "" {
		flags.TraceRedact = projectCfg.TraceRedact
	}
	if group == "log" && len(rest) > 0 && rest[0] == "export-analysis" && !outputExplicit {
		flags.Output = "jsonl"
	}

	if !isRecognizedGroup(group) {
		if defaultAgentErrorProtocol(group, rest) {
			code := writeInvocationFailure(stdout, stderr, "cli.invoke", nil, errors.New("unknown command"), "usage", "run 'volclog --help' to inspect commands", 1)
			return nil, code, true
		}
		_, _ = stderr.Write([]byte(usageText()))
		return nil, 1, true
	}
	if !isGroupEnabledInCurrentEdition(group) {
		err := errors.New("group not available in " + string(currentEdition()) + " edition: " + group)
		if defaultAgentErrorProtocol(group, rest) {
			code := writeInvocationFailure(stdout, stderr, group, rest, err, "usage", editionGroupHint(group), 1)
			return nil, code, true
		}
		writeCLIError(stderr, err, "", 0, "usage", editionGroupHint(group))
		return nil, 1, true
	}

	format, err := output.ParseFormat(flags.Output)
	if err != nil {
		if defaultAgentErrorProtocol(group, rest) {
			code := writeInvocationFailure(stdout, stderr, group, rest, errors.New("invalid output format"), "usage", "invalid --output", 1)
			return nil, code, true
		}
		writeCLIError(stderr, err, "", 0, "usage", "invalid --output")
		return nil, 1, true
	}

	outputMode := strings.ToLower(strings.TrimSpace(flags.OutputMode))
	if outputMode == "" {
		outputMode = "stdout"
	}

	ctx := newContext(stdout, stderr, format, flags.Profile, flags.Filter)
	ctx.RuntimeRegion = strings.TrimSpace(flags.Region)
	ctx.RuntimeEndpoint = strings.TrimSpace(flags.Endpoint)
	ctx.OutputExplicit = outputExplicit
	ctx.OutputMode = outputMode
	ctx.OutputModeExplicit = flags.OutputModeExplicit
	ctx.OutputDir = strings.TrimSpace(flags.OutputDir)
	ctx.OutputFile = flags.OutputFile
	ctx.GlobalSecretsFile = strings.TrimSpace(flags.SecretsFile)
	ctx.TraceDir = flags.TraceDir
	ctx.TraceRedact = normalizeTraceRedactValue(flags.TraceRedact)
	ctx.DryRun = flags.DryRun
	ctx.NonInteractive = flags.NonInteractive || !stdinIsTerminal()
	ctx.NonInteractiveExplicit = flags.NonInteractive
	ctx.AllowWrite, ctx.AllowWriteExplicit = flags.AllowWrite, flags.AllowWrite
	if flags.ControlConflict {
		code := writeRunError(ctx, stdout, stderr, group, rest, validationFailure(errors.New("conflicting --timeout values"), "InvalidExecutionControls"), "validation", "use one overall timeout", outputMode, 1)
		return nil, code, true
	}
	if flags.Timeout != "" {
		duration, err := parseInvocationTimeout(flags.Timeout)
		if err != nil {
			code := writeRunError(ctx, stdout, stderr, group, rest, validationFailure(err, "InvalidTimeout"), "validation", "use --timeout 30s", outputMode, 1)
			return nil, code, true
		}
		ctx.Timeout, ctx.TimeoutExplicit = duration, true
	}
	if err := validateTraceRedactValue(flags.TraceRedact); err != nil {
		ctx.TraceDir = ""
		code := writeRunError(ctx, stdout, stderr, group, rest, validationFailure(err, "InvalidTraceRedaction"), "validation", "trace redaction must remain enabled", outputMode, 1)
		return nil, code, true
	}
	ctx.SetProfileDefaults(config.ProfileDefaults{
		Region:         projectCfg.Region,
		Endpoint:       projectCfg.Endpoint,
		TimeoutSeconds: projectCfg.TimeoutSeconds,
	})

	return &runInvocation{
		group:      group,
		rest:       rest,
		flags:      flags,
		format:     format,
		outputMode: outputMode,
		ctx:        ctx,
	}, 0, false
}

func dispatchRunInvocation(invocation *runInvocation, stdout, stderr io.Writer, factory loginAdapterFactory, ssoFactory ssoAdapterFactory) (any, int, error, bool) {
	ctx := invocation.ctx
	group := invocation.group
	rest := invocation.rest
	var (
		out      any
		err      error
		exitCode int
	)
	switch group {
	case "configure":
		out, err = runConfigure(ctx, rest, ssoFactory)
	case "tool":
		out, err = runTool(ctx, rest)
	case "workflow":
		out, err = runWorkflow(ctx, rest)
	case "raw":
		out, err = runRaw(ctx, rest)
	case "skill":
		out, err = runSkill(ctx, rest)
	case "upgrade":
		out, err = runUpgrade(ctx, rest)
	case "capabilities":
		err = removedLegacyCommandError("capabilities", legacyCapabilitiesHint(rest))
	case "api":
		err = removedLegacyCommandError("api", legacyAPIHint(rest))
	case "doctor":
		out, exitCode, err = runDoctor(ctx, rest)
	case "login":
		out, err = runLoginWithFactory(ctx, rest, factory)
	case "logout":
		out, err = runLogoutWithFactory(ctx, rest, factory)
	case "sso":
		out, err = runSSOGroup(ctx, rest, ssoFactory)
	default:
		handled := false
		out, handled, err = runEditionSpecificGroup(ctx, group, rest)
		if !handled {
			if defaultAgentErrorProtocol(group, rest) {
				code := writeInvocationFailure(stdout, stderr, "cli.invoke", nil, errors.New("unknown command"), "usage", "run 'volclog --help' to inspect commands", 1)
				return nil, code, nil, true
			}
			_, _ = stderr.Write([]byte(usageText()))
			return nil, 1, nil, true
		}
	}
	return out, exitCode, err, false
}
