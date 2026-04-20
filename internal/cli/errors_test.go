package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestClassifyError_UsageMissingFlag(t *testing.T) {
	p, code := classifyError(errString("missing --topic-id"), "", 0, "")
	if code != 1 {
		t.Fatalf("unexpected code: %d", code)
	}
	if p.Kind != "usage" {
		t.Fatalf("unexpected kind: %q", p.Kind)
	}
}

func TestClassifyError_DecodeFilterError(t *testing.T) {
	p, code := classifyError(errString("filter expects object at \"a\""), "", 0, "")
	if code != 3 {
		t.Fatalf("unexpected code: %d", code)
	}
	if p.Kind != "decode" {
		t.Fatalf("unexpected kind: %q", p.Kind)
	}
}

func TestClassifyError_UsageUnknownActionOrGroup(t *testing.T) {
	cases := []string{
		"unknown api group: log",
		"action not found: search-logs",
		"group not found: log",
	}
	for _, msg := range cases {
		p, code := classifyError(errString(msg), "", 0, "")
		if code != 1 {
			t.Fatalf("%q unexpected code: %d", msg, code)
		}
		if p.Kind != "usage" {
			t.Fatalf("%q unexpected kind: %q", msg, p.Kind)
		}
		if !strings.Contains(p.Hint, "volclog tool list") {
			t.Fatalf("%q unexpected hint: %q", msg, p.Hint)
		}
	}
}

func TestClassifyError_UsageMissingFlagHasDescribeHint(t *testing.T) {
	p, code := classifyError(errString("missing --topic-id"), "", 0, "")
	if code != 1 {
		t.Fatalf("unexpected code: %d", code)
	}
	if !strings.Contains(p.Hint, "volclog tool describe <group.action>") {
		t.Fatalf("unexpected hint: %q", p.Hint)
	}
}

func TestClassifyError_GlobalFlagHasPositionHint(t *testing.T) {
	p, code := classifyError(errString("unknown flag: --output-mode"), "", 0, "")
	if code != 1 {
		t.Fatalf("unexpected code: %d", code)
	}
	if !strings.Contains(p.Hint, "position-sensitive") {
		t.Fatalf("unexpected hint: %q", p.Hint)
	}
}

