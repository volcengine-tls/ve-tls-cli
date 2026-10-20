package execution

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
)

func TestSavedSearchOperationsBuildExactRequestsFromFlatAndSectionedInput(t *testing.T) {
	tests := []struct {
		name       string
		id         contract.OperationID
		method     string
		path       string
		flat       map[string]any
		sectioned  map[string]any
		wantBody   map[string]any
		wantQuery  map[string]string
		bodyAbsent bool
	}{
		{
			name: "create",
			id:   "log.create-saved-search", method: "POST", path: "/CreateSavedSearch",
			flat: map[string]any{
				"TopicID": "topic-1", "DisplayName": "saved query", "SearchQuery": "*",
			},
			sectioned: map[string]any{
				"body": map[string]any{
					"TopicID": "topic-1", "DisplayName": "saved query", "SearchQuery": "*",
				},
			},
			wantBody: map[string]any{
				"TopicID": "topic-1", "DisplayName": "saved query", "SearchQuery": "*",
			},
			wantQuery: map[string]string{},
		},
		{
			name: "delete",
			id:   "log.delete-saved-search", method: "DELETE", path: "/DeleteSavedSearch",
			flat: map[string]any{
				"SavedSearchID": "savedsearch-1692687047134",
			},
			sectioned: map[string]any{
				"body": map[string]any{
					"SavedSearchID": "savedsearch-1692687047134",
				},
			},
			wantBody:  map[string]any{"SavedSearchID": "savedsearch-1692687047134"},
			wantQuery: map[string]string{},
		},
		{
			name: "describe",
			id:   "log.describe-saved-search", method: "GET", path: "/DescribeSavedSearch",
			flat: map[string]any{
				"SavedSearchID": "savedsearch-1692687047134",
			},
			sectioned: map[string]any{
				"query": map[string]any{
					"SavedSearchID": "savedsearch-1692687047134",
				},
			},
			wantBody:   map[string]any{},
			wantQuery:  map[string]string{"SavedSearchID": "savedsearch-1692687047134"},
			bodyAbsent: true,
		},
		{
			name: "list",
			id:   "log.describe-saved-searches", method: "GET", path: "/DescribeSavedSearches",
			flat: map[string]any{
				"TopicId": "topic-1",
			},
			sectioned: map[string]any{
				"query": map[string]any{
					"TopicId": "topic-1",
				},
			},
			wantBody:   map[string]any{},
			wantQuery:  map[string]string{"TopicId": "topic-1"},
			bodyAbsent: true,
		},
	}

	operations := savedSearchOperations(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			operation, ok := operations[tt.id]
			if !ok {
				t.Fatalf("missing operation %q", tt.id)
			}
			for _, inputCase := range []struct {
				name string
				raw  map[string]any
			}{
				{name: "flat", raw: tt.flat},
				{name: "sectioned", raw: tt.sectioned},
			} {
				t.Run(inputCase.name, func(t *testing.T) {
					input, err := NormalizeInput(operation, inputCase.raw)
					if err != nil {
						t.Fatalf("NormalizeInput: %v", err)
					}
					if err := ValidateInput(operation, input); err != nil {
						t.Fatalf("ValidateInput: %v", err)
					}
					if tt.bodyAbsent {
						if input.Body.Present || input.Body.JSON != nil {
							t.Fatalf("GET input unexpectedly has body: present=%v json=%#v", input.Body.Present, input.Body.JSON)
						}
					} else if !input.Body.Present {
						t.Fatalf("body input was not marked present")
					}

					request, err := BuildRequest(operation, input)
					if err != nil {
						t.Fatalf("BuildRequest: %v", err)
					}
					if request.Method != tt.method || request.Path != tt.path {
						t.Fatalf("wire = %s %s, want %s %s", request.Method, request.Path, tt.method, tt.path)
					}
					if !reflect.DeepEqual(request.Query, tt.wantQuery) {
						t.Fatalf("query = %#v, want %#v", request.Query, tt.wantQuery)
					}
					if len(request.QueryMulti) != 0 {
						t.Fatalf("query multi = %#v, want empty", request.QueryMulti)
					}
					var gotBody map[string]any
					if err := json.Unmarshal(request.Body, &gotBody); err != nil {
						t.Fatalf("decode request body: %v; raw=%q", err, request.Body)
					}
					if !reflect.DeepEqual(gotBody, tt.wantBody) {
						t.Fatalf("body = %#v, want %#v", gotBody, tt.wantBody)
					}
				})
			}
		})
	}
}

