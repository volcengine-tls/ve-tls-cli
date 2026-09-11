package runtime

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/execution"
	"github.com/volcengine-tls/ve-tls-cli/internal/tlsapi"
)

type contextKey string

type captureRoundTripper struct {
	gotContextValue any
	requests        int
}

func (r *captureRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	r.requests++
	r.gotContextValue = req.Context().Value(contextKey("marker"))
	return &http.Response{
		StatusCode: http.StatusAccepted,
		Header: http.Header{
			"X-Tls-Requestid": []string{"request-id"},
			"X-Custom":        []string{"value"},
		},
		Body: io.NopCloser(strings.NewReader(`{"ok":true}`)),
	}, nil
}

func TestTransportPropagatesContextAndMapsResponse(t *testing.T) {
	client, err := tlsapi.New("https://example.invalid", "cn-test", "", "ak", "sk", "", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	rt := &captureRoundTripper{}
	client.HTTP = &http.Client{Transport: rt}
	sourceCalls := 0
	transport := NewTransport(func() (*tlsapi.Client, error) {
		sourceCalls++
		return client, nil
	})
	ctx := context.WithValue(context.Background(), contextKey("marker"), "preserved")
	request := execution.Request{
		Method: http.MethodPost,
		Path:   "/path",
		Query:  map[string]string{"q": "value"},
		Header: map[string]string{"X-Test": "value"},
		Body:   []byte(`{"request":true}`),
	}
	response, err := transport.Do(ctx, request)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if sourceCalls != 1 || rt.requests != 1 {
		t.Fatalf("source calls=%d requests=%d", sourceCalls, rt.requests)
	}
	if rt.gotContextValue != "preserved" {
		t.Fatalf("context value=%v", rt.gotContextValue)
	}
	if response.StatusCode != http.StatusAccepted ||
		response.Header.Get("X-Tls-Requestid") != "request-id" ||
		string(response.Body) != `{"ok":true}` {
		t.Fatalf("response=%+v", response)
	}
	response.Header.Set("X-Custom", "mutated")
	response.Body[0] = '!'
	if got := response.Header.Get("X-Custom"); got != "mutated" {
		t.Fatalf("response header mutation=%q", got)
	}
}

func TestTransportClientErrorPreventsHTTP(t *testing.T) {
	want := errors.New("client unavailable")
	transport := NewTransport(func() (*tlsapi.Client, error) { return nil, want })
	_, err := transport.Do(context.Background(), execution.Request{Method: http.MethodGet, Path: "/x"})
	if !errors.Is(err, want) {
		t.Fatalf("Do error=%v", err)
	}
}

func TestTransportRejectsNilSourceAndNilClient(t *testing.T) {
	_, err := NewTransport(nil).Do(context.Background(), execution.Request{})
	if err == nil || err.Error() != "nil client source" {
		t.Fatalf("nil source error=%v", err)
	}
	_, err = NewTransport(func() (*tlsapi.Client, error) { return nil, nil }).Do(context.Background(), execution.Request{})
	if err == nil || err.Error() != "nil TLS client" {
		t.Fatalf("nil client error=%v", err)
	}
}

func TestTransportDoRaw(t *testing.T) {
	client, err := tlsapi.New("https://example.invalid", "cn-test", "", "ak", "sk", "", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	rt := &captureRoundTripper{}
	client.HTTP = &http.Client{Transport: rt}
	transport := NewTransport(func() (*tlsapi.Client, error) { return client, nil })
	response, err := transport.DoRaw(context.Background(), http.MethodGet, "/raw", nil, nil, nil)
	if err != nil {
		t.Fatalf("DoRaw: %v", err)
	}
	if response.StatusCode != http.StatusAccepted ||
		response.Header.Get("X-Tls-Requestid") != "request-id" ||
		string(response.Body) != `{"ok":true}` {
		t.Fatalf("response=%+v", response)
	}
}

var _ execution.Transport = (*Transport)(nil)
