package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
	"github.com/volcengine-tls/ve-tls-cli/internal/execution"
)

func TestC6ExecutionContextAbsoluteBudgetAndRepeatedSettings(t *testing.T) {
	var c Context
	startedAt := time.Now().Add(-2 * time.Second)
	c.initializeExecutionContext(context.Background(), startedAt)
	defer c.closeExecutionContext()
	c.setExecutionTimeout(10 * time.Second)
	first := c.ExecutionContext()
	deadline, ok := first.Deadline()
	if !ok || !deadline.Equal(startedAt.Add(10*time.Second)) {
		t.Fatalf("deadline=%v, want %v", deadline, startedAt.Add(10*time.Second))
	}
	c.setExecutionTimeout(10 * time.Second)
	if c.ExecutionContext() != first {
		t.Fatal("repeated timeout replaced the active context")
	}
	c.setExecutionTimeout(4 * time.Second)
	short := c.ExecutionContext()
	deadline, ok = short.Deadline()
	if !ok || !deadline.Equal(startedAt.Add(4*time.Second)) {
		t.Fatalf("shortened deadline=%v", deadline)
	}
	c.setExecutionTimeout(20 * time.Second)
	if c.ExecutionContext() != short {
		t.Fatal("later setting extended the existing budget")
	}
}

func TestC6ExecutionContextParentAndClose(t *testing.T) {
	var zero Context
	if zero.ExecutionContext() == nil || zero.ExecutionContext().Err() != nil {
		t.Fatal("zero context did not fall back to background")
	}
	if (*Context)(nil).ExecutionContext() == nil {
		t.Fatal("nil context did not fall back to background")
	}

	parent, cancelParent := context.WithCancel(context.Background())
	var c Context
	c.initializeExecutionContext(parent, time.Now())
	if _, ok := c.ExecutionContext().Deadline(); ok {
		t.Fatal("an invocation without a timeout acquired a default deadline")
	}
	c.setExecutionTimeout(time.Minute)
	execCtx := c.ExecutionContext()
	cancelParent()
	if !errors.Is(execCtx.Err(), context.Canceled) {
		t.Fatalf("parent cancellation: %v", execCtx.Err())
	}
	c.closeExecutionContext()

	parentDeadline := time.Now().Add(30 * time.Second)
	parent, cancelParent = context.WithDeadline(context.Background(), parentDeadline)
	defer cancelParent()
	var d Context
	d.initializeExecutionContext(parent, time.Now())
	d.setExecutionTimeout(time.Minute)
	if got, ok := d.ExecutionContext().Deadline(); !ok || !got.Equal(parentDeadline) {
		t.Fatalf("parent deadline=%v", got)
	}
	execCtx = d.ExecutionContext()
	d.closeExecutionContext()
	if !errors.Is(execCtx.Err(), context.Canceled) || parent.Err() != nil {
		t.Fatalf("close execution=%v parent=%v", execCtx.Err(), parent.Err())
	}

	expired, cancelExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelExpired()
	var e Context
	e.initializeExecutionContext(expired, time.Now())
	defer e.closeExecutionContext()
	if !errors.Is(e.ExecutionContext().Err(), context.DeadlineExceeded) {
		t.Fatalf("expired parent: %v", e.ExecutionContext().Err())
	}
}

func TestC6ExecutionContextElapsedBudget(t *testing.T) {
	var c Context
	c.initializeExecutionContext(context.Background(), time.Now().Add(-time.Second))
	defer c.closeExecutionContext()
	c.setExecutionTimeout(time.Millisecond)
	if !errors.Is(c.ExecutionContext().Err(), context.DeadlineExceeded) {
		t.Fatalf("elapsed budget: %v", c.ExecutionContext().Err())
	}
}

type c6RoundTripper func(*http.Request) (*http.Response, error)

