//go:build human

package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestCLIMinimalE2E_HTTPPathsAndShapes(t *testing.T) {
	var mu sync.Mutex
	var contexts = map[string]int{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Tls-Apiversion") != "0.3.0" {
			t.Fatalf("missing/invalid x-tls-apiversion: %q", r.Header.Get("X-Tls-Apiversion"))
		}
		if r.Header.Get("Authorization") == "" {
			t.Fatalf("missing Authorization header")
		}
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/DescribeProjects":
			_, _ = w.Write([]byte(`{"Projects":[],"Total":0}`))
		case r.Method == http.MethodGet && r.URL.Path == "/DescribeTopics":
			_, _ = w.Write([]byte(`{"Topics":[],"Total":0}`))
		case r.Method == http.MethodGet && r.URL.Path == "/DescribeMetricTopics":
			_, _ = w.Write([]byte(`{"Topics":[],"Total":0}`))
		case r.Method == http.MethodPost && r.URL.Path == "/CreateIndex":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if strings.TrimSpace(getString(body, "TopicId")) == "" {
				t.Fatalf("CreateIndex missing TopicId in body: %#v", body)
			}
			_, _ = w.Write([]byte(`{"TopicId":"` + getString(body, "TopicId") + `"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/SearchLogs":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if strings.TrimSpace(getString(body, "TopicId")) == "" {
				t.Fatalf("SearchLogs missing TopicId: %#v", body)
			}
			if strings.TrimSpace(getString(body, "Query")) == "" {
				t.Fatalf("SearchLogs missing Query: %#v", body)
			}
			if _, ok := body["StartTime"]; !ok {
				t.Fatalf("SearchLogs missing StartTime: %#v", body)
			}
			if _, ok := body["EndTime"]; !ok {
				t.Fatalf("SearchLogs missing EndTime: %#v", body)
			}
			ctx := strings.TrimSpace(getString(body, "Context"))
			mu.Lock()
			contexts[ctx]++
			mu.Unlock()
			if ctx == "" {
				_, _ = w.Write([]byte(`{"Logs":[{"x":1}],"ListOver":false,"Context":"c1"}`))
				return
			}
			_, _ = w.Write([]byte(`{"Logs":[{"x":2}],"ListOver":true,"Context":""}`))
		case r.Method == http.MethodPost && r.URL.Path == "/DescribeAppInstances":
			_, _ = w.Write([]byte(`{"InstanceInfo":[]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/CreateAppInstance":
			_, _ = w.Write([]byte(`{"InstanceID":"ai1"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/CreateAppSceneMeta":
			_, _ = w.Write([]byte(`{"Id":"s1"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/DescribeSessionAnswer":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: " + `{"RspMsgType":"inference","ModelAnswer":{"Answer":"hello "}}` + "\n\n"))
			_, _ = w.Write([]byte("data: " + `{"RspMsgType":"inference","ModelAnswer":{"Answer":"world"}}` + "\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/topic/") && strings.HasSuffix(r.URL.Path, "/api/v1/query"):
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	if err := os.WriteFile(cfgPath, []byte(`{"version":1,"profiles":{}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	indexBodyPath := filepath.Join(tmp, "index.json")
	if err := os.WriteFile(indexBodyPath, []byte(`{"EnableAutoIndex":true}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("VOLCLOG_CONFIG", cfgPath)
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", srv.URL)

	cases := []struct {
		name string
		args []string
	}{
		{name: "project list", args: []string{"project", "list"}},
		{name: "topic list", args: []string{"topic", "list", "--project-id", "pid"}},
		{name: "metric-topic list", args: []string{"metric-topic", "list", "--project-id", "pid"}},
		{name: "index create", args: []string{"index", "create", "--topic-id", "tid", "--body", "file://" + indexBodyPath}},
		{name: "log search", args: []string{"log", "search", "--topic-id", "tid", "--query", "*", "--from", "1710374400000", "--to", "1710378000000"}},
		{name: "log export", args: []string{"--output", "jsonl", "log", "export", "--topic-id", "tid", "--query", "*", "--from", "1710374400000", "--to", "1710378000000", "--max-pages", "2"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(tc.args, &stdout, &stderr)
			if code != 0 {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func getString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
