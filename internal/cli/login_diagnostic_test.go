package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth/console"
)

func TestRunLoginPreservesSafeOAuthDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, oauthCode, requestID, wantCode, wantRequestID string
		status                                              int
	}{
		{"rate limit", "temporarily_unavailable", "request-123", "temporarily_unavailable", "request-123", 429},
		{"forbidden", "access_denied", "request-456", "access_denied", "request-456", 403},
		{"untrusted fields", "secret-code-canary", "secret-request\ncanary", "unknown error", "", 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			apiErr := &console.ConsoleOAuthAPIError{
				StatusCode: tc.status, RequestID: tc.requestID,
				Response: console.ConsoleOAuthErrorResponse{Error: tc.oauthCode, ErrorDescription: "secret-description-canary"},
			}
			// Even an unsafe intermediate wrapper must never be rendered.
			cause := fmt.Errorf("secret-wrapper-canary: %w", apiErr)
			factory := func(c *Context) (*loginAdapter, error) {
				return &loginAdapter{loginSvc: &fakeLoginService{err: cause}, stdout: c.Stdout, stderr: c.Stderr}, nil
			}
			var stdout, stderr bytes.Buffer
			if code := runWithLoginAdapterFactory([]string{"login", "--profile", "default"}, &stdout, &stderr, factory, nil); code != 2 {
				t.Fatalf("exit = %d, want 2", code)
			}
			var payload errPayload
			if err := json.Unmarshal(stderr.Bytes(), &payload); err != nil {
				t.Fatalf("decode error JSON: %v", err)
			}
			if stdout.Len() != 0 || payload.Kind != "auth" || payload.StatusCode != tc.status || payload.RequestID != tc.wantRequestID {
				t.Fatalf("unexpected output: stdout=%q payload=%+v", stdout.String(), payload)
			}
			if !strings.Contains(payload.ErrorMessage, "console login failed") || !strings.Contains(payload.ErrorMessage, tc.wantCode) {
				t.Fatalf("missing safe diagnostic: %q", payload.ErrorMessage)
			}
			for _, secret := range []string{"secret-description-canary", "secret-wrapper-canary", "secret-code-canary", "secret-request"} {
				if strings.Contains(stderr.String(), secret) {
					t.Fatalf("error output leaked %q", secret)
				}
			}
			if strings.Contains(payload.Hint, "--trace-dir") || strings.Contains(payload.Hint, "--dry-run") {
				t.Fatalf("unsupported login hint: %q", payload.Hint)
			}
			if tc.status == 429 && !strings.Contains(payload.Hint, "rate limit") {
				t.Fatalf("missing rate limit hint: %q", payload.Hint)
			}
			wrapped := newConsoleLoginError(false, cause)
			var got *console.ConsoleOAuthAPIError
			if !errors.As(wrapped, &got) || got != apiErr {
				t.Fatal("underlying OAuth error was lost")
			}
		})
	}
}

func TestLoginDiagnosticPreservesUnknownCauseRedaction(t *testing.T) {
	cause := errors.New("secret-token-canary")
	err := newConsoleLoginError(false, cause)
	if err.Error() != "console login failed" || !errors.Is(err, cause) {
		t.Fatal("unknown causes must remain redacted and unwrap-able")
	}
	err = newConsoleLoginError(false, context.Canceled)
	if !strings.Contains(err.Error(), "cancel") || !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation must be safe and actionable")
	}
}
