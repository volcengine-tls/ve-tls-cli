package runtime

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth"
	"github.com/volcengine-tls/ve-tls-cli/internal/execution"
	"github.com/volcengine-tls/ve-tls-cli/internal/tlsapi"
)

func assertTransportFailure(t *testing.T, err error, stage string) {
	t.Helper()
	var failure *execution.Failure
	if !errors.As(err, &failure) || failure.RemoteOutcome() != execution.OutcomeNotSent || failure.FailureStage() != stage {
		t.Fatalf("unexpected transport preflight failure: %#v", err)
	}
}

func TestTransportPreflightFailuresAreNotSent(t *testing.T) {
	authCause := &auth.Error{Kind: auth.ReauthRequired}
	tests := []struct {
		name   string
		source ClientSource
		want   string
	}{
		{name: "nil source", want: "nil client source"},
		{name: "nil client", source: func() (*tlsapi.Client, error) { return nil, nil }, want: "nil TLS client"},
		{name: "source auth", source: func() (*tlsapi.Client, error) { return nil, authCause }, want: "TLS client resolution failed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewTransport(tc.source).DoRaw(context.Background(), http.MethodPost, "/", nil, nil, nil)
			assertTransportFailure(t, err, "resolve_client")
			if err.Error() != tc.want {
				t.Fatalf("error=%q, want %q", err.Error(), tc.want)
			}
			if tc.name == "source auth" {
				var actual *auth.Error
				if !errors.As(err, &actual) || actual != authCause {
					t.Fatalf("auth error lost: %v", err)
				}
			}
		})
	}
}

func TestTransportPreflightMessageDoesNotExposeCause(t *testing.T) {
	cause := errors.New("https://user:secret@example.invalid/?token=secret")
	transport := NewTransport(func() (*tlsapi.Client, error) { return nil, cause })
	_, err := transport.DoRaw(context.Background(), http.MethodPost, "/", nil, nil, nil)
	assertTransportFailure(t, err, "resolve_client")
	if err.Error() != "TLS client resolution failed" || !errors.Is(err, cause) {
		t.Fatalf("source error=%v", err)
	}
	_, err = transport.Do(context.Background(), execution.Request{
		Method: http.MethodPost, Path: "/%zz?token=secret", QueryMulti: map[string][]string{"k": {"v"}},
	})
	assertTransportFailure(t, err, "build_request")
	if err.Error() != "request construction failed" {
		t.Fatalf("query error=%v", err)
	}
}

func TestTransportMalformedMultiQueryStopsBeforeSource(t *testing.T) {
	calls := 0
	transport := NewTransport(func() (*tlsapi.Client, error) {
		calls++
		return nil, nil
	})
	_, err := transport.Do(context.Background(), execution.Request{
		Method: http.MethodPost, Path: "/%zz", QueryMulti: map[string][]string{"k": {"v"}},
	})
	assertTransportFailure(t, err, "build_request")
	if calls != 0 {
		t.Fatalf("source called %d times", calls)
	}
}

type failingTransportBody struct{ cause error }

func (b failingTransportBody) Read(p []byte) (int, error) { return copy(p, "partial"), b.cause }
func (b failingTransportBody) Close() error               { return nil }

type partialResponseTransport struct{ cause error }

func (t partialResponseTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"X-Tls-Requestid": {"request-id"}},
		Body: failingTransportBody{cause: t.cause}, Request: req}, nil
}

func TestTransportMapsPartialResponseOnReadFailure(t *testing.T) {
	cause := errors.New("read failed")
	client, err := tlsapi.New("https://example.invalid", "cn-test", "", "fake-ak", "fake-sk", "", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	client.HTTP = &http.Client{Transport: partialResponseTransport{cause: cause}}
	response, err := NewTransport(func() (*tlsapi.Client, error) { return client, nil }).Do(context.Background(),
		execution.Request{Method: http.MethodPost, Path: "/"})
	if !errors.Is(err, cause) || execution.ErrorOutcome(err, "") != execution.OutcomeUnknown ||
		response.StatusCode != http.StatusOK || response.Header.Get("X-Tls-Requestid") != "request-id" ||
		string(response.Body) != "partial" {
		t.Fatalf("response=%+v error=%v", response, err)
	}
}

var _ io.ReadCloser = failingTransportBody{}
