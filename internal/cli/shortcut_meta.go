package cli

import (
	"errors"
	"sort"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/util"
)

type shortcutMetaArgs struct {
	TemplateMode        string
	ShouldPrintTemplate bool
	Describe            bool
	RemainingArgs       []string
}

type shortcutCommandSpec struct {
	Group                  string
	Command                string
	Action                 string
	HiddenInHelp           bool
	Summary                string
	Description            string
	Method                 string
	Path                   string
	InputMode              string
	PreferredOutputMode    string
	RecommendedGlobalFlags []string
	RequiredFlags          []string
	Params                 []apiCapParam
	APIGroup               string
	APIAction              string
	TemplateOmit           []string
	SupportsTemplate       bool
	Notes                  []string
}

type shortcutDescribeOutput struct {
	Group                  string              `json:"group"`
	Command                string              `json:"command"`
	Action                 string              `json:"action"`
	APIGroup               string              `json:"api_group,omitempty"`
	APIAction              string              `json:"api_action,omitempty"`
	Summary                string              `json:"summary,omitempty"`
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
	Notes                  []string            `json:"notes,omitempty"`
	Guidance               apiDescribeGuidance `json:"guidance"`
}

func parseShortcutMetaArgs(args []string) (shortcutMetaArgs, error) {
	mode := "required"
	rest := make([]string, 0, len(args))
	printTemplate := false
	describe := false
	for _, a := range args {
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
		return shortcutMetaArgs{}, errors.New("invalid --print-request-template mode: " + mode)
	}
	if printTemplate && describe {
		return shortcutMetaArgs{}, errors.New("--describe cannot be used with --print-request-template")
	}
	return shortcutMetaArgs{
		TemplateMode:        mode,
		ShouldPrintTemplate: printTemplate,
		Describe:            describe,
		RemainingArgs:       rest,
	}, nil
}

func maybeHandleShortcutMeta(group, command string, args []string) (any, bool, error) {
	meta, err := parseShortcutMetaArgs(args)
	if err != nil {
		return nil, true, err
	}
	if !meta.Describe && !meta.ShouldPrintTemplate {
		return nil, false, nil
	}
	spec, ok := lookupShortcutSpec(group, command)
	if !ok {
		return nil, true, errors.New("shortcut metadata not available for " + strings.TrimSpace(group) + " " + strings.TrimSpace(command))
	}
	if meta.Describe {
		out, err := describeShortcutOutput(spec)
		return out, true, err
	}
	out, err := shortcutRequestTemplateOutput(spec, meta.TemplateMode)
	return out, true, err
}

