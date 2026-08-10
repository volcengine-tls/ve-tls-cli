package main

import (
	"strings"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
)

func TestBuildSourceOperationsFallsBackToRawSwaggerTagForGroup(t *testing.T) {
	doc := swaggerDoc{
		Paths: map[string]swaggerPathItem{
			"/ActiveTlsAccount": {
				Post: &swaggerOp{
					Summary: "ActiveTlsSvc",
					Tags:    []string{"Account"},
				},
			},
		},
	}
	groupKeys := map[string]string{
		"账号管理":    "account",
		"Account": "account",
	}

	got := buildSourceOperations(doc, groupKeys, map[string]string{}, map[string]apiDocEntry{}, toolCatalogOverrides{})
	if len(got) != 1 {
		t.Fatalf("operations=%d", len(got))
	}
	if got[0].Group != "account" {
		t.Fatalf("group=%q", got[0].Group)
	}
	if got[0].GroupTitle != "Account" {
		t.Fatalf("group title=%q", got[0].GroupTitle)
	}
}

func TestActionName_UsesSwaggerSummaryWithoutInference(t *testing.T) {
	tests := []struct {
		name    string
		summary string
		method  string
		path    string
		want    string
	}{
		{
			name:    "describe projects",
			summary: "DescribeProjects",
			method:  "GET",
			path:    "/DescribeProjects",
			want:    "DescribeProjects",
		},
		{
			name:    "search logs",
			summary: "SearchLogs",
			method:  "POST",
			path:    "/SearchLogs",
			want:    "SearchLogs",
		},
		{
			name:    "fallback without summary",
			summary: "",
			method:  "GET",
			path:    "/DescribeProject",
			want:    "DescribeProject",
		},
	}
	for _, tc := range tests {
		got := actionName("project", tc.summary, tc.method, tc.path)
		if got != tc.want {
			t.Fatalf("%s: want %q, got %q", tc.name, tc.want, got)
		}
	}
}

func TestParseDocRequestParamsMarkdown(t *testing.T) {
	md := `
## 请求参数
### Query
|**参数**|**类型**|**是否必选**|**示例值**|**描述**|
|---|---|---|---|---|
|ProjectId |String |是 |` + "`id`" + ` |日志项目的 ID。 |

### Body
|**参数**|**类型**|**是否必选**|**示例值**|**描述**|
|---|---|---|---|---|
|ProjectName |String |是 |` + "`test-project`" + ` |日志项目名称。 |
| | | | |同地域下需唯一。 |
|Tags |Array of [Tag](#tag) |否 |` + "`[{\"Key\":\"k\",\"Value\":\"v\"}]`" + ` |标签列表。 |

### Tag
|**参数**|**类型**|**是否必选**|**示例值**|**描述**|
|---|---|---|---|---|
|Key |String |否 |` + "`owner`" + ` |标签 Key。 |
`
	got := parseDocRequestParamsMarkdown(md)
	if len(got) != 3 {
		t.Fatalf("len=%d got=%+v", len(got), got)
	}
	if got[0].In != "query" || got[0].Name != "ProjectId" {
		t.Fatalf("unexpected query row: %+v", got[0])
	}
	if got[1].In != "body" || got[1].Name != "ProjectName" || !strings.Contains(got[1].Description, "同地域下需唯一") {
		t.Fatalf("unexpected body row: %+v", got[1])
	}
	if got[2].In != "body" || got[2].Name != "Tags" {
		t.Fatalf("unexpected tags row: %+v", got[2])
	}
}

func TestBuildOperationCatalogMinimalFields(t *testing.T) {
	doc := swaggerDoc{
		Paths: map[string]swaggerPathItem{
			"/DescribeProject": {
				Get: &swaggerOp{
					Summary: "DescribeProject",
					Tags:    []string{"Project"},
					Parameters: []swaggerParam{
						{
							Name:     "ProjectId",
							In:       "query",
							Type:     "string",
							Required: true,
						},
					},
				},
			},
		},
	}
	groupKeys := map[string]string{"Project": "project"}

	got := buildOperationCatalogForTest(t, doc, "stage1", groupKeys, map[string]string{}, map[string]apiDocEntry{}, toolCatalogOverrides{})
	if len(got.Operations) != 1 {
		t.Fatalf("operations=%d", len(got.Operations))
	}

	gotTool := got.Operations[0]
	if gotTool.ID != "project.describe" {
		t.Fatalf("id=%q", gotTool.ID)
	}
	if gotTool.Group != "project" {
		t.Fatalf("group=%q", gotTool.Group)
	}
	if gotTool.Action != "DescribeProject" {
		t.Fatalf("action=%q", gotTool.Action)
	}
	if gotTool.Verb != "describe" {
		t.Fatalf("verb=%q", gotTool.Verb)
	}
	if gotTool.Family != "project" {
		t.Fatalf("family=%q", gotTool.Family)
	}
	if gotTool.Resource != "project" {
		t.Fatalf("resource=%q", gotTool.Resource)
	}
	if gotTool.Visibility != "public" {
		t.Fatalf("visibility=%q", gotTool.Visibility)
	}
	if gotTool.Wire.Method != "GET" {
		t.Fatalf("method=%q", gotTool.Wire.Method)
	}
	if gotTool.Wire.Path != "/DescribeProject" {
		t.Fatalf("path=%q", gotTool.Wire.Path)
	}
	if gotTool.Docs.Summary != "DescribeProject" {
		t.Fatalf("summary=%q", gotTool.Docs.Summary)
	}
	if len(gotTool.InputSchema) == 0 {
		t.Fatalf("missing input_schema")
	}
	if got.ContextSchema == nil || got.ExecutionSchema == nil {
		t.Fatalf("missing context/execution schema")
	}
	if gotTool.Output.Policy == "" {
		t.Fatalf("missing output_policy")
	}
	if gotTool.Risk.Level == "" {
		t.Fatalf("missing risk_level")
	}
	if gotTool.Docs.Source == "" {
		t.Fatalf("missing doc_source")
	}
	ctxProps, ok := got.ContextSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("context schema malformed: %+v", got.ContextSchema)
	}
	for _, key := range []string{"region", "profile", "secrets_file", "endpoint", "trace", "contract_digest", "execution"} {
		if _, ok := ctxProps[key]; !ok {
			t.Fatalf("context missing %q field", key)
		}
	}
	execProps, ok := got.ExecutionSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("execution schema malformed: %+v", got.ExecutionSchema)
	}
	for _, key := range []string{"artifact", "dry_run", "projection", "page", "page_all"} {
		if _, ok := execProps[key]; !ok {
			t.Fatalf("execution missing %q", key)
		}
	}
	pageObj, ok := execProps["page"].(map[string]any)
	if !ok {
		t.Fatalf("execution page schema malformed: %+v", got.ExecutionSchema)
	}
	pageObjProps, ok := pageObj["properties"].(map[string]any)
	if !ok {
		t.Fatalf("execution page schema missing properties: %+v", got.ExecutionSchema)
	}
	if _, ok := pageObjProps["all"]; !ok {
		t.Fatalf("execution page schema missing all: %+v", got.ExecutionSchema)
	}
}

