package cli

import (
	"bytes"
	"slices"
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
		`"input": {`,
		`"flags": {`,
		`"--project-name"`,
		`"--description"`,
		`"--request"`,
		`"guidance"`,
		`"execute"`,
		`"fallback_discovery": "volclog capabilities --group project --view text"`,
		`"fallback_api_describe": "volclog api project CreateProject --describe"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in stdout: %q", want, out)
		}
	}
	if strings.Contains(out, `"request_body":`) {
		t.Fatalf("project create should prefer direct shortcut flags instead of request_body hint: %q", out)
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
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in stdout: %q", want, out)
		}
	}
	if strings.Contains(out, `"scenario_routing":`) {
		t.Fatalf("shortcut action describe should omit scenario_routing: %q", out)
	}
}

func TestShortcutDescribeSeparatesOptionalParamsAndUsageGuidance(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"topic", "list", "--describe"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		`"input": {`,
		`"flags": {`,
		`"fields": [`,
		`"required": false`,
		`"optional": "只在用户明确给出过滤、分页、排序、范围或额外约束时，再填写 optional；不填表示按当前快捷命令默认行为执行，不要从示例或历史请求里补齐。`,
		`"--project-id"`,
		`"--all"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in stdout: %q", want, out)
		}
	}
	for _, notWant := range []string{
		`"params":`,
		`"request_body":`,
		`"required_flags":`,
		`"required_params":`,
		`"optional_params":`,
		`"param_guidance":`,
		`"template_guidance":`,
		`"body_params_doc":`,
		`"query_params_doc":`,
	} {
		if strings.Contains(out, notWant) {
			t.Fatalf("shortcut describe should omit legacy field %q: %q", notWant, out)
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
		`"input": {`,
		`"flags": {`,
		`"--request"`,
		`"--request-format"`,
		`"request_body": {`,
		`"volclog log put --print-request-template=required|full"`,
		`"volclog api log PutLogs --describe"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in stdout: %q", want, out)
		}
	}
}

func TestShortcutDescribeLogIngestUsesPutLogsAction(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"log", "ingest", "--describe"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		`"group": "log"`,
		`"command": "ingest"`,
		`"api_action": "PutLogs"`,
		`lines 输入默认写入字段 __content__`,
		`log-count`,
		`earliest-log-time`,
		`latest-log-time`,
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
		`"input": {`,
		`"flags": {`,
		`"--host-group-name"`,
		`"--host-group-type"`,
		`"--request"`,
		`"volclog host-group list --describe"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in stdout: %q", want, out)
		}
	}
	if strings.Contains(out, `"request_body":`) {
		t.Fatalf("host-group create should prefer direct shortcut flags instead of request_body hint: %q", out)
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
		`"input": {`,
		`"flags": {`,
		`"--topic-id"`,
		`"--rule-name"`,
		`"--request"`,
		`"volclog collector list --describe"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in stdout: %q", want, out)
		}
	}
	if strings.Contains(out, `"request_body":`) {
		t.Fatalf("collector create should prefer direct shortcut flags instead of request_body hint: %q", out)
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

func TestShortcutSpecsPublicV1GroupsOnly(t *testing.T) {
	var got []string
	for _, group := range sortedShortcutGroupsWithMeta() {
		got = append(got, group)
	}
	want := []string{"collector", "host-group", "index", "log", "metric-topic", "project", "topic"}
	if !slices.Equal(got, want) {
		t.Fatalf("unexpected public shortcut groups: got=%v want=%v", got, want)
	}
}

func TestShortcutQueryFlagsFollowOfficialDocsForPublicV1(t *testing.T) {
	cases := []struct {
		group      string
		command    string
		notWantCLI []string
	}{
		{group: "project", command: "list", notWantCLI: []string{"--fuzzy-search-key", "--favourite/--no-favourite"}},
		{group: "project", command: "get", notWantCLI: []string{"--topic-types"}},
		{group: "topic", command: "list", notWantCLI: []string{"--cursor"}},
		{group: "topic", command: "modify", notWantCLI: []string{"--favourite/--no-favourite"}},
		{group: "collector", command: "list", notWantCLI: []string{"--rule-type"}},
	}
	for _, tc := range cases {
		spec, ok := lookupShortcutSpec(tc.group, tc.command)
		if !ok {
			t.Fatalf("missing shortcut spec for %s.%s", tc.group, tc.command)
		}
		for _, notWant := range tc.notWantCLI {
			for _, param := range spec.Params {
				if param.CLIFlag == notWant {
					t.Fatalf("%s.%s should not expose undocumented flag %s", tc.group, tc.command, notWant)
				}
			}
		}
	}
}

func TestPublicV1RemovesNonCRUDHostGroupAndCollectorShortcuts(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{name: "host-group bind-rules", args: []string{"host-group", "bind-rules", "--describe"}},
		{name: "host-group delete-host", args: []string{"host-group", "delete-host", "--describe"}},
		{name: "collector bind-host-groups", args: []string{"collector", "bind-host-groups", "--describe"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(tc.args, &stdout, &stderr)
			if code == 0 {
				t.Fatalf("expected non-zero exit for removed public shortcut, stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
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
		"log ingest",
		"log context",
		"log histogram",
		"写日志/WebTracking",
		"批量导入文本或 JSON 日志",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in usage: %q", want, text)
		}
	}
}
