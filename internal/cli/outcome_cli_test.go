package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

const outcomeCLICreateInput = `{"body":{"ProjectName":"demo","Region":"cn-beijing"}}`
const outcomeCLIIngestInput = `{"TopicId":"test-topic","Input":"one log","InputFormat":"lines"}`

func outcomeCLIArgs(kind string) []string {
	switch kind {
	case "raw":
		return []string{"raw", "--method", "POST", "--path", "/CreateProject", "--body", `{}`}
	case "tool":
		return []string{"tool", "exec", "project.create", "--input", outcomeCLICreateInput}
	case "workflow":
		return []string{"workflow", "exec", "log.ingest", "--input", outcomeCLIIngestInput}
	default:
		panic("unsupported outcome test command")
	}
}

func outcomeCLIServer(t *testing.T, handler func(http.ResponseWriter, *http.Request)) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	setWorkflowAppTestEnv(t, server.URL)
	return server, &requests
}

func outcomeCLIRun(t *testing.T, args []string) (int, map[string]any) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(args, &stdout, &stderr)
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr %q", stderr.String())
	}
	return code, outcomeCLIDecode(t, stdout.Bytes())
}

func outcomeCLIDecode(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var envelope map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decode CLI envelope: %v, output=%q", err, raw)
	}
	return envelope
}

func assertOutcomeCLIEnvelope(t *testing.T, envelope map[string]any, remote, delivery, kind, code string) {
	t.Helper()
	summary, ok := envelope["summary"].(map[string]any)
	if !ok || summary["remoteOutcome"] != remote || summary["deliveryOutcome"] != delivery {
		t.Fatalf("summary=%#v, want remote=%q delivery=%q", envelope["summary"], remote, delivery)
	}
	if kind == "" {
		if envelope["status"] != "success" || envelope["error"] != nil {
			t.Fatalf("expected successful envelope, got %#v", envelope)
		}
		return
	}
	failure, ok := envelope["error"].(map[string]any)
	if envelope["status"] != "failed" || !ok || failure["kind"] != kind || (code != "" && failure["code"] != code) {
		t.Fatalf("failure envelope=%#v, want kind=%q code=%q", envelope, kind, code)
	}
}

func TestOutcomeCLIAutoArtifactFailureAfterWrite(t *testing.T) {
	t.Run("tool response exceeds actual threshold", func(t *testing.T) {
		_, requests := outcomeCLIServer(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.URL.Path != "/CreateProject" {
				t.Errorf("unexpected business request: %s %s", r.Method, r.URL.Path)
			}
			w.Header().Set("x-tls-requestid", "req-created")
			_, _ = fmt.Fprintf(w, `{"ProjectId":"p1","result":"%s"}`, strings.Repeat("x", 17*1024))
		})
		code, envelope := outcomeCLIRun(t, outcomeCLIArgs("tool"))
		if code != 2 || requests.Load() != 1 {
			t.Fatalf("exit=%d requests=%d envelope=%#v", code, requests.Load(), envelope)
		}
		assertOutcomeCLIEnvelope(t, envelope, "success", "failed", "output_failed", "OutputFailed")
		if envelope["requestId"] != "req-created" {
			t.Fatalf("requestId=%#v", envelope["requestId"])
		}
	})

	t.Run("workflow ingest auto artifact", func(t *testing.T) {
		previousLimit := toolExecAutoArtifactByteLimit
		toolExecAutoArtifactByteLimit = 1
		t.Cleanup(func() { toolExecAutoArtifactByteLimit = previousLimit })
		_, requests := outcomeCLIServer(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.URL.Path != "/PutLogs" {
				t.Errorf("unexpected business request: %s %s", r.Method, r.URL.Path)
			}
			w.Header().Set("x-tls-requestid", "req-ingested")
			_, _ = w.Write([]byte(`{}`))
		})
		code, envelope := outcomeCLIRun(t, outcomeCLIArgs("workflow"))
		if code != 2 || requests.Load() != 1 {
			t.Fatalf("exit=%d requests=%d envelope=%#v", code, requests.Load(), envelope)
		}
		assertOutcomeCLIEnvelope(t, envelope, "success", "failed", "output_failed", "OutputFailed")
	})
}

