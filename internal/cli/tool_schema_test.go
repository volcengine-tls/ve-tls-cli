package cli

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
)

func TestToolSchemaReturnsSharedCatalogSchemas(t *testing.T) {
	code, schema := outcomeCLIRun(t, []string{"--profile", "missing-schema-test-profile", "tool", "schema"})
	if code != 0 {
		t.Fatalf("tool schema exit=%d result=%#v", code, schema)
	}
	catalog, err := contract.LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	contextSchema := c5Object(t, schema["context_schema"], "context_schema")
	executionSchema := c5Object(t, schema["execution_schema"], "execution_schema")
	if schema["schema_version"] != catalog.SchemaVersion || schema["contract_version"] != catalog.ContractVersion ||
		!reflect.DeepEqual(contextSchema, map[string]any(catalog.ContextSchema)) ||
		!reflect.DeepEqual(executionSchema, map[string]any(catalog.ExecutionSchema)) {
		t.Fatal("tool schema differs from the embedded shared contract")
	}
	if _, err := contract.ExpandContextSchema(contextSchema, executionSchema); err != nil {
		t.Fatalf("tool schema returned an unresolvable context schema: %v", err)
	}
	digest, _ := schema["schema_digest"].(string)
	if raw, err := hex.DecodeString(digest); err != nil || len(raw) != 32 {
		t.Fatalf("invalid schema_digest %q", digest)
	}
	if _, exists := schema["operations"]; exists {
		t.Fatal("tool schema includes operation inventory")
	}
}

func TestToolDescribeInputPreservesRequestContractAndCapabilities(t *testing.T) {
	code, shared := outcomeCLIRun(t, []string{"tool", "schema"})
	if code != 0 {
		t.Fatalf("tool schema exit=%d result=%#v", code, shared)
	}
	for _, tc := range []struct {
		id            string
		supportsAll   bool
		requiresWrite bool
	}{
		{"project.describe-projects", true, false},
		{"project.create", false, true},
		{"log.search", false, false},
	} {
		t.Run(tc.id, func(t *testing.T) {
			input := c5Describe(t, tc.id, "input")
			full := c5Describe(t, tc.id, "full")
			for _, field := range []string{"input_schema", "input_policy", "input_encoding_hint", "usage_constraints", "contract_digest", "recovery"} {
				if !reflect.DeepEqual(input[field], full[field]) {
					t.Fatalf("input view changed %s", field)
				}
			}
			for _, field := range []string{"context", "execution", "context_schema", "execution_schema"} {
				if _, exists := input[field]; exists {
					t.Fatalf("input view inlines %s", field)
				}
			}
			ref := c5Object(t, input["schema_ref"], "schema_ref")
			if ref["command"] != "volclog tool schema" || ref["schema_digest"] != shared["schema_digest"] {
				t.Fatalf("schema_ref=%#v does not identify the shared contract", ref)
			}
			behavior := c5Object(t, input["behavior"], "behavior")
			fullBehavior := c5Object(t, full["behavior"], "full behavior")
			if behavior["supports_all"] != tc.supportsAll || behavior["requires_write_permission"] != tc.requiresWrite ||
				behavior["supports_dry_run"] != fullBehavior["supports_dry_run"] {
				t.Fatalf("incorrect operation capabilities: %#v", behavior)
			}
			if tc.supportsAll {
				inputJSON, _ := json.Marshal(input)
				compactJSON, _ := json.Marshal(c5Describe(t, tc.id, "compact"))
				if len(inputJSON) >= len(compactJSON) {
					t.Fatalf("input view is %d bytes, compact is %d", len(inputJSON), len(compactJSON))
				}
			}
		})
	}
}

func TestToolSchemaHelpAndExtraArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"tool", "schema", "--help"}, &stdout, &stderr)
	if code != 0 || stderr.Len() != 0 || !strings.Contains(stdout.String(), "volclog tool schema") {
		t.Fatalf("help exit=%d stdout=%q stderr=%q", code, &stdout, &stderr)
	}
	code, result := outcomeCLIRun(t, []string{"tool", "schema", "project.create"})
	failure := c5Object(t, result["error"], "error")
	hint, _ := failure["hint"].(string)
	if code != 1 || failure["kind"] != "usage" || !strings.Contains(hint, "tool schema --help") {
		t.Fatalf("unexpected argument exit=%d result=%#v", code, result)
	}
}
