package httpx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// roundTripFunc is a function type implementing http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func newClient(rt http.RoundTripper) *http.Client {
	return &http.Client{Transport: rt}
}

// recordingSleeper records the durations it was asked to sleep and never
// actually sleeps.
type recordingSleeper struct {
	calls []time.Duration
}

func (s *recordingSleeper) sleep(ctx context.Context, d time.Duration) error {
	s.calls = append(s.calls, d)
	return ctx.Err()
}

// TestRetryRetriesNetwork408429And5xx verifies that network errors and retryable
// status codes trigger retries up to MaxAttempts, and that each attempt builds
// a fresh request. A terminal network error still surfaces as an error; a
// terminal retryable status is returned to the caller as a response (which the
// test must close) with its status preserved.
func TestRetryRetriesNetwork408429And5xx(t *testing.T) {
	cases := []struct {
		name   string
		status int
		err    error
	}{
		{"network error", 0, errors.New("connection reset")},
		{"408", http.StatusRequestTimeout, nil},
		{"429", http.StatusTooManyRequests, nil},
		{"500", http.StatusInternalServerError, nil},
		{"503", http.StatusServiceUnavailable, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var attempts int32
			rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				atomic.AddInt32(&attempts, 1)
				if tc.err != nil {
					return nil, tc.err
				}
				return newResponse(tc.status, "retry me"), nil
			})

			rc := &RetryClient{
				HTTPClient:  newClient(rt),
				MaxAttempts: 3,
				Sleeper:     (&recordingSleeper{}).sleep,
			}

			var built int32
			factory := func(ctx context.Context) (*http.Request, error) {
				atomic.AddInt32(&built, 1)
				return http.NewRequestWithContext(ctx, http.MethodPost, "https://example.com/token", strings.NewReader("body"))
			}

			resp, err := rc.Do(context.Background(), factory)
			if tc.err != nil {
				// Terminal network error: still returned as an error.
				if err == nil {
					t.Fatal("expected error after exhausting retries")
				}
				if resp != nil {
					t.Fatalf("expected nil response on network error, got %+v", resp)
				}
			} else {
				// Terminal retryable status: returned as a response.
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if resp == nil {
					t.Fatal("expected response on terminal retryable status")
				}
				if resp.StatusCode != tc.status {
					t.Fatalf("status = %d, want %d", resp.StatusCode, tc.status)
				}
				resp.Body.Close()
			}
			if got := atomic.LoadInt32(&attempts); got != 3 {
				t.Fatalf("attempts = %d, want 3", got)
			}
			if got := atomic.LoadInt32(&built); got != 3 {
				t.Fatalf("request factory called %d times, want 3", got)
			}
		})
	}
}

// TestRetryDoesNotRetryOther4xx verifies that non-retryable 4xx status codes
// are returned to the caller without retrying.
func TestRetryDoesNotRetryOther4xx(t *testing.T) {
	var attempts int32
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&attempts, 1)
		return newResponse(http.StatusBadRequest, `{"error":"invalid_grant"}`), nil
	})

	rc := &RetryClient{
		HTTPClient:  newClient(rt),
		MaxAttempts: 3,
		Sleeper:     (&recordingSleeper{}).sleep,
	}

	factory := func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodPost, "https://example.com/token", nil)
	}

	resp, err := rc.Do(context.Background(), factory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("attempts = %d, want 1", got)
	}
}

// TestRetryHonorsRetryAfterAndInjectedSleeper verifies that the Retry-After
// header (both delta-seconds and HTTP-date) is respected, and that the
// injected sleeper receives the expected durations.
func TestRetryHonorsRetryAfterAndInjectedSleeper(t *testing.T) {
	t.Run("delta-seconds", func(t *testing.T) {
		var attempts int32
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			atomic.AddInt32(&attempts, 1)
			resp := newResponse(http.StatusTooManyRequests, "slow down")
			resp.Header.Set("Retry-After", "5")
			return resp, nil
		})

		sleeper := &recordingSleeper{}
		rc := &RetryClient{
			HTTPClient:  newClient(rt),
			MaxAttempts: 3,
			Sleeper:     sleeper.sleep,
		}

		factory := func(ctx context.Context) (*http.Request, error) {
			return http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
		}

		resp, err := rc.Do(context.Background(), factory)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		resp.Body.Close()

		if len(sleeper.calls) != 2 {
			t.Fatalf("sleeper called %d times, want 2", len(sleeper.calls))
		}
		for i, d := range sleeper.calls {
			if d != 5*time.Second {
				t.Fatalf("sleep %d = %v, want 5s", i, d)
			}
		}
	})

	t.Run("http-date", func(t *testing.T) {
		now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
		fixedClock := fixedClock{t: now}

		var attempts int32
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			atomic.AddInt32(&attempts, 1)
			resp := newResponse(http.StatusServiceUnavailable, "retry later")
			// 10 seconds after now, formatted as an HTTP-date (RFC 7231).
			resp.Header.Set("Retry-After", now.Add(10*time.Second).UTC().Format(http.TimeFormat))
			return resp, nil
		})

		sleeper := &recordingSleeper{}
		rc := &RetryClient{
			HTTPClient:  newClient(rt),
			MaxAttempts: 3,
			Sleeper:     sleeper.sleep,
			Clock:       fixedClock,
		}

		factory := func(ctx context.Context) (*http.Request, error) {
			return http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
		}

		resp, err := rc.Do(context.Background(), factory)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		resp.Body.Close()

		if len(sleeper.calls) != 2 {
			t.Fatalf("sleeper called %d times, want 2", len(sleeper.calls))
		}
		for i, d := range sleeper.calls {
			if d != 10*time.Second {
				t.Fatalf("sleep %d = %v, want 10s", i, d)
			}
		}
	})

	t.Run("backoff-without-retry-after", func(t *testing.T) {
		var attempts int32
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			atomic.AddInt32(&attempts, 1)
			return newResponse(http.StatusInternalServerError, "boom"), nil
		})

		sleeper := &recordingSleeper{}
		rc := &RetryClient{
			HTTPClient:  newClient(rt),
			MaxAttempts: 3,
			Sleeper:     sleeper.sleep,
		}

		factory := func(ctx context.Context) (*http.Request, error) {
			return http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
		}

		resp, err := rc.Do(context.Background(), factory)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		resp.Body.Close()

		if len(sleeper.calls) != 2 {
			t.Fatalf("sleeper called %d times, want 2", len(sleeper.calls))
		}
		// Deterministic backoff: attempt 1 -> 200ms, attempt 2 -> 400ms.
		if sleeper.calls[0] != 200*time.Millisecond {
			t.Fatalf("sleep 0 = %v, want 200ms", sleeper.calls[0])
		}
		if sleeper.calls[1] != 400*time.Millisecond {
			t.Fatalf("sleep 1 = %v, want 400ms", sleeper.calls[1])
		}
	})
}

