package tlsapi

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth"
	"github.com/volcengine-tls/ve-tls-cli/internal/version"

	"github.com/volcengine/volc-sdk-golang/base"
)

// requestSigner signs an HTTP request using the supplied credentials. The
// production default delegates to base.Credentials.Sign; tests may inject a
// fixed-time implementation. It is package-private and not part of the public
// API.
type requestSigner func(creds base.Credentials, req *http.Request) *http.Request

func defaultRequestSigner(creds base.Credentials, req *http.Request) *http.Request {
	return creds.Sign(req)
}

// isNilProvider reports whether p is nil, including the typed-nil case where a
// non-nil interface wraps a nil pointer (e.g. var p *SomeProvider = nil). Such
// values must be rejected so Sign never calls Retrieve on a nil receiver. It
// covers every kind that reflect.Value.IsNil supports (Chan, Func, Interface,
// Map, Ptr, Slice); any other kind reports false so IsNil is never called on a
// non-nil-able value and panic.
func isNilProvider(p auth.Provider) bool {
	if p == nil {
		return true
	}
	v := reflect.ValueOf(p)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return v.IsNil()
	}
	return false
}

type Client struct {
	Endpoint string
	Region   string
	Service  string
	Timeout  time.Duration
	Creds    base.Credentials
	HTTP     *http.Client

	provider      auth.Provider
	requestSigner requestSigner
}

type Response struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

