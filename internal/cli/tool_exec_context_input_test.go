package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestToolExecAllowsMissingContextByDefaultingToEmptyObject(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--dry-run", "tool", "exec", "account.get"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v", err)
	}
	summary, ok := out["summary"].(map[string]any)
	if !ok || summary["dryRun"] != true {
		t.Fatalf("expected dryRun summary, got %#v", out["summary"])
	}
}

func TestToolExecAcceptsInputFromStdin(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	tmp := t.TempDir()
	ctxFile := filepath.Join(tmp, "ctx.json")
	if err := osWriteJSON(ctxFile, map[string]any{
		"execution": map[string]any{
			"dry_run": true,
		},
	}); err != nil {
		t.Fatalf("write context: %v", err)
	}

	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	if _, err := w.Write([]byte("{}")); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	_ = w.Close()
	os.Stdin = r
	defer func() {
		os.Stdin = oldStdin
		_ = r.Close()
	}()

	var stdout, stderr bytes.Buffer
	code := Run([]string{"tool", "exec", "project.describe-projects", "--context", "file://" + ctxFile, "--input", "-"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v", err)
	}
	summary, ok := out["summary"].(map[string]any)
	if !ok || summary["dryRun"] != true {
		t.Fatalf("expected dryRun summary, got %#v", out["summary"])
	}
}

func TestToolExecArtifactOutputModeAliasMatchesArtifactTrue(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	tmp := t.TempDir()
	ctxFile := filepath.Join(tmp, "ctx.json")
	reqFile := filepath.Join(tmp, "req.json")
	if err := osWriteJSON(ctxFile, map[string]any{
		"execution": map[string]any{
			"dry_run":     true,
			"output_mode": "artifact",
			"output": map[string]any{
				"dir": t.TempDir(),
			},
		},
	}); err != nil {
		t.Fatalf("write context: %v", err)
	}
	if err := osWriteJSON(reqFile, map[string]any{}); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"tool", "exec", "project.describe-projects", "--context", "file://" + ctxFile, "--input", "file://" + reqFile}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	path := parseNoticePath(t, stdout.String())
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read artifact envelope: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("invalid artifact json: %v", err)
	}
	summary, ok := out["summary"].(map[string]any)
	if !ok || summary["outputMode"] != "file" {
		t.Fatalf("expected file output mode, got %#v", out["summary"])
	}
	artifacts, ok := out["artifacts"].([]any)
	if !ok || len(artifacts) != 1 {
		t.Fatalf("expected single artifact, got %#v", out["artifacts"])
	}
}

func TestToolExecAcceptsInlineJSONInput(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	tmp := t.TempDir()
	ctxFile := filepath.Join(tmp, "ctx.json")
	if err := osWriteJSON(ctxFile, map[string]any{
		"execution": map[string]any{
			"dry_run": true,
		},
	}); err != nil {
		t.Fatalf("write context: %v", err)
	}

	inline := `{"ProjectId":"pid-demo","TopicName":"demo","Ttl":30,"ShardCount":1}`
	var stdout, stderr bytes.Buffer
	code := Run([]string{"tool", "exec", "topic.create", "--context", "file://" + ctxFile, "--input", inline}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v", err)
	}
	data, ok := out["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data map, got %#v", out["data"])
	}
	preview, ok := data["request_preview"].(map[string]any)
	if !ok {
		t.Fatalf("expected request_preview, got %#v", data)
	}
	body, ok := preview["body"].(map[string]any)
	if !ok {
		t.Fatalf("expected preview body, got %#v", preview["body"])
	}
	if body["TopicName"] != "demo" || body["ProjectId"] != "pid-demo" {
		t.Fatalf("unexpected preview body: %#v", body)
	}
}

