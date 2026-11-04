package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
	"gopkg.in/yaml.v2"
)

const (
	reviewedReadRecoverySource = "reviewed-read-contract"
	unverifiedRecoverySource   = "unverified"
	reviewedReadRecoveryNotes  = "Reviewed read or non-persistent purpose; this does not establish service idempotency or enable automatic retries."
	unverifiedRecoveryNotes    = "Idempotency is unverified. Do not retry automatically; reconcile the operation outcome before any retry."
)

// The policy file is deliberately a flat mapping of canonical operation IDs to
// the two supported decisions. Unlike a generic YAML mapping, malformed and
// duplicate entries are errors rather than silently discarded.
func loadRecoveryPolicies(path string) (map[string]string, error) {
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read recovery policies: %w", err)
	}
	policies := make(map[string]string)
	for i, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		id, value, ok := strings.Cut(line, ":")
		id, value = strings.TrimSpace(id), strings.TrimSpace(value)
		if !ok || id == "" || value == "" || strings.ContainsAny(id, " \t") {
			return nil, fmt.Errorf("invalid recovery policy on line %d", i+1)
		}
		if value != "safe-retry" && value != "high-risk-retry" {
			return nil, fmt.Errorf("unsupported recovery policy %q for %q", value, id)
		}
		if _, exists := policies[id]; exists {
			return nil, fmt.Errorf("duplicate recovery policy for %q", id)
		}
		policies[id] = value
	}
	return policies, nil
}

func loadUsageConstraints(path string) (map[string]string, error) {
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read usage constraints: %w", err)
	}
	var constraints map[string]string
	if err := yaml.UnmarshalStrict(raw, &constraints); err != nil {
		return nil, fmt.Errorf("decode usage constraints: %w", err)
	}
	for id, value := range constraints {
		if strings.TrimSpace(id) != id || id == "" || strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("invalid usage constraint for %q", id)
		}
		constraints[id] = strings.TrimSpace(value)
	}
	return constraints, nil
}

func loadDocumentationOverrides(path string) (map[string]string, error) {
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read documentation overrides: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errors.New("documentation overrides must be a JSON object")
	}
	docs := make(map[string]string)
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("decode documentation override key: %w", err)
		}
		id, ok := key.(string)
		if !ok || strings.TrimSpace(id) != id || id == "" {
			return nil, errors.New("documentation override ID is invalid")
		}
		if _, duplicate := docs[id]; duplicate {
			return nil, fmt.Errorf("duplicate documentation override %q", id)
		}
		var address string
		if err := decoder.Decode(&address); err != nil {
			return nil, fmt.Errorf("decode documentation override %q: %w", id, err)
		}
		parsed, err := url.Parse(address)
		if err != nil || parsed.Scheme != "https" || parsed.Host != "www.volcengine.com" || parsed.User != nil ||
			parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" ||
			!strings.HasPrefix(parsed.Path, "/docs/") || pathpkg.Clean(parsed.Path) != parsed.Path || strings.TrimSpace(address) != address {
			return nil, fmt.Errorf("invalid official documentation URL for %q", id)
		}
		docs[id] = address
	}
	if _, err := decoder.Token(); err != nil {
		return nil, fmt.Errorf("decode documentation overrides: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("documentation overrides have trailing JSON")
	}
	return docs, nil
}

