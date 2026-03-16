package runner

import (
	"testing"
	"time"
)

func TestConfirmToken_GenerateAndValidate(t *testing.T) {
	secret := []byte("s")
	now := time.Unix(0, 1710378000000*int64(time.Millisecond))
	req := ConfirmRequest{
		Account: "acctA",
		Region:  "cn-beijing",
		Profile: "acctA-cn",
		Action:  "log.export",
	}
	tok, err := GenerateConfirmToken(secret, req, now, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if tok == "" {
		t.Fatal("empty token")
	}
	if err := ValidateConfirmToken(secret, req, tok, now); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if err := ValidateConfirmToken(secret, req, tok, now.Add(6*time.Minute)); err == nil {
		t.Fatal("expected expired")
	}
}
