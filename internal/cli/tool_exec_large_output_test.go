package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestToolExecAutoArtifactsLargeDryRunResults(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	prevLimit := toolExecAutoArtifactByteLimit
	toolExecAutoArtifactByteLimit = 400
	defer func() { toolExecAutoArtifactByteLimit = prevLimit }()

	tmp := t.TempDir()
	ctxFile := filepath.Join(tmp, "ctx.json")
	reqFile := filepath.Join(tmp, "req.json")
	outDir := filepath.Join(tmp, "out")
	if err := osWriteJSON(ctxFile, map[string]any{
		"region": "cn-beijing",
		"execution": map[string]any{
			"dry_run": true,
			"output": map[string]any{
				"dir": outDir,
			},
		},
	}); err != nil {
		t.Fatalf("write context: %v", err)
	}
	if err := osWriteJSON(reqFile, map[string]any{
		"TopicId":   "t",
		"Query":     "*",
		"StartTime": 1710374400000,
		"EndTime":   1710378000000,
	}); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"tool", "exec", "log.search-logs", "--context", "file://" + ctxFile, "--input", "file://" + reqFile}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	artifactPath := parseNoticePath(t, stdout.String())
	raw, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatalf("read artifact envelope: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("invalid artifact json: %v artifact=%q", err, string(raw))
	}
	summary, ok := out["summary"].(map[string]any)
	if !ok {
		t.Fatalf("expected summary map, got %#v", out["summary"])
	}
	if summary["deliveryMode"] != "file_auto" {
		t.Fatalf("expected file_auto delivery mode, got %#v", summary)
	}
	artifacts, ok := out["artifacts"].([]any)
	if !ok || len(artifacts) != 1 {
		t.Fatalf("expected single artifact, got %#v", out["artifacts"])
	}
	artifact, ok := artifacts[0].(map[string]any)
	if !ok {
		t.Fatalf("expected artifact object, got %#v", artifacts[0])
	}
	filePath, _ := artifact["path"].(string)
	if filePath == "" {
		t.Fatalf("expected artifact path, got %#v", artifact)
	}
	if filePath != artifactPath {
		t.Fatalf("expected notice path to match artifact path: notice=%q artifact=%q", artifactPath, filePath)
	}
	if _, err := os.Stat(filePath); err != nil {
		t.Fatalf("expected artifact file to exist: %v", err)
	}
	data, ok := out["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected file envelope data object, got %#v", out["data"])
	}
	if _, ok := data["request_preview"]; !ok {
		t.Fatalf("expected file envelope data to preserve request_preview, got %#v", data)
	}
}

func TestToolExecExplicitStdoutBypassesAutoArtifact(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	prevLimit := toolExecAutoArtifactByteLimit
	toolExecAutoArtifactByteLimit = 400
	defer func() { toolExecAutoArtifactByteLimit = prevLimit }()

	tmp := t.TempDir()
	ctxFile := filepath.Join(tmp, "ctx.json")
	reqFile := filepath.Join(tmp, "req.json")
	if err := osWriteJSON(ctxFile, map[string]any{
		"region": "cn-beijing",
		"execution": map[string]any{
			"dry_run": true,
		},
	}); err != nil {
		t.Fatalf("write context: %v", err)
	}
	if err := osWriteJSON(reqFile, map[string]any{
		"TopicId":   "t",
		"Query":     "*",
		"StartTime": 1710374400000,
		"EndTime":   1710378000000,
	}); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--output-mode", "stdout", "tool", "exec", "log.search-logs", "--context", "file://" + ctxFile, "--input", "file://" + reqFile}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}

	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v stdout=%q", err, stdout.String())
	}
	summary, ok := out["summary"].(map[string]any)
	if !ok {
		t.Fatalf("expected summary map, got %#v", out["summary"])
	}
	if _, ok := summary["truncated"]; ok {
		t.Fatalf("expected explicit stdout to bypass truncation, got %#v", summary)
	}
	artifacts, ok := out["artifacts"].([]any)
	if !ok {
		t.Fatalf("expected artifacts list, got %#v", out["artifacts"])
	}
	if len(artifacts) != 0 {
		t.Fatalf("expected no artifacts when stdout is explicit, got %#v", artifacts)
	}
}

func TestToolExecAutoArtifactMissingOutputDirReturnsHelpfulError(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	prevLimit := toolExecAutoArtifactByteLimit
	toolExecAutoArtifactByteLimit = 400
	defer func() { toolExecAutoArtifactByteLimit = prevLimit }()

	tmp := t.TempDir()
	ctxFile := filepath.Join(tmp, "ctx.json")
	reqFile := filepath.Join(tmp, "req.json")
	if err := osWriteJSON(ctxFile, map[string]any{
		"region": "cn-beijing",
		"execution": map[string]any{
			"dry_run": true,
		},
	}); err != nil {
		t.Fatalf("write context: %v", err)
	}
	if err := osWriteJSON(reqFile, map[string]any{
		"TopicId":   "t",
		"Query":     "*",
		"StartTime": 1710374400000,
		"EndTime":   1710378000000,
	}); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"tool", "exec", "log.search-logs", "--context", "file://" + ctxFile, "--input", "file://" + reqFile}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected failure stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v stdout=%q", err, stdout.String())
	}
	errObj, ok := out["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %#v", out["error"])
	}
	if kind, _ := errObj["kind"].(string); kind != "validation" {
		t.Fatalf("expected validation kind, got %#v", errObj["kind"])
	}
	msg, _ := errObj["message"].(string)
	if !strings.Contains(msg, "specify --output-dir <writable-dir>") {
		t.Fatalf("expected output_dir guidance in message, got %q", msg)
	}
}
