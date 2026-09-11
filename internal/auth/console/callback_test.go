package console

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCallbackOnlyAcceptsGET(t *testing.T) {
	cs := &CallbackServer{
		result: make(chan *AuthorizationResult, 1),
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		req := httptest.NewRequest(method, CallbackPath, nil)
		rec := httptest.NewRecorder()
		cs.handleCallback(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("method %s: status = %d, want 405", method, rec.Code)
		}
		if got := rec.Header().Get("Allow"); got != http.MethodGet {
			t.Fatalf("method %s: Allow = %q, want GET", method, got)
		}
	}
}

func TestCallbackDeliversOnlyFirstResult(t *testing.T) {
	cs := &CallbackServer{
		result: make(chan *AuthorizationResult, 1),
	}

	// First callback delivers a code.
	req1 := httptest.NewRequest(http.MethodGet, CallbackPath+"?code=first-code&state=state-1", nil)
	rec1 := httptest.NewRecorder()
	cs.handleCallback(rec1, req1)

	select {
	case res := <-cs.result:
		if res.Code != "first-code" {
			t.Fatalf("first result code = %q, want first-code", res.Code)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first result")
	}

	// Second callback must not deliver anything (channel is empty).
	req2 := httptest.NewRequest(http.MethodGet, CallbackPath+"?code=second-code&state=state-2", nil)
	rec2 := httptest.NewRecorder()
	cs.handleCallback(rec2, req2)

	select {
	case res := <-cs.result:
		t.Fatalf("second callback delivered result: %+v", res)
	case <-time.After(100 * time.Millisecond):
		// Expected: no second delivery.
	}
}

func TestCallbackDoesNotDoubleDecodeParameters(t *testing.T) {
	cs := &CallbackServer{
		result: make(chan *AuthorizationResult, 1),
	}

	// The code contains a literal %2B (encoded plus). After one round of
	// URL decoding it becomes "+"; if double-decoded it would become " "
	// (space) or another character. We use a code that, if double-decoded,
	// would change meaning.
	//
	// Raw query: code=abc%252Bdef
	// After one decode: code=abc%2Bdef  (the %25 -> %, so %252B -> %2B)
	// After two decodes: code=abc+def   (%2B -> +)
	//
	// We expect the single-decoded value "abc%2Bdef".
	req := httptest.NewRequest(http.MethodGet, CallbackPath+"?code=abc%252Bdef&state=s", nil)
	rec := httptest.NewRecorder()
	cs.handleCallback(rec, req)

	select {
	case res := <-cs.result:
		if res.Code != "abc%2Bdef" {
			t.Fatalf("code = %q, want abc%%2Bdef (no double decode)", res.Code)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for result")
	}
}

func TestCallbackErrorPriority(t *testing.T) {
	cases := []struct {
		name      string
		query     string
		wantError string
		wantDesc  string
	}{
		{
			name:      "error takes priority",
			query:     "?error=primary&Error=secondary&error_description=desc",
			wantError: "primary",
			wantDesc:  "desc",
		},
		{
			name:      "Error used when error missing",
			query:     "?Error=secondary&error_description=desc",
			wantError: "secondary",
			wantDesc:  "desc",
		},
		{
			name:      "error_description used when both error and Error missing",
			query:     "?error_description=desc-only",
			wantError: "desc-only",
			wantDesc:  "",
		},
		{
			name:      "description same as error is not duplicated",
			query:     "?error=same&error_description=same",
			wantError: "same",
			wantDesc:  "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cs := &CallbackServer{
				result: make(chan *AuthorizationResult, 1),
			}
			req := httptest.NewRequest(http.MethodGet, CallbackPath+tc.query, nil)
			rec := httptest.NewRecorder()
			cs.handleCallback(rec, req)

			select {
			case res := <-cs.result:
				if res.Error != tc.wantError {
					t.Fatalf("error = %q, want %q", res.Error, tc.wantError)
				}
				if res.ErrorDescription != tc.wantDesc {
					t.Fatalf("description = %q, want %q", res.ErrorDescription, tc.wantDesc)
				}
			case <-time.After(time.Second):
				t.Fatal("timed out waiting for result")
			}
		})
	}
}

