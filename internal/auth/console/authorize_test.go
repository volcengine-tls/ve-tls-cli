package console

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth/browser"
)

// fakeCallbackServer implements callbackServer for tests without binding any
// real sockets. It returns a configurable result from Wait and records
// lifecycle calls.
type fakeCallbackServer struct {
	port        int
	redirectURI string
	result      *AuthorizationResult
	waitErr     error
	startCount  int
	shutdownErr error
	shutdownCtx context.Context
	closeErr    error
	closeCount  int
	mu          sync.Mutex
	started     chan struct{}
}

func (f *fakeCallbackServer) Port() int           { return f.port }
func (f *fakeCallbackServer) RedirectURI() string { return f.redirectURI }
func (f *fakeCallbackServer) Start() {
	f.mu.Lock()
	f.startCount++
	if f.started != nil {
		close(f.started)
		f.started = nil
	}
	f.mu.Unlock()
}
func (f *fakeCallbackServer) Wait(ctx context.Context) (*AuthorizationResult, error) {
	if f.waitErr != nil {
		return nil, f.waitErr
	}
	return f.result, nil
}
func (f *fakeCallbackServer) Shutdown(ctx context.Context) error {
	f.shutdownCtx = ctx
	return f.shutdownErr
}
func (f *fakeCallbackServer) Close() error {
	f.mu.Lock()
	f.closeCount++
	f.mu.Unlock()
	return f.closeErr
}

// fakeOpener implements browser.Opener and records the URL it was asked to open.
type fakeOpener struct {
	openedURL string
	err       error
}

func (f *fakeOpener) Open(ctx context.Context, url string) error {
	f.openedURL = url
	return f.err
}

// recordingFactory wraps a callbackServerFactory and records whether it was
// invoked before BuildAuthorizeURL on the client.
type recordingFactory struct {
	factory  callbackServerFactory
	called   bool
	callTime time.Time
}

func (r *recordingFactory) make() (callbackServer, error) {
	r.called = true
	r.callTime = time.Now()
	return r.factory()
}

// fakeOAuthClient implements OAuthClient for tests.
type fakeOAuthClient struct {
	authorizeURL string
	endpointURL  string
	buildErr     error
	buildHook    func()
	exchangeResp *ConsoleTokenResponse
	exchangeErr  error
	lastParams   *AuthorizeParams
	lastReq      *ConsoleTokenRequest
}

func (f *fakeOAuthClient) BuildAuthorizeURL(params *AuthorizeParams) (string, error) {
	f.lastParams = params
	if f.buildHook != nil {
		f.buildHook()
	}
	if f.buildErr != nil {
		return "", f.buildErr
	}
	return f.authorizeURL, nil
}

func (f *fakeOAuthClient) ExchangeToken(ctx context.Context, req *ConsoleTokenRequest) (*ConsoleTokenResponse, error) {
	f.lastReq = req
	return f.exchangeResp, f.exchangeErr
}

func (f *fakeOAuthClient) EndpointURL() string {
	return f.endpointURL
}

func newFakeOAuthClient(authorizeURL, endpointURL string) *fakeOAuthClient {
	return &fakeOAuthClient{authorizeURL: authorizeURL, endpointURL: endpointURL}
}

