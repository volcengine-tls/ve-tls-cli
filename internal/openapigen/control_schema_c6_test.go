package main

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
)

func TestC6ControlSchemasRefreshOldCatalogDeterministically(t *testing.T) {
	root, catalogPath, lockPath, _, _ := prepareSupplementalMergeFixture(t, true)
	recovery, usage, docs, err := policyInputPaths(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := contract.Load(raw)
	if err != nil {
		t.Fatal(err)
	}
	oldProps := catalog.ExecutionSchema["properties"].(map[string]any)
	for _, name := range []string{"non_interactive", "allow_write", "timeout"} {
		delete(oldProps, name)
	}
	oldProps["fixture_control"] = map[string]any{"type": "boolean", "description": "existing control"}
	contextProps := catalog.ContextSchema["properties"].(map[string]any)
	trace := contextProps["trace"].(map[string]any)
	trace["runtime_effect"] = "old trace metadata"
	for _, rawForm := range trace["oneOf"].([]any) {
		form := rawForm.(map[string]any)
		if form["type"] == "object" {
			delete(form["properties"].(map[string]any)["redact"].(map[string]any), "enum")
		}
	}
	rawLock, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	var lock operationCatalogLock
	if err := json.Unmarshal(rawLock, &lock); err != nil {
		t.Fatal(err)
	}
	lock.CatalogDigest, err = contract.CatalogV2Digest(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeOperationCatalogPair(catalogPath, catalog, lockPath, lock); err != nil {
		t.Fatal(err)
	}
	if err := refreshContractPoliciesOnly(catalogPath, lockPath, root, recovery, usage, docs); err != nil {
		t.Fatal(err)
	}
	firstCatalog, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	firstLock, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	refreshed, err := contract.Load(firstCatalog)
	if err != nil {
		t.Fatal(err)
	}
	newProps := refreshed.ExecutionSchema["properties"].(map[string]any)
	for name, kind := range map[string]string{"non_interactive": "boolean", "allow_write": "boolean", "timeout": "string", "fixture_control": "boolean"} {
		field, ok := newProps[name].(map[string]any)
		if !ok || field["type"] != kind {
			t.Fatalf("execution.%s=%#v, want type %s", name, newProps[name], kind)
		}
	}
	for _, name := range []string{"projection", "artifact", "page", "page_all", "output", "output_mode", "dry_run"} {
		if !reflect.DeepEqual(newProps[name], oldProps[name]) {
			t.Fatalf("existing execution.%s changed", name)
		}
	}
	for name, terms := range map[string][]string{
		"non_interactive": {"false", "non-TTY"},
		"allow_write":     {"CLI", "IAM", "POST log.search", "global", "false"},
		"timeout":         {"Positive Go duration", "invocation", "default", "profile HTTP", "global"},
	} {
		field := newProps[name].(map[string]any)
		description, _ := field["description"].(string)
		for _, term := range terms {
			if !strings.Contains(description, term) {
				t.Fatalf("execution.%s description=%q missing %q", name, description, term)
			}
		}
	}
	expanded, err := contract.ExpandContextSchema(refreshed.ContextSchema, refreshed.ExecutionSchema)
	if err != nil {
		t.Fatal(err)
	}
	newContextProps := expanded["properties"].(map[string]any)
	contextExecution := newContextProps["execution"].(map[string]any)["properties"].(map[string]any)
	for _, name := range []string{"non_interactive", "allow_write", "timeout", "fixture_control"} {
		if !reflect.DeepEqual(contextExecution[name], newProps[name]) {
			t.Fatalf("context.execution.%s differs from shared execution schema", name)
		}
	}
	newTrace := newContextProps["trace"].(map[string]any)
	effect, _ := newTrace["runtime_effect"].(string)
	for _, term := range []string{"false or enabled:false", "on/true/1/yes/enabled/strict/default", "off", "rejected"} {
		if !strings.Contains(effect, term) {
			t.Fatalf("trace runtime effect=%q missing %q", effect, term)
		}
	}
	found := false
	for _, rawForm := range newTrace["oneOf"].([]any) {
		form := rawForm.(map[string]any)
		if form["type"] != "object" {
			continue
		}
		redact := form["properties"].(map[string]any)["redact"].(map[string]any)
		want := []any{"on", "true", "1", "yes", "enabled", "strict", "default"}
		if !reflect.DeepEqual(redact["enum"], want) {
			t.Fatalf("trace.redact enum=%#v", redact["enum"])
		}
		found = true
	}
	if !found {
		t.Fatal("trace object form missing")
	}
	if err := refreshContractPoliciesOnly(catalogPath, lockPath, root, recovery, usage, docs); err != nil {
		t.Fatal(err)
	}
	assertFilesUnchanged(t, catalogPath, firstCatalog, lockPath, firstLock)
}
