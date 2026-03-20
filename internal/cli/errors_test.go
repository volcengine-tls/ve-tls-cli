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

type errString string

func (e errString) Error() string { return string(e) }