type fixedClock struct {
	t time.Time
}

func (f fixedClock) Now() time.Time { return f.t }

// TestRetryStopsImmediatelyOnContextCancellation verifies that a canceled
// context stops retries before the next attempt and before sleeping.
func TestRetryStopsImmediatelyOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var attempts int32
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&attempts, 1)
		return newResponse(http.StatusInternalServerError, "boom"), nil
	})

	sleeper := &recordingSleeper{}
	rc := &RetryClient{
		HTTPClient:  newClient(rt),
		MaxAttempts: 3,
		Sleeper:     sleeper.sleep,
	}

	factory := func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
	}

	_, err := rc.Do(ctx, factory)
	if err == nil {
		t.Fatal("expected error from canceled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 0 {
		t.Fatalf("attempts = %d, want 0 (context already canceled)", got)
	}
	if len(sleeper.calls) != 0 {
		t.Fatalf("sleeper called %d times, want 0", len(sleeper.calls))
	}
}

// TestRetryLimitsResponseBody verifies that reading a response body beyond
// MaxBodySize returns ErrBodyTooLarge and that the error never contains the
// raw body content.
func TestRetryLimitsResponseBody(t *testing.T) {
	body := strings.Repeat("a", 100)
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return newResponse(http.StatusOK, body), nil
	})

	rc := &RetryClient{
		HTTPClient:  newClient(rt),
		MaxAttempts: 3,
		Sleeper:     (&recordingSleeper{}).sleep,
		MaxBodySize: 10,
	}

	factory := func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
	}

	resp, err := rc.Do(context.Background(), factory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	got, err := io.ReadAll(resp.Body)
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("error = %v, want ErrBodyTooLarge", err)
	}
	if len(got) > 10 {
		t.Fatalf("read %d bytes, want at most 10", len(got))
	}
	// The error must not contain the body content.
	if strings.Contains(err.Error(), strings.Repeat("a", 5)) {
		t.Fatalf("error contains body content: %v", err)
	}
}

// TestRetryReturnsSuccessOnFirstTry verifies the happy path returns the
// response without sleeping.
func TestRetryReturnsSuccessOnFirstTry(t *testing.T) {
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return newResponse(http.StatusOK, `{"ok":true}`), nil
	})

	sleeper := &recordingSleeper{}
	rc := &RetryClient{
		HTTPClient:  newClient(rt),
		MaxAttempts: 3,
		Sleeper:     sleeper.sleep,
	}

	factory := func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
	}

	resp, err := rc.Do(context.Background(), factory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(sleeper.calls) != 0 {
		t.Fatalf("sleeper called %d times, want 0", len(sleeper.calls))
	}
}

// TestRetryClosesRetryableResponseBodies verifies that bodies of intermediate
// retryable responses are closed by the retry loop so connections are not
// leaked, while the final retryable response body is left open for the caller
// to close.
func TestRetryClosesRetryableResponseBodies(t *testing.T) {
	var closed int32
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		resp := newResponse(http.StatusTooManyRequests, "slow")
		resp.Body = &countingReadCloser{
			ReadCloser: resp.Body,
			onClose:    func() { atomic.AddInt32(&closed, 1) },
		}
		return resp, nil
	})

	rc := &RetryClient{
		HTTPClient:  newClient(rt),
		MaxAttempts: 3,
		Sleeper:     (&recordingSleeper{}).sleep,
	}

	factory := func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
	}

	resp, err := rc.Do(context.Background(), factory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The two intermediate responses were closed by the retry loop; the final
	// response is returned to the caller and not yet closed.
	if got := atomic.LoadInt32(&closed); got != 2 {
		t.Fatalf("closed %d bodies before caller close, want 2", got)
	}

	// The caller is responsible for closing the final response body.
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("close final body: %v", err)
	}
	if got := atomic.LoadInt32(&closed); got != 3 {
		t.Fatalf("closed %d bodies after caller close, want 3", got)
	}
}

