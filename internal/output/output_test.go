package output

import (
	"bytes"
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

func TestApplyFilterNilResultReturnsError(t *testing.T) {
	_, err := ApplyFilter(map[string]any{"Projects": []any{}}, "missing.field")
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, "filter matched no value:") {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{
		"result scope:",
		"available keys:",
		"missing",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected %q in error message, got %q", want, msg)
		}
	}
}

func TestApplyFilterNilResultIncludesMatchedPrefix(t *testing.T) {
	_, err := ApplyFilter(map[string]any{
		"Projects": map[string]any{
			"Total": 2,
		},
	}, "Projects.missing.field")
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "matched prefix: Projects") {
		t.Fatalf("expected matched prefix in error message, got %q", msg)
	}
	if !strings.Contains(msg, "available keys: [Total]") {
		t.Fatalf("expected keys from matched scope in error message, got %q", msg)
	}
}

func TestApplyFilterExistingNullValueReturnsNilWithoutError(t *testing.T) {
	out, err := ApplyFilter(map[string]any{
		"status": "success",
		"error":  nil,
	}, "error")
	if err != nil {
		t.Fatalf("expected nil result to be treated as success, got %v", err)
	}
	if out != nil {
		t.Fatalf("expected nil result, got %#v", out)
	}
}

func TestWriteTableFromCollection(t *testing.T) {
	var buf bytes.Buffer
	err := Write(&buf, map[string]any{
		"Projects": []any{
			map[string]any{"ProjectId": "p1", "ProjectName": "alpha", "Region": "cn-beijing"},
			map[string]any{"ProjectId": "p2", "ProjectName": "beta", "Region": "cn-shanghai"},
		},
		"Total": 2,
	}, FormatTable)
	if err != nil {
		t.Fatalf("write table error: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"ProjectId", "ProjectName", "Region", "p1", "alpha", "cn-beijing"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in table: %q", want, out)
		}
	}
}

func TestWriteTableEmptyRows(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, map[string]any{"Projects": []any{}}, FormatTable); err != nil {
		t.Fatalf("write table error: %v", err)
	}
	if got := buf.String(); got != "(no rows)\n" {
		t.Fatalf("unexpected table output: %q", got)
	}
}

func TestWriteJSONDoesNotEscapeAngleBrackets(t *testing.T) {
	var buf bytes.Buffer
	err := Write(&buf, map[string]any{"hint": "volclog api <group> <action> --describe"}, FormatJSON)
	if err != nil {
		t.Fatalf("write json error: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, `\u003c`) || strings.Contains(out, `\u003e`) {
		t.Fatalf("angle brackets should not be escaped: %q", out)
	}
}

func TestWriteJSONLDoesNotEscapeAngleBrackets(t *testing.T) {
	var buf bytes.Buffer
	err := Write(&buf, []any{map[string]any{"hint": "volclog api <group> <action> --describe"}}, FormatJSONL)
	if err != nil {
		t.Fatalf("write jsonl error: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, `\u003c`) || strings.Contains(out, `\u003e`) {
		t.Fatalf("angle brackets should not be escaped: %q", out)
	}
}
