package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
)

func TestCommittedInternalOperationOverridesAreExact(t *testing.T) {
	path := filepath.Join("..", "..", "contracts", "overrides", "internal_operations.json")
	operations, err := loadInternalOperationOverrides(path)
	if err != nil {
		t.Fatalf("load internal operations: %v", err)
	}
	wantRoutes := map[contract.OperationID]string{
		"metric-topic.describe-metric-topics": "GET /DescribeMetricTopics",
		"metric-topic.describe-metric-topic":  "GET /DescribeMetricTopic",
		"metric-topic.create":                 "POST /CreateMetricTopic",
		"metric-topic.modify":                 "PUT /ModifyMetricTopic",
		"metric-topic.delete":                 "DELETE /DeleteMetricTopic",
		"collector.describe-rules-v2":         "GET /DescribeRulesV2",
	}
	if got, want := len(operations), len(wantRoutes); got != want {
		t.Fatalf("internal operations=%d, want %d", got, want)
	}
	for _, operation := range operations {
		wantRoute, ok := wantRoutes[operation.ID]
		if !ok {
			t.Fatalf("unexpected internal operation %q", operation.ID)
		}
		if got := operation.Wire.Method + " " + operation.Wire.Path; got != wantRoute {
			t.Fatalf("%s route=%q, want %q", operation.ID, got, wantRoute)
		}
		if operation.Visibility != "internal" {
			t.Fatalf("%s visibility=%q, want internal", operation.ID, operation.Visibility)
		}
		if operation.Wire.Codec != contract.CodecJSON || operation.Wire.RequestFormat != "json" {
			t.Fatalf("%s wire=%+v, want json codec and format", operation.ID, operation.Wire)
		}
		if !operation.Runtime.SupportsDryRun {
			t.Fatalf("%s must support dry-run", operation.ID)
		}
		delete(wantRoutes, operation.ID)
	}
	if len(wantRoutes) != 0 {
		t.Fatalf("missing internal operations: %#v", wantRoutes)
	}
	embedded, err := contract.LoadEmbedded()
	if err != nil {
		t.Fatalf("load embedded catalog: %v", err)
	}
	for _, override := range operations {
		embeddedOperation := operationByID(t, embedded.Operations, override.ID)
		if !reflect.DeepEqual(embeddedOperation, override) {
			t.Fatalf("embedded operation %q differs from override:\nembedded=%#v\noverride=%#v", override.ID, embeddedOperation, override)
		}
	}

	assertOperationSchema(t, operations, "metric-topic.describe-metric-topics", "query", nil, []string{
		"PageNumber", "PageSize", "Region", "ProjectId", "ProjectName", "TopicId", "TopicName",
		"FuzzySearchKey", "Description", "Tags", "IsFullName", "Favourite", "OrderByProject",
	})
	assertOperationSchema(t, operations, "metric-topic.describe-metric-topic", "query", []string{"TopicId"}, []string{"TopicId"})
	assertOperationSchema(t, operations, "metric-topic.create", "body",
		[]string{"ProjectId", "TopicName", "Ttl", "ShardCount"},
		[]string{"ProjectId", "TopicName", "Ttl", "ShardCount", "Description", "AutoSplit", "MaxSplitShard", "Tags"})
	assertOperationSchema(t, operations, "metric-topic.modify", "body",
		[]string{"TopicId"},
		[]string{"TopicId", "TopicName", "Description", "Ttl", "Favourite", "AutoSplit", "MaxSplitShard"})
	assertOperationSchema(t, operations, "metric-topic.delete", "body", []string{"TopicId"}, []string{"TopicId"})
	assertOperationSchema(t, operations, "collector.describe-rules-v2", "query", nil, []string{
		"ProjectId", "ProjectName", "IamProjectName", "RuleId", "RuleName", "TopicId", "TopicName",
		"LogType", "RuleType", "Pause", "Hidden", "PageNumber", "PageSize",
	})
	list := operationByID(t, operations, "metric-topic.describe-metric-topics")
	listQuery := list.InputSchema["query"].(map[string]any)
	listProperties := listQuery["properties"].(map[string]any)
	if favourite := listProperties["Favourite"].(map[string]any); favourite["type"] != "boolean" {
		t.Fatalf("metric topic Favourite schema=%#v, want boolean", favourite)
	}
	create := operationByID(t, operations, "metric-topic.create")
	createBody := create.InputSchema["body"].(map[string]any)
	createProperties := createBody["properties"].(map[string]any)
	tags := createProperties["Tags"].(map[string]any)
	items := tags["items"].(map[string]any)
	itemProperties := items["properties"].(map[string]any)
	if tags["type"] != "array" || items["type"] != "object" ||
		itemProperties["Key"].(map[string]any)["type"] != "string" ||
		itemProperties["Value"].(map[string]any)["type"] != "string" {
		t.Fatalf("metric topic Tags schema=%#v, want array of {Key,Value} objects", tags)
	}

	for _, id := range []contract.OperationID{
		"metric-topic.describe-metric-topics",
		"collector.describe-rules-v2",
	} {
		operation := operationByID(t, operations, id)
		wantItems := "Topics"
		if id == "collector.describe-rules-v2" {
			wantItems = "Rules"
		}
		want := &contract.PaginationSpec{
			Mode:            contract.PaginationPageNumber,
			PageNumberParam: "PageNumber",
			PageSizeParam:   "PageSize",
			ItemsField:      wantItems,
			TotalField:      "Total",
			DefaultPageSize: 100,
			MaxPages:        1000,
		}
		if !reflect.DeepEqual(operation.Pagination, want) {
			t.Fatalf("%s pagination=%+v, want %+v", id, operation.Pagination, want)
		}
	}
}

