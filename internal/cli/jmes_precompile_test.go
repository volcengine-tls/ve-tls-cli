package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/output"
)

func TestJMESFilterPreflightRejectsBeforeDispatch(t *testing.T) {
	var stdout, stderr bytes.Buffer
	invocation := &runInvocation{
		group:      "raw",
		rest:       []string{"--method", "GET", "--path", "/DescribeProjects"},
		flags:      GlobalFlags{Filter: "???"},
		outputMode: "stdout",
		ctx:        newContext(&stdout, &stderr, output.FormatJSON, "", "???"),
	}

	code, done := preflightRunInvocation(invocation, &stdout, &stderr)
	if !done || code != 3 {
		t.Fatalf("preflight = code %d done %v, want code 3 and done", code, done)
	}
	if !strings.Contains(stdout.String(), "invalid jmes-filter expression") {
		t.Fatalf("stdout = %q, want invalid filter error", stdout.String())
	}
}

func TestToolExecProjectionRejectsBeforeExecutor(t *testing.T) {
	ctx := newContext(&bytes.Buffer{}, &bytes.Buffer{}, output.FormatJSON, "", "")
	_, err := runToolExec(ctx, []string{
		"account.get",
		"--context", `{"execution":{"projection":"???"}}`,
	})
	if err == nil || !strings.Contains(err.Error(), "invalid execution.projection: invalid jmes-filter expression") {
		t.Fatalf("runToolExec() error = %v, want invalid projection error before executor", err)
	}
	payload, code := classifyError(err, "", 0, "tool")
	if code != 3 || payload.Kind != "decode" || payload.Hint != "check context.execution.projection" {
		t.Fatalf("classifyError() = code %d payload %#v, want projection decode error", code, payload)
	}
}

func TestWorkflowExecProjectionRejectsBeforeDispatch(t *testing.T) {
	ctx := newContext(&bytes.Buffer{}, &bytes.Buffer{}, output.FormatJSON, "", "")
	_, err := runWorkflowExec(ctx, []string{
		"app.resolve-resources",
		"--context", `{"execution":{"projection":"???"}}`,
		"--input", `{"AppId":"app"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "invalid execution.projection: invalid jmes-filter expression") {
		t.Fatalf("runWorkflowExec() error = %v, want invalid projection error before dispatch", err)
	}
	payload, code := classifyError(err, "", 0, "workflow")
	if code != 3 || payload.Kind != "decode" || payload.Hint != "check context.execution.projection" {
		t.Fatalf("classifyError() = code %d payload %#v, want projection decode error", code, payload)
	}
}
