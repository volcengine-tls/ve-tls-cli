package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWorkflowCatalogSourceDoesNotReferenceShortcutLookup(t *testing.T) {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	src := filepath.Join(filepath.Dir(file), "workflow_meta.go")

	raw, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read workflow_meta.go: %v", err)
	}
	for _, forbidden := range []string{"lookupShortcutSpec", "shortcutCommandSpec"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("workflow_meta.go should not reference %q after decoupling", forbidden)
		}
	}
}

func TestWorkflowCatalogSourceMatchesCurrentWorkflowMetadata(t *testing.T) {
	t.Helper()

	got := workflowCatalogSource()
	want := []workflowCatalog{
		{
			ID:                  "log.export",
			Group:               "log",
			Command:             "export",
			Action:              "log.export",
			Summary:             "自动翻页导出日志",
			Description:         "面向纯检索结果导出；分析语句请改用 export-analysis。",
			Method:              "POST",
			Path:                "/SearchLogs",
			InputMode:           "body via --request; auto-pagination in command",
			PreferredOutputMode: "file",
			RecommendedGlobalFlags: []string{
				"--output jsonl",
				"--output-mode file",
			},
			APIGroup:  "log",
			APIAction: "SearchLogs",
			BackedBy:  []string{"SearchLogs"},
			Source:    "cli_workflow",
		},
		{
			ID:                  "log.export-analysis",
			Group:               "log",
			Command:             "export-analysis",
			Action:              "log.export-analysis",
			Summary:             "导出分析行结果",
			Description:         "面向大量 SQL/分析结果导出；交互式分析和少量结果优先用 log.search。",
			Method:              "POST",
			Path:                "/SearchLogs",
			InputMode:           "body via --request; analysis mode",
			PreferredOutputMode: "file",
			RecommendedGlobalFlags: []string{
				"--output jsonl",
				"--output-mode file",
			},
			APIGroup:  "log",
			APIAction: "SearchLogs",
			BackedBy:  []string{"SearchLogs"},
			Source:    "cli_workflow",
			Notes: []string{
				"同样基于 SearchLogs 的 SQL/分析语句，但定位是大结果导出；交互式探索和少量预览优先使用 log.search。",
				"分析可见列强依赖当前索引配置；新增或修改索引后通常只对增量写入生效，旧日志对应列可能仍为 null。",
			},
		},
		{
			ID:          "log.ingest",
			Group:       "log",
			Command:     "ingest",
			Action:      "log.ingest",
			Summary:     "批量导入文本或 JSON 日志",
			Description: "面向高层写入场景；CLI 负责补时间、组批、统计头和 protobuf 编码。",
			Method:      "POST",
			Path:        "/PutLogs",
			InputMode:   "ingest via --input; lines/jsonl/json-array normalized by CLI before PutLogs",
			APIGroup:    "log",
			APIAction:   "PutLogs",
			BackedBy:    []string{"PutLogs"},
			Source:      "cli_workflow",
			Notes: []string{
				"lines 输入默认写入字段 __content__。",
				"未指定 --time-field 时，CLI 会用本次命令启动时的毫秒时间戳补齐日志时间。",
				"jsonl/json-array 会保留用户原始字段，不做 message 字段重映射。",
				"每个批次都会自动带上 log-count、earliest-log-time、latest-log-time 请求头。",
			},
		},
	}

	if len(got) != len(want) {
		t.Fatalf("unexpected workflow count: got=%d want=%d", len(got), len(want))
	}
	for i := range want {
		if got[i].ID != want[i].ID ||
			got[i].Group != want[i].Group ||
			got[i].Command != want[i].Command ||
			got[i].Action != want[i].Action ||
			got[i].Summary != want[i].Summary ||
			got[i].Description != want[i].Description ||
			got[i].Method != want[i].Method ||
			got[i].Path != want[i].Path ||
			got[i].InputMode != want[i].InputMode ||
			got[i].PreferredOutputMode != want[i].PreferredOutputMode ||
			got[i].APIGroup != want[i].APIGroup ||
			got[i].APIAction != want[i].APIAction ||
			got[i].Source != want[i].Source {
			t.Fatalf("workflow[%d] mismatch:\n got=%#v\nwant=%#v", i, got[i], want[i])
		}
		if len(got[i].BackedBy) != len(want[i].BackedBy) {
			t.Fatalf("workflow[%d] backed_by mismatch: got=%#v want=%#v", i, got[i].BackedBy, want[i].BackedBy)
		}
		for j := range want[i].BackedBy {
			if got[i].BackedBy[j] != want[i].BackedBy[j] {
				t.Fatalf("workflow[%d] backed_by mismatch: got=%#v want=%#v", i, got[i].BackedBy, want[i].BackedBy)
			}
		}
		if len(got[i].RecommendedGlobalFlags) != len(want[i].RecommendedGlobalFlags) {
			t.Fatalf("workflow[%d] recommended_global_flags mismatch: got=%#v want=%#v", i, got[i].RecommendedGlobalFlags, want[i].RecommendedGlobalFlags)
		}
		for j := range want[i].RecommendedGlobalFlags {
			if got[i].RecommendedGlobalFlags[j] != want[i].RecommendedGlobalFlags[j] {
				t.Fatalf("workflow[%d] recommended_global_flags mismatch: got=%#v want=%#v", i, got[i].RecommendedGlobalFlags, want[i].RecommendedGlobalFlags)
			}
		}
		if len(got[i].Notes) != len(want[i].Notes) {
			t.Fatalf("workflow[%d] notes mismatch: got=%#v want=%#v", i, got[i].Notes, want[i].Notes)
		}
		for j := range want[i].Notes {
			if got[i].Notes[j] != want[i].Notes[j] {
				t.Fatalf("workflow[%d] notes mismatch: got=%#v want=%#v", i, got[i].Notes, want[i].Notes)
			}
		}
		if len(got[i].Params) != len(wantWorkflowParams(got[i].ID)) {
			t.Fatalf("workflow[%d] params mismatch: got=%#v want=%#v", i, got[i].Params, wantWorkflowParams(got[i].ID))
		}
		for j, wantParam := range wantWorkflowParams(got[i].ID) {
			gotParam := got[i].Params[j]
			if gotParam.Name != wantParam.Name || gotParam.CLIFlag != wantParam.CLIFlag || gotParam.In != wantParam.In || gotParam.Required != wantParam.Required || gotParam.Type != wantParam.Type {
				t.Fatalf("workflow[%d] params[%d] mismatch: got=%#v want=%#v", i, j, gotParam, wantParam)
			}
		}
	}
}

