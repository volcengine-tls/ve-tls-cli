package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLogExportAnalysis_DefaultOutputIsJSONLRows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/SearchLogs" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "Analysis": true,
  "AnalysisResult": {
    "Schema": ["k","v"],
    "Data": [{"k":"a","v":1},{"k":"b","v":2}],
    "Type": {}
  },
  "ListOver": true,
  "Context": ""
}`))
	}))
	defer srv.Close()

	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", srv.URL)
	t.Setenv("VOLCLOG_CONFIG", t.TempDir()+"/config.json")

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"log", "export-analysis",
		"--topic-id", "tid",
		"--query", "*|select 1",
		"--from", "1710374400000",
		"--to", "1710378000000",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v; %q", err, stdout.String())
	}
	if out["status"] != "success" {
		t.Fatalf("unexpected status: %v", out["status"])
	}
	if out["action"] != "log.export-analysis" {
		t.Fatalf("unexpected action: %v", out["action"])
	}
	rows, ok := out["data"].([]any)
	if !ok || len(rows) != 2 {
		t.Fatalf("expected 2 rows in data: %v", out["data"])
	}
	r1, ok := rows[0].(map[string]any)
	if !ok {
		t.Fatalf("invalid row1: %v", rows[0])
	}
	r2, ok := rows[1].(map[string]any)
	if !ok {
		t.Fatalf("invalid row2: %v", rows[1])
	}
	if r1["k"] != "a" || r1["v"] != float64(1) {
		t.Fatalf("unexpected row1: %v", r1)
	}
	if r2["k"] != "b" || r2["v"] != float64(2) {
		t.Fatalf("unexpected row2: %v", r2)
	}
}

func TestLogExportAnalysis_RespectsExplicitOutputJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/SearchLogs" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "Analysis": true,
  "AnalysisResult": {
    "Schema": ["k"],
    "Data": [{"k":"a"}],
    "Type": {}
  },
  "ListOver": true,
  "Context": ""
}`))
	}))
	defer srv.Close()

	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", srv.URL)
	t.Setenv("VOLCLOG_CONFIG", t.TempDir()+"/config.json")

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"--output", "json",
		"log", "export-analysis",
		"--topic-id", "tid",
		"--query", "*|select 1",
		"--from", "1710374400000",
		"--to", "1710378000000",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid json: %v; %q", err, stdout.String())
	}
	rows, ok := out["data"].([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("unexpected output: %v", out)
	}
	row, ok := rows[0].(map[string]any)
	if !ok || row["k"] != "a" {
		t.Fatalf("unexpected row: %v", rows[0])
	}
}

func TestLogExportAnalysis_RejectsArrayRows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/SearchLogs" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "Analysis": true,
  "AnalysisResult": {
    "Schema": ["k","v"],
    "Data": [["a", 1]],
    "Type": {}
  },
  "ListOver": true,
  "Context": ""
}`))
	}))
	defer srv.Close()

	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", srv.URL)
	t.Setenv("VOLCLOG_CONFIG", t.TempDir()+"/config.json")

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"log", "export-analysis",
		"--topic-id", "tid",
		"--query", "*|select 1",
		"--from", "1710374400000",
		"--to", "1710378000000",
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected failure, got exit=0 stdout=%q", stdout.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v; %q", err, stdout.String())
	}
	errObj, ok := out["error"].(map[string]any)
	if !ok || !strings.Contains(toString(errObj["errorMessage"]), "invalid AnalysisResult.Data row") {
		t.Fatalf("unexpected error object: %v", out["error"])
	}
}

func TestLogExportAnalysis_RejectsMaxPages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"Analysis":true,"AnalysisResult":{"Schema":["k"],"Type":{},"Data":[{"k":"a"}]},"ListOver":true,"Context":""}`))
	}))
	defer srv.Close()

	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", srv.URL)
	t.Setenv("VOLCLOG_CONFIG", t.TempDir()+"/config.json")

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"log", "export-analysis",
		"--topic-id", "tid",
		"--query", "*|select 1",
		"--from", "1710374400000",
		"--to", "1710378000000",
		"--max-pages", "2",
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected failure, got exit=0 stdout=%q", stdout.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v; %q", err, stdout.String())
	}
	errObj, ok := out["error"].(map[string]any)
	if !ok || !strings.Contains(toString(errObj["errorMessage"]), "--max-pages is not supported") {
		t.Fatalf("unexpected error object: %v", out["error"])
	}
}

func TestLogExportAnalysisDescribeMentionsIndexIncrementalEffect(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"log", "export-analysis", "--describe"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "索引配置") || !strings.Contains(out, "旧日志") || !strings.Contains(out, "null") {
		t.Fatalf("describe should mention index incremental effect: %q", out)
	}
}
