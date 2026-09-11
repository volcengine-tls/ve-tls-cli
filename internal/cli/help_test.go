//go:build human

package cli

import (
	"bytes"
	"encoding/json"
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

func TestUsageTextDescribesPrimaryEntryAsAgentNative(t *testing.T) {
	text := usageText()
	for _, want := range []string{
		"主入口（Agent / 自动化优先）",
		"用 tool 发现能力",
		"需要结构化执行时使用 tool exec",
		"原始 transport 调用使用 raw",
		"默认全局参数写在 group 之前",
		"输出类全局参数也可后置",
		"大输出优先使用 --output-mode file --output-dir <writable-dir>",
		"作用于完整 envelope",
		"--trace-redact <enabled>",
		`zsh/bash 下建议写成 --jmes-filter "keys(@)"`,
		"命中存在但值为 null 的字段会输出 null；缺字段或数组越界会报 filter matched no value",
		"filter matched no value / invalid --jmes-filter 属于 decode，返回 exit 3",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in usage text: %q", want, text)
		}
	}
	for _, notWant := range []string{
		"优先服务智能体、CI/CD、运维脚本、RPA、服务端任务及各类非人工交互场景",
		"补充说明:",
		"推荐流程:",
		"1) 发现能力: volclog tool list",
		"capabilities",
		"--trace-redact strict|default",
	} {
		if strings.Contains(text, notWant) {
			t.Fatalf("unexpected verbose text %q in usage text: %q", notWant, text)
		}
	}
}

func TestUsageTextMentionsShortcutDescribeAndSkills(t *testing.T) {
	text := usageText()
	for _, want := range []string{
		"project/topic/index/log 等 shortcut 仅供人工交互",
		"volclog skill install --dir <skills-dir>",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in usage text: %q", want, text)
		}
	}
	if strings.Contains(text, "skill-template/") {
		t.Fatalf("usage text should not expose maintainer-only skill-template dir: %q", text)
	}
}

func TestToolUsageOmitsRecommendedFlow(t *testing.T) {
	for name, text := range map[string]string{
		"tool":          usageTool(),
		"tool list":     usageToolList(),
		"tool describe": usageToolDescribe(),
		"tool exec":     usageToolExec(),
	} {
		for _, notWant := range []string{"Agent Flow:", "推荐流程:"} {
			if strings.Contains(text, notWant) {
				t.Fatalf("%s usage should omit %q: %q", name, notWant, text)
			}
		}
	}
}

func TestToolAndWorkflowExecHelpCarryCommonExecutionGuidance(t *testing.T) {
	for name, text := range map[string]string{
		"tool exec":     usageToolExec(),
		"workflow exec": usageWorkflowExec(),
	} {
		for _, want := range []string{
			"业务请求字段放在 --input",
			"运行时/鉴权/trace/output 控制放在 --context",
			"context.execution",
			"大结果优先",
			"--jmes-filter 命中 null 仍输出 null；缺字段或数组越界会报 filter matched no value",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s usage missing %q: %q", name, want, text)
			}
		}
	}
}

func TestRawHelpClarifiesDryRunAndLiteralBodySemantics(t *testing.T) {
	out := usageRaw()
	for _, want := range []string{
		"raw 的 --input/--body 只是 literal request body",
		"raw --dry-run 只做 transport/local checks；它不会像 tool/workflow 那样校验 API 必填字段",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("raw usage missing %q: %q", want, out)
		}
	}
}

func TestToolListHelpExplainsDiscoveryOperations(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"tool", "list", "-h"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"支持的发现方式:",
		"按 group 看有哪些 action",
		"按 verb 缩小范围",
		"常见 verb:",
		"create / get / list / describe / modify / delete / search",
		"--format <text|json>",
		"Next:",
		"volclog tool describe <group.action>",
		"volclog tool exec <group.action> [--context file://ctx.json] [--input file://req.json|-]",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in stdout: %q", want, out)
		}
	}
	for _, notWant := range []string{"--family", "--group"} {
		if strings.Contains(out, notWant) {
			t.Fatalf("unexpected legacy filter %q in stdout: %q", notWant, out)
		}
	}
}

func TestWorkflowListHelpExplainsCLIWorkflowBoundary(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"workflow", "list", "-h"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"CLI workflow",
		"log.ingest",
		"log.export",
		"log.export-analysis",
		"tool 仍只暴露官网公开 API",
		"volclog workflow describe <group.command>",
		"volclog workflow exec <group.command> --input file://req.json",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in stdout: %q", want, out)
		}
	}
}

