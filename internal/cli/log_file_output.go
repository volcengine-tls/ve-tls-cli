package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/output"
)

type streamedLogFileWriter struct {
	file      *os.File
	tempPath  string
	finalPath string
	format    output.Format
	wroteAny  bool
}

func newStreamedLogFileWriter(outputFile string, baseDir string, group string, format output.Format) (*streamedLogFileWriter, error) {
	if format != output.FormatJSON && format != output.FormatJSONL {
		return nil, errors.New("streamed log file output only supports json or jsonl")
	}
	finalPath, err := resolveOutputFilePath(outputFile, baseDir, group, format)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o700); err != nil {
		return nil, err
	}
	tempFile, err := os.CreateTemp(filepath.Dir(finalPath), "."+filepath.Base(finalPath)+".tmp-*")
	if err != nil {
		return nil, err
	}
	w := &streamedLogFileWriter{
		file:      tempFile,
		tempPath:  tempFile.Name(),
		finalPath: finalPath,
		format:    format,
	}
	if format == output.FormatJSON {
		if _, err := tempFile.Write([]byte("[")); err != nil {
			w.abort()
			return nil, err
		}
	}
	return w, nil
}

func maybeNewStreamedLogFileWriter(ctx *Context, group string) (*streamedLogFileWriter, error) {
	if ctx == nil || ctx.OutputMode != "file" || strings.TrimSpace(ctx.Filter) != "" {
		return nil, nil
	}
	if ctx.Format == output.FormatTable {
		return nil, nil
	}
	return newStreamedLogFileWriter(ctx.OutputFile, ctx.OutputDir, group, ctx.Format)
}

func (w *streamedLogFileWriter) WriteRows(rows []any) error {
	for _, row := range rows {
		if err := w.writeRow(row); err != nil {
			return err
		}
	}
	return nil
}

func (w *streamedLogFileWriter) WriteObjectRows(rows []map[string]any) error {
	for _, row := range rows {
		if err := w.writeRow(row); err != nil {
			return err
		}
	}
	return nil
}

func (w *streamedLogFileWriter) writeRow(row any) error {
	switch w.format {
	case output.FormatJSONL:
		return output.Write(w.file, row, output.FormatJSONL)
	case output.FormatJSON:
		if w.wroteAny {
			if _, err := w.file.Write([]byte(",\n")); err != nil {
				return err
			}
		} else {
			if _, err := w.file.Write([]byte("\n")); err != nil {
				return err
			}
		}
		b, err := marshalNoEscapeLocal(row)
		if err != nil {
			return err
		}
		if _, err := w.file.Write(b); err != nil {
			return err
		}
		w.wroteAny = true
		return nil
	default:
		return errors.New("unsupported streamed output format")
	}
}

func (w *streamedLogFileWriter) Commit() (fileArtifactOutput, error) {
	if w == nil {
		return fileArtifactOutput{}, errors.New("nil file writer")
	}
	if w.file == nil {
		return fileArtifactOutput{}, errors.New("output file is closed")
	}
	if w.format == output.FormatJSON {
		suffix := "]\n"
		if w.wroteAny {
			suffix = "\n]\n"
		}
		if _, err := w.file.Write([]byte(suffix)); err != nil {
			w.abort()
			return fileArtifactOutput{}, err
		}
	}
	if err := w.file.Close(); err != nil {
		w.abort()
		return fileArtifactOutput{}, err
	}
	w.file = nil
	_ = os.Remove(w.finalPath)
	if err := os.Rename(w.tempPath, w.finalPath); err != nil {
		_ = os.Remove(w.tempPath)
		return fileArtifactOutput{}, err
	}
	return fileArtifactOutput{Path: w.finalPath, Format: w.format}, nil
}

func (w *streamedLogFileWriter) abort() {
	if w == nil {
		return
	}
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}
	if strings.TrimSpace(w.tempPath) != "" {
		_ = os.Remove(w.tempPath)
	}
}

func marshalNoEscapeLocal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}
