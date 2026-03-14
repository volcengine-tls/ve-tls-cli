package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type Format string

const (
	FormatJSON  Format = "json"
	FormatJSONL Format = "jsonl"
)

func ParseFormat(s string) (Format, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "json":
		return FormatJSON, nil
	case "jsonl":
		return FormatJSONL, nil
	default:
		return "", errors.New("unsupported output: " + s)
	}
}

func Write(w io.Writer, v any, format Format) error {
	switch format {
	case FormatJSON:
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return err
		}
		_, err = w.Write(append(b, '\n'))
		return err
	case FormatJSONL:
		switch vv := v.(type) {
		case []any:
			for _, item := range vv {
				b, err := json.Marshal(item)
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
				b, err := json.Marshal(item)
				if err != nil {
					return err
				}
				if _, err := w.Write(append(b, '\n')); err != nil {
					return err
				}
			}
			return nil
		default:
			b, err := json.Marshal(v)
			if err != nil {
				return err
			}
			_, err = w.Write(append(b, '\n'))
			return err
		}
	default:
		return errors.New("unsupported output format")
	}
}

func ApplyFilter(v any, expr string) (any, error) {
	e := strings.TrimSpace(expr)
	if e == "" {
		return v, nil
	}
	parts, err := parsePath(e)
	if err != nil {
		return nil, err
	}
	cur := v
	for _, p := range parts {
		switch step := p.(type) {
		case string:
			m, ok := cur.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("filter expects object at %q", step)
			}
			cur, ok = m[step]
			if !ok {
				return nil, fmt.Errorf("filter missing key %q", step)
			}
		case int:
			a, ok := cur.([]any)
			if !ok {
				return nil, fmt.Errorf("filter expects array at index %d", step)
			}
			if step < 0 || step >= len(a) {
				return nil, fmt.Errorf("filter index out of range: %d", step)
			}
			cur = a[step]
		default:
			return nil, errors.New("invalid filter step")
		}
	}
	return cur, nil
}

func parsePath(expr string) ([]any, error) {
	var parts []any
	var buf bytes.Buffer
	for i := 0; i < len(expr); i++ {
		ch := expr[i]
		switch ch {
		case '.':
			if buf.Len() == 0 {
				continue
			}
			parts = append(parts, buf.String())
			buf.Reset()
		case '[':
			if buf.Len() > 0 {
				parts = append(parts, buf.String())
				buf.Reset()
			}
			j := strings.IndexByte(expr[i:], ']')
			if j < 0 {
				return nil, errors.New("unclosed index")
			}
			raw := expr[i+1 : i+j]
			n, err := strconv.Atoi(raw)
			if err != nil {
				return nil, errors.New("invalid index: " + raw)
			}
			parts = append(parts, n)
			i = i + j
		default:
			buf.WriteByte(ch)
		}
	}
	if buf.Len() > 0 {
		parts = append(parts, buf.String())
	}
	if len(parts) == 0 {
		return nil, errors.New("empty filter")
	}
	return parts, nil
}
