package console

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth/browser"
)

// OAuthClient is the subset of *ConsoleOAuthClient used by the authorizers and
// the login service. It is an interface so tests can inject fakes without
// performing real network calls.
type OAuthClient interface {
	BuildAuthorizeURL(params *AuthorizeParams) (string, error)
	ExchangeToken(ctx context.Context, req *ConsoleTokenRequest) (*ConsoleTokenResponse, error)
	EndpointURL() string
}

// callbackServer is the interface for the local OAuth callback server. The
// concrete *CallbackServer implements this interface; tests inject fakes to
// avoid binding real sockets. Close is the forceful cleanup path used when
// graceful Shutdown fails.
type callbackServer interface {
	Port() int
	RedirectURI() string
	Start()
	Wait(ctx context.Context) (*AuthorizationResult, error)
	Shutdown(ctx context.Context) error
	Close() error
}

// callbackServerFactory creates a callbackServer bound to a loopback random
// port. The production default wraps NewCallbackServer(nil); tests inject a
// fake factory that returns an in-memory fake server.
type callbackServerFactory func() (callbackServer, error)

// Authorizer completes the OAuth authorization step and returns the
// authorization code and the redirect URI that was used. Implementations must
// not retain or return the PKCE verifier or any token material.
type Authorizer interface {
	Authorize(ctx context.Context) (code string, redirectURI string, err error)
}

// LocalAuthorizer runs the same-device (loopback callback) authorization flow.
//
// The callback listener is created and bound before the redirect URI and
// authorize URL are built, so the URL always reflects the actual assigned port.
// The callback server is started before the browser is opened. The manual
// authorize URL is always printed to the prompt writer; opening the browser is
// best effort and its failure does not fail authorization.
type LocalAuthorizer struct {
	client          OAuthClient
	callbackFactory callbackServerFactory
	opener          browser.Opener
	prompt          io.Writer
	state           string
	codeChallenge   string
}

// NewLocalAuthorizer constructs a LocalAuthorizer. The prompt writer receives
// the manual authorize URL and any user-facing instructions.
func NewLocalAuthorizer(client OAuthClient, factory callbackServerFactory, opener browser.Opener, prompt io.Writer, state, codeChallenge string) *LocalAuthorizer {
	return &LocalAuthorizer{
		client:          client,
		callbackFactory: factory,
		opener:          opener,
		prompt:          prompt,
		state:           state,
		codeChallenge:   codeChallenge,
	}
}

// NewDefaultLocalAuthorizer constructs a production LocalAuthorizer that binds a
// real loopback callback server via NewCallbackServer(nil). It is the exported
// entry point for callers outside the console package (e.g. the CLI adapter)
// that cannot name the package-private callbackServerFactory type. The callback
// server is only created when Authorize runs, so constructing the authorizer
// never binds a socket.
func NewDefaultLocalAuthorizer(client OAuthClient, opener browser.Opener, prompt io.Writer, state, codeChallenge string) *LocalAuthorizer {
	return NewLocalAuthorizer(client, func() (callbackServer, error) {
		return NewCallbackServer(nil)
	}, opener, prompt, state, codeChallenge)
}