func TestCallbackEscapesScriptPayload(t *testing.T) {
	cs := &CallbackServer{
		result: make(chan *AuthorizationResult, 1),
	}

	// Malicious error payload that attempts to inject a script tag.
	malicious := `</script><script>alert('xss')</script>`
	req := httptest.NewRequest(http.MethodGet, CallbackPath+"?error="+malicious, nil)
	rec := httptest.NewRecorder()
	cs.handleCallback(rec, req)

	body := rec.Body.String()
	// The raw script tag must not appear verbatim in the response.
	if strings.Contains(body, malicious) {
		t.Fatalf("response contains unescaped script payload:\n%s", body)
	}
	// No executable <script> tag should be present.
	if strings.Contains(strings.ToLower(body), "<script>") {
		t.Fatalf("response contains executable script tag:\n%s", body)
	}
}

func TestCallbackDoesNotEchoOAuthErrorInHTML(t *testing.T) {
	cases := []struct {
		name  string
		query string
	}{
		{name: "error canary", query: "?error=CANARY_ERROR_VALUE"},
		{name: "Error canary", query: "?Error=CANARY_ERROR_UPPER"},
		{name: "error_description canary", query: "?error_description=CANARY_ERROR_DESC"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cs := &CallbackServer{
				result: make(chan *AuthorizationResult, 1),
			}
			req := httptest.NewRequest(http.MethodGet, CallbackPath+tc.query, nil)
			rec := httptest.NewRecorder()
			cs.handleCallback(rec, req)

			body := rec.Body.String()
			// The raw OAuth error/description must never appear in the HTML body.
			if strings.Contains(body, "CANARY_ERROR") {
				t.Fatalf("response echoes raw OAuth error:\n%s", body)
			}
			// The page must still show the fixed failure title (error case).
			if !strings.Contains(body, "Authentication failed") && !strings.Contains(body, "认证失败") {
				t.Fatalf("response missing failure title:\n%s", body)
			}
			// The internal result must still carry the error for flow judgment.
			select {
			case res := <-cs.result:
				if res.Error == "" {
					t.Fatalf("internal result lost error detail")
				}
			case <-time.After(time.Second):
				t.Fatal("timed out waiting for result")
			}
		})
	}
}

