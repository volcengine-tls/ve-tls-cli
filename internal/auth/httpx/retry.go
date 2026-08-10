// Package httpx provides a small, injectable HTTP retry client tailored for
// authentication flows (OAuth token exchange, device authorization, portal
// calls). It intentionally avoids shared global randomness and never stores
// request or response bodies in returned errors.
package httpx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// DefaultMaxAttempts is the default number of attempts when RetryClient.MaxAttempts
// is not set.
const DefaultMaxAttempts = 3

// DefaultMaxBodySize is the default cap on response body bytes read into memory.
const DefaultMaxBodySize = 64 * 1024

// DefaultMaxRetryAfter is the default cap applied to a Retry-After delay when
// RetryClient.MaxRetryAfter is zero.
const DefaultMaxRetryAfter = time.Minute

// defaultBaseDelay and defaultMaxDelay bound the exponential backoff used when
// no valid Retry-After header is present.
const (
	defaultBaseDelay = 200 * time.Millisecond
	defaultMaxDelay  = 2 * time.Second
)

// ErrBodyTooLarge is returned when a response body exceeds the configured
// MaxBodySize. It never carries the offending body content.
var ErrBodyTooLarge = errors.New("response body exceeds size limit")

// RequestFactory builds a fresh, fully-formed HTTP request for each attempt.
// The factory must return an independent request on every call so that a
// consumed request body can be safely replayed across retries.
type RequestFactory func(ctx context.Context) (*http.Request, error)

// Sleeper blocks for d or until ctx is done. Tests inject a recording or no-op
// sleeper to avoid real delays.
type Sleeper func(ctx context.Context, d time.Duration) error

// Clock returns the current time. Tests inject a fixed clock to make
// Retry-After HTTP-date parsing deterministic.
type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// RetryClient executes HTTP requests with bounded retries suitable for
// authentication flows. All dependencies are injectable; the zero value is not
// usable, construct one explicitly or use NewRetryClient.
//
// Fields must not be modified while Do is running concurrently. Injected
// HTTPClient, Clock, and Sleeper must be safe for concurrent use by the caller.
type RetryClient struct {
	// HTTPClient performs the actual request. If nil, http.DefaultClient is used.
	HTTPClient *http.Client
	// MaxAttempts is the maximum number of attempts. Defaults to DefaultMaxAttempts.
	// A negative value is a configuration error.
	MaxAttempts int
	// Sleeper waits between attempts. If nil, a context-aware real sleeper is used.
	Sleeper Sleeper
	// Clock interprets Retry-After HTTP-date values. If nil, the system clock is used.
	Clock Clock
	// MaxBodySize caps response body bytes read into memory. Defaults to DefaultMaxBodySize.
	// A negative value is a configuration error.
	MaxBodySize int64
	// MaxRetryAfter caps the delay derived from a Retry-After header. If zero,
	// DefaultMaxRetryAfter is used. A negative value is a configuration error.
	MaxRetryAfter time.Duration
}

// NewRetryClient returns a RetryClient with default settings. The caller may
// override any field before use.
func NewRetryClient() *RetryClient {
	return &RetryClient{
		HTTPClient:    http.DefaultClient,
		MaxAttempts:   DefaultMaxAttempts,
		Sleeper:       defaultSleeper,
		Clock:         systemClock{},
		MaxBodySize:   DefaultMaxBodySize,
		MaxRetryAfter: DefaultMaxRetryAfter,
	}
}

func (c *RetryClient) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

func (c *RetryClient) maxAttempts() int {
	if c.MaxAttempts > 0 {
		return c.MaxAttempts
	}
	return DefaultMaxAttempts
}

func (c *RetryClient) sleeper() Sleeper {
	if c.Sleeper != nil {
		return c.Sleeper
	}
	return defaultSleeper
}

func (c *RetryClient) clock() Clock {
	if c.Clock == nil || isTypedNil(c.Clock) {
		return systemClock{}
	}
	return c.Clock
}

