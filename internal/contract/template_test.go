package contract_test

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
	"github.com/volcengine-tls/ve-tls-cli/internal/execution"
)

func TestRequestTemplateRequiredRecursesWithoutPromotingOptionalFields(t *testing.T) {
	operation := templateTestOperation(contract.JSONSchema{
		"type":     "object",
		"required": []any{"Name", "Nested", "Items"},
		"properties": map[string]any{
			"Name": map[string]any{
				"type":    "string",
				"example": "example-name",
			},
			"Nested": map[string]any{
				"type":     "object",
				"required": []any{"Token"},
				"properties": map[string]any{
					"Token": map[string]any{
						"type":    "string",
						"default": "default-token",
					},
					"OptionalNested": map[string]any{"type": "boolean"},
				},
			},
			"Items": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":     "object",
					"required": []any{"ItemID"},
					"properties": map[string]any{
						"ItemID":       map[string]any{"type": "string", "minLength": float64(3)},
						"OptionalItem": map[string]any{"type": "integer"},
					},
				},
			},
			"OptionalTop": map[string]any{
				"type":     "object",
				"required": []any{"Hidden"},
				"properties": map[string]any{
					"Hidden": map[string]any{"type": "string"},
				},
			},
		},
	})

	got, err := contract.RequestTemplate(operation, contract.TemplateRequired)
	if err != nil {
		t.Fatalf("RequestTemplate(required): %v", err)
	}
	want := map[string]any{
		"Name": "example-name",
		"Nested": map[string]any{
			"Token": "default-token",
		},
		"Items": []any{
			map[string]any{"ItemID": "xxx"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("required template = %#v, want %#v", got, want)
	}
	assertJSONValid(t, got)
	assertPassesRequiredValidation(t, operation, got)
}

func TestRequestTemplateFullIncludesEveryPropertyWithDeterministicValues(t *testing.T) {
	operation := templateTestOperation(contract.JSONSchema{
		"type":     "object",
		"required": []string{"FromExample"},
		"properties": map[string]any{
			"FromExample": map[string]any{
				"type":    "string",
				"example": "example-value",
				"default": "ignored-default",
				"enum":    []any{"ignored-enum", "example-value"},
			},
			"FromDefault": map[string]any{
				"type":    "integer",
				"default": float64(7),
				"enum":    []any{float64(8), float64(7)},
			},
			"FromEnum": map[string]any{
				"type": "string",
				"enum": []any{"first", "second"},
			},
			"Integer": map[string]any{
				"type":    "integer",
				"minimum": float64(3),
			},
			"NegativeInteger": map[string]any{
				"type":    "integer",
				"maximum": float64(-2),
			},
			"Number": map[string]any{
				"type":    "number",
				"minimum": float64(1.5),
			},
			"String": map[string]any{
				"type":      "string",
				"minLength": float64(3),
			},
			"Boolean": map[string]any{"type": "boolean"},
			"Nested": map[string]any{
				"type":     "object",
				"required": []any{"RequiredChild"},
				"properties": map[string]any{
					"RequiredChild": map[string]any{"type": "string"},
					"OptionalChild": map[string]any{"type": "boolean"},
				},
			},
			"Items": map[string]any{
				"type":     "array",
				"minItems": float64(2),
				"items":    map[string]any{"type": "boolean"},
			},
			"Choice": map[string]any{
				"oneOf": []any{
					map[string]any{
						"type":     "object",
						"required": []any{"Selected"},
						"properties": map[string]any{
							"Selected": map[string]any{"type": "string"},
							"AlsoFull": map[string]any{"type": "integer"},
						},
					},
					map[string]any{"type": "string"},
				},
			},
		},
	})

	first, err := contract.RequestTemplate(operation, contract.TemplateFull)
	if err != nil {
		t.Fatalf("RequestTemplate(full): %v", err)
	}
	second, err := contract.RequestTemplate(operation, contract.TemplateFull)
	if err != nil {
		t.Fatalf("RequestTemplate(full) second call: %v", err)
	}
	want := map[string]any{
		"FromExample":     "example-value",
		"FromDefault":     float64(7),
		"FromEnum":        "first",
		"Integer":         float64(3),
		"NegativeInteger": float64(-2),
		"Number":          float64(1.5),
		"String":          "xxx",
		"Boolean":         false,
		"Nested": map[string]any{
			"RequiredChild": "",
			"OptionalChild": false,
		},
		"Items": []any{false, false},
		"Choice": map[string]any{
			"Selected": "",
			"AlsoFull": float64(0),
		},
	}
	if !reflect.DeepEqual(first, want) {
		t.Fatalf("full template = %#v, want %#v", first, want)
	}
	if !reflect.DeepEqual(second, first) {
		t.Fatalf("full template is nondeterministic: first=%#v second=%#v", first, second)
	}
	assertJSONValid(t, first)
	assertPassesRequiredValidation(t, operation, first)
}

func TestRequestTemplateRejectsInvalidModeAndUnknownRequiredProperties(t *testing.T) {
	valid := templateTestOperation(contract.JSONSchema{
		"type":       "object",
		"properties": map[string]any{},
	})
	if _, err := contract.RequestTemplate(valid, contract.TemplateMode("compact")); err == nil ||
		!strings.Contains(err.Error(), `unsupported request template mode "compact"`) {
		t.Fatalf("invalid mode error = %v", err)
	}

	tests := []struct {
		name   string
		schema contract.JSONSchema
		want   string
	}{
		{
			name: "top-level required property",
			schema: contract.JSONSchema{
				"type":       "object",
				"required":   []any{"Missing"},
				"properties": map[string]any{},
			},
			want: `input_schema.body required field "Missing" is absent from properties`,
		},
		{
			name: "nested required property",
			schema: contract.JSONSchema{
				"type":     "object",
				"required": []any{"Nested"},
				"properties": map[string]any{
					"Nested": map[string]any{
						"type":       "object",
						"required":   []any{"Missing"},
						"properties": map[string]any{},
					},
				},
			},
			want: `input_schema.body.properties.Nested required field "Missing" is absent from properties`,
		},
		{
			name: "optional nested schema still validated",
			schema: contract.JSONSchema{
				"type": "object",
				"properties": map[string]any{
					"Optional": map[string]any{
						"type":       "object",
						"required":   []any{"Missing"},
						"properties": map[string]any{},
					},
				},
			},
			want: `input_schema.body.properties.Optional required field "Missing" is absent from properties`,
		},
		{
			name: "malformed required",
			schema: contract.JSONSchema{
				"type":       "object",
				"required":   "Name",
				"properties": map[string]any{"Name": map[string]any{"type": "string"}},
			},
			want: "input_schema.body required must be an array of strings",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := contract.RequestTemplate(templateTestOperation(tt.schema), contract.TemplateRequired)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestRequestTemplateWithoutBodyReturnsEmptyObject(t *testing.T) {
	operation := contract.Operation{
		ID: "project.describe",
		InputSchema: contract.JSONSchema{
			"query": map[string]any{
				"type":       "object",
				"properties": map[string]any{"ProjectId": map[string]any{"type": "string"}},
			},
		},
	}
	got, err := contract.RequestTemplate(operation, contract.TemplateRequired)
	if err != nil {
		t.Fatalf("RequestTemplate(required): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("template = %#v, want empty object", got)
	}
	assertJSONValid(t, got)
}

func TestRequestTemplateSupportsJSONSchemaVariants(t *testing.T) {
	operation := templateTestOperation(contract.JSONSchema{
		"type": "object",
		"properties": map[string]any{
			"EnumStrings": contract.JSONSchema{
				"type": "string",
				"enum": []string{"first", "second"},
			},
			"InferredObject": contract.JSONSchema{
				"properties": map[string]any{
					"Value": map[string]any{},
				},
			},
			"InferredArray": map[string]any{
				"items": contract.JSONSchema{
					"type":    "integer",
					"minimum": 2,
				},
			},
			"ArrayWithoutItems": map[string]any{"type": "array"},
			"NegativeNumber": map[string]any{
				"type":    "number",
				"maximum": float64(-1.5),
			},
			"ZeroNumber": map[string]any{"type": "number"},
			"Null":       map[string]any{"type": "null"},
			"Untyped":    map[string]any{},
		},
	})

	got, err := contract.RequestTemplate(operation, contract.TemplateFull)
	if err != nil {
		t.Fatalf("RequestTemplate(full): %v", err)
	}
	want := map[string]any{
		"EnumStrings":       "first",
		"InferredObject":    map[string]any{"Value": ""},
		"InferredArray":     []any{float64(2)},
		"ArrayWithoutItems": []any{},
		"NegativeNumber":    float64(-1.5),
		"ZeroNumber":        float64(0),
		"Null":              nil,
		"Untyped":           "",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("template = %#v, want %#v", got, want)
	}
	assertJSONValid(t, got)
}

func TestRequestTemplateProjectsCompositeExamplesThroughTheirSchemas(t *testing.T) {
	operation := templateTestOperation(contract.JSONSchema{
		"type": "object",
		"properties": map[string]any{
			"Object": map[string]any{
				"type":     "object",
				"example":  map[string]any{"Required": "from-example", "Unknown": true},
				"required": []any{"Required"},
				"properties": map[string]any{
					"Required": map[string]any{"type": "string"},
					"Optional": map[string]any{"type": "boolean"},
				},
			},
			"Array": map[string]any{
				"type": "array",
				"default": []any{
					map[string]any{"Optional": true, "Unknown": true},
				},
				"items": map[string]any{
					"type":     "object",
					"required": []any{"Required"},
					"properties": map[string]any{
						"Required": map[string]any{"type": "string", "minLength": float64(2)},
						"Optional": map[string]any{"type": "boolean"},
					},
				},
			},
		},
	})

	got, err := contract.RequestTemplate(operation, contract.TemplateFull)
	if err != nil {
		t.Fatalf("RequestTemplate(full): %v", err)
	}
	want := map[string]any{
		"Object": map[string]any{
			"Required": "from-example",
			"Optional": false,
		},
		"Array": []any{
			map[string]any{
				"Required": "xx",
				"Optional": true,
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("template = %#v, want %#v", got, want)
	}
	assertJSONValid(t, got)
	assertPassesRequiredValidation(t, operation, got)
}

func TestRequestTemplateRejectsMalformedSchemaNodes(t *testing.T) {
	tests := []struct {
		name      string
		operation contract.Operation
		want      string
	}{
		{
			name: "body is not schema",
			operation: contract.Operation{
				InputSchema: contract.JSONSchema{"body": "invalid"},
			},
			want: "input_schema.body must be an object schema",
		},
		{
			name: "body is not object",
			operation: templateTestOperation(contract.JSONSchema{
				"type": "array",
			}),
			want: "input_schema.body type must be object",
		},
		{
			name: "properties is not object",
			operation: templateTestOperation(contract.JSONSchema{
				"type":       "object",
				"properties": "invalid",
			}),
			want: "input_schema.body properties must be an object",
		},
		{
			name: "property is not schema",
			operation: templateTestOperation(contract.JSONSchema{
				"type":       "object",
				"properties": map[string]any{"Value": "invalid"},
			}),
			want: "input_schema.body.Value must be a schema object",
		},
		{
			name: "array items is not schema",
			operation: templateTestOperation(contract.JSONSchema{
				"type": "object",
				"properties": map[string]any{
					"Values": map[string]any{"type": "array", "items": "invalid"},
				},
			}),
			want: "input_schema.body.Values items must be a schema object",
		},
		{
			name: "oneOf choice is not schema",
			operation: templateTestOperation(contract.JSONSchema{
				"type": "object",
				"properties": map[string]any{
					"Value": map[string]any{"oneOf": []any{"invalid"}},
				},
			}),
			want: "input_schema.body.Value oneOf[0] must be a schema object",
		},
		{
			name: "unsupported type",
			operation: templateTestOperation(contract.JSONSchema{
				"type": "object",
				"properties": map[string]any{
					"Value": map[string]any{"type": "function"},
				},
			}),
			want: `input_schema.body.Value has unsupported schema type "function"`,
		},
		{
			name: "non-finite string minimum",
			operation: templateTestOperation(contract.JSONSchema{
				"type": "object",
				"properties": map[string]any{
					"Value": map[string]any{"type": "string", "minLength": math.Inf(1)},
				},
			}),
			want: "input_schema.body.Value string minimum length must be a finite non-negative integer",
		},
		{
			name: "negative array minimum",
			operation: templateTestOperation(contract.JSONSchema{
				"type": "object",
				"properties": map[string]any{
					"Value": map[string]any{
						"type":     "array",
						"minItems": float64(-1),
						"items":    map[string]any{"type": "string"},
					},
				},
			}),
			want: "input_schema.body.Value array minimum length must be a finite non-negative integer",
		},
		{
			name: "array minimum exceeds safety limit",
			operation: templateTestOperation(contract.JSONSchema{
				"type": "object",
				"properties": map[string]any{
					"Value": map[string]any{
						"type":     "array",
						"minItems": float64(1025),
						"items":    map[string]any{"type": "string"},
					},
				},
			}),
			want: "input_schema.body.Value array minimum length 1025 exceeds safety limit 1024",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := contract.RequestTemplate(tt.operation, contract.TemplateFull)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestRequestTemplatesCoverEmbeddedOperationBodies(t *testing.T) {
	catalog, err := contract.LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}
	for _, operation := range catalog.Operations {
		for _, mode := range []contract.TemplateMode{contract.TemplateRequired, contract.TemplateFull} {
			t.Run(string(operation.ID)+"/"+string(mode), func(t *testing.T) {
				template, err := contract.RequestTemplate(operation, mode)
				if err != nil {
					t.Fatalf("RequestTemplate: %v", err)
				}
				assertJSONValid(t, template)

				bodyOnly := operation
				bodyOnly.InputSchema = contract.JSONSchema{}
				if body, ok := operation.InputSchema["body"]; ok {
					bodyOnly.InputSchema["body"] = body
				}
				assertPassesRequiredValidation(t, bodyOnly, template)
			})
		}
	}
}

func TestRequestTemplateRejectsInvalidPreferredValues(t *testing.T) {
	tests := []struct {
		name   string
		schema map[string]any
		want   string
	}{
		{
			name: "wrong example type",
			schema: map[string]any{
				"type":    "integer",
				"example": "not-a-number",
			},
			want: "must be an integer",
		},
		{
			name: "default below minimum",
			schema: map[string]any{
				"type":    "number",
				"default": float64(1),
				"minimum": float64(2),
			},
			want: "must be at least 2",
		},
		{
			name: "example exceeds max length",
			schema: map[string]any{
				"type":      "string",
				"example":   "too-long",
				"maxLength": float64(3),
			},
			want: "length must be at most 3",
		},
		{
			name: "array default above max items",
			schema: map[string]any{
				"type":     "array",
				"default":  []any{"one", "two"},
				"maxItems": float64(1),
				"items":    map[string]any{"type": "string"},
			},
			want: "length must be at most 1",
		},
		{
			name: "array default without items exceeds safety limit",
			schema: map[string]any{
				"type":    "array",
				"default": make([]any, 1025),
			},
			want: "template array length 1025 exceeds safety limit 1024",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			operation := templateTestOperation(contract.JSONSchema{
				"type": "object",
				"properties": map[string]any{
					"Value": tt.schema,
				},
			})
			_, err := contract.RequestTemplate(operation, contract.TemplateFull)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func templateTestOperation(body contract.JSONSchema) contract.Operation {
	return contract.Operation{
		ID: "test.create",
		InputSchema: contract.JSONSchema{
			"body": map[string]any(body),
		},
	}
}

func assertJSONValid(t *testing.T, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal(template): %v", err)
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(template): %v", err)
	}
}

func assertPassesRequiredValidation(t *testing.T, operation contract.Operation, body map[string]any) {
	t.Helper()
	err := execution.ValidateInput(operation, execution.Input{
		Body: execution.Payload{
			JSON:    body,
			Format:  execution.BodyFormatJSON,
			Present: true,
		},
	})
	if err != nil {
		t.Fatalf("execution.ValidateInput(template): %v", err)
	}
}