func describeShortcutOutput(spec shortcutCommandSpec) (string, error) {
	reqBody, err := shortcutRequestBodyMeta(spec)
	if err != nil {
		return "", err
	}
	var flagDoc []apiCapDocParam
	var bodyFields []describeFieldParam
	if strings.TrimSpace(spec.APIGroup) != "" && strings.TrimSpace(spec.APIAction) != "" {
		if ops, err := shortcutActionOps(spec.APIGroup, spec.APIAction); err == nil && len(ops) > 0 {
			bodyDoc, docs := splitRequestParamsDocForOutput(ops[0].Cmd.RequestParamsDoc)
			flagDoc = append(flagDoc, bodyDoc...)
			flagDoc = append(flagDoc, docs...)
			bodyFields = buildRequestBodyFields(ops[0].Cmd, ops[0].Cmd.Params, bodyDoc)
		}
	}
	out := shortcutDescribeOutput{
		Group:                  spec.Group,
		Command:                spec.Command,
		Action:                 spec.Action,
		APIGroup:               spec.APIGroup,
		APIAction:              spec.APIAction,
		Summary:                spec.Summary,
		Description:            spec.Description,
		Method:                 spec.Method,
		Path:                   spec.Path,
		InputMode:              spec.InputMode,
		PreferredOutputMode:    spec.PreferredOutputMode,
		RecommendedGlobalFlags: append([]string(nil), spec.RecommendedGlobalFlags...),
		OutputFilterScope:      "JMESPath applies to the raw command result before CLI envelope wrapping; for example, filter Total instead of data.Total.",
		OutputFilterExamples:   defaultJMESExamplesForGroup(spec.Group),
		ShellQuoting: map[string]string{
			"bash":       `--jmes-filter "keys(@)"`,
			"zsh":        `--jmes-filter "keys(@)"`,
			"fish":       `--jmes-filter 'keys(@)'`,
			"powershell": `--jmes-filter 'keys(@)'`,
		},
		Notes: append([]string(nil), spec.Notes...),
		Guidance: apiDescribeGuidance{
			ListGroup:     "volclog " + spec.Group + " --help",
			Describe:      "volclog " + spec.Group + " " + spec.Command + " --describe",
			Filter:        `volclog ` + spec.Group + ` ` + spec.Command + ` --jmes-filter "keys(@)"`,
			ShortcutFirst: relatedShortcutDescribesForShortcut(spec.Group, spec.Command),
		},
	}
	if wf, err := resolveWorkflowByIdentity(spec.Group, spec.Command); err == nil {
		out.Guidance.FallbackDiscovery = "volclog workflow describe " + strings.TrimSpace(wf.ID)
	} else if action := toolIdentityAction(spec.Action); action != "" {
		if _, ok := loadToolByIdentity(spec.Group, action); ok {
			out.Guidance.FallbackDiscovery = "volclog tool list " + strings.TrimSpace(spec.APIGroup)
			out.Guidance.FallbackAPIDescribe = "volclog tool describe " + strings.TrimSpace(spec.Action)
		}
	}
	out.Input = &describeInput{
		Flags: buildShortcutFlagInput(spec.Params, flagDoc),
	}
	if spec.SupportsTemplate {
		if spec.PreferredOutputMode == "file" {
			out.Guidance.Execute = "volclog --output-mode file " + spec.Group + " " + spec.Command + " --request file://req.json"
		} else {
			out.Guidance.Execute = "volclog " + spec.Group + " " + spec.Command + " --request file://req.json"
		}
	} else if spec.PreferredOutputMode == "file" {
		out.Guidance.Execute = "volclog --output-mode file " + spec.Group + " " + spec.Command
	} else {
		out.Guidance.Execute = "volclog " + spec.Group + " " + spec.Command
	}

	if shouldDescribeShortcutRequestBody(spec.Params) {
		if reqBody == nil {
			reqBody = &apiDescribeRequestBody{Required: shortcutBodyRequired(spec.Params)}
		}
		if out.Input == nil {
			out.Input = &describeInput{}
		}
		printTemplate := ""
		if spec.SupportsTemplate {
			printTemplate = "volclog " + spec.Group + " " + spec.Command + " --print-request-template=required|full"
		}
		actionGroup := spec.APIGroup
		if strings.TrimSpace(actionGroup) == "" {
			actionGroup = spec.Group
		}
		actionName := spec.APIAction
		if strings.TrimSpace(actionName) == "" {
			actionName = spec.Command
		}
		out.Input.RequestBody = buildShortcutRequestBodyInput(reqBody, printTemplate, actionGroup, actionName, bodyFields)
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

func rejectLegacyShortcutMeta(group, command string, args []string) error {
	flag, ok := shortcutLegacyMetaFlag(args)
	if !ok {
		return nil
	}
	return removedLegacyCommandError(
		strings.TrimSpace(group)+" "+strings.TrimSpace(command)+" "+flag,
		legacyShortcutMetaHint(group, command),
	)
}

func shortcutLegacyMetaFlag(args []string) (string, bool) {
	for _, arg := range args {
		switch {
		case arg == "--describe":
			return "--describe", true
		case arg == "--print-request-template":
			return "--print-request-template", true
		case strings.HasPrefix(arg, "--print-request-template="):
			return strings.TrimSpace(arg), true
		}
	}
	return "", false
}

func shortcutRequestBodyMeta(spec shortcutCommandSpec) (*apiDescribeRequestBody, error) {
	if strings.TrimSpace(spec.APIGroup) == "" || strings.TrimSpace(spec.APIAction) == "" {
		return nil, nil
	}
	ops, err := shortcutActionOps(spec.APIGroup, spec.APIAction)
	if err != nil {
		if !spec.SupportsTemplate {
			return nil, nil
		}
		return nil, err
	}
	if len(ops) == 0 {
		return nil, nil
	}
	reqBody := (*apiDescribeRequestBody)(nil)
	if bodyParam, ok := firstBodyParam(ops[0].Cmd.Params); ok {
		reqBody = &apiDescribeRequestBody{Required: bodyParam.Required}
	}
	return reqBody, nil
}

func shortcutBodyRequired(params []apiCapParam) bool {
	for _, param := range params {
		if strings.EqualFold(strings.TrimSpace(param.In), "body") && param.Required {
			return true
		}
	}
	return false
}

func buildShortcutFlagInput(params []apiCapParam, doc []apiCapDocParam) *describeFlagInput {
	if len(params) == 0 {
		return nil
	}
	fields := make([]apiCapParam, 0, len(params))
	for _, param := range params {
		cp := param
		cp.In = strings.ToLower(strings.TrimSpace(cp.In))
		fields = append(fields, cp)
	}
	fields = mergeParamsWithDoc(fields, doc)
	return &describeFlagInput{
		Fields:   describeFieldParams(fields),
		Guidance: buildParamGuidance(fields, "shortcut"),
	}
}

func shouldDescribeShortcutRequestBody(params []apiCapParam) bool {
	hasRequest := false
	structuredBodyFlags := 0
	for _, param := range params {
		in := strings.ToLower(strings.TrimSpace(param.In))
		flag := strings.TrimSpace(param.CLIFlag)
		if in != "body" {
			continue
		}
		if strings.Contains(flag, "--request") || strings.EqualFold(strings.TrimSpace(param.Name), "request") {
			hasRequest = true
			continue
		}
		structuredBodyFlags++
	}
	return hasRequest && structuredBodyFlags == 0
}

func buildShortcutRequestBodyInput(req *apiDescribeRequestBody, printTemplate string, group string, action string, fields []describeFieldParam) *describeRequestBodyInput {
	out := buildRequestBodyInput(req, printTemplate, group, action, fields)
	if out == nil {
		return nil
	}
	out.Note = "这个 shortcut 没有把请求体字段拆成独立 flags，请直接通过 --request 传 JSON。"
	if strings.EqualFold(strings.TrimSpace(group), "log") && strings.EqualFold(strings.TrimSpace(action), "PutLogs") {
		out.Note = "这个 shortcut 主要通过 --request 传 JSON/JSONL，再由 CLI 编码为 PutLogs 所需 protobuf。Logs[].Time 必须是 Unix 毫秒时间戳，例如 1710374400000，不要填秒级 1710374400。"
	} else if strings.TrimSpace(printTemplate) != "" {
		out.Note = "这个 shortcut 主要通过 --request 传 JSON。先用 required 看最小骨架；字段不确定、结构较深时再切到 full。"
	}
	return out
}

func shortcutRequestTemplateOutput(spec shortcutCommandSpec, mode string) (string, error) {
	if !spec.SupportsTemplate {
		return "", errors.New("request template is not available for " + spec.Group + " " + spec.Command)
	}
	ops, err := shortcutActionOps(spec.APIGroup, spec.APIAction)
	if err != nil {
		return "", err
	}
	tpl := strings.TrimSpace(requestTemplateOutput(ops, mode, generatedRequestTemplates, generatedRequestTemplatesFull))
	if tpl == "" {
		return "", errors.New("empty request template for " + spec.Group + " " + spec.Command)
	}
	if len(spec.TemplateOmit) == 0 {
		return tpl + "\n", nil
	}
	v, err := util.UnmarshalJSON([]byte(tpl))
	if err != nil {
		return "", err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return tpl + "\n", nil
	}
	for _, key := range spec.TemplateOmit {
		delete(m, key)
	}
	b, err := marshalIndentNoEscape(m)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func legacyShortcutMetaHint(group, command string) string {
	group = strings.TrimSpace(group)
	command = strings.TrimSpace(command)
	if wf, err := resolveWorkflowByIdentity(group, command); err == nil {
		return "use 'volclog workflow describe " + strings.TrimSpace(wf.ID) + "' for workflow contract or run 'volclog " + group + " " + command + " -h'"
	}
	spec, ok := lookupShortcutSpec(group, command)
	if !ok {
		return "use 'volclog " + group + " " + command + " -h' for shortcut usage"
	}
	action := toolIdentityAction(spec.Action)
	if action != "" {
		if _, ok := loadToolByIdentity(spec.Group, action); ok {
			return "use 'volclog tool describe " + strings.TrimSpace(spec.Action) + "' for public API contract or run 'volclog " + group + " " + command + " -h'"
		}
	}
	if strings.TrimSpace(spec.APIGroup) != "" {
		return "shortcut metadata flags were removed; use 'volclog tool list " + strings.TrimSpace(spec.APIGroup) + "' for public API discovery or run 'volclog " + group + " " + command + " -h'"
	}
	return "shortcut metadata flags were removed; run 'volclog " + group + " " + command + " -h'"
}

func shortcutActionOps(group, action string) ([]apiActionOp, error) {
	doc, err := loadAPICapabilities()
	if err != nil {
		return nil, err
	}
	index := buildAPIIndex(doc)
	actions, ok := index[normalizeToken(group)]
	if !ok {
		return nil, errors.New("api group not found: " + group)
	}
	ops, ok := actions[normalizeActionToken(action)]
	if !ok || len(ops) == 0 {
		return nil, errors.New("api action not found: " + group + "." + action)
	}
	return ops, nil
}

func lookupShortcutSpec(group, command string) (shortcutCommandSpec, bool) {
	key := normalizeToken(group) + "\x00" + normalizeToken(command)
	spec, ok := shortcutSpecs()[key]
	return spec, ok
}

func shortcutSpecs() map[string]shortcutCommandSpec {
	specs := []shortcutCommandSpec{
		{
			Group:         "project",
			Command:       "list",
			Action:        "project.list",
			Summary:       "列出日志项目",
			Description:   "高频列举入口，支持分页和 --all 自动翻页。",
			Method:        "GET",
			Path:          "/DescribeProjects",
			InputMode:     "filters via flags",
			RequiredFlags: nil,
			Params: []apiCapParam{
				flagParam("PageNumber", "--page-number", "query", false, "integer", "页码"),
				flagParam("PageSize", "--page-size", "query", false, "integer", "每页数量"),
				flagParam("ProjectName", "--project-name", "query", false, "string", "项目名精确过滤"),
				flagParam("ProjectId", "--project-id", "query", false, "string", "项目 ID"),
				flagParam("all", "--all", "meta", false, "boolean", "自动翻完整分页"),
			},
			APIGroup:  "project",
			APIAction: "DescribeProjects",
			Notes: []string{
				"列表结果支持 --output table 和 --all。",
				"需要结构化契约时，先用 volclog tool list project，再用 volclog tool describe。",
			},
		},
		{
			Group:         "project",
			Command:       "get",
			Action:        "project.get",
			Summary:       "查询单个日志项目",
			Description:   "按 ProjectId 获取项目详情。",
			Method:        "GET",
			Path:          "/DescribeProject",
			InputMode:     "query via flags",
			RequiredFlags: []string{"--project-id"},
			Params: []apiCapParam{
				flagParam("ProjectId", "--project-id", "query", true, "string", "项目 ID"),
			},
			APIGroup:  "project",
			APIAction: "DescribeProject",
		},
		{
			Group:         "project",
			Command:       "create",
			Action:        "project.create",
			Summary:       "创建日志项目",
			Description:   "支持少量字段直接走 flags，也支持完整 JSON 走 --request。",
			Method:        "POST",
			Path:          "/CreateProject",
			InputMode:     "body via --request; common fields via flags",
			RequiredFlags: []string{"--project-name or request.ProjectName"},
			Params: []apiCapParam{
				flagParam("ProjectName", "--project-name", "body", true, "string", "项目名称；也可放到 --request JSON"),
				flagParam("Description", "--description", "body", false, "string", "项目描述"),
				flagParam("IamProjectName", "--iam-project-name", "body", false, "string", "IAM 项目名"),
				flagParam("Region", "--region", "body", false, "string", "区域；缺省时使用当前 profile.region"),
				flagParam("Tags", "--tags", "body", false, "array", "JSON 数组、file://... 或裸文件路径"),
				flagParam("request", "--request", "body", false, "json", "inline JSON、file://...、- 或裸文件路径"),
			},
			APIGroup:         "project",
			APIAction:        "CreateProject",
			SupportsTemplate: true,
			Notes: []string{
				"当字段较多时，优先直接通过 --request file://req.json 组织完整 JSON。",
				"如果未显式传 Region，会回落到当前 profile 的 region。",
			},
		},
		{
			Group:         "project",
			Command:       "modify",
			Action:        "project.modify",
			Summary:       "修改日志项目",
			Description:   "需要 ProjectId；其余字段可通过 flags 或 --request 提供。",
			Method:        "PUT",
			Path:          "/ModifyProject",
			InputMode:     "body via --request; common fields via flags",
			RequiredFlags: []string{"--project-id"},
			Params: []apiCapParam{
				flagParam("ProjectId", "--project-id", "body", true, "string", "项目 ID"),
				flagParam("ProjectName", "--project-name", "body", false, "string", "项目名称"),
				flagParam("Description", "--description", "body", false, "string", "项目描述"),
				flagParam("request", "--request", "body", false, "json", "完整请求 JSON"),
			},
			APIGroup:         "project",
			APIAction:        "ModifyProject",
			SupportsTemplate: true,
		},
		{
			Group:         "project",
			Command:       "delete",
			Action:        "project.delete",
			Summary:       "删除日志项目",
			Description:   "按 ProjectId 删除项目。",
			Method:        "DELETE",
			Path:          "/DeleteProject",
			InputMode:     "body synthesized from flags",
			RequiredFlags: []string{"--project-id"},
			Params: []apiCapParam{
				flagParam("ProjectId", "--project-id", "body", true, "string", "项目 ID"),
			},
			APIGroup:  "project",
			APIAction: "DeleteProject",
		},
		{
			Group:       "topic",
			Command:     "list",
			Action:      "topic.list",
			Summary:     "列出日志主题",
			Description: "支持 ProjectId/TopicId/TopicName 过滤和 --all 自动翻页。",
			Method:      "GET",
			Path:        "/DescribeTopics",
			InputMode:   "filters via flags",
			Params: []apiCapParam{
				flagParam("ProjectId", "--project-id", "query", false, "string", "所属项目 ID"),
				flagParam("TopicId", "--topic-id", "query", false, "string", "主题 ID"),
				flagParam("TopicName", "--topic-name", "query", false, "string", "主题名"),
				flagParam("PageSize", "--page-size", "query", false, "integer", "每页数量"),
				flagParam("PageNumber", "--page-number", "query", false, "integer", "页码"),
				flagParam("all", "--all", "meta", false, "boolean", "自动翻完整分页"),
			},
			APIGroup:  "topic",
			APIAction: "DescribeTopics",
			Notes: []string{
				"TopicName 和 TopicId 不应同时提供。",
				"列表结果支持 --output table 和 --all。",
			},
		},
		{
			Group:         "topic",
			Command:       "get",
			Action:        "topic.get",
			Summary:       "查询单个日志主题",
			Description:   "按 TopicId 获取主题详情。",
			Method:        "GET",
			Path:          "/DescribeTopic",
			InputMode:     "query via flags",
			RequiredFlags: []string{"--topic-id"},
			Params: []apiCapParam{
				flagParam("TopicId", "--topic-id", "query", true, "string", "主题 ID"),
			},
			APIGroup:  "topic",
			APIAction: "DescribeTopic",
		},
		{
			Group:         "topic",
			Command:       "create",
			Action:        "topic.create",
			Summary:       "创建日志主题",
			Description:   "支持常用 flags 快速创建，也支持完整 JSON 走 --request。",
			Method:        "POST",
			Path:          "/CreateTopic",
			InputMode:     "body via --request; common fields via flags",
			RequiredFlags: []string{"--project-id, --topic-name, --ttl and --shard-count (or request.ProjectId/request.TopicName/request.Ttl/request.ShardCount)"},
			Params: []apiCapParam{
				flagParam("ProjectId", "--project-id", "body", true, "string", "所属项目 ID"),
				flagParam("TopicName", "--topic-name", "body", true, "string", "主题名"),
				flagParam("Ttl", "--ttl", "body", true, "integer", "日志总保存时间，单位天"),
				flagParam("ShardCount", "--shard-count", "body", true, "integer", "分区数量"),
				flagParam("Description", "--description", "body", false, "string", "主题描述"),
				flagParam("AutoSplit", "--auto-split", "body", false, "boolean", "开启自动分裂"),
				flagParam("MaxSplitShard", "--max-split-shard", "body", false, "integer", "自动分裂最大 shard"),
				flagParam("EnableTracking", "--enable-tracking", "body", false, "boolean", "开启 WebTracking"),
				flagParam("EnableTracking", "--disable-tracking", "body", false, "boolean", "关闭 WebTracking"),
				flagParam("MeteringMode", "--metering-mode", "body", false, "string", "计费模式"),
				flagParam("LogPublicIP", "--log-public-ip", "body", false, "boolean", "开启记录来源 IP"),
				flagParam("LogPublicIP", "--no-log-public-ip", "body", false, "boolean", "关闭记录来源 IP"),
				flagParam("EnableHotTtl", "--enable-hot-ttl", "body", false, "boolean", "开启分层存储"),
				flagParam("EnableHotTtl", "--disable-hot-ttl", "body", false, "boolean", "关闭分层存储"),
				flagParam("HotTtl", "--hot-ttl", "body", false, "integer", "标准存储时长"),
				flagParam("ColdTtl", "--cold-ttl", "body", false, "integer", "低频存储时长"),
				flagParam("ArchiveTtl", "--archive-ttl", "body", false, "integer", "归档存储时长"),
				flagParam("TimeKey", "--time-key", "body", false, "string", "日志时间字段名"),
				flagParam("TimeFormat", "--time-format", "body", false, "string", "日志时间字段格式"),
				flagParam("EncryptConf", "--encrypt-conf", "body", false, "json", "数据加密配置 JSON"),
				flagParam("Tags", "--tags", "body", false, "json", "标签数组 JSON"),
				flagParam("request", "--request", "body", false, "json", "完整请求 JSON"),
			},
			APIGroup:         "topic",
			APIAction:        "CreateTopic",
			SupportsTemplate: true,
			Notes: []string{
				"Ttl 和 ShardCount 按官网文档属于必填字段；shortcut describe 也按此口径展示。",
				"EnableHotTtl=true 时，Ttl 时长需与 HotTtl、ColdTtl 和 ArchiveTtl 总值相同。",
				"HotTtl、ColdTtl、ArchiveTtl 仅在 EnableHotTtl=true 时生效。",
				"AutoSplit=true 时，MaxSplitShard 必填，且必须大于 ShardCount。",
				"TimeKey 和 TimeFormat 必须成对提供。",
			},
		},
		{
			Group:         "topic",
			Command:       "modify",
			Action:        "topic.modify",
			Summary:       "修改日志主题",
			Description:   "需要 TopicId；其余字段可通过 flags 或 --request 提供。",
			Method:        "PUT",
			Path:          "/ModifyTopic",
			InputMode:     "body via --request; common fields via flags",
			RequiredFlags: []string{"--topic-id"},
			Params: []apiCapParam{
				flagParam("TopicId", "--topic-id", "body", true, "string", "主题 ID"),
				flagParam("TopicName", "--topic-name", "body", false, "string", "主题名"),
				flagParam("Description", "--description", "body", false, "string", "主题描述"),
				flagParam("Ttl", "--ttl", "body", false, "integer", "保存天数"),
				flagParam("AutoSplit", "--auto-split/--no-auto-split", "body", false, "boolean", "设置自动分裂"),
				flagParam("request", "--request", "body", false, "json", "完整请求 JSON"),
			},
			APIGroup:         "topic",
			APIAction:        "ModifyTopic",
			SupportsTemplate: true,
		},
		{
			Group:         "topic",
			Command:       "delete",
			Action:        "topic.delete",
			Summary:       "删除日志主题",
			Description:   "按 TopicId 删除主题。",
			Method:        "DELETE",
			Path:          "/DeleteTopic",
			InputMode:     "body synthesized from flags",
			RequiredFlags: []string{"--topic-id"},
			Params: []apiCapParam{
				flagParam("TopicId", "--topic-id", "body", true, "string", "主题 ID"),
			},
			APIGroup:  "topic",
			APIAction: "DeleteTopic",
		},
		{
			Group:       "metric-topic",
			Command:     "list",
			Action:      "metric-topic.list",
			Summary:     "列出指标主题",
			Description: "支持 ProjectId/TopicId/TopicName 过滤和 --all 自动翻页。",
			Method:      "GET",
			Path:        "/DescribeMetricTopics",
			InputMode:   "filters via flags",
			Params: []apiCapParam{
				flagParam("ProjectId", "--project-id", "query", false, "string", "所属项目 ID"),
				flagParam("TopicId", "--topic-id", "query", false, "string", "主题 ID"),
				flagParam("TopicName", "--topic-name", "query", false, "string", "主题名"),
				flagParam("PageSize", "--page-size", "query", false, "integer", "每页数量"),
				flagParam("PageNumber", "--page-number", "query", false, "integer", "页码"),
				flagParam("all", "--all", "meta", false, "boolean", "自动翻完整分页"),
			},
			APIGroup:  "metric-topic",
			APIAction: "DescribeMetricTopics",
			Notes: []string{
				"列表结果支持 --output table 和 --all。",
			},
		},
		{
			Group:         "metric-topic",
			Command:       "get",
			Action:        "metric-topic.get",
			Summary:       "查询单个指标主题",
			Description:   "按 TopicId 获取指标主题详情。",
			Method:        "GET",
			Path:          "/DescribeMetricTopic",
			InputMode:     "query via flags",
			RequiredFlags: []string{"--topic-id"},
			Params: []apiCapParam{
				flagParam("TopicId", "--topic-id", "query", true, "string", "主题 ID"),
			},
			APIGroup:  "metric-topic",
			APIAction: "DescribeMetricTopic",
		},
		{
			Group:         "metric-topic",
			Command:       "create",
			Action:        "metric-topic.create",
			Summary:       "创建指标主题",
			Description:   "支持常用 flags 快速创建，也支持完整 JSON 走 --request。",
			Method:        "POST",
			Path:          "/CreateMetricTopic",
			InputMode:     "body via --request; common fields via flags",
			RequiredFlags: []string{"--project-id and --topic-name (or request.ProjectId/request.TopicName)"},
			Params: []apiCapParam{
				flagParam("ProjectId", "--project-id", "body", true, "string", "所属项目 ID"),
				flagParam("TopicName", "--topic-name", "body", true, "string", "主题名"),
				flagParam("Description", "--description", "body", false, "string", "主题描述"),
				flagParam("Ttl", "--ttl", "body", false, "integer", "保存天数"),
				flagParam("ShardCount", "--shard-count", "body", false, "integer", "Shard 数量"),
				flagParam("AutoSplit", "--auto-split", "body", false, "boolean", "开启自动分裂"),
				flagParam("request", "--request", "body", false, "json", "完整请求 JSON"),
			},
			APIGroup:         "metric-topic",
			APIAction:        "CreateMetricTopic",
			SupportsTemplate: true,
		},
		{
			Group:         "metric-topic",
			Command:       "modify",
			Action:        "metric-topic.modify",
			Summary:       "修改指标主题",
			Description:   "需要 TopicId；其余字段可通过 flags 或 --request 提供。",
			Method:        "PUT",
			Path:          "/ModifyMetricTopic",
			InputMode:     "body via --request; common fields via flags",
			RequiredFlags: []string{"--topic-id"},
			Params: []apiCapParam{
				flagParam("TopicId", "--topic-id", "body", true, "string", "主题 ID"),
				flagParam("TopicName", "--topic-name", "body", false, "string", "主题名"),
				flagParam("Description", "--description/--clear-description", "body", false, "string", "主题描述"),
				flagParam("Ttl", "--ttl", "body", false, "integer", "保存天数"),
				flagParam("AutoSplit", "--auto-split/--no-auto-split", "body", false, "boolean", "设置自动分裂"),
				flagParam("request", "--request", "body", false, "json", "完整请求 JSON"),
			},
			APIGroup:         "metric-topic",
			APIAction:        "ModifyMetricTopic",
			SupportsTemplate: true,
		},
		{
			Group:         "metric-topic",
			Command:       "delete",
			Action:        "metric-topic.delete",
			Summary:       "删除指标主题",
			Description:   "按 TopicId 删除指标主题。",
			Method:        "DELETE",
			Path:          "/DeleteMetricTopic",
			InputMode:     "body synthesized from flags",
			RequiredFlags: []string{"--topic-id"},
			Params: []apiCapParam{
				flagParam("TopicId", "--topic-id", "body", true, "string", "主题 ID"),
			},
			APIGroup:  "metric-topic",
			APIAction: "DeleteMetricTopic",
		},
		{
			Group:                  "metric-topic",
			Command:                "search",
			Action:                 "metric-topic.search",
			Summary:                "在指标主题上执行查询",
			Description:            "复用 SearchLogs 请求体，常见于 SQL/PromQL 查询。",
			Method:                 "POST",
			Path:                   "/SearchLogs",
			InputMode:              "body via --request; common fields via flags",
			PreferredOutputMode:    "file",
			RecommendedGlobalFlags: []string{"--output-mode file"},
			RequiredFlags:          []string{"--topic-id and --query and --from and --to (or request fields)"},
			Params: []apiCapParam{
				flagParam("TopicId", "--topic-id", "body", true, "string", "主题 ID"),
				flagParam("Query", "--query", "body", true, "string", "查询语句"),
				flagParam("StartTime", "--from", "body", true, "integer", "起始毫秒时间戳"),
				flagParam("EndTime", "--to", "body", true, "integer", "结束毫秒时间戳"),
				flagParam("request", "--request", "body", false, "json", "完整请求 JSON"),
			},
			APIGroup:         "log",
			APIAction:        "SearchLogs",
			SupportsTemplate: true,
			Notes: []string{
				"未显式指定 --limit 时，导出默认批次大于普通 search；需要更大/更小批次时自行覆盖。",
			},
		},
		{
			Group:         "index",
			Command:       "get",
			Action:        "index.get",
			Summary:       "查询索引配置",
			Description:   "按 TopicId 获取索引详情。",
			Method:        "GET",
			Path:          "/DescribeIndex",
			InputMode:     "query via flags",
			RequiredFlags: []string{"--topic-id"},
			Params: []apiCapParam{
				flagParam("TopicId", "--topic-id", "query", true, "string", "主题 ID"),
			},
			APIGroup:  "index",
			APIAction: "DescribeIndex",
		},
		{
			Group:         "index",
			Command:       "create",
			Action:        "index.create",
			Summary:       "创建索引",
			Description:   "索引 body 建议先用模板生成，再按 TopicId 提交。",
			Method:        "POST",
			Path:          "/CreateIndex",
			InputMode:     "body via --request/--body; topic via flag",
			RequiredFlags: []string{"--topic-id", "--request or --body"},
			Params: []apiCapParam{
				flagParam("TopicId", "--topic-id", "query", true, "string", "主题 ID"),
				flagParam("request", "--request/--body", "body", true, "json", "索引 JSON；支持 inline、file://...、- 或裸文件路径"),
			},
			APIGroup:         "index",
			APIAction:        "CreateIndex",
			SupportsTemplate: true,
			TemplateOmit:     []string{"TopicId"},
			Notes: []string{
				"模板会省略 TopicId，因为该值由 --topic-id 单独提供。",
			},
		},
		{
			Group:         "index",
			Command:       "modify",
			Action:        "index.modify",
			Summary:       "修改索引",
			Description:   "索引 body 建议先用模板生成，再按 TopicId 提交。",
			Method:        "PUT",
			Path:          "/ModifyIndex",
			InputMode:     "body via --request/--body; topic via flag",
			RequiredFlags: []string{"--topic-id", "--request or --body"},
			Params: []apiCapParam{
				flagParam("TopicId", "--topic-id", "query", true, "string", "主题 ID"),
				flagParam("request", "--request/--body", "body", true, "json", "索引 JSON；支持 inline、file://...、- 或裸文件路径"),
			},
			APIGroup:         "index",
			APIAction:        "ModifyIndex",
			SupportsTemplate: true,
			TemplateOmit:     []string{"TopicId"},
		},
		{
			Group:                  "log",
			Command:                "search",
			Action:                 "log.search",
			Summary:                "执行日志检索",
			Description:            "复用 SearchLogs 请求体；大结果优先配合 --output-mode file。",
			Method:                 "POST",
			Path:                   "/SearchLogs",
			InputMode:              "body via --request; common fields via flags",
			PreferredOutputMode:    "file",
			RecommendedGlobalFlags: []string{"--output-mode file"},
			RequiredFlags:          []string{"--topic-id and --query and --from and --to (or request fields)"},
			Params: []apiCapParam{
				flagParam("TopicId", "--topic-id", "body", true, "string", "主题 ID"),
				flagParam("Query", "--query", "body", true, "string", "查询语句"),
				flagParam("StartTime", "--from", "body", true, "integer", "起始毫秒时间戳"),
				flagParam("EndTime", "--to", "body", true, "integer", "结束毫秒时间戳"),
				flagParam("Limit", "--limit", "body", false, "integer", "返回条数"),
				flagParam("Sort", "--sort", "body", false, "string", "asc/desc"),
				flagParam("request", "--request", "body", false, "json", "完整请求 JSON"),
			},
			APIGroup:         "log",
			APIAction:        "SearchLogs",
			SupportsTemplate: true,
			Notes: []string{
				"分析查询（Query 中带 |select ...）与普通检索的分页语义不同。",
			},
		},
		{
			Group:                  "log",
			Command:                "histogram",
			Action:                 "log.histogram",
			Summary:                "查询日志直方图",
			Description:            "复用 DescribeHistogram 请求体；适合先看时间分布再决定检索窗口。",
			Method:                 "POST",
			Path:                   "/DescribeHistogram",
			InputMode:              "body via --request; common fields via flags",
			PreferredOutputMode:    "file",
			RecommendedGlobalFlags: []string{"--output-mode file"},
			RequiredFlags:          []string{"--topic-id and --query and --from and --to (or request fields)"},
			Params: []apiCapParam{
				flagParam("TopicId", "--topic-id", "body", true, "string", "主题 ID"),
				flagParam("Query", "--query", "body", true, "string", "查询语句"),
				flagParam("StartTime", "--from", "body", true, "integer", "起始毫秒时间戳"),
				flagParam("EndTime", "--to", "body", true, "integer", "结束毫秒时间戳"),
				flagParam("Interval", "--interval", "body", false, "integer", "直方图桶宽"),
				flagParam("request", "--request", "body", false, "json", "完整请求 JSON"),
			},
			APIGroup:         "log",
			APIAction:        "DescribeHistogram",
			SupportsTemplate: true,
		},
		{
			Group:               "log",
			Command:             "context",
			Action:              "log.context",
			Summary:             "查看命中日志上下文",
			Description:         "复用 DescribeLogContext 请求体；适合基于 SearchLogs 命中结果查看前后文。",
			Method:              "POST",
			Path:                "/DescribeLogContext",
			InputMode:           "body via --request; common fields via flags",
			RequiredFlags:       []string{"--topic-id and --context-flow and --package-offset and --source (or request fields)"},
			PreferredOutputMode: "file",
			Params: []apiCapParam{
				flagParam("TopicId", "--topic-id", "body", true, "string", "主题 ID"),
				flagParam("ContextFlow", "--context-flow", "body", true, "string", "日志所在 LogGroup 的 ContextFlow"),
				flagParam("PackageOffset", "--package-offset", "body", true, "integer", "日志在 LogGroup 中的序号"),
				flagParam("Source", "--source", "body", true, "string", "日志来源 IP"),
				flagParam("PrevLogs", "--prev-logs", "body", false, "integer", "向前查看条数"),
				flagParam("NextLogs", "--next-logs", "body", false, "integer", "向后查看条数"),
				flagParam("request", "--request", "body", false, "json", "完整请求 JSON"),
			},
			APIGroup:         "log",
			APIAction:        "DescribeLogContext",
			SupportsTemplate: true,
			Notes: []string{
				"ContextFlow、PackageOffset、Source 一般来自 SearchLogs 命中日志对象。",
			},
		},
		{
			Group:         "log",
			Command:       "put",
			Action:        "log.put",
			Summary:       "写入日志",
			Description:   "复用 PutLogs 特殊 IO；支持 JSON 和 JSONL 输入，再由 CLI 编码为 protobuf。",
			Method:        "POST",
			Path:          "/PutLogs",
			InputMode:     "special io via --request and optional --request-format json|jsonl",
			RequiredFlags: []string{"--topic-id", "--request"},
			Params: []apiCapParam{
				flagParam("TopicId", "--topic-id", "query", true, "string", "主题 ID"),
				flagParam("request", "--request", "body", true, "json/jsonl", "LogGroupList JSON 或 JSONL 日志行"),
				flagParam("requestFormat", "--request-format", "meta", false, "string", "json 或 jsonl"),
				flagParam("x-tls-compresstype", "--compress-type", "header", false, "string", "lz4/zlib/none"),
				flagParam("x-tls-hashkey", "--hash-key", "header", false, "string", "指定写入分区的 HashKey"),
				flagParam("Content-MD5", "--content-md5", "header", false, "string", "请求体 MD5"),
			},
			APIGroup:         "log",
			APIAction:        "PutLogs",
			SupportsTemplate: true,
			Notes: []string{
				"CLI 会自动把 JSON/JSONL 编码为 PutLogs 所需 protobuf body。",
				"Logs[].Time 必须是 Unix 毫秒时间戳，不是秒级。",
			},
		},
		{
			Group:       "log",
			Command:     "ingest",
			Action:      "log.ingest",
			Summary:     "批量导入文本或 JSON 日志",
			Description: "面向高层写入场景；CLI 负责补时间、组批、统计头和 protobuf 编码。",
			Method:      "POST",
			Path:        "/PutLogs",
			InputMode:   "ingest via --input; lines/jsonl/json-array normalized by CLI before PutLogs",
			RequiredFlags: []string{
				"--topic-id",
				"--input",
			},
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
			Notes: []string{
				"lines 输入默认写入字段 __content__。",
				"未指定 --time-field 时，CLI 会用本次命令启动时的毫秒时间戳补齐日志时间。",
				"jsonl/json-array 会保留用户原始字段，不做 message 字段重映射。",
				"每个批次都会自动带上 log-count、earliest-log-time、latest-log-time 请求头。",
			},
		},
		{
			Group:                  "log",
			Command:                "export",
			Action:                 "log.export",
			Summary:                "自动翻页导出日志",
			Description:            "面向纯检索结果导出；分析语句请改用 export-analysis。",
			Method:                 "POST",
			Path:                   "/SearchLogs",
			InputMode:              "body via --request; auto-pagination in command",
			PreferredOutputMode:    "file",
			RecommendedGlobalFlags: []string{"--output jsonl", "--output-mode file"},
			RequiredFlags:          []string{"--topic-id and --query and --from and --to (or request fields)"},
			Params: []apiCapParam{
				flagParam("TopicId", "--topic-id", "body", true, "string", "主题 ID"),
				flagParam("Query", "--query", "body", true, "string", "非分析查询语句"),
				flagParam("StartTime", "--from", "body", true, "integer", "起始毫秒时间戳"),
				flagParam("EndTime", "--to", "body", true, "integer", "结束毫秒时间戳"),
				flagParam("maxPages", "--max-pages", "meta", false, "integer", "最多翻页数量"),
				flagParam("request", "--request", "body", false, "json", "完整请求 JSON"),
			},
			APIGroup:         "log",
			APIAction:        "SearchLogs",
			SupportsTemplate: true,
		},
		{
			Group:                  "log",
			Command:                "export-analysis",
			Action:                 "log.export-analysis",
			Summary:                "导出分析行结果",
			Description:            "面向 SQL/分析语句；结果为行对象集合。",
			Method:                 "POST",
			Path:                   "/SearchLogs",
			InputMode:              "body via --request; analysis mode",
			PreferredOutputMode:    "file",
			RecommendedGlobalFlags: []string{"--output jsonl", "--output-mode file"},
			RequiredFlags:          []string{"--topic-id and analysis query and --from and --to (or request fields)"},
			Params: []apiCapParam{
				flagParam("TopicId", "--topic-id", "body", true, "string", "主题 ID"),
				flagParam("Query", "--query", "body", true, "string", "分析查询语句，示例：*|select count(*)"),
				flagParam("StartTime", "--from", "body", true, "integer", "起始毫秒时间戳"),
				flagParam("EndTime", "--to", "body", true, "integer", "结束毫秒时间戳"),
				flagParam("request", "--request", "body", false, "json", "完整请求 JSON"),
			},
			APIGroup:         "log",
			APIAction:        "SearchLogs",
			SupportsTemplate: true,
			Notes: []string{
				"分析可见列强依赖当前索引配置；新增或修改索引后通常只对增量写入生效，旧日志对应列可能仍为 null。",
			},
		},
		{
			Group:       "host-group",
			Command:     "list",
			Action:      "host-group.list",
			Summary:     "列出机器组",
			Description: "支持模糊过滤和 --all 自动翻页。",
			Method:      "GET",
			Path:        "/DescribeHostGroupsV2",
			InputMode:   "filters via flags",
			Params: []apiCapParam{
				flagParam("HostGroupId", "--host-group-id", "query", false, "string", "机器组 ID 关键词"),
				flagParam("HostGroupName", "--host-group-name", "query", false, "string", "机器组名称关键词"),
				flagParam("HostIdentifier", "--host-identifier", "query", false, "string", "机器组标识"),
				flagParam("IamProjectName", "--iam-project-name", "query", false, "string", "IAM 项目名"),
				flagParam("PageNumber", "--page-number", "query", false, "integer", "页码"),
				flagParam("PageSize", "--page-size", "query", false, "integer", "每页数量"),
				flagParam("AutoUpdate", "--auto-update/--no-auto-update", "query", false, "boolean", "是否自动升级"),
				flagParam("ServiceLogging", "--service-logging/--no-service-logging", "query", false, "boolean", "是否开启服务日志"),
				flagParam("Hidden", "--hidden/--no-hidden", "query", false, "boolean", "是否隐藏专属资源机器组"),
				flagParam("all", "--all", "meta", false, "boolean", "自动翻完整分页"),
			},
			APIGroup:  "host-group",
			APIAction: "DescribeHostGroupsV2",
			Notes: []string{
				"返回层级通常较深，常用 JMESPath 入口是 HostGroupHostsRulesInfos[].HostGroupInfo。",
			},
		},
		{
			Group:         "host-group",
			Command:       "get",
			Action:        "host-group.get",
			Summary:       "查询单个机器组",
			Description:   "按 HostGroupId 获取机器组详情。",
			Method:        "GET",
			Path:          "/DescribeHostGroupV2",
			InputMode:     "query via flags",
			RequiredFlags: []string{"--host-group-id"},
			Params: []apiCapParam{
				flagParam("HostGroupId", "--host-group-id", "query", true, "string", "机器组 ID"),
			},
			APIGroup:  "host-group",
			APIAction: "DescribeHostGroupV2",
		},
		{
			Group:         "host-group",
			Command:       "create",
			Action:        "host-group.create",
			Summary:       "创建机器组",
			Description:   "支持常用 flags 快速创建，也支持完整 JSON 走 --request。",
			Method:        "POST",
			Path:          "/CreateHostGroup",
			InputMode:     "body via --request; common fields via flags",
			RequiredFlags: []string{"--host-group-name and --host-group-type (or request fields)"},
			Params: []apiCapParam{
				flagParam("HostGroupName", "--host-group-name", "body", true, "string", "机器组名称"),
				flagParam("HostGroupType", "--host-group-type", "body", true, "string", "IP 或 Label"),
				flagParam("HostIpList", "--host-ip-list", "body", false, "array", "IP 列表文件或 JSON 数组"),
				flagParam("HostIdentifier", "--host-identifier", "body", false, "string", "Label 机器标识"),
				flagParam("AutoUpdate", "--auto-update/--no-auto-update", "body", false, "boolean", "自动升级"),
				flagParam("UpdateStartTime", "--update-start-time", "body", false, "string", "升级开始时间"),
				flagParam("UpdateEndTime", "--update-end-time", "body", false, "string", "升级结束时间"),
				flagParam("ServiceLogging", "--service-logging/--no-service-logging", "body", false, "boolean", "服务日志"),
				flagParam("IamProjectName", "--iam-project-name", "body", false, "string", "IAM 项目名"),
				flagParam("request", "--request", "body", false, "json", "完整请求 JSON"),
			},
			APIGroup:         "host-group",
			APIAction:        "CreateHostGroup",
			SupportsTemplate: true,
		},
		{
			Group:         "host-group",
			Command:       "modify",
			Action:        "host-group.modify",
			Summary:       "修改机器组",
			Description:   "需要 HostGroupId；其余字段可通过 flags 或 --request 提供。",
			Method:        "PUT",
			Path:          "/ModifyHostGroup",
			InputMode:     "body via --request; common fields via flags",
			RequiredFlags: []string{"--host-group-id"},
			Params: []apiCapParam{
				flagParam("HostGroupId", "--host-group-id", "body", true, "string", "机器组 ID"),
				flagParam("HostGroupName", "--host-group-name", "body", false, "string", "机器组名称"),
				flagParam("HostGroupType", "--host-group-type", "body", false, "string", "IP 或 Label"),
				flagParam("HostIpList", "--host-ip-list", "body", false, "array", "IP 列表文件或 JSON 数组"),
				flagParam("HostIdentifier", "--host-identifier", "body", false, "string", "Label 机器标识"),
				flagParam("AutoUpdate", "--auto-update/--no-auto-update", "body", false, "boolean", "自动升级"),
				flagParam("UpdateStartTime", "--update-start-time", "body", false, "string", "升级开始时间"),
				flagParam("UpdateEndTime", "--update-end-time", "body", false, "string", "升级结束时间"),
				flagParam("ServiceLogging", "--service-logging/--no-service-logging", "body", false, "boolean", "服务日志"),
				flagParam("IamProjectName", "--iam-project-name", "body", false, "string", "IAM 项目名"),
				flagParam("request", "--request", "body", false, "json", "完整请求 JSON"),
			},
			APIGroup:         "host-group",
			APIAction:        "ModifyHostGroup",
			SupportsTemplate: true,
		},
		{
			Group:         "host-group",
			Command:       "delete",
			Action:        "host-group.delete",
			Summary:       "删除机器组",
			Description:   "按 HostGroupId 删除机器组。",
			Method:        "DELETE",
			Path:          "/DeleteHostGroup",
			InputMode:     "body synthesized from flags",
			RequiredFlags: []string{"--host-group-id"},
			Params: []apiCapParam{
				flagParam("HostGroupId", "--host-group-id", "body", true, "string", "机器组 ID"),
			},
			APIGroup:  "host-group",
			APIAction: "DeleteHostGroup",
		},
		{
			Group:       "collector",
			Command:     "list",
			Action:      "collector.list",
			Summary:     "列出采集规则",
			Description: "支持按项目、主题、规则名过滤和 --all 自动翻页。",
			Method:      "GET",
			Path:        "/DescribeRulesV2",
			InputMode:   "filters via flags",
			Params: []apiCapParam{
				flagParam("ProjectId", "--project-id", "query", false, "string", "项目 ID"),
				flagParam("ProjectName", "--project-name", "query", false, "string", "项目名"),
				flagParam("IamProjectName", "--iam-project-name", "query", false, "string", "IAM 项目名"),
				flagParam("RuleId", "--rule-id", "query", false, "string", "规则 ID 关键词"),
				flagParam("RuleName", "--rule-name", "query", false, "string", "规则名关键词"),
				flagParam("TopicId", "--topic-id", "query", false, "string", "主题 ID"),
				flagParam("TopicName", "--topic-name", "query", false, "string", "主题名"),
				flagParam("LogType", "--log-type", "query", false, "string", "采集模式"),
				flagParam("Pause", "--pause/--no-pause", "query", false, "integer", "暂停状态"),
				flagParam("PageNumber", "--page-number", "query", false, "integer", "页码"),
				flagParam("PageSize", "--page-size", "query", false, "integer", "每页数量"),
				flagParam("all", "--all", "meta", false, "boolean", "自动翻完整分页"),
			},
			APIGroup:  "collector",
			APIAction: "DescribeRulesV2",
		},
		{
			Group:         "collector",
			Command:       "get",
			Action:        "collector.get",
			Summary:       "查询单个采集规则",
			Description:   "按 RuleId 获取采集规则详情。",
			Method:        "GET",
			Path:          "/DescribeRuleV2",
			InputMode:     "query via flags",
			RequiredFlags: []string{"--rule-id"},
			Params: []apiCapParam{
				flagParam("RuleId", "--rule-id", "query", true, "string", "采集规则 ID"),
			},
			APIGroup:  "collector",
			APIAction: "DescribeRuleV2",
		},
		{
			Group:         "collector",
			Command:       "create",
			Action:        "collector.create",
			Summary:       "创建采集规则",
			Description:   "支持常用 flags 快速创建，也支持完整 JSON 走 --request。",
			Method:        "POST",
			Path:          "/CreateRule",
			InputMode:     "body via --request; common fields via flags",
			RequiredFlags: []string{"--topic-id and --rule-name (or request fields)"},
			Params: []apiCapParam{
				flagParam("TopicId", "--topic-id", "body", true, "string", "主题 ID"),
				flagParam("RuleName", "--rule-name", "body", true, "string", "规则名称"),
				flagParam("Paths", "--paths", "body", false, "array", "路径列表文件或 JSON 数组"),
				flagParam("LogType", "--log-type", "body", false, "string", "采集模式"),
				flagParam("InputType", "--input-type", "body", false, "integer", "采集类型"),
				flagParam("Pause", "--pause/--no-pause", "body", false, "integer", "是否暂停"),
				flagParam("request", "--request", "body", false, "json", "完整请求 JSON"),
			},
			APIGroup:         "collector",
			APIAction:        "CreateRule",
			SupportsTemplate: true,
			Notes: []string{
				"复杂规则体优先用模板配合 --request file://req.json。",
			},
		},
		{
			Group:         "collector",
			Command:       "modify",
			Action:        "collector.modify",
			Summary:       "修改采集规则",
			Description:   "需要 RuleId；其余字段可通过 flags 或 --request 提供。",
			Method:        "PUT",
			Path:          "/ModifyRule",
			InputMode:     "body via --request; common fields via flags",
			RequiredFlags: []string{"--rule-id"},
			Params: []apiCapParam{
				flagParam("RuleId", "--rule-id", "body", true, "string", "采集规则 ID"),
				flagParam("RuleName", "--rule-name", "body", false, "string", "规则名称"),
				flagParam("Paths", "--paths", "body", false, "array", "路径列表文件或 JSON 数组"),
				flagParam("LogType", "--log-type", "body", false, "string", "采集模式"),
				flagParam("InputType", "--input-type", "body", false, "integer", "采集类型"),
				flagParam("Pause", "--pause/--no-pause", "body", false, "integer", "是否暂停"),
				flagParam("request", "--request", "body", false, "json", "完整请求 JSON"),
			},
			APIGroup:         "collector",
			APIAction:        "ModifyRule",
			SupportsTemplate: true,
		},
		{
			Group:         "collector",
			Command:       "delete",
			Action:        "collector.delete",
			Summary:       "删除采集规则",
			Description:   "按 RuleId 删除采集规则。",
			Method:        "DELETE",
			Path:          "/DeleteRule",
			InputMode:     "body synthesized from flags",
			RequiredFlags: []string{"--rule-id"},
			Params: []apiCapParam{
				flagParam("RuleId", "--rule-id", "body", true, "string", "采集规则 ID"),
			},
			APIGroup:  "collector",
			APIAction: "DeleteRule",
		},
	}
	out := make(map[string]shortcutCommandSpec, len(specs))
	for _, spec := range specs {
		out[normalizeToken(spec.Group)+"\x00"+normalizeToken(spec.Command)] = spec
	}
	return out
}

func flagParam(name, cliFlag, in string, required bool, typ, desc string) apiCapParam {
	return apiCapParam{
		Name:        name,
		CLIFlag:     cliFlag,
		In:          in,
		Required:    required,
		Type:        typ,
		Description: desc,
	}
}

func sortedShortcutGroupsWithMeta() []string {
	groups := map[string]struct{}{}
	for _, spec := range shortcutSpecs() {
		groups[spec.Group] = struct{}{}
	}
	out := make([]string, 0, len(groups))
	for g := range groups {
		out = append(out, g)
	}
	sort.Strings(out)
	return out
}