func TestCallbackTimeoutHonorsContext(t *testing.T) {
	cs := &CallbackServer{
		result:   make(chan *AuthorizationResult, 1),
		serveErr: make(chan error, 1),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := cs.Wait(ctx)
	if err == nil {
		t.Fatal("expected context deadline error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded", err)
	}

	// Cancellation should also be honored.
	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()
	_, err = cs.Wait(ctx2)
	if err == nil {
		t.Fatal("expected context canceled error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestCallbackRequestsLoopbackRandomPort(t *testing.T) {
	var capturedNetwork string
	var capturedAddress string
	sentinelErr := errors.New("sentinel listener error")

	factory := func(network, address string) (net.Listener, error) {
		capturedNetwork = network
		capturedAddress = address
		return nil, sentinelErr
	}

	_, err := NewCallbackServer(factory)
	if err == nil {
		t.Fatal("expected sentinel error, got nil")
	}
	if !errors.Is(err, sentinelErr) {
		t.Fatalf("error = %v, want sentinel error", err)
	}
	if capturedNetwork != "tcp" {
		t.Fatalf("network = %q, want tcp", capturedNetwork)
	}
	if capturedAddress != "127.0.0.1:0" {
		t.Fatalf("address = %q, want 127.0.0.1:0", capturedAddress)
	}
}

// fakeListener is a net.Listener that returns a *net.TCPAddr with a fixed port.
// It never binds a real socket; Accept returns an error so it cannot be used
// for real serving.
type fakeListener struct {
	port int
}

func (f *fakeListener) Accept() (net.Conn, error) {
	return nil, errors.New("fake listener: not serving")
}
func (f *fakeListener) Close() error { return nil }
func (f *fakeListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: f.port}
}

func TestCallbackRedirectURIMatchesPort(t *testing.T) {
	// Use an injected fake listener factory (no real socket binding) to verify
	// that RedirectURI reflects the port extracted from the listener address.
	const wantPort = 12345
	factory := func(network, address string) (net.Listener, error) {
		if network != "tcp" {
			t.Fatalf("network = %q, want tcp", network)
		}
		if address != "127.0.0.1:0" {
			t.Fatalf("address = %q, want 127.0.0.1:0", address)
		}
		return &fakeListener{port: wantPort}, nil
	}

	cs, err := NewCallbackServer(factory)
	if err != nil {
		t.Fatalf("NewCallbackServer: %v", err)
	}
	defer cs.Close()

	if cs.Port() != wantPort {
		t.Fatalf("port = %d, want %d", cs.Port(), wantPort)
	}
	want := "http://127.0.0.1:" + strconv.Itoa(wantPort) + CallbackPath
	if got := cs.RedirectURI(); got != want {
		t.Fatalf("RedirectURI = %q, want %q", got, want)
	}
}

func TestCallbackServerErrorSurfacesToWait(t *testing.T) {
	cs := &CallbackServer{
		result:   make(chan *AuthorizationResult, 1),
		serveErr: make(chan error, 1),
	}
	cs.serveErr <- errors.New("serve failed")

	_, err := cs.Wait(context.Background())
	if err == nil {
		t.Fatal("expected server error, got nil")
	}
	if !strings.Contains(err.Error(), "serve failed") {
		t.Fatalf("error = %v, want serve failed", err)
	}
}

func TestCallbackShutdownIsIdempotent(t *testing.T) {
	cs := &CallbackServer{
		result:   make(chan *AuthorizationResult, 1),
		serveErr: make(chan error, 1),
		server:   &http.Server{},
	}

	ctx := context.Background()
	if err := cs.Shutdown(ctx); err != nil {
		t.Fatalf("first Shutdown: %v", err)
	}
	// Second call must not panic and should return nil (closeOnce ensures
	// Shutdown is only invoked once).
	if err := cs.Shutdown(ctx); err != nil {
		t.Fatalf("second Shutdown: %v", err)
	}
	if err := cs.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := cs.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestCallbackRendersBothLanguages(t *testing.T) {
	for _, lang := range []string{"en", "zh"} {
		t.Run(lang, func(t *testing.T) {
			cs := &CallbackServer{
				result: make(chan *AuthorizationResult, 1),
			}
			req := httptest.NewRequest(http.MethodGet, CallbackPath+"?lang="+lang+"&code=ok", nil)
			rec := httptest.NewRecorder()
			cs.handleCallback(rec, req)

			body := rec.Body.String()
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if !strings.Contains(body, "<!doctype html>") {
				t.Fatalf("response is not HTML:\n%s", body)
			}
		})
	}
}

func TestCallbackLanguageNormalization(t *testing.T) {
	cases := []struct {
		name     string
		lang     string
		wantText string // a substring unique to the selected language
	}{
		{"zh selects Chinese", "zh", "认证成功"},
		{"zh-CN selects Chinese", "zh-CN", "认证成功"},
		{"ZH uppercase selects Chinese", "ZH", "认证成功"},
		{"en selects English", "en", "Authentication successful"},
		{"empty falls back to English", "", "Authentication successful"},
		{"unsupported falls back to English", "fr", "Authentication successful"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cs := &CallbackServer{
				result: make(chan *AuthorizationResult, 1),
			}
			req := httptest.NewRequest(http.MethodGet, CallbackPath+"?lang="+tc.lang+"&code=ok", nil)
			rec := httptest.NewRecorder()
			cs.handleCallback(rec, req)

			body := rec.Body.String()
			if !strings.Contains(body, tc.wantText) {
				t.Fatalf("lang=%q: body does not contain %q\n%s", tc.lang, tc.wantText, body)
			}
		})
	}
}

func TestCallbackServerNilListenerReturnsError(t *testing.T) {
	// A factory returning (nil, nil) must return an error, never panic.
	factory := func(network, address string) (net.Listener, error) {
		return nil, nil
	}
	cs, err := NewCallbackServer(factory)
	if err == nil {
		t.Fatal("expected error for nil listener, got nil")
	}
	if cs != nil {
		t.Fatalf("expected nil server, got %+v", cs)
	}
}

func TestCallbackServerInvalidPortReturnsError(t *testing.T) {
	// A listener whose Addr is not a *net.TCPAddr (so no port can be
	// extracted) must be rejected.
	factory := func(network, address string) (net.Listener, error) {
		return &fakeListenerNoPort{}, nil
	}
	cs, err := NewCallbackServer(factory)
	if err == nil {
		t.Fatal("expected error for listener without valid port, got nil")
	}
	if cs != nil {
		t.Fatalf("expected nil server, got %+v", cs)
	}
}

// fakeListenerNoPort returns a non-TCP Addr so port extraction fails.
type fakeListenerNoPort struct{}

func (f *fakeListenerNoPort) Accept() (net.Conn, error) { return nil, errors.New("not serving") }
func (f *fakeListenerNoPort) Close() error              { return nil }
func (f *fakeListenerNoPort) Addr() net.Addr            { return fakeUDPAddr{} }

type fakeUDPAddr struct{}

func (f fakeUDPAddr) Network() string { return "udp" }
func (f fakeUDPAddr) String() string  { return "127.0.0.1:0" }

func TestCallbackServerTypedNilListenerReturnsError(t *testing.T) {
	// A factory returning a typed-nil listener (non-nil interface wrapping a
	// nil pointer) must return an error, never panic. *net.TCPListener is
	// used because its methods dereference the receiver and would panic if
	// called on a nil pointer.
	factory := func(network, address string) (net.Listener, error) {
		var nilListener *net.TCPListener
		return nilListener, nil
	}
	cs, err := NewCallbackServer(factory)
	if err == nil {
		t.Fatal("expected error for typed-nil listener, got nil")
	}
	if cs != nil {
		t.Fatalf("expected nil server, got %+v", cs)
	}
}

// fakeListenerNilAddr returns a nil Addr from Addr().
type fakeListenerNilAddr struct{}

func (f *fakeListenerNilAddr) Accept() (net.Conn, error) { return nil, errors.New("not serving") }
func (f *fakeListenerNilAddr) Close() error              { return nil }
func (f *fakeListenerNilAddr) Addr() net.Addr            { return nil }

func TestCallbackServerNilAddrReturnsError(t *testing.T) {
	// A listener whose Addr() returns nil must be rejected, never panic.
	factory := func(network, address string) (net.Listener, error) {
		return &fakeListenerNilAddr{}, nil
	}
	cs, err := NewCallbackServer(factory)
	if err == nil {
		t.Fatal("expected error for nil Addr, got nil")
	}
	if cs != nil {
		t.Fatalf("expected nil server, got %+v", cs)
	}
}

// fakeListenerTypedNilAddr returns a typed-nil *net.TCPAddr from Addr(): the
// interface is non-nil but the pointer is nil, so dereferencing Port panics.
type fakeListenerTypedNilAddr struct{}

func (f *fakeListenerTypedNilAddr) Accept() (net.Conn, error) { return nil, errors.New("not serving") }
func (f *fakeListenerTypedNilAddr) Close() error              { return nil }
func (f *fakeListenerTypedNilAddr) Addr() net.Addr {
	var nilAddr *net.TCPAddr
	return nilAddr
}

// fakeListenerSlice, fakeListenerMap, fakeListenerChan, and fakeListenerFunc are
// net.Listener implementations backed by nil-able kinds other than pointer. They
// exist solely to exercise isNilListener against typed-nil values of each kind.
// Methods use value receivers so a nil value can still satisfy the interface
// without panicking when reflect inspects it.
type fakeListenerSlice []byte

func (fakeListenerSlice) Accept() (net.Conn, error) { return nil, errors.New("not serving") }
func (fakeListenerSlice) Close() error              { return nil }
func (fakeListenerSlice) Addr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1}
}