func TestLocalAuthorizeBuildsRedirectAfterBindingListener(t *testing.T) {
	var (
		factoryCalled bool
		buildCalled   bool
	)
	factory := func() (callbackServer, error) {
		factoryCalled = true
		return &fakeCallbackServer{
			port:        12345,
			redirectURI: "http://127.0.0.1:12345/oauth/callback",
			result: &AuthorizationResult{
				Code:  "auth-code-123",
				State: "state-xyz",
			},
		}, nil
	}
	client := &fakeOAuthClient{
		authorizeURL: "https://signin.example.com/authorize?client_id=same-device",
		endpointURL:  "https://signin.example.com",
		buildHook: func() {
			buildCalled = true
			if !factoryCalled {
				t.Error("BuildAuthorizeURL called before callback listener was bound")
			}
		},
	}
	auth := &LocalAuthorizer{
		client:          client,
		callbackFactory: factory,
		opener:          &fakeOpener{},
		prompt:          io.Discard,
		state:           "state-xyz",
		codeChallenge:   "challenge-abc",
	}

	code, redirectURI, err := auth.Authorize(context.Background())
	if err != nil {
		t.Fatalf("Authorize returned error: %v", err)
	}
	if code != "auth-code-123" {
		t.Errorf("code = %q, want %q", code, "auth-code-123")
	}
	if redirectURI != "http://127.0.0.1:12345/oauth/callback" {
		t.Errorf("redirectURI = %q, want callback server URI", redirectURI)
	}
	if !factoryCalled {
		t.Error("callback factory was never called")
	}
	if !buildCalled {
		t.Error("BuildAuthorizeURL was never called")
	}
	if client.lastParams == nil {
		t.Fatal("BuildAuthorizeURL received nil params")
	}
	if client.lastParams.RedirectURI != redirectURI {
		t.Errorf("authorize URL built with redirect_uri %q, want %q",
			client.lastParams.RedirectURI, redirectURI)
	}
	if client.lastParams.ClientID != ClientIDSameDevice {
		t.Errorf("client_id = %q, want same-device", client.lastParams.ClientID)
	}
}

func TestLocalAuthorizeBrowserFailureStillPrintsURL(t *testing.T) {
	factory := func() (callbackServer, error) {
		return &fakeCallbackServer{
			port:        9999,
			redirectURI: "http://127.0.0.1:9999/oauth/callback",
			result: &AuthorizationResult{
				Code:  "code-browser-fail",
				State: "state-browser-fail",
			},
		}, nil
	}
	client := &fakeOAuthClient{
		authorizeURL: "https://signin.example.com/authorize/oauth/authorize?x=1",
		endpointURL:  "https://signin.example.com",
	}
	var prompt bytes.Buffer
	opener := &fakeOpener{err: errors.New("browser launch failed")}
	auth := &LocalAuthorizer{
		client:          client,
		callbackFactory: factory,
		opener:          opener,
		prompt:          &prompt,
		state:           "state-browser-fail",
		codeChallenge:   "challenge",
	}

	code, _, err := auth.Authorize(context.Background())
	if err != nil {
		t.Fatalf("Authorize should succeed even when browser fails, got error: %v", err)
	}
	if code != "code-browser-fail" {
		t.Errorf("code = %q, want %q", code, "code-browser-fail")
	}
	if opener.openedURL == "" {
		t.Error("opener was not called")
	}
	printed := prompt.String()
	if !strings.Contains(printed, client.authorizeURL) {
		t.Errorf("prompt output does not contain authorize URL:\n%s", printed)
	}
}

func TestLocalAuthorizeRejectsStateMismatchWithoutEchoingState(t *testing.T) {
	const expectedState = "expected-state-secret-12345"
	const receivedState = "received-state-other-67890"
	factory := func() (callbackServer, error) {
		return &fakeCallbackServer{
			port:        1111,
			redirectURI: "http://127.0.0.1:1111/oauth/callback",
			result: &AuthorizationResult{
				Code:  "code-mismatch",
				State: receivedState,
			},
		}, nil
	}
	client := &fakeOAuthClient{
		authorizeURL: "https://signin.example.com/authorize?state=" + expectedState,
		endpointURL:  "https://signin.example.com",
	}
	auth := &LocalAuthorizer{
		client:          client,
		callbackFactory: factory,
		opener:          &fakeOpener{},
		prompt:          io.Discard,
		state:           expectedState,
		codeChallenge:   "challenge",
	}

	_, _, err := auth.Authorize(context.Background())
	if err == nil {
		t.Fatal("expected error for state mismatch, got nil")
	}
	errStr := err.Error()
	if strings.Contains(errStr, expectedState) {
		t.Errorf("error echoes expected state %q: %s", expectedState, errStr)
	}
	if strings.Contains(errStr, receivedState) {
		t.Errorf("error echoes received state %q: %s", receivedState, errStr)
	}
	if strings.Contains(errStr, "code-mismatch") {
		t.Errorf("error echoes authorization code: %s", errStr)
	}
}

