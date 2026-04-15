package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRequestTemplateOutputMode(t *testing.T) {
	ops := []apiActionOp{
		{
			Cmd: apiCapabilityCommand{
				Params: []apiCapParam{
					{In: "body", Ref: "#/definitions/project.CreateProjectReq"},
				},
			},
		},
	}
	required := map[string]string{
		"#/definitions/project.CreateProjectReq": `{"ProjectName":""}`,
	}
	full := map[string]string{
		"#/definitions/project.CreateProjectReq": `{"ProjectName":"","Description":""}`,
	}
	if got := requestTemplateOutput(ops, "required", required, full); got != `{"ProjectName":""}` {
		t.Fatalf("required template mismatch: %s", got)
	}
	if got := requestTemplateOutput(ops, "full", required, full); got != `{"ProjectName":"","Description":""}` {
		t.Fatalf("full template mismatch: %s", got)
	}
}

func TestDescribeOperationOutput(t *testing.T) {
	ops := []apiActionOp{
		{
			Cmd: apiCapabilityCommand{
				Group:       "project",
				Action:      "CreateProject",
				GroupTitle:  "日志项目管理",
				Summary:     "CreateProject",
				Description: "创建日志项目请求",
				Method:      "POST",
				Path:        "/CreateProject",
				InputMode:   "body via --request; query/path via flags",
				RequiredFlags: []string{
					"--query ProjectName",
				},
				Params: []apiCapParam{
					{Name: "data", In: "body", Required: true, Ref: "#/definitions/project.CreateProjectReq"},
					{Name: "ProjectName", In: "query", Required: true, Type: "string", MinLength: intPtr(1)},
				},
				RequestParamsDoc: []apiCapDocParam{
					{Name: "ProjectName", In: "body", Type: "String", RequiredText: "是", Description: "日志项目名称"},
				},
			},
			ParamFlags: map[string]apiCapParam{
				"--project-name": {Name: "ProjectName", In: "query", Required: true, Type: "string", MinLength: intPtr(1)},
			},
		},
	}
	required := map[string]string{
		"#/definitions/project.CreateProjectReq": `{"ProjectName":""}`,
	}
	full := map[string]string{
		"#/definitions/project.CreateProjectReq": `{"ProjectName":"","Description":""}`,
	}
	s, err := describeOperationOutput("project", "CreateProject", ops, required, full)
	if err != nil {
		t.Fatalf("describe error: %v", err)
	}
	for _, want := range []string{
		`"group": "project"`,
		`"group_title": "日志项目管理"`,
		`"action": "CreateProject"`,
		`"description":`,
		`"method": "POST"`,
		`"input_mode":`,
		`"input": {`,
		`"flags": {`,
		`"fields": [`,
		`"request_body": {`,
		`"required": true`,
		`"print_template":`,
		`"note":`,
		`"output_filter_scope"`,
		`"output_filter_examples"`,
		`"shell_quoting"`,
		`"min_length": 1`,
		`"cli_flag": "--project-name"`,
		`"guidance"`,
		`--jmes-filter \"keys(@)\"`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("describe output missing %q: %s", want, s)
		}
	}
	for _, notWant := range []string{
		`"params":`,
		`"required_flags":`,
		`"required_params":`,
		`"optional_params":`,
		`"param_guidance":`,
		`"template_guidance":`,
		`"body_params_doc":`,
		`"query_params_doc":`,
		`"template_required":`,
		`"template_full":`,
		`"name": "data"`,
		`"summary":`,
		`"swagger_tag"`,
		`"doc_path"`,
		`"ref": "#/definitions/project.CreateProjectReq"`,
	} {
		if strings.Contains(s, notWant) {
			t.Fatalf("describe output should not include %q: %s", notWant, s)
		}
	}
}

