//go:build !human

package cli

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestDefaultUsageTextOmitsShortcutGuidance(t *testing.T) {
	text := usageText()
	for _, want := range []string{
		"\n  tool",
		"\n  workflow",
		"\n  raw",
		"当前 volclog 只暴露 configure/doctor/skill/upgrade/version/tool/workflow/raw/login/logout/sso",
		"[--output json|jsonl]",
		"--output-dir <path>",
		"--trace-redact <enabled>",
		"输出格式（tool/workflow/raw 默认用 json；需要面向机器的逐行结果时可用 jsonl）",
		"invalid --jmes-filter 属于 decode；执行成功后 filter matched no value 属于 output_failed，均返回 exit 3",
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
		"--trace-redact strict|default",
		"table 适用于常用快捷 list/get、index get、log search",
		"\n  project",
		"\n  topic",
		"\n  metric-topic",
		"\n  index",
		"\n  log ",
		"\n  host-group",
		"\n  collector",
	} {
		if contains(text, notWant) {
			t.Fatalf("agent usage should omit %q: %q", notWant, text)
		}
	}
}

func TestDefaultTableErrorDoesNotLeakHumanShortcutMatrix(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--output", "table", "tool", "list", "project"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("unexpected exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var env map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil || stderr.Len() != 0 || env["status"] != "failed" {
		t.Fatalf("expected failed JSON envelope: err=%v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}
	failure, ok := env["error"].(map[string]any)
	if !ok || failure["kind"] != "usage" {
		t.Fatalf("expected usage error: %#v", env)
	}
	errText := stdout.String()
	if !contains(errText, "default volclog agent path") {
		t.Fatalf("expected default-volclog hint, got %q", errText)
	}
	for _, notWant := range []string{
		"project/topic/metric-topic list|get",
		"index get",
		"log search",
	} {
		if contains(errText, notWant) {
			t.Fatalf("default volclog should not leak hidden table capability %q in %q", notWant, errText)
		}
	}
}
