//go:build !agent

package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestShortcutDescribeProjectCreate(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"project", "create", "--describe"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		`"group": "project"`,
		`"command": "create"`,
		`"action": "project.create"`,
		`"api_group": "project"`,
		`"api_action": "CreateProject"`,
		`"describe": "volclog tool describe project.create"`,
		`"execute": "volclog tool exec project.create --context file://ctx.json --input file://req.json"`,
		`"fallback_discovery": "volclog tool list project"`,
		`"fallback_api_describe": "volclog tool describe project.create"`,
		`"--project-name"`,
		`"--description"`,
		`"--request"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in stdout: %q", want, out)
		}
	}
	if strings.Contains(out, `"request_body":`) {
		t.Fatalf("project create should prefer direct shortcut flags instead of request_body hint: %q", out)
	}
	if strings.Contains(out, `"shortcut_first"`) {
		t.Fatalf("project create shortcut describe should not prioritize shortcut-first guidance anymore: %q", out)
	}
}

func TestShortcutPrintRequestTemplateTopicCreateFull(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"topic", "create", "--print-request-template=full"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{`"ProjectId"`, `"TopicName"`, `"Description"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in stdout: %q", want, out)
		}
	}
}

func TestShortcutDescribeLogHistogramUsesDescribeHistogramV1(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"log", "histogram", "--describe"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		`"command": "histogram"`,
		`"action": "log.histogram"`,
		`"path": "/DescribeHistogramV1"`,
		`"api_action": "DescribeHistogramV1"`,
		`"DescribeHistogramV1"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in stdout: %q", want, out)
		}
	}
	if strings.Contains(out, `"api_action": "DescribeHistogram"`) {
		t.Fatalf("unexpected legacy api_action in stdout: %q", out)
	}
}