func TestParseGeneratedCallArgsAcceptsRawAPIParamStyleFlags(t *testing.T) {
	ops := []apiActionOp{
		{
			Cmd: apiCapabilityCommand{
				Method: "GET",
				Path:   "/DescribeHostGroupsV2",
				Params: []apiCapParam{
					{Name: "PageNumber", In: "query", Type: "integer"},
					{Name: "PageSize", In: "query", Type: "integer"},
				},
			},
			ParamFlags: map[string]apiCapParam{
				"--page-number": {Name: "PageNumber", In: "query", Type: "integer"},
				"--page-size":   {Name: "PageSize", In: "query", Type: "integer"},
			},
		},
	}
	_, _, query, _, _, _, err := parseGeneratedCallArgs([]string{"--PageNumber", "1", "--PageSize", "100"}, ops)
	if err != nil {
		t.Fatalf("parse generated args error: %v", err)
	}
	if query["PageNumber"] != "1" || query["PageSize"] != "100" {
		t.Fatalf("unexpected query: %#v", query)
	}
}

func TestDescribeOperationOutputHidesEmptyTemplates(t *testing.T) {
	ops := []apiActionOp{
		{
			Cmd: apiCapabilityCommand{
				Group:   "project",
				Action:  "CreateProject",
				Summary: "CreateProject",
				Method:  "POST",
				Path:    "/CreateProject",
				Params: []apiCapParam{
					{Name: "data", In: "body", Required: true, Ref: "#/definitions/project.CreateProjectReq"},
				},
			},
		},
	}
	required := map[string]string{
		"#/definitions/project.CreateProjectReq": `{}`,
	}
	full := map[string]string{
		"#/definitions/project.CreateProjectReq": `{}`,
	}
	s, err := describeOperationOutput("project", "CreateProject", ops, required, full)
	if err != nil {
		t.Fatalf("describe error: %v", err)
	}
	if strings.Contains(s, `"template_required"`) || strings.Contains(s, `"template_full"`) {
		t.Fatalf("empty templates should be omitted: %s", s)
	}
}

func TestDescribeOperationOutputStableFieldOrder(t *testing.T) {
	ops := []apiActionOp{
		{
			Cmd: apiCapabilityCommand{
				Group:       "project",
				Action:      "CreateProject",
				Summary:     "CreateProject",
				Description: "创建日志项目请求",
				Method:      "POST",
				Path:        "/CreateProject",
				InputMode:   "body via --request",
				RequiredFlags: []string{
					"--query ProjectName",
				},
				Params: []apiCapParam{
					{Name: "data", In: "body", Required: true},
					{Name: "ProjectName", In: "query", Required: true, Type: "string"},
				},
			},
		},
	}
	s, err := describeOperationOutput("project", "CreateProject", ops, map[string]string{}, map[string]string{})
	if err != nil {
		t.Fatalf("describe error: %v", err)
	}
	order := []string{
		`"group":`,
		`"group_title":`,
		`"action":`,
		`"description":`,
		`"method":`,
		`"path":`,
		`"input_mode":`,
		`"input":`,
		`"output_filter_scope":`,
		`"output_filter_examples":`,
		`"shell_quoting":`,
		`"guidance":`,
	}
	last := -1
	for _, key := range order {
		idx := strings.Index(s, key)
		if key == `"guidance":` {
			idx = strings.LastIndex(s, key)
		}
		if idx == -1 {
			t.Fatalf("missing key %s in output: %s", key, s)
		}
		if idx < last {
			t.Fatalf("key order mismatch, key=%s idx=%d last=%d output=%s", key, idx, last, s)
		}
		last = idx
	}
}

