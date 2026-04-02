package cli

import (
	"strings"
	"testing"
)

func TestUsageAPIGeneratedIsConciseAndGuided(t *testing.T) {
	ops := []apiActionOp{
		{
			Cmd: apiCapabilityCommand{
				Group:        "log",
				Action:       "search",
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
		"--describe",
		"--print-request-template[=required|full]",
		"调用输入:",
		"推荐流程:",
		"执行前先使用 --dry-run",
		"下一步命令:",
		"接口协议:",
		"请求体: 通过 --request 传入（必填）",
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
}

func TestUsageAPICallIsDistinctFromUsageAPI(t *testing.T) {
	callText := usageAPICall()
	apiText := usageAPI()
	if callText == apiText {
		t.Fatalf("api call help should be distinct from top-level api help")
	}
	for _, want := range []string{
		"底层直调入口",
		"--method <GET|POST|PUT|DELETE>",
		"--path <path>",
	} {
		if !strings.Contains(callText, want) {
			t.Fatalf("missing %q in api call usage: %s", want, callText)
		}
	}
}

func TestUsageAPIDescribesAgentExecutionEntry(t *testing.T) {
	text := usageAPI()
	for _, want := range []string{
		"面向 agent 与自动化程序的统一执行入口",
		"优先服务智能体、CI/CD、运维脚本、RPA、服务端任务及各类非人工交互场景",
		"帮助调用方以低探索成本完成日志服务操作",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in api usage: %s", want, text)
		}
	}
}
