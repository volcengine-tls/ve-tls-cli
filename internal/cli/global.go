package cli

import (
	"strings"
	"time"
)

type GlobalFlags struct {
	Profile            string
	Region             string
	Endpoint           string
	Output             string
	Filter             string
	OutputMode         string
	OutputModeExplicit bool
	OutputDir          string
	OutputDirExplicit  bool
	OutputFile         string
	TraceDir           string
	TraceRedact        string
	SecretsFile        string
	DryRun             bool
	ShowHelp           bool
	ShowVersion        bool
	NonInteractive     bool
	AllowWrite         bool
	Timeout            string
	ControlConflict    bool
}

func parseGlobal(args []string) (group string, rest []string, flags GlobalFlags, ok bool) {
	for len(args) > 0 {
		a := args[0]
		if !strings.HasPrefix(a, "-") {
			break
		}
		switch a {
		case "--non-interactive":
			flags.NonInteractive = true
			args = args[1:]
		case "--allow-write":
			flags.AllowWrite = true
			args = args[1:]
		case "--timeout":
			if !hasGlobalFlagValue(args, 0) {
				return "", nil, GlobalFlags{}, false
			}
			setGlobalTimeout(&flags, args[1])
			args = args[2:]
		case "--profile":
			if !hasGlobalFlagValue(args, 0) {
				return "", nil, GlobalFlags{}, false
			}
			flags.Profile = args[1]
			args = args[2:]
		case "--region":
			if !hasGlobalFlagValue(args, 0) {
				return "", nil, GlobalFlags{}, false
			}
			flags.Region = args[1]
			args = args[2:]
		case "--endpoint":
			if !hasGlobalFlagValue(args, 0) {
				return "", nil, GlobalFlags{}, false
			}
			flags.Endpoint = args[1]
			args = args[2:]
		case "--output":
			if !hasGlobalFlagValue(args, 0) {
				return "", nil, GlobalFlags{}, false
			}
			flags.Output = args[1]
			args = args[2:]
		case "--output-mode":
			if !hasGlobalFlagValue(args, 0) {
				return "", nil, GlobalFlags{}, false
			}
			flags.OutputMode = args[1]
			flags.OutputModeExplicit = true
			args = args[2:]
		case "--output-dir":
			if !hasGlobalFlagValue(args, 0) {
				return "", nil, GlobalFlags{}, false
			}
			flags.OutputDir = args[1]
			flags.OutputDirExplicit = true
			args = args[2:]
		case "--output-file":
			if !hasGlobalFlagValue(args, 0) {
				return "", nil, GlobalFlags{}, false
			}
			flags.OutputFile = args[1]
			args = args[2:]
		case "--jmes-filter":
			if !hasGlobalFlagValue(args, 0) {
				return "", nil, GlobalFlags{}, false
			}
			flags.Filter = args[1]
			args = args[2:]
		case "--trace-dir":
			if !hasGlobalFlagValue(args, 0) {
				return "", nil, GlobalFlags{}, false
			}
			flags.TraceDir = args[1]
			args = args[2:]
		case "--trace-redact":
			if !hasGlobalFlagValue(args, 0) {
				return "", nil, GlobalFlags{}, false
			}
			flags.TraceRedact = args[1]
			args = args[2:]
		case "--secrets-file":
			if !hasGlobalFlagValue(args, 0) {
				return "", nil, GlobalFlags{}, false
			}
			flags.SecretsFile = args[1]
			args = args[2:]
		case "--dry-run":
			flags.DryRun = true
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

func extractTrailingGlobals(args []string, flags GlobalFlags, allowDryRun bool) (rest []string, merged GlobalFlags, ok bool) {
	merged = flags
	rest = make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "--non-interactive":
			merged.NonInteractive = true
		case "--allow-write":
			merged.AllowWrite = true
		case "--timeout":
			if !hasGlobalFlagValue(args, i) {
				return nil, GlobalFlags{}, false
			}
			setGlobalTimeout(&merged, args[i+1])
			i++
		case "--output":
			if !hasGlobalFlagValue(args, i) {
				return nil, GlobalFlags{}, false
			}
			merged.Output = args[i+1]
			i++
		case "--output-mode":
			if !hasGlobalFlagValue(args, i) {
				return nil, GlobalFlags{}, false
			}
			merged.OutputMode = args[i+1]
			merged.OutputModeExplicit = true
			i++
		case "--output-dir":
			if !hasGlobalFlagValue(args, i) {
				return nil, GlobalFlags{}, false
			}
			merged.OutputDir = args[i+1]
			merged.OutputDirExplicit = true
			i++
		case "--output-file":
			if !hasGlobalFlagValue(args, i) {
				return nil, GlobalFlags{}, false
			}
			merged.OutputFile = args[i+1]
			i++
		case "--jmes-filter":
			if !hasGlobalFlagValue(args, i) {
				return nil, GlobalFlags{}, false
			}
			merged.Filter = args[i+1]
			i++
		case "--trace-dir":
			if !hasGlobalFlagValue(args, i) {
				return nil, GlobalFlags{}, false
			}
			merged.TraceDir = args[i+1]
			i++
		case "--trace-redact":
			if !hasGlobalFlagValue(args, i) {
				return nil, GlobalFlags{}, false
			}
			merged.TraceRedact = args[i+1]
			i++
		case "--dry-run":
			if !allowDryRun {
				rest = append(rest, a)
				continue
			}
			merged.DryRun = true
		default:
			rest = append(rest, a)
		}
	}
	return rest, merged, true
}

func setGlobalTimeout(flags *GlobalFlags, value string) {
	if flags.Timeout != "" {
		before, e1 := time.ParseDuration(strings.TrimSpace(flags.Timeout))
		after, e2 := time.ParseDuration(strings.TrimSpace(value))
		if e1 != nil || e2 != nil || before != after {
			flags.ControlConflict = true
		}
	}
	flags.Timeout = value
}

// Other command groups retain their own option grammar; only these invocation
// controls are additionally accepted after the command name.
func extractTrailingInvocationControls(args []string, flags GlobalFlags) ([]string, GlobalFlags, bool) {
	var rest []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--non-interactive":
			flags.NonInteractive = true
		case "--allow-write":
			flags.AllowWrite = true
		case "--timeout":
			if !hasGlobalFlagValue(args, i) {
				return nil, GlobalFlags{}, false
			}
			setGlobalTimeout(&flags, args[i+1])
			i++
		default:
			rest = append(rest, args[i])
		}
	}
	return rest, flags, true
}

func allowsTrailingGlobalsForGroup(group string) bool {
	switch strings.TrimSpace(group) {
	case "raw":
		return true
	case "tool":
		return true
	case "workflow":
		return true
	case "project", "topic", "metric-topic", "index", "log", "host-group", "collector":
		return true
	default:
		return false
	}
}

func allowsTrailingDryRun(group string, rest []string) bool {
	g := strings.TrimSpace(group)
	if g == "raw" {
		return true
	}
	if g == "tool" || g == "workflow" {
		if len(rest) == 0 {
			return false
		}
		return strings.TrimSpace(rest[0]) == "exec"
	}
	if g != "tool" {
		return false
	}
	return false
}

// Values that are separate option tokens must not swallow execution controls.
// A path beginning with -- can be expressed with an explicit ./ prefix.
func hasGlobalFlagValue(args []string, index int) bool {
	if index+1 >= len(args) {
		return false
	}
	next := args[index+1]
	return next != "-h" && !strings.HasPrefix(next, "--")
}
