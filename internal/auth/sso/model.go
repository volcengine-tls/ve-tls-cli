// Package sso implements the Volcengine CloudIdentity SSO protocol clients:
// the OAuth 2.0 device authorization client (client registration, device
// authorization, token exchange, token revocation) and the Portal client
// (account/role listing and role credential exchange).
//
// The package is intentionally self-contained: it only depends on the Go
// standard library plus the shared internal/auth/httpx retry client. It never
// reads environment variables, never writes to os.Stdout/os.Stderr directly,
// and never stores raw response bodies in returned errors.
package sso

import (
	"strconv"
	"time"
)

// Frozen protocol constants for the CloudIdentity OAuth endpoints. These match
// the upstream volcengine-cli behavior and must not be changed without an
// explicit protocol revision.
const (
	// DefaultRegion is the default CloudIdentity region used to build the
	// OAuth and Portal base URLs when no region is supplied.
	DefaultRegion = "cn-beijing"

	// oauthBaseURLTemplate is the template for the OAuth service base URL.
	oauthBaseURLTemplate = "https://cloudidentity-oauth.%s.volces.com"

	// RegisterPath is the path appended to the base URL for client registration.
	RegisterPath = "/client/register"

	// DeviceAuthorizationPath is the path appended to the base URL for the
	// device authorization request.
	DeviceAuthorizationPath = "/device_authorization"

	// TokenPath is the path appended to the base URL for the token request.
	TokenPath = "/token"

	// RevokePath is the path appended to the base URL for the token revocation
	// request.
	RevokePath = "/revoke"

	// OAuthRequestTimeout is the default HTTP timeout for OAuth requests.
	OAuthRequestTimeout = 10 * time.Second

	// OAuthRetryAttempts is the default number of retry attempts for OAuth
	// requests. Client registration is not retried because it is not guaranteed
	// to be idempotent.
	OAuthRetryAttempts = 3
)

// Grant types supported by the CloudIdentity OAuth token endpoint.
const (
	// GrantTypeDeviceCode is the OAuth 2.0 device authorization grant type.
	GrantTypeDeviceCode = "urn:ietf:params:oauth:grant-type:device_code"

	// GrantTypeRefreshToken is the OAuth 2.0 refresh token grant type.
	GrantTypeRefreshToken = "refresh_token"
)

// Scopes requested during client registration and device authorization.
const (
	// ScopeAccountAccess is the scope granting access to the user's accounts.
	ScopeAccountAccess = "cloudidentity:account:access"

	// ScopeOfflineAccess is the scope requesting a refresh token.
	ScopeOfflineAccess = "offline_access"
)

// Frozen protocol constants for the CloudIdentity Portal endpoints.
const (
	// portalBaseURLTemplate is the template for the Portal service base URL.
	portalBaseURLTemplate = "https://cloudidentity-portal.%s.volces.com"

	// ListAccountsPath is the path appended to the base URL for listing accounts.
	ListAccountsPath = "/assignment/accounts"

	// ListRolesPath is the path appended to the base URL for listing roles.
	ListRolesPath = "/assignment/roles"

	// RoleCredentialsPath is the path appended to the base URL for exchanging
	// role credentials.
	RoleCredentialsPath = "/federation/credentials"

	// BearerTokenHeader is the request header carrying the Portal access token.
	BearerTokenHeader = "x-bd-cloudidentity-bearer-token"

	// DefaultPageSize is the default page size for paginated Portal requests.
	DefaultPageSize = 50

	// PortalRequestTimeout is the default HTTP timeout for Portal requests.
	PortalRequestTimeout = 30 * time.Second

	// PortalRetryAttempts is the default number of retry attempts for Portal
	// requests.
	PortalRetryAttempts = 3

	// maxPaginationPages bounds the number of pages a single ListAccounts or
	// ListAccountRoles call may fetch to defend against a server that never
	// terminates pagination (no progress or repeated pages).
	maxPaginationPages = 1000
)

// RequestIDHeader is the response header carrying the Volcengine request ID
// (X-Tt-Logid). It is surfaced in structured errors for troubleshooting but
// never stored alongside raw response bodies.
const RequestIDHeader = "X-Tt-Logid"

// RegisterClientRequest is the request body for the client registration
// endpoint.
type RegisterClientRequest struct {
	// ClientName is the human-readable name of the client being registered.
	ClientName string `json:"client_name"`
	// ClientType is the OAuth client type. Always "public" for volclog.
	ClientType string `json:"client_type"`
	// GrantTypes is the list of OAuth grant types the client will use.
	GrantTypes []string `json:"grant_types,omitempty"`
	// Scopes is the list of OAuth scopes the client will request.
	Scopes []string `json:"scopes,omitempty"`
}

