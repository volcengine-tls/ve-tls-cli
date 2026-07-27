package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

func TestReauthRequiredErrorNeverIncludesSecretCauseBody(t *testing.T) {
	const secret = "SECRET_RESPONSE_BODY_DO_NOT_LEAK"
	cause := errors.New("upstream response: " + secret)
	err := &Error{
		Kind:        ReauthRequired,
		Status:      401,
		RequestID:   "request-123",
		Description: "  session expired\nplease login again\t",
		Cause:       cause,
	}

	text := err.Error()
	if strings.Contains(text, secret) || strings.Contains(text, cause.Error()) {
		t.Fatalf("Error leaked secret cause: %q", text)
	}
	for _, want := range []string{
		string(ReauthRequired),
		"status=401",
		"request_id=request-123",
		"description=session expired please login again",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("Error text=%q, want %q", text, want)
		}
	}
	if !errors.Is(err, cause) {
		t.Fatal("Error must preserve cause for errors.Is without rendering it")
	}
	var got *Error
	if !errors.As(err, &got) || got != err {
		t.Fatalf("errors.As got %#v, want original error", got)
	}
}

func TestErrorKindsAreMatchable(t *testing.T) {
	err := &Error{Kind: CacheCorrupt, Description: "cache decode failed"}
	if !errors.Is(err, &Error{Kind: CacheCorrupt}) {
		t.Fatal("errors.Is must match the same non-empty error kind")
	}
	if errors.Is(err, &Error{Kind: CacheMissing}) {
		t.Fatal("errors.Is matched a different error kind")
	}
}

func TestErrorIsDoesNotPanicForTypedNilTarget(t *testing.T) {
	err := &Error{Kind: CacheMissing}
	var target *Error
	if errors.Is(err, target) {
		t.Fatal("errors.Is matched a typed nil target")
	}
}

func TestErrorSanitizeRemovesControlsAndLimitsLength(t *testing.T) {
	err := &Error{
		Kind:        ProtocolError,
		RequestID:   "\x1b[31mrequest\x00-id",
		Description: "line one\r\nline two\a " + strings.Repeat("界", 1000),
	}
	text := err.Error()
	for _, r := range text {
		if unicode.IsControl(r) {
			t.Fatalf("Error contains control rune %U: %q", r, text)
		}
	}
	if got := utf8.RuneCountInString(sanitize(err.RequestID)); got > maxSafeFieldRunes {
		t.Fatalf("sanitized request ID length=%d, want <=%d", got, maxSafeFieldRunes)
	}
	if got := utf8.RuneCountInString(sanitize(err.Description)); got > maxSafeFieldRunes {
		t.Fatalf("sanitized description length=%d, want <=%d", got, maxSafeFieldRunes)
	}
}

func TestErrorRendersSanitizedServiceCode(t *testing.T) {
	err := &Error{
		Kind:        ProtocolError,
		Status:      400,
		ServiceCode: "Throttling",
		RequestID:   "req-1",
		Description: "STS service error",
	}
	text := err.Error()
	if !strings.Contains(text, "service_code=Throttling") {
		t.Fatalf("Error text=%q, want service_code=Throttling", text)
	}
	// Control characters in ServiceCode must be sanitized.
	err2 := &Error{
		Kind:        ProtocolError,
		ServiceCode: "Throttling\x00Extra",
	}
	text2 := err2.Error()
	for _, r := range text2 {
		if unicode.IsControl(r) {
			t.Fatalf("Error contains control rune %U in ServiceCode: %q", r, text2)
		}
	}
	if !strings.Contains(text2, "service_code=Throttling Extra") {
		t.Fatalf("Error text=%q, want sanitized service_code", text2)
	}
	// Length must be capped.
	err3 := &Error{
		Kind:        ProtocolError,
		ServiceCode: strings.Repeat("A", 1000),
	}
	if got := utf8.RuneCountInString(sanitize(err3.ServiceCode)); got > maxSafeFieldRunes {
		t.Fatalf("sanitized service code length=%d, want <=%d", got, maxSafeFieldRunes)
	}
}

func TestErrorServiceCodeExtractableViaErrorsAs(t *testing.T) {
	want := "RequestLimitExceeded"
	err := &Error{Kind: ProtocolError, ServiceCode: want}
	var got *Error
	if !errors.As(err, &got) {
		t.Fatal("errors.As failed")
	}
	if got.ServiceCode != want {
		t.Fatalf("ServiceCode=%q, want %q", got.ServiceCode, want)
	}
}

func TestErrorCauseNeverLeaksThroughJSONOrFormat(t *testing.T) {
	const secret = "CAUSE-SECRET-DO-NOT-LEAK"
	cause := errors.New("inner: " + secret)
	err := &Error{
		Kind:        ProtocolError,
		Status:      500,
		ServiceCode: "InternalError",
		Description: "transport failure",
		Cause:       cause,
	}

	// Surface 1: Error() must not contain the cause.
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("Error() leaked cause: %q", err.Error())
	}

	// Surface 2: fmt %+v must not contain the cause.
	formatted := fmt.Sprintf("%+v", err)
	if strings.Contains(formatted, secret) {
		t.Fatalf("fmt %%+v leaked cause: %q", formatted)
	}

	// Surface 3: json.Marshal must not contain the cause (Cause has json:"-").
	b, jerr := json.Marshal(err)
	if jerr != nil {
		t.Fatalf("json.Marshal error: %v", jerr)
	}
	if strings.Contains(string(b), secret) {
		t.Fatalf("json.Marshal leaked cause: %s", b)
	}
	// Cause field must be absent from JSON entirely.
	if strings.Contains(string(b), "Cause") || strings.Contains(string(b), "cause") {
		t.Fatalf("json.Marshal included Cause field: %s", b)
	}

	// Unwrap must still reach the cause for errors.Is/As.
	if !errors.Is(err, cause) {
		t.Fatal("errors.Is must still reach the cause after json:\"-\"")
	}
}