func (f c6RoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestC6ToolExecUsesCancelledInvocationContext(t *testing.T) {
	calls := 0
	c := ingestTestContext(t, c6RoundTripper(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("unexpected send")
	}))
	parent, cancel := context.WithCancel(context.Background())
	cancel()
	c.initializeExecutionContext(parent, time.Now())
	defer c.closeExecutionContext()
	_, err := runToolExec(c, []string{"project.describe-projects"})
	if !errors.Is(err, context.Canceled) || calls != 0 || c.RemoteOutcome != execution.OutcomeNotSent {
		t.Fatalf("err=%v calls=%d outcome=%s", err, calls, c.RemoteOutcome)
	}
}

func TestC6RunContextStopsCancelledInvocationBeforeSend(t *testing.T) {
	_, requests := outcomeCLIServer(t, func(http.ResponseWriter, *http.Request) { t.Error("cancelled invocation reached server") })
	parent, cancel := context.WithCancel(context.Background())
	cancel()
	var stdout, stderr bytes.Buffer
	code := RunContext(parent, []string{"tool", "exec", "project.describe-projects"}, &stdout, &stderr)
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid envelope: %v, stderr=%q", err, stderr.String())
	}
	if code == 0 || requests.Load() != 0 || envelope["summary"].(map[string]any)["remoteOutcome"] != execution.OutcomeNotSent {
		t.Fatalf("exit=%d requests=%d envelope=%#v", code, requests.Load(), envelope)
	}
}

func TestC6DeadlineOnSecondPageRetainsPartialAndContinuation(t *testing.T) {
	var c Context
	c.initializeExecutionContext(context.Background(), time.Now())
	defer c.closeExecutionContext()
	c.setExecutionTimeout(500 * time.Millisecond)
	calls := 0
	transport := migrationTransportFunc(func(ctx context.Context, request execution.Request) (execution.Response, error) {
		calls++
		if calls == 1 && request.Query["Page"] == "1" {
			return execution.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: []byte(`{"Items":[{"ID":"first"}],"Total":2}`)}, nil
		}
		if calls != 2 || request.Query["Page"] != "2" {
			return execution.Response{}, errors.New("unexpected page")
		}
		select {
		case <-ctx.Done():
		case <-time.After(3 * time.Second):
			return execution.Response{}, errors.New("deadline did not propagate")
		}
		return execution.Response{}, &execution.Failure{Stage: "transport", Outcome: execution.OutcomeUnknown, Cause: ctx.Err()}
	})
	op := contract.Operation{ID: "test.list", Wire: contract.WireSpec{Method: "GET", Path: "/List", RequestFormat: "json", Codec: contract.CodecJSON},
		InputSchema: contract.JSONSchema{}, Pagination: &contract.PaginationSpec{Mode: contract.PaginationPageNumber, PageNumberParam: "Page", PageSizeParam: "Size", ItemsField: "Items", TotalField: "Total", DefaultPageSize: 1, MaxPages: 3}}
	result, err := execution.NewExecutor(transport, nil).Execute(c.ExecutionContext(), execution.Invocation{Operation: op, Options: execution.Options{PageAll: true}})
	if !errors.Is(err, context.DeadlineExceeded) || calls != 2 || result.RemoteOutcome != execution.OutcomePartial || result.Pagination == nil || result.Pagination.StopReason != "deadline" || result.Pagination.ItemCount != 1 {
		t.Fatalf("result=%#v err=%v calls=%d", result, err, calls)
	}
	var failedPage *execution.Failure
	if !errors.As(err, &failedPage) || failedPage.Outcome != execution.OutcomeUnknown {
		t.Fatalf("second-page transport evidence was lost: %v", err)
	}
	rows := result.Data.(map[string]any)["Items"].([]any)
	next := result.Pagination.Next
	if len(rows) != 1 || rows[0].(map[string]any)["ID"] != "first" || next == nil || !result.Pagination.Resumable || next.Input["query"].(map[string]any)["Page"] != 2 {
		t.Fatalf("rows=%#v continuation=%#v", rows, next)
	}
}
