package console

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth/browser"
	"github.com/volcengine-tls/ve-tls-cli/internal/auth/httpx"
)

func TestStartDeviceAuthorizationPostsExactForm(t *testing.T) {
	var gotMethod, gotPath, gotContentType string
	var gotForm map[string]string
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		gotMethod = req.Method
		gotPath = req.URL.Path
		gotContentType = req.Header.Get("Content-Type")
		if err := req.ParseForm(); err != nil {
			return nil, err
		}
		gotForm = map[string]string{}
		for k, values := range req.PostForm {
			if len(values) == 1 {
				gotForm[k] = values[0]
			}
		}
		body, _ := json.Marshal(ConsoleDeviceAuthorizationResponse{
			DeviceCode:              "device-code",
			UserCode:                "USER-CODE",
			VerificationURI:         "https://example.com/device",
			VerificationURIComplete: "https://example.com/device?user_code=USER-CODE",
			ExpiresIn:               300,
			Interval:                5,
		})
		return newResponse(http.StatusOK, string(body), nil), nil
	})
	client, err := NewConsoleOAuthClient(&ConsoleOAuthClientConfig{
		RetryClient: &httpx.RetryClient{
			HTTPClient:  &http.Client{Transport: rt, Timeout: TokenTimeout},
			MaxAttempts: 1,
			Sleeper:     noopSleeper,
			Clock:       fixedClock(time.Now()),
		},
	})
	if err != nil {
		t.Fatalf("NewConsoleOAuthClient: %v", err)
	}

	resp, err := client.StartDeviceAuthorization(context.Background(), &ConsoleDeviceAuthorizationRequest{
		ClientID:   ClientIDCrossDevice,
		Scope:      Scope,
		DeviceInfo: DeviceInfo,
	})
	if err != nil {
		t.Fatalf("StartDeviceAuthorization: %v", err)
	}
	if resp.DeviceCode != "device-code" || resp.UserCode != "USER-CODE" {
		t.Fatalf("response = %+v", resp)
	}
	if gotMethod != http.MethodPost || gotPath != DeviceAuthorizationPath {
		t.Fatalf("request = %s %s, want POST %s", gotMethod, gotPath, DeviceAuthorizationPath)
	}
	if gotContentType != "application/x-www-form-urlencoded" {
		t.Fatalf("Content-Type = %q", gotContentType)
	}
	want := map[string]string{
		"client_id":   ClientIDCrossDevice,
		"scope":       Scope,
		"device_info": DeviceInfo,
	}
	if len(gotForm) != len(want) {
		t.Fatalf("form = %#v, want exactly %#v", gotForm, want)
	}
	for key, value := range want {
		if gotForm[key] != value {
			t.Fatalf("form[%q] = %q, want %q", key, gotForm[key], value)
		}
	}
	if _, ok := gotForm["client_secret"]; ok {
		t.Fatal("device authorization form must not include client_secret")
	}
}

