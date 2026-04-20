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

func TestLogExport_OutputModeFileWritesBatchesToJSONL(t *testing.T) {
	var requests int
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/SearchLogs" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		requests++
		w.Header().Set("Content-Type", "application/json")
		if requests == 1 {
			_, _ = w.Write([]byte(`{
  "Logs": [{"k":"a"},{"k":"b"}],
  "ListOver": false,
  "Context": "ctx-2"
}`))
			return
		}
		_, _ = w.Write([]byte(`{
  "Logs": [{"k":"c"}],
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

	outFile := filepath.Join(t.TempDir(), "export.jsonl")
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"--output", "jsonl",
		"--output-mode", "file",
		"--output-file", outFile,
		"log", "export",
		"--topic-id", "tid",
		"--query", "*",
		"--from", "1710374400000",
		"--to", "1710378000000",
		"--max-pages", "5",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var env map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("invalid stdout json: %v; %q", err, stdout.String())
	}
	if env["status"] != "success" || env["action"] != "log.export" {
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
	if len(lines) != 3 {
		t.Fatalf("expected 3 jsonl rows, got %d: %q", len(lines), string(data))
	}
	if lines[0] != `{"k":"a"}` || lines[1] != `{"k":"b"}` || lines[2] != `{"k":"c"}` {
		t.Fatalf("unexpected file content: %q", string(data))
	}
	if requests != 2 {
		t.Fatalf("expected 2 paged requests, got %d", requests)
	}
}

func TestLogExport_FileModeAvoidsReturningInMemoryRows(t *testing.T) {
	var requests int
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/SearchLogs" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		requests++
		w.Header().Set("Content-Type", "application/json")
		if requests == 1 {
			_, _ = w.Write([]byte(`{"Logs":[{"k":"a"}],"ListOver":false,"Context":"ctx-2"}`))
			return
		}
		_, _ = w.Write([]byte(`{"Logs":[{"k":"b"}],"ListOver":true,"Context":""}`))
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
	ctx.OutputFile = filepath.Join(t.TempDir(), "stream.jsonl")

	out, err := logExport(ctx, []string{
		"--topic-id", "tid",
		"--query", "*",
		"--from", "1710374400000",
		"--to", "1710378000000",
		"--max-pages", "5",
	})
	if err != nil {
		t.Fatalf("logExport error: %v", err)
	}
	if _, ok := out.([]any); ok {
		t.Fatalf("expected file-mode export to avoid returning in-memory rows")
	}
	if _, err := os.Stat(ctx.OutputFile); err != nil {
		t.Fatalf("expected output file to be written: %v", err)
	}
}
