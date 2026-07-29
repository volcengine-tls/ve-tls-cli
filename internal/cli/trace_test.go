package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth"
	"github.com/volcengine-tls/ve-tls-cli/internal/config"
	"github.com/volcengine-tls/ve-tls-cli/internal/output"
)

func TestTraceClientConstructionFailureDoesNotCreateTrace(t *testing.T) {
	clearAuthTestEnv(t)
	traceDir := t.TempDir()
	ctx := newContext(&bytes.Buffer{}, &bytes.Buffer{}, output.FormatJSON, "dyn", "")
	ctx.cfg = config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"dyn": {
				Mode:     config.AuthModeSSO,
				Region:   "cn-beijing",
				Endpoint: "https://tls-cn-beijing.volces.com",
			},
		},
	}
	ctx.cfgPath = "/tmp/test-config.json"
	ctx.TraceDir = traceDir
	ctx.authFactory = &fakeAuthFactory{ssoErr: errors.New("provider construction failed")}

	if _, err := ctx.DoRaw("GET", "/DescribeProjects", nil, nil, nil); err == nil {
		t.Fatal("DoRaw error=nil, want provider construction failure")
	}
	entries, err := os.ReadDir(traceDir)
	if err != nil {
		t.Fatalf("read trace dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("trace files=%d, want 0 before client construction succeeds", len(entries))
	}
}

func TestNormalizeTraceRedactValue(t *testing.T) {
	cases := map[string]string{
		"":        "on",
		"strict":  "on",
		"default": "on",
		"on":      "on",
		"enabled": "on",
		"off":     "off",
		"false":   "off",
		"weird":   "on",
	}
	for raw, want := range cases {
		if got := normalizeTraceRedactValue(raw); got != want {
			t.Fatalf("raw=%q got=%q want=%q", raw, got, want)
		}
	}
}

// TestTraceDynamicRequestFailureDoesNotLeakSecrets proves that when a dynamic
// request fails (provider Retrieve error), the trace file does not contain
// canary secrets. The trace must only store the redacted error message, never
// raw credentials.
func TestTraceDynamicRequestFailureDoesNotLeakSecrets(t *testing.T) {
	clearAuthTestEnv(t)
	traceDir := t.TempDir()

	cfg := config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"dyn": {
				Mode:     config.AuthModeSSO,
				Region:   "cn-beijing",
				Endpoint: "https://tls-cn-beijing.volces.com",
			},
		},
	}
	ctx := newContext(&bytes.Buffer{}, &bytes.Buffer{}, output.FormatJSON, "", "")
	ctx.cfg = cfg
	ctx.cfgPath = "/tmp/test-config.json"
	ctx.Profile = "dyn"
	ctx.TraceDir = traceDir

	// Provider returns canary credentials in the value but errors on Retrieve.
	provider := &fakeProvider{}
	provider.retrieveFn = func() (auth.Value, error) {
		return auth.Value{
				AccessKeyID:     "AKLTcanary",
				SecretAccessKey: "secret_access_key_canary",
				SessionToken:    "session_token_canary",
			}, &auth.Error{
				Kind:        auth.ReauthRequired,
				Description: "sso token cache missing; run: volclog sso login",
			}
	}
	ctx.authFactory = &fakeAuthFactory{ssoProvider: provider}

	_, _ = ctx.DoRaw("GET", "/DescribeProjects", nil, nil, nil)
	ctx.Close()

	// Read the trace file and verify no canaries leaked.
	entries, err := os.ReadDir(traceDir)
	if err != nil {
		t.Fatalf("read trace dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("expected trace file to be created")
	}
	for _, e := range entries {
		data, rerr := os.ReadFile(filepath.Join(traceDir, e.Name()))
		if rerr != nil {
			t.Fatalf("read trace file: %v", rerr)
		}
		content := string(data)
		for _, c := range authCanaries {
			if strings.Contains(content, c) {
				t.Fatalf("trace file %s contains canary %q:\n%s", e.Name(), c, content)
			}
		}
	}
}