// TestRetryDoesNotRetryContextErrors verifies that context errors from the
// transport are not retried.
func TestRetryDoesNotRetryContextErrors(t *testing.T) {
	var attempts int32
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&attempts, 1)
		return nil, context.DeadlineExceeded
	})

	rc := &RetryClient{
		HTTPClient:  newClient(rt),
		MaxAttempts: 3,
		Sleeper:     (&recordingSleeper{}).sleep,
	}

	factory := func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
	}

	_, err := rc.Do(context.Background(), factory)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("attempts = %d, want 1", got)
	}
}

// TestRetryReplaysFreshRequestBody verifies that each retry gets a fresh,
// unconsumed request body by checking the body is readable on every attempt.
func TestRetryReplaysFreshRequestBody(t *testing.T) {
	var bodiesRead int32
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		// Consume the body to simulate the transport reading it.
		b, _ := io.ReadAll(req.Body)
		if string(b) == "payload" {
			atomic.AddInt32(&bodiesRead, 1)
		}
		return newResponse(http.StatusInternalServerError, "retry"), nil
	})

	rc := &RetryClient{
		HTTPClient:  newClient(rt),
		MaxAttempts: 3,
		Sleeper:     (&recordingSleeper{}).sleep,
	}

	factory := func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodPost, "https://example.com", bytes.NewReader([]byte("payload")))
	}

	resp, err := rc.Do(context.Background(), factory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp.Body.Close()

	if got := atomic.LoadInt32(&bodiesRead); got != 3 {
		t.Fatalf("read valid body %d times, want 3", got)
	}
}

// countingReadCloser wraps a ReadCloser and calls onClose when closed.
type countingReadCloser struct {
	io.ReadCloser
	onClose func()
}

func (c *countingReadCloser) Close() error {
	if c.onClose != nil {
		c.onClose()
	}
	return c.ReadCloser.Close()
}

// TestRetryFinalRetryableResponseNotReturnedAsError verifies that a terminal
// retryable response is returned to the caller rather than being converted to
// an error. Sensitive body content must therefore never be embedded in a
// returned error; the body itself is intentionally exposed so the endpoint
// layer can parse a structured error.
func TestRetryFinalRetryableResponseNotReturnedAsError(t *testing.T) {
	secret := "super-secret-token"
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return newResponse(http.StatusTooManyRequests, secret), nil
	})

	rc := &RetryClient{
		HTTPClient:  newClient(rt),
		MaxAttempts: 2,
		Sleeper:     (&recordingSleeper{}).sleep,
	}

	factory := func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
	}

	resp, err := rc.Do(context.Background(), factory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	// No error means the secret cannot leak through an error string.
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", resp.StatusCode)
	}

	// The body is still readable by the caller so the endpoint layer can parse
	// a structured error, and it remains subject to the size limit.
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(got) != secret {
		t.Fatalf("body = %q, want %q", string(got), secret)
	}
}

// TestRetryFinalRetryableResponsePreservesRequestIDAndBody verifies that the
// final retryable response retains its headers (e.g. RequestID), that its body
// is readable and bounded by MaxBodySize, that it is not closed before being
// returned, and that the caller's Close is counted exactly once.
func TestRetryFinalRetryableResponsePreservesRequestIDAndBody(t *testing.T) {
	const requestID = "req-12345"
	const bodyText = `{"error":"rate_limited","message":"slow down"}`

	var closed int32
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		resp := newResponse(http.StatusTooManyRequests, bodyText)
		resp.Header.Set("X-Request-ID", requestID)
		resp.Body = &countingReadCloser{
			ReadCloser: resp.Body,
			onClose:    func() { atomic.AddInt32(&closed, 1) },
		}
		return resp, nil
	})

	rc := &RetryClient{
		HTTPClient:  newClient(rt),
		MaxAttempts: 3,
		Sleeper:     (&recordingSleeper{}).sleep,
		MaxBodySize: 10,
	}

	factory := func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
	}

	resp, err := rc.Do(context.Background(), factory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The final response must not have been closed by the retry loop.
	if got := atomic.LoadInt32(&closed); got != 2 {
		t.Fatalf("closed %d bodies before caller close, want 2 (intermediate only)", got)
	}

	// RequestID header is preserved.
	if got := resp.Header.Get("X-Request-ID"); got != requestID {
		t.Fatalf("X-Request-ID = %q, want %q", got, requestID)
	}
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", resp.StatusCode)
	}

	// Body is readable but bounded by MaxBodySize.
	got, err := io.ReadAll(resp.Body)
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("error = %v, want ErrBodyTooLarge", err)
	}
	if len(got) > 10 {
		t.Fatalf("read %d bytes, want at most 10", len(got))
	}

	// Caller closes the final body exactly once.
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("close body: %v", err)
	}
	if got := atomic.LoadInt32(&closed); got != 3 {
		t.Fatalf("closed %d bodies after caller close, want 3", got)
	}
}

