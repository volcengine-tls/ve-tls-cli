package cli

type cliGroupSpec struct {
	Name        string
	Description string
}

type cliGlobalFlagSpec struct {
	Name        string
	Usage       string
	Description string
	TakesValue  bool
}

func cliGroups() []cliGroupSpec {
	return []cliGroupSpec{
		{Name: "configure", Description: "Manage local profiles"},
		{Name: "capabilities", Description: "Output CLI capability contract"},
		{Name: "api", Description: "Call TLS OpenAPI directly"},
		{Name: "project", Description: "Project operations (ID-first)"},
		{Name: "topic", Description: "Topic operations (ID-first)"},
		{Name: "metric-topic", Description: "Metric topic operations (ID-first)"},
		{Name: "index", Description: "Index operations (ID-first)"},
		{Name: "log", Description: "Log search/export"},
		{Name: "assistant", Description: "AI assistant operations"},
		{Name: "doctor", Description: "Diagnose config and environment"},
		{Name: "completion", Description: "Generate shell completion"},
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
		{Name: "--profile", Usage: "--profile <name>", Description: "profile name", TakesValue: true},
		{Name: "--output", Usage: "--output <json|jsonl>", Description: "output format", TakesValue: true},
		{Name: "--output-mode", Usage: "--output-mode <stdout|file>", Description: "output destination", TakesValue: true},
		{Name: "--output-file", Usage: "--output-file <path>", Description: "output file path", TakesValue: true},
		{Name: "--jmes-filter", Usage: "--jmes-filter <expr>", Description: "JMESPath output filter", TakesValue: true},
		{Name: "--trace-dir", Usage: "--trace-dir <path>", Description: "trace artifact dir", TakesValue: true},
		{Name: "--trace-redact", Usage: "--trace-redact <strict|default>", Description: "trace redact mode", TakesValue: true},
		{Name: "--secrets-file", Usage: "--secrets-file <path>", Description: "dotenv file", TakesValue: true},
		{Name: "--dry-run", Usage: "--dry-run", Description: "dry-run (api group only)"},
		{Name: "--help", Usage: "--help", Description: "show help"},
		{Name: "-h", Usage: "-h", Description: "show help"},
		{Name: "--version", Usage: "--version", Description: "show version"},
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
