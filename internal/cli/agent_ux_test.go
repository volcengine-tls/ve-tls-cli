//go:build human

package cli

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/output"
)

func TestDoctorExitCodeWhenMissingCreds(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "")
	t.Setenv("VOLCENGINE_TOKEN", "")
	t.Setenv("VOLCENGINE_REGION", "")
	t.Setenv("VOLCENGINE_ENDPOINT", "")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"doctor"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("unexpected exit code: %d, stderr=%s", code, stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"credentials"`)) {
		t.Fatalf("unexpected stdout: %s", stdout.String())
	}
}

func TestDoctorIncludesLocalTime(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "")
	t.Setenv("VOLCENGINE_TOKEN", "")
	t.Setenv("VOLCENGINE_REGION", "")
	t.Setenv("VOLCENGINE_ENDPOINT", "")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"doctor"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("unexpected exit code: %d, stderr=%s", code, stderr.String())
	}
	var v map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &v); err != nil {
		t.Fatalf("invalid json: %v, stdout=%s", err, stdout.String())
	}
	timeObj, ok := v["time"].(map[string]any)
	if !ok {
		t.Fatalf("missing time: %v", v)
	}
	localMS, ok := timeObj["local_unix_ms"].(float64)
	if !ok || localMS <= 0 {
		t.Fatalf("unexpected local_unix_ms: %v", timeObj["local_unix_ms"])
	}
}

func TestDoctorOnlineEndpointParseFailureIsReported(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "")
	t.Setenv("VOLCENGINE_TOKEN", "")
	t.Setenv("VOLCENGINE_REGION", "")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://bad host")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"doctor", "--online"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("unexpected exit code: %d, stderr=%s", code, stderr.String())
	}
	var v map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &v); err != nil {
		t.Fatalf("invalid json: %v, stdout=%s", err, stdout.String())
	}
	checks, ok := v["checks"].([]any)
	if !ok {
		t.Fatalf("missing checks: %v", v)
	}
	found := false
	for _, c := range checks {
		m, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if m["name"] == "online_endpoint_parse" {
			found = true
			if okv, _ := m["ok"].(bool); okv {
				t.Fatalf("expected ok=false for online_endpoint_parse: %v", m)
			}
		}
	}
	if !found {
		t.Fatalf("missing online_endpoint_parse check: %v", checks)
	}
}

func TestDoctorOnlineSkipsDirectChecksWhenProxySet(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "")
	t.Setenv("VOLCENGINE_TOKEN", "")
	t.Setenv("VOLCENGINE_REGION", "")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://example.com")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:8888")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"doctor", "--online"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("unexpected exit code: %d, stderr=%s", code, stderr.String())
	}
	var v map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &v); err != nil {
		t.Fatalf("invalid json: %v, stdout=%s", err, stdout.String())
	}
	checks, ok := v["checks"].([]any)
	if !ok {
		t.Fatalf("missing checks: %v", v)
	}
	hasProxy := false
	hasDNS := false
	for _, c := range checks {
		m, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if m["name"] == "online_proxy_detected" {
			hasProxy = true
		}
		if m["name"] == "online_dns_resolve" {
			hasDNS = true
			detail, _ := m["detail"].(string)
			if detail == "" {
				t.Fatalf("expected detail for online_dns_resolve: %v", m)
			}
		}
	}
	if !hasProxy {
		t.Fatalf("expected online_proxy_detected check: %v", checks)
	}
	if !hasDNS {
		t.Fatalf("expected online_dns_resolve check: %v", checks)
	}
}

func TestDoctorResolvesCredRefCredentials(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	cfg := `{
  "version": 1,
  "current_profile": "p1",
  "profiles": {
    "p1": {
      "cred_ref": "shared-aksk",
      "region": "cn-beijing",
      "endpoint": "https://tls-cn-beijing.volces.com"
    }
  },
  "creds": {
    "shared-aksk": {
      "access_key_id": "ak-from-ref",
      "secret_access_key": "sk-from-ref"
    }
  }
}`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "")
	t.Setenv("VOLCENGINE_TOKEN", "")
	t.Setenv("VOLCENGINE_REGION", "")
	t.Setenv("VOLCENGINE_ENDPOINT", "")
	t.Setenv("VOLCLOG_CONFIG", cfgPath)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"doctor"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit code: %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var v map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &v); err != nil {
		t.Fatalf("invalid json: %v stdout=%s", err, stdout.String())
	}
	creds, ok := v["credentials"].(map[string]any)
	if !ok {
		t.Fatalf("missing credentials: %v", v)
	}
	if creds["present"] != true || creds["ak"] != true || creds["sk"] != true {
		t.Fatalf("unexpected credentials: %v", creds)
	}
	if creds["source"] != "profile_cred_ref" {
		t.Fatalf("unexpected credential source: %v", creds["source"])
	}
}

func TestDoctorDoesNotDeriveRegionFromEndpoint(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	cfg := `{
  "version": 1,
  "current_profile": "p1",
  "profiles": {
    "p1": {
      "access_key_id": "ak-inline",
      "secret_access_key": "sk-inline",
      "endpoint": "https://tls-cn-beijing.volces.com"
    }
  }
}`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "")
	t.Setenv("VOLCENGINE_TOKEN", "")
	t.Setenv("VOLCENGINE_REGION", "")
	t.Setenv("VOLCENGINE_ENDPOINT", "")
	t.Setenv("VOLCLOG_CONFIG", cfgPath)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"doctor"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("unexpected exit code: %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var v map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &v); err != nil {
		t.Fatalf("invalid json: %v stdout=%s", err, stdout.String())
	}
	checks, ok := v["checks"].([]any)
	if !ok {
		t.Fatalf("missing checks: %v", v)
	}
	foundRegion := false
	for _, c := range checks {
		m, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if m["name"] == "region_present" {
			foundRegion = true
			if okv, _ := m["ok"].(bool); okv {
				t.Fatalf("expected region_present=false: %v", m)
			}
			if detail, _ := m["detail"].(string); detail != "" {
				t.Fatalf("expected empty region detail, got %q", detail)
			}
		}
	}
	if !foundRegion {
		t.Fatalf("missing region_present check: %v", checks)
	}
}

func TestDoctorOnlineUsesCredRefCredentials(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Fatalf("missing Authorization header")
		}
		w.Header().Set("Date", "Mon, 02 Jan 2006 15:04:05 GMT")
		w.Header().Set("x-tls-requestid", "req-online")
		_, _ = w.Write([]byte(`{"Projects":[],"Total":0}`))
	})
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("listen tcp4 not permitted in this environment: %v", err)
	}
	srv := &http.Server{Handler: handler}
	go func() {
		_ = srv.Serve(ln)
	}()
	defer srv.Close()

	cfgPath := filepath.Join(t.TempDir(), "config.json")
	cfg := `{
  "version": 1,
  "current_profile": "p1",
  "profiles": {
    "p1": {
      "cred_ref": "shared-aksk",
      "region": "cn-beijing",
      "endpoint": "http://` + ln.Addr().String() + `"
    }
  },
  "creds": {
    "shared-aksk": {
      "access_key_id": "ak-from-ref",
      "secret_access_key": "sk-from-ref"
    }
  }
}`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "")
	t.Setenv("VOLCENGINE_TOKEN", "")
	t.Setenv("VOLCENGINE_REGION", "")
	t.Setenv("VOLCENGINE_ENDPOINT", "")
	t.Setenv("VOLCLOG_CONFIG", cfgPath)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"doctor", "--online"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit code: %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var v map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &v); err != nil {
		t.Fatalf("invalid json: %v stdout=%s", err, stdout.String())
	}
	checks, ok := v["checks"].([]any)
	if !ok {
		t.Fatalf("missing checks: %v", v)
	}
	found := false
	for _, c := range checks {
		m, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if m["name"] == "online_describe_projects" {
			found = true
			if okv, _ := m["ok"].(bool); !okv {
				t.Fatalf("expected online_describe_projects ok=true: %v", m)
			}
		}
	}
	if !found {
		t.Fatalf("missing online_describe_projects: %v", checks)
	}
}

func TestRawAllowsTrailingDryRunGlobalFlag(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"raw", "--method", "GET", "--path", "/DescribeProjects", "--dry-run"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit code: %d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid json: %v stdout=%s", err, stdout.String())
	}
	summary, ok := out["summary"].(map[string]any)
	if !ok {
		t.Fatalf("missing summary: %v", out)
	}
	if summary["dryRun"] != true {
		t.Fatalf("expected trailing --dry-run to take effect: %v", summary)
	}
}

func TestToolDescribeRejectsTrailingOutputFileGlobals(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"tool", "describe", "project.create", "--output-mode", "file", "--output-file", filepath.Join(t.TempDir(), "describe.json")}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected output-file to be rejected stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestShortcutOutputModeFileReturnsEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-tls-requestid", "req-project-list")
		_, _ = w.Write([]byte(`{"Projects":[],"Total":0}`))
	}))
	defer srv.Close()

	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", srv.URL)
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	outFile := filepath.Join(t.TempDir(), "project-list.json")
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--output-mode", "file", "--output-file", outFile, "project", "list"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit code: %d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v stdout=%q", err, stdout.String())
	}
	if out["status"] != "success" {
		t.Fatalf("unexpected status: %v", out["status"])
	}
	artifacts, ok := out["artifacts"].([]any)
	if !ok || len(artifacts) != 1 {
		t.Fatalf("unexpected artifacts: %v", out["artifacts"])
	}
	artifact, ok := artifacts[0].(map[string]any)
	if !ok {
		t.Fatalf("invalid artifact: %v", artifacts[0])
	}
	if artifact["path"] != outFile {
		t.Fatalf("artifact path mismatch: %v", artifact["path"])
	}
	if _, err := os.Stat(outFile); err != nil {
		t.Fatalf("expected output file: %v", err)
	}
}

func TestShortcutDescribeAllowsTrailingOutputGlobals(t *testing.T) {
	outFile := filepath.Join(t.TempDir(), "project-create-describe.json")
	var stdout, stderr bytes.Buffer
	code := Run([]string{"project", "create", "--describe", "--output-mode", "file", "--output-file", outFile}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit code: %d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if stdout.String() != outFile+"\n" {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
	if _, err := os.Stat(outFile); err != nil {
		t.Fatalf("expected output file: %v", err)
	}
	b, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read output file: %v", err)
	}
	if !bytes.Contains(b, []byte(`"action": "project.create"`)) {
		t.Fatalf("unexpected output file content: %s", string(b))
	}
}

func TestExtractTrailingGlobalsAllowsShortcutJMESFilter(t *testing.T) {
	rest, merged, ok := extractTrailingGlobals([]string{"list", "--jmes-filter", "Total", "--output-mode", "file"}, GlobalFlags{}, false)
	if !ok {
		t.Fatalf("expected ok")
	}
	if got := len(rest); got != 1 || rest[0] != "list" {
		t.Fatalf("unexpected rest: %#v", rest)
	}
	if merged.Filter != "Total" {
		t.Fatalf("unexpected filter: %q", merged.Filter)
	}
	if merged.OutputMode != "file" {
		t.Fatalf("unexpected output mode: %q", merged.OutputMode)
	}
	if merged.DryRun {
		t.Fatalf("shortcut trailing globals should not enable dry-run")
	}
}

func TestAllowsTrailingDryRunScope(t *testing.T) {
	if !allowsTrailingDryRun("raw", []string{"--method", "GET"}) {
		t.Fatalf("expected raw to allow trailing dry-run")
	}
	if !allowsTrailingDryRun("tool", []string{"exec", "topic.create"}) {
		t.Fatalf("expected tool exec to allow trailing dry-run")
	}
	if allowsTrailingDryRun("tool", []string{"describe", "topic.create"}) {
		t.Fatalf("expected tool describe to reject trailing dry-run")
	}
	if !allowsTrailingDryRun("workflow", []string{"exec", "log.export"}) {
		t.Fatalf("expected workflow exec to allow trailing dry-run")
	}
	if allowsTrailingDryRun("workflow", []string{"describe", "log.export"}) {
		t.Fatalf("expected workflow describe to reject trailing dry-run")
	}
	if allowsTrailingDryRun("project", []string{"list"}) {
		t.Fatalf("expected shortcut groups to reject trailing dry-run")
	}
}

func TestTraceMetaInOutput(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "")
	t.Setenv("VOLCENGINE_TOKEN", "")
	t.Setenv("VOLCENGINE_REGION", "")
	t.Setenv("VOLCENGINE_ENDPOINT", "")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	dir := t.TempDir()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"--trace-dir", dir, "doctor"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("unexpected exit code: %d, stderr=%s", code, stderr.String())
	}
	var v map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &v); err != nil {
		t.Fatalf("invalid json: %v, stdout=%s", err, stdout.String())
	}
	meta, ok := v["meta"].(map[string]any)
	if !ok {
		t.Fatalf("missing meta: %v", v)
	}
	trace, ok := meta["trace"].(map[string]any)
	if !ok {
		t.Fatalf("missing meta.trace: %v", meta)
	}
	path, _ := trace["path"].(string)
	if path == "" {
		t.Fatalf("missing trace path: %v", trace)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected trace file: %v", err)
	}
}

func TestTraceDoesNotIncludeBody(t *testing.T) {
	dir := t.TempDir()
	ctx := newContext(&bytes.Buffer{}, &bytes.Buffer{}, output.FormatJSON, "", "")
	ctx.TraceDir = dir
	ctx.traceRequest("POST", "/x", map[string]string{"a": "b"}, []byte("VOLCENGINE_ACCESS_KEY_SECRET=abc"))
	if ctx.TracePath == "" {
		t.Fatalf("expected trace path")
	}
	b, err := os.ReadFile(ctx.TracePath)
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	if bytes.Contains(b, []byte("VOLCENGINE_ACCESS_KEY_SECRET")) || bytes.Contains(b, []byte("abc")) {
		t.Fatalf("trace leaked secrets: %s", string(b))
	}
}
