package console

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"testing"
)

func TestDiagnoseErrorPrefersOuterSafePhaseAndPreservesAPIMetadata(t *testing.T) {
	cases := []struct {
		name       string
		statusCode int
		requestID  string
	}{
		{name: "too many requests", statusCode: 429, requestID: "req-429"},
		{name: "forbidden", statusCode: 403, requestID: "req-403"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			apiErr := &ConsoleOAuthAPIError{
				StatusCode: tc.statusCode,
				Response: ConsoleOAuthErrorResponse{
					Error:            "access_denied",
					ErrorDescription: "secret-description",
				},
				RequestID: tc.requestID,
			}
			err := newSafeError("authorization code exchange failed", apiErr)

			got := DiagnoseError(err)
			wantMessage := "authorization code exchange failed: " + apiErr.Error()
			if got.Message != wantMessage {
				t.Fatalf("Message = %q, want outer phase plus safe API reason %q", got.Message, wantMessage)
			}
			if got.StatusCode != tc.statusCode {
				t.Errorf("StatusCode = %d, want %d", got.StatusCode, tc.statusCode)
			}
			if got.RequestID != tc.requestID {
				t.Errorf("RequestID = %q, want %q", got.RequestID, tc.requestID)
			}
			if strings.Contains(got.Message, "secret-description") {
				t.Errorf("diagnostic leaks API description: %q", got.Message)
			}
		})
	}
}

func TestDiagnoseErrorComposesOuterBrowserPhaseWithCallbackAndTimeout(t *testing.T) {
	t.Run("callback oauth error", func(t *testing.T) {
		callbackErr := &callbackError{kind: callbackOAuthError, oauthCode: "access_denied"}
		err := newSafeError("browser authorization failed", callbackErr)
		got := DiagnoseError(err)
		want := "browser authorization failed: " + callbackErr.Error()
		if got.Message != want {
			t.Fatalf("Message = %q, want %q", got.Message, want)
		}
	})

	t.Run("callback wait timeout", func(t *testing.T) {
		err := newSafeError("browser authorization failed",
			newSafeError("wait for callback failed", context.DeadlineExceeded),
		)
		got := DiagnoseError(err)
		want := "browser authorization failed: waiting for browser callback timed out"
		if got.Message != want {
			t.Fatalf("Message = %q, want %q", got.Message, want)
		}
	})
}

func TestDiagnoseErrorRejectsUnsafeAPIFields(t *testing.T) {
	const secret = "api-secret-description"
	apiErr := &ConsoleOAuthAPIError{
		StatusCode: 403,
		Response: ConsoleOAuthErrorResponse{
			Error:            "evil-code\n" + secret,
			ErrorDescription: secret,
		},
		RequestID: "request id\n" + secret,
	}

	got := DiagnoseError(apiErr)
	if got.StatusCode != 403 {
		t.Errorf("StatusCode = %d, want 403", got.StatusCode)
	}
	if got.RequestID != "" {
		t.Errorf("RequestID = %q, want empty for unsafe request ID", got.RequestID)
	}
	if strings.Contains(got.Message, secret) || strings.Contains(got.Message, "evil-code") {
		t.Fatalf("diagnostic leaks unsafe API fields: %q", got.Message)
	}
	if want := apiErr.Error(); got.Message != want {
		t.Errorf("Message = %q, want safe API Error() %q", got.Message, want)
	}
}

func TestDiagnoseErrorCallbackWaitAndContext(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "callback deadline",
			err:  newSafeError("wait for callback failed", context.DeadlineExceeded),
			want: "wait for callback failed: waiting for browser callback timed out",
		},
		{
			name: "general deadline",
			err:  context.DeadlineExceeded,
			want: "console login timed out",
		},
		{
			name: "canceled",
			err:  newSafeError("context already done", context.Canceled),
			want: "context already done: console login canceled",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DiagnoseError(tc.err)
			if got.Message != tc.want {
				t.Errorf("Message = %q, want %q", got.Message, tc.want)
			}
		})
	}
}

func TestDiagnoseErrorDoesNotAttributeJoinedCleanupDeadlineToCallbackWait(t *testing.T) {
	err := newSafeError("authorization and cleanup failed",
		newSafeError("wait for callback failed", errors.New("callback wait failed for another reason")),
		context.DeadlineExceeded,
	)
	got := DiagnoseError(err)
	want := "authorization and cleanup failed: console login timed out"
	if got.Message != want {
		t.Fatalf("Message = %q, want %q", got.Message, want)
	}
}

func TestDiagnoseErrorClassifiesNetworkDNSTLSWithoutCause(t *testing.T) {
	const secret = "https://signin.example.com/?access_token=" + "secret-token"
	tlsErr := tls.AlertError(42)
	cases := []struct {
		name string
		err  error
	}{
		{
			name: "network",
			err:  &net.OpError{Op: "dial", Net: "tcp", Source: nil, Addr: nil, Err: errors.New(secret)},
		},
		{
			name: "dns",
			err:  &net.DNSError{Err: secret, Name: "secret.example.com"},
		},
		{
			name: "url transport",
			err:  &url.Error{Op: "Get", URL: secret, Err: errors.New("transport secret")},
		},
		{
			name: "tls",
			err:  fmt.Errorf("handshake: %w", tlsErr),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DiagnoseError(tc.err)
			if got.Message == "" {
				t.Fatal("Message is empty for a known network failure")
			}
			if strings.Contains(got.Message, secret) || strings.Contains(got.Message, "transport secret") {
				t.Fatalf("diagnostic leaks network cause: %q", got.Message)
			}
			if got.StatusCode != 0 || got.RequestID != "" {
				t.Errorf("network diagnostic metadata = %+v, want empty metadata", got)
			}
		})
	}
}

func TestDiagnoseErrorUnknownIsEmpty(t *testing.T) {
	const secret = "unknown-secret-token"
	got := DiagnoseError(errors.New(secret))
	if got != (ErrorDiagnostic{}) {
		t.Fatalf("unknown diagnostic = %+v, want zero value", got)
	}
}

func TestDiagnoseErrorDoesNotTrustUnknownSafePhase(t *testing.T) {
	const secret = "unsafe-phase-secret"
	got := DiagnoseError(&safeError{desc: secret, causes: []error{errors.New("cause")}})
	if got != (ErrorDiagnostic{}) {
		t.Fatalf("unknown safe phase diagnostic = %+v, want zero value", got)
	}
}
