package cli

import (
	"bytes"
	"net/http"
	"testing"
)

func TestC5AcceptedTaskSurvivesDeliveryFailureWithoutReplay(t *testing.T) {
	for _, mode := range []string{"stdout", "file notice", "projection"} {
		t.Run(mode, func(t *testing.T) {
			_, requests := outcomeCLIServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("x-tls-requestid", "req-task")
				_, _ = w.Write([]byte(`{"TaskId":"task-1","Enabled":true}`))
			})
			args := []string{"--allow-write", "tool", "exec", "log.create-download-task", "--input", downloadTaskC5Input}
			var stdout, stderr bytes.Buffer
			var code int
			var envelope map[string]any
			if mode == "projection" {
				args = append(args, "--context", `{"execution":{"projection":"length(Enabled)"}}`)
				code = Run(args, &stdout, &stderr)
				envelope = outcomeCLIDecode(t, stdout.Bytes())
				if stderr.Len() != 0 {
					t.Fatalf("unexpected stderr: %q", stderr.String())
				}
			} else {
				if mode == "file notice" {
					args = append([]string{"--output-mode", "file", "--output-dir", t.TempDir()}, args...)
				}
				writer := &c4FileFailWriter{}
				code = Run(args, writer, &stderr)
				envelope = outcomeCLIDecode(t, stderr.Bytes())
				if writer.writes != 1 {
					t.Fatalf("stdout writes=%d", writer.writes)
				}
			}
			if code != 3 || requests.Load() != 1 || envelope["requestId"] != "req-task" {
				t.Fatalf("exit=%d requests=%d envelope=%#v", code, requests.Load(), envelope)
			}
			assertOutcomeCLIEnvelope(t, envelope, "accepted", "failed", "output_failed", "OutputFailed")
			summary := envelope["summary"].(map[string]any)
			async, ok := summary["async"].(map[string]any)
			if !ok || async["taskId"] != "task-1" || async["state"] != "accepted" || async["terminal"] != false || async["next"] == nil {
				t.Fatalf("lost accepted task guidance: %#v", summary)
			}
		})
	}
}

func TestC5DownloadTaskDryRunDoesNotClaimAcceptance(t *testing.T) {
	_, requests := outcomeCLIServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"TaskId":"task-1"}`))
	})
	code, envelope := outcomeCLIRun(t, []string{"--dry-run", "tool", "exec", "log.create-download-task", "--input", downloadTaskC5Input})
	assertOutcomeCLIEnvelope(t, envelope, "not_sent", "success", "", "")
	summary := envelope["summary"].(map[string]any)
	if code != 0 || requests.Load() != 0 || summary["async"] != nil {
		t.Fatalf("dry-run claimed task acceptance: exit=%d requests=%d envelope=%#v", code, requests.Load(), envelope)
	}
}
