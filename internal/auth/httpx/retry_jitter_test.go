package httpx

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRetryInjectedJitterFallback(t *testing.T) {
	cases := []struct {
		name       string
		jitter     time.Duration
		networkErr bool
		retryAfter string
		want       []time.Duration
	}{
		{
			name:       "network-50ms",
			jitter:     50 * time.Millisecond,
			networkErr: true,
			want:       []time.Duration{250 * time.Millisecond, 450 * time.Millisecond},
		},
		{
			name:   "http-99ms-without-retry-after",
			jitter: 99 * time.Millisecond,
			want:   []time.Duration{299 * time.Millisecond, 499 * time.Millisecond},
		},
		{
			name:       "http-50ms-with-invalid-retry-after",
			jitter:     50 * time.Millisecond,
			retryAfter: "not-a-date",
			want:       []time.Duration{250 * time.Millisecond, 450 * time.Millisecond},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var attempts int32
			rt := roundTripFunc(func(*http.Request) (*http.Response, error) {
				atomic.AddInt32(&attempts, 1)
				if tc.networkErr {
					return nil, errors.New("connection reset")
				}
				resp := newResponse(http.StatusInternalServerError, "retry")
				if tc.retryAfter != "" {
					resp.Header.Set("Retry-After", tc.retryAfter)
				}
				return resp, nil
			})
			sleeper := &recordingSleeper{}
			rc := &RetryClient{
				HTTPClient:  newClient(rt),
				MaxAttempts: 3,
				Sleeper:     sleeper.sleep,
				Jitter:      func() time.Duration { return tc.jitter },
			}

			factory := func(ctx context.Context) (*http.Request, error) {
				return http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
			}
			resp, err := rc.Do(context.Background(), factory)
			if tc.networkErr {
				if err == nil || resp != nil {
					t.Fatalf("network result = (%v, %v), want terminal error", resp, err)
				}
			} else {
				if err != nil || resp == nil {
					t.Fatalf("HTTP result = (%v, %v), want final response", resp, err)
				}
				resp.Body.Close()
			}
			if got := atomic.LoadInt32(&attempts); got != 3 {
				t.Fatalf("attempts = %d, want 3", got)
			}
			if len(sleeper.calls) != len(tc.want) {
				t.Fatalf("sleeper calls = %d, want %d", len(sleeper.calls), len(tc.want))
			}
			for i, want := range tc.want {
				if got := sleeper.calls[i]; got != want {
					t.Fatalf("sleep %d = %v, want %v", i, got, want)
				}
			}
		})
	}
}

func TestRetryValidRetryAfterBypassesJitter(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name       string
		retryAfter string
		want       []time.Duration
	}{
		{name: "zero", retryAfter: "0"},
		{
			name:       "http-date",
			retryAfter: now.Add(time.Second).UTC().Format(http.TimeFormat),
			want:       []time.Duration{time.Second, time.Second},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var jitterCalls int32
			rt := roundTripFunc(func(*http.Request) (*http.Response, error) {
				resp := newResponse(http.StatusTooManyRequests, "retry")
				resp.Header.Set("Retry-After", tc.retryAfter)
				return resp, nil
			})
			sleeper := &recordingSleeper{}
			rc := &RetryClient{
				HTTPClient:  newClient(rt),
				MaxAttempts: 3,
				Sleeper:     sleeper.sleep,
				Clock:       fixedClock{t: now},
				Jitter: func() time.Duration {
					atomic.AddInt32(&jitterCalls, 1)
					return 99 * time.Millisecond
				},
			}

			factory := func(ctx context.Context) (*http.Request, error) {
				return http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
			}
			resp, err := rc.Do(context.Background(), factory)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			resp.Body.Close()
			if got := atomic.LoadInt32(&jitterCalls); got != 0 {
				t.Fatalf("jitter calls = %d, want 0 for valid Retry-After", got)
			}
			if len(sleeper.calls) != len(tc.want) {
				t.Fatalf("sleeper calls = %d, want %d", len(sleeper.calls), len(tc.want))
			}
			for i, want := range tc.want {
				if got := sleeper.calls[i]; got != want {
					t.Fatalf("sleep %d = %v, want %v", i, got, want)
				}
			}
		})
	}
}

func TestRetryFallbackJitterHonorsMaxRetryAfter(t *testing.T) {
	var jitterCalls int32
	sleeper := &recordingSleeper{}
	rc := &RetryClient{
		HTTPClient: newClient(roundTripFunc(func(*http.Request) (*http.Response, error) {
			return newResponse(http.StatusInternalServerError, "retry"), nil
		})),
		MaxAttempts:   2,
		Sleeper:       sleeper.sleep,
		MaxRetryAfter: 220 * time.Millisecond,
		Jitter: func() time.Duration {
			atomic.AddInt32(&jitterCalls, 1)
			return 50 * time.Millisecond
		},
	}
	factory := func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
	}
	resp, err := rc.Do(context.Background(), factory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp.Body.Close()
	if len(sleeper.calls) != 1 || sleeper.calls[0] != 220*time.Millisecond {
		t.Fatalf("sleep calls = %v, want [220ms]", sleeper.calls)
	}
	if got := atomic.LoadInt32(&jitterCalls); got != 1 {
		t.Fatalf("jitter calls = %d, want 1", got)
	}
}

