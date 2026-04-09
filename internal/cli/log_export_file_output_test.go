package cli

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/output"
)

func TestLogExportAnalysis_OutputModeFileWritesRowsToJSONL(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	})
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("listen tcp4 not permitted in this environment: %v", err)
	}
	srv := &http.Server{Handler: handler}
	go func() {
		_ = srv.Serve(ln)
	}()
	defer srv.Close()

	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "http://"+ln.Addr().String())
	t.Setenv("VOLCLOG_CONFIG", t.TempDir()+"/config.json")

	outFile := filepath.Join(t.TempDir(), "analysis.jsonl")
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"--output-mode", "file",
		"--output-file", outFile,
		"log", "export-analysis",
		"--topic-id", "tid",
		"--query", "*|select 1",
		"--from", "1710374400000",
		"--to", "1710378000000",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var env map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("invalid stdout json: %v; %q", err, stdout.String())
	}
	if env["status"] != "success" || env["action"] != "log.export-analysis" {
		t.Fatalf("unexpected envelope: %v", env)
	}
	artifacts, ok := env["artifacts"].([]any)
	if !ok || len(artifacts) != 1 {
		t.Fatalf("unexpected artifacts: %v", env["artifacts"])
	}
	artifact, ok := artifacts[0].(map[string]any)
	if !ok {
		t.Fatalf("invalid artifact: %v", artifacts[0])
	}
	if artifact["path"] != outFile {
		t.Fatalf("unexpected path: %v", artifact["path"])
	}
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 jsonl rows, got %d: %q", len(lines), string(data))
	}
	if lines[0] != `{"k":"a","v":1}` || lines[1] != `{"k":"b","v":2}` {
		t.Fatalf("unexpected file content: %q", string(data))
	}
}

func TestLogExportAnalysis_FileModeAvoidsReturningInMemoryRows(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/SearchLogs" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "Analysis": true,
  "AnalysisResult": {
    "Schema": ["k"],
    "Data": [{"k":"a"},{"k":"b"}],
    "Type": {}
  },
  "ListOver": true,
  "Context": ""
}`))
	})
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("listen tcp4 not permitted in this environment: %v", err)
	}
	srv := &http.Server{Handler: handler}
	go func() {
		_ = srv.Serve(ln)
	}()
	defer srv.Close()

	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "http://"+ln.Addr().String())
	t.Setenv("VOLCLOG_CONFIG", t.TempDir()+"/config.json")

	var stdout, stderr bytes.Buffer
	ctx := newContext(&stdout, &stderr, output.FormatJSONL, "", "")
	ctx.OutputMode = "file"
	ctx.OutputFile = filepath.Join(t.TempDir(), "analysis.jsonl")

	out, err := logExportAnalysis(ctx, []string{
		"--topic-id", "tid",
		"--query", "*|select 1",
		"--from", "1710374400000",
		"--to", "1710378000000",
	})
	if err != nil {
		t.Fatalf("logExportAnalysis error: %v", err)
	}
	if _, ok := out.([]map[string]any); ok {
		t.Fatalf("expected file-mode export-analysis to avoid returning in-memory rows")
	}
	if _, err := os.Stat(ctx.OutputFile); err != nil {
		t.Fatalf("expected output file to be written: %v", err)
	}
}
