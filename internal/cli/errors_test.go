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
		if !strings.Contains(p.Hint, "volclog capabilities --view text") {
			t.Fatalf("%q unexpected hint: %q", msg, p.Hint)
		}
	}
}

func TestClassifyError_UsageMissingFlagHasDescribeHint(t *testing.T) {
	p, code := classifyError(errString("missing --topic-id"), "", 0, "")
	if code != 1 {
		t.Fatalf("unexpected code: %d", code)
	}
	if !strings.Contains(p.Hint, "volclog api <group> <action> --describe") {
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
	code := Run([]string{"capabilities", "--output-file", "/tmp/out.json"}, &stdout, &stderr)
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
	if !strings.Contains(hint, "before 'capabilities'") {
		t.Fatalf("hint should include group position: %q", hint)
	}
}

func TestWriteCLIErrorDoesNotEscapeAngleBrackets(t *testing.T) {
	var buf bytes.Buffer
	writeCLIError(&buf, errString("bad args"), "", 0, "usage", "inspect constraints with 'volclog api <group> <action> --describe' or run --help")
	out := buf.String()
	if strings.Contains(out, `\u003c`) || strings.Contains(out, `\u003e`) {
		t.Fatalf("angle brackets should not be escaped: %q", out)
	}
	if !strings.Contains(out, "volclog api <group> <action> --describe") {
		t.Fatalf("expected raw angle bracket hint in output: %q", out)
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
