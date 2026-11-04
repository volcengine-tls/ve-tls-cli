package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth"
	"github.com/volcengine-tls/ve-tls-cli/internal/config"
	"github.com/volcengine-tls/ve-tls-cli/internal/execution"
)

func TestClassifyError_UsageMissingFlag(t *testing.T) {
	p, code := classifyError(errString("missing --topic-id"), "", 0, "")
	if code != 1 {
		t.Fatalf("unexpected code: %d", code)
	}
	if p.Kind != "usage" {
		t.Fatalf("unexpected kind: %q", p.Kind)
	}
}

func TestClassifyError_DecodeFilterError(t *testing.T) {
	p, code := classifyError(errString("filter expects object at \"a\""), "", 0, "")
	if code != 3 {
		t.Fatalf("unexpected code: %d", code)
	}
	if p.Kind != "decode" {
		t.Fatalf("unexpected kind: %q", p.Kind)
	}
}

func TestClassifyError_UsageUnknownActionOrGroup(t *testing.T) {
	cases := []string{
		"unknown api group: log",
		"action not found: search-logs",
		"group not found: log",
	}
	for _, msg := range cases {
		p, code := classifyError(errString(msg), "", 0, "")
		if code != 1 {
			t.Fatalf("%q unexpected code: %d", msg, code)
		}
		if p.Kind != "usage" {
			t.Fatalf("%q unexpected kind: %q", msg, p.Kind)
		}
		if !strings.Contains(p.Hint, "volclog tool list") {
			t.Fatalf("%q unexpected hint: %q", msg, p.Hint)
		}
	}
}

func TestClassifyError_UsageUnknownToolAndWorkflowIdentity(t *testing.T) {
	cases := []struct {
		msg          string
		group        string
		wantHintPart string
	}{
		{
			msg:          "unknown tool: log.not-real",
			group:        "tool",
			wantHintPart: "volclog tool list",
		},
		{
			msg:          "unknown workflow: log.not-real",
			group:        "workflow",
			wantHintPart: "volclog workflow list",
		},
	}
	for _, tc := range cases {
		p, code := classifyError(errString(tc.msg), "", 0, tc.group)
		if code != 1 {
			t.Fatalf("%q unexpected code: %d", tc.msg, code)
		}
		if p.Kind != "usage" {
			t.Fatalf("%q unexpected kind: %q", tc.msg, p.Kind)
		}
		if !strings.Contains(p.Hint, tc.wantHintPart) {
			t.Fatalf("%q unexpected hint: %q", tc.msg, p.Hint)
		}
	}
}

func TestClassifyError_UsageMissingFlagHasDescribeHint(t *testing.T) {
	p, code := classifyError(errString("missing --topic-id"), "", 0, "")
	if code != 1 {
		t.Fatalf("unexpected code: %d", code)
	}
	if !strings.Contains(p.Hint, "volclog tool describe <group.action>") {
		t.Fatalf("unexpected hint: %q", p.Hint)
	}
}

func TestClassifyError_GlobalFlagHasPositionHint(t *testing.T) {
	p, code := classifyError(errString("unknown flag: --output-mode"), "", 0, "")
	if code != 1 {
		t.Fatalf("unexpected code: %d", code)
	}
	if !strings.Contains(p.Hint, "position-sensitive") {
		t.Fatalf("unexpected hint: %q", p.Hint)
	}
}

