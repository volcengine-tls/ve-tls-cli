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

func TestBuildOperationCatalogV2UsesTypedWireDocsAndOutput(t *testing.T) {
	source := []sourceOperation{{
		ID:               "project.get",
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
		UsageConstraints: "read only",
		RiskLevel:        "low",
		SupportsDryRun:   true,
		IsEnvelopeOutput: true,
	}}
	got, err := buildOperationCatalogV2FromSource("v1", source)
	if err != nil {
		t.Fatalf("buildOperationCatalogV2FromSource: %v", err)
	}
	if len(got.Operations) != 1 {
		t.Fatalf("operations=%d", len(got.Operations))
	}
	operation := got.Operations[0]
	if operation.ID != "project.get" || operation.GroupTitle != "Project" {
		t.Fatalf("identity=%+v", operation)
	}
	if operation.Wire != (contract.WireSpec{
		Method: "GET", Path: "/DescribeProject", RequestFormat: "json", Codec: contract.CodecJSON,
	}) {
		t.Fatalf("wire=%+v", operation.Wire)
	}
	if operation.Docs.Summary != "DescribeProject" || operation.Docs.Source != "Project" {
		t.Fatalf("docs=%+v", operation.Docs)
	}
	if !operation.Output.IsEnvelopeOutput || operation.Output.Policy != "envelope" {
		t.Fatalf("output=%+v", operation.Output)
	}
}

func TestSourceOperationsProjectToCanonicalCatalogWithoutIdentityDrift(t *testing.T) {
	doc := swaggerDoc{Paths: map[string]swaggerPathItem{
		"/DescribeProject": {
			Get: &swaggerOp{Summary: "DescribeProject", Tags: []string{"Project"}},
		},
	}}
	source := buildSourceOperations(
		doc,
		map[string]string{"Project": "project"},
		map[string]string{"Project": "Project"},
		map[string]apiDocEntry{},
		toolCatalogOverrides{},
	)
	v2, err := buildOperationCatalogV2FromSource("v1", source)
	if err != nil {
		t.Fatalf("buildOperationCatalogV2FromSource: %v", err)
	}
	if len(source) != 1 || len(v2.Operations) != 1 {
		t.Fatalf("projection counts source=%d canonical=%d", len(source), len(v2.Operations))
	}
	if source[0].GroupTitle != v2.Operations[0].GroupTitle {
		t.Fatalf("group title drift: source=%q v2=%q", source[0].GroupTitle, v2.Operations[0].GroupTitle)
	}
	if source[0].ID != string(v2.Operations[0].ID) ||
		source[0].Method != v2.Operations[0].Wire.Method ||
		source[0].Path != v2.Operations[0].Wire.Path {
		t.Fatalf("identity drift: source=%+v canonical=%+v", source[0], v2.Operations[0])
	}
	rebuilt, err := contract.RebuildLegacyToolV1(v2, v2.Operations[0])
	if err != nil {
		t.Fatalf("RebuildLegacyToolV1: %v", err)
	}
	if rebuilt.ID != source[0].ID || rebuilt.Method != source[0].Method || rebuilt.Path != source[0].Path {
		t.Fatalf("legacy compatibility projection drift: rebuilt=%+v source=%+v", rebuilt, source[0])
	}
}

