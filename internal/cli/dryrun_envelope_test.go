package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRawDryRunDoesNotSendRequest(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--dry-run", "raw", "--method", "GET", "--path", "/DescribeProjects"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v", err)
	}
	if out["status"] != "success" {
		t.Fatalf("unexpected status: %v", out["status"])
	}
	summary, ok := out["summary"].(map[string]any)
	if !ok {
		t.Fatalf("missing summary: %v", out)
	}
	if summary["dryRun"] != true {
		t.Fatalf("expected dryRun=true, got %v", summary["dryRun"])
	}
}

func TestRawOutputModeFileReturnsEnvelope(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	outDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--dry-run", "--output-mode", "file", "--output-dir", outDir, "raw", "--method", "GET", "--path", "/DescribeProjects"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	path := parseNoticePath(t, stdout.String())
	if path == "" {
		t.Fatalf("missing artifact path in notice: %q", stdout.String())
	}
	if filepath.Dir(path) != outDir {
		t.Fatalf("artifact path dir mismatch: %v", path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected output file: %v", err)
	}
}

func TestRawDryRunWithTraceAddsTracePathToEnvelope(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	tmp := t.TempDir()
	traceDir := filepath.Join(tmp, "traces")
	outDir := filepath.Join(tmp, "out")
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--dry-run", "--trace-dir", traceDir, "--output-mode", "file", "--output-dir", outDir, "raw", "--method", "GET", "--path", "/DescribeProjects"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	path := parseNoticePath(t, stdout.String())
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read envelope file: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("invalid file json: %v", err)
	}
	summary, ok := out["summary"].(map[string]any)
	if !ok {
		t.Fatalf("missing summary: %v", out["summary"])
	}
	tracePath, _ := summary["tracePath"].(string)
	if tracePath == "" {
		t.Fatalf("missing tracePath in summary: %v", summary)
	}
	if _, err := os.Stat(tracePath); err != nil {
		t.Fatalf("expected trace file: %v", err)
	}
}

func parseNoticePath(t *testing.T, stdout string) string {
	t.Helper()
	const prefix = "文件: "
	idx := strings.LastIndex(stdout, prefix)
	if idx < 0 {
		t.Fatalf("missing file notice in stdout: %q", stdout)
	}
	path := strings.TrimSpace(stdout[idx+len(prefix):])
	if path == "" {
		t.Fatalf("missing file path in stdout: %q", stdout)
	}
	return path
}

func TestRawDryRunIncludesRequestPreviewBody(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"--dry-run",
		"raw",
		"--method", "POST",
		"--path", "/CreateProject",
		"--query", "region=cn-beijing",
		"--body", `{"ProjectName":"demo","Description":"preview"}`,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v", err)
	}
	data, ok := out["data"].(map[string]any)
	if !ok {
		t.Fatalf("missing data plan: %v", out)
	}
	preview, ok := data["request_preview"].(map[string]any)
	if !ok {
		t.Fatalf("missing request_preview: %v", data)
	}
	body, ok := preview["body"].(map[string]any)
	if !ok {
		t.Fatalf("missing preview body: %v", preview)
	}
	if body["ProjectName"] != "demo" {
		t.Fatalf("unexpected preview body: %v", body)
	}
	query, ok := preview["query"].(map[string]any)
	if !ok {
		t.Fatalf("missing preview query: %v", preview)
	}
	if query["region"] != "cn-beijing" {
		t.Fatalf("unexpected preview query: %v", query)
	}
}

func TestRawDryRunAcceptsInputAliasForBody(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"--dry-run",
		"raw",
		"--method", "POST",
		"--path", "/CreateProject",
		"--input", `{"ProjectName":"demo-from-input"}`,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v", err)
	}
	data, ok := out["data"].(map[string]any)
	if !ok {
		t.Fatalf("missing data plan: %v", out)
	}
	preview, ok := data["request_preview"].(map[string]any)
	if !ok {
		t.Fatalf("missing request_preview: %v", data)
	}
	body, ok := preview["body"].(map[string]any)
	if !ok {
		t.Fatalf("missing preview body: %v", preview)
	}
	if body["ProjectName"] != "demo-from-input" {
		t.Fatalf("unexpected preview body: %v", body)
	}
}
