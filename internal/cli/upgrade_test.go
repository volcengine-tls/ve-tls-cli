package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/upgrade"
)

func TestUpgradeExplicitVersionCheckIsMachineReadableAndOffline(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"upgrade", "--version", "1.0.7"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr=%q", stderr.String())
	}
	var result upgrade.Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode stdout %q: %v", stdout.String(), err)
	}
	if result.SchemaVersion != 1 || result.TargetVersion != "1.0.7" || result.Edition != string(currentEdition()) {
		t.Fatalf("result=%+v", result)
	}
}

func TestParseUpgradeOptionsRejectsCheckAndApply(t *testing.T) {
	_, err := parseUpgradeOptions([]string{"--check", "--yes"})
	if err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("err=%v", err)
	}
}

func TestUpgradeHelpDoesNotCheckNetwork(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"upgrade", "--help"}, &stdout, &stderr)
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "volclog upgrade") {
		t.Fatalf("stdout=%q", stdout.String())
	}
}
