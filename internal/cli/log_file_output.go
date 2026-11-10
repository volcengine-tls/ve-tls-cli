package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/execution"
	"github.com/volcengine-tls/ve-tls-cli/internal/output"
)

type streamedLogFile interface {
	io.Writer
	io.Seeker
	Truncate(int64) error
	Close() error
}

type streamedLogFileWriter struct {
	file        streamedLogFile
	tempPath    string
	finalPath   string
	format      output.Format
	rowsWritten int
	invalidErr  error
	rename      func(string, string) error
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
	base := filepath.Base(finalPath)
	ext := filepath.Ext(base)
	// The working file can become the partial artifact without another rename.
	// CreateTemp ensures that neither an older partial nor the final path is replaced.
	tempFile, err := os.CreateTemp(filepath.Dir(finalPath), "."+strings.TrimSuffix(base, ext)+".partial-*"+ext)
	if err != nil {
		return nil, err
	}
	w := &streamedLogFileWriter{
		file:      tempFile,
		tempPath:  tempFile.Name(),
		finalPath: finalPath,
		format:    format,
		rename:    os.Rename,
	}
	if format == output.FormatJSON {
		if err := w.writeAtomic([]byte("[")); err != nil {
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
	if w == nil || w.file == nil {
		return errors.New("output file is closed")
	}
	if w.invalidErr != nil {
		return w.invalidErr
	}
	var encoded []byte
	count := 1
	switch w.format {
	case output.FormatJSONL:
		// output.Write preserves the existing JSONL behavior for slice-valued rows.
		var buf bytes.Buffer
		if err := output.Write(&buf, row, output.FormatJSONL); err != nil {
			return err
		}
		encoded = buf.Bytes()
		count = bytes.Count(encoded, []byte{'\n'})
	case output.FormatJSON:
		b, err := marshalNoEscapeLocal(row)
		if err != nil {
			return err
		}
		if w.rowsWritten > 0 {
			encoded = append([]byte(",\n"), b...)
		} else {
			encoded = append([]byte("\n"), b...)
		}
	default:
		return errors.New("unsupported streamed output format")
	}
	if err := w.writeAtomic(encoded); err != nil {
		return err
	}
	w.rowsWritten += count
	return nil
}

// writeAtomic restores the previous valid boundary if a write is incomplete.
func (w *streamedLogFileWriter) writeAtomic(p []byte) error {
	offset, err := w.file.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	n, err := w.file.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	if err == nil {
		return nil
	}
	truncateErr := w.file.Truncate(offset)
	_, seekErr := w.file.Seek(offset, io.SeekStart)
	if truncateErr != nil || seekErr != nil {
		w.invalidErr = fmt.Errorf("cannot restore the last complete row: %w", errors.Join(truncateErr, seekErr))
		return errors.Join(err, w.invalidErr)
	}
	return err
}

// RowsWritten counts records that have been completely written to the file.
func (w *streamedLogFileWriter) RowsWritten() int {
	if w == nil {
		return 0
	}
	return w.rowsWritten
}

func (w *streamedLogFileWriter) Commit() (fileArtifactOutput, error) {
	return w.CommitWithCompleteness(nil)
}

// CommitWithCompleteness keeps incomplete results at their unique partial path.
// A nonempty artifact is returned with an error if data was retained but its
// final rename or metadata write failed.
func (w *streamedLogFileWriter) CommitWithCompleteness(state *execution.Completeness) (fileArtifactOutput, error) {
	if w == nil {
		return fileArtifactOutput{}, errors.New("nil file writer")
	}
	if w.file == nil {
		return fileArtifactOutput{}, errors.New("output file is closed")
	}
	if w.invalidErr != nil {
		return fileArtifactOutput{}, w.invalidErr
	}
	var commitErr error
	if w.format == output.FormatJSON {
		suffix := "]\n"
		if w.rowsWritten > 0 {
			suffix = "\n]\n"
		}
		if err := w.writeAtomic([]byte(suffix)); err != nil {
			commitErr = fmt.Errorf("finalize output: %w", err)
			// If rollback worked, one retry may still make the stored rows usable.
			if w.invalidErr != nil {
				return fileArtifactOutput{}, commitErr
			}
			if retryErr := w.writeAtomic([]byte(suffix)); retryErr != nil {
				return fileArtifactOutput{}, errors.Join(commitErr, retryErr)
			}
		}
	}
	if err := w.file.Close(); err != nil {
		commitErr = errors.Join(commitErr, fmt.Errorf("close output: %w", err))
	}
	w.file = nil

	var actual *execution.Completeness
	if state != nil {
		copyState := *state
		copyState.ItemCount = w.RowsWritten()
		actual = &copyState
	}
	if (actual == nil || actual.Complete) && commitErr == nil {
		if err := w.rename(w.tempPath, w.finalPath); err == nil {
			w.tempPath = ""
			return fileArtifactOutput{Path: w.finalPath, Format: w.format, Completeness: actual}, nil
		} else {
			commitErr = fmt.Errorf("commit output: %w", err)
		}
	}
	if actual == nil {
		actual = &execution.Completeness{ItemCount: w.RowsWritten()}
	}
	if commitErr != nil && (actual.Complete || actual.StopReason == "") {
		actual.StopReason = "artifact_commit_error"
	}
	actual.Complete = false
	// A failed complete commit is a local delivery failure, not a resumable page.
	if commitErr != nil {
		actual.Resumable = false
		actual.Next = nil
	}
	artifact := fileArtifactOutput{Path: w.tempPath, Format: w.format, Completeness: actual}
	// Detach first: deferred abort must never remove retained data, including if
	// the sidecar cannot be created.
	w.tempPath = ""
	if err := writeStreamedLogMetadata(artifact, w.RowsWritten()); err != nil {
		return artifact, errors.Join(commitErr, err)
	}
	artifact.MetadataPath = artifact.Path + ".meta.json"
	return artifact, commitErr
}

func writeStreamedLogMetadata(artifact fileArtifactOutput, itemCount int) error {
	info, err := os.Stat(artifact.Path)
	if err != nil {
		return fmt.Errorf("stat partial output: %w", err)
	}
	metadata := struct {
		SchemaVersion int                     `json:"schemaVersion"`
		Completeness  *execution.Completeness `json:"completeness"`
		Data          struct {
			Path      string        `json:"path"`
			Format    output.Format `json:"format"`
			ItemCount int           `json:"itemCount"`
			SizeBytes int64         `json:"sizeBytes"`
		} `json:"data"`
	}{SchemaVersion: 1, Completeness: artifact.Completeness}
	metadata.Data.Path = artifact.Path
	metadata.Data.Format = artifact.Format
	metadata.Data.ItemCount = itemCount
	metadata.Data.SizeBytes = info.Size()
	b, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("encode partial metadata: %w", err)
	}
	b = append(b, '\n')
	path := artifact.Path + ".meta.json"
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create partial metadata: %w", err)
	}
	n, writeErr := f.Write(b)
	if writeErr == nil && n != len(b) {
		writeErr = io.ErrShortWrite
	}
	closeErr := f.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("write partial metadata: %w", err)
	}
	return nil
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
