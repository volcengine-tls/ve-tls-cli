package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"volclog/internal/output"
)

func attachMeta(out any, tracePath string) any {
	m := map[string]any{
		"trace": map[string]any{
			"path": tracePath,
		},
	}
	if mm, ok := out.(map[string]any); ok {
		if _, exists := mm["meta"]; !exists {
			mm["meta"] = m
			return mm
		}
		return mm
	}
	return map[string]any{
		"data": out,
		"meta": m,
	}
}

func writeOutputFile(outputFile string, group string, out any, format output.Format) (string, error) {
	p := strings.TrimSpace(outputFile)
	if p == "" {
		var err error
		p, err = defaultOutputFile(group, format)
		if err != nil {
			return "", err
		}
	}
	p = filepath.Clean(p)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return "", err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	if err := output.Write(f, out, format); err != nil {
		return "", err
	}
	return p, nil
}

func writeTextFile(outputFile string, group string, s string) (string, error) {
	p := strings.TrimSpace(outputFile)
	if p == "" {
		var err error
		p, err = defaultOutputFile(group, output.FormatJSON)
		if err != nil {
			return "", err
		}
	}
	p = filepath.Clean(p)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return "", err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write([]byte(s)); err != nil {
		return "", err
	}
	return p, nil
}

func defaultOutputFile(group string, format output.Format) (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(wd, ".volclog", "output")
	ext := "json"
	if format == output.FormatJSONL {
		ext = "jsonl"
	}
	g := strings.TrimSpace(group)
	if g == "" {
		return "", errors.New("empty group")
	}
	name := g + "-" + time.Now().UTC().Format("2006-01-02T15-04-05.000Z") + "." + ext
	return filepath.Join(dir, name), nil
}
