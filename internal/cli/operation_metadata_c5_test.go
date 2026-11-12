package cli

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
)

func c5Describe(t *testing.T, id, view string) map[string]any {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"tool", "describe", id, "--view", view}, &stdout, &stderr); code != 0 {
		t.Fatalf("describe %s (%s): exit=%d stdout=%q stderr=%q", id, view, code, stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("describe %s (%s): stderr=%q", id, view, stderr.String())
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("describe %s (%s): invalid JSON: %v", id, view, err)
	}
	return result
}

func c5Object(t *testing.T, value any, name string) map[string]any {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s: expected object, got %#v", name, value)
	}
	return object
}

func c5Array(t *testing.T, value any, name string) []any {
	t.Helper()
	array, ok := value.([]any)
	if !ok {
		t.Fatalf("%s: expected array, got %#v", name, value)
	}
	return array
}

func TestC5ToolDescribeMetadataAndLegacyDigest(t *testing.T) {
	catalog, err := contract.LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		id, recovery, recoverySource, docURL, unknownFields string
	}{
		{"log.search", "safe-retry", "reviewed-read-contract", "https://www.volcengine.com/docs/6470/112195", "schema"},
		{"log.put", "high-risk-retry", "unverified", "https://www.volcengine.com/docs/6470/112191", "schema"},
		{"log.describe-cursor-time", "high-risk-retry", "unverified", "", "schema"},
		{"account.active", "high-risk-retry", "unverified", "https://www.volcengine.com/docs/6470/1978272", "passthrough"},
	}
	for _, tc := range cases {
		for _, view := range []string{"compact", "full"} {
			t.Run(tc.id+"/"+view, func(t *testing.T) {
				got := c5Describe(t, tc.id, view)
				if got["recovery"] != tc.recovery || got["recovery_source"] != tc.recoverySource {
					t.Fatalf("recovery=%#v source=%#v", got["recovery"], got["recovery_source"])
				}
				if tc.docURL == "" {
					if value, exists := got["doc_url"]; !exists || value != nil {
						t.Fatalf("unlinked doc_url=%#v exists=%v, want null", value, exists)
					}
				} else if got["doc_url"] != tc.docURL {
					t.Fatalf("doc_url=%#v, want %s", got["doc_url"], tc.docURL)
				}
				identity := c5Object(t, got["identity"], "identity")
				if identity["api_version_default"] != "0.3.0" {
					t.Fatalf("api_version_default=%#v", identity["api_version_default"])
				}
				if view == "full" {
					if identity["api_version_source"] != "client-default-header" {
						t.Fatalf("api_version_source=%#v", identity["api_version_source"])
					}
					for _, key := range []string{"deprecated", "maintainer"} {
						if value, exists := identity[key]; !exists || value != nil {
							t.Fatalf("identity.%s=%#v exists=%v, want null", key, value, exists)
						}
					}
					if tc.docURL != "" && got["doc_url_source"] != "official-doc-index" {
						t.Fatalf("doc_url_source=%#v", got["doc_url_source"])
					}
					notes, ok := got["recovery_notes"].(string)
					if !ok || notes == "" {
						t.Fatalf("recovery_notes=%#v", got["recovery_notes"])
					}
					if tc.id == "log.put" && (!strings.Contains(notes, "Do not retry automatically") || !strings.Contains(notes, "reconcile")) {
						t.Fatalf("write recovery lacks no-automatic-retry guidance: %q", notes)
					}
					if tc.id == "log.search" && !strings.Contains(notes, "does not establish service idempotency") {
						t.Fatalf("read recovery overstates service guarantees: %q", notes)
					}
				}
				policy := c5Object(t, got["input_policy"], "input_policy")
				if policy["unknown_fields"] != tc.unknownFields || policy["extra_top_level"] != "reject" {
					t.Fatalf("input_policy=%#v", policy)
				}
				operation, ok := loadToolOperation(tc.id)
				if !ok {
					t.Fatalf("missing operation %s", tc.id)
				}
				legacy, err := contract.RebuildLegacyToolV1(catalog, operation)
				if err != nil {
					t.Fatal(err)
				}
				if legacy.RecoverySource != tc.recoverySource || legacy.DocURL != tc.docURL {
					t.Fatalf("legacy metadata for %s: %#v", tc.id, legacy)
				}
				if legacy.RecoveryNotes == "" || (view == "full" && legacy.RecoveryNotes != got["recovery_notes"]) ||
					(tc.docURL != "" && legacy.DocURLSource != "official-doc-index") {
					t.Fatalf("legacy provenance for %s: %#v", tc.id, legacy)
				}
				digest := c5Object(t, got["contract_digest"], "contract_digest")
				if want := contract.LegacyToolDigestV1(legacy); want == "" || digest["value"] != want {
					t.Fatalf("digest=%#v, want legacy projection %q", digest["value"], want)
				}
			})
		}
	}
}

