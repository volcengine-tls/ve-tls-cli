//go:build !agent

package cli

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/util"
)

func runAPI(ctx *Context, args []string) (any, error) {
	ctx.Action = "api.call"
	if len(args) == 0 {
		doc, err := loadAPICapabilities()
		if err != nil {
			return nil, err
		}
		index := buildAPIIndex(doc)
		titles := collectGroupTitles(doc)
		var b strings.Builder
		b.WriteString("Groups:\n")
		b.WriteString(listAPIGroups(index, titles))
		b.WriteString("\nTry:\n")
		b.WriteString("  volclog api <group>\n")
		b.WriteString("  volclog api <group> <action> -h\n")
		return b.String(), nil
	}
	if args[0] == "-h" || args[0] == "--help" {
		return nil, &usageError{Text: usageAPI(), ExitCode: 0}
	}
	switch args[0] {
	case "call":
		return apiCall(ctx, args[1:])
	default:
		return apiGenerated(ctx, args)
	}
}

func apiCall(ctx *Context, args []string) (any, error) {
	if hasHelp(args) {
		return nil, &usageError{Text: usageAPICall(), ExitCode: 0}
	}
	method := "GET"
	path := ""
	query := map[string]string{}
	header := map[string]string{}
	bodyArg := ""
	reqFormat := requestFormatJSON
	for len(args) > 0 {
		switch args[0] {
		case "--method":
			if len(args) < 2 {
				return nil, errors.New("missing --method value")
			}
			method = strings.ToUpper(args[1])
			args = args[2:]
		case "--path":
			if len(args) < 2 {
				return nil, errors.New("missing --path value")
			}
			path = args[1]
			args = args[2:]
		case "--query":
			if len(args) < 2 {
				return nil, errors.New("missing --query value")
			}
			k, v, ok := strings.Cut(args[1], "=")
			if !ok {
				return nil, errors.New("invalid --query, expected k=v")
			}
			query[k] = v
			args = args[2:]
		case "--header":
			if len(args) < 2 {
				return nil, errors.New("missing --header value")
			}
			k, v, ok := strings.Cut(args[1], "=")
			if !ok {
				return nil, errors.New("invalid --header, expected k=v")
			}
			header[k] = v
			args = args[2:]
		case "--body":
			if len(args) < 2 {
				return nil, errors.New("missing --body value")
			}
			bodyArg = args[1]
			args = args[2:]
		case "--request-format":
			if len(args) < 2 {
				return nil, errors.New("missing --request-format value")
			}
			reqFormat = normalizeRequestFormat(requestFormat(args[1]))
			args = args[2:]
		default:
			return nil, errors.New("unknown flag: " + args[0])
		}
	}
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("missing --path")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	body, err := util.ReadMaybeFile(bodyArg)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		body = []byte("{}")
	}
	ctx.apiIOMeta = apiIOMeta{
		Method:        method,
		Path:          path,
		RequestFormat: reqFormat,
		OutputFormat:  ctx.Format,
		OutputMode:    ctx.OutputMode,
	}
	return ctx.Do(method, path, query, header, body)
}

