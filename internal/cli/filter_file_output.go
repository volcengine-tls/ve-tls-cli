package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/volcengine-tls/ve-tls-cli/internal/jsonx"
	"github.com/volcengine-tls/ve-tls-cli/internal/output"
)

// Prepare the complete envelope without writing its unfiltered contents. File
// materialization is deferred until all command metadata and the filter result
// are available in renderRunResult.
func prepareFilteredEnvelopeFile(ctx *Context, group string, out any, env map[string]any, outputFile string) error {
	fileOut, hasDataArtifact := out.(fileArtifactOutput)
	fileGroup := group
	if hasDataArtifact {
		fileGroup += "-filtered"
	}
	path, err := resolveOutputFilePath(outputFile, ctx.OutputDir, fileGroup, output.FormatJSON)
	if err != nil {
		return err
	}
	artifacts := []map[string]any{{"path": path, "format": string(output.FormatJSON)}}
	if hasDataArtifact {
		targetPath, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		dataPath, err := filepath.Abs(fileOut.Path)
		if err != nil {
			return err
		}
		if targetPath == dataPath {
			return &os.PathError{Op: "write filtered result", Path: path, Err: errors.New("output path conflicts with an existing data artifact")}
		}
		artifacts = append(artifacts, buildOutputArtifact(fileOut.Path, fileOut.Format))
	}
	env["artifacts"] = artifacts
	return updateEnvelopeTotalBytes(env)
}

func renderFilteredEnvelopeFile(ctx *Context, stdout, stderr io.Writer, group string, rest []string, env map[string]any, exitCode int) int {
	// tool/workflow add metadata after initially constructing the envelope.
	// totalBytes describes that complete envelope, as it does for stdout filters.
	if err := updateEnvelopeTotalBytes(env); err != nil {
		return writeFilterApplicationError(ctx, stdout, stderr, group, rest, env, "file", err)
	}
	// Normalize typed Go slices (including analysis rows and artifacts) to
	// JSON arrays before JMESPath evaluation, preserving numeric precision.
	data, err := marshalOutputBytes(env, output.FormatJSON)
	if err != nil {
		return writeFilterApplicationError(ctx, stdout, stderr, group, rest, env, "file", err)
	}
	filterInput, err := jsonx.Decode(data)
	if err != nil {
		return writeFilterApplicationError(ctx, stdout, stderr, group, rest, env, "file", err)
	}
	filtered, err := applyEnvelopeFilterResult(filterInput, ctx.Filter)
	if err != nil {
		return writeFilterApplicationError(ctx, stdout, stderr, group, rest, env, "file", err)
	}
	path, ok := firstArtifactPath(env["artifacts"])
	if !ok {
		return writeDeliveryFailure(ctx, stdout, stderr, group, errors.New("missing filtered output artifact path"), "artifact", 2)
	}
	data, err = marshalOutputBytes(filtered, output.FormatJSON)
	if err != nil {
		return writeFilterApplicationError(ctx, stdout, stderr, group, rest, env, "file", err)
	}
	if err := writeFilteredOutputFile(path, data); err != nil {
		return writeDeliveryFailure(ctx, stdout, stderr, group, err, "artifact", 2)
	}
	if err := writeOutputNotice(stdout, "结果已写入文件。\n文件: "+path+"\n"); err != nil {
		return writeDeliveryFailureToStderr(ctx, stderr, group, err, env)
	}
	return exitCode
}

// Use a sibling temporary file so a filter/encoding/write failure cannot
// truncate an existing result or leave a partially written result at path.
func writeFilteredOutputFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer func() { _ = os.Remove(temporary) }()
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}
