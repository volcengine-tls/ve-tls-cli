package contract

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestEmbeddedCatalogHasUnifiedSchemasAnd131Operations(t *testing.T) {
	catalog, err := LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}
	if got, want := len(catalog.Operations), 131; got != want {
		t.Fatalf("operation count=%d, want %d", got, want)
	}
	if err := Validate(catalog); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	raw, err := json.Marshal(catalog)
	if err != nil {
		t.Fatalf("marshal catalog: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	for _, key := range []string{"context_schema", "execution_schema"} {
		if _, ok := document[key]; !ok {
			t.Fatalf("catalog missing top-level %q", key)
		}
	}
	operations, ok := document["operations"].([]any)
	if !ok {
		t.Fatalf("operations has type %T", document["operations"])
	}
	for i, value := range operations {
		operation := value.(map[string]any)
		for _, key := range []string{"context_schema", "execution_schema"} {
			if _, ok := operation[key]; ok {
				t.Fatalf("operation %d duplicates top-level %q", i, key)
			}
		}
	}
}

func TestExpandContextSchemaResolvesExecutionRefAndPreservesOverlay(t *testing.T) {
	execution := JSONSchema{
		"type": "object",
		"properties": map[string]any{
			"dry_run": map[string]any{"type": "boolean"},
		},
	}
	context := JSONSchema{
		"type": "object",
		"properties": map[string]any{
			"execution": map[string]any{
				"$ref":        "#/execution_schema",
				"default":     map[string]any{},
				"description": "overlay",
			},
		},
	}
	got, err := ExpandContextSchema(context, execution)
	if err != nil {
		t.Fatalf("ExpandContextSchema: %v", err)
	}
	field := got["properties"].(map[string]any)["execution"].(map[string]any)
	if _, ok := field["$ref"]; ok {
		t.Fatalf("expanded field still has $ref: %#v", field)
	}
	if field["description"] != "overlay" {
		t.Fatalf("overlay description=%#v", field["description"])
	}
	if !reflect.DeepEqual(field["properties"], execution["properties"]) {
		t.Fatalf("execution properties mismatch: %#v", field["properties"])
	}

	field["description"] = "mutated"
	if context["properties"].(map[string]any)["execution"].(map[string]any)["description"] != "overlay" {
		t.Fatal("expansion mutated source context schema")
	}
}

func TestCatalogV2DigestIgnoresOperationOrderAndUsesFixedAlgorithm(t *testing.T) {
	catalog, err := LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}
	if catalog.DigestAlgorithm != CatalogV2DigestAlgorithm {
		t.Fatalf("digest algorithm=%q, want %q", catalog.DigestAlgorithm, CatalogV2DigestAlgorithm)
	}
	first, err := CatalogV2Digest(catalog)
	if err != nil {
		t.Fatalf("CatalogV2Digest: %v", err)
	}
	reordered := catalog
	reordered.Operations = append([]Operation(nil), catalog.Operations...)
	for left, right := 0, len(reordered.Operations)-1; left < right; left, right = left+1, right-1 {
		reordered.Operations[left], reordered.Operations[right] = reordered.Operations[right], reordered.Operations[left]
	}
	second, err := CatalogV2Digest(reordered)
	if err != nil {
		t.Fatalf("CatalogV2Digest reordered: %v", err)
	}
	if first != second {
		t.Fatalf("digest depends on operation order: %s != %s", first, second)
	}
}

func TestLegacyToolDigestV1PreservesGoJSONFieldOrderAndNumberSemantics(t *testing.T) {
	tool := LegacyToolV1{
		ID:          "group.action",
		Group:       "group",
		Action:      "Action",
		InputSchema: JSONSchema{"minimum": float64(1), "type": "number"},
	}
	raw, err := json.Marshal(tool)
	if err != nil {
		t.Fatalf("marshal legacy tool: %v", err)
	}
	if !strings.HasPrefix(string(raw), `{"id":"group.action","group":"group","action":"Action"`) {
		t.Fatalf("legacy field order changed: %s", raw)
	}
	const want = "41819ae3a334fe0d17540ce2035f531613480676a54be3b8e6526a8cc8d0f19c"
	if got := LegacyToolDigestV1(tool); got != want {
		t.Fatalf("legacy digest=%q, want %q", got, want)
	}
}

