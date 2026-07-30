package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScopedHelpErrorPreservesWrappedErrorSemantics(t *testing.T) {
	sentinel := errors.New("sentinel")
	if err := withScopedHelpHint(sentinel, "volclog configure set"); !errors.Is(err, sentinel) {
		t.Fatal("scoped help wrapper must preserve errors.Is")
	}

	want := &usageError{Text: "usage", ExitCode: 1}
	got, ok := asUsageError(withScopedHelpHint(want, "volclog configure set"))
	if !ok || got != want {
		t.Fatal("scoped help wrapper must preserve errors.As for usageError")
	}
}

func TestRootHelpProvidesProgressiveAuthenticationRouting(t *testing.T) {
	text := usageText()
	for _, want := range []string{
		"鉴权速选:",
		"volclog login --help",
		"volclog configure sso-session --help",
		"volclog configure set --help",
		"volclog --profile <name> doctor",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("root help missing %q:\n%s", want, text)
		}
	}
}

func TestConfigureHelpListsAuthenticationSubcommands(t *testing.T) {
	text := usageConfigure()
	for _, want := range []string{
		"sso-session",
		"configure sso-session --help",
		"configure sso --help",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("configure help missing %q:\n%s", want, text)
		}
	}
}

func TestConfigureAuthenticationSubcommandHelpIsScoped(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		wantUsage  string
		notWantSet string
	}{
		{
			name:       "sso session",
			args:       []string{"configure", "sso-session", "--help"},
			wantUsage:  "volclog configure sso-session --name NAME",
			notWantSet: "Configure Set Flags:",
		},
		{
			name:       "sso profile",
			args:       []string{"configure", "sso", "--help"},
			wantUsage:  "volclog configure sso --profile NAME",
			notWantSet: "Configure Set Flags:",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, code := runHelpAuthCLI(t, tc.args...)
			if code != 0 {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
			if !strings.Contains(stdout, tc.wantUsage) {
				t.Fatalf("stdout missing %q:\n%s", tc.wantUsage, stdout)
			}
			if strings.Contains(stdout, tc.notWantSet) {
				t.Fatalf("stdout unexpectedly contains parent configure help %q:\n%s", tc.notWantSet, stdout)
			}
		})
	}
}

func TestConfigureHelpDoesNotRequireValidConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte("{invalid-json\n"), 0o600); err != nil {
		t.Fatalf("write invalid config: %v", err)
	}
	t.Setenv("VOLCLOG_CONFIG", configPath)

	cases := []struct {
		name      string
		args      []string
		wantUsage string
	}{
		{
			name:      "configure",
			args:      []string{"configure", "--help"},
			wantUsage: "volclog configure <command>",
		},
		{
			name:      "sso session",
			args:      []string{"configure", "sso-session", "--help"},
			wantUsage: "volclog configure sso-session --name NAME",
		},
		{
			name:      "sso profile",
			args:      []string{"configure", "sso", "--help"},
			wantUsage: "volclog configure sso --profile NAME",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(tc.args, &stdout, &stderr)
			if code != 0 {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if !strings.Contains(stdout.String(), tc.wantUsage) {
				t.Fatalf("stdout missing %q:\n%s", tc.wantUsage, stdout.String())
			}
		})
	}
}

func TestConfigureValidationErrorsPointToScopedHelp(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		wantKind string
		wantHint string
	}{
		{
			name:     "set",
			args:     []string{"configure", "set"},
			wantKind: "validation",
			wantHint: "volclog configure set --help",
		},
		{
			name:     "sso session",
			args:     []string{"configure", "sso-session"},
			wantKind: "validation",
			wantHint: "volclog configure sso-session --help",
		},
		{
			name:     "sso profile",
			args:     []string{"configure", "sso"},
			wantKind: "validation",
			wantHint: "volclog configure sso --help",
		},
		{
			name:     "missing flag value",
			args:     []string{"configure", "sso-session", "--name"},
			wantKind: "usage",
			wantHint: "volclog configure sso-session --help",
		},
		{
			name: "invalid start URL",
			args: []string{
				"configure", "sso-session",
				"--name", "corp",
				"--start-url", "http://example.com/start",
				"--region", "cn-beijing",
			},
			wantKind: "validation",
			wantHint: "volclog configure sso-session --help",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, code := runHelpAuthCLI(t, tc.args...)
			if code != 1 {
				t.Fatalf("exit=%d, want 1; stdout=%q stderr=%q", code, stdout, stderr)
			}
			var payload errPayload
			if err := json.Unmarshal([]byte(stderr), &payload); err != nil {
				t.Fatalf("decode stderr: %v; stderr=%q", err, stderr)
			}
			if payload.Kind != tc.wantKind {
				t.Fatalf("kind=%q, want %q; stderr=%q", payload.Kind, tc.wantKind, stderr)
			}
			if !strings.Contains(payload.Hint, tc.wantHint) {
				t.Fatalf("hint=%q, want scoped help %q", payload.Hint, tc.wantHint)
			}
			if strings.Contains(payload.Hint, "tool describe") {
				t.Fatalf("configure validation hint must not route to tool describe: %q", payload.Hint)
			}
		})
	}
}

func TestConfigureHelpExamplesMatchDefaultEdition(t *testing.T) {
	text := usageConfigure()
	for _, want := range []string{
		"configure profile add tenant-a --ak <ak> --sk <sk> --region cn-beijing --endpoint https://tls-cn-beijing.volces.com",
		"tool exec project.describe-projects",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("configure help missing runnable example %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "--profile tenant-a-sg project list") {
		t.Fatalf("default volclog help must not recommend hidden human shortcut:\n%s", text)
	}
}

func TestAuthenticationHelpCarriesNextStep(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []string
	}{
		{
			name: "sso session",
			text: usageConfigureSSOSession(),
			want: []string{"Next:", "volclog configure sso --help"},
		},
		{
			name: "sso profile",
			text: usageConfigureSSO(),
			want: []string{"Next:", "volclog --profile <name> doctor", "tool exec project.describe-projects"},
		},
		{
			name: "console login",
			text: usageLogin(),
			want: []string{"Next:", "volclog --profile <name> doctor", "tool exec project.describe-projects"},
		},
		{
			name: "sso login",
			text: usageSSOLogin(),
			want: []string{"Next:", "volclog configure sso --help", "volclog --profile <name> doctor"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, want := range tc.want {
				if !strings.Contains(tc.text, want) {
					t.Fatalf("help missing %q:\n%s", want, tc.text)
				}
			}
		})
	}
}

func TestConfigureHelpJourneyReachesToolDryRun(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("VOLCLOG_CONFIG", configPath)

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"configure", "profile", "add", "tenant-a",
		"--ak", "AKLTexample",
		"--sk", "example-secret",
		"--region", "cn-beijing",
		"--endpoint", "https://tls-cn-beijing.volces.com",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("configure exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"--profile", "tenant-a", "doctor"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doctor exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{
		"--profile", "tenant-a",
		"--dry-run",
		"tool", "exec", "project.describe-projects",
		"--input", `{"PageSize":20}`,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("tool dry-run exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("decode tool dry-run envelope: %v; stdout=%q", err, stdout.String())
	}
	if got, _ := envelope["status"].(string); got != "success" {
		t.Fatalf("tool dry-run status=%q, want success; stdout=%q", got, stdout.String())
	}
}

func runHelpAuthCLI(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	var stdout, stderr bytes.Buffer
	code := Run(args, &stdout, &stderr)
	return stdout.String(), stderr.String(), code
}