func TestRunMisplacedGlobalFlagHintIncludesFlagAndExample(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"configure", "set", "--output-file", "/tmp/out.json"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("unexpected code: %d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	p := c4InvocationFailure(t, stdout.String(), stderr.String(), false)
	if p.Kind != "usage" {
		t.Fatalf("unexpected kind: %q", p.Kind)
	}
	hint := p.Hint
	if !strings.Contains(hint, "--output-file") {
		t.Fatalf("hint should mention flag name: %q", hint)
	}
	if !strings.Contains(hint, "volclog --output-file") {
		t.Fatalf("hint should include example: %q", hint)
	}
	if !strings.Contains(hint, "before 'configure'") {
		t.Fatalf("hint should include group position: %q", hint)
	}
}

func TestWriteCLIErrorDoesNotEscapeAngleBrackets(t *testing.T) {
	var buf bytes.Buffer
	writeCLIError(&buf, errString("bad args"), "", 0, "usage", "inspect constraints with 'volclog tool describe <group.action>' or run --help")
	out := buf.String()
	if strings.Contains(out, `\u003c`) || strings.Contains(out, `\u003e`) {
		t.Fatalf("angle brackets should not be escaped: %q", out)
	}
	if !strings.Contains(out, "volclog tool describe <group.action>") {
		t.Fatalf("expected raw angle bracket hint in output: %q", out)
	}
}

func TestClassifyError_ToolExecRequiresFileURLIsUsage(t *testing.T) {
	p, code := classifyError(errString("--context must use file://"), "", 0, "tool")
	if code != 1 {
		t.Fatalf("unexpected code: %d", code)
	}
	if p.Kind != "usage" {
		t.Fatalf("unexpected kind: %q", p.Kind)
	}
	if !strings.Contains(p.Hint, "volclog tool exec") {
		t.Fatalf("unexpected hint: %q", p.Hint)
	}
}

func TestClassifyError_MissingRequiredFieldIsValidation(t *testing.T) {
	cases := []string{
		"missing required field: input.query.ProjectId",
		"missing required fields: input.body.ProjectId, input.body.TopicName",
		"workflow input missing required fields: TopicId, Input",
	}
	for _, msg := range cases {
		p, code := classifyError(errString(msg), "", 0, "tool")
		if code != 1 {
			t.Fatalf("%q unexpected code: %d", msg, code)
		}
		if p.Kind != "validation" {
			t.Fatalf("%q unexpected kind: %q", msg, p.Kind)
		}
		if strings.TrimSpace(p.Hint) == "" {
			t.Fatalf("%q expected validation hint", msg)
		}
	}
}

func TestClassifyError_ValidationHintMatchesSurface(t *testing.T) {
	toolPayload, _ := classifyError(errString("missing required fields: input.body.ProjectId, input.body.TopicName"), "", 0, "tool")
	if strings.Contains(toolPayload.Hint, "workflow describe") {
		t.Fatalf("tool validation hint should not mention workflow describe: %q", toolPayload.Hint)
	}
	if !strings.Contains(toolPayload.Hint, "tool describe") {
		t.Fatalf("tool validation hint should mention tool describe: %q", toolPayload.Hint)
	}

	workflowPayload, _ := classifyError(errString("workflow input missing required fields: TopicId, Input"), "", 0, "workflow")
	if strings.Contains(workflowPayload.Hint, "tool describe") {
		t.Fatalf("workflow validation hint should not mention tool describe: %q", workflowPayload.Hint)
	}
	if !strings.Contains(workflowPayload.Hint, "workflow describe") {
		t.Fatalf("workflow validation hint should mention workflow describe: %q", workflowPayload.Hint)
	}
}

func TestClassifyError_PageAllUnsupportedIsUnsupportedFeature(t *testing.T) {
	cases := []string{
		"execution.page.all is not supported for tool: topic.create-topic",
		"tool topic.describe-topics declares page.all support but runtime pagination metadata is unavailable",
	}
	for _, msg := range cases {
		p, code := classifyError(errString(msg), "", 0, "tool")
		if code != 1 {
			t.Fatalf("%q unexpected code: %d", msg, code)
		}
		if p.Kind != "unsupported_feature" {
			t.Fatalf("%q unexpected kind: %q", msg, p.Kind)
		}
		if strings.TrimSpace(p.Hint) == "" {
			t.Fatalf("%q expected unsupported_feature hint", msg)
		}
	}
}

func TestClassifyError_TypedUnsupportedFeature(t *testing.T) {
	payload, code := classifyError(
		newUnsupportedFeatureError("feature is unavailable", "use the generic resolver"),
		"", 0, "workflow",
	)
	if code != 1 || payload.Kind != "unsupported_feature" {
		t.Fatalf("unexpected classification: code=%d payload=%#v", code, payload)
	}
	if payload.Hint != "use the generic resolver" {
		t.Fatalf("unexpected hint: %q", payload.Hint)
	}
}

func TestClassifyError_MissingWritableOutputDirIsFilesystem(t *testing.T) {
	p, code := classifyError(errString("missing writable output_dir"), "", 0, "raw")
	if code != 2 {
		t.Fatalf("unexpected code: %d", code)
	}
	if p.Kind != "filesystem" {
		t.Fatalf("unexpected kind: %q", p.Kind)
	}
	if strings.TrimSpace(p.Hint) == "" {
		t.Fatalf("expected filesystem hint")
	}
}

func TestClassifyError_UnknownFallbackHasHint(t *testing.T) {
	p, code := classifyError(errString("some brand new residual failure"), "", 0, "tool")
	if code != 2 {
		t.Fatalf("unexpected code: %d", code)
	}
	if p.Kind != "unknown" {
		t.Fatalf("unexpected kind: %q", p.Kind)
	}
	if strings.TrimSpace(p.Hint) == "" {
		t.Fatalf("expected unknown fallback hint, got empty hint")
	}
}

func TestClassifyError_CommonResidualValidationShapes(t *testing.T) {
	cases := []string{
		"consume request body must be json object",
		"jsonl line must be object",
		"json-array input must be json array",
		"unsupported log contents type",
		"json: cannot unmarshal object into Go struct field",
	}
	for _, msg := range cases {
		p, code := classifyError(errString(msg), "", 0, "tool")
		if code != 1 {
			t.Fatalf("%q unexpected code: %d", msg, code)
		}
		if p.Kind != "validation" {
			t.Fatalf("%q unexpected kind: %q", msg, p.Kind)
		}
	}
}

func TestClassifyError_CommonResidualRuntimeKinds(t *testing.T) {
	cases := []struct {
		msg  string
		kind string
		code int
	}{
		{"kafka record payload requires --output-mode file", "incompatible_flags", 1},
		{"unexpected list response", "decode", 3},
		{"unexpected list field: Topics", "decode", 3},
		{"cannot infer list field for --all", "decode", 3},
		{"working directory not found", "filesystem", 2},
	}
	for _, tc := range cases {
		p, code := classifyError(errString(tc.msg), "", 0, "tool")
		if code != tc.code {
			t.Fatalf("%q unexpected code: %d", tc.msg, code)
		}
		if p.Kind != tc.kind {
			t.Fatalf("%q unexpected kind: %q", tc.msg, p.Kind)
		}
	}
}

func TestClassifyError_ConflictingSelectorsAreIncompatibleFlags(t *testing.T) {
	cases := []string{
		"conflicting profile selectors: global --profile=a conflicts with context.profile=b",
		"conflicting runtime selectors: global --profile=a conflicts with context.secrets_file=/tmp/secrets.env",
	}
	for _, msg := range cases {
		p, code := classifyError(errString(msg), "", 0, "tool")
		if code != 1 {
			t.Fatalf("%q unexpected code: %d", msg, code)
		}
		if p.Kind != "incompatible_flags" {
			t.Fatalf("%q unexpected kind: %q", msg, p.Kind)
		}
		if !strings.Contains(p.Hint, "use exactly one runtime selector") {
			t.Fatalf("%q unexpected hint: %q", msg, p.Hint)
		}
	}
}

func TestIndexNotExistsHintSuggestsCreateIndex(t *testing.T) {
	p, code := classifyError(&httpError{
		statusCode: 404,
		body:       []byte(`{"ErrorCode":"IndexNotExists","ErrorMessage":"index not exists"}`),
		requestID:  "req-index",
	}, "req-index", 404, "index")
	if code != 2 {
		t.Fatalf("unexpected code: %d", code)
	}
	hint := p.Hint
	if !strings.Contains(hint, "volclog index create --describe") || !strings.Contains(hint, "volclog index create --print-request-template=full") {
		t.Fatalf("unexpected hint: %q", hint)
	}
}

// TestConsoleReauthErrorHasLoginHint proves that a Console Login reauth error
// (wrapped with the console-login mode) is classified as kind=auth with a hint
// exactly equal to 'volclog login --profile <name>'.
func TestConsoleReauthErrorHasLoginHint(t *testing.T) {
	inner := &auth.Error{
		Kind:        auth.ReauthRequired,
		Description: "console login cache missing; run: volclog login",
	}
	err := newDynamicAuthError(config.AuthModeConsoleLogin, inner)
	p, code := classifyError(err, "", 0, "tool")
	if code != 2 {
		t.Fatalf("unexpected code: %d", code)
	}
	if p.Kind != "auth" {
		t.Fatalf("expected kind=auth, got %q", p.Kind)
	}
	if p.Hint != "volclog login --profile <name>" {
		t.Fatalf("expected hint exactly 'volclog login --profile <name>', got %q", p.Hint)
	}
}

// TestSSOReauthErrorHasSSOLoginHint proves that an SSO reauth error (wrapped
// with the sso mode) is classified as kind=auth with a hint exactly equal to
// 'volclog sso login --profile <name>'.
func TestSSOReauthErrorHasSSOLoginHint(t *testing.T) {
	inner := &auth.Error{
		Kind:        auth.ReauthRequired,
		Description: "sso token cache missing; run: volclog sso login",
	}
	err := newDynamicAuthError(config.AuthModeSSO, inner)
	p, code := classifyError(err, "", 0, "tool")
	if code != 2 {
		t.Fatalf("unexpected code: %d", code)
	}
	if p.Kind != "auth" {
		t.Fatalf("expected kind=auth, got %q", p.Kind)
	}
	if p.Hint != "volclog sso login --profile <name>" {
		t.Fatalf("expected hint exactly 'volclog sso login --profile <name>', got %q", p.Hint)
	}
}

// TestPlainAuthErrorIsKindAuthWithoutModeHint proves that a plain *auth.Error
// (not wrapped with a mode) is still classified as kind=auth, but carries no
// mode-specific re-login hint.
func TestPlainAuthErrorIsKindAuthWithoutModeHint(t *testing.T) {
	err := &auth.Error{Kind: auth.ReauthRequired, Description: "reauth required"}
	p, code := classifyError(err, "", 0, "tool")
	if code != 2 {
		t.Fatalf("unexpected code: %d", code)
	}
	if p.Kind != "auth" {
		t.Fatalf("expected kind=auth, got %q", p.Kind)
	}
	if p.Hint != "" {
		t.Fatalf("expected empty hint for plain auth.Error, got %q", p.Hint)
	}
}

// TestAuthErrorsAreKindAuth proves that all auth.Error kinds (not just
// ReauthRequired) are classified as kind=auth so callers can detect
// authentication failures uniformly.
func TestAuthErrorsAreKindAuth(t *testing.T) {
	cases := []struct {
		name string
		kind auth.ErrorKind
	}{
		{"reauth_required", auth.ReauthRequired},
		{"cache_missing", auth.CacheMissing},
		{"cache_corrupt", auth.CacheCorrupt},
		{"config_invalid", auth.ConfigInvalid},
		{"protocol_error", auth.ProtocolError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := &auth.Error{Kind: tc.kind, Description: "test " + string(tc.kind)}
			p, _ := classifyError(err, "", 0, "tool")
			if p.Kind != "auth" {
				t.Fatalf("expected kind=auth for %s, got %q", tc.name, p.Kind)
			}
		})
	}
}

