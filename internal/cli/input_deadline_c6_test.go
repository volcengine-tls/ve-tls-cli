package cli

import (
	"net/http"
	"os"
	"testing"
	"time"
)

func TestC6OpenStdinUsesInvocationDeadline(t *testing.T) {
	for _, args := range [][]string{
		{"tool", "exec", "project.describe-projects", "--input", "-"},
		{"tool", "exec", "project.describe-projects", "--context", "-"},
		{"raw", "--path", "/DescribeProjects", "--body", "-"},
		{"workflow", "exec", appResolveTopicIDsWorkflowID, "--input", "-"},
		{"--allow-write", "workflow", "exec", "log.ingest", "--input", `{"TopicId":"t","Input":"-"}`},
	} {
		t.Run(args[0]+args[len(args)-2], func(t *testing.T) {
			_, calls := outcomeCLIServer(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) })
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			defer writer.Close()
			original := os.Stdin
			os.Stdin = reader
			defer func() { os.Stdin = original }()
			start := time.Now()
			code, env := outcomeCLIRun(t, append([]string{"--timeout", "45ms"}, args...))
			if code != 2 || calls.Load() != 0 || time.Since(start) > time.Second {
				t.Fatalf("stdin deadline exit=%d requests=%d elapsed=%s env=%#v", code, calls.Load(), time.Since(start), env)
			}
			if env["error"].(map[string]any)["kind"] != "deadline" || env["summary"].(map[string]any)["remoteOutcome"] != "not_sent" {
				t.Fatalf("stdin cancellation lost evidence: %#v", env)
			}
		})
	}
}

func TestC6DirectRequestCannotBypassWriteGate(t *testing.T) {
	ctx := &Context{}
	_, err := ctx.DoRaw("POST", "/CreateProject", nil, nil, []byte(`{}`))
	if err == nil {
		t.Fatal("direct write was permitted")
	}
	payload, code := classifyError(err, "", 0, "raw")
	if code != 1 || payload.Kind != "permission" || currentRemoteOutcome(ctx, err) != "not_sent" {
		t.Fatalf("direct write error=%v classification=%#v exit=%d", err, payload, code)
	}
}

func TestC6ContextDeadlineStartsBeforeRequestStdin(t *testing.T) {
	for _, args := range [][]string{
		{"tool", "exec", "project.describe-projects"},
		{"workflow", "exec", appResolveTopicIDsWorkflowID},
	} {
		t.Run(args[0], func(t *testing.T) {
			_, calls := outcomeCLIServer(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) })
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			defer writer.Close()
			original := os.Stdin
			os.Stdin = reader
			defer func() { os.Stdin = original }()
			// Bound a regression without giving the command an early EOF.
			forceClose := time.AfterFunc(500*time.Millisecond, func() { _ = writer.Close() })
			defer forceClose.Stop()
			start := time.Now()
			code, env := outcomeCLIRun(t, append(args, "--context", `{"execution":{"timeout":"45ms"}}`, "--input", "-"))
			if code != 2 || calls.Load() != 0 || time.Since(start) > 350*time.Millisecond {
				t.Fatalf("context deadline exit=%d requests=%d elapsed=%s env=%#v", code, calls.Load(), time.Since(start), env)
			}
			if env["error"].(map[string]any)["kind"] != "deadline" {
				t.Fatalf("context deadline error=%#v", env)
			}
		})
	}
}
