package cli

import (
	"errors"
	"io"
	"os"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/output"
)

// Run is the production entry point. It always assembles the real console login
// adapter (FileCache, LoginService, local+remote authorizer, confirm prompt) and
// the real SSO adapter. Tests that need to inject fakes call
// runWithLoginAdapterFactory directly.
func Run(args []string, stdout, stderr io.Writer) int {
	return runWithLoginAdapterFactory(args, stdout, stderr, newProductionLoginAdapter, newProductionSSOAdapter)
}

// runWithLoginAdapterFactory contains the full Run pipeline. The factory
// parameters are only consulted for the login/logout/sso commands; all other
// commands are unaffected. Production always passes the real factories; tests
// pass fake factories for a single invocation so no process-level mutable state
// is shared.
func runWithLoginAdapterFactory(args []string, stdout, stderr io.Writer, factory loginAdapterFactory, ssoFactory ssoAdapterFactory) int {
	invocation, exitCode, done := prepareRunInvocation(args, stdout, stderr)
	if done {
		return exitCode
	}
	defer invocation.ctx.Close()

	if exitCode, done = preflightRunInvocation(invocation, stdout, stderr); done {
		return exitCode
	}
	out, dispatchExitCode, err, done := dispatchRunInvocation(invocation, stdout, stderr, factory, ssoFactory)
	if done {
		return dispatchExitCode
	}
	return renderRunResult(invocation, stdout, stderr, out, dispatchExitCode, err)
}

func knownFileDeliveryFormat(group string, rest []string, format output.Format) output.Format {
	if emitsStructuredEnvelope(group, rest) {
		return output.FormatJSON
	}
	return format
}

func rejectsOutputFileForGroup(group string) bool {
	switch strings.TrimSpace(group) {
	case "tool", "workflow", "raw":
		return true
	default:
		return false
	}
}

func filterTargetsEnvelope(group string, rest []string) bool {
	switch strings.TrimSpace(group) {
	case "raw":
		return true
	case "tool", "workflow":
		return len(rest) > 0 && strings.TrimSpace(rest[0]) == "exec"
	default:
		return false
	}
}

func emitsStructuredEnvelope(group string, rest []string) bool {
	if isEnvelopeGroup(group) {
		return true
	}
	switch strings.TrimSpace(group) {
	case "tool", "workflow":
		return true
	default:
		return false
	}
}

func writeRunError(ctx *Context, stdout, stderr io.Writer, group string, rest []string, err error, kind string, hint string, outputMode string, code int) int {
	if emitsStructuredEnvelope(group, rest) {
		return writeStructuredError(stdout, stderr, err, "", 0, group, buildAPIErrorEnvelope(ctx, group, err, outputMode))
	}
	writeCLIError(stderr, err, "", 0, kind, hint)
	return code
}

func writeFilterApplicationError(ctx *Context, stdout, stderr io.Writer, group string, rest []string, target any, outputMode string, err error) int {
	if emitsStructuredEnvelope(group, rest) || hasStructuredEnvelopeOutput(target) {
		if strings.TrimSpace(ctx.TraceDir) != "" {
			_ = ctx.initTrace()
		}
		errorEnv := buildAPIErrorEnvelope(ctx, group, err, outputMode)
		if err2 := output.Write(stdout, errorEnv, output.FormatJSON); err2 != nil {
			writeCLIError(stderr, err2, ctx.RequestID, ctx.StatusCode, "decode", "output write failed")
			return 3
		}
		return 3
	}
	writeCLIError(stderr, err, ctx.RequestID, ctx.StatusCode, "decode", "invalid --jmes-filter")
	return 3
}

func structuredEnvelopeOutput(v any) (map[string]any, bool) {
	env, ok := v.(map[string]any)
	if !ok {
		return nil, false
	}
	status, _ := env["status"].(string)
	if strings.TrimSpace(status) == "" {
		return nil, false
	}
	if _, ok := env["summary"].(map[string]any); !ok {
		return nil, false
	}
	if _, ok := env["artifacts"]; !ok {
		return nil, false
	}
	if _, ok := env["data"]; !ok {
		return nil, false
	}
	if _, ok := env["error"]; !ok {
		return nil, false
	}
	return env, true
}

func hasStructuredEnvelopeOutput(v any) bool {
	_, ok := structuredEnvelopeOutput(v)
	return ok
}

func applyEnvelopeFilterResult(v any, expr string) (any, error) {
	return output.ApplyFilter(v, expr)
}

func fileDeliveryNoticeForOutput(v any, group string, rest []string) (string, bool) {
	if !usesFixedFileNotice(group, rest) {
		return "", false
	}
	env, ok := v.(map[string]any)
	if !ok {
		return "", false
	}
	summary, _ := env["summary"].(map[string]any)
	if summary == nil {
		return "", false
	}
	deliveryMode, _ := summary["deliveryMode"].(string)
	path, ok := firstArtifactPath(env["artifacts"])
	if !ok {
		return "", false
	}
	switch strings.TrimSpace(deliveryMode) {
	case "file_auto":
		return "结果过大，已写入文件。\n文件: " + path + "\n", true
	case "file_forced":
		return "结果已写入文件。\n文件: " + path + "\n", true
	default:
		return "", false
	}
}

