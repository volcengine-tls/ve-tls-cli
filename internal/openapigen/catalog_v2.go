package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
)

type operationCatalogLock struct {
	SchemaVersion   int                     `json:"schema_version"`
	ContractVersion string                  `json:"contract_version"`
	DigestAlgorithm string                  `json:"digest_algorithm"`
	CatalogDigest   string                  `json:"catalog_digest"`
	OperationCount  int                     `json:"operation_count"`
	GenerationMode  string                  `json:"generation_mode"`
	Inputs          []operationCatalogInput `json:"inputs"`
}

type operationCatalogInput struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

func buildOperationCatalogV2FromSource(version string, source []sourceOperation) (contract.Catalog, error) {
	pagination, err := paginationOverridesForSource(source)
	if err != nil {
		return contract.Catalog{}, err
	}
	operations := make([]contract.Operation, 0, len(source))
	for _, item := range source {
		var paginationSpec *contract.PaginationSpec
		if spec, ok := pagination[contract.OperationID(item.ID)]; ok {
			copy := spec
			paginationSpec = &copy
		}
		operations = append(operations, contract.Operation{
			ID:         contract.OperationID(item.ID),
			Group:      item.Group,
			GroupTitle: item.GroupTitle,
			Action:     item.Action,
			Resource:   item.Resource,
			Verb:       item.Verb,
			Family:     item.Family,
			Visibility: item.Visibility,
			Wire: contract.WireSpec{
				Method:        item.Method,
				Path:          item.Path,
				RequestFormat: "json",
				Codec:         operationCodecForWire(item.Method, item.Path),
			},
			InputSchema: contract.JSONSchema(cloneToolSchemaMap(item.InputSchema)),
			Pagination:  paginationSpec,
			Runtime: contract.RuntimeSpec{
				SupportsDryRun: item.SupportsDryRun,
			},
			Output: contract.OutputSpec{
				Policy:           item.OutputPolicy,
				IsEnvelopeOutput: item.IsEnvelopeOutput,
			},
			Docs: contract.DocsSpec{
				Summary:          item.Summary,
				Source:           item.DocSource,
				UsageConstraints: item.UsageConstraints,
			},
			Risk: contract.RiskSpec{
				Level:         item.RiskLevel,
				ErrorRecovery: item.ErrorRecovery,
			},
		})
	}
	return contract.NewCatalog(
		version,
		contract.JSONSchema(defaultToolContextSchema()),
		contract.JSONSchema(defaultToolExecutionSchema()),
		operations,
	)
}

func paginationOverridesForSource(source []sourceOperation) (map[contract.OperationID]contract.PaginationSpec, error) {
	available := operationPaginationOverrides()
	out := make(map[contract.OperationID]contract.PaginationSpec)
	for _, operation := range source {
		id := contract.OperationID(operation.ID)
		spec, ok := available[id]
		switch {
		case operation.SupportsAll && !ok:
			return nil, fmt.Errorf("supports_all operation %q has no explicit pagination override", operation.ID)
		case !operation.SupportsAll && ok:
			return nil, fmt.Errorf("non-paginated operation %q has a pagination override", operation.ID)
		case ok:
			out[id] = spec
		}
	}
	return out, nil
}

func operationCodecForWire(method, path string) contract.CodecID {
	switch strings.ToUpper(strings.TrimSpace(method)) + " " + strings.TrimSpace(path) {
	case "POST /PutLogs":
		return contract.CodecPutLogs
	case "POST /WebTracks":
		return contract.CodecWebTracks
	case "POST /ConsumeLogs":
		return contract.CodecConsumeLogs
	default:
		return contract.CodecJSON
	}
}

