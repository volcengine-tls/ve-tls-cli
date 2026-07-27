// Package console implements the Volcengine Console Login OAuth 2.0 protocol
// client, token parsing helpers, and the local callback server used to receive
// the authorization code from the browser.
//
// The package is intentionally self-contained: it only depends on the Go
// standard library plus the shared internal/auth/httpx retry client and the
// internal/auth/oauth PKCE primitives. It never reads environment variables,
// never writes to os.Stdout/os.Stderr directly, and never stores raw response
// bodies in returned errors.
package console

import (
	"encoding/json"
	"strconv"
	"time"
)

// Frozen protocol constants for the Volcengine Console Login flow. These match
// the upstream volcengine-cli behavior and must not be changed without an
// explicit protocol revision.
const (
	// DefaultEndpoint is the default Volcengine console sign-in endpoint.
	DefaultEndpoint = "https://signin.volcengine.com"

	// AuthorizePath is the path appended to the endpoint for the authorization URL.
	AuthorizePath = "/authorize/oauth/authorize"

	// TokenPath is the path appended to the endpoint for the token URL.
	TokenPath = "/authorize/oauth/token"

	// ClientIDSameDevice is the public client ID for local/same-device login mode.
	ClientIDSameDevice = "trn:signin:::devtools/same-device"

	// ClientIDCrossDevice is the public client ID for remote/cross-device login mode.
	ClientIDCrossDevice = "trn:signin:::devtools/cross-device"

	// Scope is the OAuth scope requested for Console Login.
	Scope = "Console:All:All"

	// CodeChallengeMethodS256 is the PKCE code challenge method used by Console Login.
	CodeChallengeMethodS256 = "S256"

	// CallbackPath is the path served by the local callback server.
	CallbackPath = "/oauth/callback"

	// CallbackTimeout is the maximum time the local callback server waits for
	// the browser to deliver the authorization code.
	CallbackTimeout = 10 * time.Minute

	// TokenTimeout is the HTTP timeout for console token exchange requests.
	TokenTimeout = 30 * time.Second

	// RetryAttempts is the number of retry attempts for token exchange.
	RetryAttempts = 3

	// RefreshBuffer is the safety window subtracted from the token expiry when
	// deciding whether a cached token must be refreshed before use.
	RefreshBuffer = 60 * time.Second
)

// Grant types supported by the Console OAuth token endpoint.
const (
	GrantTypeAuthorizationCode = "authorization_code"
	GrantTypeRefreshToken      = "refresh_token"
)

// RequestIDHeader is the response header carrying the Volcengine request ID
// (X-Tt-Logid). It is surfaced in structured errors for troubleshooting but
// never stored alongside raw response bodies.
const RequestIDHeader = "X-Tt-Logid"

// LoginTokenCache is the on-disk representation of a Console Login token cache.
// The JSON schema is byte-for-byte compatible with the upstream volcengine-cli
// cache so that caches written by either tool can be read by the other.
type LoginTokenCache struct {
	// LoginSession is the stable session identifier extracted from the ID token.
	LoginSession string `json:"login_session"`
	// AccessToken holds the STS credentials. It is stored as raw JSON because
	// the upstream token endpoint may return either a JSON object or a JSON
	// string containing the inner object.
	AccessToken json.RawMessage `json:"access_token"`
	// RefreshToken is the OAuth refresh token, if issued.
	RefreshToken string `json:"refresh_token,omitempty"`
	// IDToken is the OIDC ID token (a JWT) used to derive the login session.
	IDToken string `json:"id_token,omitempty"`
	// Scope is the OAuth scope granted by the server.
	Scope string `json:"scope"`
	// ClientID is the OAuth client ID used to obtain this token.
	ClientID string `json:"client_id"`
	// EndpointURL is the token endpoint the cache was obtained from. It is
	// optional so that older caches without the field remain readable.
	EndpointURL string `json:"endpoint_url,omitempty"`
	// IssuedAt is the RFC3339 timestamp at which the token was issued.
	IssuedAt string `json:"issued_at"`
	// ExpiresIn is the token lifetime in seconds.
	ExpiresIn int `json:"expires_in"`
	// TokenType is the OAuth token type (e.g. "sts").
	TokenType string `json:"token_type"`
}

// STSCredentials represents the parsed STS temporary credentials extracted from
// the access_token field of a token response.
type STSCredentials struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	SessionToken    string `json:"session_token"`
}

