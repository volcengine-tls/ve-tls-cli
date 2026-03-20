package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigure_ListAndDelete(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	t.Setenv("VOLCLOG_CONFIG", cfgPath)

	run := func(args ...string) map[string]any {
		t.Helper()
		var stdout, stderr bytes.Buffer
		code := Run(args, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
		}
		var m map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &m); err != nil {
			t.Fatalf("invalid json: %v; stdout=%q", err, stdout.String())
		}
		return m
	}

	run("configure", "set", "--profile", "tenant-a-cn", "--ak", "akA", "--sk", "skA", "--endpoint", "https://tls-cn-beijing.volces.com")
	run("configure", "set", "--profile", "tenant-b-cn", "--ak", "akB", "--sk", "skB", "--endpoint", "https://tls-cn-beijing.volces.com")
	run("configure", "use", "tenant-b-cn")

	out := run("configure", "list")
	if out["current_profile"] != "tenant-b-cn" {
		t.Fatalf("current_profile=%v", out["current_profile"])
	}
	profiles, ok := out["profiles"].([]any)
	if !ok || len(profiles) != 2 {
		t.Fatalf("profiles=%T %v", out["profiles"], out["profiles"])
	}

	out = run("configure", "list", "--prefix", "tenant-a")
	profiles, _ = out["profiles"].([]any)
	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %v", out["profiles"])
	}

	out = run("configure", "delete", "tenant-a-cn")
	if out["deleted"] != "tenant-a-cn" {
		t.Fatalf("deleted=%v", out["deleted"])
	}
	out = run("configure", "list")
	profiles, _ = out["profiles"].([]any)
	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile after delete, got %v", out["profiles"])
	}

	out = run("configure", "delete", "tenant-b-cn")
	if out["current_profile"] != "" {
		t.Fatalf("current_profile=%v", out["current_profile"])
	}
	out = run("configure", "list")
	profiles, _ = out["profiles"].([]any)
	if len(profiles) != 0 {
		t.Fatalf("expected empty profiles, got %v", out["profiles"])
	}

	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("config file missing: %v", err)
	}
}

func TestConfigure_DeletePrefixRequiresYes(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	t.Setenv("VOLCLOG_CONFIG", cfgPath)

	runOK := func(args ...string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		code := Run(args, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
		}
	}
	runErr := func(args ...string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		code := Run(args, &stdout, &stderr)
		if code == 0 {
			t.Fatalf("expected non-zero exit; stdout=%q stderr=%q", stdout.String(), stderr.String())
		}
	}

	runOK("configure", "set", "--profile", "tenant-a-cn", "--ak", "akA", "--sk", "skA", "--endpoint", "https://tls-cn-beijing.volces.com")
	runOK("configure", "set", "--profile", "tenant-a-sg", "--ak", "akA", "--sk", "skA", "--endpoint", "https://tls-ap-singapore-1.volces.com")

	runErr("configure", "delete", "--prefix", "tenant-a")
	runOK("configure", "delete", "--prefix", "tenant-a", "--yes")
}

func TestConfigure_CredRefReuseAndDeriveRegionFromEndpoint(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	t.Setenv("VOLCLOG_CONFIG", cfgPath)

	run := func(args ...string) map[string]any {
		t.Helper()
		var stdout, stderr bytes.Buffer
		code := Run(args, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
		}
		var m map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &m); err != nil {
			t.Fatalf("invalid json: %v; stdout=%q", err, stdout.String())
		}
		return m
	}

	run("configure", "set", "--profile", "abc-bj", "--cred-ref", "ma-abc-root", "--ak", "akA", "--sk", "skA", "--endpoint", "https://tls-cn-beijing.volces.com")
	out := run("configure", "show", "--profile", "abc-bj")
	if out["cred_ref"] != "ma-abc-root" {
		t.Fatalf("cred_ref=%v", out["cred_ref"])
	}
	if out["region"] != "cn-beijing" {
		t.Fatalf("region=%v", out["region"])
	}
	if out["credential_present"] != true {
		t.Fatalf("credential_present=%v", out["credential_present"])
	}

	run("configure", "set", "--profile", "abc-sg", "--cred-ref", "ma-abc-root", "--endpoint", "https://tls-ap-singapore-1.volces.com")
	out = run("configure", "show", "--profile", "abc-sg")
	if out["cred_ref"] != "ma-abc-root" {
		t.Fatalf("cred_ref=%v", out["cred_ref"])
	}
	if out["region"] != "ap-singapore-1" {
		t.Fatalf("region=%v", out["region"])
	}
	if out["credential_present"] != true {
		t.Fatalf("credential_present=%v", out["credential_present"])
	}
}