func TestBuildOperationCatalogContextSchemaCarriesWave2Guidance(t *testing.T) {
	doc := swaggerDoc{
		Paths: map[string]swaggerPathItem{
			"/CreateTopic": {
				Post: &swaggerOp{
					Summary: "CreateTopic",
					Tags:    []string{"Topic"},
				},
			},
		},
	}
	got := buildOperationCatalogForTest(t, doc, "stage1", map[string]string{"Topic": "topic"}, map[string]string{}, map[string]apiDocEntry{
		"CreateTopic": {GroupTitle: "Topic"},
	}, toolCatalogOverrides{})
	if len(got.Operations) != 1 {
		t.Fatalf("operations=%d", len(got.Operations))
	}

	ctxProps, ok := got.ContextSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("context schema malformed: %+v", got.ContextSchema)
	}
	for _, key := range []string{"profile", "secrets_file", "region", "endpoint", "trace", "contract_digest", "execution"} {
		field, ok := ctxProps[key].(map[string]any)
		if !ok {
			t.Fatalf("context field %q malformed: %+v", key, ctxProps[key])
		}
		for _, attr := range []string{"description", "when_to_use", "default", "runtime_effect"} {
			if _, ok := field[attr]; !ok {
				t.Fatalf("context field %q missing %q: %+v", key, attr, field)
			}
		}
		if _, ok := field["example"]; ok {
			t.Fatalf("context field %q should omit example: %+v", key, field)
		}
	}
}

func TestBuildOperationCatalogContextRuntimeEffectsMatchCurrentSelectorAndTraceSemantics(t *testing.T) {
	doc := swaggerDoc{
		Paths: map[string]swaggerPathItem{
			"/DescribeTopics": {
				Get: &swaggerOp{
					Summary: "DescribeTopics",
					Tags:    []string{"Topic"},
				},
			},
		},
	}
	got := buildOperationCatalogForTest(t, doc, "stage1", map[string]string{"Topic": "topic"}, map[string]string{}, map[string]apiDocEntry{
		"DescribeTopics": {GroupTitle: "Topic"},
	}, toolCatalogOverrides{})
	if len(got.Operations) != 1 {
		t.Fatalf("operations=%d", len(got.Operations))
	}

	ctxProps, ok := got.ContextSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("context schema malformed: %+v", got.ContextSchema)
	}

	secretsField, ok := ctxProps["secrets_file"].(map[string]any)
	if !ok {
		t.Fatalf("context field secrets_file malformed: %+v", ctxProps["secrets_file"])
	}
	secretsRuntimeEffect, _ := secretsField["runtime_effect"].(string)
	for _, want := range []string{"selectors first", "supported VOLCENGINE_*"} {
		if !strings.Contains(secretsRuntimeEffect, want) {
			t.Fatalf("secrets_file runtime_effect missing %q: %q", want, secretsRuntimeEffect)
		}
	}

	traceField, ok := ctxProps["trace"].(map[string]any)
	if !ok {
		t.Fatalf("context field trace malformed: %+v", ctxProps["trace"])
	}
	traceRuntimeEffect, _ := traceField["runtime_effect"].(string)
	for _, want := range []string{"trace directory", "legacy strict/default", "on/off"} {
		if !strings.Contains(traceRuntimeEffect, want) {
			t.Fatalf("trace runtime_effect missing %q: %q", want, traceRuntimeEffect)
		}
	}
}

func TestBuildOperationCatalogSkipsUndocumentedOperationsWhenDocIndexPresent(t *testing.T) {
	doc := swaggerDoc{
		Paths: map[string]swaggerPathItem{
			"/DescribeProject": {
				Get: &swaggerOp{
					Summary: "DescribeProject",
					Tags:    []string{"Project"},
				},
			},
			"/InternalOnlyAction": {
				Post: &swaggerOp{
					Summary: "InternalOnlyAction",
					Tags:    []string{"Project"},
				},
			},
		},
	}
	got := buildOperationCatalogForTest(t, doc, "stage1", map[string]string{"Project": "project"}, map[string]string{}, map[string]apiDocEntry{
		"DescribeProject": {GroupTitle: "Project"},
	}, toolCatalogOverrides{})
	if len(got.Operations) != 1 {
		t.Fatalf("operations=%d got=%+v", len(got.Operations), got.Operations)
	}
	if got.Operations[0].Docs.Summary != "DescribeProject" {
		t.Fatalf("unexpected operation summary=%q", got.Operations[0].Docs.Summary)
	}
}

func TestBuildOperationCatalogExecutionSchemaCarriesWave2Guidance(t *testing.T) {
	doc := swaggerDoc{
		Paths: map[string]swaggerPathItem{
			"/DescribeTopics": {
				Get: &swaggerOp{
					Summary: "DescribeTopics",
					Tags:    []string{"Topic"},
				},
			},
		},
	}
	got := buildOperationCatalogForTest(t, doc, "stage1", map[string]string{"Topic": "topic"}, map[string]string{}, map[string]apiDocEntry{}, toolCatalogOverrides{})
	if len(got.Operations) != 1 {
		t.Fatalf("operations=%d", len(got.Operations))
	}

	execProps, ok := got.ExecutionSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("execution schema malformed: %+v", got.ExecutionSchema)
	}
	for _, key := range []string{"dry_run", "projection", "artifact", "page"} {
		field, ok := execProps[key].(map[string]any)
		if !ok {
			t.Fatalf("execution field %q malformed: %+v", key, execProps[key])
		}
		if strings.TrimSpace(asTestString(field["description"])) == "" {
			t.Fatalf("execution field %q missing description: %+v", key, field)
		}
	}
}

