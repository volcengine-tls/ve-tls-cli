package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestToolExecPageAllFlagDryRunForSupportedAction(t *testing.T) {
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
			"dry_run": true,
		},
	}); err != nil {
		t.Fatalf("write context: %v", err)
	}
	if err := osWriteJSON(reqFile, map[string]any{
		"query": map[string]any{
			"PageSize": 2,
		},
	}); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"tool", "exec", "topic.describe-topics", "--context", "file://" + ctxFile, "--input", "file://" + reqFile, "--page-all"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v", err)
	}
	data, ok := out["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected envelope data map, got %#v", out["data"])
	}
	pageAll, ok := data["page_all"].(map[string]any)
	if !ok || pageAll["requested"] != true {
		t.Fatalf("expected dry-run page_all annotation, got %#v", data["page_all"])
	}
}

func TestToolExecPageAllFlagRejectsUnsupportedAction(t *testing.T) {
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
			"dry_run": true,
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
	code := Run([]string{"tool", "exec", "topic.create-topic", "--context", "file://" + ctxFile, "--input", "file://" + reqFile, "--page-all"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected unsupported page.all failure stdout=%q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "page.all") {
		t.Fatalf("expected page.all error in stderr, got %q", stderr.String())
	}
}

func TestToolExecHelpMentionsPageAllFlag(t *testing.T) {
	got := usageToolExec()
	if !strings.Contains(got, "--page-all") {
		t.Fatalf("expected usage to mention --page-all, got: %s", got)
	}
}