func TestNewCatalogDeduplicatesSchemasAndRebuildsLegacyTool(t *testing.T) {
	execution := JSONSchema{
		"description": "standalone execution",
		"type":        "object",
		"properties":  map[string]any{"dry_run": map[string]any{"type": "boolean"}},
	}
	contextExecution := cloneSchema(execution)
	contextExecution["description"] = "context overlay"
	contextExecution["default"] = map[string]any{}
	context := JSONSchema{
		"type": "object",
		"properties": map[string]any{
			"execution": map[string]any(contextExecution),
		},
	}
	operation := Operation{
		ID:          "group.list",
		Group:       "group",
		GroupTitle:  "Things",
		Action:      "DescribeThings",
		Resource:    "things",
		Verb:        "list",
		Family:      "group",
		Visibility:  "public",
		Wire:        WireSpec{Method: "GET", Path: "/ListThings", RequestFormat: "json", Codec: CodecJSON},
		InputSchema: JSONSchema{"query": map[string]any{"type": "object", "properties": map[string]any{"PageNumber": map[string]any{"type": "integer"}, "PageSize": map[string]any{"type": "integer"}}}},
		Pagination: &PaginationSpec{
			Mode:            PaginationPageNumber,
			PageNumberParam: "PageNumber",
			PageSizeParam:   "PageSize",
			ItemsField:      "Things",
			TotalField:      "Total",
			DefaultPageSize: 100,
			MaxPages:        1000,
		},
		Runtime: RuntimeSpec{SupportsDryRun: true},
		Output:  OutputSpec{Policy: "envelope", IsEnvelopeOutput: true},
		Docs:    DocsSpec{Summary: "ListThings", Source: "Things", UsageConstraints: "read only"},
		Risk:    RiskSpec{Level: "low", ErrorRecovery: "safe-retry"},
	}
	got, err := NewCatalog("v1", context, execution, []Operation{operation})
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	built := got.Operations[0]
	if built.GroupTitle != "Things" {
		t.Fatalf("group title=%q", built.GroupTitle)
	}
	if built.Pagination == nil || *built.Pagination != (PaginationSpec{
		Mode:            PaginationPageNumber,
		PageNumberParam: "PageNumber",
		PageSizeParam:   "PageSize",
		ItemsField:      "Things",
		TotalField:      "Total",
		DefaultPageSize: 100,
		MaxPages:        1000,
	}) {
		t.Fatalf("pagination=%+v", built.Pagination)
	}
	rebuilt, err := RebuildLegacyToolV1(got, built)
	if err != nil {
		t.Fatalf("RebuildLegacyToolV1: %v", err)
	}
	legacy := LegacyToolV1{
		ID:               "group.list",
		Group:            "group",
		Action:           "DescribeThings",
		Resource:         "things",
		Verb:             "list",
		Family:           "group",
		Method:           "GET",
		Path:             "/ListThings",
		Visibility:       "public",
		Summary:          "ListThings",
		InputSchema:      operation.InputSchema,
		ContextSchema:    context,
		ExecutionSchema:  execution,
		OutputPolicy:     "envelope",
		ErrorRecovery:    "safe-retry",
		DocSource:        "Things",
		UsageConstraints: "read only",
		RiskLevel:        "low",
		SupportsDryRun:   true,
		SupportsAll:      true,
		IsEnvelopeOutput: true,
	}
	if !reflect.DeepEqual(rebuilt, legacy) {
		t.Fatalf("legacy rebuild mismatch:\n got=%#v\nwant=%#v", rebuilt, legacy)
	}
	contextExecution = got.ContextSchema["properties"].(map[string]any)["execution"].(map[string]any)
	if contextExecution["$ref"] != "#/execution_schema" {
		t.Fatalf("context execution ref=%#v", contextExecution["$ref"])
	}
	if _, ok := contextExecution["properties"]; ok {
		t.Fatalf("context execution duplicated properties: %#v", contextExecution)
	}
}

