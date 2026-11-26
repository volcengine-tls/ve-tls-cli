package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVEAuthFollowsVECurrentProfileAndUsesTLSTarget(t *testing.T) {
	server, requests := newCapturingServer(t)
	dir := t.TempDir()
	veConfig := filepath.Join(dir, "ve.json")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(dir, "tls.json"))
	t.Setenv("VOLCENGINE_CLI_CONFIG_FILE", veConfig)
	t.Setenv("VOLCENGINE_PROFILE", "")
	t.Setenv("VOLCSTACK_PROFILE", "")
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "test-env-ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "test-env-sk")
	t.Setenv("VOLCENGINE_REGION", "cn-shanghai")
	t.Setenv("VOLCENGINE_ENDPOINT", "http://127.0.0.1:0")

	run := func(args ...string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if code := Run(args, &stdout, &stderr); code != 0 {
			t.Fatalf("%v exited %d: %s", args, code, stderr.String())
		}
	}
	run("configure", "set", "--profile", "tls-beijing", "--mode", "ve", "--region", "cn-beijing", "--endpoint", server.URL)
	run("configure", "use", "tls-beijing")

	const veConfigTemplate = `{
		"current": %q,
		"profiles": {
			"ve-dev": {"mode":"ak", "access-key":"test-dev-ak", "secret-key":"test-dev-sk", "session-token":"test-dev-token"},
			"ve-prod": {"mode":"ak", "access-key":"test-prod-ak", "secret-key":"test-prod-sk", "session-token":"test-prod-token"}
		}
	}`
	for _, tc := range []struct {
		profile string
		ak      string
		token   string
	}{
		{"ve-dev", "test-dev-ak", "test-dev-token"},
		{"ve-prod", "test-prod-ak", "test-prod-token"},
	} {
		if err := os.WriteFile(veConfig, []byte(fmt.Sprintf(veConfigTemplate, tc.profile)), 0o600); err != nil {
			t.Fatal(err)
		}
		run("raw", "--method", "GET", "--path", "/DescribeProjects")
		select {
		case got := <-requests:
			if !strings.Contains(got.authorization, "Credential="+tc.ak+"/") ||
				!strings.Contains(got.authorization, "/cn-beijing/TLS/request") || got.securityToken != tc.token {
				t.Fatalf("request for %s did not use its VE credentials and the TLS signing region", tc.profile)
			}
		default:
			t.Fatalf("no request for %s reached the TLS profile endpoint", tc.profile)
		}
	}
}