func (c *RetryClient) maxBodySize() int64 {
	if c.MaxBodySize > 0 {
		return c.MaxBodySize
	}
	return DefaultMaxBodySize
}

func (c *RetryClient) maxRetryAfter() time.Duration {
	if c.MaxRetryAfter > 0 {
		return c.MaxRetryAfter
	}
	return DefaultMaxRetryAfter
}

// validate reports configuration errors such as negative limits.
func (c *RetryClient) validate() error {
	if c.MaxAttempts < 0 {
		return errors.New("retry: MaxAttempts must not be negative")
	}
	if c.MaxBodySize < 0 {
		return errors.New("retry: MaxBodySize must not be negative")
	}
	if c.MaxRetryAfter < 0 {
		return errors.New("retry: MaxRetryAfter must not be negative")
	}
	return nil
}

// isTypedNil reports whether v is a non-nil interface holding a nil pointer,
// slice, map, chan, func, or interface value. Such values are not == nil but
// dereferencing them panics.
func isTypedNil(v interface{}) bool {
	if v == nil {
		return false
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func:
		return rv.IsNil()
	}
	return false
}

// Do executes the request produced by factory with retry semantics. On success
// it returns the response; the caller must close the response body. The
// returned body is bounded by MaxBodySize: reading beyond the limit returns
// ErrBodyTooLarge.
//
// Retryable status codes (408, 429, 5xx) trigger up to MaxAttempts attempts.
// Intermediate retryable responses have their bodies drained and closed so the
// underlying connection can be reused before sleeping. The final retryable
// response is returned to the caller intact, exactly like a non-retryable
// terminal response, so the caller can inspect the status, headers (e.g. a
// RequestID), and parse a structured error body. The caller is responsible for
// closing that body; Do does not sleep after the final attempt.
//
// On terminal transport-level failure it returns an error that never includes
// the raw response body or any request body, and with the URL query and
// fragment stripped so secrets cannot leak through error strings.
func (c *RetryClient) Do(ctx context.Context, factory RequestFactory) (*http.Response, error) {
	if c == nil {
		return nil, errors.New("retry: nil *RetryClient")
	}
	if ctx == nil {
		return nil, errors.New("retry: nil context")
	}
	if factory == nil {
		return nil, errors.New("retry: nil RequestFactory")
	}
	if err := c.validate(); err != nil {
		return nil, err
	}

	maxAttempts := c.maxAttempts()
	client := c.httpClient()
	sleeper := c.sleeper()
	clock := c.clock()
	maxBody := c.maxBodySize()
	maxRA := c.maxRetryAfter()

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		req, err := factory(ctx)
		if err != nil {
			// If the factory returned a request alongside the error, take
			// ownership of its body and close it so the caller does not have
			// to. This prevents resource leaks from partially-built requests.
			if req != nil && req.Body != nil {
				_ = req.Body.Close()
			}
			return nil, fmt.Errorf("build request: %w", err)
		}
		if req == nil {
			return nil, errors.New("retry: RequestFactory returned nil request without error")
		}

		resp, err := client.Do(req)
		if err != nil {
			// Network-level error. Retry unless it is a context error or we
			// have exhausted attempts.
			if !isRetryableNetError(err) || attempt == maxAttempts {
				return nil, sanitizeTransportError(err)
			}
			if err := sleep(ctx, sleeper, nil, attempt, clock, maxRA); err != nil {
				return nil, err
			}
			continue
		}

		// We have a response. Decide based on status code.
		if !isRetryableStatus(resp.StatusCode) {
			// Non-retryable: return to caller with a body-size-limited body.
			resp.Body = &limitedReadCloser{r: resp.Body, remaining: maxBody}
			return resp, nil
		}

		// Retryable status.
		if attempt == maxAttempts {
			// Final attempt: do not discard the response. Return it to the
			// caller so status, headers (e.g. RequestID), and a structured
			// error body remain available. The caller must close the body.
			resp.Body = &limitedReadCloser{r: resp.Body, remaining: maxBody}
			return resp, nil
		}

		// Intermediate attempt: drain the body (bounded) so the connection
		// can be reused, then close it before sleeping.
		drainAndClose(resp.Body, maxBody)

		if err := sleep(ctx, sleeper, resp, attempt, clock, maxRA); err != nil {
			return nil, err
		}
	}
	// Unreachable: the loop always returns on the final attempt.
	return nil, errors.New("retry: exhausted attempts without result")
}

