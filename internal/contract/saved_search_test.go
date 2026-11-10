package contract

import (
	"reflect"
	"strings"
	"testing"
)

func TestSavedSearchOperationsMatchWireAndExecutionContracts(t *testing.T) {
	catalog, err := LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}
	byID := make(map[OperationID]Operation, len(catalog.Operations))
	for _, operation := range catalog.Operations {
		byID[operation.ID] = operation
	}

	wants := map[OperationID]struct {
		action         string
		method         string
		path           string
		section        string
		fields         []string
		risk           string
		recovery       string
		recoverySource string
	}{
		"log.create-saved-search": {
			action: "CreateSavedSearch", method: "POST", path: "/CreateSavedSearch",
			section: "body", fields: []string{"TopicID", "DisplayName", "SearchQuery"},
			risk: "high", recovery: "high-risk-retry", recoverySource: "unverified",
		},
		"log.delete-saved-search": {
			action: "DeleteSavedSearch", method: "DELETE", path: "/DeleteSavedSearch",
			section: "body", fields: []string{"SavedSearchID"},
			risk: "high", recovery: "high-risk-retry", recoverySource: "unverified",
		},
		"log.describe-saved-search": {
			action: "DescribeSavedSearch", method: "GET", path: "/DescribeSavedSearch",
			section: "query", fields: []string{"SavedSearchID"},
			risk: "low", recovery: "safe-retry", recoverySource: "reviewed-read-contract",
		},
		"log.describe-saved-searches": {
			action: "DescribeSavedSearches", method: "GET", path: "/DescribeSavedSearches",
			section: "query", fields: []string{"TopicId"},
			risk: "low", recovery: "safe-retry", recoverySource: "reviewed-read-contract",
		},
	}

	for id, want := range wants {
		operation, ok := byID[id]
		if !ok {
			t.Fatalf("missing saved-search operation %q", id)
		}
		if operation.Group != "log" || operation.Family != "log" || operation.Visibility != "public" {
			t.Fatalf("%s identity = group=%q family=%q visibility=%q, want log/log/public", id, operation.Group, operation.Family, operation.Visibility)
		}
		if operation.Action != want.action {
			t.Fatalf("%s action=%q, want %q", id, operation.Action, want.action)
		}
		if got := operation.Wire.Method + " " + operation.Wire.Path; got != want.method+" "+want.path {
			t.Fatalf("%s route=%q, want %q", id, got, want.method+" "+want.path)
		}
		if operation.Wire.RequestFormat != "json" || operation.Wire.Codec != CodecJSON {
			t.Fatalf("%s wire=%+v, want JSON", id, operation.Wire)
		}
		if !operation.Runtime.SupportsDryRun {
			t.Fatalf("%s supports_dry_run=false, want true", id)
		}
		if operation.Output.Policy != "full" || !operation.Output.IsEnvelopeOutput {
			t.Fatalf("%s output=%+v, want full envelope", id, operation.Output)
		}
		if operation.Risk.Level != want.risk || operation.Risk.ErrorRecovery != want.recovery || operation.Risk.RecoverySource != want.recoverySource {
			t.Fatalf("%s risk=%+v, want level=%q recovery=%q source=%q", id, operation.Risk, want.risk, want.recovery, want.recoverySource)
		}
		if operation.Pagination != nil {
			t.Fatalf("%s pagination=%+v, want nil: saved-search operations have no pagination", id, operation.Pagination)
		}
		if len(operation.InputSchema) != 1 {
			t.Fatalf("%s input schema sections=%#v, want only %s", id, operation.InputSchema, want.section)
		}
		section, ok := operation.InputSchema[want.section].(map[string]any)
		if !ok {
			t.Fatalf("%s %s schema=%#v", id, want.section, operation.InputSchema[want.section])
		}
		properties, ok := section["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s %s properties=%#v", id, want.section, section["properties"])
		}
		if got := savedSearchSchemaRequired(section["required"]); !reflect.DeepEqual(got, want.fields) {
			t.Fatalf("%s %s required=%#v, want %#v", id, want.section, got, want.fields)
		}
		if len(properties) != len(want.fields) {
			t.Fatalf("%s %s properties=%#v, want exact keys %#v", id, want.section, properties, want.fields)
		}
		for _, field := range want.fields {
			if _, ok := properties[field]; !ok {
				t.Fatalf("%s %s missing exact-case property %q; properties=%#v", id, want.section, field, properties)
			}
		}
		if want.section == "query" {
			if _, ok := operation.InputSchema["body"]; ok {
				t.Fatalf("%s unexpectedly exposes a body schema for GET", id)
			}
		} else if _, ok := operation.InputSchema["query"]; ok {
			t.Fatalf("%s unexpectedly exposes a query schema for body request", id)
		}
	}
}

func TestSavedSearchContractsDocumentQuotaIdempotencyAndRetryBoundaries(t *testing.T) {
	catalog, err := LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}
	byID := make(map[OperationID]Operation, len(catalog.Operations))
	for _, operation := range catalog.Operations {
		byID[operation.ID] = operation
	}

	for id, want := range map[OperationID][]string{
		"log.create-saved-search": {
			"scoped to a Topic", "default per-topic limit is 10", "No idempotency token",
			"Do not retry automatically", "SavedSearchAlreadyExist", "log.describe-saved-searches",
		},
		"log.delete-saved-search": {
			"shared Topic resource", "repeated delete", "SavedSearchNotExist", "Do not retry automatically",
			"log.describe-saved-search",
		},
		"log.describe-saved-search": {
			"shared by its Topic", "no pagination", "--page-all is not supported",
		},
		"log.describe-saved-searches": {
			"shared by the Topic", "no pagination or page-size parameter", "default per-topic limit is 10",
		},
	} {
		operation, ok := byID[id]
		if !ok {
			t.Fatalf("missing saved-search operation %q", id)
		}
		for _, fragment := range want {
			if !strings.Contains(operation.Docs.UsageConstraints, fragment) {
				t.Fatalf("%s usage_constraints missing %q: %q", id, fragment, operation.Docs.UsageConstraints)
			}
		}
	}
}

func savedSearchSchemaRequired(raw any) []string {
	values, ok := raw.([]any)
	if !ok {
		return nil
	}
	got := make([]string, 0, len(values))
	for _, value := range values {
		text, ok := value.(string)
		if !ok {
			return nil
		}
		got = append(got, text)
	}
	return got
}
