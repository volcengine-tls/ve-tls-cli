package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/execution"
)

func TestC4FileDeliveryClonesEnvelopeAndRefreshesArtifact(t *testing.T) {
	if currentEdition() != cliEditionVolclog {
		t.Skip("default edition delivery envelope")
	}
	for _, tc := range []struct {
		group string
		rest  []string
		mode  string
		array bool
	}{
		{"raw", nil, "file_forced", false},
		{"tool", []string{"exec"}, "file_auto", true},
		{"workflow", []string{"exec"}, "file_forced", false},
	} {
		t.Run(tc.group+"/"+tc.mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "result.json")
			payload := []byte("full business result\n")
			if err := os.WriteFile(path, payload, 0o600); err != nil {
				t.Fatal(err)
			}
			descriptor := map[string]any{"path": path, "format": "json", "sizeBytes": 999, "metadataPath": "metadata.json", "completeness": map[string]any{"complete": true}}
			var artifacts any = []map[string]any{descriptor}
			if tc.array {
				artifacts = []any{descriptor}
			}
			summary := map[string]any{"deliveryMode": tc.mode, "itemCount": 7, "totalBytes": 999, "remoteOutcome": "success"}
			env := map[string]any{"status": "success", "action": "tool.example", "requestId": "req-1", "summary": summary, "artifacts": artifacts, "data": "secret business payload", "error": nil, "contract_digest": map[string]any{"actual": "digest"}, "warnings": []any{"warning"}}
			var stdout, stderr bytes.Buffer
			if code := renderFileDelivery(&Context{}, &stdout, &stderr, tc.group, tc.rest, env, 0); code != 0 || stderr.Len() != 0 {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			got := c4DeliveryDecode(t, stdout.Bytes())
			if got["status"] != "success" || got["action"] != env["action"] || got["requestId"] != env["requestId"] || got["data"] != nil || got["error"] != nil || got["contract_digest"] == nil || got["warnings"] == nil {
				t.Fatalf("delivery envelope lost metadata: %#v", got)
			}
			gotSummary := got["summary"].(map[string]any)
			if gotSummary["itemCount"] != float64(7) || gotSummary["totalBytes"] != float64(stdout.Len()) || gotSummary["deliveryMode"] != tc.mode {
				t.Fatalf("unexpected summary %#v, bytes=%d", gotSummary, stdout.Len())
			}
			gotArtifact := got["artifacts"].([]any)[0].(map[string]any)
			if gotArtifact["sizeBytes"] != float64(len(payload)) || gotArtifact["metadataPath"] != "metadata.json" || gotArtifact["completeness"] == nil {
				t.Fatalf("unexpected descriptor: %#v", gotArtifact)
			}
			if summary["totalBytes"] != 999 || descriptor["sizeBytes"] != 999 || env["data"] != "secret business payload" {
				t.Fatalf("caller envelope was mutated: %#v", env)
			}
		})
	}
}

func TestC4FileDeliveryHumanNotice(t *testing.T) {
	if currentEdition() != cliEditionVolclogHuman {
		t.Skip("human edition file notice")
	}
	for _, tc := range []struct{ mode, want string }{
		{"file_forced", "结果已写入文件。\n文件: /tmp/result.json\n"},
		{"file_auto", "结果过大，已写入文件。\n文件: /tmp/result.json\n"},
	} {
		env := map[string]any{"summary": map[string]any{"deliveryMode": tc.mode}, "artifacts": []map[string]any{{"path": "/tmp/result.json"}}}
		var stdout, stderr bytes.Buffer
		code := renderFileDelivery(&Context{}, &stdout, &stderr, "tool", []string{"exec"}, env, 0)
		if code != 0 || stdout.String() != tc.want || stderr.Len() != 0 {
			t.Fatalf("mode=%s exit=%d stdout=%q stderr=%q", tc.mode, code, stdout.String(), stderr.String())
		}
	}
}

type c4FileFailWriter struct {
	short  bool
	writes int
	bytes  bytes.Buffer
}

func (w *c4FileFailWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.short && len(p) > 1 {
		n := len(p) / 2
		_, _ = w.bytes.Write(p[:n])
		return n, nil
	}
	return 0, errors.New("stdout unavailable")
}

