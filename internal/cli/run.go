package cli

import (
	"io"
	"os"

	"tlsctl/internal/output"
	"tlsctl/internal/version"
)

func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = stderr.Write([]byte(usageText()))
		return 1
	}

	g, rest, gf, ok := parseGlobal(args)
	if !ok {
		_, _ = stderr.Write([]byte(usageText()))
		return 1
	}

	if gf.ShowHelp {
		_, _ = stdout.Write([]byte(usageText()))
		return 0
	}
	if gf.ShowVersion {
		_, _ = stdout.Write([]byte("tlsctl " + version.Version + "\n"))
		return 0
	}

	format, err := output.ParseFormat(gf.Output)
	if err != nil {
		writeCLIError(stderr, err, "", 0)
		return 2
	}

	ctx := newContext(stdout, stderr, format, gf.Profile, gf.Filter, gf.Debug)

	var out any
	switch g {
	case "configure":
		out, err = runConfigure(ctx, rest)
	case "api":
		out, err = runAPI(ctx, rest)
	case "project":
		out, err = runProject(ctx, rest)
	case "topic":
		out, err = runTopic(ctx, rest)
	case "metric-topic":
		out, err = runMetricTopic(ctx, rest)
	case "index":
		out, err = runIndex(ctx, rest)
	case "log":
		out, err = runLog(ctx, rest)
	case "ai":
		out, err = runAI(ctx, rest)
	default:
		_, _ = stderr.Write([]byte(usageText()))
		return 1
	}
	if err != nil {
		if ue, ok := asUsageError(err); ok {
			_, _ = stdout.Write([]byte(ue.Text))
			return ue.ExitCode
		}
		writeCLIError(stderr, err, ctx.RequestID, ctx.StatusCode)
		return 2
	}
	if ctx.Filter != "" {
		out, err = output.ApplyFilter(out, ctx.Filter)
		if err != nil {
			writeCLIError(stderr, err, ctx.RequestID, ctx.StatusCode)
			return 2
		}
	}
	if err := output.Write(stdout, out, format); err != nil {
		writeCLIError(stderr, err, ctx.RequestID, ctx.StatusCode)
		return 2
	}
	return 0
}

func usageText() string {
	return `Usage:
  tlsctl [--profile <name>] [--output json|jsonl] [--jmes-filter <expr>] [--debug] <group> <command> [args]

Groups:
  configure   Manage local profiles
  api         Call TLS OpenAPI directly
  project     Project operations (ID-first)
  topic       Topic operations (ID-first)
  metric-topic Metric topic operations (ID-first)
  index       Index operations (ID-first)
  log         Log search/export
  ai          AI packs bootstrap/export

Global Flags:
  --profile <name>
  --output <json|jsonl>
  --jmes-filter <expr>
  --debug
  --help
  --version
`
}

func init() {
	_ = os.Setenv("GODEBUG", os.Getenv("GODEBUG"))
}
