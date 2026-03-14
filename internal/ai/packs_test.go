package ai

import "testing"

func TestLoadBuiltinPack(t *testing.T) {
	p, err := Load("llm-trace-v1")
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if p.Name == "" || p.Topic.TopicName == "" {
		t.Fatalf("unexpected pack: %#v", p)
	}
}