func TestMergeInternalOperationsRegeneratesCanonicalCatalog(t *testing.T) {
	public, err := buildOperationCatalogV2FromSource("v1", []sourceOperation{{
		ID:               "project.describe",
		Group:            "project",
		GroupTitle:       "Project",
		Action:           "DescribeProject",
		Resource:         "project",
		Verb:             "describe",
		Family:           "project",
		Method:           "GET",
		Path:             "/DescribeProject",
		Visibility:       "public",
		Summary:          "DescribeProject",
		InputSchema:      map[string]any{"query": map[string]any{"type": "object"}},
		OutputPolicy:     "envelope",
		ErrorRecovery:    "safe-retry",
		DocSource:        "Project",
		RiskLevel:        "low",
		SupportsDryRun:   true,
		IsEnvelopeOutput: true,
	}})
	if err != nil {
		t.Fatalf("build public catalog: %v", err)
	}
	override := contract.Operation{
		ID:         "internal.describe",
		Group:      "internal",
		GroupTitle: "Internal",
		Action:     "DescribeInternal",
		Resource:   "internal",
		Verb:       "describe",
		Family:     "internal",
		Visibility: "internal",
		Wire: contract.WireSpec{
			Method: "GET", Path: "/DescribeInternal", RequestFormat: "json", Codec: contract.CodecJSON,
		},
		InputSchema: contract.JSONSchema{"query": map[string]any{"type": "object"}},
		Runtime:     contract.RuntimeSpec{SupportsDryRun: true},
		Output:      contract.OutputSpec{Policy: "envelope", IsEnvelopeOutput: true},
		Docs:        contract.DocsSpec{Summary: "DescribeInternal", Source: "internal override"},
		Risk:        contract.RiskSpec{Level: "low", ErrorRecovery: "safe-retry"},
	}
	got, err := mergeInternalOperations(public, []contract.Operation{override})
	if err != nil {
		t.Fatalf("merge internal operations: %v", err)
	}
	if got := len(got.Operations); got != 2 {
		t.Fatalf("operations=%d, want 2", got)
	}
	if got.Operations[0].ID != "internal.describe" || got.Operations[1].ID != "project.describe" {
		t.Fatalf("operations are not canonically sorted: %#v", got.Operations)
	}
}