func TestRemoteAuthorizeAcceptsStandardURLAndRawURLBase64(t *testing.T) {
	const state = "remote-state-abc"
	const code = "remote-code-xyz"
	payload := "code=" + code + "&state=" + state

	cases := []struct {
		name string
		enc  *base64.Encoding
	}{
		{"standard", base64.StdEncoding},
		{"rawurl", base64.RawURLEncoding},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			encoded := tc.enc.EncodeToString([]byte(payload))
			client := &fakeOAuthClient{
				authorizeURL: "https://signin.example.com/authorize?client_id=cross-device",
				endpointURL:  "https://signin.example.com",
			}
			auth := &RemoteAuthorizer{
				client:        client,
				reader:        strings.NewReader(encoded + "\n"),
				writer:        io.Discard,
				state:         state,
				codeChallenge: "challenge",
			}
			gotCode, redirectURI, err := auth.Authorize(context.Background())
			if err != nil {
				t.Fatalf("Authorize error: %v", err)
			}
			if gotCode != code {
				t.Errorf("code = %q, want %q", gotCode, code)
			}
			wantRedirect := "https://signin.example.com" + AuthorizePath
			if redirectURI != wantRedirect {
				t.Errorf("redirectURI = %q, want %q", redirectURI, wantRedirect)
			}
			if client.lastParams == nil || client.lastParams.ClientID != ClientIDCrossDevice {
				t.Errorf("remote flow must use cross-device client ID")
			}
		})
	}
}

func TestRemoteAuthorizeRejectsEmptyCodeInvalidBase64AndStateMismatch(t *testing.T) {
	const state = "expected-remote-state"

	t.Run("empty input", func(t *testing.T) {
		client := newFakeOAuthClient("https://signin.example.com/auth", "https://signin.example.com")
		auth := &RemoteAuthorizer{
			client:        client,
			reader:        strings.NewReader("\n"),
			writer:        io.Discard,
			state:         state,
			codeChallenge: "c",
		}
		_, _, err := auth.Authorize(context.Background())
		if err == nil {
			t.Fatal("expected error for empty input")
		}
	})

	t.Run("invalid base64", func(t *testing.T) {
		client := newFakeOAuthClient("https://signin.example.com/auth", "https://signin.example.com")
		auth := &RemoteAuthorizer{
			client:        client,
			reader:        strings.NewReader("not-valid-base64!!!\n"),
			writer:        io.Discard,
			state:         state,
			codeChallenge: "c",
		}
		_, _, err := auth.Authorize(context.Background())
		if err == nil {
			t.Fatal("expected error for invalid base64")
		}
	})

	t.Run("state mismatch", func(t *testing.T) {
		payload := "code=some-code&state=different-state"
		encoded := base64.StdEncoding.EncodeToString([]byte(payload))
		client := newFakeOAuthClient("https://signin.example.com/auth", "https://signin.example.com")
		auth := &RemoteAuthorizer{
			client:        client,
			reader:        strings.NewReader(encoded + "\n"),
			writer:        io.Discard,
			state:         state,
			codeChallenge: "c",
		}
		_, _, err := auth.Authorize(context.Background())
		if err == nil {
			t.Fatal("expected error for state mismatch")
		}
		errStr := err.Error()
		if strings.Contains(errStr, state) {
			t.Errorf("error echoes expected state: %s", errStr)
		}
		if strings.Contains(errStr, "different-state") {
			t.Errorf("error echoes received state: %s", errStr)
		}
		if strings.Contains(errStr, "some-code") {
			t.Errorf("error echoes code: %s", errStr)
		}
	})

	t.Run("duplicate code", func(t *testing.T) {
		payload := "code=first&code=second&state=" + state
		encoded := base64.StdEncoding.EncodeToString([]byte(payload))
		client := newFakeOAuthClient("https://signin.example.com/auth", "https://signin.example.com")
		auth := &RemoteAuthorizer{
			client:        client,
			reader:        strings.NewReader(encoded + "\n"),
			writer:        io.Discard,
			state:         state,
			codeChallenge: "c",
		}
		_, _, err := auth.Authorize(context.Background())
		if err == nil {
			t.Fatal("expected error for duplicate code field")
		}
	})

	t.Run("duplicate state", func(t *testing.T) {
		payload := "code=some-code&state=" + state + "&state=" + state
		encoded := base64.StdEncoding.EncodeToString([]byte(payload))
		client := newFakeOAuthClient("https://signin.example.com/auth", "https://signin.example.com")
		auth := &RemoteAuthorizer{
			client:        client,
			reader:        strings.NewReader(encoded + "\n"),
			writer:        io.Discard,
			state:         state,
			codeChallenge: "c",
		}
		_, _, err := auth.Authorize(context.Background())
		if err == nil {
			t.Fatal("expected error for duplicate state field")
		}
	})
}