func TestBuildOperationCatalogInputSchema(t *testing.T) {
	doc := swaggerDoc{
		Paths: map[string]swaggerPathItem{
			"/DescribeProject/{projectId}": {
				Get: &swaggerOp{
					Summary: "DescribeProject",
					Tags:    []string{"Project"},
					Parameters: []swaggerParam{
						{
							Name:     "projectId",
							In:       "path",
							Type:     "string",
							Required: true,
						},
						{
							Name:     "region",
							In:       "query",
							Type:     "string",
							Required: false,
						},
						{
							Name:     "X-Ctl",
							In:       "header",
							Type:     "string",
							Required: false,
						},
						{
							Name:     "body",
							In:       "body",
							Required: true,
							Schema: &swaggerSchema{Type: "object", Properties: map[string]swaggerSchema{
								"name": {Type: "string"},
							}},
						},
					},
				},
			},
		},
	}

	got := buildOperationCatalogForTest(t, doc, "stage1", map[string]string{"Project": "project"}, map[string]string{}, map[string]apiDocEntry{}, toolCatalogOverrides{})
	if len(got.Operations) != 1 {
		t.Fatalf("operations=%d", len(got.Operations))
	}
	input := got.Operations[0].InputSchema
	query, ok := input["query"].(map[string]any)
	if !ok || query == nil {
		t.Fatalf("query schema missing: %+v", input)
	}
	queryProps, ok := query["properties"].(map[string]any)
	if !ok || queryProps["region"] == nil {
		t.Fatalf("query region missing: %+v", query)
	}
	path, ok := input["path"].(map[string]any)
	if !ok || path == nil {
		t.Fatalf("path schema missing: %+v", input)
	}
	pathProps, ok := path["properties"].(map[string]any)
	if !ok || pathProps["projectId"] == nil {
		t.Fatalf("path projectId missing: %+v", path)
	}
	head, ok := input["header"].(map[string]any)
	if !ok || head == nil {
		t.Fatalf("header schema missing: %+v", input)
	}
	headProps, ok := head["properties"].(map[string]any)
	if !ok || headProps["X-Ctl"] == nil {
		t.Fatalf("header x-ctl missing: %+v", head)
	}
	body, ok := input["body"].(map[string]any)
	if !ok || body == nil {
		t.Fatalf("body schema missing: %+v", input)
	}
	bodyProps, ok := body["properties"].(map[string]any)
	if !ok || bodyProps["name"] == nil {
		t.Fatalf("body payload missing: %+v", body)
	}
	bodyObj, ok := bodyProps["name"].(map[string]any)
	if !ok || bodyObj["type"] != "string" {
		t.Fatalf("body payload malformed: %+v", bodyProps["name"])
	}
	if got.ContextSchema["type"] == nil {
		t.Fatalf("context schema malformed: %+v", got.ContextSchema)
	}
	if got.ExecutionSchema["type"] == nil {
		t.Fatalf("execution schema malformed: %+v", got.ExecutionSchema)
	}
}

func TestBuildOperationCatalogFiltersManagedHeadersFromPublicInputSchema(t *testing.T) {
	doc := swaggerDoc{
		Paths: map[string]swaggerPathItem{
			"/CreateProject": {
				Post: &swaggerOp{
					Summary: "CreateProject",
					Tags:    []string{"Project"},
					Parameters: []swaggerParam{
						{Name: "Content-Type", In: "header", Type: "string"},
						{Name: "X-Ctl", In: "header", Type: "string"},
						{Name: "AccessKey", In: "header", Type: "string"},
						{
							Name:     "body",
							In:       "body",
							Required: true,
							Schema: &swaggerSchema{Type: "object", Properties: map[string]swaggerSchema{
								"name": {Type: "string"},
							}},
						},
					},
				},
			},
		},
	}

	got := buildOperationCatalogForTest(t, doc, "stage1", map[string]string{"Project": "project"}, map[string]string{}, map[string]apiDocEntry{}, toolCatalogOverrides{})
	if len(got.Operations) != 1 {
		t.Fatalf("operations=%d", len(got.Operations))
	}
	input := got.Operations[0].InputSchema
	header, ok := input["header"].(map[string]any)
	if !ok || header["properties"] == nil {
		t.Fatalf("header schema missing: %+v", input)
	}
	props := header["properties"].(map[string]any)
	if props["X-Ctl"] == nil {
		t.Fatalf("expected custom header to remain: %+v", props)
	}
	for _, key := range []string{"Content-Type", "AccessKey"} {
		if props[key] != nil {
			t.Fatalf("expected managed header %q to be filtered: %+v", key, props)
		}
	}
}

func TestBuildOperationCatalogIDStableAndUnique(t *testing.T) {
	doc := swaggerDoc{
		Paths: map[string]swaggerPathItem{
			"/DescribeAccountQuota": {
				Get: &swaggerOp{
					Summary: "DescribeAccountQuota",
					Tags:    []string{"Account"},
				},
			},
			"/DescribeAccountServiceTopic": {
				Get: &swaggerOp{
					Summary: "DescribeAccountServiceTopic",
					Tags:    []string{"Account"},
				},
			},
		},
	}
	groupKeys := map[string]string{"Account": "account"}

	got := buildOperationCatalogForTest(t, doc, "stage1", groupKeys, map[string]string{}, map[string]apiDocEntry{}, toolCatalogOverrides{})
	if len(got.Operations) != 2 {
		t.Fatalf("operations=%d", len(got.Operations))
	}
	ids := map[string]struct{}{}
	for _, tool := range got.Operations {
		id := string(tool.ID)
		if !strings.HasPrefix(id, "account.") {
			t.Fatalf("id=%q", tool.ID)
		}
		if _, ok := ids[id]; ok {
			t.Fatalf("duplicate id=%q", tool.ID)
		}
		ids[id] = struct{}{}
	}
}