func TestSavedSearchRequiredFieldsRejectMissingAndNullLocally(t *testing.T) {
	tests := []struct {
		name string
		id   contract.OperationID
		raw  map[string]any
		want string
	}{
		{
			name: "create missing TopicID", id: "log.create-saved-search",
			raw:  map[string]any{"DisplayName": "saved query", "SearchQuery": "*"},
			want: "missing required field: input.body.TopicID",
		},
		{
			name: "create missing DisplayName", id: "log.create-saved-search",
			raw:  map[string]any{"TopicID": "topic-1", "SearchQuery": "*"},
			want: "missing required field: input.body.DisplayName",
		},
		{
			name: "create null DisplayName", id: "log.create-saved-search",
			raw:  map[string]any{"TopicID": "topic-1", "DisplayName": nil, "SearchQuery": "*"},
			want: "missing required field: input.body.DisplayName",
		},
		{
			name: "create missing SearchQuery", id: "log.create-saved-search",
			raw:  map[string]any{"TopicID": "topic-1", "DisplayName": "saved query"},
			want: "missing required field: input.body.SearchQuery",
		},
		{
			name: "delete missing SavedSearchID", id: "log.delete-saved-search",
			raw:  map[string]any{},
			want: "missing required field: input.body.SavedSearchID",
		},
		{
			name: "delete null SavedSearchID", id: "log.delete-saved-search",
			raw:  map[string]any{"SavedSearchID": nil},
			want: "missing required field: input.body.SavedSearchID",
		},
		{
			name: "describe missing SavedSearchID", id: "log.describe-saved-search",
			raw:  map[string]any{},
			want: "missing required field: input.query.SavedSearchID",
		},
		{
			name: "describe null SavedSearchID", id: "log.describe-saved-search",
			raw:  map[string]any{"SavedSearchID": nil},
			want: "missing required field: input.query.SavedSearchID",
		},
		{
			name: "list missing TopicId", id: "log.describe-saved-searches",
			raw:  map[string]any{},
			want: "missing required field: input.query.TopicId",
		},
		{
			name: "list null TopicId", id: "log.describe-saved-searches",
			raw:  map[string]any{"TopicId": nil},
			want: "missing required field: input.query.TopicId",
		},
	}

	operations := savedSearchOperations(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			operation, ok := operations[tt.id]
			if !ok {
				t.Fatalf("missing operation %q", tt.id)
			}
			input, err := NormalizeInput(operation, tt.raw)
			if err != nil {
				t.Fatalf("NormalizeInput: %v", err)
			}
			if err := ValidateInput(operation, input); err == nil || err.Error() != tt.want {
				t.Fatalf("ValidateInput error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestSavedSearchFieldNamesRemainCaseSensitive(t *testing.T) {
	operations := savedSearchOperations(t)
	for _, tt := range []struct {
		name string
		id   contract.OperationID
		raw  map[string]any
		want string
	}{
		{
			name: "create TopicId is not TopicID",
			id:   "log.create-saved-search",
			raw: map[string]any{
				"TopicId": "topic-1", "DisplayName": "saved query", "SearchQuery": "*",
			},
			want: "missing required field: input.body.TopicID",
		},
		{
			name: "list TopicID is not TopicId",
			id:   "log.describe-saved-searches",
			raw:  map[string]any{"TopicID": "topic-1"},
			want: "flat input contains unknown fields: TopicID",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			operation := operations[tt.id]
			input, err := NormalizeInput(operation, tt.raw)
			if err != nil {
				if tt.id == "log.create-saved-search" {
					t.Fatalf("NormalizeInput unexpectedly rejected body-only unknown field: %v", err)
				}
				if err.Error() != tt.want {
					t.Fatalf("NormalizeInput error = %v, want %q", err, tt.want)
				}
				return
			}
			if err := ValidateInput(operation, input); err == nil || err.Error() != tt.want {
				t.Fatalf("ValidateInput error = %v, want %q", err, tt.want)
			}
		})
	}
}

func savedSearchOperations(t *testing.T) map[contract.OperationID]contract.Operation {
	t.Helper()
	catalog, err := contract.LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}
	operations := make(map[contract.OperationID]contract.Operation, len(catalog.Operations))
	for _, operation := range catalog.Operations {
		operations[operation.ID] = operation
	}
	return operations
}
