package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultEndpointForRegion(t *testing.T) {
	got := DefaultEndpointForRegion("cn-beijing")
	if got != "https://tls-cn-beijing.volces.com" {
		t.Fatalf("unexpected endpoint: %q", got)
	}
}

func TestEffectiveProfile_ResolveCredRefRequiresExplicitRegion(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "")
	t.Setenv("VOLCENGINE_REGION", "")
	t.Setenv("VOLCENGINE_ENDPOINT", "")

	cfg := DefaultConfig()
	cfg.PutCred("ma-abc-root", Credential{AccessKeyID: "ak", SecretAccessKey: "sk"})
	cfg.PutProfile("p1", Profile{
		CredRef:  "ma-abc-root",
		Endpoint: "https://tls-cn-beijing.volces.com",
	})
	_, err := EffectiveProfile(cfg, "p1", ProfileDefaults{})
	if err == nil || err.Error() != "missing region" {
		t.Fatalf("expected missing region, got %v", err)
	}
}

func TestEffectiveProfile_EnvEndpointRequiresExplicitRegion(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "env-ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "env-sk")
	t.Setenv("VOLCENGINE_REGION", "")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")

	_, err := EffectiveProfile(DefaultConfig(), "", ProfileDefaults{})
	if err == nil || err.Error() != "missing region" {
		t.Fatalf("expected missing region, got %v", err)
	}
}

func TestResolveProfileCredentialStatus_UsesCredRef(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PutCred("shared", Credential{AccessKeyID: "ak", SecretAccessKey: "sk"})
	status := ResolveProfileCredentialStatus(cfg, Profile{
		CredRef: "shared",
	})
	if !status.Present || !status.AK || !status.SK {
		t.Fatalf("unexpected status: %+v", status)
	}
	if status.Source != "profile_cred_ref" {
		t.Fatalf("unexpected source: %q", status.Source)
	}
	if status.AccessKeyID != "ak" || status.SecretAccessKey != "sk" {
		t.Fatalf("unexpected credentials: %+v", status)
	}
}

func TestResolveEnvCredentialStatus(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "env-ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "env-sk")
	t.Setenv("VOLCENGINE_TOKEN", "env-token")
	status := ResolveEnvCredentialStatus()
	if !status.Present || !status.Token {
		t.Fatalf("unexpected status: %+v", status)
	}
	if status.Source != "env" || status.Mode != "sts" {
		t.Fatalf("unexpected env credential source/mode: %+v", status)
	}
}

func TestSaveDoesNotEscapeAngleBrackets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg := DefaultConfig()
	cfg.CurrentProfile = "prod<main>"
	if err := Save(cfg, path); err != nil {
		t.Fatalf("save error: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	out := string(b)
	if strings.Contains(out, `\u003c`) || strings.Contains(out, `\u003e`) {
		t.Fatalf("angle brackets should not be escaped: %q", out)
	}
}
