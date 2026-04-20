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
	return out, nil
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