// applyContractPolicies runs after all complete-operation overrides. An
// unlisted operation always receives the conservative unverified policy.
// The checked-in refresh additionally requires an exact policy inventory.
func applyContractPolicies(catalog contract.Catalog, policies, usage, docs map[string]string, requireComplete bool) (contract.Catalog, error) {
	operations := append([]contract.Operation(nil), catalog.Operations...)
	seen := make(map[string]bool, len(operations))
	var missing []string
	for i := range operations {
		operation := &operations[i]
		id := string(operation.ID)
		seen[id] = true
		policy, ok := policies[id]
		if !ok {
			missing = append(missing, id)
			policy = "high-risk-retry"
		}
		switch policy {
		case "safe-retry":
			operation.Risk.ErrorRecovery = policy
			operation.Risk.RecoverySource = reviewedReadRecoverySource
			operation.Risk.RecoveryNotes = reviewedReadRecoveryNotes
		case "high-risk-retry":
			operation.Risk.ErrorRecovery = policy
			operation.Risk.RecoverySource = unverifiedRecoverySource
			operation.Risk.RecoveryNotes = unverifiedRecoveryNotes
		default:
			return contract.Catalog{}, fmt.Errorf("unsupported recovery policy %q for %q", policy, id)
		}
		if guide := strings.TrimSpace(usage[id]); guide != "" && !strings.Contains(operation.Docs.UsageConstraints, guide) {
			if strings.TrimSpace(operation.Docs.UsageConstraints) != "" {
				operation.Docs.UsageConstraints += " "
			}
			operation.Docs.UsageConstraints += guide
		}
		operation.Docs.URL, operation.Docs.URLSource = "", ""
		if address, ok := docs[id]; ok {
			operation.Docs.URL = address
			operation.Docs.URLSource = "official-doc-index"
		}
	}
	var unknownDocs []string
	for id := range docs {
		if !seen[id] {
			unknownDocs = append(unknownDocs, id)
		}
	}
	if len(unknownDocs) > 0 {
		sort.Strings(unknownDocs)
		return contract.Catalog{}, fmt.Errorf("unknown documentation operation IDs: %v", unknownDocs)
	}
	if requireComplete {
		var unknown []string
		for id := range policies {
			if !seen[id] {
				unknown = append(unknown, id)
			}
		}
		sort.Strings(missing)
		sort.Strings(unknown)
		if len(missing) > 0 || len(unknown) > 0 {
			return contract.Catalog{}, fmt.Errorf("recovery policy coverage mismatch: missing=%v unknown=%v", missing, unknown)
		}
		var unknownUsage []string
		for id := range usage {
			if !seen[id] {
				unknownUsage = append(unknownUsage, id)
			}
		}
		if len(unknownUsage) > 0 {
			sort.Strings(unknownUsage)
			return contract.Catalog{}, fmt.Errorf("unknown usage constraint operation IDs: %v", unknownUsage)
		}
	}
	// The checked-in bootstrap catalog can predate the current control fields.
	// Preserve its other controls while refreshing the shared execution schema.
	execution := contract.JSONSchema(cloneToolSchemaMap(map[string]any(catalog.ExecutionSchema)))
	executionProps, ok := execution["properties"].(map[string]any)
	if !ok {
		return contract.Catalog{}, errors.New("execution schema properties are missing")
	}
	defaultExecutionProps := defaultToolExecutionSchema()["properties"].(map[string]any)
	for _, name := range []string{"non_interactive", "allow_write", "timeout"} {
		executionProps[name] = cloneToolSchemaValue(defaultExecutionProps[name])
	}
	context, err := contract.ExpandContextSchema(catalog.ContextSchema, execution)
	if err != nil {
		return contract.Catalog{}, fmt.Errorf("expand operation catalog context: %w", err)
	}
	contextProps, ok := context["properties"].(map[string]any)
	if !ok {
		return contract.Catalog{}, errors.New("context schema properties are missing")
	}
	trace, _ := contextProps["trace"].(map[string]any)
	defaultContextProps := defaultToolContextSchema()["properties"].(map[string]any)
	defaultTrace := defaultContextProps["trace"].(map[string]any)
	if trace != nil {
		trace["runtime_effect"] = defaultTrace["runtime_effect"]
		forms, _ := trace["oneOf"].([]any)
		for _, raw := range forms {
			form, _ := raw.(map[string]any)
			if form["type"] != "object" {
				continue
			}
			props, _ := form["properties"].(map[string]any)
			redact, _ := props["redact"].(map[string]any)
			if redact != nil {
				redact["enum"] = defaultToolTraceRedactValues()
			}
		}
	}
	return contract.NewCatalog(catalog.ContractVersion, context, execution, operations)
}

func policyInputPaths(root string, paths []string) (string, string, string, error) {
	if len(paths) != 0 && len(paths) != 3 {
		return "", "", "", errors.New("recovery, usage, and documentation policy paths must be specified together")
	}
	if len(paths) == 0 {
		paths = []string{"contracts/overrides/recovery.yaml", "contracts/overrides/usage_constraints.yaml", "contracts/overrides/documentation.json"}
	}
	resolved := make([]string, len(paths))
	for i, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			return "", "", "", errors.New("policy input path is required")
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		resolved[i] = filepath.Clean(path)
	}
	return resolved[0], resolved[1], resolved[2], nil
}

func requireLockedPolicyPaths(root, recoveryPath, usagePath, documentationPath string, lock operationCatalogLock) error {
	root, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return err
	}
	expected := map[string]string{"override_recovery": recoveryPath, "override_usage_constraints": usagePath, "override_documentation": documentationPath}
	for name, path := range expected {
		expected[name], err = filepath.Abs(filepath.Clean(path))
		if err != nil {
			return err
		}
	}
	for _, input := range lock.Inputs {
		if path, ok := expected[input.Name]; ok && filepath.Clean(filepath.Join(root, filepath.FromSlash(input.Path))) != path {
			return fmt.Errorf("policy path differs from locked input %q", input.Path)
		}
	}
	return nil
}