func apiGenerated(ctx *Context, args []string) (any, error) {
	doc, err := loadAPICapabilities()
	if err != nil {
		return nil, err
	}
	index := buildAPIIndex(doc)

	group := strings.ToLower(strings.TrimSpace(args[0]))
	if group == "" {
		return nil, &usageError{Text: usageAPI(), ExitCode: 0}
	}
	actions, ok := index[group]
	if !ok {
		return nil, errors.New("unknown api group: " + args[0])
	}

	if len(args) == 1 {
		return listGroupActions(group, groupTitleFromActions(actions), actions), nil
	}
	if args[1] == "-h" || args[1] == "--help" {
		return nil, &usageError{Text: usageAPIGroup(group, groupTitleFromActions(actions), actions), ExitCode: 0}
	}

	action := normalizeActionToken(args[1])
	if action == "" {
		return listGroupActions(group, groupTitleFromActions(actions), actions), nil
	}
	ops := actions[action]
	if len(ops) == 0 {
		return nil, errors.New("action not found: " + args[1])
	}

	meta, err := parseGeneratedMetaArgs(args[2:])
	if err != nil {
		return nil, err
	}
	if meta.Describe {
		return describeOperationOutput(group, action, ops, generatedRequestTemplates, generatedRequestTemplatesFull)
	}
	if meta.ShouldPrintTemplate {
		return requestTemplateOutput(ops, meta.TemplateMode, generatedRequestTemplates, generatedRequestTemplatesFull), nil
	}
	if hasHelp(meta.RemainingArgs) {
		return nil, &usageError{Text: usageAPIGenerated(group, action, ops), ExitCode: 0}
	}

	method, path, query, header, body, reqFormat, err := parseGeneratedCallArgs(meta.RemainingArgs, ops)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("empty path")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	actionName := strings.TrimSpace(ops[0].Cmd.Action)
	if actionName == "" {
		actionName = strings.TrimSpace(args[1])
	}
	ctx.Action = "api." + group + "." + actionName
	ctx.apiIOMeta = apiIOMeta{
		Group:         group,
		Action:        action,
		Method:        method,
		Path:          path,
		RequestFormat: reqFormat,
		OutputFormat:  ctx.Format,
		OutputMode:    ctx.OutputMode,
	}
	if meta.All {
		selected := ops[0]
		if matched, ok := selectOpByMethod(ops, method); ok {
			selected = matched
		}
		return runGeneratedActionAll(ctx, selected, actionName, path, query, header, body)
	}
	return ctx.Do(method, path, query, header, body)
}

