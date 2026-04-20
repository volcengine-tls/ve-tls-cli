//go:build !human

package cli

import "testing"

func TestDefaultUsageTextOmitsShortcutGuidance(t *testing.T) {
	text := usageText()
	for _, want := range []string{
		"\n  tool",
		"\n  workflow",
		"\n  raw",
		"当前 volclog 只暴露 configure/doctor/skill/tool/workflow/raw",
		"[--output json|jsonl]",
		"输出格式（tool/workflow/raw 默认用 json；需要面向机器的逐行结果时可用 jsonl）",
	} {
		if !contains(text, want) {
			t.Fatalf("agent usage missing %q: %q", want, text)
		}
	}
	for _, notWant := range []string{
		"project/topic/index/log 等 shortcut",
		"次级入口（仅在你已明确目标资源时使用）:",
		"--output-file",
		"[--output json|jsonl|table]",
		"--output <json|jsonl|table>",
		"table 适用于常用快捷 list/get、index get、log search",
		"\n  project",
		"\n  topic",
		"\n  metric-topic",
		"\n  index",
		"\n  log",
		"\n  host-group",
		"\n  collector",
	} {
		if contains(text, notWant) {
			t.Fatalf("agent usage should omit %q: %q", notWant, text)
		}
	}
}