func TestC4FileDeliveryStdoutFailurePreservesOutcome(t *testing.T) {
	for _, short := range []bool{false, true} {
		t.Run(map[bool]string{false: "error", true: "short"}[short], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "artifact.json")
			payload := []byte("artifact payload")
			if err := os.WriteFile(path, payload, 0o600); err != nil {
				t.Fatal(err)
			}
			state := &execution.Completeness{Complete: true, StopReason: "end", PageCount: 2, ItemCount: 7}
			env := map[string]any{
				"status": "success", "action": "tool.example", "requestId": "req-1",
				"summary":   map[string]any{"deliveryMode": "file_forced", "remoteOutcome": "success", "target": map[string]any{"region": "r1"}, "progress": map[string]any{"sent": 3}, "completeness": state},
				"artifacts": []map[string]any{{"path": path, "sizeBytes": len(payload), "metadataPath": "meta"}}, "data": "DO_NOT_REPLAY_PAYLOAD", "error": nil,
			}
			stdout := &c4FileFailWriter{short: short}
			var stderr bytes.Buffer
			ctx := &Context{ResponseState: ResponseState{RequestID: "req-1", StatusCode: 200, Action: "tool.example", RemoteOutcome: "success", TargetSnapshot: map[string]any{"region": "r1"}, Progress: map[string]any{"sent": 3}, Completeness: state}}
			code := renderFileDelivery(ctx, stdout, &stderr, "tool", []string{"exec"}, env, 0)
			if code != 3 || stdout.writes != 1 || strings.Contains(stdout.bytes.String(), "DO_NOT_REPLAY_PAYLOAD") {
				t.Fatalf("exit=%d writes=%d partial stdout=%q", code, stdout.writes, stdout.bytes.String())
			}
			failed := c4DeliveryDecode(t, stderr.Bytes())
			summary := failed["summary"].(map[string]any)
			if failed["status"] != "failed" || failed["requestId"] != "req-1" || failed["error"].(map[string]any)["statusCode"] != float64(200) || summary["remoteOutcome"] != "success" || summary["deliveryOutcome"] != "failed" || summary["target"].(map[string]any)["region"] != "r1" || summary["progress"].(map[string]any)["sent"] != float64(3) || summary["completeness"].(map[string]any)["itemCount"] != float64(7) {
				t.Fatalf("failure lost outcome: %#v", failed)
			}
			artifact := failed["artifacts"].([]any)[0].(map[string]any)
			if artifact["path"] != path || artifact["metadataPath"] != "meta" || strings.Contains(stderr.String(), "DO_NOT_REPLAY_PAYLOAD") {
				t.Fatalf("failure lost artifact or repeated payload: %#v", failed)
			}
			if stored, err := os.ReadFile(path); err != nil || !bytes.Equal(stored, payload) {
				t.Fatalf("artifact changed: %q, %v", stored, err)
			}
		})
	}
}

func TestC4FileDeliveryRunStdoutFailureDoesNotReplay(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("x-tls-requestid", "req-file-failed-stdout")
		_, _ = w.Write([]byte(filterFileProjectsResponse))
	}))
	defer server.Close()
	setWorkflowAppTestEnv(t, server.URL)
	for _, filtered := range []bool{false, true} {
		for _, short := range []bool{false, true} {
			name := map[bool]string{false: "full", true: "filtered"}[filtered] + "/" + map[bool]string{false: "error", true: "short"}[short]
			t.Run(name, func(t *testing.T) {
				requests.Store(0)
				args := []string{"--output-mode", "file", "--output-dir", t.TempDir()}
				if filtered {
					args = append(args, "--jmes-filter", "data.Name")
				}
				args = append(args, "raw", "--method", "GET", "--path", "/DescribeProjects")
				stdout := &c4FileFailWriter{short: short}
				var stderr bytes.Buffer
				code := Run(args, stdout, &stderr)
				if code != 3 || stdout.writes != 1 || requests.Load() != 1 {
					t.Fatalf("exit=%d writes=%d requests=%d stderr=%q", code, stdout.writes, requests.Load(), stderr.String())
				}
				failed := c4DeliveryDecode(t, stderr.Bytes())
				summary := failed["summary"].(map[string]any)
				failure := failed["error"].(map[string]any)
				if failed["requestId"] != "req-file-failed-stdout" || failure["statusCode"] != float64(200) || failure["stage"] != "stdout" || summary["remoteOutcome"] != "success" || summary["deliveryOutcome"] != "failed" || summary["target"] == nil {
					t.Fatalf("lost remote result metadata: %#v", failed)
				}
				artifact := failed["artifacts"].([]any)[0].(map[string]any)
				stored, err := os.ReadFile(artifact["path"].(string))
				if err != nil || artifact["sizeBytes"] != float64(len(stored)) {
					t.Fatalf("artifact=%#v bytes=%d err=%v", artifact, len(stored), err)
				}
				if filtered && strings.TrimSpace(string(stored)) != `"north"` {
					t.Fatalf("filtered artifact changed: %q", stored)
				}
			})
		}
	}
}