func firstArtifactPath(v any) (string, bool) {
	switch vv := v.(type) {
	case []map[string]any:
		if len(vv) == 0 {
			return "", false
		}
		path, _ := vv[0]["path"].(string)
		return strings.TrimSpace(path), strings.TrimSpace(path) != ""
	case []any:
		if len(vv) == 0 {
			return "", false
		}
		artifact, _ := vv[0].(map[string]any)
		path, _ := artifact["path"].(string)
		return strings.TrimSpace(path), strings.TrimSpace(path) != ""
	default:
		return "", false
	}
}

func usesFixedFileNotice(group string, rest []string) bool {
	switch strings.TrimSpace(group) {
	case "raw":
		return true
	case "tool", "workflow":
		return len(rest) > 0 && strings.TrimSpace(rest[0]) == "exec"
	default:
		return false
	}
}

func defersSecretsResolutionToCommand(group string, rest []string) bool {
	switch strings.TrimSpace(group) {
	case "tool", "workflow":
		return len(rest) > 0 && strings.TrimSpace(rest[0]) == "exec"
	default:
		return false
	}
}

// preflightGlobalSecretsFile validates the global --secrets-file in the order
// required by Task 5: runtime selector conflicts (--profile vs --secrets-file)
// are checked first, then groups that manage their own dynamic identity
// (login/logout/sso) are rejected. Checking conflicts first ensures users see
// the actionable "use exactly one selector" message before the group-specific
// rejection, even when both --profile and --secrets-file are passed to a
// login/logout/sso command.
func preflightGlobalSecretsFile(group, profile, secretsFile string) error {
	if _, err := resolveRuntimeSelectors(runtimeSelectorSet{
		GlobalProfile:     profile,
		GlobalSecretsFile: secretsFile,
	}); err != nil {
		return err
	}
	if rejectsSecretsFileForGroup(group) {
		return errors.New("--secrets-file is not supported for " + strings.TrimSpace(group) + "; use --profile to select a dynamic login identity")
	}
	return nil
}

// rejectsSecretsFileForGroup reports whether the group must never accept
// --secrets-file. Interactive login commands (login/logout/sso) manage their
// own dynamic identity and must not be handed long-lived static credentials.
// configure sso is handled separately by isInteractiveAuthCommand because it
// lives under the configure group.
func rejectsSecretsFileForGroup(group string) bool {
	switch strings.TrimSpace(group) {
	case "login", "logout", "sso":
		return true
	default:
		return false
	}
}

// isInteractiveAuthCommand reports whether the invoked command is one that
// manages its own dynamic identity and freezes stdout to a fixed JSON shape:
// login, logout, sso (any subcommand), or configure sso. These commands must
// reject frozen output/secrets flags before the adapter factory is built so no
// side effect runs when stdout would be diverted.
func isInteractiveAuthCommand(group string, rest []string) bool {
	switch strings.TrimSpace(group) {
	case "login", "logout", "sso":
		return true
	case "configure":
		return len(rest) > 0 && strings.TrimSpace(rest[0]) == "sso"
	default:
		return false
	}
}

func appendEnvelopeWarning(env map[string]any, warning map[string]any) {
	if env == nil || len(warning) == 0 {
		return
	}
	switch warnings := env["warnings"].(type) {
	case []map[string]any:
		env["warnings"] = append(warnings, warning)
	case []any:
		env["warnings"] = append(warnings, warning)
	case nil:
		env["warnings"] = []map[string]any{warning}
	default:
		env["warnings"] = []any{warnings, warning}
	}
}

