package cli

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestC6TraceCannotClaimUnredactedCapture(t *testing.T) {
	_, calls := outcomeCLIServer(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"Projects":[],"Total":0}`)) })
	for _, value := range []string{"off", "false", "0", "no", "disabled", "typo"} {
		for _, nested := range []bool{false, true} {
			t.Run(value+map[bool]string{true: " context", false: " global"}[nested], func(t *testing.T) {
				dir := filepath.Join(t.TempDir(), "trace")
				args := []string{"--trace-dir", dir, "tool", "exec", "project.describe-projects"}
				if nested {
					args = append(args, "--context", `{"trace":{"redact":"`+value+`"}}`)
				} else {
					args = append(args, "--trace-redact", value)
				}
				code, env := outcomeCLIRun(t, args)
				if code != 1 || calls.Load() != 0 || env["error"].(map[string]any)["kind"] != "validation" {
					t.Fatalf("trace accepted: exit=%d calls=%d env=%#v", code, calls.Load(), env)
				}
				if _, err := os.Stat(dir); !os.IsNotExist(err) {
					t.Fatalf("invalid trace created artifacts: %v", err)
				}
			})
		}
	}
}

func TestC6TraceFalseDisablesGlobalTrace(t *testing.T) {
	for _, value := range []string{`false`, `{"enabled":false}`} {
		t.Run(value, func(t *testing.T) {
			_, calls := outcomeCLIServer(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"Projects":[],"Total":0}`)) })
			dir := filepath.Join(t.TempDir(), "trace")
			code, env := outcomeCLIRun(t, []string{"--trace-dir", dir, "tool", "exec", "project.describe-projects", "--context", `{"trace":` + value + `}`})
			if code != 0 || calls.Load() != 1 {
				t.Fatalf("trace false: exit=%d calls=%d env=%#v", code, calls.Load(), env)
			}
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatalf("disabled trace created artifacts: %v", err)
			}
		})
	}
}