func TestNewCatalogSortsOperationsAndRejectsMissingInputs(t *testing.T) {
	first := validTestOperation()
	second := first
	second.ID = "group.alpha"
	second.Action = "Alpha"
	second.Wire.Path = "/Alpha"
	first.ID = "group.zulu"
	first.Action = "Zulu"
	first.Wire.Path = "/Zulu"
	catalog, err := NewCatalog(
		"v1",
		JSONSchema{"type": "object", "properties": map[string]any{"execution": map[string]any{"type": "object"}}},
		JSONSchema{"type": "object"},
		[]Operation{first, second},
	)
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	if catalog.Operations[0].ID != "group.alpha" || catalog.Operations[1].ID != "group.zulu" {
		t.Fatalf("operations not sorted: %+v", catalog.Operations)
	}
	if _, err := NewCatalog("", catalog.ContextSchema, catalog.ExecutionSchema, catalog.Operations); err == nil || !strings.Contains(err.Error(), "contract version") {
		t.Fatalf("missing version error=%v", err)
	}
}

func TestValidateRejectsDuplicateWireAndUnknownCodec(t *testing.T) {
	base := Operation{
		ID:          "group.one",
		Group:       "group",
		GroupTitle:  "Group",
		Action:      "One",
		Resource:    "one",
		Verb:        "get",
		Family:      "group",
		Visibility:  "public",
		Wire:        WireSpec{Method: "GET", Path: "/One", RequestFormat: "json", Codec: CodecJSON},
		InputSchema: JSONSchema{},
		Runtime:     RuntimeSpec{SupportsDryRun: true},
		Output:      OutputSpec{Policy: "envelope", IsEnvelopeOutput: true},
		Docs:        DocsSpec{Summary: "One", Source: "Group"},
		Risk:        RiskSpec{Level: "low", ErrorRecovery: "safe-retry"},
	}
	catalog := Catalog{
		SchemaVersion:   CatalogV2SchemaVersion,
		ContractVersion: "v1",
		DigestAlgorithm: CatalogV2DigestAlgorithm,
		ContextSchema:   JSONSchema{"type": "object"},
		ExecutionSchema: JSONSchema{"type": "object"},
		Operations:      []Operation{base, base},
	}
	catalog.Operations[1].ID = "group.two"
	catalog.Operations[1].Action = "Two"
	if err := Validate(catalog); err == nil || !strings.Contains(err.Error(), "duplicate wire") {
		t.Fatalf("duplicate wire validation error=%v", err)
	}

	catalog.Operations[1].Wire.Path = "/Two"
	catalog.Operations[1].Wire.Codec = "protobuf-magic"
	if err := Validate(catalog); err == nil || !strings.Contains(err.Error(), "unsupported codec") {
		t.Fatalf("unknown codec validation error=%v", err)
	}
}

