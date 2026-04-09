package tlsapi

import "testing"

func TestResolveSigningCredentials_PrefersExplicit(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "env-ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "env-sk")
	t.Setenv("VOLCENGINE_TOKEN", "env-token")

	creds, err := resolveSigningCredentials("cn-beijing", "TLS", "arg-ak", "arg-sk", "arg-token")
	if err != nil {
		t.Fatalf("resolveSigningCredentials error: %v", err)
	}
	if creds.AccessKeyID != "arg-ak" || creds.SecretAccessKey != "arg-sk" || creds.SessionToken != "arg-token" {
		t.Fatalf("unexpected explicit creds: %+v", creds)
	}
}

func TestResolveSigningCredentials_FallsBackToEnv(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "env-ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "env-sk")
	t.Setenv("VOLCENGINE_TOKEN", "env-token")

	creds, err := resolveSigningCredentials("cn-beijing", "TLS", "", "", "")
	if err != nil {
		t.Fatalf("resolveSigningCredentials error: %v", err)
	}
	if creds.AccessKeyID != "env-ak" || creds.SecretAccessKey != "env-sk" || creds.SessionToken != "env-token" {
		t.Fatalf("unexpected env creds: %+v", creds)
	}
}

func TestResolveSigningCredentials_RequiresKeyPair(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "")
	t.Setenv("VOLCENGINE_TOKEN", "")

	if _, err := resolveSigningCredentials("cn-beijing", "TLS", "", "", ""); err == nil {
		t.Fatalf("expected missing credential error")
	}
}
