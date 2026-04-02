package cli

import (
	"strings"
	"testing"
)

func TestClassifyError_UsageMissingFlag(t *testing.T) {
	p, code := classifyError(errString("missing --topic-id"), "", 0)
	if code != 1 {
		t.Fatalf("unexpected code: %d", code)
	}
	if p.Kind != "usage" {
		t.Fatalf("unexpected kind: %q", p.Kind)
	}
}

func TestClassifyError_DecodeFilterError(t *testing.T) {
	p, code := classifyError(errString("filter expects object at \"a\""), "", 0)
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
		p, code := classifyError(errString(msg), "", 0)
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
	p, code := classifyError(errString("missing --topic-id"), "", 0)
	if code != 1 {
		t.Fatalf("unexpected code: %d", code)
	}
	if !strings.Contains(p.Hint, "volclog api <group> <action> --describe") {
		t.Fatalf("unexpected hint: %q", p.Hint)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
