package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/output"
)

func TestCompletionZshIncludesGroupsFlagsAndSubcommands(t *testing.T) {
	out, code, err := runCompletion(newContext(nil, nil, output.Format(""), "", ""), []string{"zsh"})
	if err != nil || code != 0 {
		t.Fatalf("unexpected: code=%d err=%v", code, err)
	}
	s, ok := out.(string)
	if !ok {
		t.Fatalf("unexpected type: %T", out)
	}
	for _, want := range []string{
		"#compdef volclog",
		"--output-mode",
		"--dry-run",
		"--cred-ref",
		"groups=(",
		"log",
		"search",
		"export",
		"export-analysis",
		"assistant_cmds",
		"describe-session-answer",
		"topic",
		"create",
		"query-range",
		"api_call_flags",
		"http_methods",
		"GET",
		"/SearchLogs",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in completion script", want)
		}
	}
}

func TestCompletionBashIncludesGroupsFlagsAndCases(t *testing.T) {
	out, code, err := runCompletion(newContext(nil, nil, output.Format(""), "", ""), []string{"bash"})
	if err != nil || code != 0 {
		t.Fatalf("unexpected: code=%d err=%v", code, err)
	}
	s, ok := out.(string)
	if !ok {
		t.Fatalf("unexpected type: %T", out)
	}
	for _, want := range []string{
		"while [[ $i -lt ${#COMP_WORDS[@]} ]]",
		"--output-mode",
		"call",
		"GET POST PUT DELETE",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in completion script", want)
		}
	}
}

func TestCompletionFishIncludesProjectSubcommands(t *testing.T) {
	out, code, err := runCompletion(newContext(nil, nil, output.Format(""), "", ""), []string{"fish"})
	if err != nil || code != 0 {
		t.Fatalf("unexpected: code=%d err=%v", code, err)
	}
	s, ok := out.(string)
	if !ok {
		t.Fatalf("unexpected type: %T", out)
	}
	for _, want := range []string{
		"__fish_seen_subcommand_from api",
		"-l method",
		"GET POST PUT DELETE",
		"-l path",
		"/SearchLogs",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in completion script", want)
		}
	}
}

func TestCompletionPowerShellNormalizesBoolFlagCompletions(t *testing.T) {
	out, code, err := runCompletion(newContext(nil, nil, output.Format(""), "", ""), []string{"powershell"})
	if err != nil || code != 0 {
		t.Fatalf("unexpected: code=%d err=%v", code, err)
	}
	s, ok := out.(string)
	if !ok {
		t.Fatalf("unexpected type: %T", out)
	}
	for _, want := range []string{
		"Register-ArgumentCompleter",
		"$apiCallFlags",
		"$httpMethods",
		"/SearchLogs",
		"'api' { $candidates = @('call') }",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in completion script", want)
		}
	}
}

func TestCompletionPowerShellIncludesGlobalFlagsAndSubcommands(t *testing.T) {
	out, code, err := runCompletion(newContext(nil, nil, output.Format(""), "", ""), []string{"powershell"})
	if err != nil || code != 0 {
		t.Fatalf("unexpected: code=%d err=%v", code, err)
	}
	s, ok := out.(string)
	if !ok {
		t.Fatalf("unexpected type: %T", out)
	}
	for _, want := range []string{
		"$configureCmds",
		"$projectCmds",
		"$topicCmds",
		"$metricTopicCmds",
		"$indexCmds",
		"$logCmds",
		"$promCmds",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in completion script", want)
		}
	}
}

func TestRunCompletionWritesPlainText(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"completion", "zsh"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected code: %d, stderr=%s", code, stderr.String())
	}
	if strings.HasPrefix(stdout.String(), "\"") {
		t.Fatalf("unexpected json-encoded string: %q", stdout.String()[:min(40, len(stdout.String()))])
	}
	if !strings.HasPrefix(stdout.String(), "#compdef volclog") {
		t.Fatalf("unexpected completion header: %q", stdout.String()[:min(40, len(stdout.String()))])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
