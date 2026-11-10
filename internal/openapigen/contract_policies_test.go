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

func TestRecoveryPolicyFileAndCoverage(t *testing.T) {
	catalog, err := contract.LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	policies, err := loadRecoveryPolicies(filepath.Join("..", "..", "contracts", "overrides", "recovery.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(policies) != len(catalog.Operations) {
		t.Fatalf("policies=%d operations=%d", len(policies), len(catalog.Operations))
	}
	refreshed, err := applyContractPolicies(catalog, policies, nil, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []contract.OperationID{"log.search", "trace.search", "tag.list", "log.preview", "log.describe-saved-search", "log.describe-saved-searches"} {
		if got := operationByID(t, refreshed.Operations, id).Risk; got.ErrorRecovery != "safe-retry" || got.RecoverySource != reviewedReadRecoverySource {
			t.Fatalf("%s: %#v", id, got)
		}
	}
	for _, id := range []contract.OperationID{"log.put", "log.consume", "shard.create", "log-app.describe-trace", "log.create-saved-search", "log.delete-saved-search"} {
		if got := operationByID(t, refreshed.Operations, id).Risk; got.ErrorRecovery != "high-risk-retry" || got.RecoverySource != unverifiedRecoverySource {
			t.Fatalf("%s: %#v", id, got)
		}
	}
	if inferToolErrorRecovery("DescribeAnything", "GET") != "high-risk-retry" {
		t.Fatal("default recovery must not infer safety from name or method")
	}
}

func TestRecoveryPolicyParsingRejectsInvalidAndDuplicateValues(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"duplicate", "a.get: safe-retry\na.get: high-risk-retry\n"},
		{"unsupported", "a.get: retry\n"},
		{"malformed", "a.get safe-retry\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "recovery.yaml")
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadRecoveryPolicies(path); err == nil {
				t.Fatal("invalid policy accepted")
			}
		})
	}
}

func TestPolicyDefaultCoverageAndUsageAppend(t *testing.T) {
	first := supplementalTestOperation("log.extract", "public", "ExtractLogSample", "/ExtractLogSample")
	first.Docs.UsageConstraints = "Original constraint."
	second := supplementalTestOperation("log.parse-path", "internal", "ParsePath", "/ParsePath")
	catalog := supplementalTestCatalog(t, first, second)
	policies := map[string]string{"log.extract": "safe-retry"}
	usage := map[string]string{"log.extract": "Follow the next step."}
	docs := map[string]string{"log.extract": "https://www.volcengine.com/docs/6470/123"}
	applied, err := applyContractPolicies(catalog, policies, usage, docs, false)
	if err != nil {
		t.Fatal(err)
	}
	read := operationByID(t, applied.Operations, first.ID)
	if read.Risk.ErrorRecovery != "safe-retry" || read.Risk.RecoverySource != reviewedReadRecoverySource ||
		read.Docs.UsageConstraints != "Original constraint. Follow the next step." ||
		read.Docs.URL != docs["log.extract"] || read.Docs.URLSource != "official-doc-index" {
		t.Fatalf("read operation=%#v", read)
	}
	unknown := operationByID(t, applied.Operations, second.ID)
	if unknown.Risk.ErrorRecovery != "high-risk-retry" || unknown.Risk.RecoverySource != unverifiedRecoverySource || unknown.Docs.URL != "" {
		t.Fatalf("unlisted operation=%#v", unknown)
	}
	repeated, err := applyContractPolicies(applied, policies, usage, docs, false)
	if err != nil || !reflect.DeepEqual(repeated, applied) {
		t.Fatalf("policy application is not idempotent: %v", err)
	}
	if _, err := applyContractPolicies(catalog, policies, usage, docs, true); err == nil || !strings.Contains(err.Error(), "log.parse-path") {
		t.Fatalf("missing policy error=%v", err)
	}
	policies["unknown.id"] = "safe-retry"
	if _, err := applyContractPolicies(catalog, policies, usage, docs, true); err == nil || !strings.Contains(err.Error(), "unknown.id") {
		t.Fatalf("unknown policy error=%v", err)
	}
	docs["unknown.id"] = "https://www.volcengine.com/docs/6470/123"
	if _, err := applyContractPolicies(catalog, policies, usage, docs, false); err == nil || !strings.Contains(err.Error(), "unknown.id") {
		t.Fatalf("unknown documentation error=%v", err)
	}
}

