// Package sts implements a standalone STS protocol client for AssumeRole and
// AssumeRoleWithOIDC. It reuses the existing volc-sdk-golang/base SignV4
// implementation and the internal/auth/httpx retry helper without pulling in the
// full volcengine-go-sdk credential chain.
package sts

import "time"

// SourceCredential is the long-lived identity used to sign an AssumeRole
// request. The SessionToken is optional and supports role chaining.
type SourceCredential struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}

// Credentials are the temporary credentials returned by STS. ExpiresAt is the
// hard expiration computed by the client; the Provider subtracts its own
// refresh window.
type Credentials struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	ExpiresAt       time.Time
}

// AssumeRoleInput carries the parameters for an AssumeRole call. Region is the
// signing region; when empty the client falls back to cn-beijing. DisableSSL
// switches the fixed STS host from HTTPS to HTTP.
type AssumeRoleInput struct {
	Source     SourceCredential
	AccountID  string
	RoleName   string
	Region     string
	DisableSSL bool
}

// OIDCInput carries the parameters for an AssumeRoleWithOIDC call. Token is the
// raw token file bytes and is forwarded verbatim, including any trailing
// newline. DisableSSL switches the fixed STS host from HTTPS to HTTP.
type OIDCInput struct {
	Token      []byte
	RoleTRN    string
	DisableSSL bool
}