// TestParseRetryAfter covers the Retry-After parsing helper directly.
func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name   string
		value  string
		want   time.Duration
		wantOK bool
	}{
		{"empty", "", 0, false},
		{"delta", "3", 3 * time.Second, true},
		{"negative", "-1", 0, false},
		{"http-date-future", now.Add(90 * time.Second).UTC().Format(http.TimeFormat), 90 * time.Second, true},
		{"http-date-past", now.Add(-5 * time.Second).UTC().Format(http.TimeFormat), 0, true},
		{"garbage", "not-a-date", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseRetryAfter(tc.value, now)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if ok && got != tc.want {
				t.Fatalf("duration = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestBackoffIsDeterministic verifies the backoff sequence is fixed and bounded.
func TestBackoffIsDeterministic(t *testing.T) {
	want := []time.Duration{
		200 * time.Millisecond,
		400 * time.Millisecond,
		800 * time.Millisecond,
		1600 * time.Millisecond,
		2 * time.Second, // capped
		2 * time.Second, // capped
	}
	for i, w := range want {
		if got := backoff(i + 1); got != w {
			t.Fatalf("backoff(%d) = %v, want %v", i+1, got, w)
		}
	}
}

// ---------------------------------------------------------------------------
// New regression tests for Task 6 quality acceptance.
// ---------------------------------------------------------------------------

// TestRetryNilReceiver verifies that calling Do on a nil *RetryClient returns
// a clear error rather than panicking.
func TestRetryNilReceiver(t *testing.T) {
	var rc *RetryClient
	factory := func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
	}
	resp, err := rc.Do(context.Background(), factory)
	if err == nil {
		t.Fatal("expected error from nil *RetryClient")
	}
	if resp != nil {
		t.Fatalf("expected nil response, got %+v", resp)
	}
	if !strings.Contains(err.Error(), "nil *RetryClient") {
		t.Fatalf("error = %v, want it to mention nil *RetryClient", err)
	}
}

// TestRetryNilContext verifies that a nil context returns a clear error rather
// than panicking.
func TestRetryNilContext(t *testing.T) {
	rc := &RetryClient{
		HTTPClient:  newClient(roundTripFunc(func(*http.Request) (*http.Response, error) { return newResponse(200, "ok"), nil })),
		MaxAttempts: 1,
		Sleeper:     (&recordingSleeper{}).sleep,
	}
	factory := func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
	}
	//lint:ignore SA1012 verifies Do rejects a nil context without invoking the factory
	resp, err := rc.Do(nil, factory)
	if err == nil {
		t.Fatal("expected error from nil context")
	}
	if resp != nil {
		t.Fatalf("expected nil response, got %+v", resp)
	}
	if !strings.Contains(err.Error(), "nil context") {
		t.Fatalf("error = %v, want it to mention nil context", err)
	}
}

// TestRetryNilFactory verifies that a nil RequestFactory returns a clear error
// rather than panicking.
func TestRetryNilFactory(t *testing.T) {
	rc := &RetryClient{
		HTTPClient:  newClient(roundTripFunc(func(*http.Request) (*http.Response, error) { return newResponse(200, "ok"), nil })),
		MaxAttempts: 1,
		Sleeper:     (&recordingSleeper{}).sleep,
	}
	resp, err := rc.Do(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error from nil RequestFactory")
	}
	if resp != nil {
		t.Fatalf("expected nil response, got %+v", resp)
	}
	if !strings.Contains(err.Error(), "nil RequestFactory") {
		t.Fatalf("error = %v, want it to mention nil RequestFactory", err)
	}
}

// TestRetryFactoryReturnsNilRequestNoError verifies that a factory returning
// (nil, nil) yields a clear error rather than a nil-pointer panic downstream.
func TestRetryFactoryReturnsNilRequestNoError(t *testing.T) {
	rc := &RetryClient{
		HTTPClient:  newClient(roundTripFunc(func(*http.Request) (*http.Response, error) { return newResponse(200, "ok"), nil })),
		MaxAttempts: 1,
		Sleeper:     (&recordingSleeper{}).sleep,
	}
	factory := func(ctx context.Context) (*http.Request, error) {
		return nil, nil
	}
	resp, err := rc.Do(context.Background(), factory)
	if err == nil {
		t.Fatal("expected error when factory returns (nil, nil)")
	}
	if resp != nil {
		t.Fatalf("expected nil response, got %+v", resp)
	}
	if !strings.Contains(err.Error(), "nil request") {
		t.Fatalf("error = %v, want it to mention nil request", err)
	}
}

// TestRetryFactoryReturnsRequestAndErrorClosesBody verifies that when a factory
// returns both a non-nil request and an error, the RetryClient closes the
// request body before returning the error.
func TestRetryFactoryReturnsRequestAndErrorClosesBody(t *testing.T) {
	var closed int32
	rc := &RetryClient{
		HTTPClient:  newClient(roundTripFunc(func(*http.Request) (*http.Response, error) { return newResponse(200, "ok"), nil })),
		MaxAttempts: 1,
		Sleeper:     (&recordingSleeper{}).sleep,
	}
	factory := func(ctx context.Context) (*http.Request, error) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.com", strings.NewReader("payload"))
		req.Body = &countingReadCloser{
			ReadCloser: req.Body,
			onClose:    func() { atomic.AddInt32(&closed, 1) },
		}
		return req, errors.New("factory boom")
	}
	resp, err := rc.Do(context.Background(), factory)
	if err == nil {
		t.Fatal("expected error from factory")
	}
	if resp != nil {
		t.Fatalf("expected nil response, got %+v", resp)
	}
	if !strings.Contains(err.Error(), "factory boom") {
		t.Fatalf("error = %v, want it to wrap factory boom", err)
	}
	if got := atomic.LoadInt32(&closed); got != 1 {
		t.Fatalf("body closed %d times, want 1", got)
	}
}

// TestRetryTypedNilClockDoesNotPanic verifies that a typed-nil Clock (a non-nil
// interface holding a nil pointer) falls back to the system clock instead of
// panicking when Now() is called.
func TestRetryTypedNilClockDoesNotPanic(t *testing.T) {
	var fc *fixedClock // typed nil: *fixedClock(nil) assigned to Clock interface
	rc := &RetryClient{
		HTTPClient: newClient(roundTripFunc(func(*http.Request) (*http.Response, error) {
			resp := newResponse(http.StatusTooManyRequests, "slow")
			resp.Header.Set("Retry-After", "1")
			return resp, nil
		})),
		MaxAttempts: 2,
		Sleeper:     (&recordingSleeper{}).sleep,
		Clock:       fc, // typed nil
	}
	factory := func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
	}
	// Must not panic.
	resp, err := rc.Do(context.Background(), factory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp.Body.Close()
}

// TestRetryNegativeConfigReturnsError verifies that negative MaxAttempts,
// MaxBodySize, and MaxRetryAfter are rejected as configuration errors.
func TestRetryNegativeConfigReturnsError(t *testing.T) {
	factory := func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
	}

	cases := []struct {
		name string
		rc   *RetryClient
		want string
	}{
		{
			name: "MaxAttempts",
			rc:   &RetryClient{MaxAttempts: -1, Sleeper: (&recordingSleeper{}).sleep},
			want: "MaxAttempts",
		},
		{
			name: "MaxBodySize",
			rc:   &RetryClient{MaxBodySize: -1, Sleeper: (&recordingSleeper{}).sleep},
			want: "MaxBodySize",
		},
		{
			name: "MaxRetryAfter",
			rc:   &RetryClient{MaxRetryAfter: -1, Sleeper: (&recordingSleeper{}).sleep},
			want: "MaxRetryAfter",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := tc.rc.Do(context.Background(), factory)
			if err == nil {
				t.Fatalf("expected error for negative %s", tc.name)
			}
			if resp != nil {
				t.Fatalf("expected nil response, got %+v", resp)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to mention %s", err, tc.want)
			}
		})
	}
}

