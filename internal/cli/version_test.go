package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/version"
)

func TestVersionSubcommandPrintsMachineReadableVersionInfo(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	var got versionInfo
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("decode version output %q: %v", stdout.String(), err)
	}
	if got.SchemaVersion != versionInfoSchemaVersion || got.Version != version.Version {
		t.Fatalf("unexpected version identity: %+v", got)
	}
	if got.Edition != string(currentEdition()) || got.Commit != version.Commit {
		t.Fatalf("unexpected build identity: %+v", got)
	}
	if len(got.CatalogDigest) != 64 || got.OperationCount <= 0 || got.PublicOperationCount <= 0 || got.WorkflowCount <= 0 {
		t.Fatalf("unexpected catalog metadata: %+v", got)
	}
	if got.PublicOperationCount > got.OperationCount {
		t.Fatalf("public operations exceed total operations: %+v", got)
	}
}

func TestVersionFlagKeepsPlainTextCompatibility(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--version"}, &stdout, &stderr)
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	want := "volclog " + version.Version + "\n"
	if stdout.String() != want {
		t.Fatalf("stdout=%q, want %q", stdout.String(), want)
	}
	if !strings.HasPrefix(stdout.String(), "volclog volclog-v") {
		t.Fatalf("unexpected compatibility output: %q", stdout.String())
	}
}
