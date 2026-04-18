package cli

import (
	"fmt"
	"sort"
	"strings"
)

type workflowCatalog struct {
	ID                     string
	Group                  string
	Command                string
	Action                 string
	Summary                string
	Description            string
	Method                 string
	Path                   string
	InputMode              string
	PreferredOutputMode    string
	RecommendedGlobalFlags []string
	Params                 []apiCapParam
	Notes                  []string
	APIGroup               string
	APIAction              string
	BackedBy               []string
	Source                 string
}

func workflowCatalogEntries(group string) []workflowCatalog {
	entries := workflowCatalogSource()
	if strings.TrimSpace(group) == "" {
		return entries
	}
	target := normalizeToken(group)
	out := make([]workflowCatalog, 0, len(entries))
	for _, item := range entries {
		if normalizeToken(item.Group) != target {
			continue
		}
		out = append(out, item)
	}
	return out
}

func resolveWorkflowByIdentity(group, command string) (workflowCatalog, error) {
	g := normalizeToken(group)
	c := normalizeToken(command)
	for _, item := range workflowCatalogSource() {
		if normalizeToken(item.Group) != g {
			continue
		}
		if normalizeToken(item.Command) != c && normalizeToken(item.ID) != normalizeToken(group+"."+command) {
			continue
		}
		return item, nil
	}
	return workflowCatalog{}, fmt.Errorf("unknown workflow: %s.%s", strings.TrimSpace(group), strings.TrimSpace(command))
}

func workflowCatalogSource() []workflowCatalog {
	out := []workflowCatalog{
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
			Params: []apiCapParam{
				flagParam("TopicId", "--topic-id", "body", true, "string", "主题 ID"),
				flagParam("Query", "--query", "body", true, "string", "非分析查询语句"),
				flagParam("StartTime", "--from", "body", true, "integer", "起始毫秒时间戳"),
				flagParam("EndTime", "--to", "body", true, "integer", "结束毫秒时间戳"),
				flagParam("maxPages", "--max-pages", "meta", false, "integer", "最多翻页数量"),
				flagParam("request", "--request", "body", false, "json", "完整请求 JSON"),
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
			Description:         "沿用 SearchLogs 的 SQL/分析 Query 语法，面向大量分析行结果导出；交互式分析和少量结果优先用 log.search。",
			Method:              "POST",
			Path:                "/SearchLogs",
			InputMode:           "body via --request; analysis mode",
			PreferredOutputMode: "file",
			RecommendedGlobalFlags: []string{
				"--output jsonl",
				"--output-mode file",
			},
			Params: []apiCapParam{
				flagParam("TopicId", "--topic-id", "body", true, "string", "主题 ID"),
				flagParam("Query", "--query", "body", true, "string", "分析查询语句，示例：*|select count(*)"),
				flagParam("StartTime", "--from", "body", true, "integer", "起始毫秒时间戳"),
				flagParam("EndTime", "--to", "body", true, "integer", "结束毫秒时间戳"),
				flagParam("request", "--request", "body", false, "json", "完整请求 JSON"),
			},
			APIGroup:  "log",
			APIAction: "SearchLogs",
			BackedBy:  []string{"SearchLogs"},
			Source:    "cli_workflow",
			Notes: []string{
				"与 log.search 一样使用 SearchLogs 的 SQL/分析 Query 语法；区别在于这里的定位是大结果导出，而不是交互式预览。",
				"如果只是担心 stdout 过大，但并不需要完整分析行导出，先留在 log.search，让 CLI 的 deliveryMode 决定 stdout 还是 file_auto。",
				"分析可见列强依赖当前索引配置；新增或修改索引后通常只对增量写入生效，旧日志对应列可能仍为 null。",
			},
		},
		{
			ID:          "log.ingest",
			Group:       "log",
			Command:     "ingest",
			Action:      "log.ingest",
			Summary:     "批量导入文本或 JSON 日志",
			Description: "面向本地 lines/jsonl/json-array 导入工作流；CLI 负责补时间、组批、统计头和 protobuf 编码。",
			Method:      "POST",
			Path:        "/PutLogs",
			InputMode:   "ingest via --input; lines/jsonl/json-array normalized by CLI before PutLogs",
			Params: []apiCapParam{
				flagParam("TopicId", "--topic-id", "query", true, "string", "主题 ID"),
				flagParam("input", "--input", "meta", true, "path|-", "输入内容；支持 file://...、-、裸文件路径"),
				flagParam("inputFormat", "--input-format", "meta", false, "string", "lines/jsonl/json-array"),
				flagParam("timeField", "--time-field", "meta", false, "string", "jsonl/json-array 的时间字段名"),
				flagParam("timeFormat", "--time-format", "meta", false, "string", "auto/unix_ms/unix/rfc3339"),
				flagParam("Source", "--source", "group", false, "string", "本次写入共用 Source"),
				flagParam("FileName", "--file-name", "group", false, "string", "本次写入共用 FileName"),
				flagParam("LogTags", "--tag", "group", false, "string", "重复传入 k=v 形式的 LogTag"),
				flagParam("batchMaxCount", "--batch-max-count", "meta", false, "integer", "每批最大发送条数，默认 500"),
				flagParam("x-tls-compresstype", "--compress-type", "header", false, "string", "lz4/zlib/none，默认 lz4"),
				flagParam("x-tls-hashkey", "--hash-key", "header", false, "string", "指定写入分区的 HashKey"),
			},
			APIGroup:  "log",
			APIAction: "PutLogs",
			BackedBy:  []string{"PutLogs"},
			Source:    "cli_workflow",
			Notes: []string{
				"这是本地导入工作流，不是 tool log.put 的别名；如果你需要直接按公开 PutLogs 契约构造请求，请改用 tool log.put。",
				"lines 输入默认写入字段 __content__。",
				"未指定 --time-field 时，CLI 会用本次命令启动时的毫秒时间戳补齐日志时间。",
				"jsonl/json-array 会保留用户原始字段，不做 message 字段重映射。",
				"每个批次都会自动带上 log-count、earliest-log-time、latest-log-time 请求头。",
			},
		},
	}
	sort.Slice(out, func(i, j int) bool {
		if normalizeToken(out[i].Group) == normalizeToken(out[j].Group) {
			return normalizeToken(out[i].ID) < normalizeToken(out[j].ID)
		}
		return normalizeToken(out[i].Group) < normalizeToken(out[j].Group)
	})
	return out
}