// operationPaginationOverrides is intentionally explicit. ItemsField values
// come from volc-sdk-golang v1.0.240 response JSON fields, except the three
// newer APIs documented in log-service:
//   - DescribeLogBackFlowTasks: LogBackFlowTasks
//   - DescribeProcessorBindings: Items
//   - DescribeProcessors: Items
//
// PageNumber/PageSize, page size 100, and the 1000-page ceiling preserve the
// current CLI page-all behavior. DescribeTopics intentionally remains
// page-number based even though its request also accepts Cursor.
func operationPaginationOverrides() map[contract.OperationID]contract.PaginationSpec {
	itemsByOperation := map[contract.OperationID]string{
		"alarm.describe-alarm-content-templates":        "AlarmContentTemplates",
		"alarm.describe-alarm-notify-groups":            "AlarmNotifyGroups",
		"alarm.describe-alarm-webhook-integrations":     "WebhookIntegrations",
		"alarm.describe-alarms":                         "Alarms",
		"collector.describe-bound-host-groups":          "HostGroupInfos",
		"collector.describe-rules":                      "RuleInfos",
		"consumer-group.describe-consumer-groups":       "ConsumerGroups",
		"etl.describe-e-t-l-tasks":                      "Tasks",
		"host-group.describe-host-group-rules":          "RuleInfos",
		"host-group.describe-hosts":                     "HostInfos",
		"import.describe-import-tasks":                  "TaskInfo",
		"log.describe-download-tasks":                   "Tasks",
		"log-back-flow.describe":                        "LogBackFlowTasks",
		"processor.describe-processor-bindings":         "Items",
		"processor.describe-processors":                 "Items",
		"project.describe-projects":                     "Projects",
		"schedule-sql-task.describe-schedule-sql-tasks": "Tasks",
		"shard.describe":                                "Shards",
		"shipper.describe-shippers":                     "Shippers",
		"topic.describe-topics":                         "Topics",
		"trace.describe-trace-instances":                "TraceInstances",
	}
	out := make(map[contract.OperationID]contract.PaginationSpec, len(itemsByOperation))
	for operationID, itemsField := range itemsByOperation {
		out[operationID] = contract.PaginationSpec{
			Mode:            contract.PaginationPageNumber,
			PageNumberParam: "PageNumber",
			PageSizeParam:   "PageSize",
			ItemsField:      itemsField,
			TotalField:      "Total",
			DefaultPageSize: 100,
			MaxPages:        1000,
		}
	}
	// DescribeTopics accepts both PageNumber and Cursor. Page-all remains
	// page-number based, but the alternate start field must stay explicit so
	// the executor can reject a caller-supplied Cursor without action-name
	// inference.
	topicSpec := out["topic.describe-topics"]
	topicSpec.CursorParam = "Cursor"
	out["topic.describe-topics"] = topicSpec
	return out
}

func writeOperationCatalogJSON(path string, catalog contract.Catalog) error {
	raw, err := json.Marshal(catalog)
	if err != nil {
		return err
	}
	return writeFile(path, append(raw, '\n'))
}

func buildOperationCatalogLock(
	root string,
	mode string,
	catalog contract.Catalog,
	inputPaths map[string]string,
) (operationCatalogLock, error) {
	digest, err := contract.CatalogV2Digest(catalog)
	if err != nil {
		return operationCatalogLock{}, err
	}
	root, err = filepath.Abs(filepath.Clean(root))
	if err != nil {
		return operationCatalogLock{}, err
	}
	names := make([]string, 0, len(inputPaths))
	for name := range inputPaths {
		names = append(names, name)
	}
	sort.Strings(names)
	inputs := make([]operationCatalogInput, 0, len(names))
	for _, name := range names {
		path, err := filepath.Abs(filepath.Clean(inputPaths[name]))
		if err != nil {
			return operationCatalogLock{}, err
		}
		paths, err := generationInputFiles(path)
		if err != nil {
			return operationCatalogLock{}, fmt.Errorf("resolve generation input %q: %w", path, err)
		}
		for _, inputPath := range paths {
			relative, err := filepath.Rel(root, inputPath)
			if err != nil {
				return operationCatalogLock{}, err
			}
			digest, err := hashFile(inputPath)
			if err != nil {
				return operationCatalogLock{}, fmt.Errorf("hash generation input %q: %w", inputPath, err)
			}
			inputName := name
			if len(paths) > 1 || inputPath != path {
				nested, err := filepath.Rel(path, inputPath)
				if err != nil {
					return operationCatalogLock{}, err
				}
				inputName += "/" + filepath.ToSlash(nested)
			}
			inputs = append(inputs, operationCatalogInput{
				Name: inputName, Path: filepath.ToSlash(relative), SHA256: digest,
			})
		}
	}
	return operationCatalogLock{
		SchemaVersion:   1,
		ContractVersion: catalog.ContractVersion,
		DigestAlgorithm: catalog.DigestAlgorithm,
		CatalogDigest:   digest,
		OperationCount:  len(catalog.Operations),
		GenerationMode:  strings.TrimSpace(mode),
		Inputs:          inputs,
	}, nil
}

