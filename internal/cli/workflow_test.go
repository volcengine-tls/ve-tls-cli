package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestWorkflowListSupportsJSONFormat(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"workflow", "list", "--format", "json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v", err)
	}
	groups, ok := out["groups"].([]any)
	if !ok || len(groups) != 1 {
		t.Fatalf("expected single workflow group, got %#v", out["groups"])
	}
	item, ok := groups[0].(map[string]any)
	if !ok {
		t.Fatalf("expected group object, got %#v", groups[0])
	}
	if item["group"] != "log" || item["count"] != float64(3) {
		t.Fatalf("unexpected workflow group summary: %#v", item)
	}
}

func TestWorkflowListByGroupReturnsRunnableIdentities(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"workflow", "list", "log", "--format", "json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v", err)
	}
	items, ok := out["workflows"].([]any)
	if !ok {
		t.Fatalf("expected workflows array, got %#v", out["workflows"])
	}
	got := make([]string, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("expected workflow object, got %#v", item)
		}
		id, _ := m["id"].(string)
		got = append(got, id)
	}
	want := []string{"log.export", "log.export-analysis", "log.ingest"}
	if len(got) != len(want) {
		t.Fatalf("unexpected workflow count: got=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected workflows: got=%v want=%v", got, want)
		}
	}
}

func TestWorkflowDescribeReturnsWorkflowContract(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--output", "json", "workflow", "describe", "log.export"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v", err)
	}
	if out["kind"] != "workflow" {
		t.Fatalf("expected workflow kind, got %#v", out["kind"])
	}
	if out["source"] != "cli_workflow" {
		t.Fatalf("expected cli_workflow source, got %#v", out["source"])
	}
	backedBy, ok := out["backed_by"].([]any)
	if !ok || len(backedBy) == 0 || backedBy[0] != "SearchLogs" {
		t.Fatalf("expected SearchLogs backing, got %#v", out["backed_by"])
	}
	if out["preferred_output_mode"] != "file" {
		t.Fatalf("expected preferred output mode file, got %#v", out["preferred_output_mode"])
	}
	inputSchema, ok := out["input_schema"].(map[string]any)
	if !ok {
		t.Fatalf("expected input_schema object, got %#v", out["input_schema"])
	}
	props, ok := inputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected input_schema.properties, got %#v", inputSchema)
	}
	for _, key := range []string{"TopicId", "Query", "StartTime", "EndTime", "MaxPages"} {
		if _, ok := props[key]; !ok {
			t.Fatalf("expected workflow input schema to contain %q, got %#v", key, props)
		}
	}
}

func TestWorkflowIdsDoNotLeakIntoToolList(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"tool", "list", "log", "--format", "json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v", err)
	}
	items, ok := out["tools"].([]any)
	if !ok {
		t.Fatalf("expected tools array, got %#v", out["tools"])
	}
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("expected tool object, got %#v", item)
		}
		id, _ := m["id"].(string)
		if id == "log.ingest" || id == "log.export" || id == "log.export-analysis" {
			t.Fatalf("workflow id leaked into tool list: %q", id)
		}
	}
}

func TestWorkflowExecLogExportAcceptsDryRunAndJSONInput(t *testing.T) {
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
	}); err != nil {
		t.Fatalf("write context: %v", err)
	}
	if err := osWriteJSON(reqFile, map[string]any{
		"TopicId":   "tid",
		"Query":     "*",
		"StartTime": 1710374400000,
		"EndTime":   1710378000000,
		"MaxPages":  2,
	}); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--dry-run", "workflow", "exec", "log.export", "--context", "file://" + ctxFile, "--input", "file://" + reqFile}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v", err)
	}
	if out["action"] != "workflow.log.export" {
		t.Fatalf("unexpected action: %#v", out["action"])
	}
	summary, ok := out["summary"].(map[string]any)
	if !ok || summary["dryRun"] != true {
		t.Fatalf("expected dryRun summary, got %#v", out["summary"])
	}
}

func TestWorkflowExecLogIngestRoutesToExistingRuntime(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	tmp := t.TempDir()
	inputPath := filepath.Join(tmp, "input.jsonl")
	if err := osWriteJSON(inputPath, []map[string]any{
		{"message": "hello", "time": 1710374400000},
	}); err != nil {
		t.Fatalf("write ingest input: %v", err)
	}
	reqFile := filepath.Join(tmp, "req.json")
	if err := osWriteJSON(reqFile, map[string]any{
		"TopicId":     "tid",
		"Input":       "file://" + inputPath,
		"InputFormat": "json-array",
		"TimeField":   "time",
	}); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--dry-run", "workflow", "exec", "log.ingest", "--input", "file://" + reqFile}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v", err)
	}
	if out["action"] != "workflow.log.ingest" {
		t.Fatalf("unexpected action: %#v", out["action"])
	}
	summary, ok := out["summary"].(map[string]any)
	if !ok || summary["dryRun"] != true {
		t.Fatalf("expected dryRun summary, got %#v", out["summary"])
	}
}

func TestWorkflowExecAcceptsInlineJSONInput(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	inline := `{"TopicId":"tid","Query":"*","StartTime":1710374400000,"EndTime":1710378000000,"MaxPages":1}`
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--dry-run", "workflow", "exec", "log.export", "--input", inline}, &stdout, &stderr)
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
