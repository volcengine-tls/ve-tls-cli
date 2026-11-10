package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func invocationC4TempWorkingDir(t *testing.T) string {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(old); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
	return dir
}

func invocationC4Failure(t *testing.T, stdout, stderr *bytes.Buffer, code, wantCode int, kind string) map[string]any {
	t.Helper()
	if code != wantCode || stderr.Len() != 0 {
		t.Fatalf("exit=%d want=%d stdout=%q stderr=%q", code, wantCode, stdout.String(), stderr.String())
	}
	var env map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("expected one JSON envelope: %v, stdout=%q", err, stdout.String())
	}
	errObj, ok := env["error"].(map[string]any)
	if !ok || env["status"] != "failed" || errObj["kind"] != kind || errObj["code"] != "CLIError" {
		t.Fatalf("unexpected error envelope: %#v", env)
	}
	summary, ok := env["summary"].(map[string]any)
	if !ok || summary["remoteOutcome"] != "not_sent" || summary["deliveryOutcome"] != "success" {
		t.Fatalf("unexpected outcome: %#v", env)
	}
	return env
}

func TestInvocationC4EarlyFailureProtocol(t *testing.T) {
	if currentEdition() != cliEditionVolclog {
		t.Skip("default edition protocol")
	}
	invocationC4TempWorkingDir(t)
	const secret = "c4-secret-canary"
	cases := []struct {
		name string
		args []string
	}{
		{"no args", nil},
		{"unknown global", []string{"--unknown", secret}},
		{"missing global value", []string{"--output"}},
		{"missing trailing global value", []string{"tool", "exec", "x", "--output-dir"}},
		{"unknown group", []string{secret}},
		{"unmatched group with whitespace", []string{" tool "}},
		{"unavailable group", []string{"assistant"}},
		{"version extra arg", []string{"version", secret}},
		{"invalid output", []string{"--output", secret, "tool", "list"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(tc.args, &stdout, &stderr)
			env := invocationC4Failure(t, &stdout, &stderr, code, 1, "usage")
			if strings.Contains(stdout.String(), secret) || strings.Contains(stderr.String(), secret) {
				t.Fatalf("unexpected argument disclosure: %#v", env)
			}
			if tc.name == "unknown group" && env["action"] != "cli.invoke" {
				t.Fatalf("unknown command action must be stable: %#v", env)
			}
		})
	}
}

func TestInvocationC4ProjectConfigFailure(t *testing.T) {
	dir := invocationC4TempWorkingDir(t)
	if err := os.Mkdir(filepath.Join(dir, ".volclog"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".volclog", "cli.config.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, group := range []string{"tool", "c4-secret-canary", "login", "logout", "sso", "configure"} {
		t.Run(group, func(t *testing.T) {
			args := []string{group}
			if group == "configure" {
				args = append(args, "sso")
			}
			var stdout, stderr bytes.Buffer
			code := Run(args, &stdout, &stderr)
			if currentEdition() == cliEditionVolclog && !isInteractiveAuthCommand(group, args[1:]) {
				env := invocationC4Failure(t, &stdout, &stderr, code, 2, "config")
				if group == "c4-secret-canary" && (env["action"] != "cli.invoke" || strings.Contains(stdout.String(), group)) {
					t.Fatalf("unknown group exposed by config failure: %#v", env)
				}
				return
			}
			if code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), `"errorCode": "CLIError"`) || !strings.Contains(stderr.String(), `"kind": "config"`) {
				t.Fatalf("legacy config failure changed: exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestInvocationC4MissingWorkingDirectory(t *testing.T) {
	dir := invocationC4TempWorkingDir(t)
	if err := os.Remove(dir); err != nil {
		t.Skipf("cannot remove current directory on this platform: %v", err)
	}
	if _, err := os.Getwd(); err == nil {
		t.Skip("platform can resolve a removed current directory")
	}
	var stdout, stderr bytes.Buffer
	code := Run([]string{"tool", "list"}, &stdout, &stderr)
	if currentEdition() == cliEditionVolclog {
		invocationC4Failure(t, &stdout, &stderr, code, 2, "config")
		return
	}
	if code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "failed to get working directory") {
		t.Fatalf("legacy cwd failure changed: exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

type invocationC4ShortWriter struct {
	calls   int
	failure error
}

func (w *invocationC4ShortWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.failure != nil {
		return 0, w.failure
	}
	return len(p) / 2, nil
}

func TestInvocationC4ShortStdout(t *testing.T) {
	if currentEdition() != cliEditionVolclog {
		t.Skip("default edition delivery protocol")
	}
	for _, args := range [][]string{nil, {"version"}, {"--version"}, {"--help"}, {"-h"}} {
		for _, failure := range []error{nil, io.ErrClosedPipe} {
			stdout := invocationC4ShortWriter{failure: failure}
			var stderr bytes.Buffer
			code := Run(args, &stdout, &stderr)
			var env map[string]any
			if code != 3 || stdout.calls != 1 || json.Unmarshal(stderr.Bytes(), &env) != nil {
				t.Fatalf("args=%v failure=%v exit=%d writes=%d stderr=%q", args, failure, code, stdout.calls, stderr.String())
			}
			errObj, ok := env["error"].(map[string]any)
			summary, summaryOK := env["summary"].(map[string]any)
			if !ok || !summaryOK || env["status"] != "failed" || errObj["kind"] != "output_failed" || errObj["stage"] != "stdout" || summary["remoteOutcome"] != "not_sent" || summary["deliveryOutcome"] != "failed" {
				t.Fatalf("unexpected delivery failure for %v (%v): %#v", args, failure, env)
			}
		}
	}
}

func TestInvocationC4LegacyAndExplicitText(t *testing.T) {
	invocationC4TempWorkingDir(t)
	for _, group := range [][]string{{"login"}, {"logout"}, {"sso"}, {"configure", "sso"}} {
		args := append([]string{"--output", "invalid"}, group...)
		var stdout, stderr bytes.Buffer
		code := Run(args, &stdout, &stderr)
		if code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), `"kind": "usage"`) {
			t.Fatalf("auth output changed: args=%v exit=%d stdout=%q stderr=%q", args, code, stdout.String(), stderr.String())
		}
	}
	for _, flag := range []string{"--help", "-h", "--version"} {
		var stdout, stderr bytes.Buffer
		if code := Run([]string{flag}, &stdout, &stderr); code != 0 || stderr.Len() != 0 || stdout.Len() == 0 || strings.HasPrefix(stdout.String(), "{") {
			t.Fatalf("explicit %s output changed: exit=%d stdout=%q stderr=%q", flag, code, stdout.String(), stderr.String())
		}
	}
	if currentEdition() != cliEditionVolclogHuman {
		return
	}
	for _, args := range [][]string{nil, {"--unknown"}, {"unknown-command"}} {
		var stdout, stderr bytes.Buffer
		if code := Run(args, &stdout, &stderr); code != 1 || stdout.Len() != 0 || !strings.HasPrefix(stderr.String(), "Usage:") {
			t.Fatalf("human usage changed: args=%v exit=%d stdout=%q stderr=%q", args, code, stdout.String(), stderr.String())
		}
	}
}
