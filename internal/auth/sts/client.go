package sts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth"
	"github.com/volcengine-tls/ve-tls-cli/internal/auth/httpx"

	"github.com/google/uuid"
	"github.com/volcengine/volc-sdk-golang/base"
)

const (
	// defaultSTSHost is the fixed production STS endpoint.
	defaultSTSHost = "sts.volcengineapi.com"
	// defaultSTSRegion is the signing region used when the caller supplies none.
	defaultSTSRegion = "cn-beijing"
	// stsService is the SignV4 service scope for STS.
	stsService = "sts"
	// stsVersion is the STS API version pinned to the upstream contract.
	stsVersion = "2018-01-01"
	// ramDurationSeconds is the AssumeRole DurationSeconds value.
	ramDurationSeconds = 3600
	// oidcDurationSeconds is the AssumeRoleWithOIDC DurationSeconds value
	// (upstream default 3600 + 60 second safety buffer).
	oidcDurationSeconds = 3660
	// oidcSessionName is the fixed RoleSessionName for OIDC, matching upstream.
	oidcSessionName = "volcengine-go-sdk-oidc-session"
	// maxAttempts is the total number of STS attempts (initial + 3 retries),
	// matching the upstream DefaultRetryerMaxNumRetries=3 plus the initial call.
	maxAttempts = 4
	// ramHardTTL is the local ceiling applied to an AssumeRole expiration.
	ramHardTTL = time.Hour
)

// stsThrottleCodes mirrors the throttle code list in
// volcengine-go-sdk/volcengine/credentials/sts_request.go.
var stsThrottleCodes = map[string]struct{}{
	"ProvisionedThroughputExceededException": {},
	"Throttling":                             {},
	"ThrottlingException":                    {},
	"RequestLimitExceeded":                   {},
	"RequestThrottled":                       {},
	"RequestThrottledException":              {},
	"TooManyRequestsException":               {},
	"PriorRequestNotComplete":                {},
	"TransactionInProgressException":         {},
}

// stsTimeoutCodes mirrors the retryable timeout code list in
// volcengine-go-sdk/volcengine/credentials/sts_request.go.
var stsTimeoutCodes = map[string]struct{}{
	"RequestError":            {},
	"RequestTimeout":          {},
	"ResponseTimeout":         {},
	"RequestTimeoutException": {},
}

// stsEnvelope extracts only the structured fields we need from an STS response.
// It never stores the raw body.
type stsEnvelope struct {
	ResponseMetadata struct {
		RequestId string `json:"RequestId"`
		Error     *struct {
			Code    string `json:"Code"`
			Message string `json:"Message"`
		} `json:"Error"`
	} `json:"ResponseMetadata"`
	Result struct {
		Credentials struct {
			AccessKeyId     string `json:"AccessKeyId"`
			SecretAccessKey string `json:"SecretAccessKey"`
			SessionToken    string `json:"SessionToken"`
			ExpiredTime     string `json:"ExpiredTime"`
			Expiration      string `json:"Expiration"`
		} `json:"Credentials"`
	} `json:"Result"`
}

// Client is the standalone STS protocol client. Production code must use
// NewClient; tests construct a Client literal to inject private seams
// (endpoint, httpClient, signer, clock, uuid, sleeper, timeouts).
type Client struct {
	endpoint    string
	httpClient  *http.Client
	signer      func(creds base.Credentials, req *http.Request) *http.Request
	clock       func() time.Time
	uuid        func() string
	sleeper     func(ctx context.Context, d time.Duration) error
	ramTimeout  time.Duration
	oidcTimeout time.Duration
}

// NewClient returns a production STS client pinned to sts.volcengineapi.com
// over HTTPS. The host cannot be overridden by callers. Per-request timeouts
// are bounded: 5s for AssumeRole, 10s for AssumeRoleWithOIDC. The client never
// follows HTTP redirects: a 307/308 (or any redirect) is treated as a terminal
// non-2xx protocol error so source credentials and OIDC tokens can never be
// replayed to an attacker-controlled Location.
func NewClient() *Client {
	return &Client{
		endpoint:    defaultSTSHost,
		httpClient:  noRedirectHTTPClient(),
		signer:      func(creds base.Credentials, req *http.Request) *http.Request { return creds.Sign(req) },
		clock:       time.Now,
		uuid:        func() string { return uuid.New().String() },
		sleeper:     defaultSleeper,
		ramTimeout:  5 * time.Second,
		oidcTimeout: 10 * time.Second,
	}
}