func TestValidateRejectsDuplicateGroupActionRoute(t *testing.T) {
	first := Operation{
		ID:          "group.one",
		Group:       "group",
		GroupTitle:  "Group",
		Action:      "DescribeThing",
		Resource:    "thing",
		Verb:        "describe",
		Family:      "group",
		Visibility:  "public",
		Wire:        WireSpec{Method: "GET", Path: "/One", RequestFormat: "json", Codec: CodecJSON},
		InputSchema: JSONSchema{},
		Output:      OutputSpec{Policy: "envelope", IsEnvelopeOutput: true},
		Docs:        DocsSpec{Summary: "One", Source: "Group"},
		Risk:        RiskSpec{Level: "low", ErrorRecovery: "safe-retry"},
	}
	second := first
	second.ID = "group.two"
	second.Wire.Path = "/Two"
	catalog := Catalog{
		SchemaVersion:   CatalogV2SchemaVersion,
		ContractVersion: "v1",
		DigestAlgorithm: CatalogV2DigestAlgorithm,
		ContextSchema:   JSONSchema{"type": "object"},
		ExecutionSchema: JSONSchema{"type": "object"},
		Operations:      []Operation{first, second},
	}
	if err := Validate(catalog); err == nil || !strings.Contains(err.Error(), "duplicate operation route") {
		t.Fatalf("duplicate route validation error=%v", err)
	}
}

func TestValidateRejectsInvalidPaginationAndSchemaReferences(t *testing.T) {
	catalog := Catalog{
		SchemaVersion:   CatalogV2SchemaVersion,
		ContractVersion: "v1",
		DigestAlgorithm: CatalogV2DigestAlgorithm,
		ContextSchema: JSONSchema{
			"type": "object",
			"properties": map[string]any{
				"execution": map[string]any{"$ref": "#/missing"},
			},
		},
		ExecutionSchema: JSONSchema{"type": "object"},
		Operations: []Operation{{
			ID:         "group.list",
			Group:      "group",
			GroupTitle: "Group",
			Action:     "List",
			Resource:   "items",
			Verb:       "list",
			Family:     "group",
			Visibility: "public",
			Wire:       WireSpec{Method: "GET", Path: "/List", RequestFormat: "json", Codec: CodecJSON},
			Pagination: &PaginationSpec{
				Mode:            PaginationPageNumber,
				PageNumberParam: "PageNumber",
			},
			InputSchema: JSONSchema{"query": map[string]any{
				"type":       "object",
				"properties": map[string]any{"PageNumber": map[string]any{"type": "integer"}},
			}},
			Output: OutputSpec{Policy: "envelope", IsEnvelopeOutput: true},
			Docs:   DocsSpec{Summary: "List", Source: "Group"},
			Risk:   RiskSpec{Level: "low", ErrorRecovery: "safe-retry"},
		}},
	}
	if err := Validate(catalog); err == nil || !strings.Contains(err.Error(), "unresolved schema ref") {
		t.Fatalf("schema ref validation error=%v", err)
	}

	catalog.ContextSchema["properties"].(map[string]any)["execution"] = map[string]any{"$ref": "#/execution_schema"}
	if err := Validate(catalog); err == nil || !strings.Contains(err.Error(), "page_size_param") {
		t.Fatalf("pagination validation error=%v", err)
	}
}