type fakeListenerMap map[string]int

func (fakeListenerMap) Accept() (net.Conn, error) { return nil, errors.New("not serving") }
func (fakeListenerMap) Close() error              { return nil }
func (fakeListenerMap) Addr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1}
}

type fakeListenerChan chan int

func (fakeListenerChan) Accept() (net.Conn, error) { return nil, errors.New("not serving") }
func (fakeListenerChan) Close() error              { return nil }
func (fakeListenerChan) Addr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1}
}

type fakeListenerFunc func()

func (fakeListenerFunc) Accept() (net.Conn, error) { return nil, errors.New("not serving") }
func (fakeListenerFunc) Close() error              { return nil }
func (fakeListenerFunc) Addr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1}
}

func TestCallbackServerTypedNilAddrReturnsError(t *testing.T) {
	// A listener whose Addr() returns a typed-nil *net.TCPAddr must be
	// rejected, never panic on tcpAddr.Port.
	factory := func(network, address string) (net.Listener, error) {
		return &fakeListenerTypedNilAddr{}, nil
	}
	cs, err := NewCallbackServer(factory)
	if err == nil {
		t.Fatal("expected error for typed-nil Addr, got nil")
	}
	if cs != nil {
		t.Fatalf("expected nil server, got %+v", cs)
	}
}

