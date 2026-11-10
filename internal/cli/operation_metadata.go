package cli

import (
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
)

func toolDocumentationURL(operation contract.Operation) any {
	if url := strings.TrimSpace(operation.Docs.URL); url != "" {
		return url
	}
	return nil
}

func toolInputPolicy(operation contract.Operation) map[string]any {
	unknown := "schema"
	if len(operation.InputSchema) == 0 {
		unknown = "passthrough"
	}
	return map[string]any{"unknown_fields": unknown, "extra_top_level": "reject"}
}
