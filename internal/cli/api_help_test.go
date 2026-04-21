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

func TestUsageRawDescribesTransportOnlySurface(t *testing.T) {
	text := usageRaw()
	for _, want := range []string{
		"volclog raw --method <GET|POST|PUT|DELETE> --path <path>",
		"原始 transport 调用入口",
		"--method <GET|POST|PUT|DELETE>",
		"--path <path>",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in raw usage: %s", want, text)
		}
	}
	for _, notWant := range []string{
		"tool list",
		"tool describe",
		"api <group>",
		"capabilities",
	} {
		if strings.Contains(text, notWant) {
			t.Fatalf("raw usage should stay transport-only and hide %q: %s", notWant, text)
		}
	}
}

func TestRawHelpStaysTransportOnly(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"raw", "-h"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"volclog raw --method <GET|POST|PUT|DELETE> --path <path>",
		"原始 transport 调用入口",
		"--query k=v",
		"--header k=v",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in raw help: %q", want, out)
		}
	}
	for _, notWant := range []string{
		"tool list",
		"tool describe",
		"api <group>",
		"capabilities",
	} {
		if strings.Contains(out, notWant) {
			t.Fatalf("raw help should stay transport-only and hide %q: %q", notWant, out)
		}
	}
}

func TestRawUsageInDefaultVolclogUsesReadonlyGuidance(t *testing.T) {
	text := usageRaw()
	for _, want := range []string{
		"默认 volclog 面向只读 agent surface",
		"/SearchLogs",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in raw usage: %q", want, text)
		}
	}
	for _, notWant := range []string{
		"/CreateProject",
		"/CreateTopic",
	} {
		if strings.Contains(text, notWant) {
			t.Fatalf("raw usage should avoid mutating examples %q: %q", notWant, text)
		}
	}
}

func TestLegacyCapabilitiesAndAPIAreRemoved(t *testing.T) {
	cases := []struct {
		name      string
		args      []string
		wantHints []string
	}{
		{
			name:      "capabilities",
			args:      []string{"capabilities", "--group", "log", "--action", "SearchLogs"},
			wantHints: []string{"volclog tool list log", "volclog tool describe log.search"},
		},
		{
			name:      "api group",
			args:      []string{"api", "project"},
			wantHints: []string{"volclog tool list project", "volclog raw --method"},
		},
		{
			name:      "api action",
			args:      []string{"api", "log", "SearchLogs", "--describe"},
			wantHints: []string{"volclog tool list log", "volclog tool describe log.search"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(tc.args, &stdout, &stderr)
			if code != 1 {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if strings.TrimSpace(stdout.String()) != "" {
				t.Fatalf("expected empty stdout, got %q", stdout.String())
			}
			text := stderr.String()
			for _, want := range append([]string{`"errorCode": "CLIError"`, `"kind": "usage"`, "removed"}, tc.wantHints...) {
				if !strings.Contains(text, want) {
					t.Fatalf("missing %q in stderr: %q", want, text)
				}
			}
		})
	}
}

func TestRawCommandRemainsAvailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/DescribeProjects" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("x-tls-requestid", "req-raw")
		_, _ = w.Write([]byte(`{"Projects":[],"Total":0}`))
	}))
	defer srv.Close()

	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", srv.URL)
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	var stdout, stderr bytes.Buffer
	code := Run([]string{"raw", "--method", "GET", "--path", "/DescribeProjects", "--jmes-filter", "data.Total"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}

	var out float64
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v stdout=%q", err, stdout.String())
	}
	if out != 0 {
		t.Fatalf("expected filtered envelope field 0, got %#v", out)
	}
}

func TestRawRejectsMutatingPathInDefaultVolclog(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"raw", "--method", "POST", "--path", "/CreateTopic", "--body", `{"TopicName":"demo"}`}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected mutating raw path to be rejected stdout=%q stderr=%q", stdout.String(), stderr.String())
	}

	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v stdout=%q", err, stdout.String())
	}
	errObj, ok := out["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %#v", out["error"])
	}
	if !strings.Contains(asStringOrEmpty(errObj["message"]), "readonly edition") {
		t.Fatalf("unexpected error message: %#v", errObj["message"])
	}
	if !strings.Contains(asStringOrEmpty(errObj["hint"]), "volclog-human") {
		t.Fatalf("expected volclog-human hint, got %#v", errObj["hint"])
	}
}
