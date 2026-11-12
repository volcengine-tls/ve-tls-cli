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

// c4InvocationFailure checks the edition-specific channel and shape. The
// frozen argument is explicit at call sites for interactive auth commands.
func c4InvocationFailure(t *testing.T, stdout, stderr string, frozen bool) errPayload {
	t.Helper()
	if currentEdition() == cliEditionVolclog && !frozen {
		if stderr != "" {
			t.Fatalf("expected empty stderr, got %q", stderr)
		}
		var envelope struct {
			Status    string            `json:"status"`
			Action    string            `json:"action"`
			Artifacts []json.RawMessage `json:"artifacts"`
			Data      json.RawMessage   `json:"data"`
			Error     *struct {
				Source  string `json:"source"`
				Code    string `json:"code"`
				Message string `json:"message"`
				Kind    string `json:"kind"`
				Hint    string `json:"hint"`
			} `json:"error"`
			Summary *struct {
				RemoteOutcome   string `json:"remoteOutcome"`
				DeliveryOutcome string `json:"deliveryOutcome"`
			} `json:"summary"`
		}
		if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
			t.Fatalf("invalid stdout failure envelope: %v, stdout=%q", err, stdout)
		}
		if envelope.Status != "failed" || envelope.Action == "" || envelope.Artifacts == nil || len(envelope.Data) == 0 ||
			envelope.Error == nil || envelope.Error.Source != "cli" || envelope.Error.Code != "CLIError" ||
			envelope.Error.Message == "" || envelope.Error.Kind == "" || envelope.Summary == nil ||
			envelope.Summary.RemoteOutcome != "not_sent" || envelope.Summary.DeliveryOutcome != "success" {
			t.Fatalf("invalid stdout failure envelope: %q", stdout)
		}
		return errPayload{ErrorCode: envelope.Error.Code, ErrorMessage: envelope.Error.Message, Kind: envelope.Error.Kind, Hint: envelope.Error.Hint}
	}
	if stdout != "" {
		t.Fatalf("expected empty stdout, got %q", stdout)
	}
	var payload errPayload
	if err := json.Unmarshal([]byte(stderr), &payload); err != nil {
		t.Fatalf("invalid stderr error: %v, stderr=%q", err, stderr)
	}
	if payload.ErrorCode != "CLIError" || payload.ErrorMessage == "" || payload.Kind == "" {
		t.Fatalf("invalid stderr error: %q", stderr)
	}
	return payload
}

func TestRootHelpShowsPageAllExample(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--help"}, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	for _, want := range []string{
		"支持全量分页的操作",
		"volclog tool exec project.describe-projects --page-all",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("root help missing %q: %q", want, stdout.String())
		}
	}
}

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
			payload := c4InvocationFailure(t, stdout.String(), stderr.String(), false)
			if payload.Kind != "usage" || !strings.Contains(payload.ErrorMessage, "removed") {
				t.Fatalf("unexpected removed command error: %#v", payload)
			}
			for _, want := range tc.wantHints {
				if !strings.Contains(payload.Hint, want) {
					t.Fatalf("missing %q in hint: %q", want, payload.Hint)
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
