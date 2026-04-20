package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestToolExecAllowsMissingInputForEmptyInputSchema(t *testing.T) {
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

	var stdout, stderr bytes.Buffer
	code := Run([]string{"tool", "exec", "account.get", "--context", "file://" + ctxFile}, &stdout, &stderr)
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

func TestToolExecMissingInputStillReportsContractMissingField(t *testing.T) {
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

	var stdout, stderr bytes.Buffer
	code := Run([]string{"tool", "exec", "topic.create", "--context", "file://" + ctxFile}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected missing required field failure stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "missing required fields:") {
		t.Fatalf("expected aggregated missing fields in stdout envelope, got %q", out)
	}
	for _, want := range []string{"input.body.ProjectId", "input.body.TopicName"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected contract field path %q in stdout envelope, got %q", want, out)
		}
	}
}
