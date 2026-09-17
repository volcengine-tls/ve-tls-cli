package console

import (
	"bytes"
	"context"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth/httpx"
)

// Documents the current policy: one refresh has bounded HTTP attempts, but
// a failed refresh is not cached or cooled down across Retrieve calls.
func TestConsoleRefreshRetryBudgetIsPerRetrieve(t *testing.T) {
	const session = "trn:iam::1:user/retry-policy"
	now := time.Unix(1700000000, 0)
	cache := newFakeCache()
	seed := makeCacheBytes(session, now.Add(-3599*time.Second), 3600, "test-refresh")
	cache.data[session] = seed
	var requests int
	var waits []time.Duration
	var jitterCalls int
	retry := &httpx.RetryClient{
		MaxAttempts: RetryAttempts,
		Jitter: func() time.Duration {
			jitterCalls++
			return 50 * time.Millisecond
		},
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests++
			return newResponse(429, `{"error":"temporarily_unavailable"}`, map[string]string{RequestIDHeader: "test-refresh-request"}), nil
		})},
		Sleeper: func(_ context.Context, d time.Duration) error {
			waits = append(waits, d)
			return nil
		},
	}
	client, err := NewConsoleOAuthClient(&ConsoleOAuthClientConfig{RetryClient: retry})
	if err != nil {
		t.Fatal(err)
	}
	provider := NewProvider(session, cache, func(string) (OAuthClient, error) { return client, nil }, func() time.Time { return now })
	for call := 1; call <= 2; call++ {
		if _, err := provider.Retrieve(context.Background()); err == nil {
			t.Fatal("expected refresh failure")
		}
		if requests != call*RetryAttempts {
			t.Fatalf("after Retrieve %d: requests = %d, want %d", call, requests, call*RetryAttempts)
		}
	}
	wantWaits := []time.Duration{250 * time.Millisecond, 450 * time.Millisecond, 250 * time.Millisecond, 450 * time.Millisecond}
	if !reflect.DeepEqual(waits, wantWaits) {
		t.Fatalf("retry waits = %v, want %v", waits, wantWaits)
	}
	if jitterCalls != 4 {
		t.Fatalf("jitter calls = %d, want exactly one per retry wait", jitterCalls)
	}
	if cache.writeCnt != 0 || !bytes.Equal(cache.data[session], seed) {
		t.Fatal("failed refresh must not overwrite the token cache")
	}
}

func TestConsoleDeviceTokenPollingDoesNotAddHTTPJitter(t *testing.T) {
	var requests, jitterCalls, sleepCalls int
	client, err := NewConsoleOAuthClient(&ConsoleOAuthClientConfig{RetryClient: &httpx.RetryClient{
		MaxAttempts: RetryAttempts,
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			requests++
			return newResponse(429, `{"error":"slow_down"}`, nil), nil
		})},
		Jitter: func() time.Duration {
			jitterCalls++
			return 50 * time.Millisecond
		},
		Sleeper: func(context.Context, time.Duration) error {
			sleepCalls++
			return nil
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ExchangeTokenOnce(context.Background(), &ConsoleTokenRequest{
		GrantType: GrantTypeDeviceCode, ClientID: ClientIDCrossDevice, Scope: Scope, DeviceCode: "test-device-code",
	})
	if err == nil {
		t.Fatal("expected slow_down response")
	}
	if requests != 1 || jitterCalls != 0 || sleepCalls != 0 {
		t.Fatalf("polling added HTTP retries: requests=%d jitter=%d sleeps=%d", requests, jitterCalls, sleepCalls)
	}
}
