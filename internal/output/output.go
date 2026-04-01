package output

import (
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/jmespath/go-jmespath"
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
	out, err := jmespath.Search(e, v)
	if err != nil {
		return nil, errors.New("invalid jmes-filter expression: " + err.Error())
	}
	return out, nil
}