func TestBuildOperationCatalogInputSchemaUsesStructuredRefBodySchema(t *testing.T) {
	defs := map[string]swaggerSchema{
		"CreateTicketRequest": {
			Type: "object",
			Properties: map[string]swaggerSchema{
				"Name": {
					Type: "string",
				},
				"ExpireSeconds": {
					Type: "integer",
				},
			},
			Required: []string{"Name"},
		},
	}
	doc := swaggerDoc{
		Paths: map[string]swaggerPathItem{
			"/CreateTicket": {
				Post: &swaggerOp{
					Summary: "CreateTicket",
					Tags:    []string{"Ticket"},
					Parameters: []swaggerParam{
						{
							Name:     "body",
							In:       "body",
							Required: true,
							Schema: &swaggerSchema{
								Ref: "#/definitions/CreateTicketRequest",
							},
						},
					},
				},
			},
		},
		Definitions: defs,
	}

	got := buildOperationCatalogForTest(t, doc, "stage1", map[string]string{"Ticket": "ticket"}, map[string]string{}, map[string]apiDocEntry{}, toolCatalogOverrides{})
	if len(got.Operations) != 1 {
		t.Fatalf("operations=%d", len(got.Operations))
	}
	input := got.Operations[0].InputSchema
	body, ok := input["body"].(map[string]any)
	if !ok {
		t.Fatalf("body schema missing: %+v", input)
	}
	bodyProps, ok := body["properties"].(map[string]any)
	if !ok {
		t.Fatalf("body properties missing: %+v", body)
	}
	nameSchema, ok := bodyProps["Name"].(map[string]any)
	if !ok {
		t.Fatalf("body payload missing: %+v", bodyProps)
	}
	if nameSchema["type"] != "string" {
		t.Fatalf("Name schema malformed: %+v", nameSchema)
	}
	required, ok := body["required"].([]string)
	if !ok {
		if arr, ok := body["required"].([]interface{}); ok {
			required = make([]string, 0, len(arr))
			for _, v := range arr {
				s, ok := v.(string)
				if !ok {
					t.Fatalf("required value not string: %+v", arr)
				}
				required = append(required, s)
			}
		}
	}
	if len(required) != 1 || required[0] != "Name" {
		t.Fatalf("required mismatch: %+v", body["required"])
	}
}

func TestBuildOperationCatalogInputSchemaExpandsAllOfAndPreservesNestedConstraints(t *testing.T) {
	minLen := 1
	maxLen := 256
	minimum := 64.0
	maximum := 1048576.0
	doc := swaggerDoc{
		Paths: map[string]swaggerPathItem{
			"/CreateIndex": {
				Post: &swaggerOp{
					Summary: "CreateIndex",
					Tags:    []string{"Index"},
					Parameters: []swaggerParam{
						{
							Name:     "body",
							In:       "body",
							Required: true,
							Schema: &swaggerSchema{
								Type: "object",
								Properties: map[string]swaggerSchema{
									"FullText": {
										Description: "全文索引配置。",
										AllOf: []swaggerSchema{
											{Ref: "#/definitions/index.FullTextInfo"},
										},
									},
									"MaxTextLen": {
										Type:        "integer",
										Description: "索引字段值的最大长度。",
										Default:     2048,
										Minimum:     &minimum,
										Maximum:     &maximum,
									},
								},
							},
						},
					},
				},
			},
		},
		Definitions: map[string]swaggerSchema{
			"index.FullTextInfo": {
				Type:        "object",
				Description: "内部全文索引结构。",
				Required:    []string{"CaseSensitive", "Delimiter"},
				Properties: map[string]swaggerSchema{
					"CaseSensitive": {
						Type:        "boolean",
						Description: "是否大小写敏感。",
					},
					"Delimiter": {
						Type:        "string",
						Description: "全文索引的分词符。",
						MinLength:   &minLen,
						MaxLength:   &maxLen,
					},
				},
			},
		},
	}

	got := buildOperationCatalogForTest(t, doc, "stage1", map[string]string{"Index": "index"}, map[string]string{}, map[string]apiDocEntry{}, toolCatalogOverrides{})
	if len(got.Operations) != 1 {
		t.Fatalf("operations=%d", len(got.Operations))
	}
	input := got.Operations[0].InputSchema
	body, ok := input["body"].(map[string]any)
	if !ok {
		t.Fatalf("body schema missing: %+v", input)
	}
	bodyProps, ok := body["properties"].(map[string]any)
	if !ok {
		t.Fatalf("body properties missing: %+v", body)
	}
	fullText, ok := bodyProps["FullText"].(map[string]any)
	if !ok {
		t.Fatalf("FullText schema missing: %+v", bodyProps["FullText"])
	}
	if asTestString(fullText["description"]) != "全文索引配置。" {
		t.Fatalf("expected FullText description to survive allOf wrapper, got %+v", fullText["description"])
	}
	if fullText["type"] != "object" {
		t.Fatalf("expected FullText type=object, got %+v", fullText)
	}
	fullTextProps, ok := fullText["properties"].(map[string]any)
	if !ok {
		t.Fatalf("FullText properties missing: %+v", fullText)
	}
	delimiter, ok := fullTextProps["Delimiter"].(map[string]any)
	if !ok {
		t.Fatalf("Delimiter schema missing: %+v", fullTextProps["Delimiter"])
	}
	if asTestString(delimiter["description"]) != "全文索引的分词符。" {
		t.Fatalf("expected Delimiter description, got %+v", delimiter)
	}
	if delimiter["minLength"] != minLen || delimiter["maxLength"] != maxLen {
		t.Fatalf("expected Delimiter length constraints, got %+v", delimiter)
	}
	required := jsonStringSlice(fullText["required"])
	if len(required) != 2 || required[0] != "CaseSensitive" || required[1] != "Delimiter" {
		t.Fatalf("expected required fields on FullText, got %+v", fullText["required"])
	}
	maxTextLen, ok := bodyProps["MaxTextLen"].(map[string]any)
	if !ok {
		t.Fatalf("MaxTextLen schema missing: %+v", bodyProps["MaxTextLen"])
	}
	if maxTextLen["default"] != 2048 {
		t.Fatalf("expected MaxTextLen default, got %+v", maxTextLen["default"])
	}
	if maxTextLen["minimum"] != minimum || maxTextLen["maximum"] != maximum {
		t.Fatalf("expected MaxTextLen numeric constraints, got %+v", maxTextLen)
	}
}

