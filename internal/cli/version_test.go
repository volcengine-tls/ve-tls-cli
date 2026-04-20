package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionSubcommandPrintsVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	if !strings.HasPrefix(strings.TrimSpace(stdout.String()), "volclog v") {
		t.Fatalf("unexpected version output: %q", stdout.String())
	}
}
