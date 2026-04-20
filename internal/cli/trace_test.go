package cli

import "testing"

func TestNormalizeTraceRedactValue(t *testing.T) {
	cases := map[string]string{
		"":        "on",
		"strict":  "on",
		"default": "on",
		"on":      "on",
		"enabled": "on",
		"off":     "off",
		"false":   "off",
		"weird":   "on",
	}
	for raw, want := range cases {
		if got := normalizeTraceRedactValue(raw); got != want {
			t.Fatalf("raw=%q got=%q want=%q", raw, got, want)
		}
	}
}
