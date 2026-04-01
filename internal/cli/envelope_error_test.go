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

func TestAPIErrorIsWrappedInEnvelope(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"api", "project", "CreateProject", "--request", "file://req.json"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected non-zero exit code")
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v; stdout=%q", err, stdout.String())
	}
	if out["status"] != "failed" {
		t.Fatalf("unexpected status: %v", out["status"])
	}
	action, _ := out["action"].(string)
	if strings.TrimSpace(action) == "" {
		t.Fatalf("missing action: %v", out["action"])
	}
	if _, ok := out["error"].(map[string]any); !ok {
		t.Fatalf("missing error: %v", out["error"])
	}
}

func TestAPIServerErrorIsWrappedInEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-tls-requestid", "req-123")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"ErrorCode":"TopicAlreadyExist","ErrorMessage":"Topic already exists"}`))
	}))
	defer srv.Close()

	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", srv.URL)
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	var stdout, stderr bytes.Buffer
	code := Run([]string{"api", "call", "--method", "POST", "--path", "/CreateTopic", "--body", `{"TopicName":"demo"}`}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("unexpected exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v; stdout=%q", err, stdout.String())
	}
	if out["status"] != "failed" {
		t.Fatalf("unexpected status: %v", out["status"])
	}
	if out["requestId"] != "req-123" {
		t.Fatalf("unexpected requestId: %v", out["requestId"])
	}
	errObj, ok := out["error"].(map[string]any)
	if !ok {
		t.Fatalf("missing error object: %v", out["error"])
	}
	if errObj["statusCode"] != float64(http.StatusConflict) {
		t.Fatalf("unexpected statusCode: %v", errObj["statusCode"])
	}
	if errObj["kind"] != "server" {
		t.Fatalf("unexpected kind: %v", errObj["kind"])
	}
}