func TestOutcomeCLIFilterFailuresAfterSuccessfulRequest(t *testing.T) {
	for _, kind := range []string{"raw", "tool", "workflow"} {
		t.Run(kind+" global filter miss", func(t *testing.T) {
			_, requests := outcomeCLIServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("x-tls-requestid", "req-success")
				_, _ = w.Write([]byte(`{"ProjectId":"p1","Enabled":true}`))
			})
			args := append([]string{"--jmes-filter", "data.nonexistent"}, outcomeCLIArgs(kind)...)
			code, envelope := outcomeCLIRun(t, args)
			if code != 3 || requests.Load() != 1 {
				t.Fatalf("exit=%d requests=%d envelope=%#v", code, requests.Load(), envelope)
			}
			assertOutcomeCLIEnvelope(t, envelope, "success", "failed", "output_failed", "OutputFailed")
		})
	}

	t.Run("tool context projection evaluation type error", func(t *testing.T) {
		_, requests := outcomeCLIServer(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("x-tls-requestid", "req-success")
			_, _ = w.Write([]byte(`{"Enabled":true}`))
		})
		args := append(outcomeCLIArgs("tool"), "--context", `{"execution":{"projection":"length(Enabled)"}}`)
		code, envelope := outcomeCLIRun(t, args)
		if code != 3 || requests.Load() != 1 {
			t.Fatalf("exit=%d requests=%d envelope=%#v", code, requests.Load(), envelope)
		}
		assertOutcomeCLIEnvelope(t, envelope, "success", "failed", "output_failed", "OutputFailed")
	})
}

func TestOutcomeCLIUnsupportedFormatAfterWrite(t *testing.T) {
	_, requests := outcomeCLIServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("x-tls-requestid", "req-created")
		_, _ = w.Write([]byte(`{"ProjectId":"p1"}`))
	})
	args := append([]string{"--output", "table"}, outcomeCLIArgs("tool")...)
	code, envelope := outcomeCLIRun(t, args)
	if code != 3 || requests.Load() != 1 {
		t.Fatalf("exit=%d requests=%d", code, requests.Load())
	}
	assertOutcomeCLIEnvelope(t, envelope, "success", "failed", "output_failed", "OutputFailed")
}

func TestOutcomeCLITargetDescribesLegacyCredentialFallback(t *testing.T) {
	server, requests := outcomeCLIServer(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Authorization"), "fallback-ak") {
			t.Error("request did not use legacy environment fallback")
		}
		_, _ = w.Write([]byte(`{"ProjectId":"p1"}`))
	})
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "fallback-ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "")
	t.Setenv("VOLCENGINE_TOKEN", "")
	config := map[string]any{"version": 1, "current_profile": "legacy", "profiles": map[string]any{
		"legacy": map[string]any{"secret_access_key": "profile-sk", "region": "cn-beijing", "endpoint": server.URL},
	}}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("VOLCLOG_CONFIG"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	code, env := outcomeCLIRun(t, outcomeCLIArgs("raw"))
	if code != 0 || requests.Load() != 1 {
		t.Fatalf("exit=%d requests=%d", code, requests.Load())
	}
	target := env["summary"].(map[string]any)["target"].(map[string]any)
	if target["credentialSource"] != "mixed" {
		t.Fatalf("credential source misreported: %#v", target)
	}
	encoded, _ := json.Marshal(env)
	if strings.Contains(string(encoded), "fallback-ak") || strings.Contains(string(encoded), "profile-sk") {
		t.Fatal("target exposed credentials")
	}
}

func TestOutcomeCLIHTTP501IsNotMarkedTransient(t *testing.T) {
	_, requests := outcomeCLIServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
		_, _ = w.Write([]byte(`{"ErrorCode":"NotImplemented","ErrorMessage":"unsupported operation"}`))
	})
	code, env := outcomeCLIRun(t, outcomeCLIArgs("tool"))
	if code != 2 || requests.Load() != 1 {
		t.Fatalf("exit=%d requests=%d", code, requests.Load())
	}
	assertOutcomeCLIEnvelope(t, env, "failed", "success", "server", "NotImplemented")
	failure := env["error"].(map[string]any)
	if failure["transient"] == true || failure["safeToReplay"] != false {
		t.Fatalf("unsafe retry metadata: %#v", failure)
	}
}