func workflowDescribeOutput(spec workflowCatalog) map[string]any {
	executionSchema := workflowExecutionSchema()
	contextSchema := compactToolContextSchema(enrichToolContextSchema(map[string]any{}, executionSchema, false))
	return map[string]any{
		"kind":                     "workflow",
		"source":                   spec.Source,
		"group":                    spec.Group,
		"command":                  spec.Command,
		"action":                   spec.Action,
		"summary":                  spec.Summary,
		"description":              spec.Description,
		"method":                   strings.ToUpper(spec.Method),
		"path":                     spec.Path,
		"input_mode":               spec.InputMode,
		"preferred_output_mode":    spec.PreferredOutputMode,
		"recommended_global_flags": append([]string(nil), spec.RecommendedGlobalFlags...),
		"backed_by":                append([]string(nil), spec.BackedBy...),
		"input_schema":             workflowInputSchema(spec),
		"input_encoding_hint": map[string]any{
			"transport":   "--input accepts file://req.json, -, or inline JSON object.",
			"recommended": "Workflow input is a flat JSON object; keep fields like TopicId, Query, StartTime, EndTime at the top level.",
		},
		"context_schema":   contextSchema,
		"execution_schema": compactToolExecutionSchema(executionSchema),
		"notes":            workflowNotes(spec),
		"guidance": apiDescribeGuidance{
			ListGroup:         "volclog workflow list " + spec.Group,
			Describe:          "volclog workflow describe " + spec.ID,
			Execute:           "volclog workflow exec " + spec.ID + " --input file://req.json",
			FallbackDiscovery: "volclog tool list " + spec.APIGroup,
		},
	}
}

func workflowExecutionSchema() map[string]any {
	return enrichToolExecutionSchema(map[string]any{}, false)
}

func workflowNotes(spec workflowCatalog) []string {
	out := append([]string(nil), spec.Notes...)
	out = append(out, "tool 仍只暴露官网公开 API；workflow 暴露的是 CLI 高层编排。")
	if strings.TrimSpace(spec.PreferredOutputMode) == "file" {
		out = append(out, "大结果优先使用 artifact/file 输出；stdout 更适合预览或配合 execution.projection。")
	}
	return out
}

func workflowInputSchema(spec workflowCatalog) map[string]any {
	fields := workflowParamsWithDoc(spec)
	props := map[string]any{}
	required := make([]string, 0, len(fields))
	for _, param := range fields {
		name := workflowInputFieldName(param.Name)
		if name == "" {
			continue
		}
		props[name] = workflowFieldSchema(param)
		if param.Required {
			required = append(required, name)
		}
	}
	sort.Strings(required)
	out := map[string]any{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

func workflowParamsWithDoc(spec workflowCatalog) []apiCapParam {
	fields := append([]apiCapParam(nil), spec.Params...)
	if strings.TrimSpace(spec.APIGroup) == "" || strings.TrimSpace(spec.APIAction) == "" {
		return fields
	}
	ops, err := shortcutActionOps(spec.APIGroup, spec.APIAction)
	if err != nil || len(ops) == 0 {
		return fields
	}
	bodyDoc, docs := splitRequestParamsDocForOutput(ops[0].Cmd.RequestParamsDoc)
	doc := append(bodyDoc, docs...)
	return mergeParamsWithDoc(fields, doc)
}

func workflowInputFieldName(name string) string {
	switch strings.TrimSpace(name) {
	case "":
		return ""
	case "request":
		return "Request"
	case "requestFormat":
		return "RequestFormat"
	case "input":
		return "Input"
	case "inputFormat":
		return "InputFormat"
	case "timeField":
		return "TimeField"
	case "timeFormat":
		return "TimeFormat"
	case "batchMaxCount":
		return "BatchMaxCount"
	case "maxPages":
		return "MaxPages"
	case "x-tls-compresstype":
		return "CompressType"
	case "x-tls-hashkey":
		return "HashKey"
	case "Content-MD5":
		return "ContentMD5"
	}
	runes := []rune(strings.TrimSpace(name))
	if len(runes) == 0 {
		return ""
	}
	runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
	return string(runes)
}

func workflowFieldSchema(param apiCapParam) map[string]any {
	out := map[string]any{
		"type": workflowFieldType(param),
	}
	if desc := conciseFieldDescription(param.Description); desc != "" {
		out["description"] = desc
	}
	if len(param.Enum) > 0 {
		out["enum"] = append([]string(nil), param.Enum...)
	}
	return out
}

func workflowFieldType(param apiCapParam) string {
	name := workflowInputFieldName(param.Name)
	switch name {
	case "Request":
		return "object"
	case "LogTags":
		return "array"
	}
	t := strings.ToLower(strings.TrimSpace(param.Type))
	switch {
	case strings.Contains(t, "boolean"):
		return "boolean"
	case strings.Contains(t, "integer"), strings.Contains(t, "number"):
		return "integer"
	case strings.Contains(t, "array"):
		return "array"
	case strings.Contains(t, "json"):
		return "object"
	default:
		return "string"
	}
}
