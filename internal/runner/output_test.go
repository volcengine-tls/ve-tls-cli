package runner

import "testing"

func TestParseTLSCTLOutput_JSONL(t *testing.T) {
	out := []byte("{\"a\":1}\n{\"b\":2}\n\n")
	v, err := parseTLSCTLOutput(out, "jsonl")
	if err != nil {
		t.Fatal(err)
	}
	a, ok := v.([]any)
	if !ok || len(a) != 2 {
		t.Fatalf("v=%T %v", v, v)
	}
}
