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
		_, _ = stderr.Write([]byte(usageText()))
		return nil, 1, true
	}

	group, rest, flags, ok := parseGlobal(args)
	if !ok {
		_, _ = stderr.Write([]byte(usageText()))
		return nil, 1, true
	}
	if allowsTrailingGlobalsForGroup(group) {
		rest, flags, ok = extractTrailingGlobals(rest, flags, allowsTrailingDryRun(group, rest))
		if !ok {
			_, _ = stderr.Write([]byte(usageText()))
			return nil, 1, true
		}
	}

	wd, err := os.Getwd()
	if err != nil {
		writeCLIError(stderr, err, "", 0, "config", "failed to get working directory")
		return nil, 2, true
	}
	projectCfg, _, err := config.LoadProjectConfig(wd)
	if err != nil {
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

	if flags.ShowHelp {
		_, _ = stdout.Write([]byte(usageText()))
		return nil, 0, true
	}
	if flags.ShowVersion {
		_, _ = stdout.Write([]byte("volclog " + version.Version + "\n"))
		return nil, 0, true
	}
	if group == "version" && len(rest) == 0 {
		_, _ = stdout.Write([]byte("volclog " + version.Version + "\n"))
		return nil, 0, true
	}

	if !isRecognizedGroup(group) {
		_, _ = stderr.Write([]byte(usageText()))
		return nil, 1, true
	}
	if !isGroupEnabledInCurrentEdition(group) {
		writeCLIError(stderr, errors.New("group not available in "+string(currentEdition())+" edition: "+group), "", 0, "usage", editionGroupHint(group))
		return nil, 1, true
	}

	format, err := output.ParseFormat(flags.Output)
	if err != nil {
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
			_, _ = stderr.Write([]byte(usageText()))
			return nil, 1, nil, true
		}
	}
	return out, exitCode, err, false
}