func TestMergeInternalOperationsIntoCheckedInCatalogUpdatesLock(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	catalogPath := filepath.Join(dir, "generated_catalog.json")
	lockPath := filepath.Join(dir, "operation-catalog-v2-lock.json")
	for source, target := range map[string]string{
		filepath.Join(root, "internal", "contract", "generated_catalog.json"): catalogPath,
		filepath.Join(root, "contracts", "operation-catalog-v2-lock.json"):    lockPath,
	} {
		raw, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	overridePath := filepath.Join(root, "contracts", "overrides", "internal_operations.json")
	if err := mergeInternalOperationsIntoCheckedInCatalog(catalogPath, lockPath, overridePath, root); err != nil {
		t.Fatalf("merge internal operations into checked-in catalog: %v", err)
	}
	raw, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := contract.Load(raw)
	if err != nil {
		t.Fatalf("load regenerated catalog: %v", err)
	}
	if got, want := len(catalog.Operations), 131; got != want {
		t.Fatalf("regenerated operations=%d, want %d", got, want)
	}
	raw, err = os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	var lock operationCatalogLock
	if err := json.Unmarshal(raw, &lock); err != nil {
		t.Fatalf("decode regenerated lock: %v", err)
	}
	if err := validateCommittedOperationCatalogLock(root, lock, catalog); err != nil {
		t.Fatalf("validate regenerated lock: %v", err)
	}
	foundOverride := false
	for _, input := range lock.Inputs {
		if input.Name == "override_internal_operations" {
			foundOverride = true
			break
		}
	}
	if !foundOverride {
		t.Fatal("regenerated lock does not include internal operation overrides")
	}
}

func TestMergeInternalOperationsRejectsCatalogThatDoesNotMatchExistingLock(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	catalogPath := filepath.Join(dir, "generated_catalog.json")
	lockPath := filepath.Join(dir, "operation-catalog-v2-lock.json")
	rawCatalog, err := os.ReadFile(filepath.Join(root, "internal", "contract", "generated_catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := contract.Load(rawCatalog)
	if err != nil {
		t.Fatal(err)
	}
	for i := range catalog.Operations {
		if catalog.Operations[i].Visibility == "public" {
			catalog.Operations[i].Docs.Summary += " tampered"
			break
		}
	}
	tamperedCatalog, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	tamperedCatalog = append(tamperedCatalog, '\n')
	if err := os.WriteFile(catalogPath, tamperedCatalog, 0o600); err != nil {
		t.Fatal(err)
	}
	rawLock, err := os.ReadFile(filepath.Join(root, "contracts", "operation-catalog-v2-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, rawLock, 0o600); err != nil {
		t.Fatal(err)
	}
	overridePath := filepath.Join(root, "contracts", "overrides", "internal_operations.json")
	err = mergeInternalOperationsIntoCheckedInCatalog(catalogPath, lockPath, overridePath, root)
	if err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("merge error=%v, want existing lock digest mismatch", err)
	}
	gotCatalog, readErr := os.ReadFile(catalogPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	gotLock, readErr := os.ReadFile(lockPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(gotCatalog, tamperedCatalog) || !bytes.Equal(gotLock, rawLock) {
		t.Fatal("failed merge changed catalog or lock")
	}
}

func TestMergeInternalOperationsRejectsChangedUnrelatedLockInput(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, inputName := range []string{
		"generator_main",
		"generator_source_operations",
	} {
		t.Run(inputName, func(t *testing.T) {
			assertMergeRejectsChangedLockedInput(t, repoRoot, inputName)
		})
	}
}

func TestMergeOnlyMutableInputsAreLimitedToInternalSources(t *testing.T) {
	root := t.TempDir()
	overridePath := filepath.Join(root, "contracts", "overrides", "internal_operations.json")
	got, err := mergeOnlyMutableInputs(root, overridePath)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"override_internal_operations":  "contracts/overrides/internal_operations.json",
		"generator_internal_operations": "internal/openapigen/internal_operations.go",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merge-only mutable inputs=%#v, want %#v", got, want)
	}
}

func assertMergeRejectsChangedLockedInput(t *testing.T, repoRoot, inputName string) {
	t.Helper()
	root := t.TempDir()
	catalogPath := filepath.Join(root, "generated_catalog.json")
	lockPath := filepath.Join(root, "operation-catalog-v2-lock.json")
	rawCatalog, err := os.ReadFile(filepath.Join(repoRoot, "internal", "contract", "generated_catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalogPath, rawCatalog, 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := contract.Load(rawCatalog)
	if err != nil {
		t.Fatal(err)
	}
	unrelatedPath := filepath.Join(root, "locked_generator_input.go")
	if err := os.WriteFile(unrelatedPath, []byte("expected input\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	expectedDigest, err := hashFile(unrelatedPath)
	if err != nil {
		t.Fatal(err)
	}
	lock := validTestOperationCatalogLock(t, catalog)
	lock.Inputs = []operationCatalogInput{{
		Name:   inputName,
		Path:   filepath.Base(unrelatedPath),
		SHA256: expectedDigest,
	}}
	if err := writeOperationCatalogLock(lockPath, lock); err != nil {
		t.Fatal(err)
	}
	rawLock, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unrelatedPath, []byte("tampered input\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	overridePath := filepath.Join(repoRoot, "contracts", "overrides", "internal_operations.json")
	err = mergeInternalOperationsIntoCheckedInCatalog(catalogPath, lockPath, overridePath, root)
	wantError := fmt.Sprintf("unexpected operation catalog input %q digest mismatch", inputName)
	if err == nil || !strings.Contains(err.Error(), wantError) {
		t.Fatalf("merge error=%v, want unrelated input digest mismatch", err)
	}
	gotCatalog, readErr := os.ReadFile(catalogPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	gotLock, readErr := os.ReadFile(lockPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(gotCatalog, rawCatalog) || !bytes.Equal(gotLock, rawLock) {
		t.Fatal("failed merge changed catalog or lock")
	}
}

func TestWriteOperationCatalogPairLockRenameFailureRestoresCatalog(t *testing.T) {
	catalog, err := contract.LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	catalogPath := filepath.Join(root, "generated_catalog.json")
	originalCatalog := []byte("original catalog\n")
	if err := os.WriteFile(catalogPath, originalCatalog, 0o600); err != nil {
		t.Fatal(err)
	}
	lock := validTestOperationCatalogLock(t, catalog)
	lockPath := filepath.Join(root, "lock-target")
	if err := os.Mkdir(lockPath, 0o700); err != nil {
		t.Fatal(err)
	}
	err = writeOperationCatalogPair(catalogPath, catalog, lockPath, lock)
	if err == nil || !strings.Contains(err.Error(), "replace operation catalog lock") {
		t.Fatalf("writeOperationCatalogPair error=%v, want lock replacement failure", err)
	}
	gotCatalog, readErr := os.ReadFile(catalogPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(gotCatalog, originalCatalog) {
		t.Fatalf("catalog changed after second staged write failed: got %q want %q", gotCatalog, originalCatalog)
	}
	entries, readErr := os.ReadDir(root)
	if readErr != nil {
		t.Fatal(readErr)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp-") {
			t.Fatalf("failed pair write left temporary file %q", entry.Name())
		}
	}
}

func TestWriteOperationCatalogPairRollbackFailureRetainsRecoveryCopy(t *testing.T) {
	catalog, err := contract.LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	catalogPath := filepath.Join(root, "generated_catalog.json")
	originalCatalog := []byte("original catalog\n")
	if err := os.WriteFile(catalogPath, originalCatalog, 0o600); err != nil {
		t.Fatal(err)
	}
	lock := validTestOperationCatalogLock(t, catalog)
	lockPath := filepath.Join(root, "operation-catalog-v2-lock.json")
	if err := os.WriteFile(lockPath, []byte("original lock\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	renameCalls := 0
	retainedPath := ""
	rename := func(oldPath, newPath string) error {
		renameCalls++
		switch renameCalls {
		case 1:
			return os.Rename(oldPath, newPath)
		case 2:
			return errors.New("injected lock rename failure")
		case 3:
			retainedPath = oldPath
			return errors.New("injected rollback rename failure")
		default:
			t.Fatalf("unexpected rename call %d: %s -> %s", renameCalls, oldPath, newPath)
			return nil
		}
	}
	err = writeOperationCatalogPairWithFileOps(
		catalogPath,
		catalog,
		lockPath,
		lock,
		rename,
		os.Remove,
	)
	if err == nil || retainedPath == "" || !strings.Contains(err.Error(), retainedPath) {
		t.Fatalf("error=%v retainedPath=%q, want retained rollback path in error", err, retainedPath)
	}
	retained, readErr := os.ReadFile(retainedPath)
	if readErr != nil {
		t.Fatalf("read retained rollback copy %q: %v", retainedPath, readErr)
	}
	if !bytes.Equal(retained, originalCatalog) {
		t.Fatalf("retained rollback copy=%q, want %q", retained, originalCatalog)
	}
}

func TestLoadInternalOperationOverridesRejectsInvalidEntries(t *testing.T) {
	valid := `{"operations":[{
		"id":"internal.describe","group":"internal","group_title":"Internal","action":"DescribeInternal",
		"resource":"internal","verb":"describe","family":"internal","visibility":"internal",
		"wire":{"method":"GET","path":"/DescribeInternal","request_format":"json","codec":"json"},
		"input_schema":{"query":{"type":"object"}},"runtime":{"supports_dry_run":true},
		"output":{"policy":"envelope","is_envelope_output":true},
		"docs":{"summary":"DescribeInternal","source":"internal override","usage_constraints":""},
		"risk":{"level":"low","error_recovery":"safe-retry"}}]}`
	operationJSON := strings.TrimSuffix(strings.TrimPrefix(valid, `{"operations":[`), `]}`)
	duplicate := `{"operations":[` + operationJSON + `,` + operationJSON + `]}`
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "public visibility", raw: strings.Replace(valid, `"visibility":"internal"`, `"visibility":"public"`, 1), want: "visibility"},
		{name: "duplicate id", raw: duplicate, want: "duplicate operation id"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "internal.json")
			if err := os.WriteFile(path, []byte(tt.raw), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadInternalOperationOverrides(path); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error=%v, want substring %q", err, tt.want)
			}
		})
	}
}

func assertOperationSchema(
	t *testing.T,
	operations []contract.Operation,
	id contract.OperationID,
	section string,
	wantRequired, wantProperties []string,
) {
	t.Helper()
	operation := operationByID(t, operations, id)
	schema := operation.InputSchema[section].(map[string]any)
	properties := schema["properties"].(map[string]any)
	gotProperties := make([]string, 0, len(properties))
	for name := range properties {
		gotProperties = append(gotProperties, name)
	}
	sortStrings(gotProperties)
	sortStrings(wantProperties)
	if !reflect.DeepEqual(gotProperties, wantProperties) {
		t.Fatalf("%s %s properties=%v, want %v", id, section, gotProperties, wantProperties)
	}
	gotRequired, _ := schema["required"].([]any)
	required := make([]string, 0, len(gotRequired))
	for _, value := range gotRequired {
		required = append(required, value.(string))
	}
	sortStrings(required)
	sortStrings(wantRequired)
	if len(required) == 0 && len(wantRequired) == 0 {
		return
	}
	if !reflect.DeepEqual(required, wantRequired) {
		t.Fatalf("%s %s required=%v, want %v", id, section, required, wantRequired)
	}
}

func operationByID(t *testing.T, operations []contract.Operation, id contract.OperationID) contract.Operation {
	t.Helper()
	for _, operation := range operations {
		if operation.ID == id {
			return operation
		}
	}
	t.Fatalf("missing operation %q", id)
	return contract.Operation{}
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}