func addContractPolicyInputs(inputs map[string]string, root, recoveryPath, usagePath, documentationPath string) {
	inputs["generator_contract_policies"] = filepath.Join(root, "internal", "openapigen", "contract_policies.go")
	inputs["override_recovery"] = recoveryPath
	inputs["override_usage_constraints"] = usagePath
	inputs["override_documentation"] = documentationPath
}

// refreshContractPoliciesOnly changes only the explicitly listed inputs. Every
// other previously locked input must still match before either output is staged.
func refreshContractPoliciesOnly(catalogPath, lockPath, root, recoveryPath, usagePath, documentationPath string) error {
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
	var lock operationCatalogLock
	if err := json.Unmarshal(rawLock, &lock); err != nil {
		return fmt.Errorf("decode checked-in operation catalog lock: %w", err)
	}
	if err := validateOperationCatalogLock(lock, catalog); err != nil {
		return fmt.Errorf("validate checked-in operation catalog lock: %w", err)
	}
	if err := validatePolicyRefreshInputs(root, recoveryPath, usagePath, documentationPath, lock); err != nil {
		return err
	}
	policies, err := loadRecoveryPolicies(recoveryPath)
	if err != nil {
		return err
	}
	usage, err := loadUsageConstraints(usagePath)
	if err != nil {
		return err
	}
	docs, err := loadDocumentationOverrides(documentationPath)
	if err != nil {
		return err
	}
	catalog, err = applyContractPolicies(catalog, policies, usage, docs, true)
	if err != nil {
		return err
	}
	inputs := make(map[string]string, len(lock.Inputs)+3)
	for _, input := range lock.Inputs {
		inputs[input.Name] = filepath.Join(root, filepath.FromSlash(input.Path))
	}
	inputs["contract_legacy"] = filepath.Join(root, "internal", "contract", "legacy.go")
	addContractPolicyInputs(inputs, root, recoveryPath, usagePath, documentationPath)
	updatedLock, err := buildOperationCatalogLock(root, lock.GenerationMode, catalog, inputs)
	if err != nil {
		return err
	}
	return writeOperationCatalogPair(catalogPath, catalog, lockPath, updatedLock)
}

func validatePolicyRefreshInputs(root, recoveryPath, usagePath, documentationPath string, lock operationCatalogLock) error {
	root, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return err
	}
	recoveryPath, err = filepath.Abs(filepath.Clean(recoveryPath))
	if err != nil {
		return err
	}
	usagePath, err = filepath.Abs(filepath.Clean(usagePath))
	if err != nil {
		return err
	}
	documentationPath, err = filepath.Abs(filepath.Clean(documentationPath))
	if err != nil {
		return err
	}
	allowed := map[string]string{
		"contract_catalog":                  filepath.Join(root, "internal", "contract", "catalog.go"),
		"contract_types":                    filepath.Join(root, "internal", "contract", "types.go"),
		"contract_legacy":                   filepath.Join(root, "internal", "contract", "legacy.go"),
		"generator_main":                    filepath.Join(root, "internal", "openapigen", "main.go"),
		"generator_internal_operations":     filepath.Join(root, "internal", "openapigen", "internal_operations.go"),
		"generator_supplemental_operations": filepath.Join(root, "internal", "openapigen", "supplemental_operations.go"),
		"generator_contract_policies":       filepath.Join(root, "internal", "openapigen", "contract_policies.go"),
		"override_recovery":                 recoveryPath,
		"override_usage_constraints":        usagePath,
		"override_documentation":            documentationPath,
	}
	for _, input := range lock.Inputs {
		path := filepath.Clean(filepath.Join(root, filepath.FromSlash(input.Path)))
		if expected, ok := allowed[input.Name]; ok {
			if path != expected {
				return fmt.Errorf("unexpected operation catalog input %q path %q", input.Name, input.Path)
			}
			continue
		}
		digest, err := hashFile(path)
		if err != nil {
			return fmt.Errorf("verify unchanged operation catalog input %q: %w", input.Name, err)
		}
		if digest != input.SHA256 {
			return fmt.Errorf("unexpected operation catalog input %q digest mismatch", input.Name)
		}
	}
	return nil
}
