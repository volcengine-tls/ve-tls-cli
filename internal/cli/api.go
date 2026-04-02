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
	return ctx.Do(method, path, query, header, body)
}

func usageAPIGroup(group string, groupTitle string, actions map[string][]apiActionOp) string {
	var b strings.Builder
	b.WriteString("Usage:\n")
	b.WriteString("  volclog api " + group + "\n")
	b.WriteString("  volclog api " + group + " <action> [flags]\n\n")
	b.WriteString("概览:\n")
	b.WriteString("  先在这个 group 内选择 action，再用 --describe / --print-request-template / --dry-run 下钻。\n")
	if strings.TrimSpace(groupTitle) != "" {
		b.WriteString("  当前 group: " + group + " (" + strings.TrimSpace(groupTitle) + ")\n")
	} else {
		b.WriteString("  当前 group: " + group + "\n")
	}
	b.WriteString("\n推荐流程:\n")
	b.WriteString("  1. 查看本组 action 列表\n")
	b.WriteString("  2. 选择目标 action 后运行: volclog api " + group + " <action> --describe\n")
	b.WriteString("  3. 如有 body，再运行: volclog api " + group + " <action> --print-request-template=full\n")
	b.WriteString("  4. 执行前先使用: volclog --dry-run api " + group + " <action> ...\n\n")
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
	b.WriteString("\n推荐流程:\n")
	b.WriteString("  1) 先用 --describe 查看机器可读约束\n")
	b.WriteString("  2) 如有 body，用 --print-request-template=full 生成模板\n")
	b.WriteString("  3) query/path 用 flags，body 用 --request\n")
	b.WriteString("  4) 执行前先使用 --dry-run\n")
	b.WriteString("  5) 大结果优先使用 --output-mode file\n")
	b.WriteString("\n下一步命令:\n")
	b.WriteString("  volclog api " + group + " " + displayAction + " --describe\n")
	if op.Cmd.BodyRequired {
		b.WriteString("  volclog api " + group + " " + displayAction + " --print-request-template=full\n")
		b.WriteString("  volclog --dry-run api " + group + " " + displayAction + " --request file://req.json\n")
		b.WriteString("  volclog api " + group + " " + displayAction + " --request file://req.json\n")
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
	case "#/definitions/code_byted_org_storage_tls-lib_proto_pb.LogGroupList":
		if strings.EqualFold(strings.TrimSpace(mode), "required") {
			return `{
  "LogGroups": [
    {
      "Source": "",
      "FileName": "",
      "Logs": [
        {
          "Time": 0,
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
          "Time": 0,
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
	RemainingArgs       []string
}

type apiDescribeGuidance struct {
	ListGroup string `json:"list_group"`
	Template  string `json:"template,omitempty"`
	Describe  string `json:"describe"`
	DryRun    string `json:"dry_run,omitempty"`
	Execute   string `json:"execute,omitempty"`
}

type apiDescribeRequestBody struct {
	Required         bool `json:"required"`
	TemplateRequired any  `json:"template_required,omitempty"`
	TemplateFull     any  `json:"template_full,omitempty"`
}

type apiDescribeOutput struct {
	Group            string                  `json:"group"`
	GroupTitle       string                  `json:"group_title"`
	Action           string                  `json:"action"`
	Description      string                  `json:"description,omitempty"`
	Method           string                  `json:"method"`
	Path             string                  `json:"path"`
	InputMode        string                  `json:"input_mode,omitempty"`
	RequiredFlags    []string                `json:"required_flags,omitempty"`
	Params           []apiCapParam           `json:"params,omitempty"`
	RequestBody      *apiDescribeRequestBody `json:"request_body,omitempty"`
	RequestParamsDoc []apiCapDocParam        `json:"request_params_doc,omitempty"`
	Guidance         apiDescribeGuidance     `json:"guidance"`
}

func parseGeneratedMetaArgs(args []string) (generatedMetaArgs, error) {
	mode := "required"
	rest := make([]string, 0, len(args))
	printTemplate := false
	describe := false
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
	return generatedMetaArgs{
		TemplateMode:        mode,
		ShouldPrintTemplate: printTemplate,
		Describe:            describe,
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
		Group:            group,
		GroupTitle:       strings.TrimSpace(op.Cmd.GroupTitle),
		Action:           actionName,
		Description:      strings.TrimSpace(op.Cmd.Description),
		Method:           strings.ToUpper(strings.TrimSpace(op.Cmd.Method)),
		Path:             strings.TrimSpace(op.Cmd.Path),
		InputMode:        strings.TrimSpace(op.Cmd.InputMode),
		RequiredFlags:    append([]string(nil), op.Cmd.RequiredFlags...),
		Params:           sanitizeParamsForOutput(op.Cmd.Params, op.ParamFlags),
		RequestParamsDoc: sanitizeRequestParamsDocForOutput(op.Cmd.RequestParamsDoc),
		Guidance: apiDescribeGuidance{
			ListGroup: "volclog api " + group,
			Describe:  "volclog api " + group + " " + actionName + " --describe",
		},
	}
	if body, ok := firstBodyParam(op.Cmd.Params); ok {
		req := &apiDescribeRequestBody{Required: body.Required}
		out.Guidance.Template = "volclog api " + group + " " + actionName + " --print-request-template=full"
		out.Guidance.DryRun = "volclog --dry-run api " + group + " " + actionName + " --request file://req.json"
		out.Guidance.Execute = "volclog api " + group + " " + actionName + " --request file://req.json"
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
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b) + "\n", nil
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
