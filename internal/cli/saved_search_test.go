package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSavedSearchOperationsAreDiscoverableAndDescribable(t *testing.T) {
	wantIDs := []string{
		"log.create-saved-search",
		"log.delete-saved-search",
		"log.describe-saved-search",
		"log.describe-saved-searches",
	}

	listed, err := runTool(nil, []string{"list", "log"})
	if err != nil {
		t.Fatalf("tool list log: %v", err)
	}
	listText := asOutputString(t, listed)
	for _, id := range wantIDs {
		if !strings.Contains(listText, id) {
			t.Errorf("tool list log missing %s: %q", id, listText)
		}
		described, err := runTool(nil, []string{"describe", id})
		if err != nil {
			t.Fatalf("tool describe %s: %v", id, err)
		}
		payload, ok := described.(map[string]any)
		if !ok {
			t.Fatalf("tool describe %s returned %T", id, described)
		}
		identity, ok := payload["identity"].(map[string]any)
		if !ok || asStringOrEmpty(identity["id"]) != id {
			t.Fatalf("tool describe %s identity=%#v", id, payload["identity"])
		}
	}
}

func TestSavedSearchFullDescribeIncludesScopeNoPaginationAndRetryGuidance(t *testing.T) {
	for _, tt := range []struct {
		id       string
		write    bool
		section  string
		field    string
		method   string
		path     string
		risk     string
		recovery string
	}{
		{id: "log.create-saved-search", write: true, section: "body", field: "TopicID", method: "POST", path: "/CreateSavedSearch", risk: "high", recovery: "high-risk-retry"},
		{id: "log.delete-saved-search", write: true, section: "body", field: "SavedSearchID", method: "DELETE", path: "/DeleteSavedSearch", risk: "high", recovery: "high-risk-retry"},
		{id: "log.describe-saved-search", section: "query", field: "SavedSearchID", method: "GET", path: "/DescribeSavedSearch", risk: "low", recovery: "safe-retry"},
		{id: "log.describe-saved-searches", section: "query", field: "TopicId", method: "GET", path: "/DescribeSavedSearches", risk: "low", recovery: "safe-retry"},
	} {
		t.Run(tt.id, func(t *testing.T) {
			described, err := runTool(nil, []string{"describe", tt.id, "--view", "full"})
			if err != nil {
				t.Fatalf("tool describe %s: %v", tt.id, err)
			}
			payload, ok := described.(map[string]any)
			if !ok {
				t.Fatalf("tool describe %s returned %T", tt.id, described)
			}
			identity, ok := payload["identity"].(map[string]any)
			if !ok {
				t.Fatalf("%s missing identity: %#v", tt.id, payload)
			}
			for key, want := range map[string]string{
				"id": tt.id, "group": "log", "family": "log", "visibility": "public",
				"method": tt.method, "path": tt.path,
			} {
				if got := asStringOrEmpty(identity[key]); got != want {
					t.Fatalf("%s identity[%q]=%q, want %q", tt.id, key, got, want)
				}
			}

			execution, ok := payload["execution"].(map[string]any)
			if !ok {
				t.Fatalf("%s missing execution metadata: %#v", tt.id, payload["execution"])
			}
			if supportsAll, ok := execution["supports_all"].(bool); !ok || supportsAll {
				t.Fatalf("%s supports_all=%#v, want false: no pagination", tt.id, execution["supports_all"])
			}
			if supportsDryRun, ok := execution["supports_dry_run"].(bool); !ok || !supportsDryRun {
				t.Fatalf("%s supports_dry_run=%#v, want true", tt.id, execution["supports_dry_run"])
			}

			input, ok := payload["input"].(map[string]any)
			if !ok {
				t.Fatalf("%s missing input schema: %#v", tt.id, payload["input"])
			}
			section, ok := input[tt.section].(map[string]any)
			if !ok {
				t.Fatalf("%s missing %s input schema: %#v", tt.id, tt.section, input)
			}
			properties, ok := section["properties"].(map[string]any)
			if !ok {
				t.Fatalf("%s %s properties=%#v", tt.id, tt.section, section["properties"])
			}
			if _, ok := properties[tt.field]; !ok {
				t.Fatalf("%s %s missing exact-case field %q: %#v", tt.id, tt.section, tt.field, properties)
			}
			oppositeSection := map[bool]string{true: "query", false: "body"}[tt.write]
			if _, ok := input[oppositeSection]; ok {
				t.Fatalf("%s input schema exposes an unexpected opposite section: %#v", tt.id, input)
			}

			constraints := strings.ToLower(asStringOrEmpty(payload["usage_constraints"]))
			if !strings.Contains(constraints, "topic") {
				t.Fatalf("%s usage_constraints must document Topic scope: %q", tt.id, constraints)
			}
			if tt.write {
				if !strings.Contains(constraints, "do not retry automatically") {
					t.Fatalf("%s usage_constraints missing ambiguous-failure retry guidance: %q", tt.id, constraints)
				}
			} else if asStringOrEmpty(payload["recovery"]) != tt.recovery {
				t.Fatalf("%s recovery=%#v, want %q", tt.id, payload["recovery"], tt.recovery)
			}
			if asStringOrEmpty(payload["risk"]) != tt.risk {
				t.Fatalf("%s risk=%#v, want %q", tt.id, payload["risk"], tt.risk)
			}
			if asStringOrEmpty(payload["output_policy"]) != "full" {
				t.Fatalf("%s output_policy=%#v, want full", tt.id, payload["output_policy"])
			}
			output, ok := payload["output"].(map[string]any)
			if !ok || asStringOrEmpty(output["policy"]) != "full" || output["is_envelope"] != true {
				t.Fatalf("%s output=%#v, want full envelope", tt.id, payload["output"])
			}
		})
	}
}

func TestSavedSearchToolExecSendsWireRequestAndPassesThroughServer4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/CreateSavedSearch" || r.URL.RawQuery != "" {
			t.Errorf("request = %s %s?%s, want POST /CreateSavedSearch without query", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		wantBody := map[string]any{
			"TopicID": "topic-1", "DisplayName": "saved query", "SearchQuery": "*",
		}
		if !reflect.DeepEqual(body, wantBody) {
			t.Errorf("request body=%#v, want %#v", body, wantBody)
		}
		w.Header().Set("x-tls-requestid", "req-saved-search-409")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"ErrorCode":"SavedSearchAlreadyExist","ErrorMessage":"saved query already exists"}`))
	}))
	defer srv.Close()

	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", srv.URL)
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"tool", "exec", "log.create-saved-search",
		"--input", `{"TopicID":"topic-1","DisplayName":"saved query","SearchQuery":"*"}`,
	}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit=%d, want 2; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid stdout envelope: %v; stdout=%q", err, stdout.String())
	}
	if envelope["status"] != "failed" || envelope["requestId"] != "req-saved-search-409" {
		t.Fatalf("unexpected envelope status/requestId: %#v", envelope)
	}
	errorObject, ok := envelope["error"].(map[string]any)
	if !ok {
		t.Fatalf("missing error object: %#v", envelope)
	}
	if errorObject["statusCode"] != float64(http.StatusConflict) ||
		errorObject["source"] != "upstream" ||
		errorObject["code"] != "SavedSearchAlreadyExist" ||
		errorObject["message"] != "saved query already exists" {
		t.Fatalf("unexpected upstream error passthrough: %#v", errorObject)
	}
}