// noRedirectHTTPClient returns an *http.Client that shares the default
// transport (and thus proxy/default transport semantics) but refuses to follow
// any redirect by returning http.ErrUseLastResponse from CheckRedirect.
func noRedirectHTTPClient() *http.Client {
	c := *http.DefaultClient
	c.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &c
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

// AssumeRole performs a signed AssumeRole GET and returns validated temporary
// credentials. The hard ExpiresAt is the earlier of request start + 1h and the
// server-returned ExpiredTime.
func (c *Client) AssumeRole(ctx context.Context, in AssumeRoleInput) (Credentials, error) {
	if ctx == nil {
		return Credentials{}, &auth.Error{Kind: auth.ConfigInvalid, Description: "nil context"}
	}
	if strings.TrimSpace(in.Source.AccessKeyID) == "" || strings.TrimSpace(in.Source.SecretAccessKey) == "" {
		return Credentials{}, &auth.Error{Kind: auth.ConfigInvalid, Description: "source access key id and secret access key must both be non-empty"}
	}
	if strings.TrimSpace(in.RoleName) == "" {
		return Credentials{}, &auth.Error{Kind: auth.ConfigInvalid, Description: "role name must be non-empty"}
	}
	if strings.TrimSpace(in.AccountID) == "" {
		return Credentials{}, &auth.Error{Kind: auth.ConfigInvalid, Description: "account id must be non-empty"}
	}

	requestStarted := c.clock()
	region := strings.TrimSpace(in.Region)
	if region == "" {
		region = defaultSTSRegion
	}
	roleTrn := fmt.Sprintf("trn:iam::%s:role/%s", in.AccountID, in.RoleName)
	signingCreds := base.Credentials{
		AccessKeyID:     in.Source.AccessKeyID,
		SecretAccessKey: in.Source.SecretAccessKey,
		SessionToken:    in.Source.SessionToken,
		Service:         stsService,
		Region:          region,
	}
	// RoleSessionName is generated once per logical AssumeRole call and reused
	// across all retry attempts, matching the upstream contract.
	roleSessionName := c.uuid()

	factory := func(ctx context.Context) (*http.Request, error) {
		q := url.Values{}
		q.Set("Action", "AssumeRole")
		q.Set("Version", stsVersion)
		q.Set("DurationSeconds", fmt.Sprintf("%d", ramDurationSeconds))
		q.Set("RoleTrn", roleTrn)
		q.Set("RoleSessionName", roleSessionName)

		scheme := "https"
		if in.DisableSSL {
			scheme = "http"
		}
		rawURL := scheme + "://" + c.endpoint + "/?" + q.Encode()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, err
		}
		// Set Host before signing so the canonical host in the Authorization
		// header matches the request target.
		req.Host = req.URL.Host
		signed := c.signer(signingCreds, req)
		if signed == nil {
			return nil, &auth.Error{Kind: auth.ProtocolError, Description: "failed to sign STS request"}
		}
		return signed, nil
	}

	body, err := c.doWithRetry(ctx, factory, c.ramTimeout)
	if err != nil {
		return Credentials{}, err
	}

	var env stsEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return Credentials{}, &auth.Error{Kind: auth.ProtocolError, Description: "failed to decode STS response"}
	}
	if env.ResponseMetadata.Error != nil {
		return Credentials{}, &auth.Error{
			Kind:        auth.ProtocolError,
			Status:      http.StatusOK,
			RequestID:   env.ResponseMetadata.RequestId,
			ServiceCode: env.ResponseMetadata.Error.Code,
			Description: "STS service returned an error",
		}
	}

	creds := Credentials{
		AccessKeyID:     env.Result.Credentials.AccessKeyId,
		SecretAccessKey: env.Result.Credentials.SecretAccessKey,
		SessionToken:    env.Result.Credentials.SessionToken,
	}
	if err := validateCredentialFields(creds); err != nil {
		return Credentials{}, err
	}

	expired, err := parseExpiration(env.Result.Credentials.ExpiredTime)
	if err != nil {
		return Credentials{}, &auth.Error{Kind: auth.ProtocolError, Description: "invalid or missing credential expiration"}
	}

	// Hard TTL is anchored to the first request start, not the validation time.
	hardLimit := requestStarted.Add(ramHardTTL)
	if expired.Before(hardLimit) {
		creds.ExpiresAt = expired
	} else {
		creds.ExpiresAt = hardLimit
	}

	// Re-validate against the clock at response-parse time. A credential that
	// expired during the request/response round-trip must fail closed.
	validationNow := c.clock()
	if !creds.ExpiresAt.After(validationNow) {
		return Credentials{}, &auth.Error{Kind: auth.ProtocolError, Description: "credential is already expired"}
	}
	return creds, nil
}

