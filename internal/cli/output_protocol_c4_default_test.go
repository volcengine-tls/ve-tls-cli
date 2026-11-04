//go:build !human

package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/output"
)

func TestC4DefaultEarlyFailuresAreEnvelopes(t *testing.T) {
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	for _, args := range [][]string{
		nil, {"--unknown"}, {"--output"}, {"unknown-command"}, {"tool"}, {"workflow"},
		{"raw"}, {"tool", "exec"}, {"tool", "exec", "account.get", "--output-dir"},
		{"--output", "invalid", "tool", "list"}, {"version", "unexpected"},
	} {
		t.Run("args="+joinC4Args(args), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(args, &stdout, &stderr)
			var env map[string]any
			if code == 0 || json.Unmarshal(stdout.Bytes(), &env) != nil {
				t.Fatalf("expected failed JSON envelope: exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if stderr.Len() != 0 || env["status"] != "failed" || env["error"] == nil {
				t.Fatalf("invalid failure channels: envelope=%#v stderr=%q", env, stderr.String())
			}
			if env["summary"].(map[string]any)["remoteOutcome"] != "not_sent" {
				t.Fatalf("early failure claims remote execution: %#v", env)
			}
		})
	}
}

func joinC4Args(args []string) string {
	raw, _ := json.Marshal(args)
	return string(raw)
}

func TestC4DefaultForcedFileReturnsArtifactEnvelope(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "fake-ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "fake-sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--dry-run", "--output-mode", "file", "--output-dir", t.TempDir(), "raw", "--path", "/DescribeProjects"}, &stdout, &stderr)
	var env map[string]any
	if code != 0 || json.Unmarshal(stdout.Bytes(), &env) != nil {
		t.Fatalf("expected file delivery JSON envelope: exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 || env["status"] != "success" || env["data"] != nil {
		t.Fatalf("expected delivery envelope without repeated result data: %#v stderr=%q", env, stderr.String())
	}
	artifacts, ok := env["artifacts"].([]any)
	if !ok || len(artifacts) != 1 {
		t.Fatalf("missing artifact descriptor: %#v", env)
	}
	path, _ := artifacts[0].(map[string]any)["path"].(string)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]any
	if json.Unmarshal(raw, &stored) != nil || stored["data"] == nil {
		t.Fatalf("artifact lost full result: %q", raw)
	}
}

func TestC4DefaultRawScalarResultsUseEnvelope(t *testing.T) {
	for _, body := range []string{`"a string response"`, `null`, `false`, `42`, `[1,2]`} {
		t.Run(body, func(t *testing.T) {
			_, requests := outcomeCLIServer(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(body))
			})
			var stdout, stderr bytes.Buffer
			code := Run([]string{"--allow-write", "raw", "--path", "/Action"}, &stdout, &stderr)
			var env map[string]any
			if code != 0 || requests.Load() != 1 || stderr.Len() != 0 || json.Unmarshal(stdout.Bytes(), &env) != nil || env["status"] != "success" {
				t.Fatalf("scalar bypassed envelope: exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			encoded, err := json.Marshal(env["data"])
			if err != nil || string(encoded) != body {
				t.Fatalf("scalar changed: body=%s data=%s err=%v", body, encoded, err)
			}
		})
	}
}

func TestC4DefaultAutomaticDeliveryAt16KiBBoundary(t *testing.T) {
	const limit = 16 * 1024
	if toolExecAutoArtifactByteLimit != limit {
		t.Fatalf("automatic artifact threshold=%d, want %d", toolExecAutoArtifactByteLimit, limit)
	}
	for _, size := range []int{limit - 1, limit, limit + 1} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			ctx := newContext(&stdout, &stderr, output.FormatJSON, "", "")
			ctx.OutputMode = "stdout"
			ctx.OutputDir = t.TempDir()
			ctx.Action = "tool.test"
			var env map[string]any
			var data map[string]any
			blobSize := size - 512
			for attempt := 0; attempt < 8; attempt++ {
				data = map[string]any{"blob": strings.Repeat("x", blobSize)}
				var err error
				env, err = buildAPIEnvelope(ctx, "tool", data, "stdout", "", output.FormatJSON)
				if err != nil {
					t.Fatal(err)
				}
				encoded, err := marshalOutputBytes(env, output.FormatJSON)
				if err != nil {
					t.Fatal(err)
				}
				if len(encoded) == size {
					break
				}
				blobSize += size - len(encoded)
			}
			actualSize, err := estimateOutputBytes(env, output.FormatJSON)
			if err != nil || actualSize != size {
				t.Fatalf("boundary fixture size=%d, want %d err=%v", actualSize, size, err)
			}
			final, err := finalizeToolExecEnvelope(ctx, data, env, toolExecutionOptions{})
			if err != nil {
				t.Fatal(err)
			}
			invocation := &runInvocation{group: "tool", rest: []string{"exec"}, format: output.FormatJSON, outputMode: "stdout", ctx: ctx}
			if code := renderRunResult(invocation, &stdout, &stderr, final, 0, nil); code != 0 || stderr.Len() != 0 {
				t.Fatalf("render boundary: exit=%d stderr=%q", code, stderr.String())
			}
			var reported map[string]any
			if err := json.Unmarshal(stdout.Bytes(), &reported); err != nil {
				t.Fatalf("invalid JSON delivery at boundary: %v stdout=%q", err, stdout.String())
			}
			artifacts := reported["artifacts"].([]any)
			if size <= limit {
				if len(artifacts) != 0 || reported["data"].(map[string]any)["blob"] != data["blob"] {
					t.Fatalf("result spilled at or below threshold: %#v", reported["summary"])
				}
				return
			}
			if reported["data"] != nil || len(artifacts) != 1 || reported["summary"].(map[string]any)["deliveryMode"] != "file_auto" {
				t.Fatalf("missing artifact above threshold: %#v", reported["summary"])
			}
			path := artifacts[0].(map[string]any)["path"].(string)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var stored map[string]any
			if json.Unmarshal(raw, &stored) != nil || stored["data"].(map[string]any)["blob"] != data["blob"] {
				t.Fatal("automatic artifact lost original response")
			}
		})
	}
}
