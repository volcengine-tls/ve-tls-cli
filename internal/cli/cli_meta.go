package cli

type cliGroupSpec struct {
	Name        string
	Description string
	Primary     bool
}

type cliGlobalFlagSpec struct {
	Name        string
	Usage       string
	Description string
	TakesValue  bool
}

func cliGroups() []cliGroupSpec {
	groups := visibleCliGroups()
	if currentEdition() != cliEditionVolclog {
		return groups
	}
	filtered := make([]cliGroupSpec, 0, len(groups))
	for _, group := range groups {
		if !volclogPrimaryGroupNames[group.Name] {
			continue
		}
		filtered = append(filtered, group)
	}
	return filtered
}

func visibleCliGroups() []cliGroupSpec {
	return []cliGroupSpec{
		{Name: "configure", Description: "配置本地凭证与默认参数", Primary: true},
		{Name: "tool", Description: "使用统一 tool 契约面发现与查看能力", Primary: true},
		{Name: "workflow", Description: "使用 CLI workflow 合约面执行高层编排能力", Primary: true},
		{Name: "raw", Description: "原始 transport 调用入口（仅在已明确 method/path 时使用）", Primary: true},
		{Name: "doctor", Description: "诊断环境、鉴权与配置状态", Primary: true},
		{Name: "skill", Description: "安装或列出内置 volclog skills", Primary: true},
		{Name: "project", Description: "项目 human shortcut（仅在 volclog-human 中提供）"},
		{Name: "topic", Description: "主题 human shortcut（仅在 volclog-human 中提供）"},
		{Name: "metric-topic", Description: "指标主题 human shortcut（仅在 volclog-human 中提供）"},
		{Name: "index", Description: "索引 human shortcut（仅在 volclog-human 中提供）"},
		{Name: "log", Description: "日志 human shortcut（仅在 volclog-human 中提供）"},
		{Name: "host-group", Description: "机器组 human shortcut（仅在 volclog-human 中提供）"},
		{Name: "collector", Description: "采集规则 human shortcut（仅在 volclog-human 中提供）"},
	}
}

func cliGroupNames() []string {
	groups := cliGroups()
	names := make([]string, 0, len(groups))
	for _, group := range groups {
		names = append(names, group.Name)
	}
	return names
}

func cliGlobalFlagSpecs() []cliGlobalFlagSpec {
	specs := []cliGlobalFlagSpec{
		{Name: "--profile", Usage: "--profile <name>", Description: "配置名称", TakesValue: true},
		{Name: "--output", Usage: "--output <json|jsonl|table>", Description: "输出格式（table 适用于常用快捷 list/get、index get、log search）", TakesValue: true},
		{Name: "--output-mode", Usage: "--output-mode <stdout|file>", Description: "输出目标", TakesValue: true},
		{Name: "--output-dir", Usage: "--output-dir <path>", Description: "file delivery 的输出目录", TakesValue: true},
		{Name: "--output-file", Usage: "--output-file <path>", Description: "输出文件路径", TakesValue: true},
		{Name: "--jmes-filter", Usage: "--jmes-filter <expr>", Description: "JMESPath 输出过滤表达式（tool/workflow/raw 作用于完整 envelope）", TakesValue: true},
		{Name: "--trace-dir", Usage: "--trace-dir <path>", Description: "trace 落盘目录", TakesValue: true},
		{Name: "--trace-redact", Usage: "--trace-redact <enabled>", Description: "启用 trace 脱敏（历史 strict/default 当前等价）", TakesValue: true},
		{Name: "--secrets-file", Usage: "--secrets-file <path>", Description: "dotenv 文件路径", TakesValue: true},
		{Name: "--dry-run", Usage: "--dry-run", Description: "仅做请求规划，不真正发送（支持 raw、tool exec、workflow exec）"},
		{Name: "--help", Usage: "--help", Description: "显示帮助"},
		{Name: "-h", Usage: "-h", Description: "显示帮助"},
		{Name: "--version", Usage: "--version", Description: "显示版本"},
	}
	if currentEdition() != cliEditionVolclog {
		return specs
	}
	filtered := make([]cliGlobalFlagSpec, 0, len(specs))
	for _, spec := range specs {
		if spec.Name == "--output-file" {
			continue
		}
		if spec.Name == "--output" {
			spec.Usage = "--output <json|jsonl>"
			spec.Description = "输出格式（tool/workflow/raw 默认用 json；需要面向机器的逐行结果时可用 jsonl）"
		}
		filtered = append(filtered, spec)
	}
	return filtered
}

func cliGlobalFlags() []string {
	specs := cliGlobalFlagSpecs()
	flags := make([]string, 0, len(specs))
	for _, spec := range specs {
		flags = append(flags, spec.Name)
	}
	return flags
}

func cliGlobalFlagsWithValue() []string {
	specs := cliGlobalFlagSpecs()
	flags := make([]string, 0, len(specs))
	for _, spec := range specs {
		if spec.TakesValue {
			flags = append(flags, spec.Name)
		}
	}
	return flags
}

func cliGlobalBareFlags() []string {
	specs := cliGlobalFlagSpecs()
	flags := make([]string, 0, len(specs))
	for _, spec := range specs {
		if !spec.TakesValue {
			flags = append(flags, spec.Name)
		}
	}
	return flags
}
