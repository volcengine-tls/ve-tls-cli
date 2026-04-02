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
	return []cliGroupSpec{
		{Name: "configure", Description: "配置本地凭证与默认参数", Primary: true},
		{Name: "capabilities", Description: "发现 API 能力与调用语义", Primary: true},
		{Name: "api", Description: "查看约束并直接调用 OpenAPI", Primary: true},
		{Name: "doctor", Description: "诊断环境、鉴权与配置状态", Primary: true},
		{Name: "project", Description: "项目快捷命令（次级入口）"},
		{Name: "topic", Description: "主题快捷命令（次级入口）"},
		{Name: "metric-topic", Description: "指标主题快捷命令（次级入口）"},
		{Name: "index", Description: "索引快捷命令（次级入口）"},
		{Name: "log", Description: "日志快捷命令（次级入口）"},
		{Name: "assistant", Description: "AI 助手快捷命令（次级入口）"},
		{Name: "completion", Description: "生成 shell 补全脚本"},
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
	return []cliGlobalFlagSpec{
		{Name: "--profile", Usage: "--profile <name>", Description: "配置名称", TakesValue: true},
		{Name: "--output", Usage: "--output <json|jsonl>", Description: "输出格式", TakesValue: true},
		{Name: "--output-mode", Usage: "--output-mode <stdout|file>", Description: "输出目标", TakesValue: true},
		{Name: "--output-file", Usage: "--output-file <path>", Description: "输出文件路径", TakesValue: true},
		{Name: "--jmes-filter", Usage: "--jmes-filter <expr>", Description: "JMESPath 输出过滤表达式", TakesValue: true},
		{Name: "--trace-dir", Usage: "--trace-dir <path>", Description: "trace 落盘目录", TakesValue: true},
		{Name: "--trace-redact", Usage: "--trace-redact <strict|default>", Description: "trace 脱敏模式", TakesValue: true},
		{Name: "--secrets-file", Usage: "--secrets-file <path>", Description: "dotenv 文件路径", TakesValue: true},
		{Name: "--dry-run", Usage: "--dry-run", Description: "仅做请求规划，不真正发送（仅 api）"},
		{Name: "--help", Usage: "--help", Description: "显示帮助"},
		{Name: "-h", Usage: "-h", Description: "显示帮助"},
		{Name: "--version", Usage: "--version", Description: "显示版本"},
	}
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