// TestRetryRetryAfterOverflowDoesNotGoNegative verifies that very large
// delta-seconds values do not overflow into a negative (or otherwise wrong)
// duration when multiplied by time.Second.
func TestRetryRetryAfterOverflowDoesNotGoNegative(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name  string
		value string
	}{
		{"max-int64", fmt.Sprintf("%d", int64(math.MaxInt64))},
		{"just-over-safe-boundary", fmt.Sprintf("%d", maxRetryAfterSeconds+1)},
		{"huge", "99999999999999999999"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseRetryAfter(tc.value, now)
			if !ok {
				t.Fatalf("expected ok=true for %q", tc.value)
			}
			if got < 0 {
				t.Fatalf("duration = %v, must not be negative (overflow)", got)
			}
			if got == 0 {
				t.Fatalf("duration = 0, expected a positive (clamped) value")
			}
		})
	}
}

// TestRetryRetryAfterCapClampsDelay verifies that a Retry-After value exceeding
// MaxRetryAfter is clamped to the cap rather than being skipped or allowed to
// run unbounded. Normal values below the cap are still respected.
func TestRetryRetryAfterCapClampsDelay(t *testing.T) {
	t.Run("clamps-excess", func(t *testing.T) {
		sleeper := &recordingSleeper{}
		rt := roundTripFunc(func(*http.Request) (*http.Response, error) {
			resp := newResponse(http.StatusTooManyRequests, "slow")
			resp.Header.Set("Retry-After", "3600") // 1 hour
			return resp, nil
		})
		rc := &RetryClient{
			HTTPClient:    newClient(rt),
			MaxAttempts:   3,
			Sleeper:       sleeper.sleep,
			MaxRetryAfter: 5 * time.Second,
		}
		factory := func(ctx context.Context) (*http.Request, error) {
			return http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
		}
		resp, err := rc.Do(context.Background(), factory)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		resp.Body.Close()

		if len(sleeper.calls) != 2 {
			t.Fatalf("sleeper called %d times, want 2", len(sleeper.calls))
		}
		for i, d := range sleeper.calls {
			if d != 5*time.Second {
				t.Fatalf("sleep %d = %v, want clamped 5s", i, d)
			}
		}
	})

	t.Run("respects-normal", func(t *testing.T) {
		sleeper := &recordingSleeper{}
		rt := roundTripFunc(func(*http.Request) (*http.Response, error) {
			resp := newResponse(http.StatusTooManyRequests, "slow")
			resp.Header.Set("Retry-After", "3")
			return resp, nil
		})
		rc := &RetryClient{
			HTTPClient:    newClient(rt),
			MaxAttempts:   3,
			Sleeper:       sleeper.sleep,
			MaxRetryAfter: 10 * time.Second,
		}
		factory := func(ctx context.Context) (*http.Request, error) {
			return http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
		}
		resp, err := rc.Do(context.Background(), factory)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		resp.Body.Close()

		if len(sleeper.calls) != 2 {
			t.Fatalf("sleeper called %d times, want 2", len(sleeper.calls))
		}
		for i, d := range sleeper.calls {
			if d != 3*time.Second {
				t.Fatalf("sleep %d = %v, want 3s (below cap)", i, d)
			}
		}
	})

	t.Run("far-future-http-date-clamped", func(t *testing.T) {
		now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
		sleeper := &recordingSleeper{}
		rt := roundTripFunc(func(*http.Request) (*http.Response, error) {
			resp := newResponse(http.StatusTooManyRequests, "slow")
			// 10 years in the future.
			resp.Header.Set("Retry-After", now.AddDate(10, 0, 0).UTC().Format(http.TimeFormat))
			return resp, nil
		})
		rc := &RetryClient{
			HTTPClient:    newClient(rt),
			MaxAttempts:   3,
			Sleeper:       sleeper.sleep,
			Clock:         fixedClock{t: now},
			MaxRetryAfter: 2 * time.Second,
		}
		factory := func(ctx context.Context) (*http.Request, error) {
			return http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
		}
		resp, err := rc.Do(context.Background(), factory)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		resp.Body.Close()

		if len(sleeper.calls) != 2 {
			t.Fatalf("sleeper called %d times, want 2", len(sleeper.calls))
		}
		for i, d := range sleeper.calls {
			if d != 2*time.Second {
				t.Fatalf("sleep %d = %v, want clamped 2s", i, d)
			}
		}
	})
}