func TestValidateRejectsIncompleteOperationMetadataAndInvalidEnums(t *testing.T) {
	valid := Operation{
		ID:          "group.get",
		Group:       "group",
		GroupTitle:  "Group",
		Action:      "Get",
		Resource:    "item",
		Verb:        "get",
		Family:      "group",
		Visibility:  "public",
		Wire:        WireSpec{Method: "GET", Path: "/Get", RequestFormat: "json", Codec: CodecJSON},
		InputSchema: JSONSchema{},
		Runtime:     RuntimeSpec{SupportsDryRun: true},
		Output:      OutputSpec{Policy: "envelope", IsEnvelopeOutput: true},
		Docs:        DocsSpec{Summary: "Get", Source: "Group"},
		Risk:        RiskSpec{Level: "low", ErrorRecovery: "safe-retry"},
	}
	base := Catalog{
		SchemaVersion:   CatalogV2SchemaVersion,
		ContractVersion: "v1",
		DigestAlgorithm: CatalogV2DigestAlgorithm,
		ContextSchema:   JSONSchema{"type": "object"},
		ExecutionSchema: JSONSchema{"type": "object"},
		Operations:      []Operation{valid},
	}
	tests := []struct {
		name   string
		mutate func(*Operation)
		want   string
	}{
		{name: "resource", mutate: func(operation *Operation) { operation.Resource = "" }, want: "resource"},
		{name: "verb", mutate: func(operation *Operation) { operation.Verb = "" }, want: "verb"},
		{name: "family", mutate: func(operation *Operation) { operation.Family = "" }, want: "family"},
		{name: "visibility", mutate: func(operation *Operation) { operation.Visibility = "secret" }, want: "visibility"},
		{name: "method", mutate: func(operation *Operation) { operation.Wire.Method = "CONNECT" }, want: "method"},
		{name: "request_format", mutate: func(operation *Operation) { operation.Wire.RequestFormat = "xml" }, want: "request_format"},
		{name: "output", mutate: func(operation *Operation) { operation.Output.Policy = "magic" }, want: "output policy"},
		{name: "summary", mutate: func(operation *Operation) { operation.Docs.Summary = "" }, want: "summary"},
		{name: "doc_source", mutate: func(operation *Operation) { operation.Docs.Source = "" }, want: "docs source"},
		{name: "risk", mutate: func(operation *Operation) { operation.Risk.Level = "catastrophic" }, want: "risk level"},
		{name: "recovery", mutate: func(operation *Operation) { operation.Risk.ErrorRecovery = "sometimes" }, want: "error recovery"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			catalog := base
			catalog.Operations = append([]Operation(nil), base.Operations...)
			tt.mutate(&catalog.Operations[0])
			if err := Validate(catalog); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate error=%v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestValidateAcceptsCompleteCursorPagination(t *testing.T) {
	catalog := Catalog{
		SchemaVersion:   CatalogV2SchemaVersion,
		ContractVersion: "v1",
		DigestAlgorithm: CatalogV2DigestAlgorithm,
		ContextSchema:   JSONSchema{"type": "object"},
		ExecutionSchema: JSONSchema{"type": "object"},
		Operations: []Operation{{
			ID:          "group.list",
			Group:       "group",
			GroupTitle:  "Group",
			Action:      "DescribeThings",
			Resource:    "things",
			Verb:        "describe",
			Family:      "group",
			Visibility:  "public",
			Wire:        WireSpec{Method: "GET", Path: "/DescribeThings", RequestFormat: "json", Codec: CodecJSON},
			InputSchema: JSONSchema{"query": map[string]any{"type": "object", "properties": map[string]any{"Cursor": map[string]any{"type": "string"}, "PageSize": map[string]any{"type": "integer"}}}},
			Pagination: &PaginationSpec{
				Mode:            PaginationCursor,
				CursorParam:     "Cursor",
				PageSizeParam:   "PageSize",
				NextCursorField: "Cursor",
				ItemsField:      "Things",
				MaxPages:        1000,
			},
			Runtime: RuntimeSpec{SupportsDryRun: true},
			Output:  OutputSpec{Policy: "envelope", IsEnvelopeOutput: true},
			Docs:    DocsSpec{Summary: "DescribeThings", Source: "Group"},
			Risk:    RiskSpec{Level: "low", ErrorRecovery: "safe-retry"},
		}},
	}
	if err := Validate(catalog); err != nil {
		t.Fatalf("Validate cursor catalog: %v", err)
	}
}

func TestValidatePaginationRejectsCrossModeParamsAbsentFromQuerySchema(t *testing.T) {
	tests := []struct {
		name       string
		pagination PaginationSpec
		properties map[string]any
		missing    string
	}{
		{
			name: "page-number cursor param",
			pagination: PaginationSpec{
				Mode:            PaginationPageNumber,
				PageNumberParam: "PageNumber",
				PageSizeParam:   "PageSize",
				CursorParam:     "Cursor",
				ItemsField:      "Items",
				DefaultPageSize: 100,
				MaxPages:        1000,
			},
			properties: map[string]any{
				"PageNumber": map[string]any{"type": "integer"},
				"PageSize":   map[string]any{"type": "integer"},
			},
			missing: "Cursor",
		},
		{
			name: "cursor page-number param",
			pagination: PaginationSpec{
				Mode:            PaginationCursor,
				PageNumberParam: "PageNumber",
				CursorParam:     "Cursor",
				NextCursorField: "Next",
				ItemsField:      "Items",
				MaxPages:        1000,
			},
			properties: map[string]any{
				"Cursor": map[string]any{"type": "string"},
			},
			missing: "PageNumber",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			operation := Operation{
				InputSchema: JSONSchema{"query": map[string]any{
					"type":       "object",
					"properties": tt.properties,
				}},
				Pagination: &tt.pagination,
			}
			err := validatePagination(operation)
			if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("%q", tt.missing)) {
				t.Fatalf("validatePagination error=%v, want missing %q", err, tt.missing)
			}

			tt.properties[tt.missing] = map[string]any{"type": "string"}
			if err := validatePagination(operation); err != nil {
				t.Fatalf("validatePagination with cross-mode property: %v", err)
			}
		})
	}
}

func TestValidateRejectsMalformedRequiredArrays(t *testing.T) {
	tests := []struct {
		name     string
		required any
	}{
		{name: "scalar", required: "TopicId"},
		{name: "mixed", required: []any{"TopicId", 42}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			catalog, err := LoadEmbedded()
			if err != nil {
				t.Fatalf("LoadEmbedded: %v", err)
			}
			catalog.Operations[0].InputSchema = JSONSchema{"body": map[string]any{
				"type":       "object",
				"properties": map[string]any{"TopicId": map[string]any{"type": "string"}},
				"required":   tt.required,
			}}
			if err := Validate(catalog); err == nil || !strings.Contains(err.Error(), "required must be an array of strings") {
				t.Fatalf("Validate error=%v", err)
			}
		})
	}
}

