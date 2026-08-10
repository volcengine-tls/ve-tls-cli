package contract

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

//go:embed generated_catalog.json
var generatedCatalogFS embed.FS

// NewCatalog constructs a v2 catalog from already-normalized operations. It
// owns only catalog-wide schema deduplication, deterministic ordering, and
// contract validation; source parsing and legacy projection stay in the
// generator.
func NewCatalog(
	contractVersion string,
	contextSchema JSONSchema,
	executionSchema JSONSchema,
	operations []Operation,
) (Catalog, error) {
	if strings.TrimSpace(contractVersion) == "" {
		return Catalog{}, errors.New("contract version is required")
	}
	deduplicatedContext, err := deduplicateContextSchema(
		cloneSchema(contextSchema),
		cloneSchema(executionSchema),
	)
	if err != nil {
		return Catalog{}, err
	}
	normalizedOperations := make([]Operation, len(operations))
	for i, operation := range operations {
		normalizedOperations[i] = operation
		if operation.InputSchema != nil {
			normalizedOperations[i].InputSchema = cloneSchema(operation.InputSchema)
		}
		if operation.Pagination != nil {
			pagination := *operation.Pagination
			normalizedOperations[i].Pagination = &pagination
		}
	}
	sort.Slice(normalizedOperations, func(i, j int) bool {
		return normalizedOperations[i].ID < normalizedOperations[j].ID
	})
	catalog := Catalog{
		SchemaVersion:   CatalogV2SchemaVersion,
		ContractVersion: strings.TrimSpace(contractVersion),
		DigestAlgorithm: CatalogV2DigestAlgorithm,
		ContextSchema:   deduplicatedContext,
		ExecutionSchema: cloneSchema(executionSchema),
		Operations:      normalizedOperations,
	}
	if err := Validate(catalog); err != nil {
		return Catalog{}, err
	}
	return catalog, nil
}

func LoadEmbedded() (Catalog, error) {
	raw, err := generatedCatalogFS.ReadFile("generated_catalog.json")
	if err != nil {
		return Catalog{}, err
	}
	return Load(raw)
}

func Load(raw []byte) (Catalog, error) {
	var catalog Catalog
	if err := json.Unmarshal(raw, &catalog); err != nil {
		return Catalog{}, fmt.Errorf("decode operation catalog: %w", err)
	}
	if err := Validate(catalog); err != nil {
		return Catalog{}, err
	}
	return catalog, nil
}

func deduplicateContextSchema(contextSchema, executionSchema JSONSchema) (JSONSchema, error) {
	properties := schemaProperties(contextSchema)
	rawExecution, ok := properties["execution"]
	if !ok {
		return nil, errors.New("context schema is missing execution property")
	}
	executionField, ok := rawExecution.(map[string]any)
	if !ok {
		return nil, errors.New("context execution property is not an object")
	}
	overlay := map[string]any{"$ref": "#/execution_schema"}
	for key, value := range executionField {
		if baseValue, ok := executionSchema[key]; ok && reflect.DeepEqual(value, baseValue) {
			continue
		}
		overlay[key] = cloneValue(value)
	}
	out := cloneSchema(contextSchema)
	schemaProperties(out)["execution"] = overlay
	expanded, err := ExpandContextSchema(out, executionSchema)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(map[string]any(expanded), map[string]any(contextSchema)) {
		return nil, errors.New("context execution ref overlay does not rebuild source schema")
	}
	return out, nil
}
