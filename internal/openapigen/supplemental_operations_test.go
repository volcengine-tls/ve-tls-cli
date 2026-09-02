package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
)

func TestLoadSupplementalOperationOverridesAcceptsPublicAndInternal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "supplemental.json")
	input := []contract.Operation{
		supplementalTestOperation("log.extract", "public", "ExtractLogSample", "/ExtractLogSample"),
		supplementalTestOperation("internal.describe", "internal", "DescribeInternal", "/DescribeInternal"),
	}
	writeSupplementalOverrides(t, path, input)

	got, err := loadSupplementalOperationOverrides(path)
	if err != nil {
		t.Fatalf("load supplemental operation overrides: %v", err)
	}
	if got, want := len(got), len(input); got != want {
		t.Fatalf("operations=%d, want %d", got, want)
	}
	for _, operation := range input {
		if gotOperation := operationByID(t, got, operation.ID); !reflect.DeepEqual(gotOperation, operation) {
			t.Fatalf("operation %q=%#v, want %#v", operation.ID, gotOperation, operation)
		}
	}
}

func TestCommittedSupplementalOperationOverridesAreValid(t *testing.T) {
	path := filepath.Join("..", "..", "contracts", "overrides", "supplemental_operations.json")
	if _, err := loadSupplementalOperationOverrides(path); err != nil {
		t.Fatalf("load committed supplemental operation overrides: %v", err)
	}
}

