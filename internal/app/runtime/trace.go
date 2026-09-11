package runtime

import (
	"context"
	"errors"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/execution"
)

// Tracer observes execution transport requests and responses. Persistence,
// redaction policy, and lifecycle remain presentation-layer responsibilities.
type Tracer interface {
	TraceRequest(context.Context, execution.Request)
	TraceResponse(context.Context, execution.Response, time.Duration, error)
}

// NoopTracer discards all trace events.
type NoopTracer struct{}

func (NoopTracer) TraceRequest(context.Context, execution.Request) {}

func (NoopTracer) TraceResponse(context.Context, execution.Response, time.Duration, error) {}

// TracingTransport decorates an execution transport without changing its
// response or error.
type TracingTransport struct {
	next   execution.Transport
	tracer Tracer
	clock  func() time.Time
}

// NewTracingTransport creates a transport decorator. A nil or typed-nil tracer
// is replaced by NoopTracer.
func NewTracingTransport(next execution.Transport, tracer Tracer) *TracingTransport {
	if isTypedNil(tracer) {
		tracer = NoopTracer{}
	}
	return &TracingTransport{
		next:   next,
		tracer: tracer,
		clock:  time.Now,
	}
}

func (t *TracingTransport) Do(ctx context.Context, request execution.Request) (execution.Response, error) {
	if t == nil || isTypedNil(t.next) {
		return execution.Response{}, errors.New("nil execution transport")
	}
	tracer := t.tracer
	if isTypedNil(tracer) {
		tracer = NoopTracer{}
	}
	clock := t.clock
	if clock == nil {
		clock = time.Now
	}
	start := clock()
	tracer.TraceRequest(ctx, cloneExecutionRequest(request))
	response, err := t.next.Do(ctx, request)
	elapsed := clock().Sub(start)
	tracer.TraceResponse(ctx, cloneExecutionResponse(response), elapsed, err)
	return response, err
}

func cloneExecutionRequest(request execution.Request) execution.Request {
	request.Query = cloneStringMap(request.Query)
	request.QueryMulti = cloneMultiStringMap(request.QueryMulti)
	request.Header = cloneStringMap(request.Header)
	request.Body = append([]byte(nil), request.Body...)
	return request
}

func cloneMultiStringMap(source map[string][]string) map[string][]string {
	if source == nil {
		return map[string][]string{}
	}
	cloned := make(map[string][]string, len(source))
	for key, values := range source {
		cloned[key] = append([]string(nil), values...)
	}
	return cloned
}

func cloneExecutionResponse(response execution.Response) execution.Response {
	response.Header = response.Header.Clone()
	response.Body = append([]byte(nil), response.Body...)
	return response
}

var _ execution.Transport = (*TracingTransport)(nil)
