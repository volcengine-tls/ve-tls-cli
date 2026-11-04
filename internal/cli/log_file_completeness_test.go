package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/execution"
	"github.com/volcengine-tls/ve-tls-cli/internal/output"
)

func TestStreamedLogPartialArtifacts(t *testing.T) {
	for _, format := range []output.Format{output.FormatJSON, output.FormatJSONL} {
		t.Run(string(format), func(t *testing.T) {
			finalPath := filepath.Join(t.TempDir(), "export."+string(format))
			if err := os.WriteFile(finalPath, []byte("existing final"), 0o600); err != nil {
				t.Fatal(err)
			}
			state := &execution.Completeness{
				StopReason: "max_pages", PageCount: 1, ItemCount: 99, Resumable: true,
				Next: &execution.Continuation{OperationID: "log.search", Input: map[string]any{"context": "next"}},
			}
			first := newCompletenessTestWriter(t, finalPath, format)
			if err := first.WriteRows([]any{map[string]any{"id": "a"}, map[string]any{"id": "b"}}); err != nil {
				t.Fatal(err)
			}
			artifact, err := first.CommitWithCompleteness(state)
			if err != nil {
				t.Fatal(err)
			}
			first.abort()
			if artifact.Path == finalPath || filepath.Dir(artifact.Path) != filepath.Dir(finalPath) ||
				filepath.Ext(artifact.Path) != filepath.Ext(finalPath) || !strings.Contains(filepath.Base(artifact.Path), ".partial-") {
				t.Fatalf("invalid partial path: %q", artifact.Path)
			}
			if artifact.Completeness == nil || artifact.Completeness.Complete || artifact.Completeness.ItemCount != 2 ||
				artifact.Completeness.PageCount != 1 || !artifact.Completeness.Resumable || artifact.Completeness.Next == nil {
				t.Fatalf("invalid completeness: %#v", artifact.Completeness)
			}
			if state.ItemCount != 99 {
				t.Fatalf("caller state mutated: %#v", state)
			}
			assertCompletenessTestRows(t, artifact, "a", "b")
			assertCompletenessTestMetadata(t, artifact, 2)
			firstData := readCompletenessTestFile(t, artifact.Path)
			firstMeta := readCompletenessTestFile(t, artifact.MetadataPath)

			second := newCompletenessTestWriter(t, finalPath, format)
			if err := second.WriteRows([]any{map[string]any{"id": "c"}}); err != nil {
				t.Fatal(err)
			}
			other, err := second.CommitWithCompleteness(state)
			if err != nil {
				t.Fatal(err)
			}
			second.abort()
			if other.Path == artifact.Path {
				t.Fatalf("partial path reused: %q", other.Path)
			}
			assertCompletenessTestRows(t, other, "c")
			assertCompletenessTestMetadata(t, other, 1)
			if !bytes.Equal(firstData, readCompletenessTestFile(t, artifact.Path)) ||
				!bytes.Equal(firstMeta, readCompletenessTestFile(t, artifact.MetadataPath)) {
				t.Fatal("earlier partial artifact was changed")
			}
			if got := string(readCompletenessTestFile(t, finalPath)); got != "existing final" {
				t.Fatalf("final path was changed: %q", got)
			}
		})
	}
}