func New(endpoint, region, sdkProfile, ak, sk, token string, timeout time.Duration) (*Client, error) {
	ep := strings.TrimSpace(endpoint)
	if ep == "" {
		return nil, errors.New("empty endpoint")
	}
	if !strings.HasPrefix(ep, "http://") && !strings.HasPrefix(ep, "https://") {
		ep = "https://" + ep
	}
	_, err := url.Parse(ep)
	if err != nil {
		return nil, err
	}
	r := strings.TrimSpace(region)
	if r == "" {
		return nil, errors.New("empty region")
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	service := "TLS"
	_ = strings.TrimSpace(sdkProfile)
	creds, err := resolveSigningCredentials(r, service, ak, sk, token)
	if err != nil {
		return nil, err
	}
	c := &Client{
		Endpoint:      strings.TrimRight(ep, "/"),
		Region:        r,
		Service:       service,
		Timeout:       timeout,
		Creds:         creds,
		HTTP:          &http.Client{Timeout: timeout},
		requestSigner: defaultRequestSigner,
	}
	return c, nil
}

// NewWithProvider constructs a client that retrieves fresh credentials from the
// supplied provider before every signature. The static Creds field is left empty
// so dynamic signing never depends on or mutates shared credentials.
func NewWithProvider(endpoint, region string, provider auth.Provider, timeout time.Duration) (*Client, error) {
	ep := strings.TrimSpace(endpoint)
	if ep == "" {
		return nil, errors.New("empty endpoint")
	}
	if !strings.HasPrefix(ep, "http://") && !strings.HasPrefix(ep, "https://") {
		ep = "https://" + ep
	}
	if _, err := url.Parse(ep); err != nil {
		return nil, err
	}
	r := strings.TrimSpace(region)
	if r == "" {
		return nil, errors.New("empty region")
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	if isNilProvider(provider) {
		return nil, errors.New("nil provider")
	}
	c := &Client{
		Endpoint:      strings.TrimRight(ep, "/"),
		Region:        r,
		Service:       "TLS",
		Timeout:       timeout,
		HTTP:          &http.Client{Timeout: timeout},
		provider:      provider,
		requestSigner: defaultRequestSigner,
	}
	return c, nil
}

// Sign signs the request. Branch order matters:
//   - c.provider == nil: explicit legacy/static mode, sign with the current
//     c.Creds. The caller's headers (including X-Security-Token) are preserved.
//   - c.provider != nil but isNilProvider(c.provider): a typed-nil dynamic
//     provider. Fail closed with "nil provider"; never silently fall back to
//     c.Creds even if it holds valid dormant credentials.
//   - otherwise: dynamic mode, retrieve fresh credentials before each signature
//     and scope them to c.Region and the hardcoded Service="TLS".
func (c *Client) Sign(ctx context.Context, req *http.Request) (*http.Request, error) {
	if req == nil {
		return nil, errors.New("nil request")
	}
	if req.Header == nil {
		req.Header = make(http.Header)
	}

	var creds base.Credentials
	if c.provider == nil {
		// Static/legacy path: sign with the current public Creds field. The
		// scope (Region/Service) comes from c.Creds, not c.Region/c.Service, so
		// mutating c.Creds between requests changes subsequent signatures. The
		// caller's X-Security-Token header is intentionally left untouched to
		// match the old SDK behavior where c.Creds.Sign never stripped it.
		if err := validateCreds(c.Creds); err != nil {
			return nil, err
		}
		creds = c.Creds
	} else if isNilProvider(c.provider) {
		// Typed-nil dynamic provider: fail closed. Do not call Retrieve on a nil
		// receiver and do not fall back to static c.Creds.
		return nil, errors.New("nil provider")
	} else {
		// Dynamic path: retrieve fresh credentials from the provider on every call.
		v, err := c.provider.Retrieve(ctx)
		if err != nil {
			return nil, err
		}
		if err := v.Validate(); err != nil {
			return nil, err
		}
		creds = base.Credentials{
			AccessKeyID:     v.AccessKeyID,
			SecretAccessKey: v.SecretAccessKey,
			SessionToken:    v.SessionToken,
			Region:          c.Region,
			Service:         "TLS",
		}
		// Clear any stale X-Security-Token left by a previous signature so a switch
		// from token-bearing to token-less credentials never leaks the old token.
		// The signer will re-add it from the freshly retrieved SessionToken.
		req.Header.Del("X-Security-Token")
	}

	signer := c.requestSigner
	if signer == nil {
		signer = defaultRequestSigner
	}
	signed := signer(creds, req)
	if signed == nil {
		return nil, errors.New("signing failed: signer returned nil request")
	}
	if strings.TrimSpace(signed.Header.Get("Authorization")) == "" {
		return nil, errors.New("signing failed: missing Authorization header")
	}
	return signed, nil
}

// validateCreds reports whether the supplied static credentials have both an
// access key id and a secret access key.
func validateCreds(creds base.Credentials) error {
	if strings.TrimSpace(creds.AccessKeyID) == "" || strings.TrimSpace(creds.SecretAccessKey) == "" {
		return errors.New("missing access key id or secret access key")
	}
	return nil
}

func (c *Client) Do(ctx context.Context, method, path string, query map[string]string, header map[string]string, body []byte) (Response, error) {
	u, err := url.Parse(c.Endpoint + path)
	if err != nil {
		return Response{}, err
	}
	q := u.Query()
	for k, v := range query {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()

	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	} else {
		r = bytes.NewReader([]byte{})
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), r)
	if err != nil {
		return Response{}, err
	}
	req.Host = u.Host

	req.Header.Set("User-Agent", "volclog/"+version.Version)
	req.Header.Set("x-tls-apiversion", "0.3.0")
	req.Header.Set("Content-Type", "application/json")

	if len(body) > 0 {
		sum := md5.Sum(body)
		req.Header.Set("Content-Md5", strings.ToUpper(hex.EncodeToString(sum[:])))
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}

	req, err = c.Sign(ctx, req)
	if err != nil {
		return Response{}, err
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()
	var reader io.Reader = resp.Body
	if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		zr, err := gzip.NewReader(resp.Body)
		if err != nil {
			return Response{}, err
		}
		defer zr.Close()
		reader = zr
	}
	b, err := io.ReadAll(reader)
	if err != nil {
		return Response{}, err
	}
	return Response{StatusCode: resp.StatusCode, Header: resp.Header, Body: b}, nil
}

func resolveSigningCredentials(region string, service string, ak string, sk string, token string) (base.Credentials, error) {
	accessKeyID := strings.TrimSpace(ak)
	secretAccessKey := strings.TrimSpace(sk)
	sessionToken := strings.TrimSpace(token)
	if accessKeyID == "" || secretAccessKey == "" {
		envAK := strings.TrimSpace(os.Getenv("VOLCENGINE_ACCESS_KEY_ID"))
		envSK := strings.TrimSpace(os.Getenv("VOLCENGINE_ACCESS_KEY_SECRET"))
		envToken := strings.TrimSpace(os.Getenv("VOLCENGINE_TOKEN"))
		if accessKeyID == "" {
			accessKeyID = envAK
		}
		if secretAccessKey == "" {
			secretAccessKey = envSK
		}
		if sessionToken == "" {
			sessionToken = envToken
		}
	}
	if accessKeyID == "" || secretAccessKey == "" {
		return base.Credentials{}, errors.New("missing access key id or secret access key")
	}
	return base.Credentials{
		AccessKeyID:     accessKeyID,
		SecretAccessKey: secretAccessKey,
		SessionToken:    sessionToken,
		Region:          strings.TrimSpace(region),
		Service:         strings.TrimSpace(service),
	}, nil
}