func TestBuildOperationCatalogExpandsNestedArrayItemSchemasInsideAllOfRefs(t *testing.T) {
	doc := swaggerDoc{
		Paths: map[string]swaggerPathItem{
			"/CreateShipper": {
				Post: &swaggerOp{
					Summary: "CreateShipper",
					Tags:    []string{"Shipper"},
					Parameters: []swaggerParam{
						{
							Name:     "body",
							In:       "body",
							Required: true,
							Schema: &swaggerSchema{
								Ref: "#/definitions/shipper.CreateReq",
							},
						},
					},
				},
			},
		},
		Definitions: map[string]swaggerSchema{
			"shipper.CreateReq": {
				Type: "object",
				Properties: map[string]swaggerSchema{
					"ContentInfo": {
						Description: "投递日志的内容格式配置。",
						AllOf: []swaggerSchema{
							{Ref: "#/definitions/dao.ContentInfo"},
						},
					},
				},
				Required: []string{"ContentInfo"},
			},
			"dao.ContentInfo": {
				Type: "object",
				Properties: map[string]swaggerSchema{
					"CsvInfo": {
						AllOf: []swaggerSchema{
							{Ref: "#/definitions/dao.CsvInfo"},
						},
					},
					"JsonInfo": {
						AllOf: []swaggerSchema{
							{Ref: "#/definitions/dao.JsonInfo"},
						},
					},
					"ParquetInfo": {
						AllOf: []swaggerSchema{
							{Ref: "#/definitions/dao.ParquetInfo"},
						},
					},
				},
			},
			"dao.CsvInfo": {
				Type: "object",
				Properties: map[string]swaggerSchema{
					"Keys": {
						Type:  "array",
						Items: &swaggerSchema{Type: "string"},
					},
				},
				Required: []string{"Keys"},
			},
			"dao.JsonInfo": {
				Type: "object",
				Properties: map[string]swaggerSchema{
					"Keys": {
						Type:  "array",
						Items: &swaggerSchema{Type: "string"},
					},
				},
			},
			"dao.ParquetInfo": {
				Type: "object",
				Properties: map[string]swaggerSchema{
					"Fields": {
						Type:  "array",
						Items: &swaggerSchema{Ref: "#/definitions/dao.ParquetField"},
					},
				},
				Required: []string{"Fields"},
			},
			"dao.ParquetField": {
				Type: "object",
				Properties: map[string]swaggerSchema{
					"Key":       {Type: "string"},
					"TransType": {Type: "string", Enum: []any{"string", "int64"}},
				},
				Required: []string{"Key", "TransType"},
			},
		},
	}

	got := buildOperationCatalogForTest(t, doc, "stage1", map[string]string{"Shipper": "shipper"}, map[string]string{}, map[string]apiDocEntry{}, toolCatalogOverrides{})
	if len(got.Operations) != 1 {
		t.Fatalf("operations=%d", len(got.Operations))
	}
	body := got.Operations[0].InputSchema["body"].(map[string]any)
	contentInfo := body["properties"].(map[string]any)["ContentInfo"].(map[string]any)
	contentProps := contentInfo["properties"].(map[string]any)

	csvKeys := contentProps["CsvInfo"].(map[string]any)["properties"].(map[string]any)["Keys"].(map[string]any)
	if csvKeys["type"] != "array" {
		t.Fatalf("expected CsvInfo.Keys type=array, got %+v", csvKeys)
	}
	if csvItems, ok := csvKeys["items"].(map[string]any); !ok || csvItems["type"] != "string" {
		t.Fatalf("expected CsvInfo.Keys items=string, got %+v", csvKeys["items"])
	}

	jsonKeys := contentProps["JsonInfo"].(map[string]any)["properties"].(map[string]any)["Keys"].(map[string]any)
	if jsonItems, ok := jsonKeys["items"].(map[string]any); !ok || jsonItems["type"] != "string" {
		t.Fatalf("expected JsonInfo.Keys items=string, got %+v", jsonKeys["items"])
	}

	parquetFields := contentProps["ParquetInfo"].(map[string]any)["properties"].(map[string]any)["Fields"].(map[string]any)
	parquetItems, ok := parquetFields["items"].(map[string]any)
	if !ok {
		t.Fatalf("expected ParquetInfo.Fields items schema, got %+v", parquetFields["items"])
	}
	parquetProps, ok := parquetItems["properties"].(map[string]any)
	if !ok || parquetProps["Key"] == nil || parquetProps["TransType"] == nil {
		t.Fatalf("expected ParquetInfo.Fields items to expand dao.ParquetField, got %+v", parquetItems)
	}
}

func TestBuildOperationCatalogInputSchemaUnwrapsBodyParamNameFromRef(t *testing.T) {
	defs := map[string]swaggerSchema{
		"CreateTicketRequest": {
			Type: "object",
			Properties: map[string]swaggerSchema{
				"Name": {
					Type: "string",
				},
				"Region": {
					Type: "string",
				},
			},
			Required: []string{"Name"},
		},
	}
	doc := swaggerDoc{
		Paths: map[string]swaggerPathItem{
			"/CreateTicketWithData": {
				Post: &swaggerOp{
					Summary: "CreateTicketWithData",
					Tags:    []string{"Ticket"},
					Parameters: []swaggerParam{
						{
							Name:     "data",
							In:       "body",
							Required: true,
							Schema: &swaggerSchema{
								Ref: "#/definitions/CreateTicketRequest",
							},
						},
					},
				},
			},
		},
		Definitions: defs,
	}

	got := buildOperationCatalogForTest(t, doc, "stage1", map[string]string{"Ticket": "ticket"}, map[string]string{}, map[string]apiDocEntry{}, toolCatalogOverrides{})
	if len(got.Operations) != 1 {
		t.Fatalf("operations=%d", len(got.Operations))
	}
	input := got.Operations[0].InputSchema
	body, ok := input["body"].(map[string]any)
	if !ok {
		t.Fatalf("body schema missing: %+v", input)
	}
	bodyProps, ok := body["properties"].(map[string]any)
	if !ok {
		t.Fatalf("body properties missing: %+v", body)
	}
	if _, ok := bodyProps["data"]; ok {
		t.Fatalf("body parameter name should not be wrapped: %+v", bodyProps)
	}
	if _, ok := bodyProps["Name"]; !ok {
		t.Fatalf("body name field missing: %+v", bodyProps)
	}
	if payload, ok := bodyProps["Region"].(map[string]any); !ok || payload["type"] != "string" {
		t.Fatalf("body field type mismatch: %+v", payload)
	}
}

