package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestCompiledFilterPreservesNumbersAndSupportsNumericOperations(t *testing.T) {
	in := map[string]any{
		"items": []any{
			map[string]any{"value": json.Number("9007199254740993"), "decimal": json.Number("0.12345678901234567890123456789")},
			map[string]any{"value": json.Number("9007199254740991"), "decimal": json.Number("0.12345678901234567890123456787")},
		},
	}
	filter, err := Compile("items[?value > `9007199254740992`].value")
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	result, err := filter.Apply(in)
	if err != nil {
		t.Fatalf("compiled filter Apply() error = %v", err)
	}
	values, ok := result.([]any)
	if !ok || len(values) != 1 {
		t.Fatalf("result = %#v (type %T), want one value", result, result)
	}
	if number, ok := values[0].(json.Number); !ok || number.String() != "9007199254740993" {
		t.Fatalf("result value = %#v, want original json.Number", values[0])
	}

	sum, err := ApplyFilter(in, "sum(items[].value)")
	if err != nil {
		t.Fatalf("sum filter error = %v", err)
	}
	encoded, err := json.Marshal(sum)
	if err != nil {
		t.Fatalf("marshal sum = %v", err)
	}
	if string(encoded) != "18014398509481984" {
		t.Fatalf("sum = %s, want exact decimal result", encoded)
	}

	decimalResult, err := ApplyFilter(in, "items[?decimal > `0.12345678901234567890123456788`].decimal")
	if err != nil {
		t.Fatalf("long-decimal comparison error = %v", err)
	}
	decimalValues, ok := decimalResult.([]any)
	if !ok || len(decimalValues) != 1 {
		t.Fatalf("long-decimal comparison result = %#v", decimalResult)
	}
	decimal, ok := decimalValues[0].(json.Number)
	if !ok || decimal.String() != "0.12345678901234567890123456789" {
		t.Fatalf("long-decimal value = %#v", decimalValues[0])
	}
}

func TestValidateFilterCompilesBeforeApplication(t *testing.T) {
	if err := Validate("items[].value"); err != nil {
		t.Fatalf("Validate(valid) error = %v", err)
	}
	if err := Validate("items[]."); err == nil {
		t.Fatal("Validate(invalid) error = nil")
	}
}

func TestWritePreservesJSONNumberLexemes(t *testing.T) {
	value := map[string]any{
		"big":     json.Number("9007199254740993"),
		"decimal": json.Number("0.12345678901234567890123456789"),
	}
	for _, format := range []Format{FormatJSON, FormatJSONL} {
		t.Run(string(format), func(t *testing.T) {
			var buf bytes.Buffer
			if err := Write(&buf, value, format); err != nil {
				t.Fatalf("Write() error = %v", err)
			}
			for _, want := range []string{"9007199254740993", "0.12345678901234567890123456789"} {
				if !strings.Contains(buf.String(), want) {
					t.Fatalf("output = %q, missing %q", buf.String(), want)
				}
			}
		})
	}
}

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
