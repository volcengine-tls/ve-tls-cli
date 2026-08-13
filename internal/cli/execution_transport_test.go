package cli

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
	"github.com/volcengine-tls/ve-tls-cli/internal/execution"
	"github.com/volcengine-tls/ve-tls-cli/internal/tlsapi"
)

type rawOnlyExecutionContext struct {
	rawCalls int
}

func (c *rawOnlyExecutionContext) DoRaw(
	method, path string,
	query map[string]string,
	header map[string]string,
	body []byte,
) (tlsapi.Response, error) {
	c.rawCalls++
	return tlsapi.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Tls-Requestid": []string{"req-raw-only"}},
		Body:       []byte(`{"ok":true}`),
	}, nil
}

func (*rawOnlyExecutionContext) Do(
	string,
	string,
	map[string]string,
	map[string]string,
	[]byte,
) (any, error) {
	panic("Context.Do must never be called by execution transport")
}

func TestContextExecutionTransportUsesDoRawNeverDo(t *testing.T) {
	raw := &rawOnlyExecutionContext{}
	transport := newContextExecutionTransport(raw)
	result, err := execution.NewExecutor(transport, execution.NewCodecRegistry()).Execute(
		context.Background(),
		execution.Invocation{
			Operation: contract.Operation{
				ID: "project.describe-projects",
				Wire: contract.WireSpec{
					Method:        "GET",
					Path:          "/DescribeProjects",
					RequestFormat: "json",
					Codec:         contract.CodecJSON,
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if raw.rawCalls != 1 {
		t.Fatalf("DoRaw calls = %d, want 1", raw.rawCalls)
	}
	if result.RequestID != "req-raw-only" || result.StatusCode != http.StatusOK {
		t.Fatalf("result metadata = %#v", result)
	}
}

func TestContextExecutionTransportPreservesCallerContext(t *testing.T) {
	type contextKey string
	const markerKey contextKey = "marker"

	raw := &contextAwareRawExecutionContext{contextKey: markerKey}
	transport := newContextExecutionTransport(raw)
	ctx := context.WithValue(context.Background(), markerKey, "preserved")
	if _, err := transport.Do(ctx, execution.Request{Method: http.MethodGet, Path: "/DescribeProjects"}); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if raw.contextValue != "preserved" {
		t.Fatalf("caller context value = %v, want preserved", raw.contextValue)
	}
	if raw.backgroundCalls != 0 {
		t.Fatalf("background DoRaw calls = %d, want 0", raw.backgroundCalls)
	}
}

func TestContextExecutionTransportCopiesRequestAndResponseBuffers(t *testing.T) {
	raw := &capturingRawExecutionContext{}
	transport := newContextExecutionTransport(raw)
	request := execution.Request{
		Method:     "POST",
		Path:       "/CreateProject",
		Query:      map[string]string{"a": "1"},
		QueryMulti: map[string][]string{"SpanIds": {"span-1", "span-2"}},
		Header:     map[string]string{"X-Test": "yes"},
		Body:       []byte(`{"name":"demo"}`),
	}
	response, err := transport.Do(context.Background(), request)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	request.Query["a"] = "changed"
	request.QueryMulti["SpanIds"][0] = "changed"
	request.Header["X-Test"] = "changed"
	request.Body[0] = '!'
	if raw.query["a"] != "1" || raw.path != "/CreateProject?SpanIds=span-1&SpanIds=span-2" || raw.header["X-Test"] != "yes" || raw.body[0] != '{' {
		t.Fatalf("adapter retained caller-owned request: query=%#v header=%#v body=%q", raw.query, raw.header, raw.body)
	}
	response.Header.Set("X-Test", "changed")
	response.Body[0] = '!'
	if raw.response.Header.Get("X-Test") != "response" || raw.response.Body[0] != '{' {
		t.Fatalf("adapter exposed context response buffers")
	}
}

func TestContextExecutionTransportRejectsNilRawContext(t *testing.T) {
	transport := newContextExecutionTransport(nil)
	_, err := transport.Do(context.Background(), execution.Request{})
	if err == nil || !strings.Contains(err.Error(), "nil raw execution context") {
		t.Fatalf("error = %v", err)
	}

	var typedNil *rawOnlyExecutionContext
	transport = newContextExecutionTransport(typedNil)
	_, err = transport.Do(context.Background(), execution.Request{})
	if err == nil || !strings.Contains(err.Error(), "nil raw execution context") {
		t.Fatalf("typed-nil error = %v", err)
	}
}

type capturingRawExecutionContext struct {
	path     string
	query    map[string]string
	header   map[string]string
	body     []byte
	response tlsapi.Response
}

type contextAwareRawExecutionContext struct {
	contextKey      any
	contextValue    any
	backgroundCalls int
}

func (c *contextAwareRawExecutionContext) DoRaw(
	string,
	string,
	map[string]string,
	map[string]string,
	[]byte,
) (tlsapi.Response, error) {
	c.backgroundCalls++
	return tlsapi.Response{}, nil
}

func (c *contextAwareRawExecutionContext) doRaw(
	ctx context.Context,
	_, _ string,
	_ map[string]string,
	_ map[string]string,
	_ []byte,
) (tlsapi.Response, error) {
	c.contextValue = ctx.Value(c.contextKey)
	return tlsapi.Response{StatusCode: http.StatusOK}, nil
}

func (c *capturingRawExecutionContext) DoRaw(
	_ string,
	path string,
	query map[string]string,
	header map[string]string,
	body []byte,
) (tlsapi.Response, error) {
	c.path = path
	c.query = query
	c.header = header
	c.body = body
	c.response = tlsapi.Response{
		StatusCode: http.StatusAccepted,
		Header:     http.Header{"X-Test": []string{"response"}},
		Body:       []byte(`{"accepted":true}`),
	}
	return c.response, nil
}