type errString string

func (e errString) Error() string { return string(e) }

type typedClassificationTestError struct {
	kind  string
	code  string
	exit  int
	cause error
}

func (e *typedClassificationTestError) Error() string         { return "synthetic typed error" }
func (e *typedClassificationTestError) ErrorKind() string     { return e.kind }
func (e *typedClassificationTestError) ErrorCode() string     { return e.code }
func (e *typedClassificationTestError) DeliveryExitCode() int { return e.exit }
func (e *typedClassificationTestError) Unwrap() error         { return e.cause }

func TestClassifyTypedOperationErrorUsesKindCodeAndDeliveryExitFromSameLayer(t *testing.T) {
	for _, tc := range []struct {
		kind     string
		code     string
		delivery int
		wantExit int
		hint     string
	}{
		{kind: "validation", code: "InvalidRequest", wantExit: 1},
		{kind: "protocol", code: "InvalidJSONResponse", wantExit: 3},
		{kind: "result_unknown", code: "RequestResultUnknown", wantExit: 2, hint: "do not automatically replay"},
		{kind: "partial_failure", code: "IngestPartialFailure", wantExit: 2, hint: "current batch"},
		{kind: "output_failed", code: "OutputFailed", delivery: 2, wantExit: 2, hint: "do not rerun"},
		{kind: "output_failed", code: "OutputFailed", delivery: 3, wantExit: 3, hint: "do not rerun"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			err := &typedClassificationTestError{
				kind: tc.kind, code: tc.code, exit: tc.delivery,
				cause: &typedClassificationTestError{kind: "validation", code: "InnerCode", exit: 3},
			}
			payload, exit := classifyError(err, "req-current", 201, "tool")
			if exit != tc.wantExit || payload.Kind != tc.kind || payload.ErrorCode != tc.code ||
				payload.RequestID != "req-current" || payload.StatusCode != 201 ||
				(tc.hint != "" && !strings.Contains(payload.Hint, tc.hint)) {
				t.Fatalf("payload=%#v exit=%d, want kind=%q code=%q exit=%d", payload, exit, tc.kind, tc.code, tc.wantExit)
			}
		})
	}
}

