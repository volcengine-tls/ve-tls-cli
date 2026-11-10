package execution

import (
	"reflect"
	"strings"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
)

func TestNormalizeInputSupportsFlatAndSectionedForms(t *testing.T) {
	op := requestTestOperation()

	flat := map[string]any{
		"ProjectId": "project-1",
		"Limit":     float64(20),
		"X-Test":    true,
		"Name":      "demo",
	}
	got, err := NormalizeInput(op, flat)
	if err != nil {
		t.Fatalf("NormalizeInput(flat): %v", err)
	}
	want := Input{
		Path:   map[string]any{"ProjectId": "project-1"},
		Query:  map[string]any{"Limit": float64(20)},
		Header: map[string]any{"X-Test": true},
		Body:   Payload{JSON: map[string]any{"Name": "demo"}, Format: BodyFormatJSON, Present: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("flat input = %#v, want %#v", got, want)
	}
	if !reflect.DeepEqual(flat, map[string]any{
		"ProjectId": "project-1",
		"Limit":     float64(20),
		"X-Test":    true,
		"Name":      "demo",
	}) {
		t.Fatalf("NormalizeInput mutated caller map: %#v", flat)
	}

	sectioned := map[string]any{
		"path":  map[string]any{"ProjectId": "project-2"},
		"query": map[string]any{"Limit": float64(10), "UnknownQuery": "kept"},
		"body":  map[string]any{"Name": "sectioned", "UnknownBody": true},
	}
	got, err = NormalizeInput(op, sectioned)
	if err != nil {
		t.Fatalf("NormalizeInput(sectioned): %v", err)
	}
	if got.Path["ProjectId"] != "project-2" ||
		got.Query["UnknownQuery"] != "kept" ||
		got.Body.JSON.(map[string]any)["UnknownBody"] != true {
		t.Fatalf("sectioned input not preserved: %#v", got)
	}
}

func TestNormalizeInputRejectsReservedUnknownAndAmbiguousFlatFields(t *testing.T) {
	op := requestTestOperation()

	if _, err := NormalizeInput(op, map[string]any{"profile": "prod"}); err == nil ||
		!strings.Contains(err.Error(), "reserved context/runtime fields: profile") {
		t.Fatalf("reserved field error = %v", err)
	}
	for _, section := range inputSections {
		schema := op.InputSchema[section].(map[string]any)
		schema["additionalProperties"] = false
	}
	if _, err := NormalizeInput(op, map[string]any{"Unknown": "value"}); err == nil ||
		err.Error() != "flat input contains unknown fields: Unknown" {
		t.Fatalf("unknown field error = %v", err)
	}

	op.InputSchema["query"].(map[string]any)["properties"].(map[string]any)["Name"] =
		map[string]any{"type": "string"}
	if _, err := NormalizeInput(op, map[string]any{"Name": "value"}); err == nil ||
		!strings.Contains(err.Error(), "flat input has ambiguous fields: Name(query,body)") {
		t.Fatalf("ambiguous field error = %v", err)
	}
}

func TestNormalizeInputRejectsClosedBodyUnknownEvenWhenOnlyBodyDeclared(t *testing.T) {
	op := requestTestOperation()
	body := op.InputSchema["body"].(map[string]any)
	body["additionalProperties"] = false
	op.InputSchema = contract.JSONSchema{"body": body}
	if _, err := NormalizeInput(op, map[string]any{"Loose": "secret-canary"}); err == nil ||
		!strings.Contains(err.Error(), "unknown fields: Loose") || strings.Contains(err.Error(), "secret-canary") {
		t.Fatalf("closed body error = %v", err)
	}
}

func TestNormalizeInputWithoutSchemaKeepsFlatObjectAsBody(t *testing.T) {
	op := requestTestOperation()
	op.InputSchema = nil
	raw := map[string]any{"Arbitrary": "value"}
	got, err := NormalizeInput(op, raw)
	if err != nil {
		t.Fatalf("NormalizeInput: %v", err)
	}
	if !reflect.DeepEqual(got.Body.JSON, raw) {
		t.Fatalf("body = %#v, want %#v", got.Body.JSON, raw)
	}
}

func TestValidateInputAggregatesRequiredFieldsAndChecksTypes(t *testing.T) {
	op := requestTestOperation()
	body := op.InputSchema["body"].(map[string]any)
	body["required"] = []any{"Name", "Nested"}
	body["properties"].(map[string]any)["Nested"] = map[string]any{
		"type":       "object",
		"required":   []any{"Value"},
		"properties": map[string]any{"Value": map[string]any{"type": "integer"}},
	}

	err := ValidateInput(op, Input{Body: Payload{
		JSON:   map[string]any{"Nested": map[string]any{}},
		Format: BodyFormatJSON,
	}})
	if err == nil || err.Error() != "missing required fields: input.body.Name, input.body.Nested.Value, input.path.ProjectId" {
		t.Fatalf("required error = %v", err)
	}

	err = ValidateInput(op, Input{
		Path:  map[string]any{"ProjectId": "project-1"},
		Query: map[string]any{"Limit": "not-a-number"},
		Body: Payload{JSON: map[string]any{
			"Name":   "demo",
			"Nested": map[string]any{"Value": float64(1.5)},
		}, Format: BodyFormatJSON},
	})
	if err == nil || !strings.Contains(err.Error(), "input.query.Limit") {
		t.Fatalf("type validation error = %v", err)
	}
}

func TestSectionedNonObjectAndNilRequiredValueAreRejected(t *testing.T) {
	op := requestTestOperation()
	if _, err := NormalizeInput(op, map[string]any{"path": "secret-canary"}); err == nil ||
		!strings.Contains(err.Error(), "path") || strings.Contains(err.Error(), "secret-canary") {
		t.Fatalf("non-object path error = %v", err)
	}
	got, err := NormalizeInput(op, map[string]any{
		"path": map[string]any{"ProjectId": nil},
	})
	if err != nil {
		t.Fatalf("NormalizeInput: %v", err)
	}
	if err := ValidateInput(op, got); err == nil || !strings.Contains(err.Error(), "input.path.ProjectId") ||
		strings.Contains(err.Error(), "missing required") {
		t.Fatalf("null required field error = %v", err)
	}
}

func TestNormalizeInputPreservesSectionedBodyPresence(t *testing.T) {
	op := requestTestOperation()
	tests := []struct {
		name    string
		raw     map[string]any
		present bool
		want    any
	}{
		{name: "absent", raw: map[string]any{}, present: false, want: nil},
		{name: "null", raw: map[string]any{"body": nil}, present: true, want: nil},
		{name: "object", raw: map[string]any{"body": map[string]any{}}, present: true, want: map[string]any{}},
		{name: "scalar", raw: map[string]any{"body": "value"}, present: true, want: "value"},
		{name: "array", raw: map[string]any{"body": []any{float64(1), "two"}}, present: true, want: []any{float64(1), "two"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeInput(op, tt.raw)
			if err != nil {
				t.Fatalf("NormalizeInput: %v", err)
			}
			if got.Body.Present != tt.present || !reflect.DeepEqual(got.Body.JSON, tt.want) {
				t.Fatalf("body = %#v, want present=%v JSON=%#v", got.Body, tt.present, tt.want)
			}
		})
	}
}

func requestTestOperation() contract.Operation {
	return contract.Operation{
		ID: "project.describe",
		Wire: contract.WireSpec{
			Method:        "POST",
			Path:          "/projects/{ProjectId}",
			RequestFormat: "json",
			Codec:         contract.CodecJSON,
		},
		InputSchema: contract.JSONSchema{
			"path": map[string]any{
				"type":       "object",
				"required":   []any{"ProjectId"},
				"properties": map[string]any{"ProjectId": map[string]any{"type": "string"}},
			},
			"query": map[string]any{
				"type":       "object",
				"properties": map[string]any{"Limit": map[string]any{"type": "integer"}},
			},
			"header": map[string]any{
				"type":       "object",
				"properties": map[string]any{"X-Test": map[string]any{"type": "boolean"}},
			},
			"body": map[string]any{
				"type":       "object",
				"properties": map[string]any{"Name": map[string]any{"type": "string"}},
			},
		},
	}
}
