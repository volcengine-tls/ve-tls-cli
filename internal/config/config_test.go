package config

import "testing"

func TestDefaultEndpointForRegion(t *testing.T) {
	got := DefaultEndpointForRegion("cn-beijing")
	if got != "https://tls-cn-beijing.volces.com" {
		t.Fatalf("unexpected endpoint: %q", got)
	}
}