// fakeListenerBadPort returns a *net.TCPAddr with a port outside 1..65535.
type fakeListenerBadPort struct {
	port int
}

func (f *fakeListenerBadPort) Accept() (net.Conn, error) { return nil, errors.New("not serving") }
func (f *fakeListenerBadPort) Close() error              { return nil }
func (f *fakeListenerBadPort) Addr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: f.port}
}

func TestCallbackServerPortOutOfRangeReturnsError(t *testing.T) {
	// TCP ports outside 1..65535 must be rejected so the redirect URI is
	// always well-formed.
	cases := []struct {
		name string
		port int
	}{
		{"zero", 0},
		{"negative", -1},
		{"just above max", 65536},
		{"far above max", 70000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			factory := func(network, address string) (net.Listener, error) {
				return &fakeListenerBadPort{port: tc.port}, nil
			}
			cs, err := NewCallbackServer(factory)
			if err == nil {
				t.Fatalf("expected error for port %d, got nil", tc.port)
			}
			if cs != nil {
				t.Fatalf("expected nil server for port %d, got %+v", tc.port, cs)
			}
		})
	}
}

// fakeListenerTrackedClose records whether Close was called.
type fakeListenerTrackedClose struct {
	closed bool
	addr   net.Addr
}

func (f *fakeListenerTrackedClose) Accept() (net.Conn, error) { return nil, errors.New("not serving") }
func (f *fakeListenerTrackedClose) Close() error {
	f.closed = true
	return nil
}
func (f *fakeListenerTrackedClose) Addr() net.Addr { return f.addr }

// fakeListenerScriptedClose counts Close calls and returns scripted errors in
// order. Once the scripted errors are exhausted, the last error repeats. It is
// used to verify that Shutdown and Close each invoke the listener close path
// independently and that results are cached.
type fakeListenerScriptedClose struct {
	mu        sync.Mutex
	closeCnt  int
	closeErrs []error
	addr      net.Addr
}

func (f *fakeListenerScriptedClose) Accept() (net.Conn, error) { return nil, errors.New("not serving") }
func (f *fakeListenerScriptedClose) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closeCnt++
	if f.closeCnt-1 < len(f.closeErrs) {
		return f.closeErrs[f.closeCnt-1]
	}
	return f.closeErrs[len(f.closeErrs)-1]
}
func (f *fakeListenerScriptedClose) Addr() net.Addr { return f.addr }

func (f *fakeListenerScriptedClose) closeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closeCnt
}

func TestCallbackServerClosesListenerOnValidationFailure(t *testing.T) {
	// When the listener is non-nil but its address/port is rejected, the
	// constructor must close the listener before returning the error so the
	// socket is not leaked.
	listener := &fakeListenerTrackedClose{addr: fakeUDPAddr{}}
	factory := func(network, address string) (net.Listener, error) {
		return listener, nil
	}
	cs, err := NewCallbackServer(factory)
	if err == nil {
		t.Fatal("expected error for invalid port, got nil")
	}
	if cs != nil {
		t.Fatalf("expected nil server, got %+v", cs)
	}
	if !listener.closed {
		t.Fatal("listener was not closed on validation failure")
	}
}