func TestGeneratedDescribeSeparatesOptionalParamsAndUsageGuidance(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"api", "topic", "DescribeTopics", "--describe"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		`"input": {`,
		`"flags": {`,
		`"fields": [`,
		`"required": false`,
		`"optional": "只在用户明确给出过滤、分页、排序、范围或额外约束时，再填写 optional；不填表示按接口默认行为执行，不要从示例或历史请求里补齐。`,
		`"--project-id"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in output: %s", want, out)
		}
	}
	for _, notWant := range []string{
		`"Cursor"`,
		`"--cursor"`,
		`"Region"`,
		`"--region"`,
		`"FuzzySearchKey"`,
		`"--fuzzy-search-key"`,
		`"Favourite"`,
		`"--favourite"`,
		`"OrderByProject"`,
		`"--order-by-project"`,
		`"IsFullName"`,
		`"--is-full-name"`,
		`"Description"`,
		`"--description"`,
	} {
		if strings.Contains(out, notWant) {
			t.Fatalf("describe output should hide undocumented param %q: %s", notWant, out)
		}
	}
	for _, notWant := range []string{
		`"params":`,
		`"request_body":`,
		`"required_flags":`,
		`"required_params":`,
		`"optional_params":`,
		`"param_guidance":`,
		`"template_guidance":`,
		`"body_params_doc":`,
		`"query_params_doc":`,
	} {
		if strings.Contains(out, notWant) {
			t.Fatalf("describe output should omit legacy field %q: %s", notWant, out)
		}
	}
}

func TestGeneratedDescribeIncludesTemplateGuidance(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"api", "log", "SearchLogs", "--describe"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		`"input": {`,
		`"request_body": {`,
		`"print_template":`,
		`"note":`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in output: %s", want, out)
		}
	}
}

func TestGeneratedDescribeCreateTopicIncludesBodyFieldsFromOfficialDoc(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"api", "topic", "CreateTopic", "--describe"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		`"request_body": {`,
		`"fields": [`,
		`"name": "TopicName"`,
		`"name": "ProjectId"`,
		`"name": "Ttl"`,
		`"name": "EnableHotTtl"`,
		`"required": true`,
		`日志主题所属的日志项目 ID。`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in output: %s", want, out)
		}
	}
	for _, notWant := range []string{
		`"name": "data"`,
		`:::tip`,
		`命名规则请参考`,
		`详细说明请参考`,
	} {
		if strings.Contains(out, notWant) {
			t.Fatalf("request body should expand official doc fields instead of generic data: %s", out)
		}
	}
}

func TestGeneratedDescribePutLogsMentionsMillisecondTimestamp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"api", "log", "PutLogs", "--describe"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		`"input": {`,
		`"request_body": {`,
		`毫秒`,
		`1710374400000`,
		`不要填秒级`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in output: %s", want, out)
		}
	}
}

func TestGeneratedDescribeOfficialNoParamTableOmitsSwaggerFallback(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"api", "log", "DescribeCursorTime", "--describe"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, notWant := range []string{
		`"input": {`,
		`"flags": {`,
		`"request_body": {`,
		`"Cursor"`,
		`"--cursor"`,
	} {
		if strings.Contains(out, notWant) {
			t.Fatalf("official no-table describe should not fall back to swagger param %q: %s", notWant, out)
		}
	}
}

func TestGeneratedDescribeDoesNotEscapeAngleBrackets(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"api", "project", "CreateProject", "--describe"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	if strings.Contains(out, `\u003c`) || strings.Contains(out, `\u003e`) {
		t.Fatalf("angle brackets should not be escaped: %q", out)
	}
}

func TestRequestTemplateOutputFallsBackToDocParamsWhenSchemaTemplateEmpty(t *testing.T) {
	ops := []apiActionOp{
		{
			Cmd: apiCapabilityCommand{
				Params: []apiCapParam{
					{In: "body", Ref: "#/definitions/project.CreateProjectReq"},
				},
				RequestParamsDoc: []apiCapDocParam{
					{Name: "ProjectName", In: "body", Type: "String", RequiredText: "是"},
					{Name: "Region", In: "body", Type: "String", RequiredText: "是"},
					{Name: "Description", In: "body", Type: "String", RequiredText: "否"},
				},
			},
		},
	}
	required := map[string]string{
		"#/definitions/project.CreateProjectReq": `{}`,
	}
	full := map[string]string{
		"#/definitions/project.CreateProjectReq": `{}`,
	}
	gotRequired := requestTemplateOutput(ops, "required", required, full)
	gotFull := requestTemplateOutput(ops, "full", required, full)
	if !strings.Contains(gotRequired, `"ProjectName"`) || strings.Contains(gotRequired, `"Description"`) {
		t.Fatalf("required fallback template unexpected: %s", gotRequired)
	}
	if !strings.Contains(gotFull, `"ProjectName"`) || !strings.Contains(gotFull, `"Description"`) {
		t.Fatalf("full fallback template unexpected: %s", gotFull)
	}
}

func TestRequestTemplateOutputUsesExpandedPutLogsTemplate(t *testing.T) {
	ops := []apiActionOp{
		{
			Cmd: apiCapabilityCommand{
				Group:  "log",
				Action: "PutLogs",
				Params: []apiCapParam{
					{In: "body", Ref: "#/definitions/code_byted_org_storage_tls-lib_proto_pb.LogGroupList"},
				},
			},
		},
	}

	gotRequired := requestTemplateOutput(ops, "required", generatedRequestTemplates, generatedRequestTemplatesFull)
	gotFull := requestTemplateOutput(ops, "full", generatedRequestTemplates, generatedRequestTemplatesFull)

	for _, got := range []string{gotRequired, gotFull} {
		if !strings.Contains(got, `"Logs"`) || !strings.Contains(got, `"Contents"`) || !strings.Contains(got, `"Key"`) || !strings.Contains(got, `"Value"`) {
			t.Fatalf("putlogs template should expand nested log fields: %s", got)
		}
	}
}

func TestRequestTemplateOutputUsesMillisecondPutLogsTimestamp(t *testing.T) {
	ops := []apiActionOp{
		{
			Cmd: apiCapabilityCommand{
				Group:  "log",
				Action: "PutLogs",
				Params: []apiCapParam{
					{In: "body", Ref: "#/definitions/code_byted_org_storage_tls-lib_proto_pb.LogGroupList"},
				},
			},
		},
	}

	for _, mode := range []string{"required", "full"} {
		got := requestTemplateOutput(ops, mode, generatedRequestTemplates, generatedRequestTemplatesFull)
		if !strings.Contains(got, `"Time": 1710374400000`) {
			t.Fatalf("putlogs %s template should use a millisecond timestamp example: %s", mode, got)
		}
	}
}

func TestGeneratedDescribeUsesLeanRequestBodyHint(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"api", "project", "CreateProject", "--describe"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		`"group_title": "日志项目管理"`,
		`"input": {`,
		`"request_body": {`,
		`"fields": [`,
		`"name": "ProjectName"`,
		`"required": true`,
		`"print_template":`,
		`"note":`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in output: %s", want, out)
		}
	}
	for _, notWant := range []string{
		`"body": {`,
		`"query": {`,
		`"required_flags":`,
		`"template_required":`,
		`"template_full":`,
	} {
		if strings.Contains(out, notWant) {
			t.Fatalf("create project describe should stay lean and omit %q: %s", notWant, out)
		}
	}
}

func TestGeneratedDescribeLargeOutputIncludesFileHint(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"api", "log", "SearchLogs", "--describe"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		`"preferred_output_mode": "file"`,
		`"recommended_global_flags": [`,
		`"--output-mode file"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in output: %s", want, out)
		}
	}
}

func TestGeneratedRequestTemplateUsesDocParamsForCreateProject(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"api", "project", "CreateProject", "--print-request-template=full"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{`"ProjectName"`, `"Region"`, `"Description"`, `"IamProjectName"`, `"Tags"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in output: %s", want, out)
		}
	}
}

func intPtr(v int) *int {
	return &v
}
