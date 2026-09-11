package cli

import (
	"errors"
	"os"
	"strconv"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/config"
)

func runConfigureProject(ctx *Context, args []string) (any, error) {
	if len(args) == 0 {
		return nil, &usageError{Text: usageConfigure(), ExitCode: 1}
	}
	switch args[0] {
	case "show":
		wd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		cfg, p, err := config.LoadProjectConfig(wd)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"path":          p,
			"output":        cfg.Output,
			"output_mode":   cfg.OutputMode,
			"output_dir":    cfg.OutputDir,
			"timeout":       cfg.TimeoutSeconds,
			"trace_redact":  cfg.TraceRedact,
			"hints_file":    cfg.HintsFile,
			"region":        cfg.Region,
			"endpoint":      cfg.Endpoint,
			"effective_wd":  wd,
			"config_exists": p != "",
		}, nil
	case "set":
		var output string
		var outputMode string
		var outputDir string
		var timeout int
		var traceRedact string
		var hintsFile string
		for len(args) > 0 {
			switch args[0] {
			case "set":
				args = args[1:]
			case "--output":
				if len(args) < 2 {
					return nil, errors.New("missing --output value")
				}
				output = args[1]
				args = args[2:]
			case "--output-mode":
				if len(args) < 2 {
					return nil, errors.New("missing --output-mode value")
				}
				outputMode = args[1]
				args = args[2:]
			case "--output-dir":
				if len(args) < 2 {
					return nil, errors.New("missing --output-dir value")
				}
				outputDir = args[1]
				args = args[2:]
			case "--timeout-seconds":
				if len(args) < 2 {
					return nil, errors.New("missing --timeout-seconds value")
				}
				v, err := strconv.Atoi(args[1])
				if err != nil {
					return nil, err
				}
				timeout = v
				args = args[2:]
			case "--trace-redact":
				if len(args) < 2 {
					return nil, errors.New("missing --trace-redact value")
				}
				traceRedact = args[1]
				args = args[2:]
			case "--hints-file":
				if len(args) < 2 {
					return nil, errors.New("missing --hints-file value")
				}
				hintsFile = args[1]
				args = args[2:]
			default:
				return nil, errors.New("unknown flag: " + args[0])
			}
		}
		wd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		cfg, p, err := config.LoadProjectConfig(wd)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(output) != "" {
			cfg.Output = strings.TrimSpace(output)
		}
		if strings.TrimSpace(outputMode) != "" {
			cfg.OutputMode = strings.TrimSpace(outputMode)
		}
		if strings.TrimSpace(outputDir) != "" {
			cfg.OutputDir = strings.TrimSpace(outputDir)
		}
		if timeout != 0 {
			cfg.TimeoutSeconds = timeout
		}
		if strings.TrimSpace(traceRedact) != "" {
			cfg.TraceRedact = strings.TrimSpace(traceRedact)
		}
		if strings.TrimSpace(hintsFile) != "" {
			cfg.HintsFile = strings.TrimSpace(hintsFile)
		}
		if p == "" {
			p = ""
			if w, err := os.Getwd(); err == nil {
				p = w
			}
			if strings.TrimSpace(p) == "" {
				return nil, errors.New("working directory not found")
			}
			p = p + string(os.PathSeparator) + ".volclog" + string(os.PathSeparator) + "cli.config.json"
		}
		if err := config.SaveProjectConfigAt(p, cfg); err != nil {
			return nil, err
		}
		return map[string]any{
			"path":         p,
			"output":       cfg.Output,
			"output_mode":  cfg.OutputMode,
			"output_dir":   cfg.OutputDir,
			"timeout":      cfg.TimeoutSeconds,
			"trace_redact": cfg.TraceRedact,
			"hints_file":   cfg.HintsFile,
		}, nil
	default:
		return nil, errors.New("unknown configure project command: " + args[0])
	}
}