func writeOperationCatalogLock(path string, lock operationCatalogLock) error {
	raw, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(path, append(raw, '\n'))
}

func hashGenerationInput(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	if !info.IsDir() {
		return hashFile(path)
	}
	files, err := generationInputFiles(path)
	if err != nil {
		return "", err
	}
	sort.Strings(files)
	for _, child := range files {
		relative, err := filepath.Rel(path, child)
		if err != nil {
			return "", err
		}
		raw, err := os.ReadFile(child)
		if err != nil {
			return "", err
		}
		_, _ = hash.Write([]byte(filepath.ToSlash(relative)))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(raw)
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func generationInputFiles(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{path}, nil
	}
	var files []string
	if err := filepath.WalkDir(path, func(child string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			files = append(files, child)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("directory %q contains no markdown inputs", path)
	}
	sort.Strings(files)
	return files, nil
}

func hashFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func validateOperationCatalogLock(lock operationCatalogLock, catalog contract.Catalog) error {
	if lock.SchemaVersion != 1 {
		return errors.New("operation catalog lock schema version mismatch")
	}
	if lock.ContractVersion != catalog.ContractVersion {
		return errors.New("operation catalog lock contract version mismatch")
	}
	if lock.DigestAlgorithm != contract.CatalogV2DigestAlgorithm {
		return errors.New("operation catalog lock digest algorithm mismatch")
	}
	if lock.OperationCount != len(catalog.Operations) {
		return errors.New("operation catalog lock count mismatch")
	}
	digest, err := contract.CatalogV2Digest(catalog)
	if err != nil {
		return err
	}
	if lock.CatalogDigest != digest {
		return errors.New("operation catalog lock digest mismatch")
	}
	if lock.GenerationMode != "bootstrap" && lock.GenerationMode != "source" {
		return errors.New("operation catalog lock generation mode is invalid")
	}
	if len(lock.Inputs) == 0 {
		return errors.New("operation catalog lock inputs are required")
	}
	names := make(map[string]struct{}, len(lock.Inputs))
	paths := make(map[string]struct{}, len(lock.Inputs))
	for _, input := range lock.Inputs {
		name := strings.TrimSpace(input.Name)
		path := strings.TrimSpace(input.Path)
		if name == "" || path == "" {
			return errors.New("operation catalog lock input name/path are required")
		}
		if name != input.Name || path != input.Path {
			return fmt.Errorf("operation catalog lock input name/path must not contain surrounding whitespace")
		}
		if filepath.IsAbs(path) {
			return fmt.Errorf("operation catalog lock input path must be relative: %q", path)
		}
		if _, ok := names[name]; ok {
			return fmt.Errorf("duplicate operation catalog lock input name %q", name)
		}
		names[name] = struct{}{}
		if _, ok := paths[path]; ok {
			return fmt.Errorf("duplicate operation catalog lock input path %q", path)
		}
		paths[path] = struct{}{}
		if len(input.SHA256) != sha256.Size*2 || strings.ToLower(input.SHA256) != input.SHA256 {
			return fmt.Errorf("operation catalog lock input %q has invalid sha256", input.Name)
		}
		if _, err := hex.DecodeString(input.SHA256); err != nil {
			return fmt.Errorf("operation catalog lock input %q has invalid sha256", input.Name)
		}
	}
	return nil
}

func validateCommittedOperationCatalogLock(root string, lock operationCatalogLock, catalog contract.Catalog) error {
	if err := validateOperationCatalogLock(lock, catalog); err != nil {
		return err
	}
	for _, input := range lock.Inputs {
		path := filepath.Join(root, filepath.FromSlash(input.Path))
		digest, err := hashFile(path)
		if err != nil {
			if lock.GenerationMode == "source" && os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("hash committed input %q: %w", input.Path, err)
		}
		if digest != input.SHA256 {
			return fmt.Errorf("committed input %q digest mismatch", input.Path)
		}
	}
	return nil
}