// sanitizeTransportError returns an error whose Error() string never includes
// the raw query or fragment of any embedded *url.Error (which may carry
// secrets such as access_token or authorization_code). The original cause is
// preserved verbatim via Unwrap so errors.Is/As still reach every joined
// cause, context.Canceled, context.DeadlineExceeded, etc.
//
// If err contains no *url.Error it is returned unchanged.
func sanitizeTransportError(err error) error {
	if err == nil {
		return nil
	}
	var ue *url.Error
	if !errors.As(err, &ue) {
		return err
	}
	return &redactedTransportError{cause: err}
}

// redactedTransportError wraps a transport error so its Error() string only
// renders a safe description and the redacted scheme/host/path of the first
// embedded *url.Error. It never renders the original cause's Error() string,
// which may itself embed a secret-bearing query. The full cause is exposed via
// Unwrap so errors.Is/As traverse the original, unmodified chain.
type redactedTransportError struct {
	cause error
}

func (e *redactedTransportError) Error() string {
	var ue *url.Error
	if errors.As(e.cause, &ue) {
		redacted := redactURL(ue.URL)
		if ue.Op != "" {
			return fmt.Sprintf("%s %s: transport error", ue.Op, redacted)
		}
		return "transport error: " + redacted
	}
	return "transport error"
}

func (e *redactedTransportError) Unwrap() error {
	return e.cause
}

// redactURL strips the query and fragment from rawURL so secrets cannot leak
// through an error string. It first removes everything from the first '?' or
// '#' onward (lexical, no parsing), then re-parses to normalize
// scheme/host/path. If parsing fails it returns a fixed placeholder rather
// than the original string, so a malformed (e.g. illegal percent-encoded) URL
// can never leak its query or fragment.
func redactURL(rawURL string) string {
	if i := strings.IndexAny(rawURL, "?#"); i >= 0 {
		rawURL = rawURL[:i]
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "<redacted-url>"
	}
	u.RawQuery = ""
	u.Fragment = ""
	u.RawFragment = ""
	return u.String()
}

// isRetryableStatus reports whether the HTTP status code should trigger a
// retry: 408 (Request Timeout), 429 (Too Many Requests), and any 5xx.
func isRetryableStatus(code int) bool {
	return code == http.StatusRequestTimeout ||
		code == http.StatusTooManyRequests ||
		code/100 == 5
}

// isRetryableNetError reports whether a transport-level error is worth
// retrying. Context cancellation and deadline errors are never retried.
func isRetryableNetError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	// Most transient transport errors (connection reset, EOF, DNS failure,
	// TLS handshake failure) are retryable. We deliberately do not inspect
	// the error string.
	return true
}

// sleep waits before the next attempt. It honors a Retry-After header from the
// last response when present and valid; otherwise it uses a deterministic
// exponential backoff (no jitter) bounded by defaultMaxDelay. The resulting
// delay is clamped to maxRetryAfter so a malicious or buggy server cannot force
// an unbounded wait.
func sleep(ctx context.Context, sleeper Sleeper, resp *http.Response, attempt int, clock Clock, maxRetryAfter time.Duration) error {
	delay := backoff(attempt)
	if resp != nil {
		if ra, ok := parseRetryAfter(resp.Header.Get("Retry-After"), clock.Now()); ok {
			delay = ra
		}
	}
	if delay > maxRetryAfter {
		delay = maxRetryAfter
	}
	if delay <= 0 {
		return nil
	}
	return sleeper(ctx, delay)
}