func TestBuildOperationCatalogInputSchemaMergesDocParamsForMissingFields(t *testing.T) {
	doc := swaggerDoc{
		Paths: map[string]swaggerPathItem{
			"/DescribeProject": {
				Get: &swaggerOp{
					Summary: "DescribeProject",
					Tags:    []string{"Project"},
					Parameters: []swaggerParam{
						{
							Name:        "ProjectId",
							In:          "query",
							Type:        "string",
							Required:    true,
							Description: "project id",
						},
					},
				},
			},
		},
	}
	docIndex := map[string]apiDocEntry{
		"DescribeProject": {
			RequestParamsDoc: []apiDocParam{
				{Name: "Region", In: "query", Type: "String", RequiredText: "是"},
				{Name: "X-Ctl", In: "header", Type: "String"},
				{Name: "Content-Type", In: "header", Type: "String"},
				{Name: "TopicName", In: "body", Type: "String"},
			},
		},
	}

	got := buildOperationCatalogForTest(t, doc, "stage1", map[string]string{"Project": "project"}, map[string]string{}, docIndex, toolCatalogOverrides{})
	if len(got.Operations) != 1 {
		t.Fatalf("operations=%d", len(got.Operations))
	}
	input := got.Operations[0].InputSchema
	query, ok := input["query"].(map[string]any)
	if !ok || query["properties"] == nil {
		t.Fatalf("query schema missing: %+v", input)
	}
	queryProps := query["properties"].(map[string]any)
	if queryProps["Region"] == nil {
		t.Fatalf("query missing doc field: %+v", query)
	}
	queryRequired, ok := query["required"].([]string)
	if !ok || len(queryRequired) != 2 {
		t.Fatalf("query required mismatch: %+v", query["required"])
	}
	header, ok := input["header"].(map[string]any)
	if !ok || header["properties"] == nil {
		t.Fatalf("header schema missing: %+v", input)
	}
	headerProps := header["properties"].(map[string]any)
	if headerProps["X-Ctl"] == nil {
		t.Fatalf("header missing doc field: %+v", header)
	}
	if headerProps["Content-Type"] != nil {
		t.Fatalf("expected managed doc header to be filtered: %+v", headerProps)
	}
	body, ok := input["body"].(map[string]any)
	if !ok {
		t.Fatalf("body schema missing: %+v", input)
	}
	bodyProps := body["properties"].(map[string]any)
	if bodyProps["TopicName"] == nil {
		t.Fatalf("body missing doc field: %+v", body)
	}
}

func TestBuildOperationCatalogOverridesPutLogsBodySchemaForAgentFriendlyInput(t *testing.T) {
	doc := swaggerDoc{
		Paths: map[string]swaggerPathItem{
			"/PutLogs": {
				Post: &swaggerOp{
					Summary: "PutLogs",
					Tags:    []string{"Log"},
					Parameters: []swaggerParam{
						{
							Name:     "body",
							In:       "body",
							Required: true,
							Schema: &swaggerSchema{
								Type: "object",
								Properties: map[string]swaggerSchema{
									"LogGroupList": {
										Type: "array",
										Items: &swaggerSchema{
											Type: "object",
										},
									},
								},
								Required: []string{"LogGroupList"},
							},
						},
					},
				},
			},
		},
	}

	got := buildOperationCatalogForTest(t, doc, "stage1", map[string]string{"Log": "log"}, map[string]string{}, map[string]apiDocEntry{}, toolCatalogOverrides{})
	if len(got.Operations) != 1 {
		t.Fatalf("operations=%d", len(got.Operations))
	}
	input := got.Operations[0].InputSchema
	body, ok := input["body"].(map[string]any)
	if !ok {
		t.Fatalf("body schema missing: %+v", input)
	}
	bodyProps, ok := body["properties"].(map[string]any)
	if !ok {
		t.Fatalf("body properties missing: %+v", body)
	}
	if bodyProps["LogGroupList"] != nil {
		t.Fatalf("expected legacy LogGroupList field to be removed from public body schema: %+v", bodyProps)
	}
	logGroups, ok := bodyProps["LogGroups"].(map[string]any)
	if !ok {
		t.Fatalf("expected LogGroups array schema, got %+v", bodyProps["LogGroups"])
	}
	if logGroups["type"] != "array" {
		t.Fatalf("expected LogGroups type=array, got %+v", logGroups)
	}
	itemSchema, ok := logGroups["items"].(map[string]any)
	if !ok {
		t.Fatalf("expected LogGroups.items object schema, got %+v", logGroups["items"])
	}
	itemProps, ok := itemSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected LogGroups item properties, got %+v", itemSchema)
	}
	if itemProps["Logs"] == nil {
		t.Fatalf("expected Logs field in LogGroups items: %+v", itemProps)
	}
	logsSchema, ok := itemProps["Logs"].(map[string]any)
	if !ok {
		t.Fatalf("expected Logs schema map, got %+v", itemProps["Logs"])
	}
	logEntry, ok := logsSchema["items"].(map[string]any)
	if !ok {
		t.Fatalf("expected Logs.items schema map, got %+v", logsSchema["items"])
	}
	logEntryProps, ok := logEntry["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected log entry properties, got %+v", logEntry)
	}
	timeField, ok := logEntryProps["Time"].(map[string]any)
	if !ok {
		t.Fatalf("expected Time schema map, got %+v", logEntryProps["Time"])
	}
	if !strings.Contains(strings.ToLower(asTestString(timeField["description"])), "unix") || !strings.Contains(asTestString(timeField["description"]), "毫秒") {
		t.Fatalf("expected Time to document unix millisecond semantics: %+v", timeField)
	}
	timeNsField, ok := logEntryProps["TimeNs"].(map[string]any)
	if !ok {
		t.Fatalf("expected TimeNs schema map, got %+v", logEntryProps["TimeNs"])
	}
	if !strings.Contains(strings.ToLower(asTestString(timeNsField["description"])), "nanosecond") {
		t.Fatalf("expected TimeNs to document nanosecond fraction semantics: %+v", timeNsField)
	}
	required := jsonStringSlice(body["required"])
	if len(required) != 1 || required[0] != "LogGroups" {
		t.Fatalf("expected required=[LogGroups], got %+v", body["required"])
	}
}