// fakeListenerCountingAccept counts how many times Accept is called. It signals
// on accepted when Accept is first entered so tests can deterministically wait
// for Serve to start, and blocks on release until the test (or Close) allows it
// to return. This keeps Serve parked inside Accept so concurrent Start calls
// cannot start a second Serve.
type fakeListenerCountingAccept struct {
	acceptCount int32
	closed      atomic.Bool
	port        int
	accepted    chan struct{}
	release     chan struct{}
	releaseOnce sync.Once
}

func (f *fakeListenerCountingAccept) Accept() (net.Conn, error) {
	atomic.AddInt32(&f.acceptCount, 1)
	// Signal that Accept has been entered so the test knows Serve is running.
	close(f.accepted)
	// Block until released, keeping Serve inside Accept.
	<-f.release
	return nil, errors.New("fake listener: not serving")
}
func (f *fakeListenerCountingAccept) Close() error {
	f.closed.Store(true)
	f.releaseOnce.Do(func() { close(f.release) })
	return nil
}
func (f *fakeListenerCountingAccept) Addr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: f.port}
}

func TestCallbackStartRunsServeAtMostOnce(t *testing.T) {
	listener := &fakeListenerCountingAccept{
		port:     12345,
		accepted: make(chan struct{}),
		release:  make(chan struct{}),
	}
	factory := func(network, address string) (net.Listener, error) {
		return listener, nil
	}
	cs, err := NewCallbackServer(factory)
	if err != nil {
		t.Fatalf("NewCallbackServer: %v", err)
	}
	defer cs.Close()

	// Call Start many times concurrently; only one Serve (and thus one Accept)
	// must ever run.
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cs.Start()
		}()
	}
	wg.Wait()

	// Wait deterministically for the single Serve goroutine to enter Accept.
	select {
	case <-listener.accepted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Serve to start")
	}

	if got := atomic.LoadInt32(&listener.acceptCount); got != 1 {
		t.Fatalf("Accept called %d times, want exactly 1", got)
	}
}

func TestCallbackServeErrorDeliveryIsNonBlocking(t *testing.T) {
	// If serveErr already holds an error (e.g. consumed by Wait then a second
	// error arrives), a subsequent Serve goroutine must not block on send.
	cs := &CallbackServer{
		result:   make(chan *AuthorizationResult, 1),
		serveErr: make(chan error, 1),
		server:   &http.Server{},
		listener: &fakeListener{port: 1},
	}
	cs.serveErr <- errors.New("first error")

	// Start a goroutine that would block forever if the send were blocking.
	done := make(chan struct{})
	go func() {
		cs.deliverServeErr(errors.New("second error"))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("deliverServeErr blocked on full channel")
	}
}

func TestCallbackCloseBeforeStartClosesListener(t *testing.T) {
	listener := &fakeListenerTrackedClose{addr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}}
	factory := func(network, address string) (net.Listener, error) {
		return listener, nil
	}
	cs, err := NewCallbackServer(factory)
	if err != nil {
		t.Fatalf("NewCallbackServer: %v", err)
	}
	// Close before Start must close the already-acquired listener.
	if err := cs.Close(); err != nil {
		t.Fatalf("Close before Start: %v", err)
	}
	if !listener.closed {
		t.Fatal("listener was not closed by Close before Start")
	}
}

func TestCallbackShutdownBeforeStartClosesListener(t *testing.T) {
	listener := &fakeListenerTrackedClose{addr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}}
	factory := func(network, address string) (net.Listener, error) {
		return listener, nil
	}
	cs, err := NewCallbackServer(factory)
	if err != nil {
		t.Fatalf("NewCallbackServer: %v", err)
	}
	// Shutdown before Start must close the already-acquired listener.
	if err := cs.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown before Start: %v", err)
	}
	if !listener.closed {
		t.Fatal("listener was not closed by Shutdown before Start")
	}
}

