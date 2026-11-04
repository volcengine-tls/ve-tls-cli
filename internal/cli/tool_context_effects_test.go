package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestInvalidToolAndWorkflowContextHasNoEffectsWithFileOutput(t *testing.T) {
	commands := []struct {
		name string
		args []string
	}{
		{"tool", []string{"tool", "exec", "account.get"}},
		{"workflow", []string{"workflow", "exec", appResolveTopicIDsWorkflowID, "--input", `{"AppId":"app-1"}`}},
	}
	cases := []struct {
		name      string
		execution map[string]any
		region    string
		flags     []string
		code      int
	}{
		{"unknown control", map[string]any{"dryRun": true}, "", nil, 1},
		{"wrong control type", map[string]any{"dry_run": "true"}, "", nil, 1},
		{"invalid projection", map[string]any{"projection": "???"}, "", nil, 3},
		{"conflicting target", nil, "context-region", []string{"--region", "flag-region"}, 1},
	}
	for _, command := range commands {
		for _, tc := range cases {
			t.Run(command.name+"/"+tc.name, func(t *testing.T) {
				clearAuthTestEnv(t)
				t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "original-ak")
				t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "original-sk")
				t.Setenv("VOLCENGINE_REGION", "cn-beijing")
				t.Setenv("VOLCENGINE_ENDPOINT", "https://example.invalid")
				t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))
				secrets := filepath.Join(t.TempDir(), "secrets.env")
				if err := os.WriteFile(secrets, []byte("VOLCENGINE_ACCESS_KEY_ID=changed-ak\nVOLCENGINE_ACCESS_KEY_SECRET=changed-sk\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				contextValue := map[string]any{"secrets_file": secrets}
				if tc.execution != nil {
					contextValue["execution"] = tc.execution
				}
				if tc.region != "" {
					contextValue["region"] = tc.region
				}
				encoded, err := json.Marshal(contextValue)
				if err != nil {
					t.Fatal(err)
				}
				transport := &explicitProfileTransport{}
				originalTransport := http.DefaultTransport
				http.DefaultTransport = transport
				t.Cleanup(func() { http.DefaultTransport = originalTransport })
				outputDir := filepath.Join(t.TempDir(), "must-not-be-created")
				args := []string{"--output-mode", "file", "--output-dir", outputDir}
				args = append(args, tc.flags...)
				args = append(args, command.args...)
				args = append(args, "--context", string(encoded))
				var stdout, stderr bytes.Buffer
				if code := Run(args, &stdout, &stderr); code != tc.code {
					t.Fatalf("exit=%d, want %d; stdout=%s stderr=%s", code, tc.code, &stdout, &stderr)
				}
				if _, err := os.Stat(outputDir); !os.IsNotExist(err) {
					t.Fatalf("invalid context created output directory: %v", err)
				}
				if got := os.Getenv("VOLCENGINE_ACCESS_KEY_ID"); got != "original-ak" {
					t.Fatalf("invalid context loaded secrets: %q", got)
				}
				if transport.calls != 0 {
					t.Fatalf("invalid context sent %d requests", transport.calls)
				}
			})
		}
	}
}