func TestBuildOperationCatalogOverridesTraceWeakTypedFields(t *testing.T) {
	doc := swaggerDoc{
		Paths: map[string]swaggerPathItem{
			"/CreateTraceInstance": {
				Post: &swaggerOp{
					Summary: "CreateTraceInstance",
					Tags:    []string{"Trace"},
					Parameters: []swaggerParam{
						{
							Name:     "body",
							In:       "body",
							Required: true,
							Schema: &swaggerSchema{
								Type: "object",
								Properties: map[string]swaggerSchema{
									"BackendConfig":     {},
									"TraceInstanceName": {Type: "string"},
								},
							},
						},
					},
				},
			},
			"/ModifyTraceInstance": {
				Put: &swaggerOp{
					Summary: "ModifyTraceInstance",
					Tags:    []string{"Trace"},
					Parameters: []swaggerParam{
						{
							Name:     "body",
							In:       "body",
							Required: true,
							Schema: &swaggerSchema{
								Type: "object",
								Properties: map[string]swaggerSchema{
									"BackendConfig":   {},
									"TraceInstanceId": {Type: "string"},
								},
								Required: []string{"TraceInstanceId"},
							},
						},
					},
				},
			},
			"/SearchTraces": {
				Post: &swaggerOp{
					Summary: "SearchTraces",
					Tags:    []string{"Trace"},
					Parameters: []swaggerParam{
						{
							Name:     "body",
							In:       "body",
							Required: true,
							Schema: &swaggerSchema{
								Type: "object",
								Properties: map[string]swaggerSchema{
									"Query":           {},
									"TraceInstanceId": {Type: "string"},
								},
								Required: []string{"Query", "TraceInstanceId"},
							},
						},
					},
				},
			},
		},
	}

	got := buildOperationCatalogForTest(t, doc, "stage1", map[string]string{"Trace": "trace"}, map[string]string{}, map[string]apiDocEntry{}, toolCatalogOverrides{})
	if len(got.Operations) != 3 {
		t.Fatalf("operations=%d", len(got.Operations))
	}
	bySummary := map[string]contract.Operation{}
	for _, operation := range got.Operations {
		bySummary[operation.Docs.Summary] = operation
	}

	for _, summary := range []string{"CreateTraceInstance", "ModifyTraceInstance"} {
		entry, ok := bySummary[summary]
		if !ok {
			t.Fatalf("missing tool %q", summary)
		}
		body, ok := entry.InputSchema["body"].(map[string]any)
		if !ok {
			t.Fatalf("%s body schema missing: %+v", summary, entry.InputSchema)
		}
		props, ok := body["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s body properties missing: %+v", summary, body)
		}
		backend, ok := props["BackendConfig"].(map[string]any)
		if !ok {
			t.Fatalf("%s BackendConfig schema missing: %+v", summary, props["BackendConfig"])
		}
		if backend["type"] != "object" {
			t.Fatalf("%s BackendConfig should be object schema: %+v", summary, backend)
		}
		if backend["additionalProperties"] != true {
			t.Fatalf("%s BackendConfig should allow free-form object values: %+v", summary, backend)
		}
	}

	search, ok := bySummary["SearchTraces"]
	if !ok {
		t.Fatalf("missing tool SearchTraces")
	}
	searchBody, ok := search.InputSchema["body"].(map[string]any)
	if !ok {
		t.Fatalf("SearchTraces body schema missing: %+v", search.InputSchema)
	}
	searchProps, ok := searchBody["properties"].(map[string]any)
	if !ok {
		t.Fatalf("SearchTraces body properties missing: %+v", searchBody)
	}
	querySchema, ok := searchProps["Query"].(map[string]any)
	if !ok {
		t.Fatalf("SearchTraces Query schema missing: %+v", searchProps["Query"])
	}
	if querySchema["type"] != "object" {
		t.Fatalf("SearchTraces Query should be object schema: %+v", querySchema)
	}
	if querySchema["additionalProperties"] != true {
		t.Fatalf("SearchTraces Query should allow free-form object values: %+v", querySchema)
	}
}

