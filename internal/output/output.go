package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/jmespath/go-jmespath"
)

type Format string

const (
	FormatJSON  Format = "json"
	FormatJSONL Format = "jsonl"
	FormatTable Format = "table"
)

func ParseFormat(s string) (Format, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "json":
		return FormatJSON, nil
	case "jsonl":
		return FormatJSONL, nil
	case "table":
		return FormatTable, nil
	default:
		return "", errors.New("unsupported output: " + s)
	}
}

func Write(w io.Writer, v any, format Format) error {
	switch format {
	case FormatJSON:
		b, err := marshalIndentNoEscape(v)
		if err != nil {
			return err
		}
		_, err = w.Write(b)
		return err
	case FormatJSONL:
		switch vv := v.(type) {
		case []any:
			for _, item := range vv {
				b, err := marshalNoEscape(item)
				if err != nil {
					return err
				}
				if _, err := w.Write(append(b, '\n')); err != nil {
					return err
				}
			}
			return nil
		case []map[string]any:
			for _, item := range vv {
				b, err := marshalNoEscape(item)
				if err != nil {
					return err
				}
				if _, err := w.Write(append(b, '\n')); err != nil {
					return err
				}
			}
			return nil
		default:
			b, err := marshalNoEscape(v)
			if err != nil {
				return err
			}
			_, err = w.Write(append(b, '\n'))
			return err
		}
	case FormatTable:
		return writeTable(w, v)
	default:
		return errors.New("unsupported output format")
	}
}

func ApplyFilter(v any, expr string) (any, error) {
	e := strings.TrimSpace(expr)
	if e == "" {
		return v, nil
	}
	out, err := jmespath.Search(e, v)
	if err != nil {
		return nil, errors.New("invalid jmes-filter expression: " + err.Error())
	}
	if out == nil {
		if resolvedNilValue(v, e) {
			return nil, nil
		}
		return nil, errors.New(buildNilFilterMessage(v, e))
	}
	return out, nil
}

func resolvedNilValue(raw any, expr string) bool {
	return strings.Contains(diagnosePathMatch(raw, expr), "resolved value: nil")
}

func buildNilFilterMessage(raw any, expr string) string {
	parts := []string{
		"filter matched no value: " + expr,
		"result scope: " + describeScope(raw),
	}
	if keys, ok := scopeKeys(raw); ok {
		parts = append(parts, "available keys: "+formatDiagnosticKeys(keys))
	}
	if pathDiag := diagnosePathMatch(raw, expr); pathDiag != "" {
		parts = append(parts, pathDiag)
	}
	return strings.Join(parts, "; ")
}

func describeScope(v any) string {
	switch vv := v.(type) {
	case nil:
		return "nil"
	case map[string]any:
		return fmt.Sprintf("object(len=%d)", len(vv))
	case []any:
		return fmt.Sprintf("array(len=%d)", len(vv))
	case []map[string]any:
		return fmt.Sprintf("array(len=%d)", len(vv))
	default:
		return fmt.Sprintf("%T", v)
	}
}

func scopeKeys(v any) ([]string, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, false
	}
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys, true
}

func formatDiagnosticKeys(keys []string) string {
	if len(keys) == 0 {
		return "[]"
	}
	const maxKeys = 8
	display := keys
	rest := 0
	if len(display) > maxKeys {
		display = display[:maxKeys]
		rest = len(keys) - maxKeys
	}
	if rest == 0 {
		return "[" + strings.Join(display, ", ") + "]"
	}
	return "[" + strings.Join(display, ", ") + fmt.Sprintf(", ... (+%d more)]", rest)
}

type diagToken struct {
	key   string
	index int
	isIdx bool
}

func diagnosePathMatch(raw any, expr string) string {
	tokens, full := parseDiagTokens(expr)
	if len(tokens) == 0 {
		return ""
	}
	cur := raw
	matched := 0
	for i, tok := range tokens {
		if !tok.isIdx {
			obj, ok := cur.(map[string]any)
			if !ok {
				return "matched prefix: " + formatMatchedPrefix(tokens, matched) + "; matched scope: " + describeScope(cur)
			}
			next, ok := obj[tok.key]
			if !ok {
				return "matched prefix: " + formatMatchedPrefix(tokens, matched) + "; available keys: " + formatDiagnosticKeys(sortedKeysFromMap(obj)) + "; missing segment: " + tok.key
			}
			cur = next
			matched = i + 1
			continue
		}

		arr, ok := asAnySlice(cur)
		if !ok {
			return "matched prefix: " + formatMatchedPrefix(tokens, matched) + "; matched scope: " + describeScope(cur)
		}
		if tok.index < 0 || tok.index >= len(arr) {
			return "matched prefix: " + formatMatchedPrefix(tokens, matched) + "; array len: " + fmt.Sprint(len(arr)) + "; missing segment: " + fmt.Sprintf("[%d]", tok.index)
		}
		cur = arr[tok.index]
		matched = i + 1
	}

	if !full {
		return "matched prefix: " + formatMatchedPrefix(tokens, matched)
	}
	if cur == nil {
		return "matched prefix: " + formatMatchedPrefix(tokens, matched) + "; resolved value: nil"
	}
	return "matched prefix: " + formatMatchedPrefix(tokens, matched)
}

