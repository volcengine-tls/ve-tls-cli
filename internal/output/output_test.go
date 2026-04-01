package output

import (
	"strings"
	"testing"
)

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

func TestApplyFilterJMESPathProjection(t *testing.T) {
	in := map[string]any{
		"Projects": []any{
			map[string]any{"ProjectId": "p1", "ProjectName": "alpha"},
			map[string]any{"ProjectId": "p2", "ProjectName": "beta"},
		},
	}
	out, err := ApplyFilter(in, "Projects[].{ProjectId: ProjectId, ProjectName: ProjectName}")
	if err != nil {
		t.Fatalf("filter error: %v", err)
	}
	items, ok := out.([]any)
	if !ok {
		t.Fatalf("unexpected type: %T", out)
	}
	if len(items) != 2 {
		t.Fatalf("unexpected len: %d", len(items))
	}
	first, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected item type: %T", items[0])
	}
	if first["ProjectId"] != "p1" || first["ProjectName"] != "alpha" {
		t.Fatalf("unexpected first item: %#v", first)
	}
}

func TestApplyFilterInvalidJMESPath(t *testing.T) {
	_, err := ApplyFilter(map[string]any{"Projects": []any{}}, "Projects[].")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.HasPrefix(err.Error(), "invalid jmes-filter expression:") {
		t.Fatalf("unexpected error: %v", err)
	}
}
