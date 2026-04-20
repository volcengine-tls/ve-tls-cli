package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
		t.Fatalf("expected unsupported page.all failure stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v stdout=%q", err, stdout.String())
	}
	errObj, ok := out["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object in stdout envelope, got %#v", out["error"])
	}
	if got := errObj["kind"]; got != "unsupported_feature" {
		t.Fatalf("expected unsupported_feature kind, got %#v", got)
	}
	if msg, _ := errObj["message"].(string); !strings.Contains(msg, "page.all") {
		t.Fatalf("expected page.all error in stdout envelope, got %#v", errObj)
	}
}

func TestToolExecPageAllFlagRejectsPageNumberConflictInDryRun(t *testing.T) {
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
			"PageNumber": 2,
		},
	}); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"tool", "exec", "topic.describe-topics", "--context", "file://" + ctxFile, "--input", "file://" + reqFile, "--page-all"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected page_all conflict failure stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v stdout=%q", err, stdout.String())
	}
	errObj, ok := out["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %#v", out["error"])
	}
	if errObj["kind"] != "incompatible_flags" {
		t.Fatalf("expected incompatible_flags kind, got %#v", errObj["kind"])
	}
	if !strings.Contains(asStringOrEmpty(errObj["message"]), "PageNumber") {
		t.Fatalf("expected PageNumber conflict message, got %#v", errObj["message"])
	}
}

func TestToolExecPageAllUsesLargeDefaultPageSizeWhenUnset(t *testing.T) {
	var firstPageSize string
	var firstPageNumber string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstPageSize = r.URL.Query().Get("PageSize")
		firstPageNumber = r.URL.Query().Get("PageNumber")
		w.Header().Set("x-tls-requestid", "req-topics-page1")
		_, _ = w.Write([]byte(`{"Topics":[],"Total":0}`))
	}))
	defer srv.Close()

	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", srv.URL)
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	tmp := t.TempDir()
	ctxFile := filepath.Join(tmp, "ctx.json")
	reqFile := filepath.Join(tmp, "req.json")
	if err := osWriteJSON(ctxFile, map[string]any{"region": "cn-beijing"}); err != nil {
		t.Fatalf("write context: %v", err)
	}
	if err := osWriteJSON(reqFile, map[string]any{"query": map[string]any{}}); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"tool", "exec", "topic.describe-topics", "--context", "file://" + ctxFile, "--input", "file://" + reqFile, "--page-all"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if firstPageSize != "100" {
		t.Fatalf("expected default page_all PageSize=100, got %q", firstPageSize)
	}
	if firstPageNumber != "1" {
		t.Fatalf("expected first page number 1, got %q", firstPageNumber)
	}
}

func TestToolExecPageAllSummaryIncludesPaginationMetadata(t *testing.T) {
	var seenPageNumbers []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("PageNumber")
		seenPageNumbers = append(seenPageNumbers, page)
		w.Header().Set("x-tls-requestid", "req-topics-"+page)
		switch page {
		case "1":
			_, _ = w.Write([]byte(`{"Topics":[{"TopicId":"t1"},{"TopicId":"t2"},{"TopicId":"t3"}],"Total":9}`))
		case "2":
			_, _ = w.Write([]byte(`{"Topics":[{"TopicId":"t4"},{"TopicId":"t5"},{"TopicId":"t6"}],"Total":9}`))
		case "3":
			_, _ = w.Write([]byte(`{"Topics":[{"TopicId":"t7"},{"TopicId":"t8"},{"TopicId":"t9"}],"Total":9}`))
		default:
			t.Fatalf("unexpected page number: %q", page)
		}
	}))
	defer srv.Close()

	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", srv.URL)
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	tmp := t.TempDir()
	ctxFile := filepath.Join(tmp, "ctx.json")
	reqFile := filepath.Join(tmp, "req.json")
	if err := osWriteJSON(ctxFile, map[string]any{"region": "cn-beijing"}); err != nil {
		t.Fatalf("write context: %v", err)
	}
	if err := osWriteJSON(reqFile, map[string]any{
		"query": map[string]any{
			"PageSize": 3,
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
		t.Fatalf("invalid stdout json: %v stdout=%q", err, stdout.String())
	}
	summary, ok := out["summary"].(map[string]any)
	if !ok {
		t.Fatalf("expected summary map, got %#v", out["summary"])
	}
	pagination, ok := summary["pagination"].(map[string]any)
	if !ok {
		t.Fatalf("expected pagination metadata, got %#v", summary["pagination"])
	}
	if pagination["pageCount"] != float64(3) {
		t.Fatalf("expected pageCount=3, got %#v", pagination["pageCount"])
	}
	if pagination["pageSize"] != float64(3) {
		t.Fatalf("expected pageSize=3, got %#v", pagination["pageSize"])
	}
	if pagination["merged"] != true {
		t.Fatalf("expected merged=true, got %#v", pagination["merged"])
	}
}

func TestToolExecHelpMentionsPageAllFlag(t *testing.T) {
	got := usageToolExec()
	if !strings.Contains(got, "--page-all") {
		t.Fatalf("expected usage to mention --page-all, got: %s", got)
	}
}
