package output

import "testing"

func TestApplyFilterPath(t *testing.T) {
	in := map[string]any{
		"a": map[string]any{
			"b": []any{
				map[string]any{"c": 1},
				map[string]any{"c": 2},
			},
		},
	}
	out, err := ApplyFilter(in, "a.b[1].c")
	if err != nil {
		t.Fatalf("filter error: %v", err)
	}
	if out.(int) != 2 {
		t.Fatalf("unexpected result: %#v", out)
	}
}
