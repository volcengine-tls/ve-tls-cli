package cli

import "testing"

func TestNewAPISuccessEnvelopeAllowsNilContext(t *testing.T) {
	env := newAPISuccessEnvelope(nil, "tool", map[string]any{}, "stdout", "stdout", nil)

	if got := env["status"]; got != "success" {
		t.Fatalf("unexpected status: %#v", got)
	}
	if got := env["action"]; got != "tool" {
		t.Fatalf("unexpected action: %#v", got)
	}
	if got := env["requestId"]; got != "" {
		t.Fatalf("unexpected requestId: %#v", got)
	}
	summary, ok := env["summary"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected summary: %#v", env["summary"])
	}
	if got := summary["dryRun"]; got != false {
		t.Fatalf("unexpected dryRun: %#v", got)
	}
	if _, ok := summary["tracePath"]; ok {
		t.Fatalf("unexpected tracePath: %#v", summary["tracePath"])
	}
	if _, ok := summary["pagination"]; ok {
		t.Fatalf("unexpected pagination: %#v", summary["pagination"])
	}
}