func TestStartDeviceAuthorizationValidatesResponse(t *testing.T) {
	cases := []struct {
		name string
		resp ConsoleDeviceAuthorizationResponse
		want string
	}{
		{name: "missing device code", resp: ConsoleDeviceAuthorizationResponse{UserCode: "u", VerificationURI: "https://example.com", ExpiresIn: 300}, want: "device_code"},
		{name: "missing user code", resp: ConsoleDeviceAuthorizationResponse{DeviceCode: "d", VerificationURI: "https://example.com", ExpiresIn: 300}, want: "user_code"},
		{name: "missing verification uri", resp: ConsoleDeviceAuthorizationResponse{DeviceCode: "d", UserCode: "u", ExpiresIn: 300}, want: "verification_uri"},
		{name: "invalid expiration", resp: ConsoleDeviceAuthorizationResponse{DeviceCode: "d", UserCode: "u", VerificationURI: "https://example.com"}, want: "expires_in"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(tc.resp)
			rt := roundTripFunc(func(*http.Request) (*http.Response, error) {
				return newResponse(http.StatusOK, string(body), nil), nil
			})
			client, err := NewConsoleOAuthClient(&ConsoleOAuthClientConfig{
				RetryClient: &httpx.RetryClient{
					HTTPClient:  &http.Client{Transport: rt, Timeout: TokenTimeout},
					MaxAttempts: 1,
					Sleeper:     noopSleeper,
					Clock:       fixedClock(time.Now()),
				},
			})
			if err != nil {
				t.Fatalf("NewConsoleOAuthClient: %v", err)
			}
			_, err = client.StartDeviceAuthorization(context.Background(), &ConsoleDeviceAuthorizationRequest{ClientID: ClientIDCrossDevice, Scope: Scope})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

type scriptedDeviceClient struct {
	authResp  *ConsoleDeviceAuthorizationResponse
	poll      []error
	tokenResp *ConsoleTokenResponse
	index     int
	requests  []*ConsoleTokenRequest
}

func (c *scriptedDeviceClient) BuildAuthorizeURL(*AuthorizeParams) (string, error) {
	return "", errors.New("BuildAuthorizeURL must not be called by device flow")
}

func (c *scriptedDeviceClient) StartDeviceAuthorization(context.Context, *ConsoleDeviceAuthorizationRequest) (*ConsoleDeviceAuthorizationResponse, error) {
	return c.authResp, nil
}

func (c *scriptedDeviceClient) ExchangeToken(ctx context.Context, req *ConsoleTokenRequest) (*ConsoleTokenResponse, error) {
	return c.ExchangeTokenOnce(ctx, req)
}

func (c *scriptedDeviceClient) ExchangeTokenOnce(_ context.Context, req *ConsoleTokenRequest) (*ConsoleTokenResponse, error) {
	c.requests = append(c.requests, req)
	if c.index < len(c.poll) {
		err := c.poll[c.index]
		c.index++
		if err != nil {
			return nil, err
		}
	}
	return c.tokenResp, nil
}

func (c *scriptedDeviceClient) EndpointURL() string { return "https://signin.volcengine.com" }

type recordingBrowser struct {
	urls []string
	err  error
}

func (b *recordingBrowser) Open(_ context.Context, url string) error {
	b.urls = append(b.urls, url)
	return b.err
}

var _ browser.Opener = (*recordingBrowser)(nil)

func TestDeviceAuthorizationFlowPollsPendingSlowDownAndUsesCrossDevice(t *testing.T) {
	client := &scriptedDeviceClient{
		authResp: &ConsoleDeviceAuthorizationResponse{
			DeviceCode:              "device-code",
			UserCode:                "USER-CODE",
			VerificationURI:         "https://example.com/device",
			VerificationURIComplete: "https://example.com/device?user_code=USER-CODE",
			ExpiresIn:               300,
			Interval:                5,
		},
		poll: []error{
			&ConsoleOAuthAPIError{StatusCode: http.StatusBadRequest, Response: ConsoleOAuthErrorResponse{Error: "authorization_pending"}},
			&ConsoleOAuthAPIError{StatusCode: http.StatusBadRequest, Response: ConsoleOAuthErrorResponse{Error: "slow_down"}},
			nil,
		},
		tokenResp: validTokenResponse("trn:iam::1:user/device"),
	}
	var waits []time.Duration
	now := time.Unix(1000, 0)
	prompt := new(strings.Builder)
	flow := NewDeviceAuthorizationFlow(client, prompt, &recordingBrowser{}, func() time.Time { return now }, func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		now = now.Add(d)
		return nil
	})

	resp, err := flow.Authorize(context.Background())
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if resp != client.tokenResp {
		t.Fatal("Authorize returned unexpected token response")
	}
	if len(waits) != 3 || waits[0] != 5*time.Second || waits[1] != 5*time.Second || waits[2] != 10*time.Second {
		t.Fatalf("waits = %v, want [5s 5s 10s]", waits)
	}
	if len(client.requests) != 3 {
		t.Fatalf("poll requests = %d, want 3", len(client.requests))
	}
	for i, req := range client.requests {
		if req.GrantType != GrantTypeDeviceCode || req.ClientID != ClientIDCrossDevice || req.Scope != Scope || req.DeviceCode != "device-code" {
			t.Fatalf("request[%d] = %+v, want device grant/cross client/frozen scope/device code", i, req)
		}
		if req.Code != "" || req.CodeVerifier != "" || req.RedirectURI != "" || req.RefreshToken != "" {
			t.Fatalf("request[%d] contains authorization-code or refresh fields: %+v", i, req)
		}
	}
	output := prompt.String()
	for _, marker := range []string{"https://example.com/device", "USER-CODE", "300"} {
		if !strings.Contains(output, marker) {
			t.Fatalf("prompt %q does not contain %q", output, marker)
		}
	}
}

func TestDeviceAuthorizationFlowTimeoutDoublesIntervalAndCapsTransientErrors(t *testing.T) {
	client := &scriptedDeviceClient{
		authResp: &ConsoleDeviceAuthorizationResponse{
			DeviceCode:      "device-code",
			UserCode:        "USER-CODE",
			VerificationURI: "https://example.com/device",
			ExpiresIn:       300,
			Interval:        5,
		},
		poll:      []error{context.DeadlineExceeded, &ConsoleOAuthAPIError{StatusCode: http.StatusTooManyRequests}, nil},
		tokenResp: validTokenResponse("trn:iam::1:user/device"),
	}
	var waits []time.Duration
	now := time.Unix(1000, 0)
	flow := NewDeviceAuthorizationFlow(client, io.Discard, nil, func() time.Time { return now }, func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		now = now.Add(d)
		return nil
	})
	if _, err := flow.Authorize(context.Background()); err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if len(waits) != 3 || waits[0] != 5*time.Second || waits[1] != 10*time.Second || waits[2] != 10*time.Second {
		t.Fatalf("waits = %v, want [5s 10s 10s]", waits)
	}
}

func TestDeviceAuthorizationFlowTreatsProtocolServerErrorsAsBoundedTransient(t *testing.T) {
	for _, code := range []string{"server_error", "temporarily_unavailable"} {
		t.Run(code, func(t *testing.T) {
			pollErrors := make([]error, deviceCodeMaxTransientErrors)
			for i := range pollErrors {
				pollErrors[i] = &ConsoleOAuthAPIError{
					StatusCode: http.StatusBadRequest,
					Response:   ConsoleOAuthErrorResponse{Error: code},
				}
			}
			client := &scriptedDeviceClient{
				authResp: &ConsoleDeviceAuthorizationResponse{
					DeviceCode:      "device-code",
					UserCode:        "USER-CODE",
					VerificationURI: "https://example.com/device",
					ExpiresIn:       300,
				},
				poll:      pollErrors,
				tokenResp: validTokenResponse("trn:iam::1:user/device"),
			}
			now := time.Unix(1000, 0)
			flow := NewDeviceAuthorizationFlow(client, io.Discard, nil, func() time.Time { return now }, func(_ context.Context, d time.Duration) error {
				now = now.Add(d)
				return nil
			})
			if _, err := flow.Authorize(context.Background()); err != nil {
				t.Fatalf("Authorize: %v", err)
			}
			if len(client.requests) != deviceCodeMaxTransientErrors+1 {
				t.Fatalf("poll requests = %d, want %d", len(client.requests), deviceCodeMaxTransientErrors+1)
			}
		})
	}
}

func TestDevicePollStateOnlyCapsTimeoutBackoff(t *testing.T) {
	poll := newDevicePollState(60)
	if poll.interval != 60*time.Second {
		t.Fatalf("initial interval = %v, want 60s", poll.interval)
	}
	poll.slowDown()
	if poll.interval != 65*time.Second {
		t.Fatalf("slow_down interval = %v, want 65s", poll.interval)
	}
	poll.nextTimeoutInterval()
	if poll.interval != deviceCodeMaxPollInterval {
		t.Fatalf("timeout backoff interval = %v, want %v", poll.interval, deviceCodeMaxPollInterval)
	}
}

func TestDeviceAuthorizationFlowNoBrowserSkipsOpenerAndBrowserFailureIsBestEffort(t *testing.T) {
	newClient := func() *scriptedDeviceClient {
		return &scriptedDeviceClient{
			authResp: &ConsoleDeviceAuthorizationResponse{
				DeviceCode:              "device-code",
				UserCode:                "USER-CODE",
				VerificationURI:         "https://example.com/device",
				VerificationURIComplete: "https://example.com/device?user_code=USER-CODE",
				ExpiresIn:               300,
				Interval:                5,
			},
			tokenResp: validTokenResponse("trn:iam::1:user/device"),
		}
	}
	now := time.Unix(1000, 0)
	sleeper := func(_ context.Context, d time.Duration) error {
		now = now.Add(d)
		return nil
	}

	noBrowser := &recordingBrowser{}
	noBrowserFlow := NewDeviceAuthorizationFlow(newClient(), io.Discard, noBrowser, func() time.Time { return now }, sleeper)
	noBrowserFlow.NoBrowser = true
	if _, err := noBrowserFlow.Authorize(context.Background()); err != nil {
		t.Fatalf("no-browser Authorize: %v", err)
	}
	if len(noBrowser.urls) != 0 {
		t.Fatalf("browser calls with NoBrowser = %d, want 0", len(noBrowser.urls))
	}

	openFailure := &recordingBrowser{err: errors.New("browser unavailable")}
	browserFlow := NewDeviceAuthorizationFlow(newClient(), io.Discard, openFailure, func() time.Time { return now }, sleeper)
	if _, err := browserFlow.Authorize(context.Background()); err != nil {
		t.Fatalf("browser failure should be best effort: %v", err)
	}
	if len(openFailure.urls) != 1 || openFailure.urls[0] != "https://example.com/device?user_code=USER-CODE" {
		t.Fatalf("browser URLs = %v", openFailure.urls)
	}
}