func TestStreamedLogCompleteArtifactCompatibility(t *testing.T) {
	for _, tc := range []struct {
		format output.Format
		want   string
	}{
		{output.FormatJSON, "[\n{\"id\":\"<a>\"},\n{\"id\":\"b\"}\n]\n"},
		{output.FormatJSONL, "{\"id\":\"<a>\"}\n{\"id\":\"b\"}\n"},
	} {
		t.Run(string(tc.format), func(t *testing.T) {
			finalPath := filepath.Join(t.TempDir(), "export."+string(tc.format))
			if err := os.WriteFile(finalPath, []byte("old"), 0o600); err != nil {
				t.Fatal(err)
			}
			writer := newCompletenessTestWriter(t, finalPath, tc.format)
			if err := writer.WriteObjectRows([]map[string]any{{"id": "<a>"}, {"id": "b"}}); err != nil {
				t.Fatal(err)
			}
			state := &execution.Completeness{Complete: true, StopReason: "complete", PageCount: 1, ItemCount: 9}
			artifact, err := writer.CommitWithCompleteness(state)
			if err != nil {
				t.Fatal(err)
			}
			writer.abort()
			if artifact.Path != finalPath || artifact.Format != tc.format || artifact.MetadataPath != "" ||
				artifact.Completeness == nil || !artifact.Completeness.Complete || artifact.Completeness.ItemCount != 2 || state.ItemCount != 9 {
				t.Fatalf("unexpected artifact: %#v, original state: %#v", artifact, state)
			}
			if got := string(readCompletenessTestFile(t, finalPath)); got != tc.want {
				t.Fatalf("output changed: got %q, want %q", got, tc.want)
			}
			if _, err := os.Stat(finalPath + ".meta.json"); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("unexpected complete sidecar: %v", err)
			}
		})
	}

	finalPath := filepath.Join(t.TempDir(), "legacy.json")
	writer := newCompletenessTestWriter(t, finalPath, output.FormatJSON)
	artifact, err := writer.Commit()
	if err != nil || artifact.Path != finalPath || artifact.Completeness != nil {
		t.Fatalf("legacy Commit: artifact=%#v err=%v", artifact, err)
	}
	if got := string(readCompletenessTestFile(t, finalPath)); got != "[]\n" {
		t.Fatalf("empty JSON output: %q", got)
	}
}

func TestStreamedLogEmptyPartial(t *testing.T) {
	for _, format := range []output.Format{output.FormatJSON, output.FormatJSONL} {
		t.Run(string(format), func(t *testing.T) {
			writer := newCompletenessTestWriter(t, filepath.Join(t.TempDir(), "export."+string(format)), format)
			artifact, err := writer.CommitWithCompleteness(&execution.Completeness{StopReason: "request_error"})
			if err != nil {
				t.Fatal(err)
			}
			writer.abort()
			assertCompletenessTestRows(t, artifact)
			assertCompletenessTestMetadata(t, artifact, 0)
		})
	}
}

func TestStreamedLogJSONLSliceRowCompatibility(t *testing.T) {
	finalPath := filepath.Join(t.TempDir(), "export.jsonl")
	writer := newCompletenessTestWriter(t, finalPath, output.FormatJSONL)
	if err := writer.WriteRows([]any{[]any{map[string]any{"id": "a"}, map[string]any{"id": "b"}}}); err != nil {
		t.Fatal(err)
	}
	artifact, err := writer.CommitWithCompleteness(&execution.Completeness{Complete: true})
	if err != nil {
		t.Fatal(err)
	}
	if writer.RowsWritten() != 2 || artifact.Completeness == nil || artifact.Completeness.ItemCount != 2 {
		t.Fatalf("slice row count: writer=%d artifact=%#v", writer.RowsWritten(), artifact)
	}
	assertCompletenessTestRows(t, artifact, "a", "b")
}

func TestStreamedLogWriteErrorsKeepCompleteRows(t *testing.T) {
	for _, format := range []output.Format{output.FormatJSON, output.FormatJSONL} {
		t.Run(string(format), func(t *testing.T) {
			finalPath := filepath.Join(t.TempDir(), "export."+string(format))
			writer := newCompletenessTestWriter(t, finalPath, format)
			if err := writer.WriteRows([]any{map[string]any{"id": "a"}}); err != nil {
				t.Fatal(err)
			}
			if err := writer.WriteRows([]any{map[string]any{"bad": make(chan int)}}); err == nil {
				t.Fatal("expected marshal error")
			}
			writer.file = &completenessShortWriteFile{streamedLogFile: writer.file}
			if err := writer.WriteRows([]any{map[string]any{"id": "b"}}); !errors.Is(err, io.ErrShortWrite) {
				t.Fatalf("expected short write, got %v", err)
			}
			if writer.RowsWritten() != 1 {
				t.Fatalf("count after failed row = %d", writer.RowsWritten())
			}
			artifact, err := writer.CommitWithCompleteness(&execution.Completeness{StopReason: "write_error"})
			if err != nil {
				t.Fatal(err)
			}
			writer.abort()
			assertCompletenessTestRows(t, artifact, "a")
			assertCompletenessTestMetadata(t, artifact, 1)
		})
	}
}

