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
