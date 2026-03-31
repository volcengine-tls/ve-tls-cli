package main

import (
	"strings"
	"testing"
)

func TestBuildCapabilities_FallsBackToRawSwaggerTagForGroup(t *testing.T) {
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
		"账号管理": "account",
		"Account": "account",
	}

	got := buildCapabilities(doc, "stage1", groupKeys, map[string]string{}, map[string]apiDocEntry{})
	if len(got.Commands) != 1 {
		t.Fatalf("commands=%d", len(got.Commands))
	}
	if got.Commands[0].Group != "account" {
		t.Fatalf("group=%q", got.Commands[0].Group)
	}
	if got.Commands[0].GroupTitle != "Account" {
		t.Fatalf("group title=%q", got.Commands[0].GroupTitle)
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