func TestClassifyTypedOperationErrorUnwrapsEmptyKindAndJoin(t *testing.T) {
	inner := &typedClassificationTestError{kind: "protocol", code: "InvalidJSONResponse"}
	outer := &typedClassificationTestError{code: "IgnoredOuterCode", exit: 2, cause: inner}
	for _, err := range []error{outer, errors.Join(errors.New("untyped branch"), outer)} {
		payload, exit := classifyError(err, "req", 200, "tool")
		if exit != 3 || payload.Kind != "protocol" || payload.ErrorCode != "InvalidJSONResponse" {
			t.Fatalf("payload=%#v exit=%d", payload, exit)
		}
	}

	// An empty outer kind must also leave legacy authentication classification intact.
	authCause := &auth.Error{Kind: auth.ReauthRequired}
	err := &execution.Failure{Code: "IgnoredOuterCode", Cause: authCause}
	payload, exit := classifyError(err, "", 0, "tool")
	if exit != 2 || payload.Kind != "auth" || payload.ErrorCode != "" {
		t.Fatalf("auth payload=%#v exit=%d", payload, exit)
	}
}

func TestClassifyTypedOperationErrorPreservesFrozenAuthContracts(t *testing.T) {
	typed := &typedClassificationTestError{kind: "result_unknown", code: "RequestResultUnknown"}
	for _, group := range []string{"login", "logout", "sso", "configure"} {
		payload, exit := classifyError(typed, "", 0, group)
		if exit != 2 || payload.Kind != "unknown" || payload.ErrorCode != "" {
			t.Fatalf("group=%q payload=%#v exit=%d", group, payload, exit)
		}
	}
	joined := errors.Join(ErrSSOLogoutPartialFailure, typed)
	payload, exit := classifyError(joined, "", 0, "tool")
	if exit != 2 || payload.Kind != "partial_failure" || !strings.Contains(payload.Hint, "sso local token") {
		t.Fatalf("sentinel payload=%#v exit=%d", payload, exit)
	}
	he := &httpError{statusCode: 409, body: []byte(`{"ErrorCode":"AlreadyExists"}`)}
	payload, exit = classifyError(he, "", 0, "login")
	if exit != 2 || payload.Kind != "server" {
		t.Fatalf("frozen auth HTTP payload=%#v exit=%d", payload, exit)
	}
}