// Authorize runs the local flow and returns the authorization code and the
// redirect URI. It always cleans up the callback server. Named returns are used
// so the deferred cleanup can replace or join the returned error: a shutdown
// error on a successful run is surfaced, and both causes are preserved (via
// errors.Is) when both the primary flow and cleanup fail. No error echoes the
// state, code, URL query, or verifier.
func (a *LocalAuthorizer) Authorize(ctx context.Context) (code string, redirectURI string, err error) {
	if a == nil {
		return "", "", errors.New("nil *LocalAuthorizer")
	}
	if isNilInterface(ctx) {
		return "", "", errors.New("nil context")
	}
	if isNilInterface(a.client) {
		return "", "", errors.New("nil oauth client")
	}
	if a.callbackFactory == nil {
		return "", "", errors.New("nil callback server factory")
	}
	prompt := a.prompt
	if isNilInterface(prompt) {
		prompt = io.Discard
	}

	// 1. Bind the callback listener before building the redirect URI.
	cs, ferr := a.callbackFactory()
	if ferr != nil {
		return "", "", newSafeError("create callback server failed", ferr)
	}
	if isNilInterface(cs) {
		return "", "", errors.New("callback server factory returned nil server")
	}

	// 8. Always clean up the callback server. Use a context derived from the
	// caller (without cancellation) so cleanup can still run after the caller
	// cancels, bounded by a short timeout. Shutdown is attempted first; if it
	// fails, Close is invoked as the forceful cleanup path. All primary,
	// Shutdown, and Close causes are preserved for errors.Is/errors.As via
	// newSafeError, whose fixed description never renders their underlying
	// (possibly secret) text.
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		serr := cs.Shutdown(cleanupCtx)
		if serr == nil {
			// Graceful shutdown succeeded: do not redundantly call Close.
			return
		}
		// Shutdown failed: force-close and collect the Close error too.
		cerr := cs.Close()
		if err == nil {
			// Primary succeeded but cleanup failed: surface the cleanup error
			// and do not return a successful code, even if forced Close
			// succeeds.
			code = ""
			redirectURI = ""
			err = newSafeError("cleanup callback server failed", serr, cerr)
		} else {
			// Both primary and cleanup failed: preserve all causes without
			// rendering sensitive primary text.
			err = newSafeError("authorization and cleanup failed", err, serr, cerr)
		}
	}()

	// 2. Build the redirect URI from the now-bound listener.
	redirectURI = cs.RedirectURI()

	// 3. Build the authorize URL.
	authURL, berr := a.client.BuildAuthorizeURL(&AuthorizeParams{
		ClientID:            ClientIDSameDevice,
		RedirectURI:         redirectURI,
		Scope:               Scope,
		State:               a.state,
		CodeChallenge:       a.codeChallenge,
		CodeChallengeMethod: CodeChallengeMethodS256,
	})
	if berr != nil {
		return "", "", newSafeError("build authorize URL failed", berr)
	}

	// 4. Always print the manual authorize URL.
	fmt.Fprintln(prompt, "Open the following URL in your browser to log in:")
	fmt.Fprintln(prompt, authURL)

	// 5. Start serving before opening the browser.
	cs.Start()

	// 6. Open the browser; failure is best effort and does not fail auth.
	if !isNilInterface(a.opener) {
		_ = a.opener.Open(ctx, authURL)
	}

	// 7. Wait for the callback with a child context capped by CallbackTimeout.
	waitCtx, cancel := context.WithTimeout(ctx, CallbackTimeout)
	defer cancel()
	result, werr := cs.Wait(waitCtx)
	if werr != nil {
		return "", "", newSafeError("wait for callback failed", werr)
	}
	if result == nil {
		return "", "", errors.New("authorization failed: callback returned no result")
	}

	// 8. Validate the callback result without echoing secret material.
	if result.Error != "" {
		return "", "", errors.New("authorization failed: oauth error returned by provider")
	}
	if strings.TrimSpace(result.Code) == "" {
		return "", "", errors.New("authorization failed: missing authorization code")
	}
	if result.State != a.state {
		return "", "", errors.New("authorization failed: state mismatch")
	}

	code = result.Code
	return code, redirectURI, nil
}

// RemoteAuthorizer runs the cross-device (manual code entry) authorization flow.
//
// The redirect URI is exactly <endpoint>/authorize/oauth/authorize. The
// authorize URL and a prompt are printed to the writer; a single response line
// is read from the reader. The response is base64-decoded (accepting standard,
// URL, raw URL, and raw standard encodings) and parsed as query data containing
// a nonempty code and state.
type RemoteAuthorizer struct {
	client        OAuthClient
	reader        io.Reader
	writer        io.Writer
	state         string
	codeChallenge string
}

