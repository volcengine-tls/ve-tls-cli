package console

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"
)

//go:embed callback.html
var callbackHTMLFS embed.FS

// callbackTemplate is parsed once from the embedded HTML. It uses html/template
// so all dynamic data (including OAuth error messages) is context-escaped and
// cannot break out of its HTML context.
var callbackTemplate = template.Must(template.ParseFS(callbackHTMLFS, "callback.html"))

// AuthorizationResult holds the result received from the OAuth callback.
type AuthorizationResult struct {
	Code             string
	State            string
	Error            string
	ErrorDescription string
}

// ListenerFactory creates a net.Listener for the callback server. Tests inject
// a fake factory to avoid binding real sockets; the production default uses
// net.Listen("tcp", "127.0.0.1:0").
type ListenerFactory func(network, address string) (net.Listener, error)

// CallbackServer is a local HTTP server that listens for OAuth callbacks from
// the browser. Only the first callback is delivered; subsequent requests are
// acknowledged but ignored.
type CallbackServer struct {
	listener     net.Listener
	server       *http.Server
	result       chan *AuthorizationResult
	deliverOnce  sync.Once
	port         int
	startOnce    sync.Once
	shutdownOnce sync.Once
	shutdownErr  error
	closeOnce    sync.Once
	closeErr     error
	serveErr     chan error
}

// callbackPageData is the data passed to the HTML template. All fields are
// rendered through html/template, which escapes them appropriately for their
// HTML context.
type callbackPageData struct {
	Lang         string
	HasError     bool
	TitleSuccess string
	TitleFailure string
	CopySuccess  string
	CopyFailure  string
}

// messagesByLang holds the static page text for supported languages. Only
// English and Simplified Chinese have explicit entries; unknown or empty
// language values fall back to English via normalizeLang, matching the
// upstream console behavior.
var messagesByLang = map[string]callbackPageData{
	"en": {
		Lang:         "en",
		TitleSuccess: "Authentication successful",
		TitleFailure: "Authentication failed",
		CopySuccess:  "You can close this page and return to the terminal.",
		CopyFailure:  "Please return to the terminal.",
	},
	"zh": {
		Lang:         "zh",
		TitleSuccess: "认证成功",
		TitleFailure: "认证失败",
		CopySuccess:  "你可以关闭此页面并返回终端继续操作。",
		CopyFailure:  "请返回终端继续操作。",
	},
}

// normalizeLang maps a language parameter to a supported message key. It
// matches upstream behavior: "zh" and "zh-CN" (case-insensitive) select
// Chinese; "en" selects English; empty and unsupported values fall back to
// English.
func normalizeLang(lang string) string {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "zh", "zh-cn":
		return "zh"
	case "en":
		return "en"
	default:
		return "en"
	}
}

// isNilListener reports whether l is either an untyped nil interface or a
// typed-nil value (a non-nil interface wrapping a nil pointer, map, slice,
// chan, func, or interface). A simple l == nil check only catches the untyped
// case; typed-nil values slip through and panic when their methods are called.
// This matches the robust pattern used in internal/auth/httpx.
func isNilListener(l net.Listener) bool {
	if l == nil {
		return true
	}
	v := reflect.ValueOf(l)
	switch v.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func:
		return v.IsNil()
	}
	return false
}

