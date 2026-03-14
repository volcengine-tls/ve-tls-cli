package util

import "testing"

func TestParsePromTime(t *testing.T) {
	got, err := ParsePromTime("1710374400000")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if got != "1710374400" {
		t.Fatalf("unexpected: %q", got)
	}
}
