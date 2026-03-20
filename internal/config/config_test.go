package config

import "testing"

func TestDefaultEndpointForRegion(t *testing.T) {
	got := DefaultEndpointForRegion("cn-beijing")
	if got != "https://tls-cn-beijing.volces.com" {
		t.Fatalf("unexpected endpoint: %q", got)
	}
}

func TestDeriveRegionFromEndpoint(t *testing.T) {
	if got := DeriveRegionFromEndpoint("https://tls-cn-beijing.volces.com"); got != "cn-beijing" {
		t.Fatalf("unexpected region: %q", got)
	}
	if got := DeriveRegionFromEndpoint("tls-ap-singapore-1.volces.com"); got != "ap-singapore-1" {
		t.Fatalf("unexpected region: %q", got)
	}
	if got := DeriveRegionFromEndpoint("https://example.com"); got != "" {
		t.Fatalf("unexpected region: %q", got)
	}
}

func TestEffectiveProfile_ResolveCredRefAndDeriveRegion(t *testing.T) {
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
	p, err := EffectiveProfile(cfg, "p1", ProfileDefaults{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Region != "cn-beijing" {
		t.Fatalf("unexpected region: %q", p.Region)
	}
	if p.AccessKeyID != "ak" || p.SecretAccessKey != "sk" {
		t.Fatalf("unexpected creds: %q %q", p.AccessKeyID, p.SecretAccessKey)
	}
}
