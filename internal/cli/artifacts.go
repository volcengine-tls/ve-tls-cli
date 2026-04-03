package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/output"
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

func writeOutputFileToDir(outputFile string, baseDir string, group string, out any, format output.Format) (string, error) {
	if raw, ok := out.(rawBinaryOutput); ok {
		p := strings.TrimSpace(outputFile)
		if p == "" {
			var err error
			p, err = defaultBinaryOutputFile(baseDir, group, raw.Ext)
			if err != nil {
				return "", err
			}
		}
		p = filepath.Clean(p)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			return "", err
		}
		if err := os.WriteFile(p, raw.Data, 0o600); err != nil {
			return "", err
		}
		return p, nil
	}
	p := strings.TrimSpace(outputFile)
	if p == "" {
		var err error
		p, err = defaultOutputFile(baseDir, group, format)
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

func writeTextFileToDir(outputFile string, baseDir string, group string, s string) (string, error) {
	p := strings.TrimSpace(outputFile)
	if p == "" {
		var err error
		p, err = defaultOutputFile(baseDir, group, output.FormatJSON)
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

func defaultOutputFile(baseDir string, group string, format output.Format) (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := strings.TrimSpace(baseDir)
	if dir == "" {
		dir = filepath.Join(wd, ".volclog", "output")
	} else if !filepath.IsAbs(dir) {
		dir = filepath.Join(wd, dir)
	}
	ext := "json"
	if format == output.FormatJSONL {
		ext = "jsonl"
	} else if format == output.FormatTable {
		ext = "txt"
	}
	g := strings.TrimSpace(group)
	if g == "" {
		return "", errors.New("empty group")
	}
	name := g + "-" + time.Now().UTC().Format("2006-01-02T15-04-05.000Z") + "." + ext
	return filepath.Join(dir, name), nil
}

func defaultBinaryOutputFile(baseDir string, group string, ext string) (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := strings.TrimSpace(baseDir)
	if dir == "" {
		dir = filepath.Join(wd, ".volclog", "output")
	} else if !filepath.IsAbs(dir) {
		dir = filepath.Join(wd, dir)
	}
	g := strings.TrimSpace(group)
	if g == "" {
		return "", errors.New("empty group")
	}
	suffix := strings.TrimSpace(ext)
	if suffix == "" {
		suffix = ".bin"
	}
	if !strings.HasPrefix(suffix, ".") {
		suffix = "." + suffix
	}
	name := g + "-" + time.Now().UTC().Format("2006-01-02T15-04-05.000Z") + suffix
	return filepath.Join(dir, name), nil
}
