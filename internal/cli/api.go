package cli

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"volclog/internal/util"
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
		return nil, &usageError{Text: usageAPI(), ExitCode: 0}
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

func usageAPIGenerated(group string, action string, ops []apiActionOp) string {
	if len(ops) == 0 {
		return usageAPI()
	}
	op := ops[0]
	var b strings.Builder
	b.WriteString("Usage:\n")
	b.WriteString("  volclog api " + group + " " + action + " [flags]\n\n")
	b.WriteString("Operation:\n")
	if strings.TrimSpace(op.Cmd.Summary) != "" {
		b.WriteString("  " + strings.TrimSpace(op.Cmd.Summary) + "\n")
	}
	b.WriteString("  " + strings.TrimSpace(op.Cmd.Method) + " " + strings.TrimSpace(op.Cmd.Path) + "\n\n")
	b.WriteString("Common Flags:\n")
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
	b.WriteString("\nAgent Guidance:\n")
	b.WriteString("  1) Use --describe for machine-readable constraints\n")
	b.WriteString("  2) Use --print-request-template=full for full body template\n")
	b.WriteString("  3) Use --dry-run before execution\n")
	return b.String()
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
	Discover  string `json:"discover"`
	Template  string `json:"template"`
	DryRun    string `json:"dry_run"`
	Execute   string `json:"execute"`
	Note      string `json:"note"`
	InputMode string `json:"inputMode"`
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
	Summary          string                  `json:"summary"`
	Method           string                  `json:"method"`
	Path             string                  `json:"path"`
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
		Summary:          strings.TrimSpace(op.Cmd.Summary),
		Method:           strings.ToUpper(strings.TrimSpace(op.Cmd.Method)),
		Path:             strings.TrimSpace(op.Cmd.Path),
		Params:           sanitizeParamsForOutput(op.Cmd.Params),
		RequestParamsDoc: sanitizeRequestParamsDocForOutput(op.Cmd.RequestParamsDoc),
		Guidance: apiDescribeGuidance{
			Discover:  "volclog capabilities --group " + group + " --action " + actionName,
			Template:  "volclog api " + group + " " + actionName + " --print-request-template=full",
			DryRun:    "volclog --dry-run api " + group + " " + actionName + " --request file://req.json",
			Execute:   "volclog api " + group + " " + actionName + " --request file://req.json",
			Note:      "Prefer --describe for machine-readable constraints.",
			InputMode: "body via --request, query/path via flags",
		},
	}
	if body, ok := firstBodyParam(op.Cmd.Params); ok {
		req := &apiDescribeRequestBody{Required: body.Required}
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

func sanitizeParamsForOutput(params []apiCapParam) []apiCapParam {
	if len(params) == 0 {
		return nil
	}
	out := make([]apiCapParam, 0, len(params))
	for _, p := range params {
		in := strings.ToLower(strings.TrimSpace(p.In))
		if in != "body" && in != "query" && in != "path" && in != "header" {
			continue
		}
		cp := p
		cp.In = in
		if in == "body" {
			cp.Ref = ""
		}
		out = append(out, cp)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