// NewRemoteAuthorizer constructs a RemoteAuthorizer.
func NewRemoteAuthorizer(client OAuthClient, reader io.Reader, writer io.Writer, state, codeChallenge string) *RemoteAuthorizer {
	return &RemoteAuthorizer{
		client:        client,
		reader:        reader,
		writer:        writer,
		state:         state,
		codeChallenge: codeChallenge,
	}
}

// Authorize runs the remote flow and returns the authorization code and the
// redirect URI. Required dependencies and a non-cancelled context are validated
// before blocking on input. The code and state query fields must each occur
// exactly once. No error echoes the raw input, code, state, URL, or verifier.
func (a *RemoteAuthorizer) Authorize(ctx context.Context) (string, string, error) {
	if a == nil {
		return "", "", errors.New("nil *RemoteAuthorizer")
	}
	if isNilInterface(ctx) {
		return "", "", errors.New("nil context")
	}
	if err := ctx.Err(); err != nil {
		return "", "", newSafeError("context already done", err)
	}
	if isNilInterface(a.client) {
		return "", "", errors.New("nil oauth client")
	}
	if isNilInterface(a.reader) {
		return "", "", errors.New("nil reader")
	}
	writer := a.writer
	if isNilInterface(writer) {
		writer = io.Discard
	}

	// 1. Redirect URI is exactly <endpoint>/authorize/oauth/authorize.
	endpoint := strings.TrimRight(a.client.EndpointURL(), "/")
	redirectURI := endpoint + AuthorizePath

	// 2. Build the authorize URL.
	authURL, err := a.client.BuildAuthorizeURL(&AuthorizeParams{
		ClientID:            ClientIDCrossDevice,
		RedirectURI:         redirectURI,
		Scope:               Scope,
		State:               a.state,
		CodeChallenge:       a.codeChallenge,
		CodeChallengeMethod: CodeChallengeMethodS256,
	})
	if err != nil {
		return "", "", newSafeError("build authorize URL failed", err)
	}

	// 3. Print the URL and prompt.
	fmt.Fprintln(writer, "Open the following URL in a browser on any device:")
	fmt.Fprintln(writer, authURL)
	fmt.Fprintln(writer, "After completing login, enter the authorization code shown in the browser:")

	// 4. Read one response line.
	reader := bufio.NewReader(a.reader)
	rawInput, rerr := reader.ReadString('\n')
	if rerr != nil && rerr != io.EOF {
		return "", "", errors.New("read authorization code failed")
	}
	rawInput = strings.TrimSpace(rawInput)
	if rawInput == "" {
		return "", "", errors.New("authorization code is empty")
	}

	// 5. Decode base64, accepting all common variants.
	decoded, derr := decodeBase64Flexible(rawInput)
	if derr != nil {
		return "", "", errors.New("invalid authorization code encoding")
	}

	// 6. Parse the decoded payload as query data.
	params, perr := url.ParseQuery(string(decoded))
	if perr != nil {
		return "", "", errors.New("invalid authorization response")
	}

	// 7. Validate: code and state must each occur exactly once.
	if got := len(params["code"]); got != 1 {
		return "", "", errors.New("authorization response must contain exactly one code field")
	}
	if got := len(params["state"]); got != 1 {
		return "", "", errors.New("authorization response must contain exactly one state field")
	}

	code := params.Get("code")
	if strings.TrimSpace(code) == "" {
		return "", "", errors.New("authorization response missing code")
	}
	respondedState := params.Get("state")
	if respondedState != a.state {
		return "", "", errors.New("authorization failed: state mismatch")
	}

	return code, redirectURI, nil
}

// decodeBase64Flexible decodes input trying standard, URL, raw URL, and raw
// standard base64 encodings in order. Raw standard base64 is accepted only when
// it is compatible (i.e. it decodes successfully).
func decodeBase64Flexible(input string) ([]byte, error) {
	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
		base64.RawStdEncoding,
	}
	for _, enc := range encodings {
		if decoded, err := enc.DecodeString(input); err == nil {
			return decoded, nil
		}
	}
	return nil, errors.New("invalid base64 input")
}
