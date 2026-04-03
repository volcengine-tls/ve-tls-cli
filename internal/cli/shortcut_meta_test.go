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
		`"shortcut_first": [`,
		`"volclog project list --describe"`,
		`"volclog project get --describe"`,
		`"request_body"`,
		`"template_guidance"`,
		`"use_required_when":`,
		`"use_full_when":`,
		`"skip_when":`,
		`"guidance"`,
		`"execute"`,
		`"template"`,
		`"fallback_discovery": "volclog capabilities --group project --view text"`,
		`"fallback_api_describe": "volclog api project CreateProject --describe"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in stdout: %q", want, out)
		}
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

func TestShortcutPrintRequestTemplateIndexCreateRequiredOmitsTopicID(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"index", "create", "--print-request-template=required"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	if strings.Contains(out, `"TopicId"`) {
		t.Fatalf("index shortcut template should omit TopicId because it is a CLI flag: %q", out)
	}
	if !strings.Contains(out, "{") {
		t.Fatalf("expected json template, got: %q", out)
	}
}

func TestShortcutDescribeLogSearchPrefersFileOutput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"log", "search", "--describe"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		`"group": "log"`,
		`"command": "search"`,
		`"preferred_output_mode": "file"`,
		`"--output-mode file"`,
		`"shortcut_first": [`,
		`"volclog log histogram --describe"`,
		`"volclog log context --describe"`,
		`"volclog log put --describe"`,
		`"scenario_routing": [`,
		`"volclog --output-mode file log export --describe"`,
		`"volclog log put --describe"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in stdout: %q", want, out)
		}
	}
}

func TestShortcutDescribeLogPutUsesPutLogsTemplate(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"log", "put", "--describe"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		`"group": "log"`,
		`"command": "put"`,
		`"api_action": "PutLogs"`,
		`"request_body"`,
		`"template_guidance"`,
		`"volclog log put --print-request-template=full"`,
		`"volclog api log PutLogs --describe"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in stdout: %q", want, out)
		}
	}
}

func TestShortcutDescribeHostGroupCreate(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"host-group", "create", "--describe"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		`"group": "host-group"`,
		`"command": "create"`,
		`"api_action": "CreateHostGroup"`,
		`"template_guidance"`,
		`"volclog host-group list --describe"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in stdout: %q", want, out)
		}
	}
}

func TestShortcutDescribeHostGroupListUsesV2Actions(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"host-group", "list", "--describe"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		`"group": "host-group"`,
		`"command": "list"`,
		`"path": "/DescribeHostGroupsV2"`,
		`"api_action": "DescribeHostGroupsV2"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in stdout: %q", want, out)
		}
	}
	for _, notWant := range []string{`"DescribeHostGroups"`} {
		if strings.Contains(out, notWant) && !strings.Contains(out, `"api_action": "DescribeHostGroupsV2"`) {
			t.Fatalf("unexpected legacy action in stdout: %q", out)
		}
	}
}

func TestShortcutDescribeHostGroupBindRulesUsesBindingAction(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"host-group", "bind-rules", "--describe"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		`"group": "host-group"`,
		`"command": "bind-rules"`,
		`"api_action": "ApplyHostGroupToRules"`,
		`"template_guidance"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in stdout: %q", want, out)
		}
	}
}

func TestShortcutDescribeCollectorCreate(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"collector", "create", "--describe"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		`"group": "collector"`,
		`"command": "create"`,
		`"api_action": "CreateRule"`,
		`"template_guidance"`,
		`"volclog collector list --describe"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in stdout: %q", want, out)
		}
	}
}

func TestShortcutDescribeCollectorListUsesV2Actions(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"collector", "list", "--describe"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		`"group": "collector"`,
		`"command": "list"`,
		`"path": "/DescribeRulesV2"`,
		`"api_action": "DescribeRulesV2"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in stdout: %q", want, out)
		}
	}
}

func TestShortcutDescribeCollectorBindHostGroupsUsesBindingAction(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"collector", "bind-host-groups", "--describe"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		`"group": "collector"`,
		`"command": "bind-host-groups"`,
		`"api_action": "ApplyRuleToHostGroups"`,
		`"template_guidance"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in stdout: %q", want, out)
		}
	}
}

func TestShortcutDescribeDoesNotEscapeAngleBrackets(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"project", "create", "--describe"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	if strings.Contains(out, `\u003c`) || strings.Contains(out, `\u003e`) {
		t.Fatalf("angle brackets should not be escaped: %q", out)
	}
}

func TestShortcutPrintRequestTemplateIndexCreateUsesBackendFieldNames(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"index", "create", "--print-request-template=full"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{`"CaseSensitive"`, `"SqlFlag"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in template: %q", want, out)
		}
	}
	for _, notWant := range []string{`"CasSensitive"`, `"SQLFlag"`} {
		if strings.Contains(out, notWant) {
			t.Fatalf("unexpected %q in template: %q", notWant, out)
		}
	}
}

func TestUsageProjectTreatsShortcutAsFirstClassEntry(t *testing.T) {
	text := usageProject()
	for _, want := range []string{
		"High-frequency shortcut for both agents and humans.",
		"volclog project create --describe",
		"volclog project create --print-request-template=full",
		"Fall back to capabilities/api when the shortcut does not cover the need.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in usage: %q", want, text)
		}
	}
	for _, notWant := range []string{
		"Do not start here for discovery.",
		"Prefer api/capabilities unless you intentionally want the shortcut command",
	} {
		if strings.Contains(text, notWant) {
			t.Fatalf("unexpected %q in usage: %q", notWant, text)
		}
	}
}

func TestUsageLogIncludesHighFrequencyWriteAndContextShortcuts(t *testing.T) {
	text := usageLog()
	for _, want := range []string{
		"log put",
		"log context",
		"log histogram",
		"写日志/WebTracking",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in usage: %q", want, text)
		}
	}
}