func TestDocumentationOverridesRejectUntrustedURLsAndDuplicateIDs(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"official", `{"log.search":"https://www.volcengine.com/docs/6470/112"}`, true},
		{"http", `{"log.search":"http://www.volcengine.com/docs/6470/112"}`, false},
		{"userinfo", `{"log.search":"https://www.volcengine.com@evil.test/docs/6470/112"}`, false},
		{"lookalike", `{"log.search":"https://www.volcengine.com.evil.test/docs/6470/112"}`, false},
		{"query", `{"log.search":"https://www.volcengine.com/docs/6470/112?next=evil"}`, false},
		{"wrong path", `{"log.search":"https://www.volcengine.com/not-docs/112"}`, false},
		{"traversal", `{"log.search":"https://www.volcengine.com/docs/../other"}`, false},
		{"duplicate", `{"log.search":"https://www.volcengine.com/docs/6470/1","log.search":"https://www.volcengine.com/docs/6470/2"}`, false},
		{"trailing", `{} {}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "docs.json")
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := loadDocumentationOverrides(path)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
}

func TestSupplementalMergeReappliesAllContractPolicies(t *testing.T) {
	root, catalogPath, lockPath, supplementalPath, _ := prepareSupplementalMergeFixture(t, true)
	replacement := supplementalTestOperation("log.extract", "public", "ExtractLogSample", "/ExtractLogSample")
	replacement.Risk.ErrorRecovery = "high-risk-retry"
	replacement.Docs.UsageConstraints = "existing"
	writeSupplementalOverrides(t, supplementalPath, []contract.Operation{replacement})
	for name, body := range map[string]string{
		"recovery.yaml":          "log.extract: safe-retry\n",
		"usage_constraints.yaml": "log.extract: follow the next step\n",
		"documentation.json":     `{"log.extract":"https://www.volcengine.com/docs/6470/123"}`,
	} {
		if err := os.WriteFile(filepath.Join(root, "contracts", "overrides", name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := mergeSupplementalOperationsIntoCheckedInCatalog(catalogPath, lockPath, supplementalPath, root); err != nil {
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
	got := operationByID(t, catalog.Operations, replacement.ID)
	if got.Risk.ErrorRecovery != "safe-retry" || got.Risk.RecoverySource != reviewedReadRecoverySource ||
		got.Docs.UsageConstraints != "existing follow the next step" || got.Docs.URLSource != "official-doc-index" {
		t.Fatalf("merged operation did not retain policies: %#v", got)
	}
}

func TestRefreshPoliciesRejectsDriftAndIsDeterministic(t *testing.T) {
	root, catalogPath, lockPath, _, _ := prepareSupplementalMergeFixture(t, true)
	recovery, usage, docs, err := policyInputPaths(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	catalogBefore, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	lockBefore, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(root, "unrelated.txt")
	if err := os.WriteFile(unrelated, []byte("drift\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := refreshContractPoliciesOnly(catalogPath, lockPath, root, recovery, usage, docs); err == nil || !strings.Contains(err.Error(), "unrelated") {
		t.Fatalf("unrelated drift error=%v", err)
	}
	assertFilesUnchanged(t, catalogPath, catalogBefore, lockPath, lockBefore)
	if err := os.WriteFile(unrelated, []byte("unchanged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(recovery, []byte("unknown.id: safe-retry\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := refreshContractPoliciesOnly(catalogPath, lockPath, root, recovery, usage, docs); err == nil || !strings.Contains(err.Error(), "coverage mismatch") {
		t.Fatalf("coverage error=%v", err)
	}
	assertFilesUnchanged(t, catalogPath, catalogBefore, lockPath, lockBefore)
	if err := os.WriteFile(recovery, []byte("log.extract: safe-retry\n"), 0o600); err != nil {
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
	if err := refreshContractPoliciesOnly(catalogPath, lockPath, root, recovery, usage, docs); err != nil {
		t.Fatal(err)
	}
	assertFilesUnchanged(t, catalogPath, firstCatalog, lockPath, firstLock)
	var lock operationCatalogLock
	if err := json.Unmarshal(firstLock, &lock); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"contract_legacy", "generator_contract_policies", "override_recovery", "override_usage_constraints", "override_documentation"} {
		found := false
		for _, input := range lock.Inputs {
			found = found || input.Name == name
		}
		if !found {
			t.Fatalf("refreshed lock missing %s", name)
		}
	}
}

func TestRefreshRejectsCatalogDigestMismatchBeforeWriting(t *testing.T) {
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
	catalog.Operations[0].Docs.Summary += " changed"
	tampered, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalogPath, tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	lockBefore, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := refreshContractPoliciesOnly(catalogPath, lockPath, root, recovery, usage, docs); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("catalog drift error=%v", err)
	}
	assertFilesUnchanged(t, catalogPath, tampered, lockPath, lockBefore)
}

func TestRefreshRejectsUnknownUsageWithoutWriting(t *testing.T) {
	root, catalogPath, lockPath, _, _ := prepareSupplementalMergeFixture(t, true)
	recovery, usage, docs, err := policyInputPaths(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	catalogBefore, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	lockBefore, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(usage, []byte("log.extrakt: typo in operation ID\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := refreshContractPoliciesOnly(catalogPath, lockPath, root, recovery, usage, docs); err == nil || !strings.Contains(err.Error(), "log.extrakt") {
		t.Fatalf("unknown usage error=%v", err)
	}
	assertFilesUnchanged(t, catalogPath, catalogBefore, lockPath, lockBefore)
}

func TestRefreshAllowsOnlyExactMutableInputPaths(t *testing.T) {
	root, catalogPath, lockPath, _, _ := prepareSupplementalMergeFixture(t, true)
	recovery, usage, docs, err := policyInputPaths(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	typesPath := filepath.Join(root, "internal", "contract", "types.go")
	if err := os.WriteFile(typesPath, []byte("old types\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	digest, err := hashFile(typesPath)
	if err != nil {
		t.Fatal(err)
	}
	rawLock, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	var lock operationCatalogLock
	if err := json.Unmarshal(rawLock, &lock); err != nil {
		t.Fatal(err)
	}
	lock.Inputs = append(lock.Inputs, operationCatalogInput{Name: "contract_types", Path: "internal/contract/types.go", SHA256: digest})
	if err := writeOperationCatalogLock(lockPath, lock); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(typesPath, []byte("new types\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := refreshContractPoliciesOnly(catalogPath, lockPath, root, recovery, usage, docs); err != nil {
		t.Fatalf("allowed type change: %v", err)
	}
	wrongPath := filepath.Join(root, "other-types.go")
	if err := os.WriteFile(wrongPath, []byte("other\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	wrongDigest, err := hashFile(wrongPath)
	if err != nil {
		t.Fatal(err)
	}
	lock.Inputs = []operationCatalogInput{{Name: "contract_types", Path: "other-types.go", SHA256: wrongDigest}}
	if err := validatePolicyRefreshInputs(root, recovery, usage, docs, lock); err == nil || !strings.Contains(err.Error(), "contract_types") {
		t.Fatalf("mutable name with unexpected path accepted: %v", err)
	}
}

func assertFilesUnchanged(t *testing.T, catalogPath string, catalogBefore []byte, lockPath string, lockBefore []byte) {
	t.Helper()
	catalogAfter, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	lockAfter, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(catalogBefore, catalogAfter) || !bytes.Equal(lockBefore, lockAfter) {
		t.Fatal("catalog or lock unexpectedly changed")
	}
}
