package cli

import (
	"fmt"
	"io"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
	"github.com/volcengine-tls/ve-tls-cli/internal/output"
	"github.com/volcengine-tls/ve-tls-cli/internal/version"
)

const versionInfoSchemaVersion = 1

type versionInfo struct {
	SchemaVersion        int    `json:"schema_version"`
	Version              string `json:"version"`
	Edition              string `json:"edition"`
	Commit               string `json:"commit"`
	CatalogDigest        string `json:"catalog_digest"`
	OperationCount       int    `json:"operation_count"`
	PublicOperationCount int    `json:"public_operation_count"`
	WorkflowCount        int    `json:"workflow_count"`
}

func writeVersionInfo(stdout io.Writer) error {
	catalog, err := loadOperationCatalog()
	if err != nil {
		return fmt.Errorf("load operation catalog: %w", err)
	}
	digest, err := contract.CatalogV2Digest(catalog)
	if err != nil {
		return fmt.Errorf("digest operation catalog: %w", err)
	}
	workflows, err := workflowCatalogEntries("")
	if err != nil {
		return fmt.Errorf("load workflow catalog: %w", err)
	}
	publicOperations := 0
	for _, operation := range catalog.Operations {
		if operation.Visibility == "public" {
			publicOperations++
		}
	}
	return output.Write(stdout, versionInfo{
		SchemaVersion:        versionInfoSchemaVersion,
		Version:              version.Version,
		Edition:              string(currentEdition()),
		Commit:               version.Commit,
		CatalogDigest:        digest,
		OperationCount:       len(catalog.Operations),
		PublicOperationCount: publicOperations,
		WorkflowCount:        len(workflows),
	}, output.FormatJSON)
}
