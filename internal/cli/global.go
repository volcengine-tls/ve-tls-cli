package cli

import "strings"

type GlobalFlags struct {
	Profile     string
	Output      string
	Filter      string
	OutputMode  string
	OutputFile  string
	TraceDir    string
	TraceRedact string
	SecretsFile string
	DryRun      bool
	Debug       bool
	ShowHelp    bool
	ShowVersion bool
}

func parseGlobal(args []string) (group string, rest []string, flags GlobalFlags, ok bool) {
	for len(args) > 0 {
		a := args[0]
		if !strings.HasPrefix(a, "-") {
			break
		}
		switch a {
		case "--profile":
			if len(args) < 2 {
				return "", nil, GlobalFlags{}, false
			}
			flags.Profile = args[1]
			args = args[2:]
		case "--output":
			if len(args) < 2 {
				return "", nil, GlobalFlags{}, false
			}
			flags.Output = args[1]
			args = args[2:]
		case "--output-mode":
			if len(args) < 2 {
				return "", nil, GlobalFlags{}, false
			}
			flags.OutputMode = args[1]
			args = args[2:]
		case "--output-file":
			if len(args) < 2 {
				return "", nil, GlobalFlags{}, false
			}
			flags.OutputFile = args[1]
			args = args[2:]
		case "--jmes-filter":
			if len(args) < 2 {
				return "", nil, GlobalFlags{}, false
			}
			flags.Filter = args[1]
			args = args[2:]
		case "--trace-dir":
			if len(args) < 2 {
				return "", nil, GlobalFlags{}, false
			}
			flags.TraceDir = args[1]
			args = args[2:]
		case "--trace-redact":
			if len(args) < 2 {
				return "", nil, GlobalFlags{}, false
			}
			flags.TraceRedact = args[1]
			args = args[2:]
		case "--secrets-file":
			if len(args) < 2 {
				return "", nil, GlobalFlags{}, false
			}
			flags.SecretsFile = args[1]
			args = args[2:]
		case "--dry-run":
			flags.DryRun = true
			args = args[1:]
		case "--debug":
			flags.Debug = true
			args = args[1:]
		case "--help", "-h":
			flags.ShowHelp = true
			args = args[1:]
		case "--version":
			flags.ShowVersion = true
			args = args[1:]
		default:
			return "", nil, GlobalFlags{}, false
		}
	}
	if len(args) == 0 {
		return "", nil, flags, true
	}
	group = args[0]
	rest = args[1:]
	return group, rest, flags, true
}
