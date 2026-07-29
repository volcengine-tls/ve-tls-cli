//go:build human

package cli

import (
	"encoding/json"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/util"
)

func firstBodyParam(params []apiCapParam) (apiCapParam, bool) {
	for _, p := range params {
		if strings.ToLower(strings.TrimSpace(p.In)) == "body" {
			return p, true
		}
	}
	return apiCapParam{}, false
}

func requestTemplateOutput(ops []apiActionOp, mode string, required map[string]string, full map[string]string) string {
	if len(ops) == 0 {
		return ""
	}
	var template string
	if bodyParam, ok := firstBodyParam(ops[0].Cmd.Params); ok {
		ref := strings.TrimSpace(bodyParam.Ref)
		if tpl, ok := specialRequestTemplate(ref, mode); ok {
			return tpl
		}
		if strings.EqualFold(strings.TrimSpace(mode), "full") {
			if tpl, ok := full[ref]; ok {
				template = tpl
			}
		}
		if strings.TrimSpace(template) == "" {
			if tpl, ok := required[ref]; ok {
				template = tpl
			}
		}
		if isMeaningfulTemplateJSON(template) {
			return template
		}
	}
	fallback := buildTemplateFromDocParams(ops[0].Cmd.RequestParamsDoc, mode)
	if isMeaningfulTemplateJSON(fallback) {
		return fallback
	}
	if strings.TrimSpace(template) != "" {
		return template
	}
	return ""
}

func specialRequestTemplate(ref string, mode string) (string, bool) {
	switch strings.TrimSpace(ref) {
	case "#/definitions/index.CreateReq", "#/definitions/index.ModifyReq":
		if strings.EqualFold(strings.TrimSpace(mode), "required") {
			return `{
  "FullText": {
    "CaseSensitive": false,
    "Delimiter": " \t\n",
    "IncludeChinese": true
  },
  "KeyValue": [
    {
      "Key": "",
      "Value": {
        "ValueType": "text",
        "CaseSensitive": false,
        "SqlFlag": true
      }
    }
  ]
}`, true
		}
		return `{
  "EnableAutoIndex": false,
  "EnablePhraseIndex": false,
  "FullText": {
    "CaseSensitive": false,
    "Delimiter": " \t\n",
    "IncludeChinese": true
  },
  "KeyValue": [
    {
      "Key": "",
      "Value": {
        "ValueType": "text",
        "CaseSensitive": false,
        "SqlFlag": true
      }
    }
  ],
  "LogReduce": false,
  "LogReduceBlackList": [
    ""
  ],
  "LogReduceWhiteList": [
    ""
  ],
  "MaxTextLen": 0,
  "TopicId": "",
  "UserInnerKeyValue": [
    {}
  ]
}`, true
	case "#/definitions/code_byted_org_storage_tls-lib_proto_pb.LogGroupList":
		if strings.EqualFold(strings.TrimSpace(mode), "required") {
			return `{
  "LogGroups": [
    {
      "Source": "",
      "FileName": "",
      "Logs": [
        {
          "Time": 1710374400000,
          "Contents": [
            {
              "Key": "",
              "Value": ""
            }
          ]
        }
      ]
    }
  ]
}`, true
		}
		return `{
  "LogGroups": [
    {
      "Source": "",
      "FileName": "",
      "ContextFlow": "",
      "LogTags": [
        {
          "Key": "",
          "Value": ""
        }
      ],
      "Logs": [
        {
          "Time": 1710374400000,
          "Contents": [
            {
              "Key": "",
              "Value": ""
            }
          ]
        }
      ]
    }
  ]
}`, true
	default:
		return "", false
	}
}

type apiDescribeRequestBody struct {
	Required bool `json:"required,omitempty"`
}

type paramGuidance struct {
	Required string `json:"required,omitempty"`
	Optional string `json:"optional,omitempty"`
}

