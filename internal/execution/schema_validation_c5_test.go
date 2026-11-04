package execution

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
)

func c5Operation(body any) contract.Operation {
	return contract.Operation{Wire: contract.WireSpec{RequestFormat: "json", Codec: contract.CodecJSON}, InputSchema: contract.JSONSchema{"body": body}}
}

func c5ValidateBody(schema any, body any) error {
	return ValidateInput(c5Operation(schema), Input{Body: Payload{JSON: body, Present: true}})
}

func TestC5NormalizeSectionsAndOpenRouting(t *testing.T) {
	for _, section := range []string{"path", "query", "header"} {
		for _, value := range []any{nil, "secret-canary", []any{}} {
			_, err := NormalizeInput(requestTestOperation(), map[string]any{section: value})
			if err == nil || !strings.Contains(err.Error(), section) || strings.Contains(err.Error(), "secret-canary") {
				t.Fatalf("%s invalid value error = %v", section, err)
			}
		}
	}
	if _, err := NormalizeInput(requestTestOperation(), map[string]any{"body": map[string]any{}, "ignored": "secret-canary"}); err == nil ||
		!strings.Contains(err.Error(), "ignored") || strings.Contains(err.Error(), "secret-canary") {
		t.Fatalf("unknown root error = %v", err)
	}

	for _, tt := range []struct {
		name     string
		sections contract.JSONSchema
		want     string
		wantErr  string
	}{
		{"missing is open", contract.JSONSchema{"body": map[string]any{"type": "object", "properties": map[string]any{"Known": map[string]any{"type": "string"}}}}, "body", ""},
		{"empty is open", contract.JSONSchema{"query": map[string]any{}}, "query", ""},
		{"true is open", contract.JSONSchema{"header": map[string]any{"additionalProperties": true}}, "header", ""},
		{"schema is open", contract.JSONSchema{"query": map[string]any{"additionalProperties": map[string]any{"type": "string"}}}, "query", ""},
		{"false is closed", contract.JSONSchema{"body": map[string]any{"additionalProperties": false}}, "", "unknown"},
		{"many open", contract.JSONSchema{"query": map[string]any{}, "body": map[string]any{"additionalProperties": true}}, "", "ambiguous"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			op := contract.Operation{InputSchema: tt.sections}
			input, err := NormalizeInput(op, map[string]any{"Extra": "value"})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) || (tt.wantErr == "ambiguous" && !strings.Contains(err.Error(), "section")) {
					t.Fatalf("error = %v, want %s", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			sections := map[string]any{"query": input.Query, "header": input.Header, "body": input.Body.JSON}
			if !reflect.DeepEqual(sections[tt.want], map[string]any{"Extra": "value"}) {
				t.Fatalf("routed = %#v", sections)
			}
		})
	}
}

func TestC5DeclaredAndUndeclaredSections(t *testing.T) {
	op := c5Operation(map[string]any{"type": "object"})
	for _, input := range []Input{
		{Query: map[string]any{"Unknown": "secret-canary"}},
		{Path: map[string]any{"Unknown": "secret-canary"}},
		{Header: map[string]any{"Unknown": "secret-canary"}},
	} {
		if err := ValidateInput(op, input); err == nil || strings.Contains(err.Error(), "secret-canary") {
			t.Fatalf("undeclared section error = %v", err)
		}
	}
	if err := ValidateInput(op, Input{Query: map[string]any{}, Body: Payload{JSON: map[string]any{}}}); err != nil {
		t.Fatalf("empty sections: %v", err)
	}
	if err := ValidateInput(contract.Operation{}, Input{Query: map[string]any{"Unknown": true}, Body: Payload{Raw: []byte("not-json")}}); err != nil {
		t.Fatalf("no schema passthrough: %v", err)
	}
	op = contract.Operation{InputSchema: contract.JSONSchema{"query": map[string]any{}}}
	if err := ValidateInput(op, Input{Body: Payload{JSON: map[string]any{"Unknown": "secret-canary"}, Present: true}}); err == nil || strings.Contains(err.Error(), "secret-canary") {
		t.Fatalf("undeclared body error = %v", err)
	}
}

