package console

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth/browser"
)

// DeviceSleeper waits for d or until ctx is done. It is injectable so device
// authorization tests never need to wait in real time.
type DeviceSleeper func(ctx context.Context, d time.Duration) error

// DeviceFlow completes a Console Device Authorization Grant and returns the
// resulting token response. Implementations must not persist or expose the
// ephemeral device/user codes beyond the prompt writer.
type DeviceFlow interface {
	Authorize(ctx context.Context) (*ConsoleTokenResponse, error)
}

// DeviceFlowFactory constructs the production or test device flow after the
// endpoint client has been selected. noBrowser is true for --no-browser and
// for the legacy --remote compatibility flag.
type DeviceFlowFactory func(client OAuthClient, prompt io.Writer, opener browser.Opener, noBrowser bool, clock func() time.Time, sleeper DeviceSleeper) DeviceFlow

// DeviceAuthorizationFlow implements RFC 8628 for Console Login. It does not
// acquire the login cache lock; LoginService starts the cache transaction only
// after a token's login_session has been established.
type DeviceAuthorizationFlow struct {
	client     deviceOAuthClient
	prompt     io.Writer
	opener     browser.Opener
	NoBrowser  bool
	clock      func() time.Time
	sleeper    DeviceSleeper
	maxRetries int
}

// deviceOAuthClient is the protocol subset needed by DeviceAuthorizationFlow.
// ExchangeTokenOnce is optional: the concrete ConsoleOAuthClient implements it
// to prevent transport retries from swallowing RFC 8628 backpressure signals.
type deviceOAuthClient interface {
	StartDeviceAuthorization(ctx context.Context, req *ConsoleDeviceAuthorizationRequest) (*ConsoleDeviceAuthorizationResponse, error)
	ExchangeToken(ctx context.Context, req *ConsoleTokenRequest) (*ConsoleTokenResponse, error)
}

type deviceTokenOnceClient interface {
	ExchangeTokenOnce(ctx context.Context, req *ConsoleTokenRequest) (*ConsoleTokenResponse, error)
}

// NewDeviceAuthorizationFlow creates an RFC 8628 Console device flow. A nil
// prompt discards progress; a nil opener simply leaves browser opening as a
// no-op. Nil clock/sleeper values use safe real-time defaults.
func NewDeviceAuthorizationFlow(client OAuthClient, prompt io.Writer, opener browser.Opener, clock func() time.Time, sleeper DeviceSleeper) *DeviceAuthorizationFlow {
	flow := &DeviceAuthorizationFlow{
		prompt:     prompt,
		opener:     opener,
		clock:      clock,
		sleeper:    sleeper,
		maxRetries: deviceCodeMaxTransientErrors,
	}
	if c, ok := client.(deviceOAuthClient); ok && !isNilInterface(c) {
		flow.client = c
	}
	if flow.prompt == nil || isNilInterface(flow.prompt) {
		flow.prompt = io.Discard
	}
	if flow.clock == nil {
		flow.clock = time.Now
	}
	if flow.sleeper == nil {
		flow.sleeper = sleepDeviceAuthorization
	}
	return flow
}

const (
	deviceCodeDefaultInterval    = 5 * time.Second
	deviceCodeSlowDownIncrement  = 5 * time.Second
	deviceCodeMaxPollInterval    = 30 * time.Second
	deviceCodeMaxTransientErrors = 5
)

type devicePollState struct {
	interval        time.Duration
	transientErrors int
}

func newDevicePollState(seconds int) devicePollState {
	interval := time.Duration(seconds) * time.Second
	if interval <= 0 {
		interval = deviceCodeDefaultInterval
	}
	return devicePollState{interval: interval}
}

func (p *devicePollState) nextTimeoutInterval() {
	if p.interval <= 0 {
		p.interval = deviceCodeDefaultInterval
	}
	next := p.interval * 2
	if next <= p.interval || next > deviceCodeMaxPollInterval {
		next = deviceCodeMaxPollInterval
	}
	p.interval = next
}