func TestC4FileDeliveryRunPaths(t *testing.T) {
	if currentEdition() != cliEditionVolclog {
		t.Skip("default edition delivery envelope")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-tls-requestid", "req-file")
		_, _ = w.Write([]byte(filterFileProjectsResponse))
	}))
	defer server.Close()
	setWorkflowAppTestEnv(t, server.URL)
	previousLimit := toolExecAutoArtifactByteLimit
	toolExecAutoArtifactByteLimit = 1
	defer func() { toolExecAutoArtifactByteLimit = previousLimit }()
	for _, tc := range []struct {
		name, mode string
		args       []string
	}{
		{"raw forced", "file_forced", []string{"raw", "--method", "GET", "--path", "/DescribeProjects"}},
		{"tool forced", "file_forced", []string{"tool", "exec", "project.describe-projects", "--input", `{}`, "--context", `{"contract_digest":"deadbeef"}`}},
		{"workflow forced", "file_forced", []string{"--dry-run", "workflow", "exec", appResolveTopicIDsWorkflowID, "--input", `{"AppId":"app-1"}`}},
		{"tool auto", "file_auto", []string{"tool", "exec", "project.describe-projects", "--input", `{}`}},
		{"workflow auto", "file_auto", []string{"--dry-run", "workflow", "exec", appResolveTopicIDsWorkflowID, "--input", `{"AppId":"app-1"}`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"--output-dir", t.TempDir()}
			if tc.mode == "file_forced" {
				args = append(args, "--output-mode", "file")
			}
			args = append(args, tc.args...)
			var stdout, stderr bytes.Buffer
			if code := Run(args, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			env := c4DeliveryDecode(t, stdout.Bytes())
			stored := c4AssertFileDelivery(t, env, stdout.Len(), tc.mode)
			var fileEnv map[string]any
			if json.Unmarshal(stored, &fileEnv) != nil || fileEnv["data"] == nil {
				t.Fatalf("artifact missing full data: %q", stored)
			}
			if strings.HasPrefix(tc.name, "raw") || strings.HasPrefix(tc.name, "tool") {
				if env["summary"].(map[string]any)["itemCount"] != float64(2) {
					t.Fatalf("business item count lost: %#v", env)
				}
			}
			if tc.name == "tool forced" && (env["contract_digest"] == nil || env["warnings"] == nil) {
				t.Fatalf("tool metadata lost: %#v", env)
			}
		})
	}
}