type describeFieldParam struct {
	Name        string   `json:"name"`
	CLIFlag     string   `json:"cli_flag,omitempty"`
	In          string   `json:"in"`
	Required    bool     `json:"required"`
	Type        string   `json:"type,omitempty"`
	Format      string   `json:"format,omitempty"`
	Ref         string   `json:"ref,omitempty"`
	Description string   `json:"description,omitempty"`
	Example     string   `json:"example,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Pattern     string   `json:"pattern,omitempty"`
	Minimum     *float64 `json:"minimum,omitempty"`
	Maximum     *float64 `json:"maximum,omitempty"`
	MinLength   *int     `json:"min_length,omitempty"`
	MaxLength   *int     `json:"max_length,omitempty"`
}

type describeFlagInput struct {
	Fields   []describeFieldParam `json:"fields,omitempty"`
	Guidance *paramGuidance       `json:"guidance,omitempty"`
}

type describeRequestBodyInput struct {
	Required      bool                 `json:"required,omitempty"`
	Fields        []describeFieldParam `json:"fields,omitempty"`
	PrintTemplate string               `json:"print_template,omitempty"`
	Note          string               `json:"note,omitempty"`
}

type describeInput struct {
	Flags       *describeFlagInput        `json:"flags,omitempty"`
	RequestBody *describeRequestBodyInput `json:"request_body,omitempty"`
}

func defaultJMESExamplesForGroup(group string) []string {
	switch strings.TrimSpace(group) {
	case "project":
		return []string{
			"Total",
			"Projects[0].ProjectId",
			"Projects[].{ProjectId: ProjectId, ProjectName: ProjectName}",
		}
	case "topic", "metric-topic":
		return []string{
			"Total",
			"Topics[0].TopicId",
			"Topics[].{TopicId: TopicId, TopicName: TopicName}",
		}
	case "host-group":
		return []string{
			"Total",
			"HostGroupHostsRulesInfos[].HostGroupInfo.HostGroupId",
			"HostGroupHostsRulesInfos[].HostGroupInfo.{HostGroupId: HostGroupId, HostGroupName: HostGroupName}",
		}
	case "collector":
		return []string{
			"Total",
			"Rules[0].RuleId",
			"Rules[].{RuleId: RuleId, RuleName: RuleName, TopicId: TopicId}",
		}
	default:
		return []string{
			"Total",
			"keys(@)",
		}
	}
}

func splitParamsByRequired(params []apiCapParam) ([]apiCapParam, []apiCapParam) {
	var required []apiCapParam
	var optional []apiCapParam
	for _, param := range params {
		if param.Required {
			required = append(required, param)
			continue
		}
		optional = append(optional, param)
	}
	if len(required) == 0 {
		required = nil
	}
	if len(optional) == 0 {
		optional = nil
	}
	return required, optional
}

func buildParamGuidance(params []apiCapParam, scope string) *paramGuidance {
	required, optional := splitParamsByRequired(params)
	if len(required) == 0 && len(optional) == 0 {
		return nil
	}
	out := &paramGuidance{}
	if len(required) > 0 {
		out.Required = "只把 required=true 的参数当成必填；缺少这些参数时不要猜值，先补齐条件或回到对应 shortcut/--describe。"
	}
	if len(optional) > 0 {
		target := "接口"
		if strings.TrimSpace(scope) == "shortcut" {
			target = "当前快捷命令"
		}
		out.Optional = "只在用户明确给出过滤、分页、排序、范围或额外约束时，再填写 optional；不填表示按" + target + "默认行为执行，不要从示例或历史请求里补齐。"
	}
	return out
}

func buildRequestBodyInput(req *apiDescribeRequestBody, printTemplate string, group string, action string, fields []describeFieldParam) *describeRequestBodyInput {
	var required bool
	if req != nil {
		required = req.Required
	}
	if !required && strings.TrimSpace(printTemplate) == "" && len(fields) == 0 {
		return nil
	}
	note := "请求体通过 --request file://req.json 传入。先用 required 看最小骨架；字段不确定、嵌套较多或准备落盘编辑时再切到 full。"
	if strings.EqualFold(strings.TrimSpace(group), "log") && strings.EqualFold(strings.TrimSpace(action), "PutLogs") {
		note = "请求体通过 --request file://req.json 传入。先用 required 看最小骨架；需要完整 Logs 结构时再切到 full。Logs[].Time 必须是 Unix 毫秒时间戳，例如 1710374400000，不要填秒级 1710374400。"
	}
	if strings.TrimSpace(printTemplate) == "" {
		note = "请求体通过 --request file://req.json 传入。当前命令未提供模板打印入口，必要时回退到底层 api --describe。"
	}
	return &describeRequestBodyInput{
		Required:      required,
		Fields:        fields,
		PrintTemplate: strings.TrimSpace(printTemplate),
		Note:          note,
	}
}

func describeFieldParams(params []apiCapParam) []describeFieldParam {
	if len(params) == 0 {
		return nil
	}
	out := make([]describeFieldParam, 0, len(params))
	for _, param := range params {
		out = append(out, describeFieldParam{
			Name:        param.Name,
			CLIFlag:     param.CLIFlag,
			In:          param.In,
			Required:    param.Required,
			Type:        param.Type,
			Format:      param.Format,
			Ref:         param.Ref,
			Description: conciseFieldDescription(param.Description),
			Example:     param.Example,
			Enum:        param.Enum,
			Pattern:     param.Pattern,
			Minimum:     param.Minimum,
			Maximum:     param.Maximum,
			MinLength:   param.MinLength,
			MaxLength:   param.MaxLength,
		})
	}
	return out
}

func hasMeaningfulTemplate(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case map[string]any:
		return len(t) > 0
	case []any:
		return len(t) > 0
	case string:
		return strings.TrimSpace(t) != ""
	default:
		return true
	}
}

func isMeaningfulTemplateJSON(tpl string) bool {
	s := strings.TrimSpace(tpl)
	if s == "" {
		return false
	}
	v, err := util.UnmarshalJSON([]byte(s))
	if err != nil {
		return false
	}
	return hasMeaningfulTemplate(v)
}

func buildTemplateFromDocParams(params []apiCapDocParam, mode string) string {
	if len(params) == 0 {
		return ""
	}
	out := map[string]any{}
	for _, p := range params {
		if !strings.EqualFold(strings.TrimSpace(p.In), "body") {
			continue
		}
		name := strings.TrimSpace(p.Name)
		if name == "" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(mode), "required") && !isDocRequired(p.RequiredText) {
			continue
		}
		if _, exists := out[name]; exists {
			continue
		}
		out[name] = defaultTemplateValueByType(p.Type)
	}
	if len(out) == 0 {
		return ""
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return ""
	}
	return string(b)
}

func defaultTemplateValueByType(typ string) any {
	t := strings.ToLower(strings.TrimSpace(typ))
	switch {
	case strings.Contains(t, "array"):
		return []any{}
	case strings.Contains(t, "bool"):
		return false
	case strings.Contains(t, "int"), strings.Contains(t, "number"), strings.Contains(t, "float"), strings.Contains(t, "double"), strings.Contains(t, "long"):
		return 0
	case strings.Contains(t, "object"), strings.Contains(t, "map"):
		return map[string]any{}
	default:
		return ""
	}
}

func buildRequestBodyFields(cmd apiCapabilityCommand, params []apiCapParam, doc []apiCapDocParam) []describeFieldParam {
	fields := mergeBodyParamsWithDoc(cmd, params, doc)
	if len(fields) == 0 {
		return nil
	}
	return describeFieldParams(fields)
}

func mergeBodyParamsWithDoc(cmd apiCapabilityCommand, params []apiCapParam, doc []apiCapDocParam) []apiCapParam {
	if len(doc) > 0 {
		paramByKey := make(map[string]apiCapParam, len(params))
		for _, param := range params {
			if !strings.EqualFold(strings.TrimSpace(param.In), "body") {
				continue
			}
			if isGenericBodyParam(param) {
				continue
			}
			paramByKey[paramDocKey("body", param.Name)] = param
		}
		out := make([]apiCapParam, 0, len(doc))
		for _, item := range doc {
			if !strings.EqualFold(strings.TrimSpace(item.In), "body") {
				continue
			}
			name := strings.TrimSpace(item.Name)
			if name == "" {
				continue
			}
			cp, ok := paramByKey[paramDocKey("body", name)]
			if !ok {
				cp = apiCapParam{Name: name, In: "body"}
			}
			cp.In = "body"
			if s := strings.TrimSpace(item.Type); s != "" && strings.TrimSpace(cp.Type) == "" {
				cp.Type = s
			}
			if s := strings.TrimSpace(item.RequiredText); s != "" {
				cp.RequiredText = s
				if requiredFromText(s) {
					cp.Required = true
				}
			}
			if s := strings.TrimSpace(item.Description); s != "" {
				cp.Description = s
			}
			if s := strings.TrimSpace(item.Example); s != "" {
				cp.Example = s
			}
			out = append(out, cp)
		}
		if len(out) > 0 {
			return out
		}
	}
	if isPublishedOfficialCommand(cmd) && !hasStructuredOfficialParamTable(cmd) {
		return nil
	}
	out := make([]apiCapParam, 0, len(params))
	for _, param := range params {
		if !strings.EqualFold(strings.TrimSpace(param.In), "body") {
			continue
		}
		if isGenericBodyParam(param) {
			continue
		}
		cp := param
		cp.In = "body"
		out = append(out, cp)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func isGenericBodyParam(param apiCapParam) bool {
	if !strings.EqualFold(strings.TrimSpace(param.In), "body") {
		return false
	}
	name := strings.ToLower(strings.TrimSpace(param.Name))
	return name == "data" || name == "body" || name == "request"
}

func hasStructuredOfficialParamTable(cmd apiCapabilityCommand) bool {
	return len(sanitizeRequestParamsDocForOutput(cmd.RequestParamsDoc)) > 0
}
