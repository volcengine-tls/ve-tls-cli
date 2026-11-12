package console

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/url"
)

// ErrorDiagnostic is the bounded, secret-free login error information exposed
// to the CLI error surface. Message is composed only from fixed local phases
// and safe protocol classifications; it is never copied from an arbitrary
// error string.
type ErrorDiagnostic struct {
	Message    string
	Stage      string
	StatusCode int
	RequestID  string
}

const (
	LoginStageSetup         = "setup"
	LoginStageDeviceCode    = "device_code_request"
	LoginStageWaitForUser   = "wait_for_user"
	LoginStageTokenPoll     = "token_poll"
	LoginStageBrowser       = "browser_callback"
	LoginStageTokenExchange = "token_exchange"
	LoginStageTokenResponse = "token_response"
	LoginStageConfirmation  = "session_confirmation"
	LoginStageCache         = "cache"
	LoginStageConfigUpdate  = "config_update"

	diagnosticLoginCanceled    = "console login canceled"
	diagnosticLoginTimedOut    = "console login timed out"
	diagnosticCallbackTimedOut = "waiting for browser callback timed out"
	diagnosticNetworkFailure   = "console login network request failed"
	diagnosticDNSFailure       = "console login DNS lookup failed"
	diagnosticTLSFailure       = "console login TLS handshake failed"
)

// safeDiagnosticPhases allows fixed descriptions and stages through the
// diagnostic boundary. A broad wrapper can supply the message while a nested
// failure supplies the stage.
var safeDiagnosticPhases = map[string]string{
	"load config failed":                      LoginStageSetup,
	"create oauth client failed":              LoginStageSetup,
	"device authorization failed":             "",
	"generate PKCE failed":                    LoginStageSetup,
	"generate OAuth state failed":             LoginStageSetup,
	"browser authorization failed":            "",
	"authorization code exchange failed":      LoginStageTokenExchange,
	"validate token response failed":          LoginStageTokenResponse,
	"validate STS credentials failed":         LoginStageTokenResponse,
	"extract login session failed":            LoginStageTokenResponse,
	"compute token expiration failed":         LoginStageTokenResponse,
	"login session confirmation failed":       LoginStageConfirmation,
	"read cache snapshot failed":              LoginStageCache,
	"marshal cache failed":                    LoginStageCache,
	"write cache failed":                      LoginStageCache,
	"config update and cache rollback failed": LoginStageConfigUpdate,
	"config update and cache cleanup failed":  LoginStageConfigUpdate,
	"config update failed":                    LoginStageConfigUpdate,
	"start device authorization failed":       LoginStageDeviceCode,
	"waiting for device authorization failed": LoginStageWaitForUser,
	"polling device authorization failed":     LoginStageTokenPoll,
	"create callback server failed":           LoginStageBrowser,
	"cleanup callback server failed":          LoginStageBrowser,
	"authorization and cleanup failed":        "",
	"build authorize URL failed":              LoginStageBrowser,
	"wait for callback failed":                LoginStageBrowser,
	"context already done":                    LoginStageSetup,
}

// DiagnoseError converts a console login error into a safe diagnostic. The
// outermost allowlisted safeError phase is retained and, when present, joined
// with one safe inner reason (API, callback, context, or transport). All
// causes remain available to errors.Is/errors.As on the original error; this
// function only controls what is rendered.
func DiagnoseError(err error) ErrorDiagnostic {
	if err == nil {
		return ErrorDiagnostic{}
	}

	var phases []*safeError
	collectSafeErrors(err, &phases)
	var phase, stage string
	for _, failure := range phases {
		value, known := safeDiagnosticPhases[failure.desc]
		if !known {
			continue
		}
		if phase == "" {
			phase = failure.desc
		}
		if value != "" {
			stage = value
			break
		}
	}

	var apiErr *ConsoleOAuthAPIError
	if errors.As(err, &apiErr) && apiErr != nil {
		result := ErrorDiagnostic{
			Message:    composeDiagnosticMessage(phase, apiErr.Error()),
			Stage:      stage,
			StatusCode: apiErr.StatusCode,
		}
		if safeRequestID(apiErr.RequestID) {
			result.RequestID = apiErr.RequestID
		}
		return result
	}

	detail := ""
	var callbackErr *callbackError
	if errors.As(err, &callbackErr) && callbackErr != nil {
		detail = callbackErr.Error()
	} else if isCallbackWaitTimeout(err) {
		detail = diagnosticCallbackTimedOut
	} else if errors.Is(err, errDeviceAuthorizationExpired) {
		detail = errDeviceAuthorizationExpired.Error()
	} else if errors.Is(err, context.Canceled) {
		detail = diagnosticLoginCanceled
	} else if errors.Is(err, context.DeadlineExceeded) {
		detail = diagnosticLoginTimedOut
	} else if networkKind := classifyNetworkError(err); networkKind != "" {
		detail = networkKind
	}

	if phase == "" && detail == "" {
		return ErrorDiagnostic{}
	}
	return ErrorDiagnostic{Message: composeDiagnosticMessage(phase, detail), Stage: stage}
}

func composeDiagnosticMessage(phase, detail string) string {
	switch {
	case phase != "" && detail != "":
		return phase + ": " + detail
	case phase != "":
		return phase
	default:
		return detail
	}
}

// isCallbackWaitTimeout distinguishes the local browser wait deadline from
// other deadline errors. It searches the chain rather than relying on the
// current outer wrapper, because LoginService adds a browser phase around the
// LocalAuthorizer error.
func isCallbackWaitTimeout(err error) bool {
	if !errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var phases []*safeError
	collectSafeErrors(err, &phases)
	for _, phase := range phases {
		if phase != nil && phase.desc == "wait for callback failed" && errors.Is(phase, context.DeadlineExceeded) {
			return true
		}
	}
	return false
}

// collectSafeErrors walks both ordinary and joined unwrap chains. errors.As
// intentionally returns only the first matching safeError, while timeout
// detection needs to see an inner wait phase as well.
func collectSafeErrors(err error, out *[]*safeError) {
	if err == nil {
		return
	}
	if phase, ok := err.(*safeError); ok && phase != nil {
		*out = append(*out, phase)
		for _, cause := range phase.causes {
			collectSafeErrors(cause, out)
		}
		return
	}

	switch unwrapped := err.(type) {
	case interface{ Unwrap() []error }:
		for _, cause := range unwrapped.Unwrap() {
			collectSafeErrors(cause, out)
		}
	case interface{ Unwrap() error }:
		collectSafeErrors(unwrapped.Unwrap(), out)
	}
}

// classifyNetworkError returns fixed descriptions for standard network, DNS,
// and TLS error types. It never reads their Error() strings, URLs, or causes.
func classifyNetworkError(err error) string {
	var tlsCertErr *tls.CertificateVerificationError
	if errors.As(err, &tlsCertErr) && tlsCertErr != nil {
		return diagnosticTLSFailure
	}
	var tlsRecordErr tls.RecordHeaderError
	if errors.As(err, &tlsRecordErr) {
		return diagnosticTLSFailure
	}
	var tlsAlertErr tls.AlertError
	if errors.As(err, &tlsAlertErr) {
		return diagnosticTLSFailure
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) && dnsErr != nil {
		return diagnosticDNSFailure
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr != nil {
		return diagnosticNetworkFailure
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr != nil {
		return diagnosticNetworkFailure
	}
	return ""
}
