package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAPIDryRunDoesNotSendRequest(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--dry-run", "api", "call", "--method", "GET", "--path", "/DescribeProjects"}, &stdout, &stderr)
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

func TestAPIOutputModeFileReturnsEnvelope(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	outFile := filepath.Join(t.TempDir(), "api-out.json")
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--dry-run", "--output-mode", "file", "--output-file", outFile, "api", "call", "--method", "GET", "--path", "/DescribeProjects"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v; stdout=%q", err, stdout.String())
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

func TestAPIDryRunWithTraceAddsTracePathToEnvelope(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	tmp := t.TempDir()
	traceDir := filepath.Join(tmp, "traces")
	outFile := filepath.Join(tmp, "api-out.json")
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--dry-run", "--trace-dir", traceDir, "--output-mode", "file", "--output-file", outFile, "api", "call", "--method", "GET", "--path", "/DescribeProjects"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v", err)
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
