package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestUsageAPIGeneratedIsConciseAndGuided(t *testing.T) {
	ops := []apiActionOp{
		{
			Cmd: apiCapabilityCommand{
				Group:        "log",
				Action:       "SearchLogs",
				Summary:      "SearchLogs",
				Description:  "检索日志请求。",
				Method:       "POST",
				Path:         "/SearchLogs",
				InputMode:    "body via --request; query/path via flags",
				BodyRequired: true,
			},
			ParamFlags: map[string]apiCapParam{
				"--topic-id": {Name: "TopicId", In: "query", Required: true},
			},
		},
	}
	s := usageAPIGenerated("log", "search", ops)
	for _, want := range []string{
		"优先入口:",
		"volclog log search --describe",
		"volclog capabilities --group log --view text",
		"模板建议:",
		"--print-request-template=required",
		"--print-request-template=full",
		"--describe",
		"--print-request-template[=required|full]",
		"调用输入:",
		"下一步命令:",
		"接口协议:",
		"请求体: 通过 --request 传入（必填）",
		"--output-mode file api log SearchLogs --request file://req.json",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in usage: %s", want, s)
		}
	}
	for _, notWant := range []string{"protocol:", "input:", "request body:"} {
		if strings.Contains(s, notWant) {
			t.Fatalf("unexpected legacy label %q in usage: %s", notWant, s)
		}
	}
	for _, notWant := range []string{"推荐流程:", "大结果优先使用 --output-mode file"} {
		if strings.Contains(s, notWant) {
			t.Fatalf("usage should stay concise and hide %q: %s", notWant, s)
		}
	}
}

func TestUsageAPIGeneratedPluralDescribeIncludesAll(t *testing.T) {
	ops := []apiActionOp{
		{
			Cmd: apiCapabilityCommand{
				Group:   "project",
				Action:  "DescribeProjects",
				Summary: "DescribeProjects",
				Method:  "GET",
				Path:    "/DescribeProjects",
				Params: []apiCapParam{
					{Name: "PageNumber", In: "query", Type: "integer"},
					{Name: "PageSize", In: "query", Type: "integer"},
				},
			},
		},
	}
	s := usageAPIGenerated("project", "DescribeProjects", ops)
	for _, want := range []string{
		"--all",
		"volclog api project DescribeProjects --all",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in usage: %s", want, s)
		}
	}
}

func TestUsageAPIGeneratedPutLogsMentionsMillisecondTimestamp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"api", "log", "PutLogs", "-h"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"Logs[].Time 必须填写 Unix 毫秒时间戳",
		"1710374400000",
		"不要填秒级 1710374400",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in usage: %s", want, out)
		}
	}
}

func TestUsageAPICallIsDistinctFromUsageAPI(t *testing.T) {
	callText := usageAPICall()
	apiText := usageAPI()
	if callText == apiText {
		t.Fatalf("api call help should be distinct from top-level api help")
	}
	for _, want := range []string{
		"底层直调入口；仅在已明确 method/path 时使用",
		"--method <GET|POST|PUT|DELETE>",
		"--path <path>",
	} {
		if !strings.Contains(callText, want) {
			t.Fatalf("missing %q in api call usage: %s", want, callText)
		}
	}
	for _, notWant := range []string{
		"推荐场景:",
		"对比 generated action 与底层直调的请求差异",
		"特殊 IO 接口如需 protobuf/压缩适配",
	} {
		if strings.Contains(callText, notWant) {
			t.Fatalf("api call usage should stay concise and hide %q: %s", notWant, callText)
		}
	}
}

func TestUsageAPIDescribesAgentExecutionEntry(t *testing.T) {
	text := usageAPI()
	for _, want := range []string{
		"用于执行 OpenAPI；优先使用 api <group> <action>",
		"只有在已明确 method/path 时才使用 api call",
		"有 body 时先生成模板",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in api usage: %s", want, text)
		}
	}
	for _, notWant := range []string{
		"优先服务智能体、CI/CD、运维脚本、RPA、服务端任务及各类非人工交互场景",
		"退出码:",
	} {
		if strings.Contains(text, notWant) {
			t.Fatalf("unexpected verbose text %q in api usage: %s", notWant, text)
		}
	}
}

func TestAPIDescribeIncludesShortcutFallbackGuidance(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"api", "log", "SearchLogs", "--describe"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		`"shortcut_first": [`,
		`"volclog log search --describe"`,
		`"volclog log export --describe"`,
		`"fallback_discovery": "volclog capabilities --group log --view text"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in api describe output: %q", want, out)
		}
	}
	if strings.Contains(out, `"scenario_routing":`) {
		t.Fatalf("api action describe should omit scenario_routing: %q", out)
	}
}