func sortedKeysFromMap(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func asAnySlice(v any) ([]any, bool) {
	switch vv := v.(type) {
	case []any:
		return vv, true
	case []map[string]any:
		out := make([]any, 0, len(vv))
		for _, item := range vv {
			out = append(out, item)
		}
		return out, true
	default:
		return nil, false
	}
}

func formatMatchedPrefix(tokens []diagToken, matched int) string {
	if matched <= 0 {
		return "<root>"
	}
	var b strings.Builder
	for i := 0; i < matched && i < len(tokens); i++ {
		tok := tokens[i]
		if tok.isIdx {
			b.WriteString(fmt.Sprintf("[%d]", tok.index))
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('.')
		}
		b.WriteString(tok.key)
	}
	if b.Len() == 0 {
		return "<root>"
	}
	return b.String()
}

func parseDiagTokens(expr string) ([]diagToken, bool) {
	e := strings.TrimSpace(expr)
	if e == "" {
		return nil, false
	}
	tokens := make([]diagToken, 0, 4)
	i := 0
	for i < len(e) {
		ch := e[i]
		switch {
		case isDiagIdentStart(ch):
			start := i
			i++
			for i < len(e) && isDiagIdentPart(e[i]) {
				i++
			}
			tokens = append(tokens, diagToken{key: e[start:i]})
		case ch == '[':
			i++
			start := i
			for i < len(e) && e[i] >= '0' && e[i] <= '9' {
				i++
			}
			if start == i || i >= len(e) || e[i] != ']' {
				return tokens, false
			}
			idx := 0
			for _, c := range e[start:i] {
				idx = idx*10 + int(c-'0')
			}
			tokens = append(tokens, diagToken{index: idx, isIdx: true})
			i++
		default:
			return tokens, false
		}
		if i >= len(e) {
			break
		}
		if e[i] == '.' {
			i++
			if i >= len(e) {
				return tokens, false
			}
			continue
		}
		if e[i] == '[' {
			continue
		}
		return tokens, false
	}
	return tokens, true
}

func isDiagIdentStart(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_'
}

func isDiagIdentPart(ch byte) bool {
	return isDiagIdentStart(ch) || (ch >= '0' && ch <= '9')
}

func writeTable(w io.Writer, v any) error {
	rows, err := extractTableRows(v)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		_, err := io.WriteString(w, "(no rows)\n")
		return err
	}
	columns := detectTableColumns(rows)
	if len(columns) == 0 {
		return errors.New("table output requires object rows")
	}
	widths := make([]int, len(columns))
	for i, col := range columns {
		widths[i] = len(col)
	}
	cells := make([][]string, 0, len(rows))
	for _, row := range rows {
		line := make([]string, 0, len(columns))
		for i, col := range columns {
			cell := formatTableValue(row[col])
			if len(cell) > widths[i] {
				widths[i] = len(cell)
			}
			line = append(line, cell)
		}
		cells = append(cells, line)
	}
	var buf bytes.Buffer
	writeTableLine(&buf, columns, widths)
	writeTableSeparator(&buf, widths)
	for _, row := range cells {
		writeTableLine(&buf, row, widths)
	}
	_, err = w.Write(buf.Bytes())
	return err
}

func extractTableRows(v any) ([]map[string]any, error) {
	switch vv := v.(type) {
	case []map[string]any:
		return vv, nil
	case []any:
		rows := make([]map[string]any, 0, len(vv))
		for _, item := range vv {
			row, ok := item.(map[string]any)
			if !ok {
				return nil, errors.New("table output requires object rows")
			}
			rows = append(rows, row)
		}
		return rows, nil
	case map[string]any:
		if rows, ok := extractTableRowsFromCollection(vv); ok {
			return rows, nil
		}
		return []map[string]any{vv}, nil
	default:
		return nil, errors.New("table output requires object or object array")
	}
}

func extractTableRowsFromCollection(v map[string]any) ([]map[string]any, bool) {
	for _, key := range []string{"Projects", "Topics", "MetricTopics", "Items", "Results", "Data"} {
		if rows, ok := extractRowSlice(v[key]); ok {
			return rows, true
		}
	}
	for _, value := range v {
		if rows, ok := extractRowSlice(value); ok {
			return rows, true
		}
	}
	return nil, false
}

func extractRowSlice(v any) ([]map[string]any, bool) {
	switch rows := v.(type) {
	case []map[string]any:
		return rows, true
	case []any:
		out := make([]map[string]any, 0, len(rows))
		for _, item := range rows {
			row, ok := item.(map[string]any)
			if !ok {
				return nil, false
			}
			out = append(out, row)
		}
		return out, true
	default:
		return nil, false
	}
}

func detectTableColumns(rows []map[string]any) []string {
	seen := map[string]struct{}{}
	for _, row := range rows {
		for key := range row {
			seen[key] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	preferred := []string{
		"ProjectId", "ProjectName",
		"TopicId", "TopicName",
		"Id", "Name",
		"Region", "Status", "Count", "Total",
	}
	cols := make([]string, 0, len(seen))
	for _, key := range preferred {
		if _, ok := seen[key]; ok {
			cols = append(cols, key)
			delete(seen, key)
		}
	}
	rest := make([]string, 0, len(seen))
	for key := range seen {
		rest = append(rest, key)
	}
	sort.Strings(rest)
	return append(cols, rest...)
}

func formatTableValue(v any) string {
	switch vv := v.(type) {
	case nil:
		return ""
	case string:
		return vv
	case fmt.Stringer:
		return vv.String()
	case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return fmt.Sprint(vv)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(b)
	}
}

func writeTableLine(buf *bytes.Buffer, values []string, widths []int) {
	for i, value := range values {
		if i > 0 {
			buf.WriteString("  ")
		}
		width := widths[i]
		buf.WriteString(value)
		if pad := width - len(value); pad > 0 {
			buf.WriteString(strings.Repeat(" ", pad))
		}
	}
	buf.WriteByte('\n')
}

func writeTableSeparator(buf *bytes.Buffer, widths []int) {
	for i, width := range widths {
		if i > 0 {
			buf.WriteString("  ")
		}
		buf.WriteString(strings.Repeat("-", width))
	}
	buf.WriteByte('\n')
}