func usageText() string {
	var b strings.Builder
	b.WriteString("Usage:\n")
	if currentEdition() == cliEditionVolclog {
		b.WriteString("  volclog [--profile <name>] [--region <region>] [--endpoint <url>] [--output json|jsonl] [--output-mode stdout|file] [--output-dir <path>] [--jmes-filter <expr>] [--trace-dir <path>] [--trace-redact <enabled>] [--secrets-file <path>] [--dry-run] <group> <command> [args]\n\n")
	} else {
		b.WriteString("  volclog [--profile <name>] [--region <region>] [--endpoint <url>] [--output json|jsonl|table] [--output-mode stdout|file] [--output-dir <path>] [--jmes-filter <expr>] [--trace-dir <path>] [--trace-redact <enabled>] [--secrets-file <path>] [--dry-run] <group> <command> [args]\n\n")
	}
	b.WriteString("主入口（Agent / 自动化优先）:\n")
	if currentEdition() == cliEditionVolclog {
		b.WriteString("  用 tool 发现公开 API 并查看契约；需要结构化执行时使用 tool exec。\n")
		b.WriteString("  用 workflow 执行 CLI 提供的高层编排能力；只有已明确 method/path 时才使用 raw。\n")
		b.WriteString("  tool/workflow 走 JSON input/context；raw 走 method/path 与 transport flags。\n")
		b.WriteString("  大结果优先使用 --output-mode file --output-dir <writable-dir>；--jmes-filter 命中 null 仍成功，缺字段或数组越界会报 filter matched no value。\n")
		b.WriteString("  filter matched no value / invalid --jmes-filter 属于 decode，返回 exit 3。\n\n")
	} else {
		b.WriteString("  用 tool 发现能力并查看契约；需要结构化执行时使用 tool exec。\n")
		b.WriteString("  已明确 method/path 的原始 transport 调用使用 raw。\n")
		b.WriteString("  默认全局参数写在 group 之前；但 raw 与 project/topic/metric-topic/index/log/host-group/collector 的输出类全局参数也可后置。\n")
		b.WriteString("  大输出优先使用 --output-mode file --output-dir <writable-dir>。\n")
		b.WriteString("  --jmes-filter 作用于完整 envelope；筛选结果字段时写 data.Total，筛选交付语义时写 summary.deliveryMode。\n")
		b.WriteString("  命中存在但值为 null 的字段会输出 null；缺字段或数组越界会报 filter matched no value。\n")
		b.WriteString("  filter matched no value / invalid --jmes-filter 属于 decode，返回 exit 3。\n")
		b.WriteString("  zsh/bash 下建议写成 --jmes-filter \"keys(@)\"；fish/PowerShell 下优先用单引号。\n\n")
	}
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
	b.WriteString("\n鉴权速选:\n")
	b.WriteString("  个人终端 Console Login: volclog login --help\n")
	b.WriteString("  企业 SSO 首次配置:      volclog configure sso-session --help\n")
	b.WriteString("  静态 AK/SK 或工作负载:  volclog configure set --help\n")
	b.WriteString("  配置完成后验证:         volclog --profile <name> doctor\n")
	if currentEdition() == cliEditionVolclog {
		b.WriteString("\n  可用 volclog skill install --dir <skills-dir> 安装内置 volclog 技能。\n")
		b.WriteString("  当前 volclog 只暴露 configure/doctor/skill/tool/workflow/raw/login/logout/sso；human shortcut 需切到 volclog-human（-tags=human）。\n\n")
	} else {
		b.WriteString("\n  project/topic/index/log 等 shortcut 仅供人工交互；默认 volclog 不把它们当主流程，也不要默认停在 shortcut 的 --describe / --print-request-template。\n")
		b.WriteString("  可用 volclog skill install --dir <skills-dir> 安装内置 volclog 技能。\n")
		b.WriteString("  仓库内提供可直接安装的 skills/ 目录。\n\n")
		b.WriteString("次级入口（仅在你已明确目标资源时使用）:\n")
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
		b.WriteString("\n")
	}
	b.WriteString("全局参数:\n")
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
	b.WriteString("\n补充:\n")
	b.WriteString("  --output-dir <path> 用于 file / file_auto 的落盘目录；建议与 --output-mode file 一起提供。\n")
	return b.String()
}

func legacyCapabilitiesHint(args []string) string {
	group := ""
	action := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--group":
			if i+1 < len(args) {
				group = strings.TrimSpace(args[i+1])
				i++
			}
		case "--action":
			if i+1 < len(args) {
				action = strings.TrimSpace(args[i+1])
				i++
			}
		}
	}
	if hint := resolvedToolMigrationHint(group, action); hint != "" {
		return hint
	}
	if strings.TrimSpace(group) != "" {
		return "use 'volclog tool list " + strings.TrimSpace(group) + "' for discovery"
	}
	return "use 'volclog tool list' for discovery"
}

func legacyAPIHint(args []string) string {
	if len(args) == 0 {
		return "use 'volclog tool list' for discovery or 'volclog raw --method <METHOD> --path <PATH>' for transport-level calls"
	}
	if strings.TrimSpace(args[0]) == "call" {
		return "use 'volclog raw --method <METHOD> --path <PATH>' for transport-level calls"
	}
	group := strings.TrimSpace(args[0])
	action := ""
	if len(args) > 1 && !strings.HasPrefix(strings.TrimSpace(args[1]), "-") {
		action = strings.TrimSpace(args[1])
	}
	if hint := resolvedToolMigrationHint(group, action); hint != "" {
		return hint
	}
	if group != "" {
		return "use 'volclog tool list " + group + "' for discovery or 'volclog raw --method <METHOD> --path <PATH>' if you already know the transport path"
	}
	return "use 'volclog tool list' for discovery or 'volclog raw --method <METHOD> --path <PATH>' for transport-level calls"
}

func resolvedToolMigrationHint(group, action string) string {
	group = strings.TrimSpace(group)
	action = strings.TrimSpace(action)
	if group == "" {
		return ""
	}
	if action == "" {
		return "use 'volclog tool list " + group + "' for discovery or 'volclog raw --method <METHOD> --path <PATH>' if you already know the transport path"
	}
	tool, err := resolveToolByIdentity(group, action)
	if err != nil {
		return "use 'volclog tool list " + group + "' for discovery"
	}
	return "use 'volclog tool list " + group + "' or 'volclog tool describe " + strings.TrimSpace(string(tool.ID)) + "'"
}

func init() {
	_ = os.Setenv("GODEBUG", os.Getenv("GODEBUG"))
}
