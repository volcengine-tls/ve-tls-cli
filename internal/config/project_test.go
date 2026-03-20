package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProjectConfigRejectsCredentials(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".volclog"), 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, ".volclog", "cli.config.json")
	if err := os.WriteFile(p, []byte(`{"access_key_id":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadProjectConfig(dir); err == nil {
		t.Fatalf("expected error")
	}
}

func TestEffectiveProfileUsesDefaults(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PutProfile("default", Profile{
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
	})
	p, err := EffectiveProfile(cfg, "default", ProfileDefaults{
		Endpoint: "https://tls-cn-beijing.volces.com",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Region != "cn-beijing" {
		t.Fatalf("unexpected region: %q", p.Region)
	}
	if p.Endpoint != "https://tls-cn-beijing.volces.com" {
		t.Fatalf("unexpected endpoint: %q", p.Endpoint)
	}
}
