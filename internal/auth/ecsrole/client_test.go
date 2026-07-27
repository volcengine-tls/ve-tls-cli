package ecsrole

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClient builds a Client pointed at the given test server with a
// zero-delay sleeper so retry tests do not wall-clock wait.
func newTestClient(baseURL string) *Client {
	return newClientForTest(baseURL, &http.Client{
		Transport: &http.Transport{Proxy: nil},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errRedirectSentinel
		},
	}, func(ctx context.Context, _ time.Duration) error {
		// Zero sleep for tests; still honor context cancellation.
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}, 2*time.Second)
}

func credsJSON(expiredTime string) []byte {
	resp := map[string]string{
		"AccessKeyId":     "TEMP-AK",
		"SecretAccessKey": "TEMP-SK",
		"SessionToken":    "TEMP-TOKEN",
		"ExpiredTime":     expiredTime,
	}
	b, _ := json.Marshal(resp)
	return b
}

func TestClientRequestsIMDSv2TokenWithSixHourTTL(t *testing.T) {
	var gotMethod, gotPath, gotTTL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/latest/api/token" {
			gotMethod = r.Method
			gotPath = r.URL.Path
			gotTTL = r.Header.Get("X-volc-ecs-metadata-token-ttl-seconds")
			w.Write([]byte("test-token-value"))
			return
		}
		w.Write(credsJSON("2099-01-01T00:00:00Z"))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	if _, err := c.FetchCredentials(context.Background(), "my-role"); err != nil {
		t.Fatalf("FetchCredentials: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Fatalf("token method=%q, want PUT", gotMethod)
	}
	if gotPath != "/latest/api/token" {
		t.Fatalf("token path=%q, want /latest/api/token", gotPath)
	}
	if gotTTL != "21600" {
		t.Fatalf("token TTL header=%q, want 21600", gotTTL)
	}
}

func TestClientRequestsEscapedExplicitRoleWithToken(t *testing.T) {
	var gotToken, gotEscapedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/latest/api/token" {
			w.Write([]byte("secret-token-123"))
			return
		}
		gotToken = r.Header.Get("X-volc-ecs-metadata-token")
		gotEscapedPath = r.URL.EscapedPath()
		w.Write(credsJSON("2099-01-01T00:00:00Z"))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	if _, err := c.FetchCredentials(context.Background(), "my role/with spaces"); err != nil {
		t.Fatalf("FetchCredentials: %v", err)
	}
	if gotToken != "secret-token-123" {
		t.Fatalf("credential token header=%q, want secret-token-123", gotToken)
	}
	wantPath := "/volcstack/latest/iam/security_credentials/" + url.PathEscape("my role/with spaces")
	if gotEscapedPath != wantPath {
		t.Fatalf("credential escaped path=%q, want %q (must preserve %%20/%%2F)", gotEscapedPath, wantPath)
	}
}

func TestClientIgnoresProxyEnvironment(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("NO_PROXY", "")

	var hit int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hit, 1)
		if r.URL.Path == "/latest/api/token" {
			w.Write([]byte("tok"))
			return
		}
		w.Write(credsJSON("2099-01-01T00:00:00Z"))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	if _, err := c.FetchCredentials(context.Background(), "r"); err != nil {
		t.Fatalf("FetchCredentials: %v", err)
	}
	if atomic.LoadInt32(&hit) == 0 {
		t.Fatal("test server was never hit (proxy env blocked the request?)")
	}
}

func TestClientRejectsRedirects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/latest/api/token" {
			http.Redirect(w, r, "/elsewhere", http.StatusFound)
			return
		}
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.FetchCredentials(context.Background(), "r")
	if err == nil {
		t.Fatal("expected error when token endpoint redirects")
	}
}

func TestClientRejectsEmptyTokenAndIncompleteCredentials(t *testing.T) {
	t.Run("empty token rejected", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/latest/api/token" {
				w.Write([]byte("   "))
				return
			}
		}))
		defer srv.Close()
		c := newTestClient(srv.URL)
		_, err := c.FetchCredentials(context.Background(), "r")
		if err == nil {
			t.Fatal("expected error for empty token")
		}
	})

	t.Run("incomplete credentials rejected", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/latest/api/token" {
				w.Write([]byte("tok"))
				return
			}
			// Missing SessionToken.
			resp := map[string]string{
				"AccessKeyId": "AK", "SecretAccessKey": "SK",
				"ExpiredTime": "2099-01-01T00:00:00Z",
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer srv.Close()
		c := newTestClient(srv.URL)
		_, err := c.FetchCredentials(context.Background(), "r")
		if err == nil {
			t.Fatal("expected error for incomplete credentials")
		}
	})
}

