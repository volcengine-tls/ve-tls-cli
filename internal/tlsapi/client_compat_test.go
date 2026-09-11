package tlsapi

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/volcengine/volc-sdk-golang/base"
)

func TestLegacyAKRequestSigningScopeUnchanged(t *testing.T) {
	client := newLegacyStaticClient(t, "")
	signed := legacyFixedSignRequest(client.Creds)

	const wantAuthorization = "HMAC-SHA256 Credential=legacy-ak/20240314/cn-beijing/TLS/request, SignedHeaders=content-type;host;x-content-sha256;x-date;x-tls-apiversion, Signature=899a022f46e57bcc19afdcccb6c35bffcb7026091e4a2029e93d05cb94fd2978"
	const wantContentSHA256 = "f8e2d052b7bd4663968f9c7f3d0fd21150949d4c63e1ee4e8ffbb3daf4b8b9f6"
	if signed.Authorization != wantAuthorization {
		t.Fatalf("legacy Authorization fixture changed:\n got: %s\nwant: %s", signed.Authorization, wantAuthorization)
	}
	if signed.XContentSha256 != wantContentSHA256 {
		t.Fatalf("legacy X-Content-Sha256 fixture changed: got %q, want %q", signed.XContentSha256, wantContentSHA256)
	}
	const wantScope = "Credential=legacy-ak/20240314/cn-beijing/TLS/request"
	if !strings.Contains(signed.Authorization, wantScope) {
		t.Fatalf("legacy credential scope missing %q from %q", wantScope, signed.Authorization)
	}
	if client.Service != "TLS" || client.Creds.Service != "TLS" {
		t.Fatalf("legacy service changed: client=%q credentials=%q", client.Service, client.Creds.Service)
	}
}

func TestLegacyManualSTSSignsSecurityToken(t *testing.T) {
	const token = "legacy-session-token"
	client := newLegacyStaticClient(t, token)
	signed := legacyFixedSignRequest(client.Creds)

	const wantAuthorization = "HMAC-SHA256 Credential=legacy-ak/20240314/cn-beijing/TLS/request, SignedHeaders=content-type;host;x-content-sha256;x-date;x-security-token;x-tls-apiversion, Signature=be7f108bf24259f18d1e56346571ae3f08defa257eeb269ecfbc626a65f2122c"
	if signed.XSecurityToken != token {
		t.Fatalf("legacy X-Security-Token=%q, want configured token", signed.XSecurityToken)
	}
	if signed.Authorization != wantAuthorization {
		t.Fatalf("legacy STS Authorization fixture changed:\n got: %s\nwant: %s", signed.Authorization, wantAuthorization)
	}
	if !strings.Contains(signed.Authorization, "x-security-token") {
		t.Fatalf("legacy STS token must remain part of SignedHeaders: %q", signed.Authorization)
	}
}

func TestLegacyEndpointNormalizationAndTimeoutUnchanged(t *testing.T) {
	for _, tc := range []struct {
		name         string
		endpoint     string
		timeout      time.Duration
		wantEndpoint string
		wantTimeout  time.Duration
	}{
		{
			name:         "default scheme and timeout",
			endpoint:     " tls-cn-beijing.volces.com/// ",
			wantEndpoint: "https://tls-cn-beijing.volces.com",
			wantTimeout:  60 * time.Second,
		},
		{
			name:         "explicit scheme and timeout",
			endpoint:     " http://localhost:8080/ ",
			timeout:      7 * time.Second,
			wantEndpoint: "http://localhost:8080",
			wantTimeout:  7 * time.Second,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, err := New(tc.endpoint, " cn-beijing ", "legacy", "legacy-ak", "legacy-sk", "", tc.timeout)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if client.Endpoint != tc.wantEndpoint {
				t.Fatalf("endpoint=%q, want %q", client.Endpoint, tc.wantEndpoint)
			}
			if client.Timeout != tc.wantTimeout || client.HTTP.Timeout != tc.wantTimeout {
				t.Fatalf("timeouts=(%s, %s), want %s", client.Timeout, client.HTTP.Timeout, tc.wantTimeout)
			}
			if client.Region != "cn-beijing" || client.Service != "TLS" {
				t.Fatalf("legacy signing scope inputs changed: region=%q service=%q", client.Region, client.Service)
			}
		})
	}
}

func newLegacyStaticClient(t *testing.T, token string) *Client {
	t.Helper()
	client, err := New(
		"https://tls-cn-beijing.volces.com/",
		"cn-beijing",
		"legacy",
		"legacy-ak",
		"legacy-sk",
		token,
		0,
	)
	if err != nil {
		t.Fatalf("New legacy static client: %v", err)
	}
	return client
}

func legacyFixedSignRequest(credentials base.Credentials) base.SignRequest {
	return base.GetSignRequest(base.RequestParam{
		Body:   []byte(`{"TopicId":"topic-legacy"}`),
		Method: http.MethodPost,
		Date:   time.Date(2024, time.March, 14, 15, 9, 26, 0, time.UTC),
		Path:   "/PutLogs",
		Host:   "tls-cn-beijing.volces.com",
		QueryList: url.Values{
			"topic_id": {"topic-legacy"},
		},
		Headers: http.Header{
			"Content-Type":     {"application/json"},
			"X-Tls-Apiversion": {"0.3.0"},
		},
	}, credentials)
}