func TestClassifyContextValidationUsesStableCode(t *testing.T) {
	payload, exit := classifyError(&toolContextValidationError{Path: "$.execution", Reason: "invalid"}, "", 0, "tool")
	if exit != 1 || payload.Kind != "validation" || payload.ErrorCode != "InvalidContext" {
		t.Fatalf("payload=%#v exit=%d", payload, exit)
	}
}

func TestClassifyKnownThrottleCodes(t *testing.T) {
	for _, serviceCode := range []string{"Throttling", "ThrottlingException", "LimitExceeded", "RequestLimitExceeded", "TooManyRequests"} {
		t.Run(serviceCode, func(t *testing.T) {
			err := &httpError{statusCode: 400, body: []byte(`{"ErrorCode":"` + serviceCode + `"}`), requestID: "req"}
			payload, exit := classifyError(err, "", 0, "tool")
			if exit != 2 || payload.Kind != "throttled" || payload.RequestID != "req" || payload.StatusCode != 400 {
				t.Fatalf("payload=%#v exit=%d", payload, exit)
			}
		})
	}
}

// TestSentinelErrorsTakePriorityOverAuthCause proves that the stable
// transaction/partial-failure sentinels are classified before the generic
// dynamicAuthError/auth.Error checks, even when their cause is an *auth.Error
// (which is production-reachable). The dedicated kind/hint must not be lost.
func TestSentinelErrorsTakePriorityOverAuthCause(t *testing.T) {
	// A unique canary embedded in the cause description must never leak into
	// the classified payload, error string, or hint.
	const causeCanary = "cause_canary_zzz_9f3a"
	authCause := &auth.Error{Kind: auth.ProtocolError, Description: causeCanary}

	cases := []struct {
		name     string
		err      error
		wantKind string
		wantHint string
	}{
		{
			name:     "sso_logout_partial_with_auth_cause",
			err:      newSSOLogoutPartialFailureError(authCause, ssoPartialRevoke),
			wantKind: "partial_failure",
			wantHint: "sso local token/sts cache cleared but remote revoke or config update failed",
		},
		{
			name:     "sso_rollback_with_auth_cause",
			err:      newSSORollbackFailureError(authCause, errors.New("rollback failed")),
			wantKind: "auth",
			wantHint: "sso login failed and token cache rollback failed",
		},
		{
			name:     "console_logout_partial_with_auth_cause",
			err:      newLogoutPartialFailureError(authCause),
			wantKind: "partial_failure",
			wantHint: "console login cache deleted but profile binding could not be cleared",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, code := classifyError(tc.err, "", 0, "tool")
			if code != 2 {
				t.Fatalf("expected exit code 2, got %d", code)
			}
			if p.Kind != tc.wantKind {
				t.Fatalf("expected kind=%q, got %q", tc.wantKind, p.Kind)
			}
			if !strings.Contains(p.Hint, tc.wantHint) {
				t.Fatalf("expected hint to contain %q, got %q", tc.wantHint, p.Hint)
			}
			// The cause canary must not leak into the payload or hint.
			if strings.Contains(p.Hint, causeCanary) {
				t.Fatalf("hint leaked cause canary: %q", p.Hint)
			}
			if strings.Contains(tc.err.Error(), causeCanary) {
				t.Fatalf("error string leaked cause canary: %q", tc.err.Error())
			}
		})
	}
}