func TestClientValidatesRFC3339ExpiredTime(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/latest/api/token" {
			w.Write([]byte("tok"))
			return
		}
		resp := map[string]string{
			"AccessKeyId": "AK", "SecretAccessKey": "SK", "SessionToken": "ST",
			"ExpiredTime": "not-a-date",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	_, err := c.FetchCredentials(context.Background(), "r")
	if err == nil {
		t.Fatal("expected error for invalid ExpiredTime")
	}
}

func TestClientHonorsMetadataDisabledEnvironment(t *testing.T) {
	t.Setenv("VOLCENGINE_ECS_METADATA_DISABLED", "true")
	var hit int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hit, 1)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.FetchCredentials(context.Background(), "r")
	if err == nil {
		t.Fatal("expected error when metadata disabled")
	}
	if atomic.LoadInt32(&hit) != 0 {
		t.Fatalf("server hit %d times, want 0 (must fail before network)", hit)
	}
}

func TestClientHonorsMetadataDisabledCaseInsensitive(t *testing.T) {
	t.Setenv("VOLCENGINE_ECS_METADATA_DISABLED", "True")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	_, err := c.FetchCredentials(context.Background(), "r")
	if err == nil {
		t.Fatal("expected error when metadata disabled (case-insensitive)")
	}
}

func TestClientBoundsBodiesAndRejectsOversize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/latest/api/token" {
			w.Write([]byte("tok"))
			return
		}
		big := make([]byte, 64*1024+10)
		for i := range big {
			big[i] = 'x'
		}
		w.Write(big)
	}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	_, err := c.FetchCredentials(context.Background(), "r")
	if err == nil {
		t.Fatal("expected error for oversize body")
	}
}

func TestClientRetriesTransientFailures(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/latest/api/token" {
			if atomic.AddInt32(&calls, 1) < 3 {
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			w.Write([]byte("tok"))
			return
		}
		w.Write(credsJSON("2099-01-01T00:00:00Z"))
	}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	if _, err := c.FetchCredentials(context.Background(), "r"); err != nil {
		t.Fatalf("FetchCredentials: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("token calls=%d, want 3 (2 retries then success)", got)
	}
}

func TestClientRetriesAtMostFourTimes(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	_, _ = c.FetchCredentials(context.Background(), "r")
	if got := atomic.LoadInt32(&calls); got != 4 {
		t.Fatalf("token calls=%d, want 4 (max attempts)", got)
	}
}

func TestClientDoesNotRetryNonTransientErrors(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	_, err := c.FetchCredentials(context.Background(), "r")
	if err == nil {
		t.Fatal("expected error for 400")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("token calls=%d, want 1 (no retry on 400)", got)
	}
}

func TestClientRespectsContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Wait for the request context to be cancelled, then return.
		<-r.Context().Done()
		w.WriteHeader(http.StatusRequestTimeout)
	}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := c.FetchCredentials(ctx, "r")
	if err == nil {
		t.Fatal("expected error from canceled context")
	}
}

func TestClientRejectsEmptyRoleName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	_, err := c.FetchCredentials(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty role name")
	}
}

func TestClientErrorsNeverExposeCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/latest/api/token" {
			w.Write([]byte("tok"))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"AccessKeyId":"SECRET-AK","SecretAccessKey":"SECRET-SK"}`))
	}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	_, err := c.FetchCredentials(context.Background(), "r")
	if err == nil {
		t.Fatal("expected error")
	}
	text := err.Error()
	if strings.Contains(text, "SECRET-AK") || strings.Contains(text, "SECRET-SK") {
		t.Fatalf("error leaked credentials: %q", text)
	}
}

func TestClientDoesNotReadECSMetadataEnv(t *testing.T) {
	t.Setenv("VOLCENGINE_ECS_METADATA", "some-value")
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/latest/api/token" {
			w.Write([]byte("tok"))
			return
		}
		gotPath = r.URL.Path
		w.Write(credsJSON("2099-01-01T00:00:00Z"))
	}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	if _, err := c.FetchCredentials(context.Background(), "explicit-role"); err != nil {
		t.Fatalf("FetchCredentials: %v", err)
	}
	if !strings.Contains(gotPath, "explicit-role") {
		t.Fatalf("credential path=%q, want explicit role (no env role fallback)", gotPath)
	}
}

// recordingTransport records requests and returns a canned response.
type recordingTransport struct {
	mu       sync.Mutex
	requests []*http.Request
	response *http.Response
	err      error
}

func (rt *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.mu.Lock()
	rt.requests = append(rt.requests, req.Clone(req.Context()))
	rt.mu.Unlock()
	if rt.err != nil {
		return nil, rt.err
	}
	return rt.response, nil
}

func TestClientUsesNoProxyTransport(t *testing.T) {
	rt := &recordingTransport{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("tok")),
		},
	}
	c := newClientForTest("http://100.96.0.96", &http.Client{Transport: rt}, nil, 2*time.Second)
	_, _ = c.FetchCredentials(context.Background(), "r")
	if len(rt.requests) == 0 {
		t.Fatal("no request was made through the transport")
	}
}

// --- New retry/classification tests ---

func TestClientRetriesAfterAttemptTimeout(t *testing.T) {
	// First attempt times out (no response), second succeeds.
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			// Block until the per-attempt timeout fires.
			time.Sleep(150 * time.Millisecond)
			return
		}
		if r.URL.Path == "/latest/api/token" {
			w.Write([]byte("tok"))
			return
		}
		w.Write(credsJSON("2099-01-01T00:00:00Z"))
	}))
	defer srv.Close()
	// Short attempt timeout so the first attempt fails fast.
	c := newClientForTest(srv.URL, &http.Client{
		Transport: &http.Transport{Proxy: nil},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errRedirectSentinel
		},
	}, func(ctx context.Context, _ time.Duration) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}, 50*time.Millisecond)

	if _, err := c.FetchCredentials(context.Background(), "r"); err != nil {
		t.Fatalf("FetchCredentials: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got < 2 {
		t.Fatalf("calls=%d, want >=2 (retry after attempt timeout)", got)
	}
}

func TestClientRetriesAfterBodyReadTimeout(t *testing.T) {
	var tokenCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/latest/api/token" {
			if atomic.AddInt32(&tokenCalls, 1) == 1 {
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				return
			}
			_, _ = w.Write([]byte("token-after-body-timeout"))
			return
		}
		_, _ = w.Write(credsJSON("2099-01-01T00:00:00Z"))
	}))
	defer srv.Close()

	c := newClientForTest(
		srv.URL,
		&http.Client{Transport: &http.Transport{Proxy: nil}},
		func(context.Context, time.Duration) error { return nil },
		50*time.Millisecond,
	)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := c.FetchCredentials(ctx, "r"); err != nil {
		t.Fatalf("FetchCredentials: %v", err)
	}
	if got := atomic.LoadInt32(&tokenCalls); got != 2 {
		t.Fatalf("token calls=%d, want 2", got)
	}
}

func TestClientCallerCancelMakesSingleAttempt(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		// Block until the caller cancels.
		<-r.Context().Done()
		w.WriteHeader(http.StatusRequestTimeout)
	}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_, _ = c.FetchCredentials(ctx, "r")
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("calls=%d, want 1 (no retry after caller cancel)", got)
	}
}

func TestClientRedirectMakesSingleAttemptAndHitsZeroTargets(t *testing.T) {
	var tokenHits, redirectHits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/latest/api/token" {
			atomic.AddInt32(&tokenHits, 1)
			http.Redirect(w, r, "/elsewhere", http.StatusFound)
			return
		}
		atomic.AddInt32(&redirectHits, 1)
	}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	_, err := c.FetchCredentials(context.Background(), "r")
	if err == nil {
		t.Fatal("expected error on redirect")
	}
	if got := atomic.LoadInt32(&tokenHits); got != 1 {
		t.Fatalf("token endpoint hits=%d, want 1", got)
	}
	if got := atomic.LoadInt32(&redirectHits); got != 0 {
		t.Fatalf("redirect target hits=%d, want 0", got)
	}
}

func TestClientOversizeCredentialBodyMakesSingleAttempt(t *testing.T) {
	var credCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/latest/api/token" {
			w.Write([]byte("tok"))
			return
		}
		atomic.AddInt32(&credCalls, 1)
		big := make([]byte, 64*1024+10)
		for i := range big {
			big[i] = 'x'
		}
		w.Write(big)
	}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	_, err := c.FetchCredentials(context.Background(), "r")
	if err == nil {
		t.Fatal("expected error for oversize body")
	}
	if got := atomic.LoadInt32(&credCalls); got != 1 {
		t.Fatalf("credential calls=%d, want 1 (no retry on oversize)", got)
	}
}

func TestClientNilReceiverAndContextSafety(t *testing.T) {
	t.Run("nil receiver", func(t *testing.T) {
		var c *Client
		_, err := c.FetchCredentials(context.Background(), "r")
		if err == nil {
			t.Fatal("expected error from nil *Client")
		}
	})
	t.Run("nil context", func(t *testing.T) {
		c := newTestClient("http://127.0.0.1:1")
		//nolint:staticcheck
		_, err := c.FetchCredentials(nil, "r")
		if err == nil {
			t.Fatal("expected error for nil context")
		}
	})
	t.Run("nil httpClient", func(t *testing.T) {
		c := &Client{baseURL: "http://127.0.0.1:1", sleeper: defaultSleeper, attemptTimeout: perRequestTimeout}
		_, err := c.FetchCredentials(context.Background(), "r")
		if err == nil {
			t.Fatal("expected error for nil httpClient")
		}
	})
}

func TestNewClientProductionProxyRedirectAndTimeout(t *testing.T) {
	c := NewClient()
	// Repoint to a test server without rebuilding the http client.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/latest/api/token" {
			w.Write([]byte("tok"))
			return
		}
		w.Write(credsJSON("2099-01-01T00:00:00Z"))
	}))
	defer srv.Close()
	c.baseURL = srv.URL

	tr, ok := c.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type=%T, want *http.Transport", c.httpClient.Transport)
	}
	if tr.Proxy != nil {
		t.Fatal("production transport must have Proxy=nil")
	}
	if c.httpClient.Timeout != perRequestTimeout {
		t.Fatalf("timeout=%v, want %v", c.httpClient.Timeout, perRequestTimeout)
	}
	if c.httpClient.CheckRedirect == nil {
		t.Fatal("production client must set CheckRedirect")
	}
	if _, err := c.FetchCredentials(context.Background(), "r"); err != nil {
		t.Fatalf("FetchCredentials: %v", err)
	}
}

func TestNewClientIgnoresProxyEnvironment(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	c := NewClient()
	var hit int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hit, 1)
		if r.URL.Path == "/latest/api/token" {
			w.Write([]byte("tok"))
			return
		}
		w.Write(credsJSON("2099-01-01T00:00:00Z"))
	}))
	defer srv.Close()
	c.baseURL = srv.URL
	if _, err := c.FetchCredentials(context.Background(), "r"); err != nil {
		t.Fatalf("FetchCredentials: %v", err)
	}
	if atomic.LoadInt32(&hit) == 0 {
		t.Fatal("test server was never hit (proxy env blocked the request?)")
	}
}

func TestClientFetchesFreshTokenEachRefreshAgainstRealServer(t *testing.T) {
	var mu sync.Mutex
	var lastToken string
	var credentialTokens []string
	tokenRound := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/latest/api/token" {
			mu.Lock()
			tokenRound++
			tok := "token-" + string(rune('0'+tokenRound))
			lastToken = tok
			mu.Unlock()
			w.Write([]byte(tok))
			return
		}
		got := r.Header.Get("X-volc-ecs-metadata-token")
		mu.Lock()
		want := lastToken
		credentialTokens = append(credentialTokens, got)
		mu.Unlock()
		if got != want {
			t.Errorf("credential token header=%q, want %q (round mismatch)", got, want)
		}
		w.Write(credsJSON("2099-01-01T00:00:00Z"))
	}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	if _, err := c.FetchCredentials(context.Background(), "r"); err != nil {
		t.Fatalf("first FetchCredentials: %v", err)
	}
	if _, err := c.FetchCredentials(context.Background(), "r"); err != nil {
		t.Fatalf("second FetchCredentials: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if tokenRound != 2 {
		t.Fatalf("token PUT count=%d, want 2", tokenRound)
	}
	if len(credentialTokens) != 2 || credentialTokens[0] != "token-1" || credentialTokens[1] != "token-2" {
		t.Fatalf("credential token headers=%q, want [token-1 token-2]", credentialTokens)
	}
}
