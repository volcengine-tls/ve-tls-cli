package runtime

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/execution"
)

type fakeExecutionTransport struct {
	response execution.Response
	err      error
	calls    int
}

func (t *fakeExecutionTransport) Do(context.Context, execution.Request) (execution.Response, error) {
	t.calls++
	return t.response, t.err
}

type recordingTracer struct {
	requests  []execution.Request
	responses []execution.Response
	errs      []error
	elapsed   []time.Duration
	onRequest func()
}

func (t *recordingTracer) TraceRequest(_ context.Context, request execution.Request) {
	if t.onRequest != nil {
		t.onRequest()
	}
	t.requests = append(t.requests, request)
}

func (t *recordingTracer) TraceResponse(_ context.Context, response execution.Response, elapsed time.Duration, err error) {
	t.responses = append(t.responses, response)
	t.elapsed = append(t.elapsed, elapsed)
	t.errs = append(t.errs, err)
}

func TestTracingTransportDecoratesRequestAndResponse(t *testing.T) {
	wantErr := errors.New("server failed")
	next := &fakeExecutionTransport{
		response: execution.Response{
			StatusCode: http.StatusBadGateway,
			Header:     http.Header{"X-Tls-Requestid": []string{"request-id"}},
			Body:       []byte(`{"error":true}`),
		},
		err: wantErr,
	}
	tracer := &recordingTracer{}
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	transport := NewTracingTransport(next, tracer)
	clockCalls := 0
	clockCallsAtRequest := 0
	tracer.onRequest = func() {
		clockCallsAtRequest = clockCalls
	}
	transport.clock = func() time.Time {
		clockCalls++
		current := now
		now = now.Add(25 * time.Millisecond)
		return current
	}
	request := execution.Request{Method: http.MethodGet, Path: "/path"}
	response, err := transport.Do(context.Background(), request)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Do error=%v", err)
	}
	if response.StatusCode != http.StatusBadGateway || next.calls != 1 {
		t.Fatalf("response=%+v calls=%d", response, next.calls)
	}
	if len(tracer.requests) != 1 || tracer.requests[0].Path != "/path" {
		t.Fatalf("traced requests=%+v", tracer.requests)
	}
	if clockCallsAtRequest != 1 {
		t.Fatalf("clock calls at request trace=%d, want 1", clockCallsAtRequest)
	}
	if len(tracer.responses) != 1 ||
		tracer.responses[0].Header.Get("X-Tls-Requestid") != "request-id" ||
		!errors.Is(tracer.errs[0], wantErr) ||
		tracer.elapsed[0] != 25*time.Millisecond {
		t.Fatalf("traced responses=%+v errs=%v elapsed=%v", tracer.responses, tracer.errs, tracer.elapsed)
	}
}

func TestTracingTransportNoopAndNilNext(t *testing.T) {
	next := &fakeExecutionTransport{response: execution.Response{StatusCode: http.StatusOK}}
	transport := NewTracingTransport(next, nil)
	if _, err := transport.Do(context.Background(), execution.Request{}); err != nil {
		t.Fatalf("no-op Do: %v", err)
	}
	if next.calls != 1 {
		t.Fatalf("next calls=%d", next.calls)
	}

	_, err := NewTracingTransport(nil, NoopTracer{}).Do(context.Background(), execution.Request{})
	if err == nil || err.Error() != "nil execution transport" {
		t.Fatalf("nil next error=%v", err)
	}
}

var _ Tracer = NoopTracer{}
var _ execution.Transport = (*TracingTransport)(nil)
