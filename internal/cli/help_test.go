package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestSubcommandHelpFlagWorksAfterArgs(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		wantText string
	}{
		{name: "project list -h", args: []string{"project", "list", "-h"}, wantText: "volclog project"},
		{name: "configure set -h", args: []string{"configure", "set", "-h"}, wantText: "volclog configure"},
		{name: "doctor --online -h", args: []string{"doctor", "--online", "-h"}, wantText: "volclog doctor"},
		{name: "metric-topic prom query -h", args: []string{"metric-topic", "prom", "query", "--topic-id", "t", "--query", "up", "-h"}, wantText: "/topic/{topic_id}/api/v1/query"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(tc.args, &stdout, &stderr)
			if code != 0 {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if !strings.Contains(stdout.String(), tc.wantText) {
				t.Fatalf("missing %q in stdout: %q", tc.wantText, stdout.String())
			}
		})
	}
}

func TestAPIProjectUsesSwaggerSummaryActions(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"api", "project"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "CreateProject") {
		t.Fatalf("expected Swagger summary action in output: %q", out)
	}
	if strings.Contains(out, "create-project") {
		t.Fatalf("kebab-case action should not appear in output: %q", out)
	}
	if strings.Contains(out, "POST /CreateProject") {
		t.Fatalf("group action list should not expose method/path anymore: %q", out)
	}
}

func TestAPIDescribeAcceptsSwaggerSummaryAction(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"api", "project", "CreateProject", "--describe"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, `"action": "CreateProject"`) {
		t.Fatalf("unexpected describe output: %q", out)
	}
}

func TestAPIGroupHelpWorks(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"api", "project", "-h"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"volclog api project",
		"当前 group: project",
		"场景速选:",
		"模糊找项目",
		"volclog project list --describe",
		"CreateProject: 创建日志项目请求",
		"下一步命令:",
		"volclog api project <action> --describe",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in stdout: %q", want, out)
		}
	}
	for _, notWant := range []string{
		"执行前先使用:",
		"如有 body，再运行:",
	} {
		if strings.Contains(out, notWant) {
			t.Fatalf("group help should stay concise and hide %q: %q", notWant, out)
		}
	}
}

func TestUsageCapabilitiesUsesRunnableActionOnlyExample(t *testing.T) {
	text := usageCapabilities()
	if strings.Contains(text, "--action create") {
		t.Fatalf("deprecated ambiguous example should not appear: %q", text)
	}
	if !strings.Contains(text, "--action CreateProject") {
		t.Fatalf("expected runnable action-only example: %q", text)
	}
}

func TestUsageTextDescribesPrimaryEntryAsAgentNative(t *testing.T) {
	text := usageText()
	for _, want := range []string{
		"主入口（Agent / 自动化优先）",
		"用 capabilities 发现能力，用 api 查看约束并执行",
		"1) 发现能力: volclog capabilities --view groups",
		"默认全局参数写在 group 之前",
		"输出类全局参数也可后置",
		"大输出优先使用 --output-mode file",
		"作用于原始命令/API 结果",
		`zsh/bash 下建议写成 --jmes-filter "keys(@)"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in usage text: %q", want, text)
		}
	}
	for _, notWant := range []string{
		"优先服务智能体、CI/CD、运维脚本、RPA、服务端任务及各类非人工交互场景",
		"补充说明:",
	} {
		if strings.Contains(text, notWant) {
			t.Fatalf("unexpected verbose text %q in usage text: %q", notWant, text)
		}
	}
}

func TestUsageTextMentionsShortcutDescribeAndSkills(t *testing.T) {
	text := usageText()
	for _, want := range []string{
		"高频 shortcut 也支持 --describe 与 --print-request-template",
		"skills/ 与 skill-template/ 目录",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in usage text: %q", want, text)
		}
	}
}

func TestAPISearchLogsHelpPrefersFileOutputExample(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"api", "log", "SearchLogs", "-h"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "volclog --output-mode file api log SearchLogs --request file://req.json") {
		t.Fatalf("expected file output example in help: %q", out)
	}
	for _, want := range []string{
		"例如取 Total 写 Total，不写 data.Total",
		`zsh/bash: --jmes-filter "keys(@)"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in help: %q", want, out)
		}
	}
}

func TestManualGroupHelpRedirectsAgentsToCapabilitiesAndDescribe(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"project", "-h"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"High-frequency shortcut for both agents and humans.",
		"volclog project create --describe",
		"volclog project create --print-request-template=full",
		"Fall back to capabilities/api when the shortcut does not cover the need.",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in stdout: %q", want, out)
		}
	}
}

func TestUsageGroupHelpIncludesScenarioRouting(t *testing.T) {
	cases := []struct {
		text string
		want []string
	}{
		{
			text: usageProject(),
			want: []string{
				"场景速选:",
				"列项目/拿 ProjectId",
				"模糊找项目",
			},
		},
		{
			text: usageLog(),
			want: []string{
				"场景速选:",
				"普通日志检索",
				"写日志/WebTracking",
			},
		},
		{
			text: usageIndex(),
			want: []string{
				"场景速选:",
				"看当前索引",
				"不确定 body 怎么写",
			},
		},
	}
	for _, tc := range cases {
		for _, want := range tc.want {
			if !strings.Contains(tc.text, want) {
				t.Fatalf("missing %q in usage: %q", want, tc.text)
			}
		}
	}
}
