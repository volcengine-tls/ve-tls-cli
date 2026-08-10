package contract

import (
	"strings"
	"testing"
)

func TestLoadRejectsMalformedAndInvalidCatalogs(t *testing.T) {
	if _, err := Load([]byte(`{`)); err == nil || !strings.Contains(err.Error(), "decode operation catalog") {
		t.Fatalf("malformed catalog error=%v", err)
	}
	if _, err := Load([]byte(`{"schema_version":"v2"}`)); err == nil {
		t.Fatal("incomplete catalog unexpectedly loaded")
	}
}

func TestNewCatalogRejectsInvalidCatalogSchemas(t *testing.T) {
	operation := validTestOperation()
	tests := []struct {
		name      string
		context   JSONSchema
		execution JSONSchema
		want      string
	}{
		{
			name:      "missing execution property",
			context:   JSONSchema{"type": "object"},
			execution: JSONSchema{"type": "object"},
			want:      "missing execution property",
		},
		{
			name: "execution property is not object",
			context: JSONSchema{"type": "object", "properties": map[string]any{
				"execution": "invalid",
			}},
			execution: JSONSchema{"type": "object"},
			want:      "not an object",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewCatalog("v1", tt.context, tt.execution, []Operation{operation}); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("NewCatalog error=%v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestNewCatalogPreservesMissingInputSchemaForValidation(t *testing.T) {
	operation := validTestOperation()
	operation.InputSchema = nil
	context := JSONSchema{"type": "object", "properties": map[string]any{
		"execution": map[string]any{"type": "object"},
	}}
	if _, err := NewCatalog("v1", context, JSONSchema{"type": "object"}, []Operation{operation}); err == nil || !strings.Contains(err.Error(), "input_schema is required") {
		t.Fatalf("NewCatalog missing input schema error=%v", err)
	}
}

func TestExpandContextSchemaRejectsInvalidReferences(t *testing.T) {
	tests := []struct {
		name    string
		context JSONSchema
		want    string
	}{
		{name: "non-string", context: JSONSchema{"$ref": 42}, want: "schema ref has type"},
		{name: "missing", context: JSONSchema{"$ref": "#/missing"}, want: "unresolved schema ref"},
		{name: "cycle", context: JSONSchema{"$ref": "#/context_schema"}, want: "schema ref cycle"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ExpandContextSchema(tt.context, JSONSchema{"type": "object"}); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ExpandContextSchema error=%v, want substring %q", err, tt.want)
			}
		})
	}
}
