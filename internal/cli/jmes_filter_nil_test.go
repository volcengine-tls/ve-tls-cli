package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestRawJMESFilterNilResultReturnsEnvelopeError(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"--dry-run",
		"raw",
		"--method", "GET",
		"--path", "/DescribeProjects",
		"--jmes-filter", "data.missing.field",
	}, &stdout, &stderr)
	if code != 3 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr for envelope error, got %q", stderr.String())
	}

	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v stdout=%q", err, stdout.String())
	}
	if out["status"] != "failed" {
		t.Fatalf("expected status=failed, got %v", out["status"])
	}
	errObj, ok := out["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %#v", out["error"])
	}
	if errObj["kind"] != "decode" {
		t.Fatalf("expected decode error kind, got %#v", errObj)
	}
	if !strings.Contains(asStringOrEmpty(errObj["message"]), "matched no value") {
		t.Fatalf("expected matched no value message, got %#v", errObj["message"])
	}
	if !strings.Contains(asStringOrEmpty(errObj["message"]), "result scope") {
		t.Fatalf("expected result scope in error message, got %#v", errObj["message"])
	}
	if !strings.Contains(asStringOrEmpty(errObj["message"]), "available keys") {
		t.Fatalf("expected available keys hint in error message, got %#v", errObj["message"])
	}
}

func TestToolExecProjectionNilResultReturnsEnvelopeError(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	tmp := t.TempDir()
	ctxFile := filepath.Join(tmp, "ctx.json")
	reqFile := filepath.Join(tmp, "req.json")
	if err := osWriteJSON(ctxFile, map[string]any{
		"region": "cn-beijing",
		"execution": map[string]any{
			"dry_run":    true,
			"projection": "missing.field",
		},
	}); err != nil {
		t.Fatalf("write context: %v", err)
	}
	if err := osWriteJSON(reqFile, map[string]any{
		"body": map[string]any{
			"ProjectId":  "pid",
			"ShardCount": 2,
			"TopicName":  "demo",
			"Ttl":        30,
		},
	}); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"tool", "exec", "topic.create-topic",
		"--context", "file://" + ctxFile,
		"--input", "file://" + reqFile,
	}, &stdout, &stderr)
	if code != 3 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr for envelope error, got %q", stderr.String())
	}

	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v stdout=%q", err, stdout.String())
	}
	if out["status"] != "failed" {
		t.Fatalf("expected status=failed, got %#v", out["status"])
	}
	errObj, ok := out["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %#v", out["error"])
	}
	if errObj["kind"] != "decode" {
		t.Fatalf("expected decode error kind, got %#v", errObj)
	}
	if !strings.Contains(asStringOrEmpty(errObj["message"]), "matched no value") {
		t.Fatalf("expected matched no value message, got %#v", errObj["message"])
	}
	if !strings.Contains(asStringOrEmpty(errObj["message"]), "result scope") {
		t.Fatalf("expected result scope in error message, got %#v", errObj["message"])
	}
	if !strings.Contains(asStringOrEmpty(errObj["message"]), "available keys") {
		t.Fatalf("expected available keys hint in error message, got %#v", errObj["message"])
	}
}