// TestRetryTransportErrorStripsURLSecrets verifies that a transport-level
// *url.Error has its query and fragment stripped before being returned, so
// secrets such as access_token cannot leak through the error string. The
// underlying cause is still reachable via errors.Is.
func TestRetryTransportErrorStripsURLSecrets(t *testing.T) {
	sentinel := errors.New("dial tcp: connection refused")
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		// Simulate what http.Client.Do returns on transport failure: a
		// *url.Error wrapping the real cause, with the full request URL.
		return nil, &url.Error{
			Op:  "Get",
			URL: "https://example.com/token?access_token=abc123&client_secret=topsecret#frag",
			Err: sentinel,
		}
	})

	rc := &RetryClient{
		HTTPClient:  newClient(rt),
		MaxAttempts: 1,
		Sleeper:     (&recordingSleeper{}).sleep,
	}
	factory := func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com/token?access_token=abc123&client_secret=topsecret#frag", nil)
	}

	_, err := rc.Do(context.Background(), factory)
	if err == nil {
		t.Fatal("expected error")
	}

	msg := err.Error()
	for _, secret := range []string{"abc123", "topsecret", "access_token", "client_secret"} {
		if strings.Contains(msg, secret) {
			t.Fatalf("error string leaked secret %q: %v", secret, err)
		}
	}
	// Scheme, host, and path may remain.
	if !strings.Contains(msg, "https://example.com/token") {
		t.Fatalf("error string should retain scheme/host/path: %v", err)
	}
	// The underlying sentinel must still be reachable.
	if !errors.Is(err, sentinel) {
		t.Fatalf("errors.Is(err, sentinel) = false, want true; err = %v", err)
	}
}

// TestRetryTransportErrorPreservesContextErrors verifies that context
// cancellation/deadline semantics survive error sanitization.
func TestRetryTransportErrorPreservesContextErrors(t *testing.T) {
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, &url.Error{
			Op:  "Get",
			URL: "https://example.com/token?access_token=abc",
			Err: context.Canceled,
		}
	})
	rc := &RetryClient{
		HTTPClient:  newClient(rt),
		MaxAttempts: 1,
		Sleeper:     (&recordingSleeper{}).sleep,
	}
	factory := func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com/token", nil)
	}
	_, err := rc.Do(context.Background(), factory)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("errors.Is(err, context.Canceled) = false, want true; err = %v", err)
	}
	if strings.Contains(err.Error(), "abc") {
		t.Fatalf("error leaked secret: %v", err)
	}
}