// RegisterClientResponse is the response body for a successful client
// registration.
type RegisterClientResponse struct {
	// ClientID is the registered client identifier.
	ClientID string `json:"client_id"`
	// ClientSecret is the registered client secret.
	ClientSecret string `json:"client_secret"`
	// ClientIDIssuedAt is the Unix timestamp (seconds) at which the client ID
	// was issued.
	ClientIDIssuedAt int64 `json:"client_id_issued_at,omitempty"`
	// ClientSecretExpiresAt is the Unix timestamp (seconds) at which the client
	// secret expires. Zero means it does not expire.
	ClientSecretExpiresAt int64 `json:"client_secret_expires_at,omitempty"`
}

// CreateTokenRequest is the request body for the token endpoint.
type CreateTokenRequest struct {
	// GrantType is the OAuth grant type (device_code or refresh_token).
	GrantType string `json:"grant_type"`
	// ClientID is the registered client identifier.
	ClientID string `json:"client_id"`
	// ClientSecret is the registered client secret.
	ClientSecret string `json:"client_secret"`
	// RefreshToken is the refresh token. Required for the refresh_token grant.
	RefreshToken string `json:"refresh_token,omitempty"`
	// DeviceCode is the device code. Required for the device_code grant.
	DeviceCode string `json:"device_code,omitempty"`
}

// CreateTokenResponse is the response body for a successful token request.
type CreateTokenResponse struct {
	// AccessToken is the OAuth access token.
	AccessToken string `json:"access_token"`
	// TokenType is the OAuth token type (e.g. "Bearer").
	TokenType string `json:"token_type"`
	// RefreshToken is the refresh token, if issued.
	RefreshToken string `json:"refresh_token,omitempty"`
	// ExpiresIn is the token lifetime in seconds.
	ExpiresIn int `json:"expires_in"`
}

// RevokeTokenRequest is the request body for the token revocation endpoint.
type RevokeTokenRequest struct {
	// ClientID is the registered client identifier.
	ClientID string `json:"client_id"`
	// ClientSecret is the registered client secret.
	ClientSecret string `json:"client_secret"`
	// Token is the access or refresh token to revoke.
	Token string `json:"token"`
}

// StartDeviceAuthorizationRequest is the request body for the device
// authorization endpoint.
type StartDeviceAuthorizationRequest struct {
	// ClientID is the registered client identifier.
	ClientID string `json:"client_id"`
	// ClientSecret is the registered client secret.
	ClientSecret string `json:"client_secret"`
	// Scopes is the list of OAuth scopes to request.
	Scopes []string `json:"scopes,omitempty"`
	// PortalURL is an optional portal URL hint.
	PortalURL string `json:"portal_url,omitempty"`
}

// StartDeviceAuthorizationResponse is the response body for a successful device
// authorization request.
type StartDeviceAuthorizationResponse struct {
	// DeviceCode is the device code used to poll for the token.
	DeviceCode string `json:"device_code"`
	// UserCode is the code the user enters at the verification URI.
	UserCode string `json:"user_code"`
	// VerificationURI is the URI the user should visit to authorize.
	VerificationURI string `json:"verification_uri"`
	// VerificationURIComplete is the verification URI with the user code
	// pre-filled.
	VerificationURIComplete string `json:"verification_uri_complete"`
	// ExpiresIn is the lifetime of the device code in seconds.
	ExpiresIn int `json:"expires_in"`
	// Interval is the polling interval in seconds.
	Interval int `json:"interval,omitempty"`
}

// OAuthErrorResponse is the structured error response body from the OAuth
// endpoints.
type OAuthErrorResponse struct {
	// Error is the OAuth 2.0 error code.
	Error string `json:"error"`
	// ErrorDescription is a human-readable description of the error.
	ErrorDescription string `json:"error_description,omitempty"`
}

// OAuthAPIError wraps a non-2xx response from the OAuth endpoints. It
// intentionally does not store the raw response body or the server-supplied
// error_description. Only the HTTP status code, an allowlisted OAuth error code
// (or empty when the code is unknown/unsafe), and a safety-validated request ID
// are retained so that secrets cannot leak through error strings, exported
// fields, or JSON marshaling.
type OAuthAPIError struct {
	// StatusCode is the HTTP status code of the response.
	StatusCode int
	// Code is the allowlisted OAuth 2.0 error code. It is empty when the server
	// returned no code or a code outside the allowlist.
	Code string
	// RequestID is the safety-validated Volcengine request ID from the response
	// header. It is empty when the header value fails the safety check.
	RequestID string
}

// allowedOAuthErrorCodes is the fixed allowlist of standard OAuth 2.0 error
// codes that may be rendered in Error(). Any other value is replaced with a
// generic label so secrets and control characters can never leak through the
// error string.
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
	"authorization_pending":     {},
	"slow_down":                 {},
	"expired_token":             {},
}