func TestRetryOutOfRangeJitterFallsBackToZero(t *testing.T) {
	cases := []struct {
		name   string
		jitter time.Duration
	}{
		{name: "negative", jitter: -time.Nanosecond},
		{name: "upper-bound", jitter: 100 * time.Millisecond},
		{name: "overflow", jitter: time.Duration(1<<63 - 1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sleeper := &recordingSleeper{}
			rc := &RetryClient{
				HTTPClient: newClient(roundTripFunc(func(*http.Request) (*http.Response, error) {
					return newResponse(http.StatusInternalServerError, "retry"), nil
				})),
				MaxAttempts: 2,
				Sleeper:     sleeper.sleep,
				Jitter:      func() time.Duration { return tc.jitter },
			}
			factory := func(ctx context.Context) (*http.Request, error) {
				return http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
			}
			resp, err := rc.Do(context.Background(), factory)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			resp.Body.Close()
			if len(sleeper.calls) != 1 || sleeper.calls[0] != 200*time.Millisecond {
				t.Fatalf("sleep calls = %v, want [200ms]", sleeper.calls)
			}
		})
	}
}

func TestRetryJitterNotSampledWithoutFallbackWait(t *testing.T) {
	cases := []struct {
		name string
		rc   func(*int32) *RetryClient
		ctx  func() context.Context
	}{
		{
			name: "first-success",
			rc: func(calls *int32) *RetryClient {
				return &RetryClient{
					HTTPClient: newClient(roundTripFunc(func(*http.Request) (*http.Response, error) {
						return newResponse(http.StatusOK, "ok"), nil
					})),
					MaxAttempts: 3,
					Jitter: func() time.Duration {
						atomic.AddInt32(calls, 1)
						return 50 * time.Millisecond
					},
				}
			},
			ctx: context.Background,
		},
		{
			name: "last-attempt",
			rc: func(calls *int32) *RetryClient {
				return &RetryClient{
					HTTPClient: newClient(roundTripFunc(func(*http.Request) (*http.Response, error) {
						return newResponse(http.StatusInternalServerError, "retry"), nil
					})),
					MaxAttempts: 1,
					Jitter: func() time.Duration {
						atomic.AddInt32(calls, 1)
						return 50 * time.Millisecond
					},
				}
			},
			ctx: context.Background,
		},
		{
			name: "context-canceled",
			rc: func(calls *int32) *RetryClient {
				return &RetryClient{
					HTTPClient: newClient(roundTripFunc(func(*http.Request) (*http.Response, error) {
						return newResponse(http.StatusInternalServerError, "retry"), nil
					})),
					MaxAttempts: 3,
					Jitter: func() time.Duration {
						atomic.AddInt32(calls, 1)
						return 50 * time.Millisecond
					},
				}
			},
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var jitterCalls int32
			rc := tc.rc(&jitterCalls)
			factory := func(ctx context.Context) (*http.Request, error) {
				return http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
			}
			resp, err := rc.Do(tc.ctx(), factory)
			if tc.name == "first-success" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				resp.Body.Close()
			} else if tc.name == "last-attempt" {
				if err != nil || resp == nil {
					t.Fatalf("result = (%v, %v), want final response", resp, err)
				}
				resp.Body.Close()
			} else if !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v, want context.Canceled", err)
			}
			if got := atomic.LoadInt32(&jitterCalls); got != 0 {
				t.Fatalf("jitter calls = %d, want 0", got)
			}
		})
	}
}

func TestRetryDefaultJitterRange(t *testing.T) {
	sleeper := &recordingSleeper{}
	rc := &RetryClient{
		HTTPClient: newClient(roundTripFunc(func(*http.Request) (*http.Response, error) {
			return newResponse(http.StatusInternalServerError, "retry"), nil
		})),
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
		t.Fatalf("sleeper calls = %d, want 2", len(sleeper.calls))
	}
	for i, got := range sleeper.calls {
		base := backoff(i + 1)
		if got < base || got >= base+100*time.Millisecond {
			t.Fatalf("sleep %d = %v, want [%v, %v)", i, got, base, base+100*time.Millisecond)
		}
	}
}

func TestNewRetryClientLeavesJitterUnset(t *testing.T) {
	if got := NewRetryClient().Jitter; got != nil {
		t.Fatal("NewRetryClient must not retain a shared default jitter closure")
	}
}

func TestRetryDefaultJitterConcurrentDo(t *testing.T) {
	const workers = 16
	var attempts int64
	var sleeps int64
	rc := &RetryClient{
		HTTPClient: newClient(roundTripFunc(func(*http.Request) (*http.Response, error) {
			atomic.AddInt64(&attempts, 1)
			return newResponse(http.StatusInternalServerError, "retry"), nil
		})),
		MaxAttempts: 3,
		Sleeper: func(context.Context, time.Duration) error {
			atomic.AddInt64(&sleeps, 1)
			return nil
		},
	}
	factory := func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
	}

	errs := make(chan error, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			resp, err := rc.Do(context.Background(), factory)
			if resp != nil {
				_ = resp.Body.Close()
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("unexpected concurrent Do error: %v", err)
		}
	}
	if got, want := atomic.LoadInt64(&attempts), int64(workers*3); got != want {
		t.Fatalf("attempts = %d, want %d", got, want)
	}
	if got, want := atomic.LoadInt64(&sleeps), int64(workers*2); got != want {
		t.Fatalf("sleeps = %d, want %d", got, want)
	}
}