func TestRunMisplacedGlobalFlagHintIncludesFlagAndExample(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"configure", "set", "--output-file", "/tmp/out.json"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("unexpected code: %d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var p map[string]any
	if err := json.Unmarshal(stderr.Bytes(), &p); err != nil {
		t.Fatalf("invalid stderr json: %v stderr=%q", err, stderr.String())
	}
	hint, _ := p["hint"].(string)
	if !strings.Contains(hint, "--output-file") {
		t.Fatalf("hint should mention flag name: %q", hint)
	}
	if !strings.Contains(hint, "volclog --output-file") {
		t.Fatalf("hint should include example: %q", hint)
	}
	if !strings.Contains(hint, "before 'configure'") {
		t.Fatalf("hint should include group position: %q", hint)
	}
}

func TestWriteCLIErrorDoesNotEscapeAngleBrackets(t *testing.T) {
	var buf bytes.Buffer
	writeCLIError(&buf, errString("bad args"), "", 0, "usage", "inspect constraints with 'volclog tool describe <group.action>' or run --help")
	out := buf.String()
	if strings.Contains(out, `\u003c`) || strings.Contains(out, `\u003e`) {
		t.Fatalf("angle brackets should not be escaped: %q", out)
	}
	if !strings.Contains(out, "volclog tool describe <group.action>") {
		t.Fatalf("expected raw angle bracket hint in output: %q", out)
	}
}

func TestClassifyError_ToolExecRequiresFileURLIsUsage(t *testing.T) {
	p, code := classifyError(errString("--context must use file://"), "", 0, "tool")
	if code != 1 {
		t.Fatalf("unexpected code: %d", code)
	}
	if p.Kind != "usage" {
		t.Fatalf("unexpected kind: %q", p.Kind)
	}
	if !strings.Contains(p.Hint, "volclog tool exec") {
		t.Fatalf("unexpected hint: %q", p.Hint)
	}
}

func TestClassifyError_MissingRequiredFieldIsValidation(t *testing.T) {
	cases := []string{
		"missing required field: input.query.ProjectId",
		"missing required fields: input.body.ProjectId, input.body.TopicName",
		"workflow input missing required fields: TopicId, Input",
	}
	for _, msg := range cases {
		p, code := classifyError(errString(msg), "", 0, "tool")
		if code != 1 {
			t.Fatalf("%q unexpected code: %d", msg, code)
		}
		if p.Kind != "validation" {
			t.Fatalf("%q unexpected kind: %q", msg, p.Kind)
		}
		if strings.TrimSpace(p.Hint) == "" {
			t.Fatalf("%q expected validation hint", msg)
		}
	}
}

func TestClassifyError_ValidationHintMatchesSurface(t *testing.T) {
	toolPayload, _ := classifyError(errString("missing required fields: input.body.ProjectId, input.body.TopicName"), "", 0, "tool")
	if strings.Contains(toolPayload.Hint, "workflow describe") {
		t.Fatalf("tool validation hint should not mention workflow describe: %q", toolPayload.Hint)
	}
	if !strings.Contains(toolPayload.Hint, "tool describe") {
		t.Fatalf("tool validation hint should mention tool describe: %q", toolPayload.Hint)
	}

	workflowPayload, _ := classifyError(errString("workflow input missing required fields: TopicId, Input"), "", 0, "workflow")
	if strings.Contains(workflowPayload.Hint, "tool describe") {
		t.Fatalf("workflow validation hint should not mention tool describe: %q", workflowPayload.Hint)
	}
	if !strings.Contains(workflowPayload.Hint, "workflow describe") {
		t.Fatalf("workflow validation hint should mention workflow describe: %q", workflowPayload.Hint)
	}
}

func TestClassifyError_PageAllUnsupportedIsUnsupportedFeature(t *testing.T) {
	cases := []string{
		"execution.page.all is not supported for tool: topic.create-topic",
		"tool topic.describe-topics declares page.all support but runtime pagination metadata is unavailable",
	}
	for _, msg := range cases {
		p, code := classifyError(errString(msg), "", 0, "tool")
		if code != 1 {
			t.Fatalf("%q unexpected code: %d", msg, code)
		}
		if p.Kind != "unsupported_feature" {
			t.Fatalf("%q unexpected kind: %q", msg, p.Kind)
		}
		if strings.TrimSpace(p.Hint) == "" {
			t.Fatalf("%q expected unsupported_feature hint", msg)
		}
	}
}

func TestClassifyError_MissingWritableOutputDirIsFilesystem(t *testing.T) {
	p, code := classifyError(errString("missing writable output_dir"), "", 0, "raw")
	if code != 2 {
		t.Fatalf("unexpected code: %d", code)
	}
	if p.Kind != "filesystem" {
		t.Fatalf("unexpected kind: %q", p.Kind)
	}
	if strings.TrimSpace(p.Hint) == "" {
		t.Fatalf("expected filesystem hint")
	}
}

func TestClassifyError_UnknownFallbackHasHint(t *testing.T) {
	p, code := classifyError(errString("some brand new residual failure"), "", 0, "tool")
	if code != 2 {
		t.Fatalf("unexpected code: %d", code)
	}
	if p.Kind != "unknown" {
		t.Fatalf("unexpected kind: %q", p.Kind)
	}
	if strings.TrimSpace(p.Hint) == "" {
		t.Fatalf("expected unknown fallback hint, got empty hint")
	}
}

func TestClassifyError_CommonResidualValidationShapes(t *testing.T) {
	cases := []string{
		"consume request body must be json object",
		"jsonl line must be object",
		"json-array input must be json array",
		"unsupported log contents type",
		"json: cannot unmarshal object into Go struct field",
	}
	for _, msg := range cases {
		p, code := classifyError(errString(msg), "", 0, "tool")
		if code != 1 {
			t.Fatalf("%q unexpected code: %d", msg, code)
		}
		if p.Kind != "validation" {
			t.Fatalf("%q unexpected kind: %q", msg, p.Kind)
		}
	}
}

func TestClassifyError_CommonResidualRuntimeKinds(t *testing.T) {
	cases := []struct {
		msg  string
		kind string
		code int
	}{
		{"kafka record payload requires --output-mode file", "incompatible_flags", 1},
		{"unexpected list response", "decode", 3},
		{"unexpected list field: Topics", "decode", 3},
		{"cannot infer list field for --all", "decode", 3},
		{"working directory not found", "filesystem", 2},
	}
	for _, tc := range cases {
		p, code := classifyError(errString(tc.msg), "", 0, "tool")
		if code != tc.code {
			t.Fatalf("%q unexpected code: %d", tc.msg, code)
		}
		if p.Kind != tc.kind {
			t.Fatalf("%q unexpected kind: %q", tc.msg, p.Kind)
		}
	}
}

func TestIndexNotExistsHintSuggestsCreateIndex(t *testing.T) {
	p, code := classifyError(&httpError{
		statusCode: 404,
		body:       []byte(`{"ErrorCode":"IndexNotExists","ErrorMessage":"index not exists"}`),
		requestID:  "req-index",
	}, "req-index", 404, "index")
	if code != 2 {
		t.Fatalf("unexpected code: %d", code)
	}
	hint := p.Hint
	if !strings.Contains(hint, "volclog index create --describe") || !strings.Contains(hint, "volclog index create --print-request-template=full") {
		t.Fatalf("unexpected hint: %q", hint)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