// ConsoleTokenResponse represents the raw token response from the console OAuth
// token endpoint.
type ConsoleTokenResponse struct {
	// AccessToken is the access token. For Console Login this may be either a
	// JSON object or a JSON string containing the inner STS credential object.
	// It is stored as raw JSON so both encodings are preserved verbatim.
	AccessToken json.RawMessage `json:"access_token"`
	// TokenType is the OAuth token type.
	TokenType string `json:"token_type"`
	// ExpiresIn is the token lifetime in seconds.
	ExpiresIn int `json:"expires_in"`
	// RefreshToken is the refresh token, if issued.
	RefreshToken string `json:"refresh_token"`
	// Scope is the granted scope.
	Scope string `json:"scope"`
	// IDToken is the OIDC ID token (a JWT).
	IDToken string `json:"id_token"`
}

// AuthorizeParams holds the parameters needed to build an authorization URL.
type AuthorizeParams struct {
	ClientID            string
	RedirectURI         string
	Scope               string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string
}

// ConsoleTokenRequest represents the token exchange request for console OAuth.
type ConsoleTokenRequest struct {
	GrantType    string
	Code         string
	RedirectURI  string
	ClientID     string
	Scope        string
	CodeVerifier string
	RefreshToken string
}

// ConsoleOAuthErrorResponse represents the structured error response body from
// the console sign-in OAuth endpoints.
type ConsoleOAuthErrorResponse struct {
	State            string `json:"state,omitempty"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
	ErrorURI         string `json:"error_uri,omitempty"`
}

// ConsoleOAuthAPIError wraps a non-2xx response from the console OAuth
// endpoints. It intentionally does not store the raw response body; only the
// status code, the parsed OAuth error fields, and the request ID are retained
// so that secrets cannot leak through error strings.
type ConsoleOAuthAPIError struct {
	StatusCode int
	Response   ConsoleOAuthErrorResponse
	RequestID  string
}

// allowedOAuthErrorCodes is the fixed allowlist of standard OAuth 2.0 error
// codes that may be rendered in Error(). Any other value (including a server
// mirroring a token or injecting newlines) is replaced with a generic label so
// secrets and control characters can never leak through the error string. The
// raw value is always preserved in the structured Response.Error field for
// programmatic inspection via errors.As.
var allowedOAuthErrorCodes = map[string]struct{}{
	"invalid_request":           {},
	"invalid_client":            {},
	"invalid_grant":             {},
	"unauthorized_client":       {},
	"unsupported_grant_type":    {},
	"access_denied":             {},
	"unsupported_response_type": {},
	"invalid_scope":             {},
	"server_error":              {},
	"temporarily_unavailable":   {},
}

// safeRequestID reports whether id is safe to render in an error string: it
// must be at most 128 bytes, contain no CR/LF, and consist solely of a
// conservative diagnostic character set (alphanumerics and a small set of
// punctuation commonly used in request IDs). The raw value is always preserved
// in the RequestID field regardless of this check.
func safeRequestID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '-' || c == '_' || c == '.' || c == '/' || c == ':':
		default:
			return false
		}
	}
	return true
}

// Error renders a safe, human-readable description of the OAuth failure. It
// exposes only an allowlisted OAuth error code (or a generic label otherwise),
// the HTTP status, and a request ID that passes a strict safety check. It never
// includes the raw response body, the untrusted error_description, or any
// secret material (access/refresh tokens, authorization code, PKCE verifier,
// secret access key, or session token) that a malicious server might mirror in
// error_description. The parsed Error, ErrorDescription, and RequestID remain
// available on the structured fields for explicit programmatic inspection.
func (e *ConsoleOAuthAPIError) Error() string {
	if e == nil {
		return ""
	}
	msg := "unknown error"
	if _, ok := allowedOAuthErrorCodes[e.Response.Error]; ok {
		msg = e.Response.Error
	}
	suffix := "[status " + strconv.Itoa(e.StatusCode)
	if safeRequestID(e.RequestID) {
		suffix += ", requestId: " + e.RequestID
	}
	suffix += "]"
	return "console oauth request failed: " + msg + " " + suffix
}

// IsRetryable reports whether the error is transient and the request should be
// retried. 429 (Too Many Requests), 408 (Request Timeout), and any 5xx are
// considered retryable; all other status codes (including 400 invalid_grant)
// are terminal.
func (e *ConsoleOAuthAPIError) IsRetryable() bool {
	if e == nil {
		return false
	}
	return e.StatusCode == 429 || e.StatusCode == 408 || e.StatusCode/100 == 5
}