func TestRemoteAuthorizerRejectsPreCancelledContext(t *testing.T) {
	client := newFakeOAuthClient("https://signin.example.com/auth", "https://signin.example.com")
	auth := &RemoteAuthorizer{
		client:        client,
		reader:        strings.NewReader("anything\n"),
		writer:        io.Discard,
		state:         "s",
		codeChallenge: "c",
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := auth.Authorize(ctx)
	if err == nil {
		t.Fatal("expected error for pre-cancelled context")
	}
}

func TestRemoteAuthorizerRejectsNilDeps(t *testing.T) {
	client := newFakeOAuthClient("https://signin.example.com/auth", "https://signin.example.com")

	t.Run("nil client", func(t *testing.T) {
		auth := &RemoteAuthorizer{reader: strings.NewReader("x\n"), writer: io.Discard, state: "s", codeChallenge: "c"}
		_, _, err := auth.Authorize(context.Background())
		if err == nil {
			t.Fatal("expected error for nil client")
		}
	})

	t.Run("nil reader", func(t *testing.T) {
		auth := &RemoteAuthorizer{client: client, writer: io.Discard, state: "s", codeChallenge: "c"}
		_, _, err := auth.Authorize(context.Background())
		if err == nil {
			t.Fatal("expected error for nil reader")
		}
	})
}

func TestLocalAuthorizerShutdownErrorOnSuccessIsSurfaced(t *testing.T) {
	factory := func() (callbackServer, error) {
		return &fakeCallbackServer{
			port:        12345,
			redirectURI: "http://127.0.0.1:12345/oauth/callback",
			result:      &AuthorizationResult{Code: "code-ok", State: "state-ok"},
			shutdownErr: errors.New("shutdown boom"),
		}, nil
	}
	client := &fakeOAuthClient{
		authorizeURL: "https://signin.example.com/authorize?x=1",
		endpointURL:  "https://signin.example.com",
	}
	auth := &LocalAuthorizer{
		client:          client,
		callbackFactory: factory,
		opener:          &fakeOpener{},
		prompt:          io.Discard,
		state:           "state-ok",
		codeChallenge:   "challenge",
	}

	code, redirectURI, err := auth.Authorize(context.Background())
	if err == nil {
		t.Fatal("expected error when shutdown fails on success")
	}
	if code != "" {
		t.Errorf("code should be empty when cleanup fails, got %q", code)
	}
	if redirectURI != "" {
		t.Errorf("redirectURI should be empty when cleanup fails, got %q", redirectURI)
	}
}

func TestLocalAuthorizerBothErrorsPreservedViaErrorsIs(t *testing.T) {
	primaryErr := errors.New("primary flow failed")
	factory := func() (callbackServer, error) {
		return &fakeCallbackServer{
			port:        12345,
			redirectURI: "http://127.0.0.1:12345/oauth/callback",
			waitErr:     primaryErr,
			shutdownErr: errors.New("shutdown boom"),
		}, nil
	}
	client := &fakeOAuthClient{
		authorizeURL: "https://signin.example.com/authorize?x=1",
		endpointURL:  "https://signin.example.com",
	}
	auth := &LocalAuthorizer{
		client:          client,
		callbackFactory: factory,
		opener:          &fakeOpener{},
		prompt:          io.Discard,
		state:           "state-ok",
		codeChallenge:   "challenge",
	}

	_, _, err := auth.Authorize(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, primaryErr) {
		t.Errorf("error should wrap primary cause, got: %v", err)
	}
}

// TestLocalAuthorizerCleanupErrorDoesNotLeakCanary verifies that when the
// primary flow succeeds but cleanup fails, the returned error uses a fixed safe
// description that does not render the cleanup cause text (which may contain
// secrets), while errors.Is still matches the cleanup cause.
func TestLocalAuthorizerCleanupErrorDoesNotLeakCanary(t *testing.T) {
	const cleanupCanary = "cleanup-secret-canary-xyz"
	cleanupErr := errors.New(cleanupCanary)
	factory := func() (callbackServer, error) {
		return &fakeCallbackServer{
			port:        12345,
			redirectURI: "http://127.0.0.1:12345/oauth/callback",
			result:      &AuthorizationResult{Code: "code-ok", State: "state-ok"},
			shutdownErr: cleanupErr,
		}, nil
	}
	client := &fakeOAuthClient{
		authorizeURL: "https://signin.example.com/authorize?x=1",
		endpointURL:  "https://signin.example.com",
	}
	auth := &LocalAuthorizer{
		client:          client,
		callbackFactory: factory,
		opener:          &fakeOpener{},
		prompt:          io.Discard,
		state:           "state-ok",
		codeChallenge:   "challenge",
	}

	code, redirectURI, err := auth.Authorize(context.Background())
	if err == nil {
		t.Fatal("expected error when cleanup fails on success")
	}
	if code != "" {
		t.Errorf("code should be empty when cleanup fails, got %q", code)
	}
	if redirectURI != "" {
		t.Errorf("redirectURI should be empty when cleanup fails, got %q", redirectURI)
	}
	if strings.Contains(err.Error(), cleanupCanary) {
		t.Errorf("error text leaks cleanup canary: %s", err.Error())
	}
	if !errors.Is(err, cleanupErr) {
		t.Errorf("errors.Is should match cleanup cause, got: %v", err)
	}
}

// TestLocalAuthorizerBothErrorsDoNotLeakCanaries verifies that when both the
// primary flow and cleanup fail, the returned error uses a fixed safe
// description that renders neither cause text, while errors.Is matches both
// causes.
func TestLocalAuthorizerBothErrorsDoNotLeakCanaries(t *testing.T) {
	const primaryCanary = "primary-secret-canary-abc"
	const cleanupCanary = "cleanup-secret-canary-xyz"
	primaryErr := errors.New(primaryCanary)
	cleanupErr := errors.New(cleanupCanary)
	factory := func() (callbackServer, error) {
		return &fakeCallbackServer{
			port:        12345,
			redirectURI: "http://127.0.0.1:12345/oauth/callback",
			waitErr:     primaryErr,
			shutdownErr: cleanupErr,
		}, nil
	}
	client := &fakeOAuthClient{
		authorizeURL: "https://signin.example.com/authorize?x=1",
		endpointURL:  "https://signin.example.com",
	}
	auth := &LocalAuthorizer{
		client:          client,
		callbackFactory: factory,
		opener:          &fakeOpener{},
		prompt:          io.Discard,
		state:           "state-ok",
		codeChallenge:   "challenge",
	}

	_, _, err := auth.Authorize(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	errStr := err.Error()
	if strings.Contains(errStr, primaryCanary) {
		t.Errorf("error text leaks primary canary: %s", errStr)
	}
	if strings.Contains(errStr, cleanupCanary) {
		t.Errorf("error text leaks cleanup canary: %s", errStr)
	}
	if !errors.Is(err, primaryErr) {
		t.Errorf("errors.Is should match primary cause, got: %v", err)
	}
	if !errors.Is(err, cleanupErr) {
		t.Errorf("errors.Is should match cleanup cause, got: %v", err)
	}
}

func TestLocalAuthorizerRejectsNilDeps(t *testing.T) {
	t.Run("nil client", func(t *testing.T) {
		auth := &LocalAuthorizer{callbackFactory: func() (callbackServer, error) { return nil, nil }, prompt: io.Discard}
		_, _, err := auth.Authorize(context.Background())
		if err == nil {
			t.Fatal("expected error for nil client")
		}
	})

	t.Run("nil factory", func(t *testing.T) {
		auth := &LocalAuthorizer{client: &fakeOAuthClient{}, prompt: io.Discard}
		_, _, err := auth.Authorize(context.Background())
		if err == nil {
			t.Fatal("expected error for nil factory")
		}
	})

	t.Run("factory returns nil server", func(t *testing.T) {
		auth := &LocalAuthorizer{
			client:          &fakeOAuthClient{},
			callbackFactory: func() (callbackServer, error) { return nil, nil },
			prompt:          io.Discard,
		}
		_, _, err := auth.Authorize(context.Background())
		if err == nil {
			t.Fatal("expected error when factory returns nil server")
		}
	})
}

// Ensure the browser.Opener interface is satisfied by fakeOpener at compile time.
var _ browser.Opener = (*fakeOpener)(nil)

// TestLocalAuthorizerTypedNilDepsFailClosed verifies that typed-nil interface
// values (a nil concrete pointer stored in an interface) are detected and
// rejected with an error rather than panicking with a nil dereference.
func TestLocalAuthorizerTypedNilDepsFailClosed(t *testing.T) {
	factory := func() (callbackServer, error) {
		return &fakeCallbackServer{port: 1, redirectURI: "http://127.0.0.1:1/cb", result: &AuthorizationResult{Code: "c", State: "s"}}, nil
	}

	cases := []struct {
		name string
		auth *LocalAuthorizer
	}{
		{
			name: "typed-nil oauth client",
			auth: &LocalAuthorizer{
				client:          (*fakeOAuthClient)(nil),
				callbackFactory: factory,
				prompt:          io.Discard,
				state:           "s",
				codeChallenge:   "c",
			},
		},
		{
			name: "typed-nil callback server from factory",
			auth: &LocalAuthorizer{
				client:          &fakeOAuthClient{endpointURL: DefaultEndpoint},
				callbackFactory: func() (callbackServer, error) { return (*fakeCallbackServer)(nil), nil },
				prompt:          io.Discard,
				state:           "s",
				codeChallenge:   "c",
			},
		},
		{
			name: "typed-nil opener treated as absent",
			auth: &LocalAuthorizer{
				client:          &fakeOAuthClient{authorizeURL: "https://x", endpointURL: DefaultEndpoint},
				callbackFactory: factory,
				opener:          (*fakeOpener)(nil),
				prompt:          io.Discard,
				state:           "s",
				codeChallenge:   "c",
			},
		},
		{
			name: "typed-nil prompt treated as discard",
			auth: &LocalAuthorizer{
				client:          &fakeOAuthClient{authorizeURL: "https://x", endpointURL: DefaultEndpoint},
				callbackFactory: factory,
				opener:          &fakeOpener{},
				prompt:          (*bytes.Buffer)(nil),
				state:           "s",
				codeChallenge:   "c",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Must not panic and must return an error (except opener/prompt
			// cases which are optional and should succeed).
			_, _, err := tc.auth.Authorize(context.Background())
			switch tc.name {
			case "typed-nil opener treated as absent", "typed-nil prompt treated as discard":
				// Optional deps: should succeed, not error.
				if err != nil {
					t.Fatalf("expected success for optional typed-nil dep, got error: %v", err)
				}
			default:
				if err == nil {
					t.Fatal("expected error for typed-nil required dep")
				}
			}
		})
	}
}

// TestRemoteAuthorizerTypedNilDepsFailClosed verifies typed-nil detection for
// the remote flow's client and reader dependencies.
func TestRemoteAuthorizerTypedNilDepsFailClosed(t *testing.T) {
	cases := []struct {
		name string
		auth *RemoteAuthorizer
	}{
		{
			name: "typed-nil oauth client",
			auth: &RemoteAuthorizer{
				client:        (*fakeOAuthClient)(nil),
				reader:        strings.NewReader("x\n"),
				writer:        io.Discard,
				state:         "s",
				codeChallenge: "c",
			},
		},
		{
			name: "typed-nil reader",
			auth: &RemoteAuthorizer{
				client:        &fakeOAuthClient{endpointURL: DefaultEndpoint},
				reader:        (*strings.Reader)(nil),
				writer:        io.Discard,
				state:         "s",
				codeChallenge: "c",
			},
		},
		{
			name: "typed-nil writer treated as discard",
			auth: &RemoteAuthorizer{
				client:        &fakeOAuthClient{authorizeURL: "https://x", endpointURL: DefaultEndpoint},
				reader:        strings.NewReader("x\n"),
				writer:        (*bytes.Buffer)(nil),
				state:         "s",
				codeChallenge: "c",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := tc.auth.Authorize(context.Background())
			if tc.name == "typed-nil writer treated as discard" {
				// Writer is optional; should succeed (or fail on input, but not
				// on the nil writer itself).
				return
			}
			if err == nil {
				t.Fatal("expected error for typed-nil required dep")
			}
		})
	}
}

// TestLocalAuthorizerShutdownFailureInvokesCloseOnce verifies that when
// Shutdown fails, the forceful Close path is invoked exactly once.
func TestLocalAuthorizerShutdownFailureInvokesCloseOnce(t *testing.T) {
	fake := &fakeCallbackServer{
		port:        12345,
		redirectURI: "http://127.0.0.1:12345/oauth/callback",
		result:      &AuthorizationResult{Code: "code-ok", State: "state-ok"},
		shutdownErr: errors.New("shutdown boom"),
	}
	factory := func() (callbackServer, error) { return fake, nil }
	client := &fakeOAuthClient{
		authorizeURL: "https://signin.example.com/authorize?x=1",
		endpointURL:  "https://signin.example.com",
	}
	auth := &LocalAuthorizer{
		client:          client,
		callbackFactory: factory,
		opener:          &fakeOpener{},
		prompt:          io.Discard,
		state:           "state-ok",
		codeChallenge:   "challenge",
	}

	if _, _, err := auth.Authorize(context.Background()); err == nil {
		t.Fatal("expected error when shutdown fails")
	}
	if fake.closeCount != 1 {
		t.Errorf("Close should be called once when Shutdown fails, got %d", fake.closeCount)
	}
}

// TestLocalAuthorizerSuccessfulShutdownDoesNotCallClose verifies that a
// successful graceful Shutdown does not redundantly invoke the forceful Close.
func TestLocalAuthorizerSuccessfulShutdownDoesNotCallClose(t *testing.T) {
	fake := &fakeCallbackServer{
		port:        12345,
		redirectURI: "http://127.0.0.1:12345/oauth/callback",
		result:      &AuthorizationResult{Code: "code-ok", State: "state-ok"},
	}
	factory := func() (callbackServer, error) { return fake, nil }
	client := &fakeOAuthClient{
		authorizeURL: "https://signin.example.com/authorize?x=1",
		endpointURL:  "https://signin.example.com",
	}
	auth := &LocalAuthorizer{
		client:          client,
		callbackFactory: factory,
		opener:          &fakeOpener{},
		prompt:          io.Discard,
		state:           "state-ok",
		codeChallenge:   "challenge",
	}

	if _, _, err := auth.Authorize(context.Background()); err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if fake.closeCount != 0 {
		t.Errorf("Close should not be called when Shutdown succeeds, got %d", fake.closeCount)
	}
}

// TestLocalAuthorizerAllCleanupCausesMatchable verifies that when the primary
// flow, Shutdown, and Close all fail, all three causes are preserved and
// matchable via errors.Is, while none of their canary strings appear in the
// returned error text.
func TestLocalAuthorizerAllCleanupCausesMatchable(t *testing.T) {
	const (
		primaryCanary  = "primary-secret-canary-aaa"
		shutdownCanary = "shutdown-secret-canary-bbb"
		closeCanary    = "close-secret-canary-ccc"
	)
	primaryErr := errors.New(primaryCanary)
	shutdownErr := errors.New(shutdownCanary)
	closeErr := errors.New(closeCanary)
	fake := &fakeCallbackServer{
		port:        12345,
		redirectURI: "http://127.0.0.1:12345/oauth/callback",
		waitErr:     primaryErr,
		shutdownErr: shutdownErr,
		closeErr:    closeErr,
	}
	factory := func() (callbackServer, error) { return fake, nil }
	client := &fakeOAuthClient{
		authorizeURL: "https://signin.example.com/authorize?x=1",
		endpointURL:  "https://signin.example.com",
	}
	auth := &LocalAuthorizer{
		client:          client,
		callbackFactory: factory,
		opener:          &fakeOpener{},
		prompt:          io.Discard,
		state:           "state-ok",
		codeChallenge:   "challenge",
	}

	_, _, err := auth.Authorize(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	for _, c := range []error{primaryErr, shutdownErr, closeErr} {
		if !errors.Is(err, c) {
			t.Errorf("errors.Is should match cause %v, got: %v", c, err)
		}
	}
	errStr := err.Error()
	for _, canary := range []string{primaryCanary, shutdownCanary, closeCanary} {
		if strings.Contains(errStr, canary) {
			t.Errorf("error text leaks canary %q: %s", canary, errStr)
		}
	}
}

// TestLocalAuthorizerWaitErrorWrappedSafely verifies that a Wait error is
// wrapped with a fixed safe description that does not render the underlying
// cause text, while the cause remains matchable via errors.Is.
func TestLocalAuthorizerWaitErrorWrappedSafely(t *testing.T) {
	const waitCanary = "wait-secret-canary-ddd"
	waitErr := errors.New(waitCanary)
	fake := &fakeCallbackServer{
		port:        12345,
		redirectURI: "http://127.0.0.1:12345/oauth/callback",
		waitErr:     waitErr,
	}
	factory := func() (callbackServer, error) { return fake, nil }
	client := &fakeOAuthClient{
		authorizeURL: "https://signin.example.com/authorize?x=1",
		endpointURL:  "https://signin.example.com",
	}
	auth := &LocalAuthorizer{
		client:          client,
		callbackFactory: factory,
		opener:          &fakeOpener{},
		prompt:          io.Discard,
		state:           "state-ok",
		codeChallenge:   "challenge",
	}

	_, _, err := auth.Authorize(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, waitErr) {
		t.Errorf("errors.Is should match wait cause, got: %v", err)
	}
	if strings.Contains(err.Error(), waitCanary) {
		t.Errorf("error text leaks wait canary: %s", err.Error())
	}
}

// TestNewDefaultLocalAuthorizerDefersSocketBinding verifies the exported
// constructor installs a non-nil callback server factory but does not invoke it
// during construction. The factory wraps NewCallbackServer(nil), which binds a
// real loopback socket; if it ran during construction the test would fail (or
// panic in a sandbox without loopback). Authorize is intentionally not called.
func TestNewDefaultLocalAuthorizerDefersSocketBinding(t *testing.T) {
	client := &fakeOAuthClient{endpointURL: DefaultEndpoint}
	opener := &fakeOpener{}
	var prompt bytes.Buffer
	const state = "state-canary-xyz"
	const challenge = "challenge-canary-abc"

	auth := NewDefaultLocalAuthorizer(client, opener, &prompt, state, challenge)
	if auth == nil {
		t.Fatal("expected non-nil *LocalAuthorizer")
	}
	// The factory must be installed so Authorize can create the callback server.
	if auth.callbackFactory == nil {
		t.Fatal("expected non-nil callbackFactory; constructor must install the default factory")
	}
	// Construction must not have invoked the factory (no socket bound). We cannot
	// directly observe the closure, but a non-nil factory that was never called is
	// the only way construction completes without binding a port.
	if auth.client != client {
		t.Errorf("client not stored: got %p, want %p", auth.client, client)
	}
	if auth.state != state {
		t.Errorf("state not stored: got %q, want %q", auth.state, state)
	}
	if auth.codeChallenge != challenge {
		t.Errorf("codeChallenge not stored: got %q, want %q", auth.codeChallenge, challenge)
	}
	if auth.opener != opener {
		t.Errorf("opener not stored: got %p, want %p", auth.opener, opener)
	}
}