func TestLoadSupplementalOperationOverridesRejectsMalformedContracts(t *testing.T) {
	validOperation := supplementalTestOperation("log.extract", "public", "ExtractLogSample", "/ExtractLogSample")
	validRaw, err := json.Marshal(supplementalOperationOverrides{Operations: []contract.Operation{validOperation}})
	if err != nil {
		t.Fatal(err)
	}
	duplicateRaw, err := json.Marshal(supplementalOperationOverrides{Operations: []contract.Operation{validOperation, validOperation}})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		raw  []byte
		want string
	}{
		{name: "unknown field", raw: []byte(`{"operations":[],"unknown":true}`), want: "unknown field"},
		{name: "trailing JSON", raw: append(validRaw, []byte(` {}`)...), want: "trailing JSON value"},
		{name: "duplicate ID", raw: duplicateRaw, want: "duplicate operation id"},
		{name: "incomplete contract", raw: []byte(`{"operations":[{"id":"missing"}]}`), want: "identity id/group/action are required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "supplemental.json")
			if err := os.WriteFile(path, tt.raw, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadSupplementalOperationOverrides(path); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error=%v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestMergeSupplementalOperationsReplacesAndAdds(t *testing.T) {
	original := supplementalTestOperation("log.extract", "public", "ExtractLogSample", "/ExtractLogSample")
	base := supplementalTestCatalog(t, original)
	replacement := original
	replacement.Docs.Summary = "Extract log sample replacement"
	added := supplementalTestOperation("log.parse-path", "internal", "ParsePath", "/ParsePath")

	merged, err := mergeSupplementalOperations(base, []contract.Operation{replacement, added})
	if err != nil {
		t.Fatalf("merge supplemental operations: %v", err)
	}
	if got, want := len(merged.Operations), 2; got != want {
		t.Fatalf("operations=%d, want %d", got, want)
	}
	if got := operationByID(t, merged.Operations, replacement.ID); !reflect.DeepEqual(got, replacement) {
		t.Fatalf("replacement=%#v, want %#v", got, replacement)
	}
	if got := operationByID(t, merged.Operations, added.ID); !reflect.DeepEqual(got, added) {
		t.Fatalf("added=%#v, want %#v", got, added)
	}
}

func TestMergeSupplementalOperationsIntoCheckedInCatalogUpdatesCatalogAndLock(t *testing.T) {
	root, catalogPath, lockPath, supplementalPath, base := prepareSupplementalMergeFixture(t, true)
	replacement := supplementalTestOperation("log.extract", "public", "ExtractLogSample", "/ExtractLogSample")
	replacement.Docs.Summary = "replacement"
	added := supplementalTestOperation("log.parse-path", "internal", "ParsePath", "/ParsePath")
	writeSupplementalOverrides(t, supplementalPath, []contract.Operation{replacement, added})

	if err := mergeSupplementalOperationsIntoCheckedInCatalog(catalogPath, lockPath, supplementalPath, root); err != nil {
		t.Fatalf("merge supplemental operations into catalog: %v", err)
	}
	raw, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	merged, err := contract.Load(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(merged.Operations), len(base.Operations)+1; got != want {
		t.Fatalf("operations=%d, want %d", got, want)
	}
	if got := operationByID(t, merged.Operations, replacement.ID); !reflect.DeepEqual(got, replacement) {
		t.Fatalf("replacement=%#v, want %#v", got, replacement)
	}
	raw, err = os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	var lock operationCatalogLock
	if err := json.Unmarshal(raw, &lock); err != nil {
		t.Fatal(err)
	}
	if err := validateCommittedOperationCatalogLock(root, lock, merged); err != nil {
		t.Fatalf("validate updated lock: %v", err)
	}
}

func TestMergeSupplementalOperationsRejectsChangedNonMutableInput(t *testing.T) {
	root, catalogPath, lockPath, supplementalPath, _ := prepareSupplementalMergeFixture(t, true)
	if err := os.WriteFile(filepath.Join(root, "unrelated.txt"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	originalCatalog, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	originalLock, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	err = mergeSupplementalOperationsIntoCheckedInCatalog(catalogPath, lockPath, supplementalPath, root)
	if err == nil || !strings.Contains(err.Error(), `unexpected operation catalog input "unrelated" digest mismatch`) {
		t.Fatalf("merge error=%v, want unrelated input digest mismatch", err)
	}
	gotCatalog, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	gotLock, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotCatalog, originalCatalog) || !bytes.Equal(gotLock, originalLock) {
		t.Fatal("failed merge changed catalog or lock")
	}
}

func TestSupplementalMergeOnlyAllowsMainMigrationOnce(t *testing.T) {
	root, catalogPath, lockPath, supplementalPath, _ := prepareSupplementalMergeFixture(t, false)
	if err := os.WriteFile(filepath.Join(root, "internal", "openapigen", "main.go"), []byte("new merge flag\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := mergeSupplementalOperationsIntoCheckedInCatalog(catalogPath, lockPath, supplementalPath, root); err != nil {
		t.Fatalf("first supplemental merge must permit main migration: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "openapigen", "main.go"), []byte("unexpected later change\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := mergeSupplementalOperationsIntoCheckedInCatalog(catalogPath, lockPath, supplementalPath, root)
	if err == nil || !strings.Contains(err.Error(), `unexpected operation catalog input "generator_main" digest mismatch`) {
		t.Fatalf("second merge error=%v, want generator_main digest mismatch", err)
	}
}

func TestSupplementalMergeOnlyMutableInputsAreLimited(t *testing.T) {
	root := t.TempDir()
	got, err := supplementalMergeOnlyMutableInputs(root, filepath.Join(root, "contracts", "overrides", "supplemental_operations.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"override_supplemental_operations":  "contracts/overrides/supplemental_operations.json",
		"generator_supplemental_operations": "internal/openapigen/supplemental_operations.go",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merge-only mutable inputs=%#v, want %#v", got, want)
	}
}

func prepareSupplementalMergeFixture(t *testing.T, includeSupplementalInputs bool) (root, catalogPath, lockPath, supplementalPath string, catalog contract.Catalog) {
	t.Helper()
	root = t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal", "openapigen"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "contracts", "overrides"), 0o700); err != nil {
		t.Fatal(err)
	}
	mainPath := filepath.Join(root, "internal", "openapigen", "main.go")
	supplementalGeneratorPath := filepath.Join(root, "internal", "openapigen", "supplemental_operations.go")
	if err := os.WriteFile(mainPath, []byte("initial main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(supplementalGeneratorPath, []byte("initial supplemental generator\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	supplementalPath = filepath.Join(root, "contracts", "overrides", "supplemental_operations.json")
	original := supplementalTestOperation("log.extract", "public", "ExtractLogSample", "/ExtractLogSample")
	writeSupplementalOverrides(t, supplementalPath, []contract.Operation{original})
	catalog = supplementalTestCatalog(t, original)
	if err := os.WriteFile(filepath.Join(root, "unrelated.txt"), []byte("unchanged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	inputPaths := map[string]string{
		"generator_main": mainPath,
		"unrelated":      filepath.Join(root, "unrelated.txt"),
	}
	if includeSupplementalInputs {
		inputPaths["generator_supplemental_operations"] = supplementalGeneratorPath
		inputPaths["override_supplemental_operations"] = supplementalPath
	}
	lock, err := buildOperationCatalogLock(root, "bootstrap", catalog, inputPaths)
	if err != nil {
		t.Fatal(err)
	}
	catalogPath = filepath.Join(root, "generated_catalog.json")
	lockPath = filepath.Join(root, "operation-catalog-v2-lock.json")
	if err := writeOperationCatalogPair(catalogPath, catalog, lockPath, lock); err != nil {
		t.Fatal(err)
	}
	return root, catalogPath, lockPath, supplementalPath, catalog
}

func supplementalTestCatalog(t *testing.T, operations ...contract.Operation) contract.Catalog {
	t.Helper()
	catalog, err := contract.NewCatalog(
		"v1",
		contract.JSONSchema(defaultToolContextSchema()),
		contract.JSONSchema(defaultToolExecutionSchema()),
		operations,
	)
	if err != nil {
		t.Fatalf("new test catalog: %v", err)
	}
	return catalog
}

func supplementalTestOperation(id contract.OperationID, visibility, action, path string) contract.Operation {
	verb := "describe"
	if strings.HasPrefix(action, "Extract") || strings.HasPrefix(action, "Parse") {
		verb = "validate"
	}
	return contract.Operation{
		ID:         id,
		Group:      "log",
		GroupTitle: "Log",
		Action:     action,
		Resource:   "log",
		Verb:       verb,
		Family:     "log",
		Visibility: visibility,
		Wire: contract.WireSpec{
			Method: "POST", Path: path, RequestFormat: "json", Codec: contract.CodecJSON,
		},
		InputSchema: contract.JSONSchema{"body": map[string]any{"type": "object"}},
		Runtime:     contract.RuntimeSpec{SupportsDryRun: true},
		Output:      contract.OutputSpec{Policy: "envelope", IsEnvelopeOutput: true},
		Docs:        contract.DocsSpec{Summary: action, Source: "supplemental test"},
		Risk:        contract.RiskSpec{Level: "low", ErrorRecovery: "safe-retry"},
	}
}

func writeSupplementalOverrides(t *testing.T, path string, operations []contract.Operation) {
	t.Helper()
	raw, err := json.Marshal(supplementalOperationOverrides{Operations: operations})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}