// backoff returns a deterministic exponential delay for the given attempt
// number (1-based). No jitter is added to keep tests deterministic and avoid
// shared global random state.
func backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := defaultBaseDelay
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= defaultMaxDelay {
			return defaultMaxDelay
		}
	}
	if delay > defaultMaxDelay {
		return defaultMaxDelay
	}
	return delay
}

// maxRetryAfterSeconds is the largest delta-seconds value that can be converted
// to a time.Duration without overflow.
var maxRetryAfterSeconds = int64(time.Duration(math.MaxInt64) / time.Second)

// parseRetryAfter parses a Retry-After header value, which may be either
// delta-seconds or an HTTP-date. It returns the duration to wait and true if
// the value was valid, or zero and false otherwise.
//
// Delta-seconds per RFC 7231 MUST be 1*DIGIT (ASCII digits only). ParseUint
// strictly rejects '-' and '+' signs, internal whitespace, decimals, and
// negative values, so a negative overflow (e.g. "-9223372036854775809" or
// "-99999999999999999999") can never be mistaken for a valid delay. A positive
// overflow is clamped to the maximum representable Duration; it is then capped
// by MaxRetryAfter in the sleep path. The boundary is checked before
// multiplying by time.Second to avoid integer overflow.
func parseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	// Try delta-seconds first. ParseUint accepts only ASCII digit sequences.
	if secs, err := strconv.ParseUint(value, 10, 64); err == nil {
		// Boundary check before multiplying by time.Second to avoid overflow.
		if secs > uint64(maxRetryAfterSeconds) {
			return time.Duration(math.MaxInt64), true
		}
		return time.Duration(secs) * time.Second, true
	} else {
		var ne *strconv.NumError
		if errors.As(err, &ne) && ne.Err == strconv.ErrRange {
			// Positive overflow: clamp to the maximum representable Duration.
			return time.Duration(math.MaxInt64), true
		}
		// Any other parse failure (syntax, negative, sign, decimal, etc.) is
		// not a valid delta-seconds; fall through to HTTP-date parsing.
	}
	// Try HTTP-date. time.Time.Sub clamps the result to the representable
	// Duration range, so a far-future date yields the maximum Duration rather
	// than overflowing.
	if t, err := http.ParseTime(value); err == nil {
		d := t.Sub(now)
		if d < 0 {
			return 0, true
		}
		return d, true
	}
	return 0, false
}

// drainAndClose reads up to limit bytes from r then closes it. This allows the
// underlying TCP connection to be reused without buffering unbounded data.
func drainAndClose(r io.ReadCloser, limit int64) {
	if r == nil {
		return
	}
	_, _ = io.CopyN(io.Discard, r, limit)
	_ = r.Close()
}

// limitedReadCloser wraps a response body so that reads beyond the configured
// limit return ErrBodyTooLarge instead of growing memory without bound.
type limitedReadCloser struct {
	r         io.ReadCloser
	remaining int64
	exceeded  bool
}

func (l *limitedReadCloser) Read(p []byte) (int, error) {
	// A zero-length Read must not probe or consume the underlying stream.
	if len(p) == 0 {
		return 0, nil
	}
	if l.exceeded {
		return 0, ErrBodyTooLarge
	}
	if l.remaining <= 0 {
		// Probe for one more byte to detect overflow.
		one := make([]byte, 1)
		n, err := l.r.Read(one)
		if n > 0 {
			l.exceeded = true
			return 0, ErrBodyTooLarge
		}
		return 0, err
	}
	if int64(len(p)) > l.remaining {
		p = p[:l.remaining]
	}
	n, err := l.r.Read(p)
	l.remaining -= int64(n)
	return n, err
}

func (l *limitedReadCloser) Close() error {
	return l.r.Close()
}

// defaultSleeper blocks for d or until ctx is done.
func defaultSleeper(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