func TestOutcomeCLIForcedArtifactFailsAfterPreflightAndWrite(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "output")
	directoryCheck := make(chan error, 1)
	_, requests := outcomeCLIServer(t, func(w http.ResponseWriter, _ *http.Request) {
		info, err := os.Stat(outDir)
		if err == nil && !info.IsDir() {
			err = errors.New("preflight output path is not a directory")
		}
		if err == nil {
			err = os.Remove(outDir)
		}
		if err == nil {
			err = os.WriteFile(outDir, []byte("directory replaced"), 0o600)
		}
		select {
		case directoryCheck <- err:
		default:
		}
		w.Header().Set("x-tls-requestid", "req-created")
		_, _ = w.Write([]byte(`{"ProjectId":"p1"}`))
	})
	args := append([]string{"--output-mode", "file", "--output-dir", outDir}, outcomeCLIArgs("tool")...)
	code, envelope := outcomeCLIRun(t, args)
	if requests.Load() != 1 {
		t.Fatalf("requests=%d exit=%d envelope=%#v", requests.Load(), code, envelope)
	}
	if err := <-directoryCheck; err != nil {
		t.Fatalf("replace preflighted directory: %v", err)
	}
	if code != 2 || envelope["requestId"] != "req-created" {
		t.Fatalf("exit=%d envelope=%#v", code, envelope)
	}
	assertOutcomeCLIEnvelope(t, envelope, "success", "failed", "output_failed", "OutputFailed")
}

type outcomeCLIFailingWriter struct{ short bool }

func (w outcomeCLIFailingWriter) Write([]byte) (int, error) {
	if w.short {
		return 0, nil
	}
	return 0, errors.New("simulated stdout write failure")
}

func TestOutcomeCLIStdoutFailureReportsStructuredStderr(t *testing.T) {
	for _, kind := range []string{"raw", "tool", "workflow"} {
		for _, short := range []bool{false, true} {
			name := kind + "/write error"
			if short {
				name = kind + "/short write"
			}
			t.Run(name, func(t *testing.T) {
				_, requests := outcomeCLIServer(t, func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("x-tls-requestid", "req-success")
					_, _ = w.Write([]byte(`{"ProjectId":"p1"}`))
				})
				var stderr bytes.Buffer
				code := Run(outcomeCLIArgs(kind), outcomeCLIFailingWriter{short: short}, &stderr)
				if code != 3 || requests.Load() != 1 {
					t.Fatalf("exit=%d requests=%d stderr=%q", code, requests.Load(), stderr.String())
				}
				envelope := outcomeCLIDecode(t, stderr.Bytes())
				assertOutcomeCLIEnvelope(t, envelope, "success", "failed", "output_failed", "OutputFailed")
				if envelope["requestId"] != "req-success" {
					t.Fatalf("requestId=%#v", envelope["requestId"])
				}
			})
		}
	}
}

func TestOutcomeCLINoticeFailurePreservesDeliveredArtifact(t *testing.T) {
	for _, kind := range []string{"raw", "tool", "workflow"} {
		t.Run(kind, func(t *testing.T) {
			_, requests := outcomeCLIServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("x-tls-requestid", "req-created")
				_, _ = w.Write([]byte(`{"ProjectId":"p1"}`))
			})
			args := append([]string{"--output-mode", "file", "--output-dir", t.TempDir()}, outcomeCLIArgs(kind)...)
			var stderr bytes.Buffer
			code := Run(args, outcomeCLIFailingWriter{short: true}, &stderr)
			if code != 3 || requests.Load() != 1 {
				t.Fatalf("exit=%d requests=%d", code, requests.Load())
			}
			env := outcomeCLIDecode(t, stderr.Bytes())
			assertOutcomeCLIEnvelope(t, env, "success", "failed", "output_failed", "OutputFailed")
			artifacts := env["artifacts"].([]any)
			if len(artifacts) != 1 {
				t.Fatalf("lost artifact: %#v", env)
			}
			path := artifacts[0].(map[string]any)["path"].(string)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			stored := outcomeCLIDecode(t, data)
			assertOutcomeCLIEnvelope(t, stored, "success", "success", "", "")
		})
	}
}