func TestManualGroupHelpMentionsShortcutAndToolWorkflow(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"project", "-h"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"Human Shortcut:",
		"默认 volclog 不要停在 shortcut 元命令",
		"volclog project create --describe",
		"volclog project create --print-request-template=full",
		"场景速选:",
		"volclog tool describe project.create",
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
		"Agent:",
		"默认 volclog 先走 volclog tool/workflow describe/exec",
		"列出日志主题",
		"Required:",
		"(none)",
		"--page-size",
		"--all",
		"Tips:",
		"不要因为可选 query 参数里出现了 --project-id",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in stdout: %q", want, out)
		}
	}
	for _, notWant := range []string{
		"volclog topic create --describe",
		"--print-request-template",
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
		"这是 volclog-human 提供的 human shortcut，不是默认 volclog 主流程",
		"执行日志检索",
		"--topic-id",
		"--query",
		"--from",
		"--to",
		"--limit",
		"Tips:",
		"查询条件不完整时先补 topic/time/query",
		"volclog log search --print-request-template=full",
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

func TestShortcutSubcommandHelpPrioritizesToolWorkflowNext(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"project", "create", "-h"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	toolIdx := strings.Index(out, "volclog tool describe project.create")
	describeIdx := strings.Index(out, "volclog project create --describe")
	templateIdx := strings.Index(out, "volclog project create --print-request-template=full")
	if toolIdx < 0 || describeIdx < 0 || templateIdx < 0 {
		t.Fatalf("missing next commands in stdout: %q", out)
	}
	if !(toolIdx < describeIdx && describeIdx < templateIdx) {
		t.Fatalf("expected tool guidance before shortcut metadata in stdout: %q", out)
	}
}

func TestPublicV1DoesNotExposeAssistant(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"assistant", "describe-session-answer"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected non-zero exit for hidden public entry, stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestMetricTopicPromRemainsUnavailable(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"metric-topic", "prom", "query", "--topic-id", "t", "--query", "up"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit=%d, want 1; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr=%q, want empty", stderr.String())
	}
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout should be a structured failure envelope: %v stdout=%q", err, stdout.String())
	}
	errValue, _ := envelope["error"].(map[string]any)
	if got, _ := errValue["message"].(string); got != "unknown metric-topic command: prom" {
		t.Fatalf("error.message=%q, want exact unknown-command error; stdout=%q", got, stdout.String())
	}
	if got, _ := envelope["status"].(string); got != "failed" {
		t.Fatalf("status=%q, want failed; stdout=%q", got, stdout.String())
	}
}

func TestPublicV1DoesNotExposeUnroutedHostGroupOrCollectorCommands(t *testing.T) {
	cases := []struct {
		name        string
		args        []string
		wantMessage string
	}{
		{name: "host group bind rules", args: []string{"host-group", "bind-rules"}, wantMessage: "unknown host-group command: bind-rules"},
		{name: "host group unbind rules", args: []string{"host-group", "unbind-rules"}, wantMessage: "unknown host-group command: unbind-rules"},
		{name: "host group delete host", args: []string{"host-group", "delete-host"}, wantMessage: "unknown host-group command: delete-host"},
		{name: "collector bind host groups", args: []string{"collector", "bind-host-groups"}, wantMessage: "unknown collector command: bind-host-groups"},
		{name: "collector unbind host groups", args: []string{"collector", "unbind-host-groups"}, wantMessage: "unknown collector command: unbind-host-groups"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(tc.args, &stdout, &stderr)
			if code != 1 {
				t.Fatalf("exit=%d, want 1; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout=%q, want empty", stdout.String())
			}
			var payload struct {
				ErrorCode    string `json:"errorCode"`
				ErrorMessage string `json:"errorMessage"`
			}
			if err := json.Unmarshal(stderr.Bytes(), &payload); err != nil {
				t.Fatalf("stderr should be CLIError JSON: %v stderr=%q", err, stderr.String())
			}
			if payload.ErrorCode != "CLIError" {
				t.Fatalf("errorCode=%q, want CLIError; stderr=%q", payload.ErrorCode, stderr.String())
			}
			if payload.ErrorMessage != tc.wantMessage {
				t.Fatalf("errorMessage=%q, want %q; stderr=%q", payload.ErrorMessage, tc.wantMessage, stderr.String())
			}
		})
	}
}

func TestConfigureHelpDescribesDisableSSLScopeCorrectly(t *testing.T) {
	text := usageConfigure()
	// Positive: --disable-ssl only switches the RAM/OIDC STS scheme; the
	// endpoint URL scheme remains authoritative.
	for _, want := range []string{
		"STS scheme",
		"endpoint URL scheme",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("help text missing %q:\n%s", want, text)
		}
	}
	// Positive: --token must be documented as the source session/security token
	// for ramrolearn (RAM source temporary credentials).
	for _, want := range []string{
		"--token",
		"ramrolearn",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("help text missing %q:\n%s", want, text)
		}
	}
	// Positive: the warning must name the fixed STS host and state that
	// --disable-ssl=true sends RAM/OIDC authentication materials over plaintext
	// HTTP to it.
	for _, want := range []string{
		"sts.volcengineapi.com",
		"plaintext",
		"authentication",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("help text missing %q:\n%s", want, text)
		}
	}
	// Negative: must not claim it disables TLS verification, send all
	// subsequent requests over plain HTTP, or reference configurable/private
	// STS endpoints (the STS host is fixed).
	for _, bad := range []string{
		"Disable TLS verification",
		"all subsequent requests over plain HTTP",
		"private STS endpoint",
		"private STS endpoints",
	} {
		if strings.Contains(text, bad) {
			t.Fatalf("help text must not contain %q:\n%s", bad, text)
		}
	}
}
