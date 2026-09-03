package cli

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/execution"
	"github.com/volcengine-tls/ve-tls-cli/internal/jsonx"
	"github.com/volcengine-tls/ve-tls-cli/internal/tlsapi"
)

func TestDecodeResponsePreservesNumbers(t *testing.T) {
	value, err := decodeResponse(tlsapi.Response{
		StatusCode: http.StatusOK,
		Body:       []byte(`{"big":9007199254740993,"decimal":0.12345678901234567890123456789}`),
	})
	if err != nil {
		t.Fatalf("decodeResponse() error = %v", err)
	}
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("value type = %T, want map[string]any", value)
	}
	for key, want := range map[string]string{
		"big":     "9007199254740993",
		"decimal": "0.12345678901234567890123456789",
	} {
		number, ok := object[key].(json.Number)
		if !ok || number.String() != want {
			t.Errorf("%s = %#v, want json.Number(%q)", key, object[key], want)
		}
	}
}

func TestDecodeResponseRejectsTrailingValue(t *testing.T) {
	_, err := decodeResponse(tlsapi.Response{
		StatusCode: http.StatusOK,
		Body:       []byte(`{"ok":true} {"second":true}`),
	})
	if !errors.Is(err, jsonx.ErrTrailingData) {
		t.Fatalf("error = %v, want jsonx.ErrTrailingData", err)
	}
}

func TestParseHTTPErrorPayloadPreservesNumbers(t *testing.T) {
	code, message, details := parseHTTPErrorPayload(&httpError{body: []byte(`{"ErrorCode":"Invalid","ErrorMessage":"{\"errorMessage\":\"bad\",\"retry_after\":9007199254740993}"}`)})
	if code != "Invalid" || message != "bad" {
		t.Fatalf("code/message = %q/%q", code, message)
	}
	number, ok := details["retry_after"].(json.Number)
	if !ok || number.String() != "9007199254740993" {
		t.Fatalf("details = %#v, want json.Number", details)
	}
}

func TestToolExecutionPlanValuePreservesNumbers(t *testing.T) {
	value, err := toolExecutionPlanValue(&execution.DryRunPlan{
		RequestPreview: map[string]any{
			"body": map[string]any{"big": json.Number("9007199254740993")},
		},
	})
	if err != nil {
		t.Fatalf("toolExecutionPlanValue() error = %v", err)
	}
	preview, ok := value["request_preview"].(map[string]any)
	if !ok {
		t.Fatalf("request_preview type = %T", value["request_preview"])
	}
	body, ok := preview["body"].(map[string]any)
	if !ok {
		t.Fatalf("body type = %T", preview["body"])
	}
	number, ok := body["big"].(json.Number)
	if !ok || number.String() != "9007199254740993" {
		t.Fatalf("body = %#v, want json.Number", body)
	}
}