func TestCallbackShutdownNilContextReturnsError(t *testing.T) {
	cs := &CallbackServer{
		result:   make(chan *AuthorizationResult, 1),
		serveErr: make(chan error, 1),
		server:   &http.Server{},
	}
	// Shutdown(nil) must return an explicit error and never panic.
	//lint:ignore SA1012 verifies Shutdown rejects a nil context without panicking
	err := cs.Shutdown(nil)
	if err == nil {
		t.Fatal("expected error for nil context, got nil")
	}
}

func TestCallbackFailedShutdownDoesNotDisableClose(t *testing.T) {
	// The first listener.Close (invoked by Shutdown) returns a sentinel error so
	// Shutdown definitely fails. The later Close must still execute the
	// independent force-close path and call listener.Close a second time.
	sentinel := errors.New("sentinel shutdown close failure")
	listener := &fakeListenerScriptedClose{
		closeErrs: []error{sentinel, nil},
		addr:      &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345},
	}
	factory := func(network, address string) (net.Listener, error) {
		return listener, nil
	}
	cs, err := NewCallbackServer(factory)
	if err != nil {
		t.Fatalf("NewCallbackServer: %v", err)
	}

	// First Shutdown: listener.Close (call 1) returns the sentinel, so Shutdown
	// must surface it rather than swallowing it.
	if err := cs.Shutdown(context.Background()); !errors.Is(err, sentinel) {
		t.Fatalf("first Shutdown error = %v, want sentinel %v", err, sentinel)
	}

	// Close must still execute the independent force-close path, invoking
	// listener.Close a second time (call 2) even though Shutdown already ran.
	if err := cs.Close(); err != nil {
		t.Fatalf("Close after failed Shutdown: %v", err)
	}

	if got := listener.closeCount(); got != 2 {
		t.Fatalf("listener.Close called %d times, want exactly 2 (Shutdown + Close)", got)
	}
}

func TestCallbackCloseCachesFirstResult(t *testing.T) {
	// The first listener.Close returns a sentinel; a hypothetical second call
	// returns a different sentinel. Close must cache the first result and run
	// the underlying close path exactly once.
	firstSentinel := errors.New("first close failure")
	secondSentinel := errors.New("second close failure")
	listener := &fakeListenerScriptedClose{
		closeErrs: []error{firstSentinel, secondSentinel},
		addr:      &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345},
	}
	factory := func(network, address string) (net.Listener, error) {
		return listener, nil
	}
	cs, err := NewCallbackServer(factory)
	if err != nil {
		t.Fatalf("NewCallbackServer: %v", err)
	}

	// First Close: listener.Close (call 1) returns firstSentinel.
	first := cs.Close()
	if !errors.Is(first, firstSentinel) {
		t.Fatalf("first Close error = %v, want sentinel %v", first, firstSentinel)
	}

	// Second Close: must return the cached first result, NOT the second sentinel.
	second := cs.Close()
	if !errors.Is(second, firstSentinel) {
		t.Fatalf("second Close error = %v, want cached sentinel %v", second, firstSentinel)
	}
	if errors.Is(second, secondSentinel) {
		t.Fatalf("second Close returned the second sentinel %v; result was not cached", second)
	}

	// The underlying close path must execute exactly once.
	if got := listener.closeCount(); got != 1 {
		t.Fatalf("listener.Close called %d times, want exactly 1", got)
	}
}

