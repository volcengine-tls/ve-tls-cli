package cli

import (
	"strings"
)

func shortcutCommandHelpLookup(group string) subcommandHelpLookup {
	return func(command string) (string, bool) {
		spec, ok := lookupShortcutSpec(group, command)
		if !ok || spec.HiddenInHelp {
			return "", false
		}
		return shortcutCommandUsage(spec), true
	}
}

func shortcutCommandUsage(spec shortcutCommandSpec) string {
	var b strings.Builder
	requiredParams := filterShortcutParams(spec.Params, true)
	optionalParams := filterShortcutParams(spec.Params, false)

	b.WriteString("Usage:\n")
	b.WriteString("  volclog " + spec.Group + " " + spec.Command + " [flags]\n\n")

	if strings.TrimSpace(spec.Summary) != "" {
		b.WriteString("Summary:\n")
		b.WriteString("  " + strings.TrimSpace(spec.Summary) + "\n")
		if strings.TrimSpace(spec.Description) != "" {
			b.WriteString("  " + strings.TrimSpace(spec.Description) + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("Interface:\n")
	b.WriteString("  " + strings.ToUpper(strings.TrimSpace(spec.Method)) + " " + strings.TrimSpace(spec.Path) + "\n")
	if strings.TrimSpace(spec.APIGroup) != "" && strings.TrimSpace(spec.APIAction) != "" {
		b.WriteString("  API describe: volclog api " + spec.APIGroup + " " + spec.APIAction + " --describe\n")
	}
	b.WriteString("\n")

	if shouldRenderRequiredSummary(spec, requiredParams) {
		b.WriteString("Required:\n")
		if len(spec.RequiredFlags) == 0 {
			b.WriteString("  (none)\n\n")
		} else {
			for _, item := range spec.RequiredFlags {
				b.WriteString("  - " + strings.TrimSpace(item) + "\n")
			}
			b.WriteString("\n")
		}
	}

	if len(requiredParams) > 0 {
		b.WriteString("Required Flags:\n")
		writeShortcutParams(&b, requiredParams)
		b.WriteString("\n")
	}
	if len(optionalParams) > 0 {
		b.WriteString("Optional Flags:\n")
		b.WriteString("  " + optionalFlagsIntro(spec) + "\n")
		writeShortcutParams(&b, optionalParams)
		b.WriteString("\n")
	}

	if strings.TrimSpace(spec.PreferredOutputMode) != "" || len(spec.RecommendedGlobalFlags) > 0 {
		b.WriteString("Output:\n")
		if strings.TrimSpace(spec.PreferredOutputMode) != "" {
			b.WriteString("  - Preferred output mode: " + strings.TrimSpace(spec.PreferredOutputMode) + "\n")
		}
		for _, item := range spec.RecommendedGlobalFlags {
			b.WriteString("  - " + strings.TrimSpace(item) + "\n")
		}
		b.WriteString("\n")
	}

	if len(spec.Notes) > 0 {
		b.WriteString("Notes:\n")
		for _, item := range spec.Notes {
			b.WriteString("  - " + strings.TrimSpace(item) + "\n")
		}
		b.WriteString("\n")
	}

	guidance := shortcutAgentGuidance(spec)
	if len(guidance) > 0 {
		b.WriteString("Agent Guidance:\n")
		for _, item := range guidance {
			b.WriteString("  " + item + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("Next:\n")
	b.WriteString("  volclog " + spec.Group + " " + spec.Command + " --describe\n")
	if spec.SupportsTemplate {
		b.WriteString("  volclog " + spec.Group + " " + spec.Command + " --print-request-template=full\n")
	}
	b.WriteString("  volclog capabilities --group " + spec.Group + " --view text\n")
	return b.String()
}

func shouldRenderRequiredSummary(spec shortcutCommandSpec, requiredParams []apiCapParam) bool {
	if len(spec.RequiredFlags) == 0 {
		return true
	}
	if len(requiredParams) == 0 {
		return true
	}
	requiredFlagSet := map[string]struct{}{}
	for _, param := range requiredParams {
		flag := primaryShortcutFlag(param.CLIFlag)
		if flag == "" {
			continue
		}
		requiredFlagSet[flag] = struct{}{}
	}
	if len(requiredFlagSet) == 0 {
		return true
	}
	for _, item := range spec.RequiredFlags {
		raw := strings.TrimSpace(item)
		flag := primaryShortcutFlag(raw)
		if flag == "" || flag != raw {
			return true
		}
		if _, ok := requiredFlagSet[flag]; !ok {
			return true
		}
	}
	return false
}

func optionalFlagsIntro(spec shortcutCommandSpec) string {
	switch normalizeToken(spec.Command) {
	case "list":
		return "这些都是可选项。不带参数就先按默认方式列结果；只有用户明确要筛选、翻页或列全时再加。"
	case "search", "histogram", "context", "export", "export-analysis":
		return "这些都是附加条件。先满足核心查询条件，再按需要补范围、条数、排序或输出相关参数。"
	case "create", "modify":
		return "这些都是补充项。只有用户明确要设置对应能力时再加；字段一多就切到模板，不要继续硬拼。"
	default:
		return "这些都是可选项。用户没明确提到就先别加，按当前命令默认行为执行。"
	}
}

func filterShortcutParams(params []apiCapParam, required bool) []apiCapParam {
	out := make([]apiCapParam, 0, len(params))
	for _, param := range params {
		if param.Required == required {
			out = append(out, param)
		}
	}
	return out
}

func writeShortcutParams(b *strings.Builder, params []apiCapParam) {
	for _, param := range params {
		line := strings.TrimSpace(param.CLIFlag)
		if line == "" {
			line = param.Name
		}
		if strings.TrimSpace(param.Type) != "" {
			line += " <" + strings.TrimSpace(param.Type) + ">"
		}
		if strings.TrimSpace(param.Description) != "" {
			line += "  " + strings.TrimSpace(param.Description)
		}
		b.WriteString("  - " + line + "\n")
	}
}

func primaryShortcutFlag(raw string) string {
	flag := strings.TrimSpace(raw)
	if flag == "" {
		return ""
	}
	if strings.Contains(flag, " ") || strings.Contains(flag, ",") {
		return ""
	}
	if strings.Contains(flag, "/") {
		flag = strings.TrimSpace(strings.Split(flag, "/")[0])
	}
	return flag
}

func shortcutAgentGuidance(spec shortcutCommandSpec) []string {
	base := "volclog " + spec.Group + " " + spec.Command
	switch normalizeToken(spec.Command) {
	case "list":
		return []string{
			"- 用户只是想随便列举资源时，先直接执行: " + base,
			"- 只有用户明确给出过滤条件时，再补可选 query 参数。",
			"- 不要因为可选 query 参数里出现了 --project-id/--topic-id/--topic-name，就把它们当成必填。",
		}
	case "get", "delete", "bind-rules", "unbind-rules", "delete-host", "bind-host-groups", "unbind-host-groups":
		if flag := firstRequiredShortcutFlag(spec.Params); flag != "" {
			return []string{
				"- 先确认资源 ID 是否已知；未知时先回到同组的 list/get 入口。",
				"- 当前命令的核心必填是: " + flag,
				"- 不要猜资源 ID。",
			}
		}
		return []string{
			"- 先确认资源 ID 是否已知；未知时先回到同组的 list/get 入口。",
			"- 不要猜资源 ID。",
		}
	case "search", "histogram", "context", "export", "export-analysis":
		return []string{
			"- 只把 Required 里的参数视为必填。",
			"- 用户没有给足查询条件时，不要猜 topic/time/query；先回到 --describe 或补齐条件。",
			"- Optional Flags 只在用户明确提出额外约束时再补。",
		}
	case "put", "ingest":
		return []string{
			"- 先确认用户是想精确构造写入体，还是让 CLI 代为组装批次。",
			"- 只把 Required 里的参数视为必填；不要从可选 header 或输入格式推断必填。",
		}
	case "create", "modify":
		return []string{
			"- 字段不多时可以直接按 flags 组织；字段多或嵌套深时再看 --print-request-template=full。",
			"- 不要把某个示例里的可选字段当成固定必填。",
		}
	default:
		return []string{
			"- 只把 Required 里的内容视为必填。",
			"- 不确定时先看 --describe，而不是从历史示例里猜参数。",
		}
	}
}

func firstRequiredShortcutFlag(params []apiCapParam) string {
	for _, param := range params {
		if !param.Required || strings.TrimSpace(param.CLIFlag) == "" {
			continue
		}
		flag := primaryShortcutFlag(param.CLIFlag)
		return flag
	}
	return ""
}