func TestExpandContextSchemaRejectsReferenceToNonObject(t *testing.T) {
	context := JSONSchema{"$ref": "#/execution_schema/type"}
	_, err := ExpandContextSchema(context, JSONSchema{"type": "object"})
	if err == nil || !strings.Contains(err.Error(), "target is not an object") {
		t.Fatalf("ExpandContextSchema error=%v", err)
	}
}

func TestValidateAndLoadRejectReferenceToNonObject(t *testing.T) {
	catalog, err := LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}
	catalog.ContextSchema = JSONSchema{
		"type": "object",
		"properties": map[string]any{
			"execution": map[string]any{"$ref": "#/execution_schema/type"},
		},
	}
	if err := Validate(catalog); err == nil || !strings.Contains(err.Error(), "target is not an object") {
		t.Fatalf("Validate error=%v", err)
	}
	raw, err := json.Marshal(catalog)
	if err != nil {
		t.Fatalf("marshal catalog: %v", err)
	}
	if _, err := Load(raw); err == nil || !strings.Contains(err.Error(), "target is not an object") {
		t.Fatalf("Load error=%v", err)
	}
}

func validTestOperation() Operation {
	return Operation{
		ID:          "group.get",
		Group:       "group",
		GroupTitle:  "Group",
		Action:      "Get",
		Resource:    "item",
		Verb:        "get",
		Family:      "group",
		Visibility:  "public",
		Wire:        WireSpec{Method: "GET", Path: "/Get", RequestFormat: "json", Codec: CodecJSON},
		InputSchema: JSONSchema{},
		Runtime:     RuntimeSpec{SupportsDryRun: true},
		Output:      OutputSpec{Policy: "envelope", IsEnvelopeOutput: true},
		Docs:        DocsSpec{Summary: "Get", Source: "Group"},
		Risk:        RiskSpec{Level: "low", ErrorRecovery: "safe-retry"},
	}
}
