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
		"put",
		"context",
		"histogram",
		"host-group",
		"bind-rules",
		"unbind-rules",
		"delete-host",
		"collector",
		"bind-host-groups",
		"unbind-host-groups",
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
	for _, notWant := range []string{"assistant_cmds", "describe-session-answer"} {
		if strings.Contains(s, notWant) {
			t.Fatalf("unexpected hidden assistant completion token %q in %q", notWant, s)
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
		"bind-rules unbind-rules delete-host",
		"bind-host-groups unbind-host-groups",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in completion script", want)
		}
	}
	if strings.Contains(s, "describe-session-answer") {
		t.Fatalf("assistant shortcut should be hidden from bash completion: %q", s)
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
		"bind-rules unbind-rules delete-host",
		"bind-host-groups unbind-host-groups",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in completion script", want)
		}
	}
	if strings.Contains(s, "describe-session-answer") {
		t.Fatalf("assistant shortcut should be hidden from fish completion: %q", s)
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
		"$hostGroupCmds",
		"$collectorCmds",
		"bind-rules",
		"bind-host-groups",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in completion script", want)
		}
	}
	if strings.Contains(s, "describe-session-answer") {
		t.Fatalf("assistant shortcut should be hidden from powershell completion: %q", s)
	}
}

func TestCompletionGroupsPrioritizeAgentFirstOrder(t *testing.T) {
	groups := completionGroups()
	got := strings.Join(groups[:4], ",")
	want := "configure,capabilities,api,doctor"
	if got != want {
		t.Fatalf("unexpected leading completion groups: got=%q want=%q", got, want)
	}
	if strings.Index(strings.Join(groups, ","), "project") < strings.Index(strings.Join(groups, ","), "doctor") {
		t.Fatalf("expected manual groups after primary agent groups: %v", groups)
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