// AssumeRoleWithOIDC performs an unsigned form POST and returns validated
// temporary credentials. The hard ExpiresAt is the server-returned Expiration.
func (c *Client) AssumeRoleWithOIDC(ctx context.Context, in OIDCInput) (Credentials, error) {
	if ctx == nil {
		return Credentials{}, &auth.Error{Kind: auth.ConfigInvalid, Description: "nil context"}
	}
	if len(in.Token) == 0 {
		return Credentials{}, &auth.Error{Kind: auth.ConfigInvalid, Description: "OIDC token must be non-empty"}
	}
	if strings.TrimSpace(in.RoleTRN) == "" {
		return Credentials{}, &auth.Error{Kind: auth.ConfigInvalid, Description: "role TRN must be non-empty"}
	}

	requestStarted := c.clock()
	form := url.Values{}
	form.Set("RoleTrn", in.RoleTRN)
	form.Set("OIDCToken", string(in.Token))
	form.Set("RoleSessionName", oidcSessionName)
	form.Set("DurationSeconds", fmt.Sprintf("%d", oidcDurationSeconds))

	factory := func(ctx context.Context) (*http.Request, error) {
		scheme := "https"
		if in.DisableSSL {
			scheme = "http"
		}
		rawURL := scheme + "://" + c.endpoint + "/?Action=AssumeRoleWithOIDC&Version=" + stsVersion
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, strings.NewReader(form.Encode()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return req, nil
	}

	body, err := c.doWithRetry(ctx, factory, c.oidcTimeout)
	if err != nil {
		return Credentials{}, err
	}

	var env stsEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return Credentials{}, &auth.Error{Kind: auth.ProtocolError, Description: "failed to decode STS response"}
	}
	if env.ResponseMetadata.Error != nil {
		return Credentials{}, &auth.Error{
			Kind:        auth.ProtocolError,
			Status:      http.StatusOK,
			RequestID:   env.ResponseMetadata.RequestId,
			ServiceCode: env.ResponseMetadata.Error.Code,
			Description: "STS service returned an error",
		}
	}

	creds := Credentials{
		AccessKeyID:     env.Result.Credentials.AccessKeyId,
		SecretAccessKey: env.Result.Credentials.SecretAccessKey,
		SessionToken:    env.Result.Credentials.SessionToken,
	}
	if err := validateCredentialFields(creds); err != nil {
		return Credentials{}, err
	}

	// OIDC uses only the Expiration field; ExpiredTime is not accepted.
	expired, err := parseExpiration(env.Result.Credentials.Expiration)
	if err != nil {
		return Credentials{}, &auth.Error{Kind: auth.ProtocolError, Description: "invalid or missing credential expiration"}
	}
	if !expired.After(requestStarted) {
		return Credentials{}, &auth.Error{Kind: auth.ProtocolError, Description: "credential is already expired"}
	}

	// Re-validate against the clock at response-parse time. A credential that
	// expired during the request/response round-trip must fail closed.
	validationNow := c.clock()
	if !expired.After(validationNow) {
		return Credentials{}, &auth.Error{Kind: auth.ProtocolError, Description: "credential is already expired"}
	}
	creds.ExpiresAt = expired
	return creds, nil
}