func TestC4FilteredFileDeliveryRunValues(t *testing.T) {
	if currentEdition() != cliEditionVolclog {
		t.Skip("default edition delivery envelope")
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(filterFileProjectsResponse))
	}))
	defer server.Close()
	setWorkflowAppTestEnv(t, server.URL)
	for _, tc := range []struct{ name, expr, want string }{
		{"scalar", "data.Name", `"north"`},
		{"null", "data.NullValue", `null`},
		{"array", "data.Projects[].ProjectId", `["p1","p2"]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run([]string{"--output-mode", "file", "--output-dir", t.TempDir(), "--jmes-filter", tc.expr, "raw", "--method", "GET", "--path", "/DescribeProjects"}, &stdout, &stderr)
			if code != 0 || stderr.Len() != 0 {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			env := c4DeliveryDecode(t, stdout.Bytes())
			stored := c4AssertFileDelivery(t, env, stdout.Len(), "file_forced")
			var compact bytes.Buffer
			if err := json.Compact(&compact, stored); err != nil || compact.String() != tc.want {
				t.Fatalf("filtered artifact=%q want=%q err=%v", stored, tc.want, err)
			}
			if env["summary"].(map[string]any)["itemCount"] != float64(2) {
				t.Fatalf("business item count lost: %#v", env)
			}
		})
	}
	if requests.Load() != 3 {
		t.Fatalf("requests=%d, want 3", requests.Load())
	}
}

func TestC4FilteredFileEvaluationUsesOriginalMetadata(t *testing.T) {
	if currentEdition() != cliEditionVolclog {
		t.Skip("default edition delivery envelope")
	}
	path := filepath.Join(t.TempDir(), "filtered.json")
	ctx := &Context{InvocationOptions: InvocationOptions{Filter: "artifacts[0].sizeBytes", OutputMode: "file"}}
	env := map[string]any{"status": "success", "action": "raw.call", "requestId": "", "summary": map[string]any{"deliveryMode": "file_forced", "totalBytes": 0}, "artifacts": []map[string]any{{"path": path, "format": "json", "sizeBytes": 987}}, "data": map[string]any{"items": []any{1, 2}}, "error": nil}
	var stdout, stderr bytes.Buffer
	if code := renderFilteredEnvelopeFile(ctx, &stdout, &stderr, "raw", nil, env, 0); code != 0 || stderr.Len() != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	got := c4DeliveryDecode(t, stdout.Bytes())
	stored := c4AssertFileDelivery(t, got, stdout.Len(), "file_forced")
	if string(stored) != "987\n" {
		t.Fatalf("filter evaluated after descriptor refresh: %q", stored)
	}
}

func c4DeliveryDecode(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("invalid envelope %q: %v", raw, err)
	}
	return env
}

func c4AssertFileDelivery(t *testing.T, env map[string]any, stdoutBytes int, mode string) []byte {
	t.Helper()
	summary, ok := env["summary"].(map[string]any)
	if !ok || env["status"] != "success" || env["data"] != nil || env["error"] != nil || summary["deliveryMode"] != mode || summary["totalBytes"] != float64(stdoutBytes) {
		t.Fatalf("unexpected delivery envelope: %#v stdout bytes=%d", env, stdoutBytes)
	}
	artifacts, ok := env["artifacts"].([]any)
	if !ok || len(artifacts) == 0 {
		t.Fatalf("missing artifact: %#v", env)
	}
	artifact := artifacts[0].(map[string]any)
	path, ok := artifact["path"].(string)
	if !ok || path == "" {
		t.Fatalf("missing artifact path: %#v", artifact)
	}
	stored, err := os.ReadFile(path)
	if err != nil || artifact["sizeBytes"] != float64(len(stored)) {
		t.Fatalf("artifact=%#v bytes=%d err=%v", artifact, len(stored), err)
	}
	return stored
}

func TestC4DeliveryDoesNotChangeStdoutFiltersOrProjection(t *testing.T) {
	if currentEdition() != cliEditionVolclog {
		t.Skip("default edition output")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(filterFileProjectsResponse))
	}))
	defer server.Close()
	setWorkflowAppTestEnv(t, server.URL)
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--output-mode", "stdout", "--jmes-filter", "summary.deliveryMode", "raw", "--method", "GET", "--path", "/DescribeProjects"}, &stdout, &stderr)
	if code != 0 || stderr.Len() != 0 || strings.TrimSpace(stdout.String()) != `"stdout"` {
		t.Fatalf("stdout filter exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	code = Run([]string{"--output-mode", "file", "--output-dir", t.TempDir(), "tool", "exec", "project.describe-projects", "--input", `{}`, "--context", `{"execution":{"projection":"Projects[].ProjectId"}}`}, &stdout, &stderr)
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("projection exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	env := c4DeliveryDecode(t, stdout.Bytes())
	stored := c4AssertFileDelivery(t, env, stdout.Len(), "file_forced")
	var fileEnv map[string]any
	if err := json.Unmarshal(stored, &fileEnv); err != nil {
		t.Fatal(err)
	}
	data, ok := fileEnv["data"].(map[string]any)
	if !ok || !reflect.DeepEqual(data["items"], []any{"p1", "p2"}) {
		t.Fatalf("projection no longer targets business data: %#v", fileEnv["data"])
	}
}
