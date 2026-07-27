package catalogdiff

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCompareCatalogs(t *testing.T) {
	oldCatalog := `{
  "version":"v1",
  "tools":[
    {
      "id":"group1.read",
      "group":"group1",
      "action":"Read",
      "resource":"item",
      "verb":"read",
      "family":"read",
      "method":"GET",
      "path":"/Read",
      "input_schema":{"type":"object","properties":{"a":{"type":"string"}}},
      "risk_level":"low",
      "doc_source":"sdk",
      "source_quality":"official",
      "supports_dry_run":true,
      "supports_all":false
    },
    {
      "id":"group1.write",
      "group":"group1",
      "action":"Write",
      "resource":"item",
      "verb":"write",
      "family":"write",
      "method":"POST",
      "path":"/Write",
      "input_schema":{"type":"object","properties":{"name":{"type":"string"}}},
      "risk_level":"high",
      "doc_source":"sdk",
      "source_quality":"official",
      "supports_dry_run":false,
      "supports_all":true
    },
    {
      "id":"group2.delete",
      "group":"group2",
      "action":"Delete",
      "resource":"item",
      "verb":"delete",
      "family":"delete",
      "method":"DELETE",
      "path":"/Delete",
      "input_schema":{"type":"object","properties":{"force":{"type":"boolean"}}},
      "risk_level":"medium",
      "doc_source":"sdk",
      "source_quality":"community",
      "supports_dry_run":false,
      "supports_all":false
    }
  ]
}`

	newCatalog := `{
  "version":"v1",
  "tools":[
    {
      "id":"group1.read",
      "group":"group1",
      "action":"Read",
      "resource":"item",
      "verb":"read",
      "family":"read",
      "method":"GET",
      "path":"/Read",
      "input_schema":{"type":"object","properties":{"a":{"type":"string"}},"required":["a"]},
      "risk_level":"low",
      "doc_source":"sdk",
      "source_quality":"official",
      "supports_dry_run":true,
      "supports_all":false
    },
    {
      "id":"group1.write",
      "group":"group1",
      "action":"Write",
      "resource":"item",
      "verb":"write",
      "family":"write",
      "method":"PATCH",
      "path":"/WriteV2",
      "input_schema":{"type":"object","properties":{"name":{"type":"string"}}},
      "risk_level":"medium",
      "doc_source":"sdk",
      "source_quality":"community",
      "supports_dry_run":false,
      "supports_all":false
    },
    {
      "id":"group3.new",
      "group":"group3",
      "action":"Create",
      "resource":"item",
      "verb":"create",
      "family":"create",
      "method":"POST",
      "path":"/Create",
      "input_schema":{"type":"object","properties":{"name":{"type":"string"}}},
      "risk_level":"low",
      "doc_source":"internal",
      "source_quality":"community",
      "supports_dry_run":true,
      "supports_all":false
    }
  ]
}`

	oldPath := writeTempFile(t, "old-catalog", oldCatalog)
	newPath := writeTempFile(t, "new-catalog", newCatalog)

	report, err := CompareFiles(oldPath, newPath)
	if err != nil {
		t.Fatalf("CompareFiles() error: %v", err)
	}

	if got, want := len(report.Added), 1; got != want {
		t.Fatalf("added count = %d, want %d", got, want)
	}
	if got := len(report.Removed); got != 1 {
		t.Fatalf("removed count = %d, want 1", got)
	}
	if got := len(report.MethodPathChanges); got != 1 {
		t.Fatalf("method/path change count = %d, want 1", got)
	}
	if got := len(report.InputSchemaDigestChanges); got != 1 {
		t.Fatalf("input schema digest change count = %d, want 1", got)
	}
	if got := len(report.RiskLevelChanges); got != 1 {
		t.Fatalf("risk level change count = %d, want 1", got)
	}
	if got := len(report.SourceChanges); got != 1 {
		t.Fatalf("source change count = %d, want 1", got)
	}
	if got := len(report.SupportsChanges); got != 1 {
		t.Fatalf("supports flag change count = %d, want 1", got)
	}
}

func TestCompareFilesMatchesReportJSON(t *testing.T) {
	oldCatalog := `{"version":"v1","tools":[{"id":"group.action","group":"group","action":"Action","method":"GET","path":"/Action","input_schema":{},"risk_level":"low","doc_source":"sdk","supports_dry_run":false,"supports_all":false}]}`
	newCatalog := `{"version":"v1","tools":[{"id":"group.action","group":"group","action":"Action","method":"GET","path":"/Action","input_schema":{},"risk_level":"low","doc_source":"sdk","supports_dry_run":false,"supports_all":true}]}`

	oldPath := writeTempFile(t, "old-catalog-simple", oldCatalog)
	newPath := writeTempFile(t, "new-catalog-simple", newCatalog)

	report, err := CompareFiles(oldPath, newPath)
	if err != nil {
		t.Fatalf("CompareFiles() error: %v", err)
	}

	summary := report.Summary
	if summary.SupportsChanges != 1 || summary.TotalChanges != 1 {
		t.Fatalf("unexpected summary: %#v", summary)
	}

	if len(report.SupportsChanges) != 1 {
		t.Fatalf("supports change count = %d, want 1", len(report.SupportsChanges))
	}
}

func TestLoadCatalogRejectsInvalidJSON(t *testing.T) {
	tmp := writeTempFile(t, "bad-catalog", `{"version":"v1","tools":[}`)
	if _, err := LoadCatalog(tmp); err == nil {
		t.Fatalf("expected invalid json error")
	}
}

func TestBuildInputSchemaDigestStable(t *testing.T) {
	schemaDigest := "93dcc0b5524386110df60f473ad58a842ef6dc5c6fed37460036e5c506e273fc" // expected for the normalized schema below
	got, err := inputSchemaDigest(map[string]any{
		"properties": map[string]any{
			"b": map[string]any{"type": "string"},
			"a": map[string]any{"type": "string"},
		},
	})
	if err != nil {
		t.Fatalf("inputSchemaDigest() error: %v", err)
	}
	if got != schemaDigest {
		t.Fatalf("digest mismatch: %s, want %s", got, schemaDigest)
	}
}

func writeTempFile(t *testing.T, pattern, body string) string {
	t.Helper()
	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, pattern+".json")
	if err := os.WriteFile(f, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}
	return f
}
