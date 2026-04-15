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
		"按项目名过滤",
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
				"按项目名过滤",
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

func TestShortcutGroupUsageOmitsExamples(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{name: "project", text: usageProject()},
		{name: "topic", text: usageTopic()},
		{name: "metric-topic", text: usageMetricTopic()},
		{name: "index", text: usageIndex()},
		{name: "log", text: usageLog()},
		{name: "host-group", text: usageHostGroup()},
		{name: "collector", text: usageCollector()},
	}
	for _, tc := range cases {
		if strings.Contains(tc.text, "Examples:") {
			t.Fatalf("%s usage should not include Examples anymore: %q", tc.name, tc.text)
		}
	}
}

func TestShortcutSubcommandHelpIsCommandScopedForTopicList(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"topic", "list", "-h"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"Usage:",
		"volclog topic list",
		"列出日志主题",
		"Required:",
		"(none)",
		"--page-size",
		"--all",
		"Agent Guidance:",
		"volclog topic list",
		"不要因为可选 query 参数里出现了 --project-id",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in stdout: %q", want, out)
		}
	}
	for _, notWant := range []string{
		"volclog topic create --describe",
		"volclog topic modify --topic-id",
		"volclog topic delete --topic-id",
		"volclog topic get --topic-id",
		"Commands:",
		"Examples:",
	} {
		if strings.Contains(out, notWant) {
			t.Fatalf("unexpected %q in stdout: %q", notWant, out)
		}
	}
}

func TestShortcutSubcommandHelpExplainsOptionalFlagUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"topic", "list", "-h"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"Optional Flags:",
		"这些都是可选项。不带参数就先按默认方式列结果；只有用户明确要筛选、翻页或列全时再加。",
		"--project-id",
		"--all",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in stdout: %q", want, out)
		}
	}
}

func TestShortcutSubcommandHelpAvoidsDuplicateRequiredSummary(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"topic", "get", "-h"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	if strings.Contains(out, "Required:\n  - --topic-id\n\nRequired Flags:") {
		t.Fatalf("single required flag should not be repeated in both Required and Required Flags: %q", out)
	}
	for _, want := range []string{
		"Required Flags:",
		"--topic-id <string>  主题 ID",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in stdout: %q", want, out)
		}
	}
}

func TestShortcutSubcommandHelpIsCommandScopedForLogSearch(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"log", "search", "-h"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"volclog log search",
		"执行日志检索",
		"--topic-id",
		"--query",
		"--from",
		"--to",
		"--limit",
		"Agent Guidance:",
		"只把 Required 里的参数视为必填",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in stdout: %q", want, out)
		}
	}
	for _, notWant := range []string{
		"volclog log put --describe",
		"volclog log export-analysis",
		"Commands:",
		"批量导入文本或 JSON 日志",
		"Examples:",
	} {
		if strings.Contains(out, notWant) {
			t.Fatalf("unexpected %q in stdout: %q", notWant, out)
		}
	}
}

func TestAPIGeneratedHelpSeparatesOptionalQueryPathParams(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"api", "topic", "DescribeTopics", "-h"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"必填 query/path 参数:",
		"(none)",
		"可选 query/path 参数:",
		"这些都是筛选或翻页项。不带参数就先按默认方式请求；需要缩小范围、分页或列全时再加。",
		"--project-id <value> - 日志项目ID",
		"输出过滤与引号:",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in stdout: %q", want, out)
		}
	}
	for _, notWant := range []string{
		"--cursor <value>",
		"--region <value>",
		"--fuzzy-search-key <value>",
		"--favourite <value>",
		"--order-by-project <value>",
		"--is-full-name <value>",
		"--description <value>",
	} {
		if strings.Contains(out, notWant) {
			t.Fatalf("api help should hide undocumented param %q: %q", notWant, out)
		}
	}
}

func TestAPIGeneratedHelpAvoidsDuplicateRequiredFlagSummary(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"api", "topic", "DescribeTopic", "-h"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	if strings.Contains(out, "调用输入:\n  query/path 参数通过 flags 传入\n  必填 flags: TopicId") {
		t.Fatalf("api help should not repeat required flags summary above the detailed required param table: %q", out)
	}
	for _, want := range []string{
		"调用输入:",
		"query/path 参数通过 flags 传入",
		"必填 query/path 参数:",
		"--topic-id <value> - 日志主题ID（UUID）。",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in stdout: %q", want, out)
		}
	}
}

func TestAPIGeneratedHelpOfficialNoParamTableOmitsSwaggerFallback(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"api", "log", "DescribeCursorTime", "-h"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"官网有接口页面，但当前未解析到结构化参数表",
		"必填 query/path 参数:",
		"(none)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in stdout: %q", want, out)
		}
	}
	for _, notWant := range []string{
		"--cursor <value>",
		"请求体: 通过 --request 传入（必填）",
		"--print-request-template=full",
	} {
		if strings.Contains(out, notWant) {
			t.Fatalf("official no-table help should not fall back to swagger detail %q: %q", notWant, out)
		}
	}
}

func TestPublicV1DoesNotExposeAssistantOrMetricTopicProm(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{name: "assistant group", args: []string{"assistant", "describe-session-answer"}},
		{name: "metric-topic prom", args: []string{"metric-topic", "prom", "query", "--topic-id", "t", "--query", "up"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(tc.args, &stdout, &stderr)
			if code == 0 {
				t.Fatalf("expected non-zero exit for hidden public entry, stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}
