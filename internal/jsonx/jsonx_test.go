package jsonx

import (
	"encoding/json"
	"testing"
)

func TestDecodePreservesNumbers(t *testing.T) {
	value, err := Decode([]byte(`{"big":9007199254740993,"decimal":0.12345678901234567890123456789}`))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("Decode() type = %T, want map[string]any", value)
	}
	for key, want := range map[string]string{
		"big":     "9007199254740993",
		"decimal": "0.12345678901234567890123456789",
	} {
		number, ok := object[key].(json.Number)
		if !ok {
			t.Fatalf("%s type = %T, want json.Number", key, object[key])
		}
		if number.String() != want {
			t.Errorf("%s = %q, want %q", key, number.String(), want)
		}
	}
}

func TestDecodeRejectsTrailingJSON(t *testing.T) {
	for _, input := range []string{
		`{"ok":true} {"second":true}`,
		`{"ok":true} trailing`,
	} {
		t.Run(input, func(t *testing.T) {
			if _, err := Decode([]byte(input)); err == nil {
				t.Fatal("Decode() error = nil, want trailing input error")
			}
		})
	}
}

func TestDecodePreservesTruncatedJSONDiagnostic(t *testing.T) {
	_, err := Decode([]byte(`{"value":`))
	if err == nil || err.Error() != "unexpected end of JSON input" {
		t.Fatalf("Decode() error = %v, want stable truncated JSON diagnostic", err)
	}
}