// NewCallbackServer creates a new local callback server. The listener is
// created via the provided factory (or net.Listen if nil) bound to
// 127.0.0.1:0 so the OS assigns a random free port.
//
// The constructor is fail-closed: a factory that returns a nil or typed-nil
// listener, a listener whose Addr is nil or a typed-nil *net.TCPAddr, or a
// listener bound to a port outside 1..65535 is rejected with an error rather
// than panicking. Any non-nil listener created during a failed validation is
// closed so the socket is not leaked. If closing that listener also fails, the
// Close error is combined with the primary validation error so neither cause
// is lost.
func NewCallbackServer(factory ListenerFactory) (*CallbackServer, error) {
	if factory == nil {
		factory = net.Listen
	}
	listener, err := factory("tcp", "127.0.0.1:0")
	if err != nil {
		// A factory may return both a listener and an error. We must close the
		// listener so it is not leaked, then return the factory error. If the
		// close also fails, both errors are preserved via errors.Join.
		if !isNilListener(listener) {
			if cerr := listener.Close(); cerr != nil && !errors.Is(cerr, net.ErrClosed) {
				return nil, errors.Join(fmt.Errorf("create callback listener: %w", err), cerr)
			}
		}
		return nil, fmt.Errorf("create callback listener: %w", err)
	}
	if isNilListener(listener) {
		return nil, errors.New("create callback listener: factory returned nil listener")
	}

	// failValidation closes the listener and returns an error that combines the
	// primary validation error with any Close failure (via errors.Join) so
	// neither cause is lost.
	failValidation := func(primary error) (*CallbackServer, error) {
		cerr := listener.Close()
		if cerr != nil && !errors.Is(cerr, net.ErrClosed) {
			return nil, errors.Join(primary, cerr)
		}
		return nil, primary
	}

	addr := listener.Addr()
	if addr == nil {
		return failValidation(errors.New("callback listener returned nil address"))
	}
	tcpAddr, ok := addr.(*net.TCPAddr)
	if !ok || tcpAddr == nil {
		return failValidation(errors.New("callback listener did not bind a valid TCP address"))
	}
	port := tcpAddr.Port
	if port < 1 || port > 65535 {
		return failValidation(fmt.Errorf("callback listener bound invalid TCP port %d", port))
	}

	cs := &CallbackServer{
		listener: listener,
		result:   make(chan *AuthorizationResult, 1),
		port:     port,
		serveErr: make(chan error, 1),
	}

	mux := http.NewServeMux()
	mux.HandleFunc(CallbackPath, cs.handleCallback)

	cs.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
	}

	return cs, nil
}

// Port returns the assigned port number.
func (s *CallbackServer) Port() int {
	if s == nil {
		return 0
	}
	return s.port
}

// RedirectURI returns the full OAuth redirect URI for this callback server.
func (s *CallbackServer) RedirectURI() string {
	if s == nil {
		return ""
	}
	return fmt.Sprintf("http://127.0.0.1:%d%s", s.port, CallbackPath)
}

// Start begins serving HTTP requests in a background goroutine (non-blocking).
// It is safe to call multiple times or concurrently: Serve runs at most once.
// If Serve returns an error other than http.ErrServerClosed, it is delivered to
// the server's error channel so Wait can surface it. The delivery is
// non-blocking: if the channel already holds an error, the new error is
// dropped rather than blocking the goroutine.
func (s *CallbackServer) Start() {
	if s == nil {
		return
	}
	s.startOnce.Do(func() {
		go func() {
			err := s.server.Serve(s.listener)
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				s.deliverServeErr(err)
			}
		}()
	})
}

// deliverServeErr sends err to serveErr without blocking. If the channel is
// full (e.g. a previous error was never consumed), the error is dropped so the
// calling goroutine can never be left blocked.
func (s *CallbackServer) deliverServeErr(err error) {
	select {
	case s.serveErr <- err:
	default:
	}
}

// Wait blocks until the first OAuth callback is received, the server fails, or
// ctx is done. It returns the AuthorizationResult or a context error.
func (s *CallbackServer) Wait(ctx context.Context) (*AuthorizationResult, error) {
	if s == nil {
		return nil, errors.New("nil *CallbackServer")
	}
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	select {
	case result := <-s.result:
		return result, nil
	case err := <-s.serveErr:
		return nil, fmt.Errorf("callback server error: %w", err)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Shutdown gracefully shuts down the HTTP server and closes the owned listener.
// It is idempotent and safe to call multiple times. A nil context returns an
// explicit error and never panics.
//
// Shutdown is independent of Close: a failed, cancelled, or timed-out Shutdown
// does not consume or disable the later forceful Close path. Already-closed
// errors (net.ErrClosed, http.ErrServerClosed) are treated as success.
func (s *CallbackServer) Shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		return errors.New("nil context")
	}
	s.shutdownOnce.Do(func() {
		// Collect meaningful errors from each operation independently.
		// Already-closed states (net.ErrClosed, http.ErrServerClosed) are
		// normalized to nil per operation so a benign close on one side does
		// not mask a real failure on the other. The remaining errors are then
		// combined with errors.Join so no cause is lost.
		var errs []error
		if s.server != nil {
			if serr := s.server.Shutdown(ctx); serr != nil &&
				!errors.Is(serr, net.ErrClosed) &&
				!errors.Is(serr, http.ErrServerClosed) {
				errs = append(errs, serr)
			}
		}
		// Always close the owned listener, even if Start was never called or
		// Shutdown was called before Start.
		if s.listener != nil {
			if cerr := s.listener.Close(); cerr != nil && !errors.Is(cerr, net.ErrClosed) {
				errs = append(errs, cerr)
			}
		}
		s.shutdownErr = errors.Join(errs...)
	})
	return s.shutdownErr
}