// TestLimitedReadCloserEdgeCases exercises the limitedReadCloser directly:
// exact-limit, one-byte-over, short read, (n>0, io.EOF), and zero-length Read
// which must not consume underlying data.
func TestLimitedReadCloserEdgeCases(t *testing.T) {
	t.Run("exact-limit", func(t *testing.T) {
		body := io.NopCloser(strings.NewReader("hello"))
		lrc := &limitedReadCloser{r: body, remaining: 5}
		got, err := io.ReadAll(lrc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(got) != "hello" {
			t.Fatalf("got %q, want %q", got, "hello")
		}
	})

	t.Run("one-byte-over", func(t *testing.T) {
		body := io.NopCloser(strings.NewReader("hello!"))
		lrc := &limitedReadCloser{r: body, remaining: 5}
		got, err := io.ReadAll(lrc)
		if !errors.Is(err, ErrBodyTooLarge) {
			t.Fatalf("error = %v, want ErrBodyTooLarge", err)
		}
		if string(got) != "hello" {
			t.Fatalf("got %q, want %q (first 5 bytes)", got, "hello")
		}
	})

	t.Run("short-read", func(t *testing.T) {
		body := io.NopCloser(strings.NewReader("hi"))
		lrc := &limitedReadCloser{r: body, remaining: 10}
		got, err := io.ReadAll(lrc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(got) != "hi" {
			t.Fatalf("got %q, want %q", got, "hi")
		}
	})

	t.Run("n-positive-then-eof", func(t *testing.T) {
		// A reader that returns data and io.EOF in the same call.
		body := io.NopCloser(&eofReader{data: []byte("data")})
		lrc := &limitedReadCloser{r: body, remaining: 10}
		got, err := io.ReadAll(lrc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(got) != "data" {
			t.Fatalf("got %q, want %q", got, "data")
		}
	})

	t.Run("zero-length-read-does-not-consume", func(t *testing.T) {
		// A reader that records every Read call.
		tr := &trackingReader{data: []byte("abc")}
		lrc := &limitedReadCloser{r: io.NopCloser(tr), remaining: 3}

		// Zero-length read must not touch the underlying reader.
		n, err := lrc.Read(nil)
		if n != 0 || err != nil {
			t.Fatalf("Read(nil) = (%d, %v), want (0, nil)", n, err)
		}
		if tr.readCalls != 0 {
			t.Fatalf("underlying reader called %d times, want 0", tr.readCalls)
		}

		// A real read still works and consumes data.
		buf := make([]byte, 3)
		n, err = lrc.Read(buf)
		if n != 3 || err != nil {
			t.Fatalf("Read(buf) = (%d, %v), want (3, nil)", n, err)
		}
		if string(buf) != "abc" {
			t.Fatalf("got %q, want %q", buf, "abc")
		}
	})
}

// eofReader returns all its data and io.EOF in a single Read call.
type eofReader struct {
	data []byte
	done bool
}

func (r *eofReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	n := copy(p, r.data)
	r.done = true
	return n, io.EOF
}

// trackingReader records the number of Read calls.
type trackingReader struct {
	data      []byte
	readCalls int
}

func (r *trackingReader) Read(p []byte) (int, error) {
	r.readCalls++
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

// ---------------------------------------------------------------------------
// Regression tests for Task 6 final quality review (Important 1 & 2).
// ---------------------------------------------------------------------------

// TestParseRetryAfterRejectsNonDigits verifies that RFC delta-seconds values
// that are not pure ASCII DIGIT sequences are rejected as invalid rather than
// being clamped or misinterpreted. In particular, negative numbers (including
// negative overflow), explicit '+' signs, decimals, and values with internal
// whitespace must NOT enter the positive clamp path.
func TestParseRetryAfterRejectsNonDigits(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		value string
	}{
		{"negative-one", "-1"},
		{"negative-overflow-minint64-minus-one", "-9223372036854775809"},
		{"negative-huge", "-99999999999999999999"},
		{"explicit-plus", "+1"},
		{"decimal", "1.5"},
		{"internal-space", "1 2"},
		{"leading-plus-zeros", "+007"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseRetryAfter(tc.value, now)
			if ok {
				t.Fatalf("ok = true for %q, want false (invalid delta-seconds); got=%v", tc.value, got)
			}
			if got != 0 {
				t.Fatalf("duration = %v for %q, want 0 when invalid", got, tc.value)
			}
		})
	}
}

// TestParseRetryAfterPositiveOverflowClamps verifies that positive overflow is
// clamped to the maximum representable Duration (and is valid), in contrast to
// negative overflow which is rejected.
func TestParseRetryAfterPositiveOverflowClamps(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	cases := []string{
		"99999999999999999999",
		fmt.Sprintf("%d", uint64(math.MaxUint64)),
	}
	for _, v := range cases {
		t.Run(v, func(t *testing.T) {
			got, ok := parseRetryAfter(v, now)
			if !ok {
				t.Fatalf("ok = false for %q, want true (positive overflow clamped)", v)
			}
			if got != time.Duration(math.MaxInt64) {
				t.Fatalf("duration = %v, want MaxInt64 (clamped)", got)
			}
		})
	}
}

// TestRetryAfterNegativeUsesDefaultBackoffNotCap proves via the Do path with a
// fake sleeper that an out-of-range negative Retry-After falls back to the
// default exponential backoff instead of being clamped to MaxRetryAfter. If the
// negative were misinterpreted as a huge positive, the delay would be capped to
// MaxRetryAfter (5s) rather than the 200ms base backoff.
func TestRetryAfterNegativeUsesDefaultBackoffNotCap(t *testing.T) {
	cases := []string{
		"-1",
		"-9223372036854775809",
		"-99999999999999999999",
	}
	for _, ra := range cases {
		t.Run(ra, func(t *testing.T) {
			sleeper := &recordingSleeper{}
			rt := roundTripFunc(func(*http.Request) (*http.Response, error) {
				resp := newResponse(http.StatusTooManyRequests, "slow")
				resp.Header.Set("Retry-After", ra)
				return resp, nil
			})
			rc := &RetryClient{
				HTTPClient:    newClient(rt),
				MaxAttempts:   2,
				Sleeper:       sleeper.sleep,
				MaxRetryAfter: 5 * time.Second,
			}
			factory := func(ctx context.Context) (*http.Request, error) {
				return http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
			}
			resp, err := rc.Do(context.Background(), factory)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			resp.Body.Close()

			if len(sleeper.calls) != 1 {
				t.Fatalf("sleeper called %d times, want 1", len(sleeper.calls))
			}
			// Default backoff for attempt 1 is 200ms. If the negative had been
			// clamped to MaxInt64 it would be capped to MaxRetryAfter (5s).
			if sleeper.calls[0] != 200*time.Millisecond {
				t.Fatalf("sleep = %v, want 200ms default backoff (not 5s cap)", sleeper.calls[0])
			}
		})
	}
}

// TestRedactURLFailClosed verifies that redactURL never returns the original
// URL when parsing fails, and always strips query/fragment. A malformed URL
// (e.g. illegal percent-encoding) must yield the fixed placeholder.
func TestRedactURLFailClosed(t *testing.T) {
	t.Run("strips-query-and-fragment", func(t *testing.T) {
		got := redactURL("https://example.com/token?access_token=abc&client_secret=xyz#frag")
		if got != "https://example.com/token" {
			t.Fatalf("redactURL = %q, want scheme/host/path only", got)
		}
	})
	t.Run("malformed-returns-placeholder", func(t *testing.T) {
		// An illegal percent sequence in the host makes url.Parse fail.
		got := redactURL("https://exa%mple.com/token?authorization_code=secret")
		if got == "" || got == "https://exa%mple.com/token?authorization_code=secret" {
			t.Fatalf("redactURL returned empty or original URL: %q", got)
		}
		if strings.Contains(got, "authorization_code") || strings.Contains(got, "secret") {
			t.Fatalf("redactURL leaked secret: %q", got)
		}
	})
	t.Run("plain-path", func(t *testing.T) {
		got := redactURL("https://example.com/path")
		if got != "https://example.com/path" {
			t.Fatalf("redactURL = %q, want unchanged", got)
		}
	})
}

// TestSanitizeTransportErrorNestedIllegalPercent verifies that a nested
// *url.Error whose URL contains an illegal percent sequence plus a secret
// query does not leak the secret through the error string.
func TestSanitizeTransportErrorNestedIllegalPercent(t *testing.T) {
	sentinel := errors.New("dial tcp: connection refused")
	nested := &url.Error{
		Op:  "Get",
		URL: "https://exa%mple.com/token?authorization_code=supersecret&access_token=abc",
		Err: sentinel,
	}
	// Wrap again to simulate http.Client.Do re-wrapping a transport error.
	wrapped := fmt.Errorf("request failed: %w", nested)

	err := sanitizeTransportError(wrapped)
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	msg := err.Error()
	for _, secret := range []string{"supersecret", "abc", "authorization_code", "access_token"} {
		if strings.Contains(msg, secret) {
			t.Fatalf("error leaked %q: %v", secret, err)
		}
	}
	// The underlying cause must still be reachable.
	if !errors.Is(err, sentinel) {
		t.Fatalf("errors.Is(err, sentinel) = false, want true; err = %v", err)
	}
}

// TestSanitizeTransportErrorJoinPreservesBothCauses verifies that when the
// transport error is built with errors.Join, errors.Is remains true for every
// joined cause after sanitization.
func TestSanitizeTransportErrorJoinPreservesBothCauses(t *testing.T) {
	sentinel := errors.New("dial tcp: connection refused")
	secondCause := errors.New("secondary failure")
	urlErr := &url.Error{
		Op:  "Get",
		URL: "https://example.com/token?access_token=abc",
		Err: sentinel,
	}
	joined := errors.Join(urlErr, secondCause)

	err := sanitizeTransportError(joined)
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("errors.Is(err, sentinel) = false, want true; err = %v", err)
	}
	if !errors.Is(err, secondCause) {
		t.Fatalf("errors.Is(err, secondCause) = false, want true; err = %v", err)
	}
	// Secret must not leak through the rendered error string.
	if strings.Contains(err.Error(), "abc") || strings.Contains(err.Error(), "access_token") {
		t.Fatalf("error leaked secret: %v", err)
	}
}

// TestSanitizeTransportErrorValidURLRetainsSchemeHostPath verifies that a
// well-formed URL still exposes scheme/host/path in the sanitized error
// string while dropping the query/fragment.
func TestSanitizeTransportErrorValidURLRetainsSchemeHostPath(t *testing.T) {
	sentinel := errors.New("dial tcp: connection refused")
	urlErr := &url.Error{
		Op:  "Post",
		URL: "https://example.com/token?client_secret=topsecret#frag",
		Err: sentinel,
	}
	err := sanitizeTransportError(urlErr)
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "https://example.com/token") {
		t.Fatalf("error should retain scheme/host/path: %v", err)
	}
	for _, secret := range []string{"topsecret", "client_secret", "frag"} {
		if strings.Contains(msg, secret) {
			t.Fatalf("error leaked %q: %v", secret, err)
		}
	}
}

// TestSanitizeTransportErrorPreservesContextErrors verifies that context
// cancellation and deadline errors remain reachable via errors.Is after
// sanitization, even when wrapped in a *url.Error.
func TestSanitizeTransportErrorPreservesContextErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want error
	}{
		{"canceled", &url.Error{Op: "Get", URL: "https://example.com/?access_token=abc", Err: context.Canceled}, context.Canceled},
		{"deadline", &url.Error{Op: "Get", URL: "https://example.com/?access_token=abc", Err: context.DeadlineExceeded}, context.DeadlineExceeded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := sanitizeTransportError(tc.err)
			if !errors.Is(err, tc.want) {
				t.Fatalf("errors.Is(err, %v) = false, want true; err = %v", tc.want, err)
			}
			if strings.Contains(err.Error(), "abc") {
				t.Fatalf("error leaked secret: %v", err)
			}
		})
	}
}