func (p *devicePollState) slowDown() {
	p.interval += deviceCodeSlowDownIncrement
}

func (p *devicePollState) noteTransient() bool {
	p.transientErrors++
	return p.transientErrors <= deviceCodeMaxTransientErrors
}

func (p *devicePollState) resetTransient() { p.transientErrors = 0 }

// Authorize starts device authorization, renders the verification details to
// the injected prompt, optionally opens the browser, and polls until success or
// the server-provided expires_in deadline.
func (f *DeviceAuthorizationFlow) Authorize(ctx context.Context) (*ConsoleTokenResponse, error) {
	if f == nil {
		return nil, errors.New("nil *DeviceAuthorizationFlow")
	}
	if isNilInterface(ctx) {
		return nil, errors.New("nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, newSafeError("device authorization canceled", err)
	}
	if isNilInterface(f.client) {
		return nil, errors.New("device authorization client is unavailable")
	}
	prompt := f.prompt
	if prompt == nil || isNilInterface(prompt) {
		prompt = io.Discard
	}
	clock := f.clock
	if clock == nil {
		clock = time.Now
	}
	sleeper := f.sleeper
	if sleeper == nil {
		sleeper = sleepDeviceAuthorization
	}

	authResp, err := f.client.StartDeviceAuthorization(ctx, &ConsoleDeviceAuthorizationRequest{
		ClientID:   ClientIDCrossDevice,
		Scope:      Scope,
		DeviceInfo: DeviceInfo,
	})
	if err != nil {
		return nil, newSafeError("start device authorization failed", err)
	}
	if authResp == nil {
		return nil, errors.New("device authorization returned empty response")
	}
	if authResp.ExpiresIn <= 0 {
		return nil, errors.New("device authorization returned invalid expires_in")
	}
	lifetime, ok := deviceDuration(authResp.ExpiresIn)
	if !ok {
		return nil, errors.New("device authorization expires_in is too large")
	}

	browserURL := strings.TrimSpace(authResp.VerificationURIComplete)
	if browserURL == "" {
		browserURL = strings.TrimSpace(authResp.VerificationURI)
	}
	f.printAuthorizationPrompt(prompt, authResp)
	if !f.NoBrowser && !isNilInterface(f.opener) {
		// Browser opening is explicitly best effort. The URL was already
		// printed, and the underlying error may contain platform details or the
		// URL, so only a fixed message is rendered to the prompt.
		if err := f.opener.Open(ctx, browserURL); err != nil {
			fmt.Fprintln(prompt, "Unable to open the browser automatically; use the URL above.")
		}
	}

	deadline := clock().Add(lifetime)
	poll := newDevicePollState(authResp.Interval)
	maxTransient := f.maxRetries
	if maxTransient <= 0 {
		maxTransient = deviceCodeMaxTransientErrors
	}
	for {
		now := clock()
		if !now.Before(deadline) {
			return nil, errors.New("device authorization timed out; please run 'volclog login' again")
		}
		wait := poll.interval
		if remaining := deadline.Sub(now); wait > remaining {
			wait = remaining
		}
		if err := sleeper(ctx, wait); err != nil {
			return nil, newSafeError("waiting for device authorization failed", err)
		}
		if !clock().Before(deadline) {
			return nil, errors.New("device authorization timed out; please run 'volclog login' again")
		}

		tokenResp, pollErr := f.exchangeDeviceToken(ctx, &ConsoleTokenRequest{
			GrantType:  GrantTypeDeviceCode,
			DeviceCode: authResp.DeviceCode,
			ClientID:   ClientIDCrossDevice,
			Scope:      Scope,
		})
		if pollErr == nil {
			if tokenResp == nil {
				return nil, errors.New("device authorization returned empty token response")
			}
			return tokenResp, nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, newSafeError("device authorization canceled", ctxErr)
		}

		handled, terminalErr := handleDevicePollError(&poll, pollErr, maxTransient)
		if terminalErr != nil {
			return nil, terminalErr
		}
		if !handled {
			return nil, newSafeError("polling device authorization failed", pollErr)
		}
	}
}

func (f *DeviceAuthorizationFlow) exchangeDeviceToken(ctx context.Context, req *ConsoleTokenRequest) (*ConsoleTokenResponse, error) {
	if once, ok := f.client.(deviceTokenOnceClient); ok && !isNilInterface(once) {
		return once.ExchangeTokenOnce(ctx, req)
	}
	// Test/custom clients predating ExchangeTokenOnce remain usable. Production
	// *ConsoleOAuthClient always takes the single-attempt branch above.
	return f.client.ExchangeToken(ctx, req)
}

func (f *DeviceAuthorizationFlow) printAuthorizationPrompt(w io.Writer, resp *ConsoleDeviceAuthorizationResponse) {
	if resp == nil {
		return
	}
	fmt.Fprintln(w, "Open the following URL to authorize this device:")
	fmt.Fprintln(w, resp.VerificationURI)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Then enter the code: %s\n", resp.UserCode)
	if complete := strings.TrimSpace(resp.VerificationURIComplete); complete != "" && complete != strings.TrimSpace(resp.VerificationURI) {
		fmt.Fprintln(w, "Alternatively, open:")
		fmt.Fprintln(w, complete)
	}
	fmt.Fprintf(w, "This device code expires in %d seconds.\n", resp.ExpiresIn)
}

func handleDevicePollError(poll *devicePollState, err error, maxTransient int) (handled bool, terminal error) {
	if err == nil {
		return true, nil
	}
	if errors.Is(err, context.Canceled) {
		return false, newSafeError("device authorization canceled", err)
	}
	var apiErr *ConsoleOAuthAPIError
	if errors.As(err, &apiErr) {
		switch apiErr.Response.Error {
		case "authorization_pending":
			poll.resetTransient()
			return true, nil
		case "slow_down":
			poll.resetTransient()
			poll.slowDown()
			return true, nil
		case "access_denied":
			return false, errors.New("device authorization was denied")
		case "expired_token", "invalid_device_code":
			return false, errors.New("device code is invalid or expired; please run 'volclog login' again")
		case "server_error", "temporarily_unavailable":
			if !poll.noteTransient() || poll.transientErrors > maxTransient {
				return false, newSafeError("polling device authorization failed", err)
			}
			return true, nil
		}
		if apiErr.StatusCode == httpStatusRequestTimeout {
			poll.nextTimeoutInterval()
		}
		if !apiErr.IsRetryable() {
			return false, newSafeError("polling device authorization failed", err)
		}
		if !poll.noteTransient() || poll.transientErrors > maxTransient {
			return false, newSafeError("polling device authorization failed", err)
		}
		return true, nil
	}
	if isDevicePollTimeout(err) {
		poll.nextTimeoutInterval()
	}
	if !isDevicePollTransient(err) {
		return false, newSafeError("polling device authorization failed", err)
	}
	if !poll.noteTransient() || poll.transientErrors > maxTransient {
		return false, newSafeError("polling device authorization failed", err)
	}
	return true, nil
}

// Local constants avoid importing net/http solely for one comparison in this
// small state machine; client errors retain the actual status in their typed
// ConsoleOAuthAPIError value.
const httpStatusRequestTimeout = 408

func isDevicePollTransient(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

func isDevicePollTimeout(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func deviceDuration(seconds int) (time.Duration, bool) {
	if seconds <= 0 || int64(seconds) > int64(^uint64(0)>>1)/int64(time.Second) {
		return 0, false
	}
	return time.Duration(seconds) * time.Second, true
}

func sleepDeviceAuthorization(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