// TestSentinelErrorsTakePriorityOverDynamicAuthCause proves the stable
// sentinels also win when the cause is wrapped in *dynamicAuthError (which the
// generic auth classification would otherwise match). This guards the ordering
// robustness even if a future code path wraps a sentinel's cause in the
// mode-aware wrapper.
func TestSentinelErrorsTakePriorityOverDynamicAuthCause(t *testing.T) {
	const causeCanary = "dynamic_cause_canary_7b2c"
	innerCause := &auth.Error{Kind: auth.ProtocolError, Description: causeCanary}
	daeCause := newDynamicAuthError(config.AuthModeSSO, innerCause)

	cases := []struct {
		name     string
		err      error
		wantKind string
		wantHint string
	}{
		{
			name:     "sso_logout_partial",
			err:      newSSOLogoutPartialFailureError(daeCause, ssoPartialRevoke),
			wantKind: "partial_failure",
			wantHint: "sso local token/sts cache cleared but remote revoke or config update failed",
		},
		{
			name:     "sso_rollback",
			err:      newSSORollbackFailureError(daeCause, errors.New("rollback failed")),
			wantKind: "auth",
			wantHint: "sso login failed and token cache rollback failed",
		},
		{
			name:     "console_logout_partial",
			err:      newLogoutPartialFailureError(daeCause),
			wantKind: "partial_failure",
			wantHint: "console login cache deleted but profile binding could not be cleared",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, code := classifyError(tc.err, "", 0, "tool")
			if code != 2 {
				t.Fatalf("expected exit code 2, got %d", code)
			}
			if p.Kind != tc.wantKind {
				t.Fatalf("expected kind=%q (sentinel must win over dynamicAuthError), got %q", tc.wantKind, p.Kind)
			}
			// The dedicated sentinel hint must be present; a generic re-login
			// hint (e.g. from the dynamicAuthError branch) must not pass.
			if !strings.Contains(p.Hint, tc.wantHint) {
				t.Fatalf("expected hint to contain %q (sentinel branch must win), got %q", tc.wantHint, p.Hint)
			}
			if strings.Contains(p.Hint, causeCanary) {
				t.Fatalf("hint leaked cause canary: %q", p.Hint)
			}
		})
	}
}

