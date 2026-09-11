package main

import (
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
)

func TestSourceCatalogSupplementalMergeUsesStableDownloadTaskIDs(t *testing.T) {
	doc := swaggerDoc{
		Paths: map[string]swaggerPathItem{
			"/CreateDownloadTask": {Post: &swaggerOp{Summary: "CreateDownloadTask", Tags: []string{"Log"}}},
			"/CancelDownloadTask": {Post: &swaggerOp{Summary: "CancelDownloadTask", Tags: []string{"Log"}}},
		},
	}
	source := buildSourceOperations(
		doc,
		map[string]string{"Log": "log"},
		map[string]string{},
		map[string]apiDocEntry{},
		toolCatalogOverrides{},
	)
	catalog, err := buildOperationCatalogV2FromSource("v1", source)
	if err != nil {
		t.Fatalf("build source catalog: %v", err)
	}
	merged, err := mergeSupplementalOperations(catalog, nil)
	if err != nil {
		t.Fatalf("merge source catalog: %v", err)
	}

	want := map[contract.OperationID]struct {
		action string
		method string
		path   string
	}{
		"log.create-download-task": {action: "CreateDownloadTask", method: "POST", path: "/CreateDownloadTask"},
		"log.cancel-download-task": {action: "CancelDownloadTask", method: "POST", path: "/CancelDownloadTask"},
	}
	if len(merged.Operations) != len(want) {
		t.Fatalf("operations=%d, want %d", len(merged.Operations), len(want))
	}
	for _, operation := range merged.Operations {
		expected, ok := want[operation.ID]
		if !ok {
			t.Fatalf("unexpected operation ID %q", operation.ID)
		}
		if operation.Action != expected.action || operation.Wire.Method != expected.method || operation.Wire.Path != expected.path {
			t.Fatalf("operation %q metadata changed: action=%q wire=%s %s", operation.ID, operation.Action, operation.Wire.Method, operation.Wire.Path)
		}
	}
}

func TestCanonicalStableOperationIDRequiresExactDownloadTaskMetadata(t *testing.T) {
	tests := []struct {
		name   string
		id     string
		action string
		path   string
		want   string
	}{
		{
			name:   "unrelated create action",
			id:     "log.create",
			action: "CreateSomethingElse",
			path:   "/CreateSomethingElse",
			want:   "log.create",
		},
		{
			name:   "already canonical create action",
			id:     "log.create-download-task",
			action: "CreateDownloadTask",
			path:   "/CreateDownloadTask",
			want:   "log.create-download-task",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := canonicalStableOperationID(
				tt.id,
				"log",
				tt.action,
				"create",
				"POST",
				tt.path,
			)
			if got != tt.want {
				t.Fatalf("canonicalStableOperationID()=%q, want %q", got, tt.want)
			}
		})
	}
}

func TestMigrateCheckedInCatalogOperationIDs(t *testing.T) {
	legacy := supplementalTestOperation("log.create", "public", "CreateDownloadTask", "/CreateDownloadTask")
	legacy.Verb = "create"
	catalog := supplementalTestCatalog(t, legacy)

	migrated, err := migrateCheckedInCatalogOperationIDs(catalog)
	if err != nil {
		t.Fatalf("migrate checked-in operation IDs: %v", err)
	}
	if got, want := len(migrated.Operations), 1; got != want {
		t.Fatalf("operations=%d, want %d", got, want)
	}
	if migrated.Operations[0].ID != contract.OperationID("log.create-download-task") {
		t.Fatalf("migrated ID=%q", migrated.Operations[0].ID)
	}
	if migrated.Operations[0].Action != legacy.Action ||
		migrated.Operations[0].Verb != legacy.Verb ||
		migrated.Operations[0].Wire != legacy.Wire {
		t.Fatalf("metadata changed during migration: got=%#v want action=%q verb=%q wire=%#v", migrated.Operations[0], legacy.Action, legacy.Verb, legacy.Wire)
	}
}
