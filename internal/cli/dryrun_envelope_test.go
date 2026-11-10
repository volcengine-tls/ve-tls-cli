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
	assertFileDeliveryNotice(t, stdout.String(), path, "file_forced")
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
	assertFileDeliveryNotice(t, stdout.String(), path, "file_forced")
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
	if currentEdition() == cliEditionVolclogHuman {
		for _, prefix := range []string{
			"结果已写入文件。\n文件: ",
			"结果过大，已写入文件。\n文件: ",
		} {
			if strings.HasPrefix(stdout, prefix) && strings.HasSuffix(stdout, "\n") {
				path := strings.TrimSuffix(strings.TrimPrefix(stdout, prefix), "\n")
				if path != "" && !strings.Contains(path, "\n") {
					return path
				}
			}
		}
		t.Fatalf("invalid human file notice: %q", stdout)
	}
	if currentEdition() != cliEditionVolclog {
		t.Fatalf("unexpected edition %q", currentEdition())
	}

	var env map[string]any
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("invalid file delivery JSON: %v stdout=%q", err, stdout)
	}
	data, hasData := env["data"]
	if env["status"] != "success" || !hasData || data != nil {
		t.Fatalf("expected successful file delivery without result data: %#v", env)
	}
	artifacts, ok := env["artifacts"].([]any)
	if !ok || len(artifacts) == 0 {
		t.Fatalf("missing file delivery artifacts: %#v", env["artifacts"])
	}
	artifact, ok := artifacts[0].(map[string]any)
	if !ok {
		t.Fatalf("invalid first artifact: %#v", artifacts[0])
	}
	path, ok := artifact["path"].(string)
	if !ok || strings.TrimSpace(path) == "" {
		t.Fatalf("missing first artifact path: %#v", artifact)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat delivery artifact %q: %v", path, err)
	}
	if size, ok := artifact["sizeBytes"].(float64); !ok || size != float64(fileInfo.Size()) {
		t.Fatalf("artifact sizeBytes=%#v, want %d", artifact["sizeBytes"], fileInfo.Size())
	}
	summary, ok := env["summary"].(map[string]any)
	if !ok {
		t.Fatalf("missing delivery summary: %#v", env["summary"])
	}
	if mode := summary["deliveryMode"]; mode != "file_forced" && mode != "file_auto" {
		t.Fatalf("unexpected deliveryMode: %#v", mode)
	}
	if total, ok := summary["totalBytes"].(float64); !ok || total != float64(len(stdout)) {
		t.Fatalf("delivery totalBytes=%#v, want stdout length %d", summary["totalBytes"], len(stdout))
	}
	for _, unwanted := range []string{`"request_preview"`, `"preview"`, `"omitted"`, "结果已写入文件。", "结果过大，已写入文件。", "文件: "} {
		if strings.Contains(stdout, unwanted) {
			t.Fatalf("file delivery contains preview or human notice %q: %q", unwanted, stdout)
		}
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
