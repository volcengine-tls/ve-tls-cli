//go:build human

package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLogHistogramUsesDescribeHistogramV1Endpoint(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"TotalCount":1,"Histogram":[],"ResultStatus":"complete"}`))
	}))
	defer srv.Close()

	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", srv.URL)
	t.Setenv("VOLCLOG_CONFIG", t.TempDir()+"/config.json")

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"log", "histogram",
		"--topic-id", "tid",
		"--query", "*",
		"--from", "1710374400000",
		"--to", "1710378000000",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if gotPath != "/DescribeHistogramV1" {
		t.Fatalf("expected DescribeHistogramV1 path, got %q", gotPath)
	}

	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v; %q", err, stdout.String())
	}
	if out["status"] != "success" {
		t.Fatalf("unexpected status: %#v", out["status"])
	}
}