func TestStreamedLogFailedRollbackCannotPublishArtifact(t *testing.T) {
	finalPath := filepath.Join(t.TempDir(), "export.json")
	writer := newCompletenessTestWriter(t, finalPath, output.FormatJSON)
	if err := writer.WriteRows([]any{map[string]any{"id": "a"}}); err != nil {
		t.Fatal(err)
	}
	writer.file = &completenessBadRollbackFile{completenessShortWriteFile: completenessShortWriteFile{streamedLogFile: writer.file}}
	if err := writer.WriteRows([]any{map[string]any{"id": "b"}}); err == nil {
		t.Fatal("expected rollback error")
	}
	partialPath := writer.tempPath
	artifact, err := writer.CommitWithCompleteness(&execution.Completeness{StopReason: "write_error"})
	if err == nil || artifact.Path != "" {
		t.Fatalf("published corrupt artifact: %#v, error: %v", artifact, err)
	}
	writer.abort()
	if _, err := os.Stat(partialPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("corrupt working file was not cleaned up: %v", err)
	}
}

func TestStreamedLogCommitFailureRetainsPartial(t *testing.T) {
	finalPath := filepath.Join(t.TempDir(), "export.json")
	if err := os.WriteFile(finalPath, []byte("existing final"), 0o600); err != nil {
		t.Fatal(err)
	}
	writer := newCompletenessTestWriter(t, finalPath, output.FormatJSON)
	if err := writer.WriteRows([]any{map[string]any{"id": "a"}}); err != nil {
		t.Fatal(err)
	}
	renameErr := errors.New("rename failed")
	writer.rename = func(_, _ string) error { return renameErr }
	artifact, err := writer.CommitWithCompleteness(&execution.Completeness{Complete: true, ItemCount: 1})
	if !errors.Is(err, renameErr) || artifact.Path == "" || artifact.Path == finalPath || artifact.Completeness == nil || artifact.Completeness.Complete {
		t.Fatalf("failed rename result: artifact=%#v err=%v", artifact, err)
	}
	writer.abort()
	assertCompletenessTestRows(t, artifact, "a")
	assertCompletenessTestMetadata(t, artifact, 1)
	if got := string(readCompletenessTestFile(t, finalPath)); got != "existing final" {
		t.Fatalf("rename failure changed original: %q", got)
	}
}

func TestStreamedLogFinalizeFailureRetainsPartial(t *testing.T) {
	finalPath := filepath.Join(t.TempDir(), "export.json")
	writer := newCompletenessTestWriter(t, finalPath, output.FormatJSON)
	if err := writer.WriteRows([]any{map[string]any{"id": "a"}}); err != nil {
		t.Fatal(err)
	}
	writer.file = &completenessShortWriteFile{streamedLogFile: writer.file}
	artifact, err := writer.CommitWithCompleteness(&execution.Completeness{Complete: true})
	if !errors.Is(err, io.ErrShortWrite) || artifact.Path == "" || artifact.Completeness == nil || artifact.Completeness.Complete {
		t.Fatalf("failed finalization result: artifact=%#v err=%v", artifact, err)
	}
	writer.abort()
	assertCompletenessTestRows(t, artifact, "a")
	assertCompletenessTestMetadata(t, artifact, 1)
	if _, err := os.Stat(finalPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed commit published final path: %v", err)
	}
}

