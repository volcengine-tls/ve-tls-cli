package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"volclog/internal/output"
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

func TestCompletionOutputModeFile(t *testing.T) {
	dir := t.TempDir()
	outFile := filepath.Join(dir, "out.json")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"--output-mode", "file", "--output-file", outFile, "completion", "bash"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit code: %d, stderr=%s", code, stderr.String())
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
	if bytes.HasPrefix(b, []byte("\"")) {
		t.Fatalf("unexpected json-encoded output: %s", string(b[:min(len(b), 40)]))
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
