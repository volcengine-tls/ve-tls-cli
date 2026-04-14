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
	b.WriteString("调用输入:\n")
	if input := strings.TrimSpace(op.Cmd.InputMode); input != "" {
		b.WriteString("  " + humanizeInputMode(input) + "\n")
	}
	if len(op.Cmd.RequiredFlags) > 0 {
		b.WriteString("  必填 flags: " + strings.Join(op.Cmd.RequiredFlags, ", ") + "\n")
	}
	if op.Cmd.BodyRequired {
		b.WriteString("  请求体: 通过 --request 传入（必填）\n")
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
	if op.Cmd.BodyRequired {
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
	b.WriteString("\n过滤与引号:\n")
	b.WriteString("  --jmes-filter 作用于原始 API 结果，不是 CLI envelope\n")
	b.WriteString("  例如取 Total 写 Total，不写 data.Total\n")
	b.WriteString("  zsh/bash: --jmes-filter \"keys(@)\"\n")
	keys := make([]string, 0, len(op.ParamFlags))
	for k := range op.ParamFlags {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, f := range keys {
		p := op.ParamFlags[f]
		loc := strings.ToLower(strings.TrimSpace(p.In))
		if loc != "query" && loc != "path" {
			continue
		}
		req := ""
		if p.Required {
			req = " (required)"
		}
		desc := strings.TrimSpace(p.Description)
		if desc != "" {
			desc = " - " + desc
		}
		b.WriteString("  " + f + " <value>" + req + desc + "\n")
	}
	b.WriteString("\n下一步命令:\n")
	b.WriteString("  volclog api " + group + " " + displayAction + " --describe\n")
	if supportsGeneratedActionAll(op) {
		b.WriteString("  volclog api " + group + " " + displayAction + " --all\n")
	}
	if op.Cmd.BodyRequired {
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

type apiDescribeGuidance struct {
	ListGroup           string   `json:"list_group"`
	Template            string   `json:"template,omitempty"`
	Describe            string   `json:"describe"`
	DryRun              string   `json:"dry_run,omitempty"`
	Execute             string   `json:"execute,omitempty"`
	Filter              string   `json:"filter,omitempty"`
	ShortcutFirst       []string `json:"shortcut_first,omitempty"`
	FallbackDiscovery   string   `json:"fallback_discovery,omitempty"`
	FallbackAPIDescribe string   `json:"fallback_api_describe,omitempty"`
}

type apiDescribeRequestBody struct {
	Required         bool `json:"required"`
	TemplateRequired any  `json:"template_required,omitempty"`
	TemplateFull     any  `json:"template_full,omitempty"`
}

type templateGuidance struct {
	UseRequiredWhen string `json:"use_required_when,omitempty"`
	UseFullWhen     string `json:"use_full_when,omitempty"`
	SkipWhen        string `json:"skip_when,omitempty"`
	AfterGenerate   string `json:"after_generate,omitempty"`
}

type describeScenarioHint struct {
	Intent       string `json:"intent"`
	FirstCommand string `json:"first_command"`
	InsteadOf    string `json:"instead_of,omitempty"`
}

type apiDescribeOutput struct {
	Group                  string                  `json:"group"`
	GroupTitle             string                  `json:"group_title"`
	Action                 string                  `json:"action"`
	Description            string                  `json:"description,omitempty"`
	Method                 string                  `json:"method"`
	Path                   string                  `json:"path"`
	InputMode              string                  `json:"input_mode,omitempty"`
	PreferredOutputMode    string                  `json:"preferred_output_mode,omitempty"`
	RecommendedGlobalFlags []string                `json:"recommended_global_flags,omitempty"`
	RequiredFlags          []string                `json:"required_flags,omitempty"`
	Params                 []apiCapParam           `json:"params,omitempty"`
	RequestBody            *apiDescribeRequestBody `json:"request_body,omitempty"`
	TemplateGuidance       *templateGuidance       `json:"template_guidance,omitempty"`
	RequestParamsDoc       []apiCapDocParam        `json:"request_params_doc,omitempty"`
	OutputFilterScope      string                  `json:"output_filter_scope,omitempty"`
	OutputFilterExamples   []string                `json:"output_filter_examples,omitempty"`
	ShellQuoting           map[string]string       `json:"shell_quoting,omitempty"`
	ScenarioRouting        []describeScenarioHint  `json:"scenario_routing,omitempty"`
	Guidance               apiDescribeGuidance     `json:"guidance"`
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

func describeOperationOutput(group string, action string, ops []apiActionOp, required map[string]string, full map[string]string) (string, error) {
	if len(ops) == 0 {
		return "", errors.New("no matched operation")
	}
	op := ops[0]
	actionName := strings.TrimSpace(op.Cmd.Action)
	if actionName == "" {
		actionName = strings.TrimSpace(action)
	}
	out := apiDescribeOutput{
		Group:                group,
		GroupTitle:           strings.TrimSpace(op.Cmd.GroupTitle),
		Action:               actionName,
		Description:          strings.TrimSpace(op.Cmd.Description),
		Method:               strings.ToUpper(strings.TrimSpace(op.Cmd.Method)),
		Path:                 strings.TrimSpace(op.Cmd.Path),
		InputMode:            strings.TrimSpace(op.Cmd.InputMode),
		RequiredFlags:        append([]string(nil), op.Cmd.RequiredFlags...),
		Params:               sanitizeParamsForOutput(op.Cmd.Params, op.ParamFlags),
		RequestParamsDoc:     sanitizeRequestParamsDocForOutput(op.Cmd.RequestParamsDoc),
		OutputFilterScope:    "JMESPath applies to the raw command/API result before CLI envelope wrapping; for example, filter Total instead of data.Total.",
		OutputFilterExamples: defaultJMESExamplesForGroup(group),
		ShellQuoting: map[string]string{
			"bash":       `--jmes-filter "keys(@)"`,
			"zsh":        `--jmes-filter "keys(@)"`,
			"fish":       `--jmes-filter 'keys(@)'`,
			"powershell": `--jmes-filter 'keys(@)'`,
		},
		ScenarioRouting: defaultScenarioRoutingForGroup(group),
		Guidance: apiDescribeGuidance{
			ListGroup:         "volclog api " + group,
			Describe:          "volclog api " + group + " " + actionName + " --describe",
			Filter:            `volclog api ` + group + ` ` + actionName + ` --jmes-filter "keys(@)"`,
			FallbackDiscovery: "volclog capabilities --group " + group + " --view text",
		},
	}
	if shortcuts := relatedShortcutDescribesForAPI(group, actionName); len(shortcuts) > 0 {
		out.Guidance.ShortcutFirst = shortcuts
	}
	if prefersFileOutput(op.Cmd) {
		out.PreferredOutputMode = "file"
		out.RecommendedGlobalFlags = []string{"--output-mode file"}
	}
	if body, ok := firstBodyParam(op.Cmd.Params); ok {
		req := &apiDescribeRequestBody{Required: body.Required}
		out.Guidance.Template = "volclog api " + group + " " + actionName + " --print-request-template=full"
		out.Guidance.DryRun = "volclog --dry-run api " + group + " " + actionName + " --request file://req.json"
		if prefersFileOutput(op.Cmd) {
			out.Guidance.Execute = "volclog --output-mode file api " + group + " " + actionName + " --request file://req.json"
		} else {
			out.Guidance.Execute = "volclog api " + group + " " + actionName + " --request file://req.json"
		}
		if tpl := strings.TrimSpace(requestTemplateOutput(ops, "required", required, full)); tpl != "" {
			if v, err := util.UnmarshalJSON([]byte(tpl)); err == nil && hasMeaningfulTemplate(v) {
				req.TemplateRequired = v
			}
		}
		if tpl := strings.TrimSpace(requestTemplateOutput(ops, "full", required, full)); tpl != "" {
			if v, err := util.UnmarshalJSON([]byte(tpl)); err == nil && hasMeaningfulTemplate(v) {
				req.TemplateFull = v
			}
		}
		out.RequestBody = req
		out.TemplateGuidance = buildTemplateGuidance(group, actionName, out.InputMode, out.PreferredOutputMode)
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

func buildTemplateGuidance(group string, action string, inputMode string, preferredOutputMode string) *templateGuidance {
	execute := "volclog api " + group + " " + action + " --request file://req.json"
	if strings.TrimSpace(preferredOutputMode) == "file" {
		execute = "volclog --output-mode file api " + group + " " + action + " --request file://req.json"
	}
	if strings.EqualFold(strings.TrimSpace(group), "log") && strings.EqualFold(strings.TrimSpace(action), "PutLogs") {
		return &templateGuidance{
			UseRequiredWhen: "先确认最小必填写日志请求体时使用 required；Logs[].Time 必须填写 Unix 毫秒时间戳，例如 1710374400000，不要填秒级 1710374400。",
			UseFullWhen:     "需要同时填写 ContextFlow、LogTags 等完整结构时使用 full；其中 Logs[].Time 仍必须是 Unix 毫秒时间戳。",
			SkipWhen:        "如果你已经有现成的 PutLogs 请求体，可直接执行；但提交前仍要确认 Logs[].Time 是毫秒，不是秒级。",
			AfterGenerate:   "生成模板后先把 Logs[].Time 改成 Unix 毫秒时间戳（例如 1710374400000），再执行 " + execute + "。",
		}
	}
	skip := "已知常用字段且字段不多时，可直接按 flags 或 --request 执行，不必先看 full 模板。"
	if !strings.Contains(strings.ToLower(strings.TrimSpace(inputMode)), "flags") {
		skip = "如果你已经明确请求体结构，可直接准备 req.json 并执行，不必重复生成模板。"
	}
	return &templateGuidance{
		UseRequiredWhen: "先确认最小必填请求体，或只想快速起一个可执行骨架时使用 required。",
		UseFullWhen:     "字段较多、嵌套结构不熟、或准备复制完整 JSON 落盘编辑时使用 full。",
		SkipWhen:        skip,
		AfterGenerate:   "生成模板后补齐 req.json，再执行 " + execute + "。",
	}
}

func defaultScenarioRoutingForGroup(group string) []describeScenarioHint {
	switch strings.TrimSpace(group) {
	case "project":
		return []describeScenarioHint{
			{Intent: "列项目或拿 ProjectId", FirstCommand: `volclog project list --describe`, InsteadOf: "不要先跑底层 api 或 api call"},
			{Intent: "模糊找项目", FirstCommand: `volclog project list --fuzzy-search-key <keyword>`},
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
			{Intent: "绑定或解绑规则", FirstCommand: `volclog host-group bind-rules --describe`},
			{Intent: "从机器组删除主机", FirstCommand: `volclog host-group delete-host --describe`},
			{Intent: "创建或修改机器组", FirstCommand: `volclog host-group create --describe`},
		}
	case "collector":
		return []describeScenarioHint{
			{Intent: "列采集规则或拿 RuleId", FirstCommand: `volclog collector list --describe`},
			{Intent: "看单采集规则详情", FirstCommand: `volclog collector get --describe`},
			{Intent: "绑定或解绑机器组", FirstCommand: `volclog collector bind-host-groups --describe`},
			{Intent: "创建或修改采集规则", FirstCommand: `volclog collector create --describe`},
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

func isDocRequired(v string) bool {
	s := strings.ToLower(strings.TrimSpace(v))
	return s == "是" || s == "true" || s == "required" || s == "yes"
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

func sanitizeRequestParamsDocForOutput(params []apiCapDocParam) []apiCapDocParam {
	if len(params) == 0 {
		return nil
	}
	out := make([]apiCapDocParam, 0, len(params))
	for _, p := range params {
		in := strings.ToLower(strings.TrimSpace(p.In))
		if in != "body" && in != "query" && in != "path" && in != "header" {
			continue
		}
		cp := p
		cp.In = in
		out = append(out, cp)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sanitizeParamsForOutput(params []apiCapParam, paramFlags map[string]apiCapParam) []apiCapParam {
	if len(params) == 0 {
		return nil
	}
	flagByName := map[string]string{}
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