func TestC5ObjectAdditionalPropertiesAndPresence(t *testing.T) {
	schema := map[string]any{"type": "object", "required": []any{"Zero", "False", "Null"}, "properties": map[string]any{
		"Zero": map[string]any{"type": "integer"}, "False": map[string]any{"type": "boolean"}, "Null": map[string]any{"type": "null"},
	}}
	value := map[string]any{"Zero": json.Number("0"), "False": false, "Null": nil, "Unknown": 1}
	if err := c5ValidateBody(schema, value); err != nil {
		t.Fatalf("default open and present falsy values: %v", err)
	}
	schema["additionalProperties"] = false
	if err := c5ValidateBody(schema, value); err == nil || !strings.Contains(err.Error(), "Unknown") {
		t.Fatalf("closed object error = %v", err)
	}
	schema["additionalProperties"] = map[string]any{"type": "string"}
	if err := c5ValidateBody(schema, value); err == nil || !strings.Contains(err.Error(), "Unknown") {
		t.Fatalf("additional schema error = %v", err)
	}
	value["Unknown"] = "ok"
	if err := c5ValidateBody(schema, value); err != nil {
		t.Fatalf("typed additional value: %v", err)
	}
	delete(value, "Zero")
	delete(value, "False")
	if err := c5ValidateBody(schema, value); err == nil || err.Error() != "missing required fields: input.body.False, input.body.Zero" {
		t.Fatalf("required aggregation = %v", err)
	}
}

func TestC5NumericPrecisionAndBounds(t *testing.T) {
	for _, tt := range []struct {
		name   string
		value  any
		schema map[string]any
		valid  bool
	}{
		{"big exact integer", json.Number("9007199254740993"), map[string]any{"type": "integer", "minimum": json.Number("9007199254740993"), "maximum": json.Number("9007199254740993")}, true},
		{"big integer below minimum", json.Number("9007199254740992"), map[string]any{"minimum": json.Number("9007199254740993")}, false},
		{"precise decimal above maximum", json.Number("0.123456789012345678902"), map[string]any{"maximum": json.Number("0.123456789012345678901")}, false},
		{"precise decimal equal", json.Number("0.1234567890123456789010"), map[string]any{"const": json.Number("0.123456789012345678901")}, true},
		{"huge positive exponent", json.Number("1e1000000000"), map[string]any{"minimum": json.Number("9e999999999")}, true},
		{"huge negative exponent", json.Number("1e-1000000000"), map[string]any{"maximum": json.Number("2e-1000000000")}, true},
		{"huge fractional exponent", json.Number("1e-1000000000"), map[string]any{"type": "integer"}, false},
		{"integral exponent", json.Number("1.2300e2"), map[string]any{"type": "integer"}, true},
		{"near integral", json.Number("1.00000000000000000001"), map[string]any{"type": "integer"}, false},
		{"negative ordering", json.Number("-1e1000000000"), map[string]any{"minimum": json.Number("-9e999999999")}, false},
		{"enum numerical equivalence", json.Number("1.00"), map[string]any{"enum": []any{json.Number("1e0")}}, true},
		{"invalid number", json.Number("1e"), map[string]any{"type": "number"}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := c5ValidateBody(tt.schema, tt.value)
			if (err == nil) != tt.valid {
				t.Fatalf("valid=%v, error=%v", tt.valid, err)
			}
		})
	}
}

func TestC5ArrayStringAndCompositions(t *testing.T) {
	schema := map[string]any{"type": "object", "properties": map[string]any{
		"Text":   map[string]any{"type": "string", "minLength": 2, "maxLength": 2, "format": "unrecognized-annotation"},
		"List":   map[string]any{"type": "array", "minItems": 1, "maxItems": 2, "items": map[string]any{"type": "integer"}},
		"Choice": map[string]any{"oneOf": []any{map[string]any{"type": "integer"}, map[string]any{"type": "string", "enum": []any{"ok"}}}},
	}}
	for _, tt := range []struct {
		name string
		body map[string]any
		path string
	}{
		{"valid unicode", map[string]any{"Text": "界🙂", "List": []any{json.Number("0"), json.Number("1e0")}, "Choice": "ok"}, ""},
		{"unicode too long", map[string]any{"Text": "界ab"}, "input.body.Text"},
		{"array empty", map[string]any{"List": []any{}}, "input.body.List"},
		{"array too long", map[string]any{"List": []any{1, 2, 3}}, "input.body.List"},
		{"nested element", map[string]any{"List": []any{1, "secret-canary"}}, "input.body.List[1]"},
		{"oneof no match", map[string]any{"Choice": "secret-canary"}, "input.body.Choice"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := c5ValidateBody(schema, tt.body)
			if tt.path == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.path) || strings.Contains(err.Error(), "secret-canary") {
				t.Fatalf("error = %v", err)
			}
		})
	}
	if err := c5ValidateBody(map[string]any{"oneOf": []any{map[string]any{"type": "number"}, map[string]any{"type": "integer"}}}, 1); err == nil {
		t.Fatal("oneOf matched two schemas")
	}
	if err := c5ValidateBody(map[string]any{"type": "array", "items": map[string]any{"type": "integer"}}, make([]any, 1001)); err == nil { // nil elements violate integer, independent of length
		t.Fatal("invalid items accepted")
	}
	large := make([]any, 1001)
	for i := range large {
		large[i] = 1
	}
	if err := c5ValidateBody(map[string]any{"type": "array", "items": map[string]any{"type": "integer"}}, large); err != nil {
		t.Fatalf("unbounded array: %v", err)
	}
	if err := c5ValidateBody(map[string]any{"type": "string"}, strings.Repeat("界", 4097)); err != nil {
		t.Fatalf("unbounded string: %v", err)
	}
}

