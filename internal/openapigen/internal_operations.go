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

type internalOperationOverrides struct {
	Operations []contract.Operation `json:"operations"`
}

func loadInternalOperationOverrides(path string) ([]contract.Operation, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("internal operation overrides path is required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read internal operation overrides: %w", err)
	}
	var overrides internalOperationOverrides
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&overrides); err != nil {
		return nil, fmt.Errorf("decode internal operation overrides: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return nil, errors.New("decode internal operation overrides: trailing JSON value")
	} else if !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("decode internal operation overrides: %w", err)
	}
	for _, operation := range overrides.Operations {
		if operation.Visibility != "internal" {
			return nil, fmt.Errorf("internal operation %q visibility must be internal", operation.ID)
		}
	}
	validated, err := contract.NewCatalog(
		"internal-overrides",
		contract.JSONSchema(defaultToolContextSchema()),
		contract.JSONSchema(defaultToolExecutionSchema()),
		overrides.Operations,
	)
	if err != nil {
		return nil, fmt.Errorf("validate internal operation overrides: %w", err)
	}
	return validated.Operations, nil
}

func mergeInternalOperations(
	public contract.Catalog,
	internal []contract.Operation,
) (contract.Catalog, error) {
	if err := contract.Validate(public); err != nil {
		return contract.Catalog{}, fmt.Errorf("validate public operation catalog: %w", err)
	}
	for _, operation := range internal {
		if operation.Visibility != "internal" {
			return contract.Catalog{}, fmt.Errorf("internal operation %q visibility must be internal", operation.ID)
		}
	}
	contextSchema, err := contract.ExpandContextSchema(public.ContextSchema, public.ExecutionSchema)
	if err != nil {
		return contract.Catalog{}, fmt.Errorf("expand public context schema: %w", err)
	}
	operations := make([]contract.Operation, 0, len(public.Operations)+len(internal))
	for _, operation := range public.Operations {
		if operation.Visibility == "public" {
			operations = append(operations, operation)
		}
	}
	operations = append(operations, internal...)
	merged, err := contract.NewCatalog(
		public.ContractVersion,
		contextSchema,
		public.ExecutionSchema,
		operations,
	)
	if err != nil {
		return contract.Catalog{}, fmt.Errorf("merge internal operation overrides: %w", err)
	}
	return merged, nil
}

func mergeInternalOperationsIntoCheckedInCatalog(
	catalogPath, lockPath, internalOperationsPath, lockRoot string,
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
	if err := validateMergeOnlyUnchangedInputs(lockRoot, internalOperationsPath, existingLock); err != nil {
		return err
	}
	internalOperations, err := loadInternalOperationOverrides(internalOperationsPath)
	if err != nil {
		return err
	}
	catalog, err = mergeInternalOperations(catalog, internalOperations)
	if err != nil {
		return err
	}
	inputs := make(map[string]string, len(existingLock.Inputs)+2)
	for _, input := range existingLock.Inputs {
		inputs[input.Name] = filepath.Join(lockRoot, filepath.FromSlash(input.Path))
	}
	inputs["generator_internal_operations"] = filepath.Join(lockRoot, "internal", "openapigen", "internal_operations.go")
	if filepath.IsAbs(internalOperationsPath) {
		inputs["override_internal_operations"] = internalOperationsPath
	} else {
		inputs["override_internal_operations"] = filepath.Join(lockRoot, internalOperationsPath)
	}
	lock, err := buildOperationCatalogLock(lockRoot, existingLock.GenerationMode, catalog, inputs)
	if err != nil {
		return err
	}
	return writeOperationCatalogPair(catalogPath, catalog, lockPath, lock)
}

func validateMergeOnlyUnchangedInputs(
	root string,
	internalOperationsPath string,
	lock operationCatalogLock,
) error {
	root, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return err
	}
	mutableInputs, err := mergeOnlyMutableInputs(root, internalOperationsPath)
	if err != nil {
		return err
	}
	for _, input := range lock.Inputs {
		if path, ok := mutableInputs[input.Name]; ok && input.Path == path {
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

// merge-only may change only the internal operation source and its dedicated
// loader/validator/writer. Every public/source generator input remains pinned
// to the previous lock.
func mergeOnlyMutableInputs(root, internalOperationsPath string) (map[string]string, error) {
	if !filepath.IsAbs(internalOperationsPath) {
		internalOperationsPath = filepath.Join(root, internalOperationsPath)
	}
	paths := map[string]string{
		"override_internal_operations":  internalOperationsPath,
		"generator_internal_operations": filepath.Join(root, "internal", "openapigen", "internal_operations.go"),
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

func writeOperationCatalogPair(
	catalogPath string,
	catalog contract.Catalog,
	lockPath string,
	lock operationCatalogLock,
) error {
	return writeOperationCatalogPairWithFileOps(
		catalogPath,
		catalog,
		lockPath,
		lock,
		os.Rename,
		os.Remove,
	)
}

func writeOperationCatalogPairWithFileOps(
	catalogPath string,
	catalog contract.Catalog,
	lockPath string,
	lock operationCatalogLock,
	renameFile func(string, string) error,
	removeFile func(string) error,
) error {
	catalogRaw, err := json.Marshal(catalog)
	if err != nil {
		return err
	}
	lockRaw, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return err
	}
	catalogRaw = append(catalogRaw, '\n')
	lockRaw = append(lockRaw, '\n')
	originalCatalog, err := os.ReadFile(catalogPath)
	catalogExisted := err == nil
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read original operation catalog: %w", err)
	}
	stagedCatalog, err := stageReplacementFile(catalogPath, catalogRaw)
	if err != nil {
		return fmt.Errorf("stage operation catalog: %w", err)
	}
	defer removeFile(stagedCatalog)
	stagedLock, err := stageReplacementFile(lockPath, lockRaw)
	if err != nil {
		return fmt.Errorf("stage operation catalog lock: %w", err)
	}
	defer removeFile(stagedLock)
	stagedRollback := ""
	if catalogExisted {
		stagedRollback, err = stageReplacementFile(catalogPath, originalCatalog)
		if err != nil {
			return fmt.Errorf("stage operation catalog rollback: %w", err)
		}
		defer func() {
			if stagedRollback != "" {
				_ = removeFile(stagedRollback)
			}
		}()
	}

	if err := renameFile(stagedCatalog, catalogPath); err != nil {
		return fmt.Errorf("replace operation catalog: %w", err)
	}
	if err := renameFile(stagedLock, lockPath); err != nil {
		rollbackErr := removeFile(catalogPath)
		if catalogExisted && rollbackErr == nil {
			rollbackErr = renameFile(stagedRollback, catalogPath)
		}
		if rollbackErr != nil {
			recoveryPath := stagedRollback
			stagedRollback = ""
			return fmt.Errorf(
				"replace operation catalog lock: %w (restore operation catalog: %v; recovery copy retained at %q)",
				err,
				rollbackErr,
				recoveryPath,
			)
		}
		return fmt.Errorf("replace operation catalog lock: %w", err)
	}
	return nil
}

func stageReplacementFile(target string, data []byte) (path string, err error) {
	target = filepath.Clean(target)
	file, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".tmp-*")
	if err != nil {
		return "", err
	}
	path = file.Name()
	defer func() {
		if err == nil {
			return
		}
		_ = file.Close()
		_ = os.Remove(path)
	}()
	if _, err = file.Write(data); err != nil {
		return "", err
	}
	if err = file.Sync(); err != nil {
		return "", err
	}
	if err = file.Chmod(0o644); err != nil {
		return "", err
	}
	if err = file.Close(); err != nil {
		return "", err
	}
	return path, nil
}