func TestC5CompactDescribePreservesSearchConstraints(t *testing.T) {
	got := c5Describe(t, "log.search", "compact")
	input := c5Object(t, got["input_schema"], "input_schema")
	body := c5Object(t, input["body"], "body")
	if body["additionalProperties"] != false {
		t.Fatalf("body additionalProperties=%#v", body["additionalProperties"])
	}
	properties := c5Object(t, body["properties"], "body.properties")
	limit := c5Object(t, properties["Limit"], "Limit")
	if limit["minimum"] != float64(0) || limit["maximum"] != float64(1000) {
		t.Fatalf("Limit bounds=%#v", limit)
	}
	sortField := c5Object(t, properties["Sort"], "Sort")
	if !reflect.DeepEqual(sortField["enum"], []any{"desc", "asc"}) {
		t.Fatalf("Sort enum=%#v", sortField["enum"])
	}
}

func TestC5CompactDescribePreservesDownloadTaskConditions(t *testing.T) {
	got := c5Describe(t, "log.create-download-task", "compact")
	input := c5Object(t, got["input_schema"], "input_schema")
	body := c5Object(t, input["body"], "body")
	properties := c5Object(t, body["properties"], "body.properties")
	taskType := c5Object(t, properties["TaskType"], "TaskType")
	if !reflect.DeepEqual(taskType["enum"], []any{float64(0), float64(1), float64(2)}) {
		t.Fatalf("TaskType enum=%#v", taskType["enum"])
	}
	conditions := c5Array(t, body["allOf"], "body.allOf")
	if len(conditions) != 2 {
		t.Fatalf("body.allOf=%#v", conditions)
	}
	first := c5Object(t, conditions[0], "allOf[0]")
	firstIf := c5Object(t, first["if"], "allOf[0].if")
	firstIfProps := c5Object(t, firstIf["properties"], "allOf[0].if.properties")
	firstType := c5Object(t, firstIfProps["TaskType"], "allOf[0].if.TaskType")
	if !reflect.DeepEqual(firstType["enum"], []any{float64(0), float64(1)}) ||
		!reflect.DeepEqual(firstIf["required"], []any{"TaskType"}) {
		t.Fatalf("search/analyze types=%#v", firstType["enum"])
	}
	firstThen := c5Object(t, first["then"], "allOf[0].then")
	if !reflect.DeepEqual(firstThen["required"], []any{"Limit", "Sort"}) {
		t.Fatalf("search/analyze requirements=%#v", firstThen["required"])
	}
	thenProps := c5Object(t, firstThen["properties"], "allOf[0].then.properties")
	if c5Object(t, thenProps["Limit"], "Limit")["minimum"] != float64(1) ||
		!reflect.DeepEqual(c5Object(t, thenProps["Sort"], "Sort")["enum"], []any{"asc", "desc"}) {
		t.Fatalf("search/analyze constraints=%#v", thenProps)
	}
	second := c5Object(t, conditions[1], "allOf[1]")
	secondIf := c5Object(t, second["if"], "allOf[1].if")
	secondIfProps := c5Object(t, secondIf["properties"], "allOf[1].if.properties")
	if c5Object(t, secondIfProps["TaskType"], "allOf[1].if.TaskType")["const"] != float64(2) ||
		!reflect.DeepEqual(secondIf["required"], []any{"TaskType"}) {
		t.Fatalf("log context condition=%#v", secondIfProps)
	}
	secondThen := c5Object(t, second["then"], "allOf[1].then")
	if !reflect.DeepEqual(secondThen["required"], []any{"LogContextInfos"}) {
		t.Fatalf("log context requirements=%#v", secondThen["required"])
	}
	secondProps := c5Object(t, secondThen["properties"], "allOf[1].then.properties")
	context := c5Object(t, secondProps["LogContextInfos"], "LogContextInfos")
	if !reflect.DeepEqual(context["required"], []any{"ContextFlow", "Source"}) {
		t.Fatalf("log context field requirements=%#v", context["required"])
	}
	contextProps := c5Object(t, context["properties"], "LogContextInfos.properties")
	for _, name := range []string{"ContextFlow", "Source"} {
		if c5Object(t, contextProps[name], name)["minLength"] != float64(1) {
			t.Fatalf("log context %s constraint=%#v", name, contextProps[name])
		}
	}
}

