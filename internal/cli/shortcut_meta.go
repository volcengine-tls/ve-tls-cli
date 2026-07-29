//go:build human

package cli

import (
	"errors"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
)

type shortcutMetaArgs struct {
	TemplateMode        string
	ShouldPrintTemplate bool
	Describe            bool
	RemainingArgs       []string
}

type shortcutCommandSpec struct {
	Kind                   shortcutKind
	OperationID            string
	WorkflowID             string
	Group                  string
	Command                string
	Action                 string
	HiddenInHelp           bool
	Summary                string
	Description            string
	InputMode              string
	PreferredOutputMode    string
	RecommendedGlobalFlags []string
	RequiredFlags          []string
	Bindings               []shortcutBinding
	Defaults               []shortcutDefault
	Validators             []shortcutValidator
	ResultTransforms       []shortcutResultTransform
	Presentation           shortcutPresentation
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
	target, err := resolveShortcutTarget(spec)
	if err != nil {
		return "", err
	}
	reqBody, err := shortcutRequestBodyMeta(spec)
	if err != nil {
		return "", err
	}
	params := shortcutParams(spec.Bindings)
	describeCmd := "volclog " + spec.Group + " " + spec.Command + " --describe"
	executeCmd := "volclog " + spec.Group + " " + spec.Command
	fallbackDiscovery := ""
	fallbackAPIDescribe := ""
	shortcutFirst := relatedShortcutDescribesForShortcut(spec.Group, spec.Command)

	if target.IsWorkflow {
		wf := target.Workflow
		describeCmd = "volclog workflow describe " + strings.TrimSpace(wf.ID)
		executeCmd = "volclog workflow exec " + strings.TrimSpace(wf.ID) + " --input file://req.json"
		fallbackDiscovery = "volclog workflow list " + strings.TrimSpace(wf.Group)
		shortcutFirst = nil
	} else if target.IsOperation && target.Operation.Visibility == "public" {
		describeCmd = "volclog tool describe " + strings.TrimSpace(spec.Action)
		executeCmd = "volclog tool exec " + strings.TrimSpace(spec.Action) + " --context file://ctx.json --input file://req.json"
		fallbackDiscovery = "volclog tool list " + strings.TrimSpace(target.APIGroup)
		fallbackAPIDescribe = "volclog tool describe " + strings.TrimSpace(spec.Action)
		shortcutFirst = nil
	}
	var flagDoc []apiCapDocParam
	var bodyFields []describeFieldParam
	if target.HasOperation {
		bodyDoc, docs := splitRequestParamsDocForOutput(shortcutOperationDocParams(target.Operation))
		flagDoc = append(flagDoc, bodyDoc...)
		flagDoc = append(flagDoc, docs...)
		bodyFields = shortcutOperationBodyFields(target.Operation)
	}
	out := shortcutDescribeOutput{
		Group:                  spec.Group,
		Command:                spec.Command,
		Action:                 spec.Action,
		APIGroup:               target.APIGroup,
		APIAction:              target.APIAction,
		Summary:                spec.Summary,
		Description:            spec.Description,
		Method:                 target.Method,
		Path:                   target.Path,
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
			ListGroup:           "volclog " + spec.Group + " --help",
			Describe:            describeCmd,
			Execute:             executeCmd,
			Filter:              `volclog ` + spec.Group + ` ` + spec.Command + ` --jmes-filter "keys(@)"`,
			ShortcutFirst:       shortcutFirst,
			FallbackDiscovery:   fallbackDiscovery,
			FallbackAPIDescribe: fallbackAPIDescribe,
		},
	}
	out.Input = &describeInput{
		Flags: buildShortcutFlagInput(params, flagDoc),
	}
	if spec.Presentation.SupportsTemplate && describeCmd == "volclog "+spec.Group+" "+spec.Command+" --describe" {
		if spec.PreferredOutputMode == "file" {
			out.Guidance.Execute = "volclog --output-mode file " + spec.Group + " " + spec.Command + " --request file://req.json"
		} else {
			out.Guidance.Execute = "volclog " + spec.Group + " " + spec.Command + " --request file://req.json"
		}
	} else if spec.PreferredOutputMode == "file" && describeCmd == "volclog "+spec.Group+" "+spec.Command+" --describe" {
		out.Guidance.Execute = "volclog --output-mode file " + spec.Group + " " + spec.Command
	} else if describeCmd == "volclog "+spec.Group+" "+spec.Command+" --describe" {
		out.Guidance.Execute = "volclog " + spec.Group + " " + spec.Command
	}

	if shouldDescribeShortcutRequestBody(params) {
		if reqBody == nil {
			reqBody = &apiDescribeRequestBody{Required: shortcutBodyRequired(params)}
		}
		if out.Input == nil {
			out.Input = &describeInput{}
		}
		printTemplate := ""
		if spec.Presentation.SupportsTemplate {
			printTemplate = "volclog " + spec.Group + " " + spec.Command + " --print-request-template=required|full"
		}
		actionGroup := target.APIGroup
		if strings.TrimSpace(actionGroup) == "" {
			actionGroup = spec.Group
		}
		actionName := target.APIAction
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

func shortcutRequestBodyMeta(spec shortcutCommandSpec) (*apiDescribeRequestBody, error) {
	target, err := resolveShortcutTarget(spec)
	if err != nil {
		return nil, err
	}
	if target.HasOperation {
		return shortcutRequestBodyMetaFromTarget(target), nil
	}
	return nil, nil
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
	if !spec.Presentation.SupportsTemplate {
		return "", errors.New("request template is not available for " + spec.Group + " " + spec.Command)
	}
	target, err := resolveShortcutTarget(spec)
	if err != nil {
		return "", err
	}
	if !target.HasOperation {
		return "", errors.New("request template operation not found for " + spec.Group + " " + spec.Command)
	}
	templateMode := contract.TemplateMode(strings.ToLower(strings.TrimSpace(mode)))
	template, ok, err := contract.RequestSample(target.Operation, templateMode)
	if err != nil {
		return "", err
	}
	if !ok {
		template, err = contract.RequestTemplate(target.Operation, templateMode)
		if err != nil {
			return "", err
		}
	}
	for _, key := range spec.Presentation.TemplateOmit {
		delete(template, key)
	}
	b, err := marshalIndentNoEscape(template)
	if err != nil {
		return "", err
	}
	return string(b), nil
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
			Kind:          shortcutKindOperation,
			OperationID:   "project.describe-projects",
			Summary:       "列出日志项目",
			Description:   "高频列举入口，支持分页和 --all 自动翻页。",
			InputMode:     "filters via flags",
			RequiredFlags: nil,
			Bindings: []shortcutBinding{
				shortcutBindingParam("PageNumber", "--page-number", "query", false, "integer", "页码"),
				shortcutBindingParam("PageSize", "--page-size", "query", false, "integer", "每页数量"),
				shortcutBindingParam("ProjectName", "--project-name", "query", false, "string", "项目名精确过滤"),
				shortcutBindingParam("ProjectId", "--project-id", "query", false, "string", "项目 ID"),
				shortcutBindingParam("all", "--all", "meta", false, "boolean", "自动翻完整分页"),
			},
			ResultTransforms: []shortcutResultTransform{{ID: shortcutTransformPageNumberList}},
			Notes: []string{
				"列表结果支持 --output table 和 --all。",
				"需要结构化契约时，先用 volclog tool list project，再用 volclog tool describe。",
			},
		},
		{
			Group:         "project",
			Command:       "get",
			Action:        "project.get",
			Kind:          shortcutKindOperation,
			OperationID:   "project.describe-project",
			Summary:       "查询单个日志项目",
			Description:   "按 ProjectId 获取项目详情。",
			InputMode:     "query via flags",
			RequiredFlags: []string{"--project-id"},
			Bindings: []shortcutBinding{
				shortcutBindingParam("ProjectId", "--project-id", "query", true, "string", "项目 ID"),
			},
		},
		{
			Group:         "project",
			Command:       "create",
			Action:        "project.create",
			Kind:          shortcutKindOperation,
			OperationID:   "project.create",
			Summary:       "创建日志项目",
			Description:   "支持少量字段直接走 flags，也支持完整 JSON 走 --request。",
			InputMode:     "body via --request; common fields via flags",
			RequiredFlags: []string{"--project-name or request.ProjectName"},
			Bindings: []shortcutBinding{
				shortcutBindingParam("ProjectName", "--project-name", "body", true, "string", "项目名称；也可放到 --request JSON"),
				shortcutBindingParam("Description", "--description", "body", false, "string", "项目描述"),
				shortcutBindingParam("IamProjectName", "--iam-project-name", "body", false, "string", "IAM 项目名"),
				shortcutBindingParam("Region", "--region", "body", false, "string", "区域；缺省时使用当前 profile.region"),
				shortcutBindingParam("Tags", "--tags", "body", false, "array", "JSON 数组、file://... 或裸文件路径"),
				shortcutBindingParam("request", "--request", "body", false, "json", "inline JSON、file://...、- 或裸文件路径"),
			},
			Defaults: []shortcutDefault{
				{ID: shortcutDefaultProfileRegion, Binding: "Region", Source: "profile.region"},
			},
			Presentation: shortcutPresentation{SupportsTemplate: true},
			Notes: []string{
				"当字段较多时，优先直接通过 --request file://req.json 组织完整 JSON。",
				"如果未显式传 Region，会回落到当前 profile 的 region。",
			},
		},
		{
			Group:         "project",
			Command:       "modify",
			Action:        "project.modify",
			Kind:          shortcutKindOperation,
			OperationID:   "project.modify",
			Summary:       "修改日志项目",
			Description:   "需要 ProjectId；其余字段可通过 flags 或 --request 提供。",
			InputMode:     "body via --request; common fields via flags",
			RequiredFlags: []string{"--project-id"},
			Bindings: []shortcutBinding{
				shortcutBindingParam("ProjectId", "--project-id", "body", true, "string", "项目 ID"),
				shortcutBindingParam("ProjectName", "--project-name", "body", false, "string", "项目名称"),
				shortcutBindingParam("Description", "--description", "body", false, "string", "项目描述"),
				shortcutBindingParam("request", "--request", "body", false, "json", "完整请求 JSON"),
			},
			Presentation: shortcutPresentation{SupportsTemplate: true},
		},
		{
			Group:         "project",
			Command:       "delete",
			Action:        "project.delete",
			Kind:          shortcutKindOperation,
			OperationID:   "project.delete",
			Summary:       "删除日志项目",
			Description:   "按 ProjectId 删除项目。",
			InputMode:     "body synthesized from flags",
			RequiredFlags: []string{"--project-id"},
			Bindings: []shortcutBinding{
				shortcutBindingParam("ProjectId", "--project-id", "body", true, "string", "项目 ID"),
			},
		},
		{
			Group:       "topic",
			Command:     "list",
			Action:      "topic.list",
			Kind:        shortcutKindOperation,
			OperationID: "topic.describe-topics",
			Summary:     "列出日志主题",
			Description: "支持 ProjectId/TopicId/TopicName 过滤和 --all 自动翻页。",
			InputMode:   "filters via flags",
			Bindings: []shortcutBinding{
				shortcutBindingParam("ProjectId", "--project-id", "query", false, "string", "所属项目 ID"),
				shortcutBindingParam("ProjectName", "--project-name", "query", false, "string", "所属项目名"),
				shortcutBindingParam("TopicId", "--topic-id", "query", false, "string", "主题 ID"),
				shortcutBindingParam("TopicName", "--topic-name", "query", false, "string", "主题名"),
				shortcutBindingParam("Region", "--region", "query", false, "string", "多 Region 查询候选 Region"),
				shortcutBindingParam("FuzzySearchKey", "--fuzzy-search-key", "query", false, "string", "模糊搜索关键词"),
				shortcutBindingParam("Description", "--description", "query", false, "string", "主题描述关键词"),
				shortcutBindingParam("Tags", "--tags", "query", false, "string", "标签过滤 JSON 或文件"),
				shortcutBindingParam("IsFullName", "--is-full-name/--no-is-full-name", "query", false, "boolean", "是否精确匹配主题名"),
				shortcutBindingParam("Favourite", "--favourite/--no-favourite", "query", false, "boolean", "是否收藏"),
				shortcutBindingParam("OrderByProject", "--order-by-project/--no-order-by-project", "query", false, "boolean", "是否按项目排序分页"),
				shortcutBindingParam("PageSize", "--page-size", "query", false, "integer", "每页数量"),
				shortcutBindingParam("PageNumber", "--page-number", "query", false, "integer", "页码"),
				shortcutBindingParam("all", "--all", "meta", false, "boolean", "自动翻完整分页"),
			},
			Validators: []shortcutValidator{
				{ID: shortcutValidatorTopicNameID, Bindings: []string{"TopicName", "TopicId"}},
			},
			ResultTransforms: []shortcutResultTransform{{ID: shortcutTransformPageNumberList}},
			Notes: []string{
				"TopicName 和 TopicId 不应同时提供。",
				"列表结果支持 --output table 和 --all。",
			},
		},
		{
			Group:         "topic",
			Command:       "get",
			Action:        "topic.get",
			Kind:          shortcutKindOperation,
			OperationID:   "topic.describe-topic",
			Summary:       "查询单个日志主题",
			Description:   "按 TopicId 获取主题详情。",
			InputMode:     "query via flags",
			RequiredFlags: []string{"--topic-id"},
			Bindings: []shortcutBinding{
				shortcutBindingParam("TopicId", "--topic-id", "query", true, "string", "主题 ID"),
			},
		},
		{
			Group:         "topic",
			Command:       "create",
			Action:        "topic.create",
			Kind:          shortcutKindOperation,
			OperationID:   "topic.create",
			Summary:       "创建日志主题",
			Description:   "支持常用 flags 快速创建，也支持完整 JSON 走 --request。",
			InputMode:     "body via --request; common fields via flags",
			RequiredFlags: []string{"--project-id, --topic-name, --ttl and --shard-count (or request.ProjectId/request.TopicName/request.Ttl/request.ShardCount)"},
			Bindings: []shortcutBinding{
				shortcutBindingParam("ProjectId", "--project-id", "body", true, "string", "所属项目 ID"),
				shortcutBindingParam("TopicName", "--topic-name", "body", true, "string", "主题名"),
				shortcutBindingParam("Ttl", "--ttl", "body", true, "integer", "日志总保存时间，单位天"),
				shortcutBindingParam("ShardCount", "--shard-count", "body", true, "integer", "分区数量"),
				shortcutBindingParam("Description", "--description", "body", false, "string", "主题描述"),
				shortcutBindingParam("AutoSplit", "--auto-split", "body", false, "boolean", "开启自动分裂"),
				shortcutBindingParam("MaxSplitShard", "--max-split-shard", "body", false, "integer", "自动分裂最大 shard"),
				shortcutBindingParam("EnableTracking", "--enable-tracking", "body", false, "boolean", "开启 WebTracking"),
				shortcutBindingParam("EnableTracking", "--disable-tracking", "body", false, "boolean", "关闭 WebTracking"),
				shortcutBindingParam("MeteringMode", "--metering-mode", "body", false, "string", "计费模式"),
				shortcutBindingParam("LogPublicIP", "--log-public-ip", "body", false, "boolean", "开启记录来源 IP"),
				shortcutBindingParam("LogPublicIP", "--no-log-public-ip", "body", false, "boolean", "关闭记录来源 IP"),
				shortcutBindingParam("EnableHotTtl", "--enable-hot-ttl", "body", false, "boolean", "开启分层存储"),
				shortcutBindingParam("EnableHotTtl", "--disable-hot-ttl", "body", false, "boolean", "关闭分层存储"),
				shortcutBindingParam("HotTtl", "--hot-ttl", "body", false, "integer", "标准存储时长"),
				shortcutBindingParam("ColdTtl", "--cold-ttl", "body", false, "integer", "低频存储时长"),
				shortcutBindingParam("ArchiveTtl", "--archive-ttl", "body", false, "integer", "归档存储时长"),
				shortcutPassthroughBindingParam("TimeKey", "--time-key", "body", false, "string", "日志时间字段名"),
				shortcutPassthroughBindingParam("TimeFormat", "--time-format", "body", false, "string", "日志时间字段格式"),
				shortcutBindingParam("EncryptConf", "--encrypt-conf", "body", false, "json", "数据加密配置 JSON"),
				shortcutBindingParam("Tags", "--tags", "body", false, "json", "标签数组 JSON"),
				shortcutBindingParam("request", "--request", "body", false, "json", "完整请求 JSON"),
			},
			Defaults: []shortcutDefault{
				{ID: shortcutDefaultCreateTTL30, Binding: "Ttl", Source: "constant", Value: 30},
				{ID: shortcutDefaultCreateShardCount2, Binding: "ShardCount", Source: "constant", Value: 2},
			},
			Validators: []shortcutValidator{
				{ID: shortcutValidatorTimeKeyFormatPair, Bindings: []string{"TimeKey", "TimeFormat"}},
				{ID: shortcutValidatorAutoSplitMax, Bindings: []string{"AutoSplit", "MaxSplitShard"}},
				{ID: shortcutValidatorHotTTLSum, Bindings: []string{"EnableHotTtl", "Ttl", "HotTtl", "ColdTtl", "ArchiveTtl"}},
			},
			Presentation: shortcutPresentation{SupportsTemplate: true},
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
			Kind:          shortcutKindOperation,
			OperationID:   "topic.modify",
			Summary:       "修改日志主题",
			Description:   "需要 TopicId；其余字段可通过 flags 或 --request 提供。",
			InputMode:     "body via --request; common fields via flags",
			RequiredFlags: []string{"--topic-id"},
			Bindings: []shortcutBinding{
				shortcutBindingParam("TopicId", "--topic-id", "body", true, "string", "主题 ID"),
				shortcutBindingParam("TopicName", "--topic-name", "body", false, "string", "主题名"),
				shortcutBindingParam("Description", "--description", "body", false, "string", "主题描述"),
				shortcutBindingParam("Ttl", "--ttl", "body", false, "integer", "保存天数"),
				shortcutBindingParam("AutoSplit", "--auto-split/--no-auto-split", "body", false, "boolean", "设置自动分裂"),
				shortcutHiddenBindingParam("MaxSplitShard", "--max-split-shard", "body", false, "integer", "自动分裂最大 shard"),
				shortcutHiddenBindingParam("EnableTracking", "--enable-tracking/--disable-tracking", "body", false, "boolean", "设置 WebTracking"),
				shortcutHiddenBindingParam("Favourite", "--favourite/--no-favourite", "body", false, "boolean", "设置收藏状态"),
				shortcutHiddenBindingParam("MeteringMode", "--metering-mode", "body", false, "string", "计费模式"),
				shortcutHiddenBindingParam("LogPublicIP", "--log-public-ip/--no-log-public-ip", "body", false, "boolean", "设置记录来源 IP"),
				shortcutHiddenBindingParam("EnableHotTtl", "--enable-hot-ttl/--disable-hot-ttl", "body", false, "boolean", "设置分层存储"),
				shortcutHiddenBindingParam("HotTtl", "--hot-ttl", "body", false, "integer", "标准存储时长"),
				shortcutHiddenBindingParam("ColdTtl", "--cold-ttl", "body", false, "integer", "低频存储时长"),
				shortcutHiddenBindingParam("ArchiveTtl", "--archive-ttl", "body", false, "integer", "归档存储时长"),
				shortcutHiddenBindingParam("TimeKey", "--time-key", "body", false, "string", "日志时间字段名"),
				shortcutHiddenBindingParam("TimeFormat", "--time-format", "body", false, "string", "日志时间字段格式"),
				shortcutHiddenBindingParam("EncryptConf", "--encrypt-conf", "body", false, "json", "数据加密配置 JSON"),
				shortcutBindingParam("request", "--request", "body", false, "json", "完整请求 JSON"),
			},
			Validators: []shortcutValidator{
				{ID: shortcutValidatorTimeKeyFormatPair, Bindings: []string{"TimeKey", "TimeFormat"}},
				{ID: shortcutValidatorAutoSplitMax, Bindings: []string{"AutoSplit", "MaxSplitShard"}},
				{ID: shortcutValidatorHotTTLSum, Bindings: []string{"EnableHotTtl", "Ttl", "HotTtl", "ColdTtl", "ArchiveTtl"}},
			},
			Presentation: shortcutPresentation{SupportsTemplate: true},
		},
		{
			Group:         "topic",
			Command:       "delete",
			Action:        "topic.delete",
			Kind:          shortcutKindOperation,
			OperationID:   "topic.delete",
			Summary:       "删除日志主题",
			Description:   "按 TopicId 删除主题。",
			InputMode:     "body synthesized from flags",
			RequiredFlags: []string{"--topic-id"},
			Bindings: []shortcutBinding{
				shortcutBindingParam("TopicId", "--topic-id", "body", true, "string", "主题 ID"),
			},
		},
		{
			Group:       "metric-topic",
			Command:     "list",
			Action:      "metric-topic.list",
			Kind:        shortcutKindOperation,
			OperationID: "metric-topic.describe-metric-topics",
			Summary:     "列出指标主题",
			Description: "支持 ProjectId/TopicId/TopicName 过滤和 --all 自动翻页。",
			InputMode:   "filters via flags",
			Bindings: []shortcutBinding{
				shortcutBindingParam("ProjectId", "--project-id", "query", false, "string", "所属项目 ID"),
				shortcutBindingParam("ProjectName", "--project-name", "query", false, "string", "所属项目名"),
				shortcutBindingParam("TopicId", "--topic-id", "query", false, "string", "主题 ID"),
				shortcutBindingParam("TopicName", "--topic-name", "query", false, "string", "主题名"),
				shortcutBindingParam("Region", "--region", "query", false, "string", "多 Region 查询候选 Region"),
				shortcutBindingParam("FuzzySearchKey", "--fuzzy-search-key", "query", false, "string", "模糊搜索关键词"),
				shortcutBindingParam("Description", "--description", "query", false, "string", "主题描述关键词"),
				shortcutBindingParam("Tags", "--tags", "query", false, "string", "标签过滤 JSON 或文件"),
				shortcutBindingParam("IsFullName", "--is-full-name/--no-is-full-name", "query", false, "boolean", "是否精确匹配主题名"),
				shortcutBindingParam("Favourite", "--favourite/--no-favourite", "query", false, "boolean", "是否收藏"),
				shortcutBindingParam("OrderByProject", "--order-by-project/--no-order-by-project", "query", false, "boolean", "是否按项目排序分页"),
				shortcutBindingParam("PageSize", "--page-size", "query", false, "integer", "每页数量"),
				shortcutBindingParam("PageNumber", "--page-number", "query", false, "integer", "页码"),
				shortcutBindingParam("all", "--all", "meta", false, "boolean", "自动翻完整分页"),
			},
			Validators: []shortcutValidator{
				{ID: shortcutValidatorTopicNameID, Bindings: []string{"TopicName", "TopicId"}},
			},
			ResultTransforms: []shortcutResultTransform{{ID: shortcutTransformPageNumberList}},
			Notes: []string{
				"列表结果支持 --output table 和 --all。",
			},
		},
		{
			Group:         "metric-topic",
			Command:       "get",
			Action:        "metric-topic.get",
			Kind:          shortcutKindOperation,
			OperationID:   "metric-topic.describe-metric-topic",
			Summary:       "查询单个指标主题",
			Description:   "按 TopicId 获取指标主题详情。",
			InputMode:     "query via flags",
			RequiredFlags: []string{"--topic-id"},
			Bindings: []shortcutBinding{
				shortcutBindingParam("TopicId", "--topic-id", "query", true, "string", "主题 ID"),
			},
		},
		{
			Group:         "metric-topic",
			Command:       "create",
			Action:        "metric-topic.create",
			Kind:          shortcutKindOperation,
			OperationID:   "metric-topic.create",
			Summary:       "创建指标主题",
			Description:   "支持常用 flags 快速创建，也支持完整 JSON 走 --request。",
			InputMode:     "body via --request; common fields via flags",
			RequiredFlags: []string{"--project-id and --topic-name (or request.ProjectId/request.TopicName)"},
			Bindings: []shortcutBinding{
				shortcutBindingParam("ProjectId", "--project-id", "body", true, "string", "所属项目 ID"),
				shortcutBindingParam("TopicName", "--topic-name", "body", true, "string", "主题名"),
				shortcutBindingParam("Description", "--description", "body", false, "string", "主题描述"),
				shortcutBindingParam("Ttl", "--ttl", "body", false, "integer", "保存天数"),
				shortcutBindingParam("ShardCount", "--shard-count", "body", false, "integer", "Shard 数量"),
				shortcutBindingParam("AutoSplit", "--auto-split", "body", false, "boolean", "开启自动分裂"),
				shortcutHiddenBindingParam("MaxSplitShard", "--max-split-shard", "body", false, "integer", "自动分裂最大 shard"),
				shortcutHiddenBindingParam("Tags", "--tags", "body", false, "array", "标签数组 JSON"),
				shortcutBindingParam("request", "--request", "body", false, "json", "完整请求 JSON"),
			},
			Defaults: []shortcutDefault{
				{ID: shortcutDefaultCreateTTL30, Binding: "Ttl", Source: "constant", Value: 30},
				{ID: shortcutDefaultCreateShardCount2, Binding: "ShardCount", Source: "constant", Value: 2},
			},
			Validators: []shortcutValidator{
				{ID: shortcutValidatorAutoSplitMax, Bindings: []string{"AutoSplit", "MaxSplitShard"}},
			},
			Presentation: shortcutPresentation{SupportsTemplate: true},
		},
		{
			Group:         "metric-topic",
			Command:       "modify",
			Action:        "metric-topic.modify",
			Kind:          shortcutKindOperation,
			OperationID:   "metric-topic.modify",
			Summary:       "修改指标主题",
			Description:   "需要 TopicId；其余字段可通过 flags 或 --request 提供。",
			InputMode:     "body via --request; common fields via flags",
			RequiredFlags: []string{"--topic-id"},
			Bindings: []shortcutBinding{
				shortcutBindingParam("TopicId", "--topic-id", "body", true, "string", "主题 ID"),
				shortcutBindingParam("TopicName", "--topic-name", "body", false, "string", "主题名"),
				shortcutBindingParam("Description", "--description/--clear-description", "body", false, "string", "主题描述"),
				shortcutBindingParam("Ttl", "--ttl", "body", false, "integer", "保存天数"),
				shortcutHiddenBindingParam("Favourite", "--favourite/--no-favourite", "body", false, "boolean", "设置收藏状态"),
				shortcutBindingParam("AutoSplit", "--auto-split/--no-auto-split", "body", false, "boolean", "设置自动分裂"),
				shortcutHiddenBindingParam("MaxSplitShard", "--max-split-shard", "body", false, "integer", "自动分裂最大 shard"),
				shortcutBindingParam("request", "--request", "body", false, "json", "完整请求 JSON"),
			},
			Validators: []shortcutValidator{
				{ID: shortcutValidatorClearDescription, Bindings: []string{"Description"}},
				{ID: shortcutValidatorAutoSplitMax, Bindings: []string{"AutoSplit", "MaxSplitShard"}},
			},
			Presentation: shortcutPresentation{SupportsTemplate: true},
		},
		{
			Group:         "metric-topic",
			Command:       "delete",
			Action:        "metric-topic.delete",
			Kind:          shortcutKindOperation,
			OperationID:   "metric-topic.delete",
			Summary:       "删除指标主题",
			Description:   "按 TopicId 删除指标主题。",
			InputMode:     "body synthesized from flags",
			RequiredFlags: []string{"--topic-id"},
			Bindings: []shortcutBinding{
				shortcutBindingParam("TopicId", "--topic-id", "body", true, "string", "主题 ID"),
			},
		},
		{
			Group:                  "metric-topic",
			Command:                "search",
			Action:                 "metric-topic.search",
			Kind:                   shortcutKindOperation,
			OperationID:            "log.search",
			Summary:                "在指标主题上执行查询",
			Description:            "复用 SearchLogs 请求体，常见于 SQL/PromQL 查询。",
			InputMode:              "body via --request; common fields via flags",
			PreferredOutputMode:    "file",
			RecommendedGlobalFlags: []string{"--output-mode file"},
			RequiredFlags:          []string{"--topic-id and --query and --from and --to (or request fields)"},
			Bindings: []shortcutBinding{
				shortcutBindingParam("TopicId", "--topic-id", "body", true, "string", "主题 ID"),
				shortcutBindingParam("Query", "--query", "body", true, "string", "查询语句"),
				shortcutBindingParam("StartTime", "--from", "body", true, "integer", "起始毫秒时间戳"),
				shortcutBindingParam("EndTime", "--to", "body", true, "integer", "结束毫秒时间戳"),
				shortcutHiddenBindingParam("Limit", "--limit", "body", false, "integer", "返回条数"),
				shortcutBindingParam("Context", "--context", "body", false, "string", "普通检索翻页上下文"),
				shortcutBindingParam("HighLight", "--highlight", "body", false, "boolean", "高亮命中内容"),
				shortcutBindingParam("AccurateQuery", "--accurate-query/--no-accurate-query", "body", false, "boolean", "精确查询"),
				shortcutBindingParam("MustComplete", "--must-complete/--no-must-complete", "body", false, "boolean", "要求查询完整返回"),
				shortcutBindingParam("Offset", "--offset", "body", false, "integer", "普通检索偏移量"),
				shortcutBindingParam("request", "--request", "body", false, "json", "完整请求 JSON"),
			},
			Defaults: []shortcutDefault{
				{ID: shortcutDefaultSearchLimit100, Binding: "Limit", Source: "constant", Value: 100},
			},
			Validators: []shortcutValidator{
				{ID: shortcutValidatorAnalysisFlags, Bindings: []string{"Query", "Limit", "request"}},
			},
			Presentation: shortcutPresentation{SupportsTemplate: true},
			Notes: []string{
				"未显式指定 --limit 时，导出默认批次大于普通 search；需要更大/更小批次时自行覆盖。",
			},
		},
		{
			Group:         "index",
			Command:       "get",
			Action:        "index.get",
			Kind:          shortcutKindOperation,
			OperationID:   "index.describe",
			Summary:       "查询索引配置",
			Description:   "按 TopicId 获取索引详情。",
			InputMode:     "query via flags",
			RequiredFlags: []string{"--topic-id"},
			Bindings: []shortcutBinding{
				shortcutBindingParam("TopicId", "--topic-id", "query", true, "string", "主题 ID"),
			},
		},
		{
			Group:         "index",
			Command:       "create",
			Action:        "index.create",
			Kind:          shortcutKindOperation,
			OperationID:   "index.create",
			Summary:       "创建索引",
			Description:   "索引 body 建议先用模板生成，再按 TopicId 提交。",
			InputMode:     "body via --request/--body; topic via flag",
			RequiredFlags: []string{"--topic-id", "--request or --body"},
			Bindings: []shortcutBinding{
				shortcutBindingParamWithPresentationLocation("TopicId", "--topic-id", "body", "query", true, "string", "主题 ID"),
				shortcutBindingParam("request", "--request/--body", "body", true, "json", "索引 JSON；支持 inline、file://...、- 或裸文件路径"),
			},
			Validators: []shortcutValidator{
				{ID: shortcutValidatorIndexBody, Bindings: []string{"TopicId", "request"}},
			},
			Presentation: shortcutPresentation{
				SupportsTemplate: true,
				TemplateOmit:     []string{"TopicId"},
			},
			Notes: []string{
				"模板会省略 TopicId，因为该值由 --topic-id 单独提供。",
			},
		},
		{
			Group:         "index",
			Command:       "modify",
			Action:        "index.modify",
			Kind:          shortcutKindOperation,
			OperationID:   "index.modify",
			Summary:       "修改索引",
			Description:   "索引 body 建议先用模板生成，再按 TopicId 提交。",
			InputMode:     "body via --request/--body; topic via flag",
			RequiredFlags: []string{"--topic-id", "--request or --body"},
			Bindings: []shortcutBinding{
				shortcutBindingParamWithPresentationLocation("TopicId", "--topic-id", "body", "query", true, "string", "主题 ID"),
				shortcutBindingParam("request", "--request/--body", "body", true, "json", "索引 JSON；支持 inline、file://...、- 或裸文件路径"),
			},
			Validators: []shortcutValidator{
				{ID: shortcutValidatorIndexBody, Bindings: []string{"TopicId", "request"}},
			},
			Presentation: shortcutPresentation{
				SupportsTemplate: true,
				TemplateOmit:     []string{"TopicId"},
			},
		},
		{
			Group:                  "log",
			Command:                "search",
			Action:                 "log.search",
			Kind:                   shortcutKindOperation,
			OperationID:            "log.search",
			Summary:                "执行日志检索",
			Description:            "复用 SearchLogs 请求体；既支持普通检索，也支持 SQL/分析语法。适合交互式分析和少量结果预览。",
			InputMode:              "body via --request; common fields via flags",
			PreferredOutputMode:    "file",
			RecommendedGlobalFlags: []string{"--output-mode file"},
			RequiredFlags:          []string{"--topic-id and --query and --from and --to (or request fields)"},
			Bindings: []shortcutBinding{
				shortcutBindingParam("TopicId", "--topic-id", "body", true, "string", "主题 ID"),
				shortcutBindingParam("Query", "--query", "body", true, "string", "查询语句"),
				shortcutBindingParam("StartTime", "--from", "body", true, "integer", "起始毫秒时间戳"),
				shortcutBindingParam("EndTime", "--to", "body", true, "integer", "结束毫秒时间戳"),
				shortcutBindingParam("Limit", "--limit", "body", false, "integer", "返回条数"),
				shortcutBindingParam("Context", "--context", "body", false, "string", "普通检索翻页上下文"),
				shortcutBindingParam("Sort", "--sort", "body", false, "string", "asc/desc"),
				shortcutBindingParam("HighLight", "--highlight", "body", false, "boolean", "高亮命中内容"),
				shortcutBindingParam("AccurateQuery", "--accurate-query/--no-accurate-query", "body", false, "boolean", "精确查询"),
				shortcutBindingParam("MustComplete", "--must-complete/--no-must-complete", "body", false, "boolean", "要求查询完整返回"),
				shortcutBindingParam("Offset", "--offset", "body", false, "integer", "普通检索偏移量"),
				shortcutBindingParam("request", "--request", "body", false, "json", "完整请求 JSON"),
			},
			Defaults: []shortcutDefault{
				{ID: shortcutDefaultSearchLimit100, Binding: "Limit", Source: "constant", Value: 100},
			},
			Validators: []shortcutValidator{
				{ID: shortcutValidatorAnalysisFlags, Bindings: []string{"Query", "Limit", "Sort", "request"}},
			},
			Presentation: shortcutPresentation{SupportsTemplate: true},
			Notes: []string{
				"分析查询（Query 中带 |select ...）与普通检索的分页语义不同。",
				"交互式分析、验证语句、少量结果预览优先使用 log.search；大量分析结果导出再切换到 export-analysis。",
			},
		},
		{
			Group:                  "log",
			Command:                "histogram",
			Action:                 "log.histogram",
			Kind:                   shortcutKindOperation,
			OperationID:            "log.describe-histogram-v1",
			Summary:                "查询日志直方图",
			Description:            "复用 DescribeHistogramV1 请求体；适合先看时间分布再决定检索窗口。",
			InputMode:              "body via --request; common fields via flags",
			PreferredOutputMode:    "file",
			RecommendedGlobalFlags: []string{"--output-mode file"},
			RequiredFlags:          []string{"--topic-id and --query and --from and --to (or request fields)"},
			Bindings: []shortcutBinding{
				shortcutBindingParam("TopicId", "--topic-id", "body", true, "string", "主题 ID"),
				shortcutBindingParam("Query", "--query", "body", true, "string", "查询语句"),
				shortcutBindingParam("StartTime", "--from", "body", true, "integer", "起始毫秒时间戳"),
				shortcutBindingParam("EndTime", "--to", "body", true, "integer", "结束毫秒时间戳"),
				shortcutBindingParam("Interval", "--interval", "body", false, "integer", "直方图桶宽"),
				shortcutBindingParam("request", "--request", "body", false, "json", "完整请求 JSON"),
			},
			Presentation: shortcutPresentation{SupportsTemplate: true},
		},
		{
			Group:               "log",
			Command:             "context",
			Action:              "log.context",
			Kind:                shortcutKindOperation,
			OperationID:         "log.describe-log-context",
			Summary:             "查看命中日志上下文",
			Description:         "复用 DescribeLogContext 请求体；适合基于 SearchLogs 命中结果查看前后文。",
			InputMode:           "body via --request; common fields via flags",
			RequiredFlags:       []string{"--topic-id and --context-flow and --package-offset and --source (or request fields)"},
			PreferredOutputMode: "file",
			Bindings: []shortcutBinding{
				shortcutBindingParam("TopicId", "--topic-id", "body", true, "string", "主题 ID"),
				shortcutBindingParam("ContextFlow", "--context-flow", "body", true, "string", "日志所在 LogGroup 的 ContextFlow"),
				shortcutBindingParam("PackageOffset", "--package-offset", "body", true, "integer", "日志在 LogGroup 中的序号"),
				shortcutBindingParam("Source", "--source", "body", true, "string", "日志来源 IP"),
				shortcutBindingParam("PrevLogs", "--prev-logs", "body", false, "integer", "向前查看条数"),
				shortcutBindingParam("NextLogs", "--next-logs", "body", false, "integer", "向后查看条数"),
				shortcutBindingParam("request", "--request", "body", false, "json", "完整请求 JSON"),
			},
			Presentation: shortcutPresentation{SupportsTemplate: true},
			Notes: []string{
				"ContextFlow、PackageOffset、Source 一般来自 SearchLogs 命中日志对象。",
			},
		},
		{
			Group:         "log",
			Command:       "put",
			Action:        "log.put",
			Kind:          shortcutKindOperation,
			OperationID:   "log.put",
			Summary:       "写入日志",
			Description:   "复用 PutLogs 特殊 IO；支持 JSON 和 JSONL 输入，再由 CLI 编码为 protobuf。",
			InputMode:     "special io via --request and optional --request-format json|jsonl",
			RequiredFlags: []string{"--topic-id", "--request"},
			Bindings: []shortcutBinding{
				shortcutBindingParam("TopicId", "--topic-id", "query", true, "string", "主题 ID"),
				shortcutBindingParam("request", "--request", "body", true, "json/jsonl", "LogGroupList JSON 或 JSONL 日志行"),
				shortcutBindingParam("requestFormat", "--request-format", "meta", false, "string", "json 或 jsonl"),
				shortcutBindingParam("x-tls-compresstype", "--compress-type", "header", false, "string", "lz4/zlib/none"),
				shortcutBindingParam("x-tls-hashkey", "--hash-key", "header", false, "string", "指定写入分区的 HashKey"),
				shortcutBindingParam("Content-MD5", "--content-md5", "header", false, "string", "请求体 MD5"),
			},
			Presentation: shortcutPresentation{SupportsTemplate: true},
			Notes: []string{
				"CLI 会自动把 JSON/JSONL 编码为 PutLogs 所需 protobuf body。",
				"Logs[].Time 必须是 Unix 毫秒时间戳，不是秒级。",
			},
		},
		{
			Group:       "log",
			Command:     "ingest",
			Action:      "log.ingest",
			Kind:        shortcutKindWorkflow,
			WorkflowID:  "log.ingest",
			Summary:     "批量导入文本或 JSON 日志",
			Description: "面向高层写入场景；CLI 负责补时间、组批、统计头和 protobuf 编码。",
			InputMode:   "ingest via --input; lines/jsonl/json-array normalized by CLI before PutLogs",
			RequiredFlags: []string{
				"--topic-id",
				"--input",
			},
			Bindings: []shortcutBinding{
				shortcutBindingParam("TopicId", "--topic-id", "query", true, "string", "主题 ID"),
				shortcutBindingParam("input", "--input", "meta", true, "path|-", "输入内容；支持 file://...、-、裸文件路径"),
				shortcutBindingParam("inputFormat", "--input-format", "meta", false, "string", "lines/jsonl/json-array"),
				shortcutBindingParam("timeField", "--time-field", "meta", false, "string", "jsonl/json-array 的时间字段名"),
				shortcutBindingParam("timeFormat", "--time-format", "meta", false, "string", "auto/unix_ms/unix/rfc3339"),
				shortcutBindingParam("Source", "--source", "group", false, "string", "本次写入共用 Source"),
				shortcutBindingParam("FileName", "--file-name", "group", false, "string", "本次写入共用 FileName"),
				shortcutBindingParam("LogTags", "--tag", "group", false, "string", "重复传入 k=v 形式的 LogTag"),
				shortcutBindingParam("batchMaxCount", "--batch-max-count", "meta", false, "integer", "每批最大发送条数，默认 500"),
				shortcutBindingParam("x-tls-compresstype", "--compress-type", "header", false, "string", "lz4/zlib/none，默认 lz4"),
				shortcutBindingParam("x-tls-hashkey", "--hash-key", "header", false, "string", "指定写入分区的 HashKey"),
			},
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
			Kind:                   shortcutKindWorkflow,
			WorkflowID:             "log.export",
			Summary:                "自动翻页导出日志",
			Description:            "面向纯检索结果导出；分析语句请改用 export-analysis。",
			InputMode:              "body via --request; auto-pagination in command",
			PreferredOutputMode:    "file",
			RecommendedGlobalFlags: []string{"--output jsonl", "--output-mode file"},
			RequiredFlags:          []string{"--topic-id and --query and --from and --to (or request fields)"},
			Bindings: []shortcutBinding{
				shortcutBindingParam("TopicId", "--topic-id", "body", true, "string", "主题 ID"),
				shortcutBindingParam("Query", "--query", "body", true, "string", "非分析查询语句"),
				shortcutBindingParam("StartTime", "--from", "body", true, "integer", "起始毫秒时间戳"),
				shortcutBindingParam("EndTime", "--to", "body", true, "integer", "结束毫秒时间戳"),
				shortcutHiddenBindingParam("Limit", "--limit", "body", false, "integer", "每批返回条数"),
				shortcutBindingParam("maxPages", "--max-pages", "meta", false, "integer", "最多翻页数量"),
				shortcutBindingParam("request", "--request", "body", false, "json", "完整请求 JSON"),
			},
			Defaults: []shortcutDefault{
				{ID: shortcutDefaultExportLimit500, Binding: "Limit", Source: "constant", Value: 500},
				{ID: shortcutDefaultExportMaxPages100, Binding: "maxPages", Source: "constant", Value: 100},
			},
			Validators: []shortcutValidator{
				{ID: shortcutValidatorPureSearch, Bindings: []string{"Query", "request"}},
			},
			Presentation: shortcutPresentation{SupportsTemplate: true},
		},
		{
			Group:                  "log",
			Command:                "export-analysis",
			Action:                 "log.export-analysis",
			Kind:                   shortcutKindWorkflow,
			WorkflowID:             "log.export-analysis",
			Summary:                "导出分析行结果",
			Description:            "面向大量 SQL/分析结果导出；交互式分析和少量结果优先用 log.search。",
			InputMode:              "body via --request; analysis mode",
			PreferredOutputMode:    "file",
			RecommendedGlobalFlags: []string{"--output jsonl", "--output-mode file"},
			RequiredFlags:          []string{"--topic-id and analysis query and --from and --to (or request fields)"},
			Bindings: []shortcutBinding{
				shortcutBindingParam("TopicId", "--topic-id", "body", true, "string", "主题 ID"),
				shortcutBindingParam("Query", "--query", "body", true, "string", "分析查询语句，示例：*|select count(*)"),
				shortcutBindingParam("StartTime", "--from", "body", true, "integer", "起始毫秒时间戳"),
				shortcutBindingParam("EndTime", "--to", "body", true, "integer", "结束毫秒时间戳"),
				shortcutBindingParam("request", "--request", "body", false, "json", "完整请求 JSON"),
			},
			Validators: []shortcutValidator{
				{ID: shortcutValidatorAnalysisQuery, Bindings: []string{"Query", "request"}},
			},
			Presentation: shortcutPresentation{SupportsTemplate: true},
			Notes: []string{
				"同样基于 SearchLogs 的 SQL/分析语句，但定位是大结果导出；交互式探索和少量预览优先使用 log.search。",
				"分析可见列强依赖当前索引配置；新增或修改索引后通常只对增量写入生效，旧日志对应列可能仍为 null。",
			},
		},
		{
			Group:       "host-group",
			Command:     "list",
			Action:      "host-group.list",
			Kind:        shortcutKindOperation,
			OperationID: "host-group.describe-host-groups-v2",
			Summary:     "列出机器组",
			Description: "支持模糊过滤和 --all 自动翻页。",
			InputMode:   "filters via flags",
			Bindings: []shortcutBinding{
				shortcutBindingParam("HostGroupId", "--host-group-id", "query", false, "string", "机器组 ID 关键词"),
				shortcutBindingParam("HostGroupName", "--host-group-name", "query", false, "string", "机器组名称关键词"),
				shortcutBindingParam("HostIdentifier", "--host-identifier", "query", false, "string", "机器组标识"),
				shortcutBindingParam("IamProjectName", "--iam-project-name", "query", false, "string", "IAM 项目名"),
				shortcutBindingParam("PageNumber", "--page-number", "query", false, "integer", "页码"),
				shortcutBindingParam("PageSize", "--page-size", "query", false, "integer", "每页数量"),
				shortcutBindingParam("AutoUpdate", "--auto-update/--no-auto-update", "query", false, "boolean", "是否自动升级"),
				shortcutBindingParam("ServiceLogging", "--service-logging/--no-service-logging", "query", false, "boolean", "是否开启服务日志"),
				shortcutBindingParam("Hidden", "--hidden/--no-hidden", "query", false, "boolean", "是否隐藏专属资源机器组"),
				shortcutBindingParam("all", "--all", "meta", false, "boolean", "自动翻完整分页"),
			},
			ResultTransforms: []shortcutResultTransform{{ID: shortcutTransformPageNumberList}},
			Notes: []string{
				"返回层级通常较深，常用 JMESPath 入口是 HostGroupHostsRulesInfos[].HostGroupInfo。",
			},
		},
		{
			Group:         "host-group",
			Command:       "get",
			Action:        "host-group.get",
			Kind:          shortcutKindOperation,
			OperationID:   "host-group.describe-host-group-v2",
			Summary:       "查询单个机器组",
			Description:   "按 HostGroupId 获取机器组详情。",
			InputMode:     "query via flags",
			RequiredFlags: []string{"--host-group-id"},
			Bindings: []shortcutBinding{
				shortcutBindingParam("HostGroupId", "--host-group-id", "query", true, "string", "机器组 ID"),
			},
		},
		{
			Group:         "host-group",
			Command:       "create",
			Action:        "host-group.create",
			Kind:          shortcutKindOperation,
			OperationID:   "host-group.create",
			Summary:       "创建机器组",
			Description:   "支持常用 flags 快速创建，也支持完整 JSON 走 --request。",
			InputMode:     "body via --request; common fields via flags",
			RequiredFlags: []string{"--host-group-name and --host-group-type (or request fields)"},
			Bindings: []shortcutBinding{
				shortcutBindingParam("HostGroupName", "--host-group-name", "body", true, "string", "机器组名称"),
				shortcutBindingParam("HostGroupType", "--host-group-type", "body", true, "string", "IP 或 Label"),
				shortcutBindingParam("HostIpList", "--host-ip-list", "body", false, "array", "IP 列表文件或 JSON 数组"),
				shortcutBindingParam("HostIdentifier", "--host-identifier", "body", false, "string", "Label 机器标识"),
				shortcutBindingParam("AutoUpdate", "--auto-update/--no-auto-update", "body", false, "boolean", "自动升级"),
				shortcutBindingParam("UpdateStartTime", "--update-start-time", "body", false, "string", "升级开始时间"),
				shortcutBindingParam("UpdateEndTime", "--update-end-time", "body", false, "string", "升级结束时间"),
				shortcutBindingParam("ServiceLogging", "--service-logging/--no-service-logging", "body", false, "boolean", "服务日志"),
				shortcutBindingParam("IamProjectName", "--iam-project-name", "body", false, "string", "IAM 项目名"),
				shortcutBindingParam("request", "--request", "body", false, "json", "完整请求 JSON"),
			},
			Presentation: shortcutPresentation{SupportsTemplate: true},
		},
		{
			Group:         "host-group",
			Command:       "modify",
			Action:        "host-group.modify",
			Kind:          shortcutKindOperation,
			OperationID:   "host-group.modify-host-group",
			Summary:       "修改机器组",
			Description:   "需要 HostGroupId；其余字段可通过 flags 或 --request 提供。",
			InputMode:     "body via --request; common fields via flags",
			RequiredFlags: []string{"--host-group-id"},
			Bindings: []shortcutBinding{
				shortcutBindingParam("HostGroupId", "--host-group-id", "body", true, "string", "机器组 ID"),
				shortcutBindingParam("HostGroupName", "--host-group-name", "body", false, "string", "机器组名称"),
				shortcutBindingParam("HostGroupType", "--host-group-type", "body", false, "string", "IP 或 Label"),
				shortcutBindingParam("HostIpList", "--host-ip-list", "body", false, "array", "IP 列表文件或 JSON 数组"),
				shortcutBindingParam("HostIdentifier", "--host-identifier", "body", false, "string", "Label 机器标识"),
				shortcutBindingParam("AutoUpdate", "--auto-update/--no-auto-update", "body", false, "boolean", "自动升级"),
				shortcutBindingParam("UpdateStartTime", "--update-start-time", "body", false, "string", "升级开始时间"),
				shortcutBindingParam("UpdateEndTime", "--update-end-time", "body", false, "string", "升级结束时间"),
				shortcutBindingParam("ServiceLogging", "--service-logging/--no-service-logging", "body", false, "boolean", "服务日志"),
				shortcutPassthroughBindingParam("IamProjectName", "--iam-project-name", "body", false, "string", "IAM 项目名"),
				shortcutBindingParam("request", "--request", "body", false, "json", "完整请求 JSON"),
			},
			Presentation: shortcutPresentation{SupportsTemplate: true},
		},
		{
			Group:         "host-group",
			Command:       "delete",
			Action:        "host-group.delete",
			Kind:          shortcutKindOperation,
			OperationID:   "host-group.delete-host-group",
			Summary:       "删除机器组",
			Description:   "按 HostGroupId 删除机器组。",
			InputMode:     "body synthesized from flags",
			RequiredFlags: []string{"--host-group-id"},
			Bindings: []shortcutBinding{
				shortcutBindingParam("HostGroupId", "--host-group-id", "body", true, "string", "机器组 ID"),
			},
		},
		{
			Group:       "collector",
			Command:     "list",
			Action:      "collector.list",
			Kind:        shortcutKindOperation,
			OperationID: "collector.describe-rules-v2",
			Summary:     "列出采集规则",
			Description: "支持按项目、主题、规则名过滤和 --all 自动翻页。",
			InputMode:   "filters via flags",
			Bindings: []shortcutBinding{
				shortcutBindingParam("ProjectId", "--project-id", "query", false, "string", "项目 ID"),
				shortcutBindingParam("ProjectName", "--project-name", "query", false, "string", "项目名"),
				shortcutBindingParam("IamProjectName", "--iam-project-name", "query", false, "string", "IAM 项目名"),
				shortcutBindingParam("RuleId", "--rule-id", "query", false, "string", "规则 ID 关键词"),
				shortcutBindingParam("RuleName", "--rule-name", "query", false, "string", "规则名关键词"),
				shortcutBindingParam("TopicId", "--topic-id", "query", false, "string", "主题 ID"),
				shortcutBindingParam("TopicName", "--topic-name", "query", false, "string", "主题名"),
				shortcutBindingParam("LogType", "--log-type", "query", false, "string", "采集模式"),
				shortcutBindingParam("RuleType", "--rule-type", "query", false, "integer", "采集规则类型"),
				shortcutBindingParam("Pause", "--pause/--no-pause", "query", false, "integer", "暂停状态"),
				shortcutBindingParam("Hidden", "--hidden/--no-hidden", "query", false, "boolean", "是否包含隐藏规则"),
				shortcutBindingParam("PageNumber", "--page-number", "query", false, "integer", "页码"),
				shortcutBindingParam("PageSize", "--page-size", "query", false, "integer", "每页数量"),
				shortcutBindingParam("all", "--all", "meta", false, "boolean", "自动翻完整分页"),
			},
			ResultTransforms: []shortcutResultTransform{{ID: shortcutTransformPageNumberList}},
		},
		{
			Group:         "collector",
			Command:       "get",
			Action:        "collector.get",
			Kind:          shortcutKindOperation,
			OperationID:   "collector.describe-rule-v2",
			Summary:       "查询单个采集规则",
			Description:   "按 RuleId 获取采集规则详情。",
			InputMode:     "query via flags",
			RequiredFlags: []string{"--rule-id"},
			Bindings: []shortcutBinding{
				shortcutBindingParam("RuleId", "--rule-id", "query", true, "string", "采集规则 ID"),
			},
		},
		{
			Group:         "collector",
			Command:       "create",
			Action:        "collector.create",
			Kind:          shortcutKindOperation,
			OperationID:   "collector.create",
			Summary:       "创建采集规则",
			Description:   "支持常用 flags 快速创建，也支持完整 JSON 走 --request。",
			InputMode:     "body via --request; common fields via flags",
			RequiredFlags: []string{"--topic-id and --rule-name (or request fields)"},
			Bindings: []shortcutBinding{
				shortcutBindingParam("TopicId", "--topic-id", "body", true, "string", "主题 ID"),
				shortcutBindingParam("RuleName", "--rule-name", "body", true, "string", "规则名称"),
				shortcutBindingParam("Paths", "--paths", "body", false, "array", "路径列表文件或 JSON 数组"),
				shortcutBindingParam("LogType", "--log-type", "body", false, "string", "采集模式"),
				shortcutBindingParam("InputType", "--input-type", "body", false, "integer", "采集类型"),
				shortcutBindingParam("Pause", "--pause/--no-pause", "body", false, "integer", "是否暂停"),
				shortcutBindingParam("request", "--request", "body", false, "json", "完整请求 JSON"),
			},
			Presentation: shortcutPresentation{SupportsTemplate: true},
			Notes: []string{
				"复杂规则体优先用模板配合 --request file://req.json。",
			},
		},
		{
			Group:         "collector",
			Command:       "modify",
			Action:        "collector.modify",
			Kind:          shortcutKindOperation,
			OperationID:   "collector.modify-rule",
			Summary:       "修改采集规则",
			Description:   "需要 RuleId；其余字段可通过 flags 或 --request 提供。",
			InputMode:     "body via --request; common fields via flags",
			RequiredFlags: []string{"--rule-id"},
			Bindings: []shortcutBinding{
				shortcutBindingParam("RuleId", "--rule-id", "body", true, "string", "采集规则 ID"),
				shortcutPassthroughBindingParam("TopicId", "--topic-id", "body", false, "string", "主题 ID"),
				shortcutBindingParam("RuleName", "--rule-name", "body", false, "string", "规则名称"),
				shortcutBindingParam("Paths", "--paths", "body", false, "array", "路径列表文件或 JSON 数组"),
				shortcutBindingParam("LogType", "--log-type", "body", false, "string", "采集模式"),
				shortcutBindingParam("InputType", "--input-type", "body", false, "integer", "采集类型"),
				shortcutBindingParam("Pause", "--pause/--no-pause", "body", false, "integer", "是否暂停"),
				shortcutBindingParam("request", "--request", "body", false, "json", "完整请求 JSON"),
			},
			Presentation: shortcutPresentation{SupportsTemplate: true},
		},
		{
			Group:         "collector",
			Command:       "delete",
			Action:        "collector.delete",
			Kind:          shortcutKindOperation,
			OperationID:   "collector.delete-rule",
			Summary:       "删除采集规则",
			Description:   "按 RuleId 删除采集规则。",
			InputMode:     "body synthesized from flags",
			RequiredFlags: []string{"--rule-id"},
			Bindings: []shortcutBinding{
				shortcutBindingParam("RuleId", "--rule-id", "body", true, "string", "采集规则 ID"),
			},
		},
	}
	out := make(map[string]shortcutCommandSpec, len(specs))
	for _, spec := range specs {
		out[normalizeToken(spec.Group)+"\x00"+normalizeToken(spec.Command)] = spec
	}
	return out
}
