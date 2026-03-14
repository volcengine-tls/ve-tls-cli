package signv4

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestSignSetsHeaders(t *testing.T) {
	u, _ := url.Parse("https://tls-cn-beijing.volces.com/DescribeProject?ProjectId=proj-1")
	req := &http.Request{
		Method: "GET",
		URL:    u,
		Header: http.Header{},
		Host:   u.Host,
	}
	creds := Credentials{
		AccessKeyID:     "ak_test_123",
		SecretAccessKey: "sk_test_456",
		Region:          "cn-beijing",
		Service:         "TLS",
	}
	now := time.Date(2026, 3, 14, 0, 0, 0, 0, time.UTC)

	if err := Sign(req, creds, now); err != nil {
		t.Fatalf("sign error: %v", err)
	}
	if req.Header.Get("X-Date") != "20260314T000000Z" {
		t.Fatalf("unexpected X-Date: %q", req.Header.Get("X-Date"))
	}
	if req.Header.Get("X-Content-Sha256") == "" {
		t.Fatalf("missing X-Content-Sha256")
	}
	auth := req.Header.Get("Authorization")
	if auth == "" {
		t.Fatalf("missing Authorization")
	}
	if !strings.Contains(auth, "Credential="+creds.AccessKeyID+"/20260314/"+creds.Region+"/"+creds.Service+"/request") {
		t.Fatalf("unexpected Authorization: %q", auth)
	}
	if !strings.Contains(auth, "SignedHeaders=") || !strings.Contains(auth, "Signature=") {
		t.Fatalf("unexpected Authorization fields: %q", auth)
	}
}