func TestC5CompactDescribePreservesIndexFullTextConditions(t *testing.T) {
	got := c5Describe(t, "index.create", "compact")
	input := c5Object(t, got["input_schema"], "input_schema")
	body := c5Object(t, input["body"], "body")
	props := c5Object(t, body["properties"], "body.properties")
	fullText := c5Object(t, props["FullText"], "FullText")
	conditions := c5Array(t, fullText["allOf"], "FullText.allOf")
	if len(conditions) != 1 {
		t.Fatalf("FullText.allOf=%#v", conditions)
	}
	condition := c5Object(t, conditions[0], "FullText.allOf[0]")
	predicate := c5Object(t, condition["if"], "FullText.if")
	predicateProps := c5Object(t, predicate["properties"], "FullText.if.properties")
	if c5Object(t, predicateProps["IncludeChinese"], "IncludeChinese")["const"] != true ||
		!reflect.DeepEqual(predicate["required"], []any{"IncludeChinese"}) {
		t.Fatalf("FullText condition=%#v", predicate)
	}
	then := c5Object(t, condition["then"], "FullText.then")
	thenProps := c5Object(t, then["properties"], "FullText.then.properties")
	if c5Object(t, thenProps["Delimiter"], "then.Delimiter")["maxLength"] != float64(256) {
		t.Fatalf("FullText then=%#v", then)
	}
	otherwise := c5Object(t, condition["else"], "FullText.else")
	elseProps := c5Object(t, otherwise["properties"], "FullText.else.properties")
	delimiter := c5Object(t, elseProps["Delimiter"], "else.Delimiter")
	if !reflect.DeepEqual(otherwise["required"], []any{"Delimiter"}) || delimiter["minLength"] != float64(1) || delimiter["maxLength"] != float64(256) {
		t.Fatalf("FullText else=%#v", otherwise)
	}
}

func TestC5CompactAndFlatAdditionalProperties(t *testing.T) {
	cases := []struct {
		name      string
		set       bool
		value     any
		flatClose bool
	}{
		{"closed", true, false, true},
		{"open", true, true, false},
		{"schema", true, map[string]any{"type": "string"}, false},
		{"absent", false, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			section := map[string]any{"type": "object", "properties": map[string]any{"Known": map[string]any{"type": "string"}}}
			if tc.set {
				section["additionalProperties"] = tc.value
			}
			input := map[string]any{"query": section}
			compact := compactToolInputSchema(input)
			query := c5Object(t, compact["query"], "compact query")
			value, present := query["additionalProperties"]
			if present != tc.set || !reflect.DeepEqual(value, tc.value) {
				t.Fatalf("compact additionalProperties=(%#v,%v), want (%#v,%v)", value, present, tc.value, tc.set)
			}
			flat := buildToolFlatInputSchema(input)
			if flat == nil {
				t.Fatal("missing flat input schema")
			}
			flatValue, flatPresent := flat["additionalProperties"]
			if flatPresent != tc.flatClose || (flatPresent && flatValue != false) {
				t.Fatalf("flat additionalProperties=(%#v,%v), want closed=%v", flatValue, flatPresent, tc.flatClose)
			}
		})
	}
}