func usageAPIGroup(group string, groupTitle string, actions map[string][]apiActionOp) string {
	var b strings.Builder
	b.WriteString("Usage:\n")
	b.WriteString("  volclog api " + group + "\n")
	b.WriteString("  volclog api " + group + " <action> [flags]\n\n")
	b.WriteString("概览:\n")
	b.WriteString("  先在这个 group 内选择 action，再转到具体 action help 或 --describe。\n")
	if entry := defaultShortcutDescribeForGroup(group); entry != "" {
		b.WriteString("  常见需求先走 shortcut: " + entry + "\n")
	} else {
		b.WriteString("  当前 group 更适合直接走 capabilities -> api --describe。\n")
	}
	if strings.TrimSpace(groupTitle) != "" {
		b.WriteString("  当前 group: " + group + " (" + strings.TrimSpace(groupTitle) + ")\n")
	} else {
		b.WriteString("  当前 group: " + group + "\n")
	}
	if routes := defaultScenarioRoutingForGroup(group); len(routes) > 0 {
		b.WriteString("\n场景速选:\n")
		for _, route := range routes {
			b.WriteString("  - ")
			b.WriteString(route.Intent)
			b.WriteString(": ")
			b.WriteString(route.FirstCommand)
			if alt := strings.TrimSpace(route.InsteadOf); alt != "" {
				b.WriteString("（")
				b.WriteString(alt)
				b.WriteString("）")
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("\n下一步命令:\n")
	b.WriteString("  volclog api " + group + " <action> -h\n")
	b.WriteString("  volclog api " + group + " <action> --describe\n\n")
	b.WriteString(listGroupActions(group, groupTitle, actions))
	return b.String()
}

func usageAPIGenerated(group string, action string, ops []apiActionOp) string {
	if len(ops) == 0 {
		return usageAPI()
	}
	op := ops[0]
	displayAction := strings.TrimSpace(op.Cmd.Action)
	if displayAction == "" {
		displayAction = strings.TrimSpace(action)
	}
	var b strings.Builder
	b.WriteString("Usage:\n")
	b.WriteString("  volclog api " + group + " " + displayAction + " [flags]\n\n")
	b.WriteString("概览:\n")
	if summary := strings.TrimSpace(op.Cmd.Summary); summary != "" && !strings.EqualFold(summary, displayAction) {
		b.WriteString("  " + strings.TrimSpace(op.Cmd.Summary) + "\n")
	}
	if desc := strings.TrimSpace(op.Cmd.Description); desc != "" {
		b.WriteString("  " + desc + "\n")
	}
	b.WriteString("  接口协议: " + strings.TrimSpace(op.Cmd.Method) + " " + strings.TrimSpace(op.Cmd.Path) + "\n\n")
	b.WriteString("优先入口:\n")
	if shortcuts := relatedShortcutDescribesForAPI(group, displayAction); len(shortcuts) > 0 {
		for _, shortcut := range shortcuts {
			b.WriteString("  ")
			b.WriteString(shortcut)
			b.WriteString("\n")
		}
	} else {
		b.WriteString("  当前 action 无快捷命令，直接继续当前 action 的 --describe / --print-request-template。\n")
	}
	b.WriteString("  未命中时回到: volclog capabilities --group " + group + " --view text\n\n")
	docHasStructuredParams := !isPublishedOfficialCommand(op.Cmd) || hasStructuredOfficialParamTable(op.Cmd)
	b.WriteString("调用输入:\n")
	if input := strings.TrimSpace(op.Cmd.InputMode); input != "" {
		b.WriteString("  " + humanizeInputMode(input) + "\n")
	}
	if op.Cmd.BodyRequired && docHasStructuredParams {
		b.WriteString("  请求体: 通过 --request 传入（必填）\n")
	}
	if !docHasStructuredParams {
		b.WriteString("  官网有接口页面，但当前未解析到结构化参数表；此入口仅保留接口发现能力，执行时优先参考官网文档，必要时使用 api call 原始调用。\n")
	}
	b.WriteString("\n关键参数:\n")
	b.WriteString("  --request <json|file://...|->\n")
	b.WriteString("  --request-format <json|jsonl>\n")
	b.WriteString("  --query k=v\n")
	b.WriteString("  --header k=v\n")
	b.WriteString("  --print-request-template[=required|full]\n")
	b.WriteString("  --describe\n")
	if supportsGeneratedActionAll(op) {
		b.WriteString("  --all\n")
	}
	if op.Cmd.BodyRequired && docHasStructuredParams {
		b.WriteString("\n模板建议:\n")
		b.WriteString("  --print-request-template=required: 先拿最小必填骨架，快速确认请求体必填字段\n")
		b.WriteString("  --print-request-template=full: 字段较多、结构不熟或准备落盘编辑时使用\n")
		b.WriteString("  常用字段很少时，可直接用 flags 或现成 req.json，不必先生成 full 模板\n")
	}
	if strings.EqualFold(strings.TrimSpace(group), "log") && strings.EqualFold(strings.TrimSpace(displayAction), "PutLogs") {
		b.WriteString("\n关键约束:\n")
		b.WriteString("  Logs[].Time 必须填写 Unix 毫秒时间戳，例如 1710374400000；不要填秒级 1710374400\n")
		b.WriteString("  如果请求里同时填写 TimeNs，也不要把 Time 降成秒级\n")
	}
	_, flagParamsDoc := splitRequestParamsDocForOutput(op.Cmd.RequestParamsDoc)
	requiredParams, optionalParams := splitParamsByRequired(sortedAPIFlagParams(op.Cmd, op.ParamFlags, flagParamsDoc))
	b.WriteString("\n必填 query/path 参数:\n")
	if len(requiredParams) == 0 {
		b.WriteString("  (none)\n")
	} else {
		writeGeneratedFlagParams(&b, requiredParams)
	}
	b.WriteString("\n可选 query/path 参数:\n")
	if len(optionalParams) == 0 {
		b.WriteString("  (none)\n")
	} else {
		b.WriteString("  " + optionalAPIParamIntro(op, optionalParams) + "\n")
		writeGeneratedFlagParams(&b, optionalParams)
	}
	b.WriteString("\n输出过滤与引号:\n")
	b.WriteString("  --jmes-filter 作用于原始 API 结果，不是 CLI envelope\n")
	b.WriteString("  例如取 Total 写 Total，不写 data.Total\n")
	b.WriteString("  zsh/bash: --jmes-filter \"keys(@)\"\n")
	b.WriteString("\n下一步命令:\n")
	b.WriteString("  volclog api " + group + " " + displayAction + " --describe\n")
	if supportsGeneratedActionAll(op) {
		b.WriteString("  volclog api " + group + " " + displayAction + " --all\n")
	}
	if op.Cmd.BodyRequired && docHasStructuredParams {
		b.WriteString("  volclog api " + group + " " + displayAction + " --print-request-template=full\n")
		b.WriteString("  volclog --dry-run api " + group + " " + displayAction + " --request file://req.json\n")
		if prefersFileOutput(op.Cmd) {
			b.WriteString("  volclog --output-mode file api " + group + " " + displayAction + " --request file://req.json\n")
		} else {
			b.WriteString("  volclog api " + group + " " + displayAction + " --request file://req.json\n")
		}
	}
	return b.String()
}

func sortedAPIFlagParams(cmd apiCapabilityCommand, paramFlags map[string]apiCapParam, doc []apiCapDocParam) []apiCapParam {
	if isPublishedOfficialCommand(cmd) && !hasStructuredOfficialParamTable(cmd) {
		return nil
	}
	keys := make([]string, 0, len(paramFlags))
	for key := range paramFlags {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	docKeys := documentedParamKeys(doc, "query", "path")
	restrictToDoc := len(docKeys) > 0
	out := make([]apiCapParam, 0, len(keys))
	for _, key := range keys {
		param := paramFlags[key]
		loc := strings.ToLower(strings.TrimSpace(param.In))
		if loc != "query" && loc != "path" {
			continue
		}
		if restrictToDoc && !docKeys[paramDocKey(loc, param.Name)] {
			continue
		}
		param.CLIFlag = key
		out = append(out, param)
	}
	return out
}

func writeGeneratedFlagParams(b *strings.Builder, params []apiCapParam) {
	for _, param := range params {
		flag := strings.TrimSpace(param.CLIFlag)
		if flag == "" {
			flag = "--" + toKebab(param.Name)
		}
		desc := strings.TrimSpace(param.Description)
		if desc != "" {
			desc = " - " + desc
		}
		b.WriteString("  " + flag + " <value>" + desc + "\n")
	}
}

func humanizeInputMode(input string) string {
	switch strings.TrimSpace(input) {
	case "body via --request":
		return "body 通过 --request 传入"
	case "optional body via --request":
		return "可选 body 通过 --request 传入"
	case "query/path via flags":
		return "query/path 参数通过 flags 传入"
	case "body via --request; query/path via flags":
		return "body 通过 --request 传入；query/path 参数通过 flags 传入"
	case "optional body via --request; query/path via flags":
		return "可选 body 通过 --request 传入；query/path 参数通过 flags 传入"
	case "no required request body; optional flags only":
		return "无必填请求体；如有需要可补充 flags"
	default:
		return input
	}
}

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

type generatedMetaArgs struct {
	TemplateMode        string
	ShouldPrintTemplate bool
	Describe            bool
	All                 bool
	RemainingArgs       []string
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

type describeScenarioHint struct {
	Intent       string `json:"intent"`
	FirstCommand string `json:"first_command"`
	InsteadOf    string `json:"instead_of,omitempty"`
}

type apiDescribeOutput struct {
	Group                  string              `json:"group"`
	GroupTitle             string              `json:"group_title"`
	Action                 string              `json:"action"`
	Description            string              `json:"description,omitempty"`
	Method                 string              `json:"method"`
	Path                   string              `json:"path"`
	InputMode              string              `json:"input_mode,omitempty"`
	PreferredOutputMode    string              `json:"preferred_output_mode,omitempty"`
	RecommendedGlobalFlags []string            `json:"recommended_global_flags,omitempty"`
	Input                  *describeInput      `json:"input,omitempty"`
	OutputFilterScope      string              `json:"output_filter_scope,omitempty"`
	OutputFilterExamples   []string            `json:"output_filter_examples,omitempty"`
	ShellQuoting           map[string]string   `json:"shell_quoting,omitempty"`
	Guidance               apiDescribeGuidance `json:"guidance"`
}

func parseGeneratedMetaArgs(args []string) (generatedMetaArgs, error) {
	mode := "required"
	rest := make([]string, 0, len(args))
	printTemplate := false
	describe := false
	all := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--print-request-template":
			printTemplate = true
		case strings.HasPrefix(a, "--print-request-template="):
			printTemplate = true
			v := strings.TrimSpace(strings.TrimPrefix(a, "--print-request-template="))
			if v != "" {
				mode = strings.ToLower(v)
			}
		case a == "--describe":
			describe = true
		case a == "--all":
			all = true
		default:
			rest = append(rest, a)
		}
	}
	if mode != "required" && mode != "full" {
		return generatedMetaArgs{}, errors.New("invalid --print-request-template mode: " + mode)
	}
	if printTemplate && describe {
		return generatedMetaArgs{}, errors.New("--describe cannot be used with --print-request-template")
	}
	if all && describe {
		return generatedMetaArgs{}, errors.New("--all cannot be used with --describe")
	}
	if all && printTemplate {
		return generatedMetaArgs{}, errors.New("--all cannot be used with --print-request-template")
	}
	return generatedMetaArgs{
		TemplateMode:        mode,
		ShouldPrintTemplate: printTemplate,
		Describe:            describe,
		All:                 all,
		RemainingArgs:       rest,
	}, nil
}

func optionalAPIParamIntro(op apiActionOp, optionalParams []apiCapParam) string {
	if len(optionalParams) == 0 {
		return ""
	}
	if supportsGeneratedActionAll(op) || looksLikeListAction(op) {
		return "这些都是筛选或翻页项。不带参数就先按默认方式请求；需要缩小范围、分页或列全时再加。"
	}
	if normalizeToken(op.Cmd.Action) == "searchlogs" || normalizeToken(op.Cmd.Action) == "searchhistogram" {
		return "这些都是补充条件。先给够核心查询条件，再按需要补范围、条数、排序或输出相关参数。"
	}
	return "这些都是可选项。用户没明确提到就先别加，按接口默认行为请求。"
}

func looksLikeListAction(op apiActionOp) bool {
	action := normalizeToken(op.Cmd.Action)
	if strings.HasPrefix(action, "describe") && strings.HasSuffix(action, "s") {
		return true
	}
	path := strings.ToLower(strings.TrimSpace(op.Cmd.Path))
	return strings.HasPrefix(path, "/describe") && strings.HasSuffix(path, "s")
}

func describeOperationOutput(group string, action string, ops []apiActionOp, required map[string]string, full map[string]string) (string, error) {
	if len(ops) == 0 {
		return "", errors.New("no matched operation")
	}
	op := ops[0]
	actionName := strings.TrimSpace(op.Cmd.Action)
	if actionName == "" {
		actionName = strings.TrimSpace(action)
	}
	bodyParamsDoc, flagParamsDoc := splitRequestParamsDocForOutput(op.Cmd.RequestParamsDoc)
	params := sanitizeParamsForOutput(op.Cmd, op.Cmd.Params, op.ParamFlags, flagParamsDoc)
	bodyFields := buildRequestBodyFields(op.Cmd, op.Cmd.Params, bodyParamsDoc)
	out := apiDescribeOutput{
		Group:                group,
		GroupTitle:           strings.TrimSpace(op.Cmd.GroupTitle),
		Action:               actionName,
		Description:          strings.TrimSpace(op.Cmd.Description),
		Method:               strings.ToUpper(strings.TrimSpace(op.Cmd.Method)),
		Path:                 strings.TrimSpace(op.Cmd.Path),
		InputMode:            strings.TrimSpace(op.Cmd.InputMode),
		OutputFilterScope:    "JMESPath applies to the raw command/API result before CLI envelope wrapping; for example, filter Total instead of data.Total.",
		OutputFilterExamples: defaultJMESExamplesForGroup(group),
		ShellQuoting: map[string]string{
			"bash":       `--jmes-filter "keys(@)"`,
			"zsh":        `--jmes-filter "keys(@)"`,
			"fish":       `--jmes-filter 'keys(@)'`,
			"powershell": `--jmes-filter 'keys(@)'`,
		},
		Guidance: apiDescribeGuidance{
			ListGroup:         "volclog api " + group,
			Describe:          "volclog api " + group + " " + actionName + " --describe",
			Filter:            `volclog api ` + group + ` ` + actionName + ` --jmes-filter "keys(@)"`,
			FallbackDiscovery: "volclog capabilities --group " + group + " --view text",
		},
	}
	out.Input = &describeInput{
		Flags: buildFlagInput(params, flagParamsDoc, "api"),
	}
	if shortcuts := relatedShortcutDescribesForAPI(group, actionName); len(shortcuts) > 0 {
		out.Guidance.ShortcutFirst = shortcuts
	}
	if prefersFileOutput(op.Cmd) {
		out.PreferredOutputMode = "file"
		out.RecommendedGlobalFlags = []string{"--output-mode file"}
	}
	if body, ok := firstBodyParam(op.Cmd.Params); ok && hasStructuredOfficialParamTable(op.Cmd) {
		req := &apiDescribeRequestBody{Required: body.Required}
		out.Guidance.DryRun = "volclog --dry-run api " + group + " " + actionName + " --request file://req.json"
		if prefersFileOutput(op.Cmd) {
			out.Guidance.Execute = "volclog --output-mode file api " + group + " " + actionName + " --request file://req.json"
		} else {
			out.Guidance.Execute = "volclog api " + group + " " + actionName + " --request file://req.json"
		}
		if out.Input == nil {
			out.Input = &describeInput{}
		}
		out.Input.RequestBody = buildRequestBodyInput(req, "volclog api "+group+" "+actionName+" --print-request-template=required|full", group, actionName, bodyFields)
	}
	if out.Input != nil && out.Input.Flags == nil && out.Input.RequestBody == nil {
		out.Input = nil
	}
	b, err := marshalIndentNoEscape(out)
	if err != nil {
		return "", err
	}
	return string(b), nil
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

func splitParamsByLocation(params []apiCapParam) ([]apiCapParam, []apiCapParam) {
	var body []apiCapParam
	var flags []apiCapParam
	for _, param := range params {
		if strings.EqualFold(strings.TrimSpace(param.In), "body") {
			body = append(body, param)
			continue
		}
		flags = append(flags, param)
	}
	if len(body) == 0 {
		body = nil
	}
	if len(flags) == 0 {
		flags = nil
	}
	return body, flags
}

func buildFlagInput(params []apiCapParam, doc []apiCapDocParam, scope string) *describeFlagInput {
	merged := mergeParamsWithDoc(params, doc)
	fields := describeFieldParams(merged)
	if len(fields) == 0 {
		return nil
	}
	return &describeFlagInput{
		Fields:   fields,
		Guidance: buildParamGuidance(params, scope),
	}
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

func defaultScenarioRoutingForGroup(group string) []describeScenarioHint {
	switch strings.TrimSpace(group) {
	case "project":
		return []describeScenarioHint{
			{Intent: "列项目或拿 ProjectId", FirstCommand: `volclog project list --describe`, InsteadOf: "不要先跑底层 api 或 api call"},
			{Intent: "按项目名过滤", FirstCommand: `volclog project list --project-name <name>`},
			{Intent: "看单个项目详情", FirstCommand: `volclog project get --describe`},
			{Intent: "创建或修改项目", FirstCommand: `volclog project create --describe`},
		}
	case "topic":
		return []describeScenarioHint{
			{Intent: "列主题或拿 TopicId", FirstCommand: `volclog topic list --describe`},
			{Intent: "创建或修改主题", FirstCommand: `volclog topic create --describe`},
			{Intent: "字段较多时组织请求体", FirstCommand: `volclog topic create --print-request-template=full`, InsteadOf: "不要继续堆很多 flags"},
		}
	case "index":
		return []describeScenarioHint{
			{Intent: "看当前索引", FirstCommand: `volclog index get --topic-id <TopicId>`},
			{Intent: "创建或修改索引", FirstCommand: `volclog index create --describe`},
			{Intent: "不确定 body 怎么写", FirstCommand: `volclog index create --print-request-template=full`, InsteadOf: "不要靠记忆写字段名"},
		}
	case "log":
		return []describeScenarioHint{
			{Intent: "普通日志检索", FirstCommand: `volclog log search --describe`},
			{Intent: "看命中日志上下文", FirstCommand: `volclog log context --describe`},
			{Intent: "看时间分布直方图", FirstCommand: `volclog log histogram --describe`},
			{Intent: "写日志或 WebTracking", FirstCommand: `volclog log put --describe`, InsteadOf: "不要继续留在 log search"},
			{Intent: "批量导入文本或 JSON 日志", FirstCommand: `volclog log ingest --describe`, InsteadOf: "不要手工组装每批 PutLogs body"},
			{Intent: "大量原始日志导出", FirstCommand: `volclog --output-mode file log export --describe`},
			{Intent: "SQL/聚合/分析结果导出", FirstCommand: `volclog --output-mode file log export-analysis --describe`},
		}
	case "metric-topic":
		return []describeScenarioHint{
			{Intent: "列指标主题", FirstCommand: `volclog metric-topic list --describe`},
			{Intent: "PromQL/指标查询", FirstCommand: `volclog metric-topic search --describe`},
			{Intent: "资源创建或修改", FirstCommand: `volclog metric-topic create --describe`},
		}
	case "host-group":
		return []describeScenarioHint{
			{Intent: "列机器组或拿 HostGroupId", FirstCommand: `volclog host-group list --describe`},
			{Intent: "看单机器组详情", FirstCommand: `volclog host-group get --describe`},
			{Intent: "创建或修改机器组", FirstCommand: `volclog host-group create --describe`},
			{Intent: "绑定规则、解绑规则或删除组内主机", FirstCommand: `volclog api host-group <action> --describe`, InsteadOf: "不要假设这些操作仍有公开 shortcut"},
		}
	case "collector":
		return []describeScenarioHint{
			{Intent: "列采集规则或拿 RuleId", FirstCommand: `volclog collector list --describe`},
			{Intent: "看单采集规则详情", FirstCommand: `volclog collector get --describe`},
			{Intent: "创建或修改采集规则", FirstCommand: `volclog collector create --describe`},
			{Intent: "绑定或解绑机器组", FirstCommand: `volclog api collector <action> --describe`, InsteadOf: "不要假设这些操作仍有公开 shortcut"},
		}
	case "assistant":
		return []describeScenarioHint{
			{Intent: "实例管理或更底层接口", FirstCommand: `volclog capabilities --group assistant --view text`},
		}
	default:
		return nil
	}
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

func sanitizeParamsForOutput(cmd apiCapabilityCommand, params []apiCapParam, paramFlags map[string]apiCapParam, doc []apiCapDocParam) []apiCapParam {
	if isPublishedOfficialCommand(cmd) && !hasStructuredOfficialParamTable(cmd) {
		return nil
	}
	if len(params) == 0 {
		return nil
	}
	flagByName := map[string]string{}
	docKeys := documentedParamKeys(doc, "query", "path", "header")
	restrictToDoc := len(docKeys) > 0
	for flag, p := range paramFlags {
		key := strings.ToLower(strings.TrimSpace(p.In)) + "\x00" + strings.TrimSpace(p.Name)
		if _, exists := flagByName[key]; exists {
			continue
		}
		flagByName[key] = flag
	}
	out := make([]apiCapParam, 0, len(params))
	for _, p := range params {
		in := strings.ToLower(strings.TrimSpace(p.In))
		if in != "query" && in != "path" && in != "header" {
			continue
		}
		if restrictToDoc && !docKeys[paramDocKey(in, p.Name)] {
			continue
		}
		cp := p
		cp.In = in
		cp.CLIFlag = flagByName[in+"\x00"+strings.TrimSpace(p.Name)]
		out = append(out, cp)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func hasStructuredOfficialParamTable(cmd apiCapabilityCommand) bool {
	return len(sanitizeRequestParamsDocForOutput(cmd.RequestParamsDoc)) > 0
}

func documentedParamKeys(doc []apiCapDocParam, locations ...string) map[string]bool {
	if len(doc) == 0 {
		return nil
	}
	allowed := make(map[string]bool, len(locations))
	for _, loc := range locations {
		allowed[strings.ToLower(strings.TrimSpace(loc))] = true
	}
	out := map[string]bool{}
	for _, item := range doc {
		in := strings.ToLower(strings.TrimSpace(item.In))
		if len(allowed) > 0 && !allowed[in] {
			continue
		}
		name := strings.TrimSpace(item.Name)
		if in == "" || name == "" {
			continue
		}
		out[paramDocKey(in, name)] = true
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func prefersFileOutput(cmd apiCapabilityCommand) bool {
	action := strings.TrimSpace(cmd.Action)
	if action == "" {
		return false
	}
	switch {
	case strings.HasPrefix(action, "Search"),
		strings.HasPrefix(action, "Consume"),
		strings.HasPrefix(action, "Export"),
		strings.HasPrefix(action, "List"):
		return true
	case strings.HasPrefix(action, "Describe") && strings.HasSuffix(action, "s"):
		return true
	default:
		return false
	}
}