func TestSourcePaginationInferenceReachesCanonicalOperation(t *testing.T) {
	doc := swaggerDoc{Paths: map[string]swaggerPathItem{
		"/DescribeProjects": {
			Get: &swaggerOp{
				Summary: "DescribeProjects",
				Tags:    []string{"Project"},
				Parameters: []swaggerParam{
					{Name: "PageNumber", In: "query", Type: "integer"},
					{Name: "PageSize", In: "query", Type: "integer"},
				},
			},
		},
		"/DescribeProject": {
			Get: &swaggerOp{Summary: "DescribeProject", Tags: []string{"Project"}},
		},
	}}
	source := buildSourceOperations(
		doc,
		map[string]string{"Project": "project"},
		map[string]string{"Project": "Project"},
		map[string]apiDocEntry{},
		toolCatalogOverrides{},
	)
	byID := make(map[string]sourceOperation, len(source))
	for _, operation := range source {
		byID[operation.ID] = operation
	}
	if len(source) != 2 || !byID["project.describe-projects"].SupportsAll || byID["project.describe-project"].SupportsAll {
		t.Fatalf("source pagination inference drift: %+v", source)
	}
	catalog, err := buildOperationCatalogV2FromSource("v1", source)
	if err != nil {
		t.Fatalf("buildOperationCatalogV2FromSource: %v", err)
	}
	if len(catalog.Operations) != 2 {
		t.Fatalf("operations=%d", len(catalog.Operations))
	}
	var pagination *contract.PaginationSpec
	for _, operation := range catalog.Operations {
		if operation.ID == "project.describe-projects" {
			pagination = operation.Pagination
			break
		}
	}
	if pagination == nil || pagination.Mode != contract.PaginationPageNumber || pagination.ItemsField != "Projects" {
		t.Fatalf("canonical pagination drift: %+v", pagination)
	}
}

func TestCatalogV2PaginationOverridesAreExplicitAndComplete(t *testing.T) {
	overrides := operationPaginationOverrides()
	if got, want := len(overrides), 21; got != want {
		t.Fatalf("pagination overrides=%d, want %d", got, want)
	}
	for id, expectedItems := range map[contract.OperationID]string{
		"alarm.describe-alarm-content-templates":        "AlarmContentTemplates",
		"alarm.describe-alarm-notify-groups":            "AlarmNotifyGroups",
		"alarm.describe-alarm-webhook-integrations":     "WebhookIntegrations",
		"alarm.describe-alarms":                         "Alarms",
		"collector.describe-bound-host-groups":          "HostGroupInfos",
		"collector.describe-rules":                      "RuleInfos",
		"consumer-group.describe-consumer-groups":       "ConsumerGroups",
		"etl.describe-e-t-l-tasks":                      "Tasks",
		"host-group.describe-host-group-rules":          "RuleInfos",
		"host-group.describe-hosts":                     "HostInfos",
		"import.describe-import-tasks":                  "TaskInfo",
		"log.describe-download-tasks":                   "Tasks",
		"log-back-flow.describe":                        "LogBackFlowTasks",
		"processor.describe-processor-bindings":         "Items",
		"processor.describe-processors":                 "Items",
		"project.describe-projects":                     "Projects",
		"schedule-sql-task.describe-schedule-sql-tasks": "Tasks",
		"shard.describe":                                "Shards",
		"shipper.describe-shippers":                     "Shippers",
		"topic.describe-topics":                         "Topics",
		"trace.describe-trace-instances":                "TraceInstances",
	} {
		spec, ok := overrides[id]
		if !ok {
			t.Fatalf("missing pagination override %q", id)
		}
		if spec.ItemsField != expectedItems {
			t.Fatalf("%s items_field=%q, want %q", id, spec.ItemsField, expectedItems)
		}
		if spec.Mode != contract.PaginationPageNumber ||
			spec.PageNumberParam != "PageNumber" ||
			spec.PageSizeParam != "PageSize" ||
			spec.DefaultPageSize != 100 ||
			spec.MaxPages != 1000 {
			t.Fatalf("%s pagination=%+v", id, spec)
		}
		if id == "topic.describe-topics" {
			if spec.CursorParam != "Cursor" {
				t.Fatalf("%s cursor_param=%q, want Cursor", id, spec.CursorParam)
			}
		} else if spec.CursorParam != "" {
			t.Fatalf("%s unexpectedly declares cursor_param=%q", id, spec.CursorParam)
		}
	}
}