func TestStreamedLogMetadataFailureRetainsData(t *testing.T) {
	finalPath := filepath.Join(t.TempDir(), "export.jsonl")
	writer := newCompletenessTestWriter(t, finalPath, output.FormatJSONL)
	if err := writer.WriteRows([]any{map[string]any{"id": "a"}}); err != nil {
		t.Fatal(err)
	}
	sidecarPath := writer.tempPath + ".meta.json"
	if err := os.WriteFile(sidecarPath, []byte("existing metadata"), 0o600); err != nil {
		t.Fatal(err)
	}
	artifact, err := writer.CommitWithCompleteness(&execution.Completeness{StopReason: "request_error"})
	if err == nil || artifact.Path == "" || artifact.MetadataPath != "" || artifact.Completeness == nil || artifact.Completeness.Complete {
		t.Fatalf("metadata failure result: artifact=%#v err=%v", artifact, err)
	}
	writer.abort()
	assertCompletenessTestRows(t, artifact, "a")
	if got := string(readCompletenessTestFile(t, sidecarPath)); got != "existing metadata" {
		t.Fatalf("existing metadata was changed: %q", got)
	}
}

type completenessShortWriteFile struct {
	streamedLogFile
	fired bool
}

func (f *completenessShortWriteFile) Write(p []byte) (int, error) {
	if !f.fired {
		f.fired = true
		return f.streamedLogFile.Write(p[:len(p)/2])
	}
	return f.streamedLogFile.Write(p)
}

type completenessBadRollbackFile struct{ completenessShortWriteFile }

func (f *completenessBadRollbackFile) Truncate(int64) error { return errors.New("truncate failed") }

func newCompletenessTestWriter(t *testing.T, path string, format output.Format) *streamedLogFileWriter {
	t.Helper()
	writer, err := newStreamedLogFileWriter(path, "", "log", format)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(writer.abort)
	return writer
}

func readCompletenessTestFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func assertCompletenessTestRows(t *testing.T, artifact fileArtifactOutput, want ...string) {
	t.Helper()
	b := readCompletenessTestFile(t, artifact.Path)
	var rows []map[string]any
	if artifact.Format == output.FormatJSON {
		if err := json.Unmarshal(b, &rows); err != nil {
			t.Fatalf("invalid JSON artifact: %v: %q", err, b)
		}
	} else {
		if len(b) > 0 && b[len(b)-1] != '\n' {
			t.Fatalf("JSONL missing final newline: %q", b)
		}
		for _, line := range bytes.Split(bytes.TrimSuffix(b, []byte("\n")), []byte("\n")) {
			if len(line) == 0 && len(b) == 0 {
				continue
			}
			var row map[string]any
			if err := json.Unmarshal(line, &row); err != nil {
				t.Fatalf("invalid JSONL row: %v: %q", err, line)
			}
			rows = append(rows, row)
		}
	}
	if len(rows) != len(want) {
		t.Fatalf("got %d rows, want %d: %q", len(rows), len(want), b)
	}
	for i, id := range want {
		if rows[i]["id"] != id {
			t.Fatalf("row %d: got %v, want %q", i, rows[i]["id"], id)
		}
	}
}

func assertCompletenessTestMetadata(t *testing.T, artifact fileArtifactOutput, count int) {
	t.Helper()
	if artifact.MetadataPath != artifact.Path+".meta.json" {
		t.Fatalf("sidecar path: %q", artifact.MetadataPath)
	}
	var metadata struct {
		SchemaVersion int                    `json:"schemaVersion"`
		Completeness  execution.Completeness `json:"completeness"`
		Data          struct {
			Path      string        `json:"path"`
			Format    output.Format `json:"format"`
			ItemCount int           `json:"itemCount"`
			SizeBytes int64         `json:"sizeBytes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(readCompletenessTestFile(t, artifact.MetadataPath), &metadata); err != nil {
		t.Fatalf("invalid metadata: %v", err)
	}
	info, err := os.Stat(artifact.Path)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.SchemaVersion != 1 || metadata.Completeness.Complete || metadata.Completeness.ItemCount != count ||
		metadata.Data.Path != artifact.Path || metadata.Data.Format != artifact.Format || metadata.Data.ItemCount != count || metadata.Data.SizeBytes != info.Size() {
		t.Fatalf("invalid metadata: %#v", metadata)
	}
}
