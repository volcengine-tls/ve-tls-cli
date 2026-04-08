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

func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = stderr.Write([]byte(usageText()))
		return 1
	}

	g, rest, gf, ok := parseGlobal(args)
	if !ok {
		_, _ = stderr.Write([]byte(usageText()))
		return 1
	}
	if allowsTrailingGlobalsForGroup(g) {
		rest, gf, ok = extractTrailingGlobals(rest, gf, g == "api")
		if !ok {
			_, _ = stderr.Write([]byte(usageText()))
			return 1
		}
	}

	if strings.TrimSpace(gf.SecretsFile) != "" {
		if err := loadSecretsFile(gf.SecretsFile); err != nil {
			writeCLIError(stderr, err, "", 0, "config", "failed to load --secrets-file")
			return 2
		}
	}
	wd, err := os.Getwd()
	if err != nil {
		writeCLIError(stderr, err, "", 0, "config", "failed to get working directory")
		return 2
	}
	projectCfg, _, err := config.LoadProjectConfig(wd)
	if err != nil {
		writeCLIError(stderr, err, "", 0, "config", "failed to load project config")
		return 2
	}
	outputExplicit := strings.TrimSpace(gf.Output) != ""
	if strings.TrimSpace(gf.Output) == "" && strings.TrimSpace(projectCfg.Output) != "" {
		gf.Output = projectCfg.Output
	}
	if strings.TrimSpace(gf.OutputMode) == "" && strings.TrimSpace(projectCfg.OutputMode) != "" {
		gf.OutputMode = projectCfg.OutputMode
	}
	// Resolve default output directory precedence:
	// VOLCLOG_OUTPUT_DIR env > project config output_dir > default (.volclog/output)
	defaultOutDir := strings.TrimSpace(os.Getenv("VOLCLOG_OUTPUT_DIR"))
	if defaultOutDir == "" {
		defaultOutDir = strings.TrimSpace(projectCfg.OutputDir)
	}
	if strings.TrimSpace(gf.TraceRedact) == "" && strings.TrimSpace(projectCfg.TraceRedact) != "" {
		gf.TraceRedact = projectCfg.TraceRedact
	}
	if g == "capabilities" && !hasFlagWithValue(rest, "--hints-file") {
		hintsFile := strings.TrimSpace(os.Getenv("VOLCLOG_HINTS_FILE"))
		if hintsFile == "" {
			hintsFile = strings.TrimSpace(projectCfg.HintsFile)
		}
		if hintsFile != "" {
			rest = append(rest, "--hints-file", hintsFile)
		}
	}
	if g == "log" && len(rest) > 0 && rest[0] == "export-analysis" && !outputExplicit {
		gf.Output = "jsonl"
	}

	if gf.ShowHelp {
		_, _ = stdout.Write([]byte(usageText()))
		return 0
	}
	if gf.ShowVersion {
		_, _ = stdout.Write([]byte("volclog " + version.Version + "\n"))
		return 0
	}

	format, err := output.ParseFormat(gf.Output)
	if err != nil {
		writeCLIError(stderr, err, "", 0, "usage", "invalid --output")
		return 1
	}

	outputMode := strings.ToLower(strings.TrimSpace(gf.OutputMode))
	if outputMode == "" {
		outputMode = "stdout"
	}

	ctx := newContext(stdout, stderr, format, gf.Profile, gf.Filter)
	ctx.OutputMode = outputMode
	ctx.OutputDir = defaultOutDir
	ctx.TraceDir = gf.TraceDir
	ctx.TraceRedact = gf.TraceRedact
	ctx.DryRun = gf.DryRun
	ctx.SetProfileDefaults(config.ProfileDefaults{
		Region:         projectCfg.Region,
		Endpoint:       projectCfg.Endpoint,
		TimeoutSeconds: projectCfg.TimeoutSeconds,
	})
	defer ctx.Close()

	if outputMode != "stdout" && outputMode != "file" {
		writeCLIError(stderr, errors.New("unsupported output-mode: "+gf.OutputMode), "", 0, "usage", "invalid --output-mode")
		return 1
	}
	if err := ctx.validateDryRunScope(g); err != nil {
		writeCLIError(stderr, err, "", 0, "usage", "invalid --dry-run scope")
		return 1
	}

	var out any
	exitCode := 0
	switch g {
	case "configure":
		out, err = runConfigure(ctx, rest)
	case "skill":
		out, err = runSkill(ctx, rest)
	case "capabilities":
		out, err = runCapabilities(ctx, rest)
	case "api":
		out, err = runAPI(ctx, rest)
	case "project":
		out, err = runProject(ctx, rest)
	case "topic":
		out, err = runTopic(ctx, rest)
	case "metric-topic":
		out, err = runMetricTopic(ctx, rest)
	case "index":
		out, err = runIndex(ctx, rest)
	case "log":
		out, err = runLog(ctx, rest)
	case "host-group":
		out, err = runHostGroup(ctx, rest)
	case "collector":
		out, err = runCollector(ctx, rest)
	case "assistant":
		out, err = runAssistant(ctx, rest)
	case "doctor":
		out, exitCode, err = runDoctor(ctx, rest)
	default:
		_, _ = stderr.Write([]byte(usageText()))
		return 1
	}
	if err != nil {
		if ue, ok := asUsageError(err); ok {
			_, _ = stdout.Write([]byte(ue.Text))
			return ue.ExitCode
		}
		payload, code := classifyError(err, ctx.RequestID, ctx.StatusCode, g)
		if isEnvelopeGroup(g) {
			if strings.TrimSpace(ctx.TraceDir) != "" {
				_ = ctx.initTrace()
			}
			env := buildAPIErrorEnvelope(ctx, g, err, outputMode)
			if err2 := output.Write(stdout, env, output.FormatJSON); err2 != nil {
				writeCLIError(stderr, err2, payload.RequestID, payload.StatusCode, "decode", "output write failed")
				return 3
			}
			return code
		}
		writeCLIError(stderr, err, payload.RequestID, payload.StatusCode, payload.Kind, payload.Hint)
		return code
	}
	if ctx.Filter != "" {
		out, err = output.ApplyFilter(out, ctx.Filter)
		if err != nil {
			if isEnvelopeGroup(g) {
				if strings.TrimSpace(ctx.TraceDir) != "" {
					_ = ctx.initTrace()
				}
				env := buildAPIErrorEnvelope(ctx, g, err, outputMode)
				if err2 := output.Write(stdout, env, output.FormatJSON); err2 != nil {
					writeCLIError(stderr, err2, ctx.RequestID, ctx.StatusCode, "decode", "output write failed")
					return 3
				}
				return 3
			}
			writeCLIError(stderr, err, ctx.RequestID, ctx.StatusCode, "decode", "invalid --jmes-filter")
			return 3
		}
	}
	if format == output.FormatTable {
		if !supportsTableOutput(ctx) {
			writeCLIError(stderr, errors.New("unsupported output: table"), ctx.RequestID, ctx.StatusCode, "usage", "table is only supported for project/topic/metric-topic list|get, index get, and log search")
			return 1
		}
		if outputMode == "file" {
			p, err := writeOutputFileToDir(gf.OutputFile, ctx.OutputDir, g, out, format)
			if err != nil {
				writeCLIError(stderr, err, ctx.RequestID, ctx.StatusCode, "decode", "output write failed")
				return 3
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
			p, err := writeTextFileToDir(gf.OutputFile, ctx.OutputDir, g, s)
			if err != nil {
				writeCLIError(stderr, err, ctx.RequestID, ctx.StatusCode, "decode", "output write failed")
				return 3
			}
			_, _ = stdout.Write([]byte(p + "\n"))
			return exitCode
		}
		_, _ = stdout.Write([]byte(s))
		return exitCode
	}
	if strings.TrimSpace(ctx.TraceDir) != "" {
		_ = ctx.initTrace()
		if strings.TrimSpace(ctx.TracePath) != "" && !isAPIEnvelopeCandidate(g, out) {
			out = attachMeta(out, ctx.TracePath)
		}
	}
	if isAPIEnvelopeCandidate(g, out) {
		env, err := buildAPIEnvelope(ctx, g, out, outputMode, gf.OutputFile, format)
		if err != nil {
			writeCLIError(stderr, err, ctx.RequestID, ctx.StatusCode, "decode", "output write failed")
			return 3
		}
		if err := output.Write(stdout, env, output.FormatJSON); err != nil {
			writeCLIError(stderr, err, ctx.RequestID, ctx.StatusCode, "decode", "output write failed")
			return 3
		}
		return exitCode
	}
	if outputMode == "file" {
		p, err := writeOutputFileToDir(gf.OutputFile, ctx.OutputDir, g, out, format)
		if err != nil {
			writeCLIError(stderr, err, ctx.RequestID, ctx.StatusCode, "decode", "output write failed")
			return 3
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

func usageText() string {
	var b strings.Builder
	b.WriteString("Usage:\n")
	b.WriteString("  volclog [--profile <name>] [--output json|jsonl|table] [--output-mode stdout|file] [--output-file <path>] [--jmes-filter <expr>] [--trace-dir <path>] [--trace-redact strict|default] [--secrets-file <path>] [--dry-run] <group> <command> [args]\n\n")
	b.WriteString("主入口（Agent / 自动化优先）:\n")
	b.WriteString("  用 capabilities 发现能力，用 api 查看约束并执行。\n")
	b.WriteString("  适合智能体、CI/CD、脚本和其他非人工交互场景。\n")
	b.WriteString("  默认全局参数写在 group 之前；但 api 与 project/topic/metric-topic/index/log/host-group/collector 的输出类全局参数也可后置。\n")
	b.WriteString("  大输出优先使用 --output-mode file。\n")
	b.WriteString("  --jmes-filter 作用于原始命令/API 结果，而不是 CLI envelope；筛选 Total 时写 Total，不写 data.Total。\n")
	b.WriteString("  zsh/bash 下建议写成 --jmes-filter \"keys(@)\"；fish/PowerShell 下优先用单引号。\n\n")
	for _, group := range cliGroups() {
		if !group.Primary {
			continue
		}
		b.WriteString("  ")
		b.WriteString(group.Name)
		if len(group.Name) < 12 {
			b.WriteString(strings.Repeat(" ", 12-len(group.Name)))
		} else {
			b.WriteString(" ")
		}
		b.WriteString(group.Description)
		b.WriteString("\n")
	}
	b.WriteString(`

推荐流程:
  1) 发现能力: volclog capabilities --view groups
  2) 缩小范围: volclog capabilities --group <group> --view text
  3) 查看约束: volclog api <group> <action> --describe
  4) 请求校验: volclog --dry-run api <group> <action> --request file://req.json
  5) 正式执行: volclog api <group> <action> --request file://req.json
  6) 高频 shortcut 也支持 --describe 与 --print-request-template，可先用在 project/topic/index/log 等入口上
  7) 安装内置 Agent 技能: volclog skill install --dir <agent-skills-dir>
  8) 仓库内提供 skills/ 与 skill-template/ 目录，可直接为 Agent 安装或扩展技能

次级入口（仅在你已明确目标资源时使用）:
`)
	for _, group := range cliGroups() {
		if group.Primary {
			continue
		}
		b.WriteString("  ")
		b.WriteString(group.Name)
		if len(group.Name) < 12 {
			b.WriteString(strings.Repeat(" ", 12-len(group.Name)))
		} else {
			b.WriteString(" ")
		}
		b.WriteString(group.Description)
		b.WriteString("\n")
	}
	b.WriteString("\n全局参数:\n")
	maxUsageLen := 0
	for _, flag := range cliGlobalFlagSpecs() {
		if flag.Name == "-h" {
			continue
		}
		if len(flag.Usage) > maxUsageLen {
			maxUsageLen = len(flag.Usage)
		}
	}
	for _, flag := range cliGlobalFlagSpecs() {
		if flag.Name == "-h" {
			continue
		}
		b.WriteString("  ")
		b.WriteString(flag.Usage)
		if flag.Description != "" {
			if len(flag.Usage) < maxUsageLen {
				b.WriteString(strings.Repeat(" ", maxUsageLen-len(flag.Usage)))
			}
			b.WriteString("  ")
			b.WriteString(flag.Description)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func hasFlagWithValue(args []string, flag string) bool {
	for i := 0; i < len(args); i++ {
		if args[i] == flag {
			return true
		}
	}
	return false
}

func init() {
	_ = os.Setenv("GODEBUG", os.Getenv("GODEBUG"))
}
