package cli

import "testing"

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
	}
}

type errString string

func (e errString) Error() string { return string(e) }