func TestOutcomeCLIBodyReadFailureRetainsCurrentHeaders(t *testing.T) {
	for _, kind := range []string{"raw", "tool"} {
		t.Run(kind, func(t *testing.T) {
			_, requests := outcomeCLIServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("x-tls-requestid", "req-incomplete")
				w.Header().Set("Content-Length", "100")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"partial":`))
			})
			code, envelope := outcomeCLIRun(t, outcomeCLIArgs(kind))
			if code == 0 || requests.Load() != 1 {
				t.Fatalf("exit=%d requests=%d envelope=%#v", code, requests.Load(), envelope)
			}
			assertOutcomeCLIEnvelope(t, envelope, "unknown", "success", "result_unknown", "")
			failure := envelope["error"].(map[string]any)
			if envelope["requestId"] != "req-incomplete" || failure["requestId"] != "req-incomplete" || failure["statusCode"] != float64(http.StatusOK) {
				t.Fatalf("response metadata not preserved: %#v", envelope)
			}
		})
	}
}

func TestOutcomeCLIRawCompatibilityAndStandardJSONProtocol(t *testing.T) {
	t.Run("raw accepts 201 text", func(t *testing.T) {
		_, requests := outcomeCLIServer(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("x-tls-requestid", "req-raw")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte("plain text"))
		})
		code, envelope := outcomeCLIRun(t, outcomeCLIArgs("raw"))
		if code != 0 || requests.Load() != 1 || envelope["requestId"] != "req-raw" {
			t.Fatalf("exit=%d requests=%d envelope=%#v", code, requests.Load(), envelope)
		}
		assertOutcomeCLIEnvelope(t, envelope, "success", "success", "", "")
		data, ok := envelope["data"].(map[string]any)
		if !ok || data["data"] != "plain text" {
			t.Fatalf("raw response data=%#v", envelope["data"])
		}
	})
	for _, tc := range []struct {
		name   string
		status int
		body   string
		code   string
	}{
		{name: "201", status: http.StatusCreated, body: `{"ProjectId":"p1"}`, code: "UnexpectedHTTPStatus"},
		{name: "HTML", status: http.StatusOK, body: "<html>error</html>", code: "InvalidJSONResponse"},
	} {
		t.Run("tool "+tc.name, func(t *testing.T) {
			_, requests := outcomeCLIServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("x-tls-requestid", "req-protocol")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			code, envelope := outcomeCLIRun(t, outcomeCLIArgs("tool"))
			if code != 3 || requests.Load() != 1 || envelope["requestId"] != "req-protocol" {
				t.Fatalf("exit=%d requests=%d envelope=%#v", code, requests.Load(), envelope)
			}
			assertOutcomeCLIEnvelope(t, envelope, "unknown", "success", "protocol", tc.code)
		})
	}
}

func TestOutcomeCLIHTTPClassificationPreservesUpstreamError(t *testing.T) {
	for _, kind := range []string{"raw", "tool"} {
		for _, tc := range []struct {
			status  int
			service string
			kind    string
		}{
			{http.StatusBadRequest, "BadParameter", "usage"},
			{http.StatusUnauthorized, "Unauthorized", "auth"},
			{http.StatusForbidden, "Forbidden", "auth"},
			{http.StatusNotFound, "NotFound", "not_found"},
			{http.StatusConflict, "AlreadyExists", "conflict"},
			{http.StatusTooManyRequests, "RateLimited", "throttled"},
			{http.StatusInternalServerError, "UnknownServiceFailure", "server"},
			{http.StatusBadRequest, "Throttling", "throttled"},
			{http.StatusBadRequest, "LimitExceeded", "throttled"},
		} {
			t.Run(fmt.Sprintf("%s/%d/%s", kind, tc.status, tc.service), func(t *testing.T) {
				_, requests := outcomeCLIServer(t, func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("x-tls-requestid", "req-upstream")
					w.WriteHeader(tc.status)
					_, _ = fmt.Fprintf(w, `{"ErrorCode":%q,"ErrorMessage":"upstream detail"}`, tc.service)
				})
				code, envelope := outcomeCLIRun(t, outcomeCLIArgs(kind))
				if code != 2 || requests.Load() != 1 {
					t.Fatalf("exit=%d requests=%d envelope=%#v", code, requests.Load(), envelope)
				}
				assertOutcomeCLIEnvelope(t, envelope, "failed", "success", tc.kind, tc.service)
				failure := envelope["error"].(map[string]any)
				if envelope["requestId"] != "req-upstream" || failure["requestId"] != "req-upstream" ||
					failure["statusCode"] != float64(tc.status) || failure["source"] != "upstream" || failure["message"] != "upstream detail" {
					t.Fatalf("upstream evidence not preserved: %#v", envelope)
				}
			})
		}
	}
}
