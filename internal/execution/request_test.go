package execution

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestBuildRequestResolvesPathAndConvertsQueryHeaderAndBody(t *testing.T) {
	op := requestTestOperation()
	input := Input{
		Path: map[string]any{"ProjectId": "project 1"},
		Query: map[string]any{
			"Bool":  true,
			"Count": float64(3),
			"Tags":  []any{"a", "b"},
			"Empty": nil,
		},
		Header: map[string]any{"X-Test": false},
		Body: Payload{
			JSON:   map[string]any{"Name": "demo"},
			Format: BodyFormatJSON,
		},
	}
	snapshot := cloneInputForTest(input)

	got, err := BuildRequest(op, input)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	if got.Method != "POST" || got.Path != "/projects/project 1" {
		t.Fatalf("method/path = %s %s", got.Method, got.Path)
	}
	if want := map[string]string{
		"Bool": "true", "Count": "3", "Tags": `["a","b"]`, "Empty": "",
	}; !reflect.DeepEqual(got.Query, want) {
		t.Fatalf("query = %#v, want %#v", got.Query, want)
	}
	if !reflect.DeepEqual(got.Header, map[string]string{"X-Test": "false"}) {
		t.Fatalf("header = %#v", got.Header)
	}
	var body map[string]any
	if err := json.Unmarshal(got.Body, &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["Name"] != "demo" {
		t.Fatalf("body = %#v", body)
	}
	if !reflect.DeepEqual(input, snapshot) {
		t.Fatalf("BuildRequest mutated input: got %#v want %#v", input, snapshot)
	}
}

func TestBuildRequestUsesRawPayloadAndRejectsUnresolvedPath(t *testing.T) {
	op := requestTestOperation()
	raw := []byte("{\"Name\":\"raw\"}\n")
	got, err := BuildRequest(op, Input{
		Path: map[string]any{"ProjectId": "project-1"},
		Body: Payload{Raw: raw, Format: BodyFormatJSONL},
	})
	if err != nil {
		t.Fatalf("BuildRequest(raw): %v", err)
	}
	if string(got.Body) != string(raw) || got.BodyFormat != BodyFormatJSONL {
		t.Fatalf("raw payload = %q format=%q", got.Body, got.BodyFormat)
	}
	got.Body[0] = '!'
	if raw[0] != '{' {
		t.Fatal("BuildRequest retained caller-owned raw body")
	}

	_, err = BuildRequest(op, Input{Body: Payload{JSON: map[string]any{}}})
	if err == nil || !strings.Contains(err.Error(), "path still contains unresolved params") {
		t.Fatalf("unresolved path error = %v", err)
	}
}

func TestBuildRequestDefaultsEmptyMethodToGET(t *testing.T) {
	op := requestTestOperation()
	op.Wire.Method = ""
	op.Wire.Path = "/health"
	got, err := BuildRequest(op, Input{})
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	if got.Method != "GET" {
		t.Fatalf("method = %q, want GET", got.Method)
	}
}

func TestNormalizeAndBuildRequestDistinguishesAbsentAndExplicitBodyValues(t *testing.T) {
	op := requestTestOperation()
	op.Wire.Path = "/body"
	tests := []struct {
		name string
		raw  map[string]any
		want string
	}{
		{name: "absent", raw: map[string]any{}, want: `{}`},
		{name: "null", raw: map[string]any{"body": nil}, want: `null`},
		{name: "object", raw: map[string]any{"body": map[string]any{}}, want: `{}`},
		{name: "scalar", raw: map[string]any{"body": "value"}, want: `"value"`},
		{name: "array", raw: map[string]any{"body": []any{float64(1), "two"}}, want: `[1,"two"]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input, err := NormalizeInput(op, tt.raw)
			if err != nil {
				t.Fatalf("NormalizeInput: %v", err)
			}
			request, err := BuildRequest(op, input)
			if err != nil {
				t.Fatalf("BuildRequest: %v", err)
			}
			if string(request.Body) != tt.want {
				t.Fatalf("body = %q, want %q", request.Body, tt.want)
			}
		})
	}
}

func TestBodyFormatPrecedenceExplicitThenWireThenJSON(t *testing.T) {
	op := requestTestOperation()
	op.Wire.Path = "/body"
	op.Wire.RequestFormat = "jsonl"

	normalized, err := NormalizeInput(op, map[string]any{"body": map[string]any{"Name": "demo"}})
	if err != nil {
		t.Fatalf("NormalizeInput: %v", err)
	}
	if normalized.Body.Format != BodyFormatJSONL {
		t.Fatalf("normalized format = %q, want jsonl from wire", normalized.Body.Format)
	}
	request, err := BuildRequest(op, normalized)
	if err != nil {
		t.Fatalf("BuildRequest(normalized): %v", err)
	}
	if request.BodyFormat != BodyFormatJSONL {
		t.Fatalf("request format = %q, want jsonl from wire", request.BodyFormat)
	}

	request, err = BuildRequest(op, Input{Body: Payload{
		Raw:    []byte(`{"Name":"explicit"}`),
		Format: BodyFormatJSON,
	}})
	if err != nil {
		t.Fatalf("BuildRequest(explicit): %v", err)
	}
	if request.BodyFormat != BodyFormatJSON {
		t.Fatalf("explicit request format = %q, want json", request.BodyFormat)
	}

	op.Wire.RequestFormat = ""
	request, err = BuildRequest(op, Input{})
	if err != nil {
		t.Fatalf("BuildRequest(default): %v", err)
	}
	if request.BodyFormat != BodyFormatJSON {
		t.Fatalf("default request format = %q, want json", request.BodyFormat)
	}
}

func cloneInputForTest(input Input) Input {
	out := Input{
		Path:   cloneAnyMapForTest(input.Path),
		Query:  cloneAnyMapForTest(input.Query),
		Header: cloneAnyMapForTest(input.Header),
		Body: Payload{
			JSON:    input.Body.JSON,
			Raw:     append([]byte(nil), input.Body.Raw...),
			Format:  input.Body.Format,
			Present: input.Body.Present,
		},
	}
	return out
}

func cloneAnyMapForTest(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	out := make(map[string]any, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}
