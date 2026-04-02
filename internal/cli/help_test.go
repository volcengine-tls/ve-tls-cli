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
		"CreateProject: 创建日志项目请求",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in stdout: %q", want, out)
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
		"统一执行入口",
		"优先服务智能体、CI/CD、运维脚本、RPA、服务端任务及各类非人工交互场景",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in usage text: %q", want, text)
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
		"Do not start here for discovery.",
		"volclog capabilities --view text",
		"volclog api project <action> --describe",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in stdout: %q", want, out)
		}
	}
}