func wantWorkflowParams(id string) []apiCapParam {
	switch id {
	case "log.export":
		return []apiCapParam{
			{Name: "TopicId", CLIFlag: "--topic-id", In: "body", Required: true, Type: "string"},
			{Name: "Query", CLIFlag: "--query", In: "body", Required: true, Type: "string"},
			{Name: "StartTime", CLIFlag: "--from", In: "body", Required: true, Type: "integer"},
			{Name: "EndTime", CLIFlag: "--to", In: "body", Required: true, Type: "integer"},
			{Name: "maxPages", CLIFlag: "--max-pages", In: "meta", Required: false, Type: "integer"},
			{Name: "request", CLIFlag: "--request", In: "body", Required: false, Type: "json"},
		}
	case "log.export-analysis":
		return []apiCapParam{
			{Name: "TopicId", CLIFlag: "--topic-id", In: "body", Required: true, Type: "string"},
			{Name: "Query", CLIFlag: "--query", In: "body", Required: true, Type: "string"},
			{Name: "StartTime", CLIFlag: "--from", In: "body", Required: true, Type: "integer"},
			{Name: "EndTime", CLIFlag: "--to", In: "body", Required: true, Type: "integer"},
			{Name: "request", CLIFlag: "--request", In: "body", Required: false, Type: "json"},
		}
	case "log.ingest":
		return []apiCapParam{
			{Name: "TopicId", CLIFlag: "--topic-id", In: "query", Required: true, Type: "string"},
			{Name: "input", CLIFlag: "--input", In: "meta", Required: true, Type: "path|-"},
			{Name: "inputFormat", CLIFlag: "--input-format", In: "meta", Required: false, Type: "string"},
			{Name: "timeField", CLIFlag: "--time-field", In: "meta", Required: false, Type: "string"},
			{Name: "timeFormat", CLIFlag: "--time-format", In: "meta", Required: false, Type: "string"},
			{Name: "Source", CLIFlag: "--source", In: "group", Required: false, Type: "string"},
			{Name: "FileName", CLIFlag: "--file-name", In: "group", Required: false, Type: "string"},
			{Name: "LogTags", CLIFlag: "--tag", In: "group", Required: false, Type: "string"},
			{Name: "batchMaxCount", CLIFlag: "--batch-max-count", In: "meta", Required: false, Type: "integer"},
			{Name: "x-tls-compresstype", CLIFlag: "--compress-type", In: "header", Required: false, Type: "string"},
			{Name: "x-tls-hashkey", CLIFlag: "--hash-key", In: "header", Required: false, Type: "string"},
		}
	default:
		return nil
	}
}