func TestCallbackShutdownCachesFirstResult(t *testing.T) {
	// The first Shutdown gets a deterministic listener.Close error. The second
	// Shutdown must return the same cached error (not nil) and the listener
	// close path must run exactly once. A cached Shutdown failure must not
	// prevent the later Close path.
	sentinel := errors.New("sentinel shutdown close failure")
	listener := &fakeListenerScriptedClose{
		closeErrs: []error{sentinel, nil},
		addr:      &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345},
	}
	factory := func(network, address string) (net.Listener, error) {
		return listener, nil
	}
	cs, err := NewCallbackServer(factory)
	if err != nil {
		t.Fatalf("NewCallbackServer: %v", err)
	}

	// First Shutdown: listener.Close (call 1) returns the sentinel.
	if err := cs.Shutdown(context.Background()); !errors.Is(err, sentinel) {
		t.Fatalf("first Shutdown error = %v, want sentinel %v", err, sentinel)
	}

	// Second Shutdown: must return the cached first result, not nil.
	if err := cs.Shutdown(context.Background()); !errors.Is(err, sentinel) {
		t.Fatalf("second Shutdown error = %v, want cached sentinel %v", err, sentinel)
	}

	// The listener close path must execute exactly once (cached by shutdownOnce).
	if got := listener.closeCount(); got != 1 {
		t.Fatalf("listener.Close called %d times during Shutdown, want exactly 1", got)
	}

	// Cached Shutdown failure must not prevent the later Close path.
	if err := cs.Close(); err != nil {
		t.Fatalf("Close after cached Shutdown failure: %v", err)
	}
	if got := listener.closeCount(); got != 2 {
		t.Fatalf("listener.Close called %d times after Close, want exactly 2", got)
	}
}

// fakeListenerCloseError returns an error from Close so we can verify that
// validation cleanup combines the Close error with the primary error.
type fakeListenerCloseError struct {
	closeErr error
	addr     net.Addr
}

func (f *fakeListenerCloseError) Accept() (net.Conn, error) { return nil, errors.New("not serving") }
func (f *fakeListenerCloseError) Close() error              { return f.closeErr }
func (f *fakeListenerCloseError) Addr() net.Addr            { return f.addr }

func TestCallbackFactoryErrorClosesListener(t *testing.T) {
	// A factory that returns both a listener and an error must not leak the
	// listener: it should be closed before the error is returned.
	listener := &fakeListenerTrackedClose{addr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}}
	sentinel := errors.New("factory boom")
	factory := func(network, address string) (net.Listener, error) {
		return listener, sentinel
	}
	cs, err := NewCallbackServer(factory)
	if err == nil {
		t.Fatal("expected factory error, got nil")
	}
	if cs != nil {
		t.Fatalf("expected nil server, got %+v", cs)
	}
	if !listener.closed {
		t.Fatal("listener returned alongside factory error was not closed")
	}
}

func TestCallbackValidationCleanupRetainsCloseError(t *testing.T) {
	// When validation fails after a listener is created, the listener is closed.
	// If that Close also fails, the returned error must include both causes.
	closeErr := errors.New("close failed: socket wedged")
	listener := &fakeListenerCloseError{
		closeErr: closeErr,
		addr:     fakeUDPAddr{}, // non-TCP addr triggers validation failure
	}
	factory := func(network, address string) (net.Listener, error) {
		return listener, nil
	}
	_, err := NewCallbackServer(factory)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "close failed") {
		t.Fatalf("error %q does not include Close failure", err.Error())
	}
	if !errors.Is(err, closeErr) {
		t.Fatalf("error %v does not wrap Close error", err)
	}
}

func TestCallbackIsNilListenerHandlesAllNilableKinds(t *testing.T) {
	// isNilListener must catch typed-nil values for every nil-able reflect kind
	// that could plausibly implement net.Listener. Each case below is a non-nil
	// interface wrapping a nil concrete value of the given kind.
	cases := []struct {
		name     string
		listener net.Listener
	}{
		{"nil interface", nil},
		{"typed-nil pointer", func() net.Listener { var l *fakeListener; return l }()},
		{"typed-nil slice", func() net.Listener { var l fakeListenerSlice; return l }()},
		{"typed-nil map", func() net.Listener { var l fakeListenerMap; return l }()},
		{"typed-nil chan", func() net.Listener { var l fakeListenerChan; return l }()},
		{"typed-nil func", func() net.Listener { var l fakeListenerFunc; return l }()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !isNilListener(tc.listener) {
				t.Fatalf("isNilListener(%s) = false, want true", tc.name)
			}
		})
	}

	// A real (non-nil) listener must not be reported as nil.
	real := &fakeListener{port: 1}
	if isNilListener(real) {
		t.Fatal("isNilListener(real) = true, want false")
	}
}