// safeRequestID reports whether id is safe to render in an error string: it
// must be at most 128 bytes, contain no CR/LF, and consist solely of a
// conservative diagnostic character set.
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

// sanitizeRequestID returns id when it passes the safety check, otherwise it
// returns an empty string so that unsafe values are never stored in exported
// error fields.
func sanitizeRequestID(id string) string {
	if safeRequestID(id) {
		return id
	}
	return ""
}

// Error renders a safe, human-readable description of the OAuth failure. It
// exposes only an allowlisted OAuth error code (or a generic label otherwise),
// the HTTP status, and a request ID that passes a strict safety check. It never
// includes the raw response body or any secret material.
func (e *OAuthAPIError) Error() string {
	if e == nil {
		return ""
	}
	msg := "unknown error"
	if _, ok := allowedOAuthErrorCodes[e.Code]; ok {
		msg = e.Code
	}
	suffix := "[status " + strconv.Itoa(e.StatusCode)
	if safeRequestID(e.RequestID) {
		suffix += ", requestId: " + e.RequestID
	}
	suffix += "]"
	return "cloudidentity oauth request failed: " + msg + " " + suffix
}

// AccountInfo represents an account returned by the ListAccounts endpoint.
type AccountInfo struct {
	// AccountID is the unique identifier of the account.
	AccountID string `json:"AccountId"`
	// AccountName is the human-readable name of the account.
	AccountName string `json:"AccountName"`
}

// RoleInfo represents a role returned by the ListAccountRoles endpoint.
type RoleInfo struct {
	// AccountID is the account the role belongs to.
	AccountID string `json:"AccountId"`
	// RoleName is the name of the role.
	RoleName string `json:"RoleName"`
}

// RoleCredentials represents the temporary credentials returned by the
// GetRoleCredentials endpoint.
type RoleCredentials struct {
	// AccessKeyID is the temporary access key ID.
	AccessKeyID string `json:"AccessKeyId"`
	// SecretAccessKey is the temporary secret access key.
	SecretAccessKey string `json:"SecretAccessKey"`
	// SessionToken is the temporary session token.
	SessionToken string `json:"sessionToken"`
	// Expiration is the expiration time as a Unix timestamp. The server may
	// return this in either seconds or milliseconds; ExpirationTime normalizes
	// both to a time.Time.
	Expiration int64 `json:"Expiration"`
}

// ExpirationTime returns the expiration as a time.Time, normalizing both
// second- and millisecond-epoch values returned by the server.
func (r RoleCredentials) ExpirationTime() time.Time {
	if r.Expiration <= 0 {
		return time.Time{}
	}
	// Heuristic: values larger than 1e12 (year ~33658 in seconds) are
	// milliseconds; everything else is seconds. This correctly handles all
	// reasonable expiration timestamps for the foreseeable future.
	if r.Expiration >= 1e12 {
		return time.UnixMilli(r.Expiration)
	}
	return time.Unix(r.Expiration, 0)
}

// ResponseMetadata is the metadata envelope returned by Portal API responses.
type ResponseMetadata struct {
	// RequestID is the Volcengine request ID.
	RequestID string `json:"RequestId"`
	// Action is the API action name.
	Action string `json:"Action,omitempty"`
	// Service is the service name.
	Service string `json:"Service,omitempty"`
	// Region is the region.
	Region string `json:"Region,omitempty"`
	// Error is the structured error, if any.
	Error *ResponseError `json:"Error,omitempty"`
}

// ResponseError is the structured error within ResponseMetadata.
type ResponseError struct {
	// Code is the error code.
	Code string `json:"Code"`
	// Message is the error message.
	Message string `json:"Message"`
}

// PortalAPIError wraps a non-2xx response (or a 2xx response carrying an error
// in ResponseMetadata) from the Portal endpoints. It intentionally does not
// store the raw response body or the server-supplied error Code/Message. Only
// the HTTP status code and a safety-validated request ID are retained so that
// secrets cannot leak through error strings, exported fields, or JSON
// marshaling.
type PortalAPIError struct {
	// StatusCode is the HTTP status code of the response.
	StatusCode int
	// RequestID is the safety-validated Volcengine request ID. It is empty when
	// the value fails the safety check.
	RequestID string
}

// Error renders a safe, human-readable description of the Portal failure. It
// exposes only the HTTP status and a request ID that passes a strict safety
// check. It never includes the raw response body, the server-supplied code or
// message, or any secret material.
func (e *PortalAPIError) Error() string {
	if e == nil {
		return ""
	}
	suffix := "[status " + strconv.Itoa(e.StatusCode)
	if safeRequestID(e.RequestID) {
		suffix += ", requestId: " + e.RequestID
	}
	suffix += "]"
	return "cloudidentity portal request failed " + suffix
}
