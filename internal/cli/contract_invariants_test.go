package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// Contract invariants for agent-facing surfaces:
// 1. machine-readable errors on tool/workflow/raw prefer stdout failed envelope
// 2. exit 1 = deterministic local/config/input failures
// 3. exit 2 = upstream/auth/network/server boundary
// 4. exit 3 = decode/filter/post-process failures
// 5. known deterministic identities must not fall back to kind=unknown
// 6. --input carries business payload only; runtime/context fields belong in --context

func TestContractInvariant_MachineReadableErrorsUseStdoutFailedEnvelope(t *testing.T) {
	setInvariantRuntimeEnv(t)

	cases := []struct {
		name     string
		args     []string
		wantExit int
		wantKind string
	}{
		{
			name:     "tool list bad group",
			args:     []string{"tool", "list", "definitely-missing-group"},
			wantExit: 1,
			wantKind: "usage",
		},
		{
			name:     "tool describe unknown",
			args:     []string{"tool", "describe", "log.not-real"},
			wantExit: 1,
			wantKind: "usage",
		},
		{
			name:     "tool exec unknown",
			args:     []string{"tool", "exec", "log.not-real"},
			wantExit: 1,
			wantKind: "usage",
		},
		{
			name:     "workflow describe unknown",
			args:     []string{"workflow", "describe", "log.not-real"},
			wantExit: 1,
			wantKind: "usage",
		},
		{
			name:     "raw invalid filter",
			args:     []string{"--dry-run", "--jmes-filter", "???", "raw", "--method", "GET", "--path", "/DescribeProjects"},
			wantExit: 3,
			wantKind: "decode",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := requireFailedEnvelope(t, tc.args, tc.wantExit)
			errObj, ok := out["error"].(map[string]any)
			if !ok {
				t.Fatalf("expected error object, got %#v", out["error"])
			}
			if got := asStringOrEmpty(errObj["kind"]); got != tc.wantKind {
				t.Fatalf("expected kind=%q, got %#v", tc.wantKind, errObj["kind"])
			}
		})
	}
}

func TestContractInvariant_ExitCodeBoundary(t *testing.T) {
	setInvariantRuntimeEnv(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-tls-requestid", "req-server-error")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"ErrorCode":"InternalError","ErrorMessage":"boom"}`))
	}))
	defer srv.Close()

	t.Run("local secrets file missing stays exit1", func(t *testing.T) {
		out := requireFailedEnvelope(t, []string{
			"--secrets-file", filepath.Join(t.TempDir(), "missing.env"),
			"tool", "exec", "account.get",
		}, 1)
		errObj := out["error"].(map[string]any)
		if got := asStringOrEmpty(errObj["kind"]); got != "config" {
			t.Fatalf("expected config kind, got %#v", errObj["kind"])
		}
		if got := asStringOrEmpty(errObj["source"]); got != "cli" {
			t.Fatalf("expected cli source, got %#v", errObj["source"])
		}
	})

	t.Run("upstream server error stays exit2", func(t *testing.T) {
		t.Setenv("VOLCENGINE_ENDPOINT", srv.URL)
		out := requireFailedEnvelope(t, []string{
			"raw", "--method", "GET", "--path", "/DescribeProjects",
		}, 2)
		errObj := out["error"].(map[string]any)
		if got := asStringOrEmpty(errObj["kind"]); got != "server" {
			t.Fatalf("expected server kind, got %#v", errObj["kind"])
		}
		if got := asStringOrEmpty(errObj["source"]); got != "upstream" {
			t.Fatalf("expected upstream source, got %#v", errObj["source"])
		}
	})

	t.Run("decode failure stays exit3", func(t *testing.T) {
		out := requireFailedEnvelope(t, []string{
			"--dry-run", "--jmes-filter", "???", "raw", "--method", "GET", "--path", "/DescribeProjects",
		}, 3)
		errObj := out["error"].(map[string]any)
		if got := asStringOrEmpty(errObj["kind"]); got != "decode" {
			t.Fatalf("expected decode kind, got %#v", errObj["kind"])
		}
	})
}

func TestContractInvariant_InputContextBoundary(t *testing.T) {
	setInvariantRuntimeEnv(t)

	out := requireFailedEnvelope(t, []string{
		"--dry-run",
		"tool", "exec", "log.search",
		"--input", `{"TopicId":"tid","Query":"*","StartTime":1,"EndTime":2,"context":{"trace":{"enabled":true}}}`,
	}, 1)
	errObj := out["error"].(map[string]any)
	if got := asStringOrEmpty(errObj["kind"]); got != "validation" {
		t.Fatalf("expected validation kind, got %#v", errObj["kind"])
	}
	if !strings.Contains(asStringOrEmpty(errObj["message"]), "reserved context/runtime fields") {
		t.Fatalf("unexpected message: %#v", errObj["message"])
	}
	if !strings.Contains(asStringOrEmpty(errObj["hint"]), "--context") {
		t.Fatalf("unexpected hint: %#v", errObj["hint"])
	}
}

func TestContractInvariant_FilterSemantics(t *testing.T) {
	setInvariantRuntimeEnv(t)

	t.Run("null remains success", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := Run([]string{
			"--dry-run",
			"--jmes-filter", "error",
			"raw", "--method", "GET", "--path", "/DescribeProjects",
		}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("expected exit=0 stdout=%q stderr=%q", stdout.String(), stderr.String())
		}
		if strings.TrimSpace(stderr.String()) != "" {
			t.Fatalf("expected empty stderr, got %q", stderr.String())
		}
		if got := strings.TrimSpace(stdout.String()); got != "null" {
			t.Fatalf("expected null filter result, got %q", got)
		}
	})

	t.Run("tool exec filter miss returns envelope", func(t *testing.T) {
		out := requireFailedEnvelope(t, []string{
			"--dry-run",
			"--jmes-filter", "missing.path",
			"tool", "exec", "project.describe-projects",
			"--input", `{}`,
		}, 3)
		errObj := out["error"].(map[string]any)
		if got := asStringOrEmpty(errObj["kind"]); got != "decode" {
			t.Fatalf("expected decode kind, got %#v", errObj["kind"])
		}
	})

	t.Run("workflow exec filter syntax returns envelope", func(t *testing.T) {
		out := requireFailedEnvelope(t, []string{
			"--dry-run",
			"--jmes-filter", "???",
			"workflow", "exec", "log.export",
			"--input", `{"TopicId":"tid","Query":"*","StartTime":1,"EndTime":2,"MaxPages":1}`,
		}, 3)
		errObj := out["error"].(map[string]any)
		if got := asStringOrEmpty(errObj["kind"]); got != "decode" {
			t.Fatalf("expected decode kind, got %#v", errObj["kind"])
		}
		if !strings.Contains(asStringOrEmpty(errObj["message"]), "invalid jmes-filter expression") {
			t.Fatalf("unexpected message: %#v", errObj["message"])
		}
	})
}

func requireFailedEnvelope(t *testing.T, args []string, wantExit int) map[string]any {
	t.Helper()

	var stdout, stderr bytes.Buffer
	code := Run(args, &stdout, &stderr)
	if code != wantExit {
		t.Fatalf("expected exit=%d, got %d stdout=%q stderr=%q", wantExit, code, stdout.String(), stderr.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v stdout=%q", err, stdout.String())
	}
	if out["status"] != "failed" {
		t.Fatalf("expected failed status, got %#v", out["status"])
	}
	return out
}

func setInvariantRuntimeEnv(t *testing.T) {
	t.Helper()
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))
}
