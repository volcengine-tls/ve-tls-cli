package execution

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/jsonx"
)

func TestDecodeJSONResponsePreservesNumbers(t *testing.T) {
	value, err := decodeJSONResponse(Response{
		StatusCode: http.StatusOK,
		Body:       []byte(`{"big":9007199254740993,"decimal":0.12345678901234567890123456789}`),
	})
	if err != nil {
		t.Fatalf("decodeJSONResponse() error = %v", err)
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

func TestDecodeJSONResponseRejectsTrailingValue(t *testing.T) {
	_, err := decodeJSONResponse(Response{
		StatusCode: http.StatusOK,
		Body:       []byte(`{"ok":true} {"second":true}`),
	})
	if !errors.Is(err, jsonx.ErrTrailingData) {
		t.Fatalf("error = %v, want jsonx.ErrTrailingData", err)
	}
}