func TestBuildOperationCatalogPreservesStructuredTraceSchemasWhenPresent(t *testing.T) {
	doc := swaggerDoc{
		Paths: map[string]swaggerPathItem{
			"/CreateTraceInstance": {
				Post: &swaggerOp{
					Summary: "CreateTraceInstance",
					Tags:    []string{"Trace"},
					Parameters: []swaggerParam{
						{
							Name:     "body",
							In:       "body",
							Required: true,
							Schema: &swaggerSchema{
								Type: "object",
								Properties: map[string]swaggerSchema{
									"BackendConfig": {
										Type: "object",
										Properties: map[string]swaggerSchema{
											"StorageType": {Type: "string"},
										},
										Required: []string{"StorageType"},
									},
								},
							},
						},
					},
				},
			},
			"/SearchTraces": {
				Post: &swaggerOp{
					Summary: "SearchTraces",
					Tags:    []string{"Trace"},
					Parameters: []swaggerParam{
						{
							Name:     "body",
							In:       "body",
							Required: true,
							Schema: &swaggerSchema{
								Type: "object",
								Properties: map[string]swaggerSchema{
									"Query": {
										Type: "object",
										Properties: map[string]swaggerSchema{
											"TraceID": {Type: "string"},
										},
										Required: []string{"TraceID"},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	got := buildOperationCatalogForTest(t, doc, "stage1", map[string]string{"Trace": "trace"}, map[string]string{}, map[string]apiDocEntry{}, toolCatalogOverrides{})
	if len(got.Operations) != 2 {
		t.Fatalf("operations=%d", len(got.Operations))
	}
	bySummary := map[string]contract.Operation{}
	for _, operation := range got.Operations {
		bySummary[operation.Docs.Summary] = operation
	}

	create := bySummary["CreateTraceInstance"]
	createBody := create.InputSchema["body"].(map[string]any)
	createProps := createBody["properties"].(map[string]any)
	backend := createProps["BackendConfig"].(map[string]any)
	backendProps, ok := backend["properties"].(map[string]any)
	if !ok || backendProps["StorageType"] == nil {
		t.Fatalf("expected structured BackendConfig to remain, got %+v", backend)
	}
	if backend["additionalProperties"] != nil {
		t.Fatalf("expected structured BackendConfig to avoid forced additionalProperties=true, got %+v", backend)
	}

	search := bySummary["SearchTraces"]
	searchBody := search.InputSchema["body"].(map[string]any)
	searchProps := searchBody["properties"].(map[string]any)
	query := searchProps["Query"].(map[string]any)
	queryProps, ok := query["properties"].(map[string]any)
	if !ok || queryProps["TraceID"] == nil {
		t.Fatalf("expected structured Query to remain, got %+v", query)
	}
	if query["additionalProperties"] != nil {
		t.Fatalf("expected structured Query to avoid forced additionalProperties=true, got %+v", query)
	}
}

func TestBuildOperationCatalogOverrideFields(t *testing.T) {
	doc := swaggerDoc{
		Paths: map[string]swaggerPathItem{
			"/DescribeProject": {
				Get: &swaggerOp{
					Summary: "DescribeProject",
					Tags:    []string{"Project"},
				},
			},
		},
	}
	overrides := toolCatalogOverrides{
		Risk: map[string]string{
			"project.describe": "high",
		},
		OutputPolicy: map[string]string{
			"project.describe": "full",
		},
		ErrorRecovery: map[string]string{
			"project.describe": "retry",
		},
		UsageConstraints: map[string]string{
			"project.describe": "require-region",
		},
	}

	got := buildOperationCatalogForTest(t, doc, "stage1", map[string]string{"Project": "project"}, map[string]string{}, map[string]apiDocEntry{}, overrides)
	if len(got.Operations) != 1 {
		t.Fatalf("operations=%d", len(got.Operations))
	}
	gotTool := got.Operations[0]
	if gotTool.Risk.Level != "high" {
		t.Fatalf("risk override=%q", gotTool.Risk.Level)
	}
	if gotTool.Output.Policy != "full" {
		t.Fatalf("output_policy override=%q", gotTool.Output.Policy)
	}
	if gotTool.Risk.ErrorRecovery != "retry" {
		t.Fatalf("error_recovery override=%q", gotTool.Risk.ErrorRecovery)
	}
	if gotTool.Docs.UsageConstraints != "require-region" {
		t.Fatalf("usage_constraints override=%q", gotTool.Docs.UsageConstraints)
	}
}

func TestBuildOperationCatalogSupportsAllMatchesGeneratedPaginationHeuristic(t *testing.T) {
	doc := swaggerDoc{
		Paths: map[string]swaggerPathItem{
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
			"/ListTagsForResources": {
				Get: &swaggerOp{
					Summary: "ListTagsForResources",
					Tags:    []string{"Tag"},
					Parameters: []swaggerParam{
						{Name: "NextToken", In: "query", Type: "string"},
						{Name: "MaxResults", In: "query", Type: "integer"},
					},
				},
			},
		},
	}
	groupKeys := map[string]string{
		"Project": "project",
		"Tag":     "tag",
	}

	source := buildSourceOperations(doc, groupKeys, map[string]string{}, map[string]apiDocEntry{}, toolCatalogOverrides{})
	if len(source) != 2 {
		t.Fatalf("operations=%d", len(source))
	}

	byID := map[string]sourceOperation{}
	for _, operation := range source {
		byID[operation.ID] = operation
	}
	if !byID["project.describe"].SupportsAll {
		t.Fatalf("expected DescribeProjects to support all")
	}
	if byID["tag.list"].SupportsAll {
		t.Fatalf("expected ListTagsForResources to stay unsupported until runtime pagination exists")
	}
}

func TestBuildOperationCatalogDoesNotFallbackAllLogPostActionsToCreate(t *testing.T) {
	doc := swaggerDoc{
		Paths: map[string]swaggerPathItem{
			"/PutLogs": {
				Post: &swaggerOp{Summary: "PutLogs", Tags: []string{"Log"}},
			},
			"/ConsumeLogs": {
				Post: &swaggerOp{Summary: "ConsumeLogs", Tags: []string{"Log"}},
			},
			"/CancelDownloadTask": {
				Post: &swaggerOp{Summary: "CancelDownloadTask", Tags: []string{"Log"}},
			},
			"/CreateDownloadTask": {
				Post: &swaggerOp{Summary: "CreateDownloadTask", Tags: []string{"Log"}},
			},
			"/WebTracks": {
				Post: &swaggerOp{Summary: "WebTracks", Tags: []string{"Log"}},
			},
		},
	}

	got := buildOperationCatalogForTest(t, doc, "stage1", map[string]string{"Log": "log"}, map[string]string{}, map[string]apiDocEntry{}, toolCatalogOverrides{})
	if len(got.Operations) != 5 {
		t.Fatalf("operations=%d", len(got.Operations))
	}
	verbs := map[string]string{}
	for _, operation := range got.Operations {
		verbs[operation.Docs.Summary] = strings.TrimSpace(operation.Verb)
	}
	if verbs["CreateDownloadTask"] != "create" {
		t.Fatalf("CreateDownloadTask verb=%q", verbs["CreateDownloadTask"])
	}
	for summary, want := range map[string]string{
		"PutLogs":            "put",
		"ConsumeLogs":        "consume",
		"CancelDownloadTask": "cancel",
		"WebTracks":          "track",
	} {
		if verbs[summary] != want {
			t.Fatalf("%s verb=%q want=%q", summary, verbs[summary], want)
		}
	}
}

func asTestString(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func buildOperationCatalogForTest(
	t *testing.T,
	doc swaggerDoc,
	version string,
	groupKeys map[string]string,
	tagTitles map[string]string,
	docIndex map[string]apiDocEntry,
	overrides toolCatalogOverrides,
) contract.Catalog {
	t.Helper()
	source := buildSourceOperations(doc, groupKeys, tagTitles, docIndex, overrides)
	// Input/schema tests use synthetic action IDs that are intentionally not
	// part of the explicit production pagination registry.
	for i := range source {
		source[i].SupportsAll = false
	}
	catalog, err := buildOperationCatalogV2FromSource(version, source)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}