func TestEmbeddedCatalogTopicPaginationPreservesLegacyCursorConflict(t *testing.T) {
	catalog, err := contract.LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}
	for _, operation := range catalog.Operations {
		if operation.ID != "topic.describe-topics" {
			continue
		}
		if operation.Pagination == nil || operation.Pagination.CursorParam != "Cursor" {
			t.Fatalf("topic pagination=%+v, want CursorParam=Cursor", operation.Pagination)
		}
		query, _ := operation.InputSchema["query"].(map[string]any)
		properties, _ := query["properties"].(map[string]any)
		if _, ok := properties["Cursor"]; !ok {
			t.Fatal("topic pagination declares CursorParam absent from query schema")
		}
		return
	}
	t.Fatal("embedded catalog is missing topic.describe-topics")
}

func TestCatalogV2CodecOverridesAreStable(t *testing.T) {
	tests := []struct {
		method string
		path   string
		want   contract.CodecID
	}{
		{method: "POST", path: "/PutLogs", want: contract.CodecPutLogs},
		{method: "POST", path: "/WebTracks", want: contract.CodecWebTracks},
		{method: "POST", path: "/ConsumeLogs", want: contract.CodecConsumeLogs},
		{method: "GET", path: "/DescribeProjects", want: contract.CodecJSON},
	}
	for _, tt := range tests {
		if got := operationCodecForWire(tt.method, tt.path); got != tt.want {
			t.Fatalf("%s %s codec=%q, want %q", tt.method, tt.path, got, tt.want)
		}
	}
}

func TestWriteOperationCatalogV2IsDeterministic(t *testing.T) {
	catalog := contract.Catalog{
		SchemaVersion:   contract.CatalogV2SchemaVersion,
		ContractVersion: "v1",
		DigestAlgorithm: contract.CatalogV2DigestAlgorithm,
		ContextSchema:   contract.JSONSchema{"type": "object"},
		ExecutionSchema: contract.JSONSchema{"type": "object"},
		Operations:      []contract.Operation{},
	}
	dir := t.TempDir()
	first := filepath.Join(dir, "first.json")
	second := filepath.Join(dir, "second.json")
	if err := writeOperationCatalogJSON(first, catalog); err != nil {
		t.Fatalf("write first: %v", err)
	}
	if err := writeOperationCatalogJSON(second, catalog); err != nil {
		t.Fatalf("write second: %v", err)
	}
	firstRaw, _ := os.ReadFile(first)
	secondRaw, _ := os.ReadFile(second)
	if !bytes.Equal(firstRaw, secondRaw) {
		t.Fatal("consecutive catalog writes differ")
	}
}