// Close immediately closes the server and listener. It is independently
// idempotent from Shutdown: calling Close after a failed Shutdown still
// executes the force-close path. The first result is cached and returned to
// all subsequent callers. Already-closed errors are normalized to nil.
func (s *CallbackServer) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		// Attempt both server.Close and listener.Close and combine any
		// meaningful errors before caching. Already-closed states are
		// normalized to nil per operation so a benign close on one side does
		// not mask a real failure on the other.
		var errs []error
		if s.server != nil {
			if err := s.server.Close(); err != nil &&
				!errors.Is(err, net.ErrClosed) &&
				!errors.Is(err, http.ErrServerClosed) {
				errs = append(errs, err)
			}
		}
		if s.listener != nil {
			if err := s.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
				errs = append(errs, err)
			}
		}
		s.closeErr = errors.Join(errs...)
	})
	return s.closeErr
}

// handleCallback processes the OAuth callback request from the browser. It
// extracts the authorization code and state (or error information) from the
// query parameters, delivers the result to the waiting goroutine exactly once,
// and returns an HTML page to the browser.
func (s *CallbackServer) handleCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse query parameters exactly once. r.URL.Query() returns the parsed
	// values; we never call ParseForm or re-decode, so parameters are not
	// double-decoded.
	query := r.URL.Query()
	lang := query.Get("lang")
	code := query.Get("code")
	state := query.Get("state")
	errorParam := query.Get("error")
	errorParamUpper := query.Get("Error")
	errorDescription := query.Get("error_description")

	// Error priority: error > Error > error_description.
	oauthError := errorParam
	if oauthError == "" {
		oauthError = errorParamUpper
	}
	if oauthError == "" {
		oauthError = errorDescription
	}

	// If the description is identical to the primary error, do not duplicate it.
	normalizedDescription := errorDescription
	if normalizedDescription == oauthError {
		normalizedDescription = ""
	}

	result := &AuthorizationResult{
		Code:             code,
		State:            state,
		Error:            oauthError,
		ErrorDescription: normalizedDescription,
	}

	// Deliver the result only once; ignore duplicate callbacks.
	s.deliverOnce.Do(func() {
		select {
		case s.result <- result:
		default:
		}
	})

	// Build the error flag for display (if any). The raw OAuth error/description
	// is never rendered in the HTML page; it is only carried in the internal
	// AuthorizationResult for flow judgment.
	hasError := oauthError != ""

	s.renderPage(w, hasError, lang)
}

// renderPage writes the callback HTML page, selecting messages by language.
// Only the fixed localized success/failure title and prompt are shown; the raw
// OAuth error/error_description is never rendered. All dynamic content is
// rendered through html/template, which escapes it for its HTML context.
//
// The template is executed into an in-memory buffer before any headers or body
// are written, so a template execution error cannot send partial HTML followed
// by a second status code.
func (s *CallbackServer) renderPage(w http.ResponseWriter, hasError bool, lang string) {
	data := messagesByLang[normalizeLang(lang)]
	data.HasError = hasError

	var buf bytes.Buffer
	if err := callbackTemplate.Execute(&buf, data); err != nil {
		// If template execution fails, write a minimal escaped fallback page
		// directly (it is small enough to render atomically).
		writeFallbackPage(w, data)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(buf.Bytes())
}

// writeFallbackPage writes a minimal HTML page using fmt with explicit
// html/template escaping as a last resort if the embedded template fails.
func writeFallbackPage(w http.ResponseWriter, data callbackPageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	title := data.TitleSuccess
	copy := data.CopySuccess
	if data.HasError {
		title = data.TitleFailure
		copy = data.CopyFailure
	}
	fmt.Fprintf(w,
		`<!doctype html><html lang="%s"><head><meta charset="utf-8"><title>%s</title></head><body><h1>%s</h1><p>%s</p></body></html>`,
		template.HTMLEscapeString(data.Lang),
		template.HTMLEscapeString(title),
		template.HTMLEscapeString(title),
		template.HTMLEscapeString(copy),
	)
}