func TestC5EmbeddedConditions(t *testing.T) {
	operations := alignmentOperations(t)
	for _, tt := range []struct {
		id   contract.OperationID
		body string
		want string
	}{
		{"index.create", `{"TopicId":"x","FullText":{"IncludeChinese":true}}`, ""},
		{"index.create", `{"TopicId":"x","FullText":{"IncludeChinese":false}}`, "input.body.FullText.Delimiter"},
		{"index.create", `{"TopicId":"x","FullText":{"Delimiter":""}}`, "input.body.FullText.Delimiter"},
		{"log.create-download-task", `{"TopicId":"x","TaskName":"x","TaskType":0,"StartTime":1,"EndTime":2,"Compression":"none","DataFormat":"json"}`, "input.body.Limit"},
		{"log.create-download-task", `{"TopicId":"x","TaskName":"x","TaskType":2,"StartTime":1,"EndTime":2,"Compression":"none","DataFormat":"json","LogContextInfos":{"ContextFlow":"flow"}}`, "input.body.LogContextInfos.Source"},
	} {
		t.Run(fmt.Sprintf("%s/%s", tt.id, tt.want), func(t *testing.T) {
			err := ValidateInput(operations[tt.id], Input{Body: Payload{JSON: alignmentJSON(t, tt.body), Present: true}})
			if tt.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("condition error = %v", err)
			}
		})
	}
}

func TestC5RawWireJSONAndSpecialJSONL(t *testing.T) {
	schema := map[string]any{"type": "object", "required": []any{"N"}, "properties": map[string]any{"N": map[string]any{"type": "integer", "minimum": json.Number("9007199254740993")}}}
	op := c5Operation(schema)
	for _, tt := range []struct {
		raw   string
		valid bool
	}{
		{`{"N":9007199254740993}`, true}, {`{"N":9007199254740992}`, false}, {`{"N":"secret-canary"}`, false}, {`{"N":`, false},
	} {
		err := ValidateInput(op, Input{Body: Payload{Raw: []byte(tt.raw), JSON: map[string]any{"N": json.Number("9007199254740993")}}})
		if (err == nil) != tt.valid || (err != nil && strings.Contains(err.Error(), "secret-canary")) {
			t.Fatalf("raw valid=%v error=%v", tt.valid, err)
		}
	}
	operations := alignmentOperations(t)
	put := operations["log.put"]
	for _, body := range []map[string]any{
		{"LogGroups": []any{map[string]any{"LogTags": map[string]any{"env": "prod"}, "Logs": []any{map[string]any{"Time": json.Number("1710000000000"), "Contents": map[string]any{"message": "ok"}}}}}},
		{"LogGroups": []any{map[string]any{"LogTags": []any{map[string]any{"Key": "env", "Value": "prod"}}, "Logs": []any{map[string]any{"Contents": []any{map[string]any{"Key": "message", "Value": "ok"}}}}}}},
	} {
		if err := ValidateInput(put, Input{Query: map[string]any{"TopicId": "x"}, Body: Payload{JSON: body, Present: true}}); err != nil {
			t.Fatalf("putlogs JSON form: %v", err)
		}
	}
	for _, id := range []contract.OperationID{"log.put", "log.track"} {
		op = operations[id]
		query := map[string]any{"TopicId": "x"}
		if id == "log.track" {
			query["ProjectId"] = "p"
		}
		raw := []byte("{\"Source\":\"browser\",\"Contents\":{\"message\":\"ok\"}}\n{\"Source\":\"browser\",\"Contents\":{\"message\":\"ok2\"}}")
		for _, format := range []BodyFormat{BodyFormatJSONL, " JSONL "} {
			if err := ValidateInput(op, Input{Query: query, Body: Payload{Raw: raw, Format: format}}); err != nil {
				t.Fatalf("%s JSONL format %q: %v", id, format, err)
			}
		}
	}
}
