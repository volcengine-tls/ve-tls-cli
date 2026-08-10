//go:build human

package cli

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth"
	"github.com/volcengine-tls/ve-tls-cli/internal/output"
	"github.com/volcengine-tls/ve-tls-cli/internal/tlsapi"
)

type assistantCaptureRT struct {
	mu       sync.Mutex
	requests []*http.Request
}

func (rt *assistantCaptureRT) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.mu.Lock()
	rt.requests = append(rt.requests, req.Clone(req.Context()))
	rt.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
		Body:       http.NoBody,
	}, nil
}

func (rt *assistantCaptureRT) snapshot() []*http.Request {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	out := make([]*http.Request, len(rt.requests))
	copy(out, rt.requests)
	return out
}

type assistantFakeProvider struct {
	calls int
}

func (p *assistantFakeProvider) Retrieve(context.Context) (auth.Value, error) {
	p.calls++
	return auth.Value{
		AccessKeyID:     "assistant-ak",
		SecretAccessKey: "assistant-sk",
		SessionToken:    "assistant-token",
	}, nil
}

// TestAssistantStreamUsesClientSign proves assistant.doStream signs the request
// through Client.Sign/Provider rather than the legacy c.Creds.Sign path.
func TestAssistantStreamUsesClientSign(t *testing.T) {
	provider := &assistantFakeProvider{}
	cl, err := tlsapi.NewWithProvider("https://tls-cn-beijing.volces.com", "cn-beijing", provider, time.Second)
	if err != nil {
		t.Fatalf("NewWithProvider: %v", err)
	}
	rt := &assistantCaptureRT{}
	cl.HTTP = &http.Client{Transport: rt, Timeout: time.Second}

	ctx := newContext(&bytes.Buffer{}, &bytes.Buffer{}, output.FormatJSON, "", "")
	ctx.client = cl

	if _, err := assistantStreamAnswer(ctx, "instance-1", "topic-1", "session-1", "what is TLS", "Text2Tls"); err != nil {
		t.Fatalf("assistantStreamAnswer: %v", err)
	}

	if provider.calls != 1 {
		t.Fatalf("provider retrieve calls = %d, want 1 (stream must use Client.Sign)", provider.calls)
	}

	reqs := rt.snapshot()
	if len(reqs) != 1 {
		t.Fatalf("captured %d requests, want 1", len(reqs))
	}
	req := reqs[0]

	authz := req.Header.Get("Authorization")
	if !strings.Contains(authz, "Credential=assistant-ak/") {
		t.Fatalf("stream request not signed with provider identity: %q", authz)
	}
	if got, want := req.Header.Get("X-Security-Token"), "assistant-token"; got != want {
		t.Fatalf("stream X-Security-Token = %q, want %q", got, want)
	}
	if !strings.Contains(authz, "/TLS/request") {
		t.Fatalf("stream credential scope must use Service=TLS: %q", authz)
	}
}

// assistantFailingProvider returns a configurable error from Retrieve so tests
// can prove provider failures propagate and never reach the transport.
type assistantFailingProvider struct {
	err error
}

func (p *assistantFailingProvider) Retrieve(context.Context) (auth.Value, error) {
	return auth.Value{}, p.err
}

// TestAssistantProviderFailureDoesNotHitTransport proves that when the provider
// returns a sentinel error, the error is propagatable via errors.Is and the
// RoundTripper is never invoked (fail-closed before signing/HTTP).
func TestAssistantProviderFailureDoesNotHitTransport(t *testing.T) {
	sentinel := &auth.Error{Kind: auth.ReauthRequired, Description: "reauth required"}
	provider := &assistantFailingProvider{err: sentinel}
	cl, err := tlsapi.NewWithProvider("https://tls-cn-beijing.volces.com", "cn-beijing", provider, time.Second)
	if err != nil {
		t.Fatalf("NewWithProvider: %v", err)
	}
	rt := &assistantCaptureRT{}
	cl.HTTP = &http.Client{Transport: rt, Timeout: time.Second}

	ctx := newContext(&bytes.Buffer{}, &bytes.Buffer{}, output.FormatJSON, "", "")
	ctx.client = cl

	_, err = assistantStreamAnswer(ctx, "instance-1", "topic-1", "session-1", "what is TLS", "Text2Tls")
	if err == nil {
		t.Fatalf("expected error from failing provider, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("error %v not matchable to sentinel via errors.Is", err)
	}
	if got := len(rt.snapshot()); got != 0 {
		t.Fatalf("round tripper called %d times, want 0", got)
	}
}