func TestToolExecAllowsFlatQueryInputWithoutSectionWrapper(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	tmp := t.TempDir()
	ctxFile := filepath.Join(tmp, "ctx.json")
	reqFile := filepath.Join(tmp, "req.json")
	if err := osWriteJSON(ctxFile, map[string]any{
		"execution": map[string]any{
			"dry_run": true,
		},
	}); err != nil {
		t.Fatalf("write context: %v", err)
	}
	if err := osWriteJSON(reqFile, map[string]any{
		"ProjectId": "pid-flat",
	}); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"tool", "exec", "project.describe-projects", "--context", "file://" + ctxFile, "--input", "file://" + reqFile}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v", err)
	}
	data, ok := out["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data map, got %#v", out["data"])
	}
	preview, ok := data["request_preview"].(map[string]any)
	if !ok {
		t.Fatalf("expected request_preview, got %#v", data)
	}
	query, ok := preview["query"].(map[string]any)
	if !ok {
		t.Fatalf("expected preview query, got %#v", preview["query"])
	}
	if query["ProjectId"] != "pid-flat" {
		t.Fatalf("unexpected preview query: %#v", query)
	}
	body, _ := preview["body"].(map[string]any)
	if body != nil && len(body) != 0 {
		t.Fatalf("expected empty body for GET flat query input, got %#v", body)
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

func TestToolExecRejectsConflictingGlobalAndContextProfile(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	tmp := t.TempDir()
	ctxFile := filepath.Join(tmp, "ctx.json")
	reqFile := filepath.Join(tmp, "req.json")
	if err := osWriteJSON(ctxFile, map[string]any{
		"profile": "context-profile",
		"execution": map[string]any{
			"dry_run": true,
		},
	}); err != nil {
		t.Fatalf("write context: %v", err)
	}
	if err := osWriteJSON(reqFile, map[string]any{}); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"--profile", "global-profile",
		"tool", "exec", "project.describe-projects",
		"--context", "file://" + ctxFile,
		"--input", "file://" + reqFile,
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected conflict failure stdout=%q stderr=%q", stdout.String(), stderr.String())
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
	errObj, ok := out["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %#v", out["error"])
	}
	if errObj["kind"] != "validation" {
		t.Fatalf("expected validation kind, got %#v", errObj)
	}
	if errObj["source"] != "cli" {
		t.Fatalf("expected cli source, got %#v", errObj["source"])
	}
	if !strings.Contains(asStringOrEmpty(errObj["message"]), "conflicting profile selectors") {
		t.Fatalf("unexpected error message: %#v", errObj["message"])
	}
}

func TestToolExecProfileConflictDoesNotLoadSecretsSideEffects(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	const sideEffectKey = "VOLCLOG_SIDE_EFFECT_TEST"
	original, hadOriginal := os.LookupEnv(sideEffectKey)
	_ = os.Unsetenv(sideEffectKey)
	defer func() {
		if hadOriginal {
			_ = os.Setenv(sideEffectKey, original)
		} else {
			_ = os.Unsetenv(sideEffectKey)
		}
	}()

	tmp := t.TempDir()
	secretsFile := filepath.Join(tmp, "secrets.env")
	if err := os.WriteFile(secretsFile, []byte(sideEffectKey+"=from-secrets\n"), 0o600); err != nil {
		t.Fatalf("write secrets file: %v", err)
	}
	ctxFile := filepath.Join(tmp, "ctx.json")
	reqFile := filepath.Join(tmp, "req.json")
	if err := osWriteJSON(ctxFile, map[string]any{
		"profile":      "context-profile",
		"secrets_file": secretsFile,
		"execution": map[string]any{
			"dry_run": true,
		},
	}); err != nil {
		t.Fatalf("write context: %v", err)
	}
	if err := osWriteJSON(reqFile, map[string]any{}); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"--profile", "global-profile",
		"tool", "exec", "project.describe-projects",
		"--context", "file://" + ctxFile,
		"--input", "file://" + reqFile,
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected conflict failure stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if got, ok := os.LookupEnv(sideEffectKey); ok {
		t.Fatalf("expected secrets side effect to be absent, got %q", got)
	}
}

func TestToolExecRejectsConflictingGlobalProfileAndSecretsFile(t *testing.T) {
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	tmp := t.TempDir()
	secretsFile := filepath.Join(tmp, "secrets.env")
	if err := os.WriteFile(secretsFile, []byte("VOLCENGINE_ACCESS_KEY_ID=from-secrets\nVOLCENGINE_ACCESS_KEY_SECRET=from-secrets\n"), 0o600); err != nil {
		t.Fatalf("write secrets file: %v", err)
	}
	reqFile := filepath.Join(tmp, "req.json")
	if err := osWriteJSON(reqFile, map[string]any{}); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"--profile", "selected-profile",
		"--secrets-file", secretsFile,
		"tool", "exec", "project.describe-projects",
		"--input", "file://" + reqFile,
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected conflict failure stdout=%q stderr=%q", stdout.String(), stderr.String())
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
	errObj, ok := out["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %#v", out["error"])
	}
	if errObj["kind"] != "validation" {
		t.Fatalf("expected validation kind, got %#v", errObj)
	}
	if !strings.Contains(asStringOrEmpty(errObj["message"]), "conflicting runtime selectors") {
		t.Fatalf("unexpected error message: %#v", errObj["message"])
	}
}