// doWithRetry runs the STS retry loop. Each attempt uses httpx.RetryClient with
// MaxAttempts=1 so the two retry layers never multiply. A fresh per-attempt
// context with the given timeout is created for each rc.Do call and cancelled
// immediately after; the retry sleep uses the caller's ctx so the per-attempt
// timeout does not consume the sleep budget. It returns the response body on
// success or an auth.Error on terminal failure.
func (c *Client) doWithRetry(ctx context.Context, factory httpx.RequestFactory, timeout time.Duration) ([]byte, error) {
	rc := &httpx.RetryClient{
		HTTPClient:  c.httpClient,
		MaxAttempts: 1,
		Sleeper:     c.sleeper,
		MaxBodySize: httpx.DefaultMaxBodySize,
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// Create an independent per-attempt context. If the caller's ctx already
		// has a shorter deadline, WithTimeout picks the earlier one.
		attemptCtx := ctx
		var cancel context.CancelFunc
		if timeout > 0 {
			attemptCtx, cancel = context.WithTimeout(ctx, timeout)
		}

		resp, err := rc.Do(attemptCtx, factory)
		if err != nil {
			// rc.Do failed: cancel the attempt context immediately.
			if cancel != nil {
				cancel()
			}
			// Distinguish an attempt-level timeout (transient, retry) from a
			// caller-level cancellation/deadline (terminal, stop now).
			if isContextError(err) && ctx.Err() != nil {
				return nil, mapTransportError(err)
			}
			if attempt == maxAttempts {
				return nil, mapTransportError(err)
			}
			// Sleep uses the caller's ctx, not the per-attempt ctx.
			if sleepErr := c.sleep(ctx); sleepErr != nil {
				return nil, sleepErr
			}
			continue
		}

		// rc.Do succeeded: keep the attempt context alive through the full body
		// read and close, because real net/http ties the streaming body to the
		// request context. Cancelling earlier would interrupt the read.
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if cancel != nil {
			cancel()
		}
		if readErr != nil {
			// ErrBodyTooLarge or another read failure is terminal: never retry and
			// never include the body in the error.
			return nil, &auth.Error{
				Kind:        auth.ProtocolError,
				Status:      resp.StatusCode,
				Description: "failed to read STS response body",
			}
		}

		// Extract the service error code (if any) for retry classification.
		var env stsEnvelope
		var errCode string
		if jsonErr := json.Unmarshal(body, &env); jsonErr == nil && env.ResponseMetadata.Error != nil {
			errCode = env.ResponseMetadata.Error.Code
		}

		failed := resp.StatusCode < 200 || resp.StatusCode >= 300 || errCode != ""
		if !failed {
			return body, nil
		}

		if isRetryableSTSFailure(resp.StatusCode, errCode) && attempt < maxAttempts {
			if sleepErr := c.sleep(ctx); sleepErr != nil {
				return nil, sleepErr
			}
			continue
		}

		// Terminal failure.
		return nil, &auth.Error{
			Kind:        auth.ProtocolError,
			Status:      resp.StatusCode,
			RequestID:   env.ResponseMetadata.RequestId,
			ServiceCode: errCode,
			Description: describeSTSError(errCode, resp.StatusCode),
		}
	}
	return nil, &auth.Error{Kind: auth.ProtocolError, Description: "exhausted STS retry attempts"}
}

// sleep waits before the next retry attempt. It honors context cancellation.
func (c *Client) sleep(ctx context.Context) error {
	return c.sleeper(ctx, time.Second)
}

// isRetryableSTSFailure reports whether an STS failure should be retried,
// matching the upstream policy in sts_request.go.
func isRetryableSTSFailure(statusCode int, errCode string) bool {
	if statusCode == 0 {
		return true
	}
	switch statusCode {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	if errCode != "" {
		if _, ok := stsThrottleCodes[errCode]; ok {
			return true
		}
		if _, ok := stsTimeoutCodes[errCode]; ok {
			return true
		}
	}
	return false
}

// describeSTSError returns a fixed, stable description. The service error code
// is stored separately in ServiceCode and never embedded in Description.
func describeSTSError(errCode string, statusCode int) string {
	return "STS request failed"
}

// mapTransportError converts a transport-level error into an auth.Error. The
// cause is preserved for errors.Is/As but never rendered in the Error string,
// so query secrets cannot leak.
func mapTransportError(err error) error {
	return &auth.Error{
		Kind:        auth.ProtocolError,
		Status:      0,
		Description: "STS transport error",
		Cause:       err,
	}
}

// isContextError reports whether err is a context cancellation or deadline.
func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// validateCredentialFields rejects empty AK, SK, or SessionToken.
func validateCredentialFields(c Credentials) error {
	if strings.TrimSpace(c.AccessKeyID) == "" ||
		strings.TrimSpace(c.SecretAccessKey) == "" ||
		strings.TrimSpace(c.SessionToken) == "" {
		return &auth.Error{Kind: auth.ProtocolError, Description: "STS response missing required credential fields"}
	}
	return nil
}

// parseExpiration parses an RFC3339 timestamp.
func parseExpiration(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, errors.New("empty expiration")
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, err
	}
	return t, nil
}
