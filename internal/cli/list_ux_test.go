//go:build !agent

package cli

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"testing"
)

func TestProjectListAllAggregatesPages(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/DescribeProjects" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		switch r.URL.Query().Get("PageNumber") {
		case "1":
			_, _ = w.Write([]byte(`{"Projects":[{"ProjectId":"p1","ProjectName":"alpha"},{"ProjectId":"p2","ProjectName":"beta"}],"Total":3}`))
		case "2":
			_, _ = w.Write([]byte(`{"Projects":[{"ProjectId":"p3","ProjectName":"gamma"}],"Total":3}`))
		default:
			t.Fatalf("unexpected page number: %q", r.URL.Query().Get("PageNumber"))
		}
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

	var stdout, stderr bytes.Buffer
	code := Run([]string{"project", "list", "--all", "--page-size", "2"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var env map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("invalid json: %v stdout=%s", err, stdout.String())
	}
	data, ok := env["data"].(map[string]any)
	if !ok {
		t.Fatalf("missing data: %v", env)
	}
	items, ok := data["Projects"].([]any)
	if !ok || len(items) != 3 {
		t.Fatalf("unexpected projects: %v", data["Projects"])
	}
	if total, _ := data["Total"].(float64); total != 3 {
		t.Fatalf("unexpected total: %v", data["Total"])
	}
}

func TestProjectListTableOutput(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"Projects":[{"ProjectId":"p1","ProjectName":"alpha","Region":"cn-beijing"}],"Total":1}`))
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

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--output", "table", "project", "list"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"ProjectId", "ProjectName", "Region", "p1", "alpha"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in table output: %q", want, out)
		}
	}
	if strings.Contains(out, `"status"`) {
		t.Fatalf("table output should not be envelope json: %q", out)
	}
}

func TestIndexGetTableOutput(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/DescribeIndex" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"TopicId":"tid","EnableAutoIndex":true,"MaxTextLen":2048}`))
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

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--output", "table", "index", "get", "--topic-id", "tid"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"TopicId", "EnableAutoIndex", "MaxTextLen", "tid", "2048"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in table output: %q", want, out)
		}
	}
}

func TestLogSearchTableOutput(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/SearchLogs" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"Logs":[{"level":"info","message":"started","service":"demo"}],"Count":1}`))
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

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--output", "table", "log", "search", "--topic-id", "tid", "--query", "*", "--from", "1710374400000", "--to", "1710378000000"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"level", "message", "service", "info", "started", "demo"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in table output: %q", want, out)
		}
	}
}

func TestIndexCreateSuggestsClosestBodyField(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"index", "create", "--topic-id", "tid", "--body", `{"EnableAutoIndexes":true}`}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "EnableAutoIndex") {
		t.Fatalf("expected suggestion in stdout: %s", stdout.String())
	}
}
