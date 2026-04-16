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
	candidates := [][2]string{
		{"log", "export"},
		{"log", "export-analysis"},
		{"log", "ingest"},
	}
	out := make([]workflowCatalog, 0, len(candidates))
	for _, item := range candidates {
		spec, ok := lookupShortcutSpec(item[0], item[1])
		if !ok {
			continue
		}
		out = append(out, workflowFromShortcutSpec(spec))
	}
	sort.Slice(out, func(i, j int) bool {
		if normalizeToken(out[i].Group) == normalizeToken(out[j].Group) {
			return normalizeToken(out[i].ID) < normalizeToken(out[j].ID)
		}
		return normalizeToken(out[i].Group) < normalizeToken(out[j].Group)
	})
	return out
}

func workflowFromShortcutSpec(spec shortcutCommandSpec) workflowCatalog {
	backedBy := []string{}
	if action := strings.TrimSpace(spec.APIAction); action != "" {
		backedBy = append(backedBy, action)
	}
	return workflowCatalog{
		ID:                     strings.TrimSpace(spec.Action),
		Group:                  strings.TrimSpace(spec.Group),
		Command:                strings.TrimSpace(spec.Command),
		Action:                 strings.TrimSpace(spec.Action),
		Summary:                strings.TrimSpace(spec.Summary),
		Description:            strings.TrimSpace(spec.Description),
		Method:                 strings.TrimSpace(spec.Method),
		Path:                   strings.TrimSpace(spec.Path),
		InputMode:              strings.TrimSpace(spec.InputMode),
		PreferredOutputMode:    strings.TrimSpace(spec.PreferredOutputMode),
		RecommendedGlobalFlags: append([]string(nil), spec.RecommendedGlobalFlags...),
		Params:                 append([]apiCapParam(nil), spec.Params...),
		Notes:                  append([]string(nil), spec.Notes...),
		APIGroup:               strings.TrimSpace(spec.APIGroup),
		APIAction:              strings.TrimSpace(spec.APIAction),
		BackedBy:               backedBy,
		Source:                 "cli_workflow",
	}
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
		"context_schema":           contextSchema,
		"execution_schema":         compactToolExecutionSchema(executionSchema),
		"notes":                    workflowNotes(spec),
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
	if strings.TrimSpace(param.CLIFlag) != "" {
		out["cli_flag"] = strings.TrimSpace(param.CLIFlag)
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