// TestDynamicAuthHintsAreModeSpecific proves that each workload mode produces a
// mode-specific hint that never suggests volclog login/sso login and never
// contains role/TRN/token path or credential material.
func TestDynamicAuthHintsAreModeSpecific(t *testing.T) {
	cases := []struct {
		mode string
		want string
	}{
		{config.AuthModeRamRoleARN, "source credential"},
		{config.AuthModeOIDC, "token file"},
		{config.AuthModeECSRole, "IMDS"},
	}
	for _, tc := range cases {
		t.Run(tc.mode, func(t *testing.T) {
			hint := dynamicReauthHint(tc.mode)
			if !strings.Contains(hint, tc.want) {
				t.Fatalf("hint=%q, want it to contain %q", hint, tc.want)
			}
			if strings.Contains(hint, "sso login") || strings.Contains(hint, "volclog login") {
				t.Fatalf("hint=%q must not suggest sso/login for workload mode", hint)
			}
		})
	}
	// SSO and Console hints remain exactly as before.
	if got := dynamicReauthHint(config.AuthModeSSO); got != "volclog sso login --profile <name>" {
		t.Fatalf("SSO hint=%q, want unchanged", got)
	}
	if got := dynamicReauthHint(config.AuthModeConsoleLogin); got != "volclog login --profile <name>" {
		t.Fatalf("Console hint=%q, want unchanged", got)
	}
}
