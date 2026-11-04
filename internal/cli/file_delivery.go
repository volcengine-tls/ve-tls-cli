package cli

import (
	"io"
	"os"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/output"
)

// renderFileDelivery is called only after the result artifact was written.
func renderFileDelivery(ctx *Context, stdout, stderr io.Writer, group string, rest []string, env map[string]any, exitCode int) int {
	if currentEdition() == cliEditionVolclogHuman {
		if notice, ok := fileDeliveryNoticeForOutput(env, group, rest); ok {
			if err := writeOutputNotice(stdout, notice); err != nil {
				return writeDeliveryFailureToStderr(ctx, stderr, group, err, env)
			}
			return exitCode
		}
	}

	delivery := cloneOutcomeMap(env)
	if summary, ok := env["summary"].(map[string]any); ok {
		delivery["summary"] = cloneOutcomeMap(summary)
	}
	delivery["data"] = nil
	if err := refreshFileDeliveryArtifacts(delivery); err != nil {
		return writeDeliveryFailure(ctx, stdout, stderr, group, err, "artifact", 2, delivery)
	}
	if err := updateEnvelopeTotalBytes(delivery); err != nil {
		return writeDeliveryFailure(ctx, stdout, stderr, group, err, "stdout", 3, delivery)
	}
	if err := output.Write(stdout, delivery, output.FormatJSON); err != nil {
		return writeDeliveryFailureToStderr(ctx, stderr, group, err, delivery)
	}
	return exitCode
}

// Clone descriptors before refreshing their sizes so the materialized envelope
// and caller-owned metadata remain unchanged.
func refreshFileDeliveryArtifacts(env map[string]any) error {
	refresh := func(artifact map[string]any) (map[string]any, error) {
		if artifact == nil {
			return nil, nil
		}
		cloned := cloneOutcomeMap(artifact)
		path, _ := cloned["path"].(string)
		if strings.TrimSpace(path) == "" {
			return cloned, nil
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		cloned["sizeBytes"] = info.Size()
		return cloned, nil
	}

	switch artifacts := env["artifacts"].(type) {
	case []map[string]any:
		cloned := make([]map[string]any, len(artifacts))
		for i, artifact := range artifacts {
			var err error
			cloned[i], err = refresh(artifact)
			if err != nil {
				return err
			}
		}
		env["artifacts"] = cloned
	case []any:
		cloned := make([]any, len(artifacts))
		for i, artifact := range artifacts {
			if descriptor, ok := artifact.(map[string]any); ok {
				var err error
				cloned[i], err = refresh(descriptor)
				if err != nil {
					return err
				}
			} else {
				cloned[i] = artifact
			}
		}
		env["artifacts"] = cloned
	}
	return nil
}