func TestBuildOperationCatalogLockHashesRelativeInputs(t *testing.T) {
	root := t.TempDir()
	spec := filepath.Join(root, "swagger.json")
	if err := os.WriteFile(spec, []byte(`{"paths":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog := contract.Catalog{
		SchemaVersion:   contract.CatalogV2SchemaVersion,
		ContractVersion: "v1",
		DigestAlgorithm: contract.CatalogV2DigestAlgorithm,
		ContextSchema:   contract.JSONSchema{"type": "object"},
		ExecutionSchema: contract.JSONSchema{"type": "object"},
		Operations:      []contract.Operation{},
	}
	lock, err := buildOperationCatalogLock(root, "source", catalog, map[string]string{"spec": spec})
	if err != nil {
		t.Fatalf("buildOperationCatalogLock: %v", err)
	}
	if len(lock.Inputs) != 1 || filepath.IsAbs(lock.Inputs[0].Path) {
		t.Fatalf("lock inputs=%+v", lock.Inputs)
	}
	if lock.Inputs[0].Path != "swagger.json" || lock.Inputs[0].SHA256 == "" {
		t.Fatalf("lock input=%+v", lock.Inputs[0])
	}
	if lock.OperationCount != 0 || lock.CatalogDigest == "" {
		t.Fatalf("lock summary=%+v", lock)
	}
}

func TestHashGenerationInputIncludesOnlyMarkdownFromDocDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "api.md"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "noise.txt"), []byte("noise-v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := hashGenerationInput(root)
	if err != nil {
		t.Fatalf("hash first: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "noise.txt"), []byte("noise-v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := hashGenerationInput(root)
	if err != nil {
		t.Fatalf("hash after noise change: %v", err)
	}
	if first != second {
		t.Fatal("non-markdown file changed API docs input digest")
	}
	if err := os.WriteFile(filepath.Join(root, "api.md"), []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	third, err := hashGenerationInput(root)
	if err != nil {
		t.Fatalf("hash after markdown change: %v", err)
	}
	if second == third {
		t.Fatal("markdown file change did not affect API docs input digest")
	}
}

func TestBuildOperationCatalogLockListsActualMarkdownInputs(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "docs")
	if err := os.MkdirAll(filepath.Join(docs, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{
		filepath.Join(docs, "one.md"):           "one",
		filepath.Join(docs, "nested", "two.MD"): "two",
		filepath.Join(docs, "ignored.json"):     "{}",
	} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	catalog := contract.Catalog{
		SchemaVersion:   contract.CatalogV2SchemaVersion,
		ContractVersion: "v1",
		DigestAlgorithm: contract.CatalogV2DigestAlgorithm,
		ContextSchema:   contract.JSONSchema{"type": "object"},
		ExecutionSchema: contract.JSONSchema{"type": "object"},
	}
	lock, err := buildOperationCatalogLock(root, "source", catalog, map[string]string{"api_doc_root": docs})
	if err != nil {
		t.Fatalf("buildOperationCatalogLock: %v", err)
	}
	if got, want := len(lock.Inputs), 2; got != want {
		t.Fatalf("lock inputs=%d, want %d: %+v", got, want, lock.Inputs)
	}
	wantPaths := []string{"docs/nested/two.MD", "docs/one.md"}
	for i, input := range lock.Inputs {
		if input.Path != wantPaths[i] || filepath.IsAbs(input.Path) {
			t.Fatalf("input[%d]=%+v, want path %q", i, input, wantPaths[i])
		}
	}
}

func TestCommittedOperationCatalogLockMatchesEmbeddedCatalogAndInputs(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "contracts", "operation-catalog-v2-lock.json"))
	if err != nil {
		t.Fatalf("read committed lock: %v", err)
	}
	var lock operationCatalogLock
	if err := json.Unmarshal(raw, &lock); err != nil {
		t.Fatalf("decode committed lock: %v", err)
	}
	catalog, err := contract.LoadEmbedded()
	if err != nil {
		t.Fatalf("load embedded catalog: %v", err)
	}
	root := filepath.Join("..", "..")
	if err := validateCommittedOperationCatalogLock(root, lock, catalog); err != nil {
		t.Fatalf("validate committed lock: %v", err)
	}
	if lock.GenerationMode != "bootstrap" && lock.GenerationMode != "source" {
		t.Fatalf("generation mode=%q", lock.GenerationMode)
	}
}

func TestSourceLockAllowsMissingExternalInputs(t *testing.T) {
	catalog, err := contract.LoadEmbedded()
	if err != nil {
		t.Fatalf("load embedded catalog: %v", err)
	}
	digest, err := contract.CatalogV2Digest(catalog)
	if err != nil {
		t.Fatalf("catalog digest: %v", err)
	}
	lock := operationCatalogLock{
		SchemaVersion:   1,
		ContractVersion: catalog.ContractVersion,
		DigestAlgorithm: catalog.DigestAlgorithm,
		CatalogDigest:   digest,
		OperationCount:  len(catalog.Operations),
		GenerationMode:  "source",
		Inputs: []operationCatalogInput{{
			Name: "spec", Path: "../external/swagger.json",
			SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}},
	}
	root := filepath.Join(t.TempDir(), "checkout")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := validateCommittedOperationCatalogLock(root, lock, catalog); err != nil {
		t.Fatalf("source lock with unavailable external input: %v", err)
	}
	lock.GenerationMode = "bootstrap"
	if err := validateCommittedOperationCatalogLock(root, lock, catalog); err == nil {
		t.Fatal("bootstrap lock accepted a missing input")
	}
}

func TestSourceLockRejectsExistingInputDigestMismatch(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "swagger.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog, err := contract.LoadEmbedded()
	if err != nil {
		t.Fatalf("load embedded catalog: %v", err)
	}
	lock := validTestOperationCatalogLock(t, catalog)
	lock.GenerationMode = "source"
	lock.Inputs[0].Path = "swagger.json"
	if err := validateCommittedOperationCatalogLock(root, lock, catalog); err == nil {
		t.Fatal("source lock accepted a digest mismatch for an existing input")
	}
}

func TestValidateOperationCatalogLockRejectsMalformedInputs(t *testing.T) {
	catalog, err := contract.LoadEmbedded()
	if err != nil {
		t.Fatalf("load embedded catalog: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*operationCatalogLock)
		want   string
	}{
		{
			name: "duplicate name",
			mutate: func(lock *operationCatalogLock) {
				lock.Inputs = append(lock.Inputs, operationCatalogInput{
					Name: lock.Inputs[0].Name, Path: "other.go", SHA256: lock.Inputs[0].SHA256,
				})
			},
			want: "duplicate operation catalog lock input name",
		},
		{
			name: "duplicate path",
			mutate: func(lock *operationCatalogLock) {
				lock.Inputs = append(lock.Inputs, operationCatalogInput{
					Name: "other", Path: lock.Inputs[0].Path, SHA256: lock.Inputs[0].SHA256,
				})
			},
			want: "duplicate operation catalog lock input path",
		},
		{
			name: "absolute path",
			mutate: func(lock *operationCatalogLock) {
				lock.Inputs[0].Path = filepath.Join(string(filepath.Separator), "tmp", "swagger.json")
			},
			want: "must be relative",
		},
		{
			name: "invalid sha",
			mutate: func(lock *operationCatalogLock) {
				lock.Inputs[0].SHA256 = "not-a-sha"
			},
			want: "invalid sha256",
		},
		{
			name: "surrounding whitespace",
			mutate: func(lock *operationCatalogLock) {
				lock.Inputs[0].Path = " swagger.json"
			},
			want: "surrounding whitespace",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lock := validTestOperationCatalogLock(t, catalog)
			tt.mutate(&lock)
			if err := validateOperationCatalogLock(lock, catalog); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validateOperationCatalogLock error=%v, want substring %q", err, tt.want)
			}
		})
	}
}

func validTestOperationCatalogLock(t *testing.T, catalog contract.Catalog) operationCatalogLock {
	t.Helper()
	digest, err := contract.CatalogV2Digest(catalog)
	if err != nil {
		t.Fatalf("catalog digest: %v", err)
	}
	return operationCatalogLock{
		SchemaVersion:   1,
		ContractVersion: catalog.ContractVersion,
		DigestAlgorithm: catalog.DigestAlgorithm,
		CatalogDigest:   digest,
		OperationCount:  len(catalog.Operations),
		GenerationMode:  "bootstrap",
		Inputs: []operationCatalogInput{{
			Name: "spec", Path: "swagger.json",
			SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}},
	}
}

func TestOperationPaginationOverridesReturnsIndependentMaps(t *testing.T) {
	first := operationPaginationOverrides()
	second := operationPaginationOverrides()
	delete(first, "project.describe-projects")
	if reflect.DeepEqual(first, second) {
		t.Fatal("operationPaginationOverrides returned shared mutable map")
	}
	if _, ok := second["project.describe-projects"]; !ok {
		t.Fatal("mutation leaked across pagination override calls")
	}
}
