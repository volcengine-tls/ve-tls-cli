package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/output"
)

var errMissingWritableOutputDir = errors.New("missing writable output_dir")

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

type fileArtifactOutput struct {
	Path   string
	Format output.Format
}

func buildOutputArtifact(path string, format output.Format) map[string]any {
	artifact := map[string]any{
		"path":   path,
		"format": string(format),
	}
	if fi, err := os.Stat(path); err == nil {
		artifact["sizeBytes"] = fi.Size()
	}
	return artifact
}

func resolveOutputFilePath(outputFile string, baseDir string, group string, format output.Format) (string, error) {
	p := strings.TrimSpace(outputFile)
	if p == "" {
		var err error
		p, err = defaultOutputFile(baseDir, group, format)
		if err != nil {
			return "", err
		}
	}
	return filepath.Clean(p), nil
}

func writeOutputFileToDir(outputFile string, baseDir string, group string, out any, format output.Format) (string, error) {
	if fileOut, ok := out.(fileArtifactOutput); ok {
		return filepath.Clean(fileOut.Path), nil
	}
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
	p, err := resolveOutputFilePath(outputFile, baseDir, group, format)
	if err != nil {
		return "", err
	}
	if err := writeValueToPath(p, out, format); err != nil {
		return "", err
	}
	return p, nil
}

func writeEnvelopeFileToDir(outputFile string, baseDir string, group string, env map[string]any) (string, error) {
	p, err := resolveOutputFilePath(outputFile, baseDir, group, output.FormatJSON)
	if err != nil {
		return "", err
	}
	if err := writeValueToPath(p, env, output.FormatJSON); err != nil {
		return "", err
	}
	return p, nil
}

func preflightOutputFilePath(outputFile string, baseDir string, group string, format output.Format) error {
	p, err := resolveOutputFilePath(outputFile, baseDir, group, format)
	if err != nil {
		return err
	}
	return os.MkdirAll(filepath.Dir(p), 0o700)
}

func writeValueToPath(path string, out any, format output.Format) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if err := output.Write(f, out, format); err != nil {
		return err
	}
	return nil
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
	dir := strings.TrimSpace(baseDir)
	if dir == "" {
		return "", errMissingWritableOutputDir
	}
	if !filepath.IsAbs(dir) {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
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
	dir := strings.TrimSpace(baseDir)
	if dir == "" {
		return "", errMissingWritableOutputDir
	}
	if !filepath.IsAbs(dir) {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
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

func materializeEnvelopeFile(path string, env map[string]any) error {
	if env == nil {
		return errors.New("nil envelope")
	}
	cleanPath := filepath.Clean(path)
	lastSize := -1
	for i := 0; i < 8; i++ {
		artifact := map[string]any{
			"path":   cleanPath,
			"format": string(output.FormatJSON),
		}
		if lastSize >= 0 {
			artifact["sizeBytes"] = lastSize
		}
		env["artifacts"] = []map[string]any{artifact}
		if err := updateEnvelopeTotalBytes(env); err != nil {
			return err
		}
		b, err := marshalOutputBytes(env, output.FormatJSON)
		if err != nil {
			return err
		}
		size := len(b)
		if size == lastSize {
			return writeBytesToPath(cleanPath, b)
		}
		lastSize = size
	}
	return errors.New("failed to stabilize envelope file size")
}

func marshalOutputBytes(v any, format output.Format) ([]byte, error) {
	var buf bytes.Buffer
	if err := output.Write(&buf, v, format); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeBytesToPath(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}
