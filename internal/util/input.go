package util

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/jsonx"
)

func ReadMaybeFile(s string) ([]byte, error) {
	v := strings.TrimSpace(s)
	if v == "" {
		return []byte{}, nil
	}
	if looksLikeInlineJSON(v) {
		return []byte(v), nil
	}
	if v == "-" {
		return io.ReadAll(os.Stdin)
	}
	if strings.HasPrefix(v, "file://") {
		p := strings.TrimPrefix(v, "file://")
		if p == "" {
			return nil, errors.New("empty file path")
		}
		b, err := os.ReadFile(filepath.Clean(p))
		if err != nil {
			return nil, err
		}
		return b, nil
	}
	if b, ok, err := readLocalFileIfExists(v); ok || err != nil {
		return b, err
	}
	if looksLikeLocalFilePath(v) {
		return nil, errors.New("file not found: " + filepath.Clean(v))
	}
	return []byte(v), nil
}

func readLocalFileIfExists(path string) ([]byte, bool, error) {
	p := filepath.Clean(path)
	fi, err := os.Stat(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if fi.IsDir() {
		return nil, false, errors.New("path is a directory: " + p)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, false, err
	}
	return b, true, nil
}

func looksLikeInlineJSON(v string) bool {
	if v == "" {
		return false
	}
	switch v[0] {
	case '{', '[', '"':
		return true
	default:
		return false
	}
}

func looksLikeLocalFilePath(v string) bool {
	if v == "" {
		return false
	}
	if strings.Contains(v, "/") || strings.Contains(v, `\`) {
		return true
	}
	if strings.HasPrefix(v, ".") || strings.HasPrefix(v, "~") {
		return true
	}
	switch strings.ToLower(filepath.Ext(v)) {
	case ".json", ".jsonl", ".ndjson", ".yaml", ".yml", ".txt", ".log":
		return true
	default:
		return false
	}
}

func MustJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}

func UnmarshalJSON(b []byte) (any, error) {
	if len(bytesTrimSpace(b)) == 0 {
		return map[string]any{}, nil
	}
	v, err := jsonx.Decode(b)
	if err != nil {
		return nil, err
	}
	return v, nil
}

func ReadStringMaybeFile(s string) (string, error) {
	b, err := ReadMaybeFile(s)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func ReadStringListMaybeFile(s string) ([]string, error) {
	b, err := ReadMaybeFile(s)
	if err != nil {
		return nil, err
	}
	b = bytesTrimSpace(b)
	if len(b) == 0 {
		return []string{}, nil
	}
	if b[0] == '[' {
		var a []any
		if err := jsonx.Unmarshal(b, &a); err != nil {
			return nil, err
		}
		out := make([]string, 0, len(a))
		for _, v := range a {
			s, ok := v.(string)
			if !ok {
				return nil, errors.New("json array must contain strings")
			}
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		return out, nil
	}
	lines := strings.Split(string(b), "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			out = append(out, l)
		}
	}
	return out, nil
}

func ReadJSONValueMaybeFile(s string) (any, error) {
	b, err := ReadMaybeFile(s)
	if err != nil {
		return nil, err
	}
	if len(bytesTrimSpace(b)) == 0 {
		return map[string]any{}, nil
	}
	return UnmarshalJSON(b)
}

func ReadJSONObjectMaybeFile(s string) (map[string]any, error) {
	v, err := ReadJSONValueMaybeFile(s)
	if err != nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("json must be object")
	}
	return m, nil
}

func ReadJSONArrayMaybeFile(s string) ([]any, error) {
	v, err := ReadJSONValueMaybeFile(s)
	if err != nil {
		return nil, err
	}
	a, ok := v.([]any)
	if !ok {
		return nil, errors.New("json must be array")
	}
	return a, nil
}

func bytesTrimSpace(b []byte) []byte {
	i := 0
	j := len(b)
	for i < j {
		if b[i] == ' ' || b[i] == '\n' || b[i] == '\r' || b[i] == '\t' {
			i++
			continue
		}
		break
	}
	for j > i {
		if b[j-1] == ' ' || b[j-1] == '\n' || b[j-1] == '\r' || b[j-1] == '\t' {
			j--
			continue
		}
		break
	}
	return b[i:j]
}
