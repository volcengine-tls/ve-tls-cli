//go:build human

package cli

import (
	"bytes"
	"encoding/json"
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

func TestShortcutDescribeMetricTopicCreateAndModifyWithoutTemplateClaim(t *testing.T) {
	tests := []struct {
		command   string
		action    string
		apiAction string
		method    string
		path      string
	}{
		{
			command:   "create",
			action:    "metric-topic.create",
			apiAction: "CreateMetricTopic",
			method:    "POST",
			path:      "/CreateMetricTopic",
		},
		{
			command:   "modify",
			action:    "metric-topic.modify",
			apiAction: "ModifyMetricTopic",
			method:    "PUT",
			path:      "/ModifyMetricTopic",
		},
	}

	for _, tc := range tests {
		t.Run(tc.command, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run([]string{"metric-topic", tc.command, "--describe"}, &stdout, &stderr)
			if code != 0 {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			out := stdout.String()
			for _, want := range []string{
				`"group": "metric-topic"`,
				`"command": "` + tc.command + `"`,
				`"action": "` + tc.action + `"`,
				`"api_group": "metric-topic"`,
				`"api_action": "` + tc.apiAction + `"`,
				`"method": "` + tc.method + `"`,
				`"path": "` + tc.path + `"`,
				`"flags":`,
				`"--request"`,
			} {
				if !strings.Contains(out, want) {
					t.Fatalf("missing %q in stdout: %q", want, out)
				}
			}
			if strings.Contains(out, `"print_template"`) || strings.Contains(out, "--print-request-template") {
				t.Fatalf("metric-topic %s describe must not advertise an unavailable request template: %q", tc.command, out)
			}
		})
	}
}

func TestShortcutPrintRequestTemplateMetricTopicUnavailable(t *testing.T) {
	tests := []struct {
		command string
		mode    string
		want    string
	}{
		{
			command: "create",
			mode:    "required",
			want:    "request template is not available for metric-topic create",
		},
		{
			command: "modify",
			mode:    "full",
			want:    "request template is not available for metric-topic modify",
		},
	}

	for _, tc := range tests {
		t.Run(tc.command+"_"+tc.mode, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run([]string{"metric-topic", tc.command, "--print-request-template=" + tc.mode}, &stdout, &stderr)
			if code == 0 {
				t.Fatalf("expected failure, stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("structured shortcut error should be written to stdout, stderr=%q", stderr.String())
			}
			var envelope struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatalf("decode error envelope: %v\nstdout=%q", err, stdout.String())
			}
			if envelope.Error.Message != tc.want {
				t.Fatalf("error=%q, want %q", envelope.Error.Message, tc.want)
			}
		})
	}
}

func TestShortcutTemplateSupportRequiresResolvableCurrentOperation(t *testing.T) {
	for _, spec := range shortcutSpecs() {
		if !spec.SupportsTemplate {
			continue
		}
		t.Run(spec.Group+"/"+spec.Command, func(t *testing.T) {
			if strings.TrimSpace(spec.APIGroup) == "" {
				t.Fatal("SupportsTemplate requires a non-empty APIGroup")
			}
			if strings.TrimSpace(spec.APIAction) == "" {
				t.Fatal("SupportsTemplate requires a non-empty APIAction")
			}
			ops, err := shortcutActionOps(spec.APIGroup, spec.APIAction)
			if err != nil {
				t.Fatalf("SupportsTemplate requires a resolvable current operation: %v", err)
			}
			if len(ops) == 0 {
				t.Fatal("SupportsTemplate requires at least one current operation")
			}
			template, err := shortcutRequestTemplateOutput(spec, "required")
			if err != nil {
				t.Fatalf("SupportsTemplate requires a usable required template: %v", err)
			}
			if strings.TrimSpace(template) == "" {
				t.Fatal("SupportsTemplate requires a non-empty required template")
			}
		})
	}
}
