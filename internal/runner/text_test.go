package runner

import "testing"

func TestParseTextArgs_Basic(t *testing.T) {
	m, err := ParseTextArgs(`帮我查日志 account=acctA region=cn-beijing action=log.search topic_id=xxx query="timeout error" from_ms=1710374400000 to_ms=1710378000000 dry_run=true`)
	if err != nil {
		t.Fatal(err)
	}
	if m["account"] != "acctA" {
		t.Fatalf("account=%v", m["account"])
	}
	if m["region"] != "cn-beijing" {
		t.Fatalf("region=%v", m["region"])
	}
	if m["action"] != "log.search" {
		t.Fatalf("action=%v", m["action"])
	}
	if m["topic_id"] != "xxx" {
		t.Fatalf("topic_id=%v", m["topic_id"])
	}
	if m["query"] != "timeout error" {
		t.Fatalf("query=%v", m["query"])
	}
	if m["from_ms"] != "1710374400000" {
		t.Fatalf("from_ms=%v", m["from_ms"])
	}
	if m["dry_run"] != "true" {
		t.Fatalf("dry_run=%v", m["dry_run"])
	}
}
