package contract

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestNewCatalogClosesOnlyDeclaredTransportSectionRoots(t *testing.T) {
	base, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	context, err := ExpandContextSchema(base.ContextSchema, base.ExecutionSchema)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name      string
		section   map[string]any
		wantExtra any
		wantField bool
	}{
		{"implicit", map[string]any{"type": "object", "properties": map[string]any{"nested": map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}}}}}, false, true},
		{"explicit false", map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}, false, true},
		{"explicit true", map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": true}, true, true},
		{"explicit schema", map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": map[string]any{"type": "string"}}, map[string]any{"type": "string"}, true},
		{"unspecified fields", map[string]any{"type": "object"}, nil, false},
	}
	for _, sectionName := range []string{"path", "query", "header", "body"} {
		for _, tc := range cases {
			t.Run(sectionName+"/"+tc.name, func(t *testing.T) {
				operation := base.Operations[0]
				operation.InputSchema = JSONSchema{sectionName: tc.section}
				original := cloneSchema(operation.InputSchema)
				catalog, err := NewCatalog(base.ContractVersion, context, base.ExecutionSchema, []Operation{operation})
				if err != nil {
					t.Fatal(err)
				}
				got := catalog.Operations[0].InputSchema[sectionName].(map[string]any)
				value, exists := got["additionalProperties"]
				if exists != tc.wantField || !reflect.DeepEqual(value, tc.wantExtra) {
					t.Fatalf("additionalProperties=(%#v,%v), want (%#v,%v)", value, exists, tc.wantExtra, tc.wantField)
				}
				if !reflect.DeepEqual(operation.InputSchema, original) {
					t.Fatal("NewCatalog mutated its input")
				}
				if tc.name == "implicit" {
					nested := got["properties"].(map[string]any)["nested"].(map[string]any)
					if _, exists := nested["additionalProperties"]; exists {
						t.Fatal("nested object was unexpectedly closed")
					}
				}
			})
		}
	}
}

func TestLoadDoesNotNormalizeTransportSections(t *testing.T) {
	catalog, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	catalog.Operations = catalog.Operations[:1]
	catalog.Operations[0].InputSchema = JSONSchema{"query": map[string]any{
		"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}},
	}}
	raw, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(raw)
	if err != nil {
		t.Fatal(err)
	}
	section := loaded.Operations[0].InputSchema["query"].(map[string]any)
	if _, exists := section["additionalProperties"]; exists {
		t.Fatal("Load unexpectedly normalized the catalog")
	}
}

func TestLegacyDigestIncludesRecoveryAndDocumentationProvenance(t *testing.T) {
	catalog, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	operation := catalog.Operations[0]
	legacy, err := RebuildLegacyToolV1(catalog, operation)
	if err != nil {
		t.Fatal(err)
	}
	baseline := LegacyToolDigestV1(legacy)
	operation.Risk.RecoverySource = "unverified"
	operation.Risk.RecoveryNotes = "reconcile first"
	operation.Docs.URL = "https://www.volcengine.com/docs/6470/123"
	operation.Docs.URLSource = "official-doc-index"
	projected, err := RebuildLegacyToolV1(catalog, operation)
	if err != nil {
		t.Fatal(err)
	}
	if projected.RecoverySource != operation.Risk.RecoverySource || projected.RecoveryNotes != operation.Risk.RecoveryNotes ||
		projected.DocURL != operation.Docs.URL || projected.DocURLSource != operation.Docs.URLSource {
		t.Fatalf("projection lost provenance: %#v", projected)
	}
	if LegacyToolDigestV1(projected) == baseline {
		t.Fatal("provenance did not affect legacy digest")
	}
}
