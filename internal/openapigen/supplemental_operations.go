package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
)

// supplementalOperationOverrides contains explicit public or internal
// operations which supplement the bootstrap/source-derived catalog. An entry
// with an existing ID replaces that operation; otherwise it is added.
type supplementalOperationOverrides struct {
	Operations []contract.Operation `json:"operations"`
}

func loadSupplementalOperationOverrides(path string) ([]contract.Operation, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("supplemental operation overrides path is required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read supplemental operation overrides: %w", err)
	}
	var overrides supplementalOperationOverrides
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&overrides); err != nil {
		return nil, fmt.Errorf("decode supplemental operation overrides: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return nil, errors.New("decode supplemental operation overrides: trailing JSON value")
	} else if !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("decode supplemental operation overrides: %w", err)
	}
	validated, err := contract.NewCatalog(
		"supplemental-overrides",
		contract.JSONSchema(defaultToolContextSchema()),
		contract.JSONSchema(defaultToolExecutionSchema()),
		overrides.Operations,
	)
	if err != nil {
		return nil, fmt.Errorf("validate supplemental operation overrides: %w", err)
	}
	return validated.Operations, nil
}

func mergeSupplementalOperations(catalog contract.Catalog, supplemental []contract.Operation) (contract.Catalog, error) {
	if err := contract.Validate(catalog); err != nil {
		return contract.Catalog{}, fmt.Errorf("validate operation catalog before supplemental merge: %w", err)
	}
	contextSchema, err := contract.ExpandContextSchema(catalog.ContextSchema, catalog.ExecutionSchema)
	if err != nil {
		return contract.Catalog{}, fmt.Errorf("expand operation catalog context schema: %w", err)
	}
	replacements := make(map[contract.OperationID]struct{}, len(supplemental))
	for _, operation := range supplemental {
		if _, exists := replacements[operation.ID]; exists {
			return contract.Catalog{}, fmt.Errorf("duplicate supplemental operation id %q", operation.ID)
		}
		replacements[operation.ID] = struct{}{}
	}
	operations := make([]contract.Operation, 0, len(catalog.Operations)+len(supplemental))
	for _, operation := range catalog.Operations {
		if _, replaced := replacements[operation.ID]; !replaced {
			operations = append(operations, operation)
		}
	}
	operations = append(operations, supplemental...)
	merged, err := contract.NewCatalog(
		catalog.ContractVersion,
		contextSchema,
		catalog.ExecutionSchema,
		operations,
	)
	if err != nil {
		return contract.Catalog{}, fmt.Errorf("merge supplemental operation overrides: %w", err)
	}
	return merged, nil
}

func mergeSupplementalOperationsIntoCheckedInCatalog(
	catalogPath, lockPath, supplementalOperationsPath, lockRoot string,
) error {
	rawCatalog, err := os.ReadFile(catalogPath)
	if err != nil {
		return fmt.Errorf("read checked-in operation catalog: %w", err)
	}
	catalog, err := contract.Load(rawCatalog)
	if err != nil {
		return err
	}
	rawLock, err := os.ReadFile(lockPath)
	if err != nil {
		return fmt.Errorf("read checked-in operation catalog lock: %w", err)
	}
	var existingLock operationCatalogLock
	if err := json.Unmarshal(rawLock, &existingLock); err != nil {
		return fmt.Errorf("decode checked-in operation catalog lock: %w", err)
	}
	if err := validateOperationCatalogLock(existingLock, catalog); err != nil {
		return fmt.Errorf("validate checked-in operation catalog lock: %w", err)
	}
	if err := validateSupplementalMergeOnlyUnchangedInputs(lockRoot, supplementalOperationsPath, existingLock); err != nil {
		return err
	}
	supplemental, err := loadSupplementalOperationOverrides(supplementalOperationsPath)
	if err != nil {
		return err
	}
	catalog, err = mergeSupplementalOperations(catalog, supplemental)
	if err != nil {
		return err
	}
	inputs := make(map[string]string, len(existingLock.Inputs)+2)
	for _, input := range existingLock.Inputs {
		inputs[input.Name] = filepath.Join(lockRoot, filepath.FromSlash(input.Path))
	}
	inputs["generator_supplemental_operations"] = filepath.Join(lockRoot, "internal", "openapigen", "supplemental_operations.go")
	if filepath.IsAbs(supplementalOperationsPath) {
		inputs["override_supplemental_operations"] = supplementalOperationsPath
	} else {
		inputs["override_supplemental_operations"] = filepath.Join(lockRoot, supplementalOperationsPath)
	}
	lock, err := buildOperationCatalogLock(lockRoot, existingLock.GenerationMode, catalog, inputs)
	if err != nil {
		return err
	}
	return writeOperationCatalogPair(catalogPath, catalog, lockPath, lock)
}

func validateSupplementalMergeOnlyUnchangedInputs(
	root string,
	supplementalOperationsPath string,
	lock operationCatalogLock,
) error {
	root, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return err
	}
	mutableInputs, err := supplementalMergeOnlyMutableInputs(root, supplementalOperationsPath)
	if err != nil {
		return err
	}
	firstSupplementalMerge := true
	for _, input := range lock.Inputs {
		if input.Name == "generator_supplemental_operations" {
			firstSupplementalMerge = false
			break
		}
	}
	for _, input := range lock.Inputs {
		if path, ok := mutableInputs[input.Name]; ok && input.Path == path {
			continue
		}
		// main.go must change once to add the merge-only flag. It is accepted
		// only while migrating a lock that predates this generator input; after
		// the first supplemental merge it is pinned again.
		if firstSupplementalMerge && input.Name == "generator_main" && input.Path == "internal/openapigen/main.go" {
			continue
		}
		path := filepath.Join(root, filepath.FromSlash(input.Path))
		digest, err := hashFile(path)
		if err != nil {
			return fmt.Errorf("verify unexpected operation catalog input %q: %w", input.Name, err)
		}
		if digest != input.SHA256 {
			return fmt.Errorf("unexpected operation catalog input %q digest mismatch", input.Name)
		}
	}
	return nil
}

func supplementalMergeOnlyMutableInputs(root, supplementalOperationsPath string) (map[string]string, error) {
	if !filepath.IsAbs(supplementalOperationsPath) {
		supplementalOperationsPath = filepath.Join(root, supplementalOperationsPath)
	}
	paths := map[string]string{
		"override_supplemental_operations":  supplementalOperationsPath,
		"generator_supplemental_operations": filepath.Join(root, "internal", "openapigen", "supplemental_operations.go"),
	}
	for name, path := range paths {
		relative, err := filepath.Rel(root, filepath.Clean(path))
		if err != nil {
			return nil, err
		}
		paths[name] = filepath.ToSlash(relative)
	}
	return paths, nil
}
