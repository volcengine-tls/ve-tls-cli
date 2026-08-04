package execution

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
)

type BodyFormat string

const (
	BodyFormatJSON  BodyFormat = "json"
	BodyFormatJSONL BodyFormat = "jsonl"
)

type Input struct {
	Path   map[string]any
	Query  map[string]any
	Header map[string]any
	Body   Payload
}

type Payload struct {
	JSON    any
	Raw     []byte
	Format  BodyFormat
	Present bool
}

var inputSections = []string{"path", "query", "header", "body"}

// NormalizeInput converts the CLI's flat or sectioned JSON object into the
// executor's typed input. Sectioned input remains authoritative and permissive
// to preserve the legacy tool-exec contract.
func NormalizeInput(operation contract.Operation, raw map[string]any) (Input, error) {
	if reserved := reservedInputKeys(raw); len(reserved) > 0 {
		return Input{}, fmt.Errorf(
			"tool exec input contains reserved context/runtime fields: %s; move them to --context",
			strings.Join(reserved, ", "),
		)
	}
	if hasInputSections(raw) {
		return sectionedInput(operation, raw), nil
	}
	if len(operation.InputSchema) == 0 {
		return Input{Body: Payload{
			JSON: cloneAnyMap(raw), Format: wireBodyFormat(operation.Wire.RequestFormat), Present: len(raw) > 0,
		}}, nil
	}

	present := make([]string, 0, len(inputSections))
	properties := make(map[string]map[string]any, len(inputSections))
	hasBody := false
	bodyAllowsLooseFields := false
	for _, section := range inputSections {
		schema, ok := schemaMap(operation.InputSchema[section])
		if !ok || len(schema) == 0 {
			continue
		}
		present = append(present, section)
		props, _ := schemaMap(schema["properties"])
		properties[section] = props
		if section == "body" {
			hasBody = true
			bodyAllowsLooseFields = schemaAllowsLooseFields(schema)
		}
	}
	if len(present) == 0 {
		return Input{Body: Payload{
			JSON: cloneAnyMap(raw), Format: wireBodyFormat(operation.Wire.RequestFormat), Present: len(raw) > 0,
		}}, nil
	}

	normalized := make(map[string]map[string]any, len(inputSections))
	ambiguous := make([]string, 0)
	unknown := make([]string, 0)
	for key, value := range raw {
		matches := matchingInputSections(properties, key)
		switch len(matches) {
		case 0:
			if hasBody && (len(present) == 1 || bodyAllowsLooseFields) {
				assignInputSection(normalized, "body", key, value)
				continue
			}
			unknown = append(unknown, key)
		case 1:
			assignInputSection(normalized, matches[0], key, value)
		default:
			ambiguous = append(ambiguous, fmt.Sprintf("%s(%s)", key, strings.Join(matches, ",")))
		}
	}
	if len(ambiguous) > 0 {
		sort.Strings(ambiguous)
		return Input{}, fmt.Errorf(
			"flat input has ambiguous fields: %s; use nested input sections {query,path,header,body}",
			strings.Join(ambiguous, ", "),
		)
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return Input{}, fmt.Errorf("flat input contains unknown fields: %s", strings.Join(unknown, ", "))
	}
	body := normalized["body"]
	var bodyValue any
	if body != nil {
		bodyValue = body
	}
	return Input{
		Path:   normalized["path"],
		Query:  normalized["query"],
		Header: normalized["header"],
		Body: Payload{
			JSON:    bodyValue,
			Format:  wireBodyFormat(operation.Wire.RequestFormat),
			Present: body != nil,
		},
	}, nil
}

func sectionedInput(operation contract.Operation, raw map[string]any) Input {
	path, _ := schemaMap(raw["path"])
	query, _ := schemaMap(raw["query"])
	header, _ := schemaMap(raw["header"])
	body, bodyExists := raw["body"]
	payload := Payload{
		Format: wireBodyFormat(operation.Wire.RequestFormat), Present: bodyExists,
	}
	if bodyExists {
		payload.JSON = cloneJSONValue(body)
	}
	return Input{
		Path:   cloneAnyMap(path),
		Query:  cloneAnyMap(query),
		Header: cloneAnyMap(header),
		Body:   payload,
	}
}

func wireBodyFormat(requestFormat string) BodyFormat {
	if strings.EqualFold(strings.TrimSpace(requestFormat), string(BodyFormatJSONL)) {
		return BodyFormatJSONL
	}
	return BodyFormatJSON
}

func hasInputSections(raw map[string]any) bool {
	for _, key := range inputSections {
		if _, ok := raw[key]; ok {
			return true
		}
	}
	return false
}

func matchingInputSections(properties map[string]map[string]any, key string) []string {
	out := make([]string, 0, 2)
	for _, section := range inputSections {
		if _, ok := properties[section][key]; ok {
			out = append(out, section)
		}
	}
	return out
}

func assignInputSection(dst map[string]map[string]any, section, key string, value any) {
	if dst[section] == nil {
		dst[section] = map[string]any{}
	}
	dst[section][key] = cloneJSONValue(value)
}

func schemaAllowsLooseFields(schema map[string]any) bool {
	if schema["additionalProperties"] != nil {
		return true
	}
	properties, _ := schemaMap(schema["properties"])
	return len(properties) == 0
}

func reservedInputKeys(raw map[string]any) []string {
	reserved := make([]string, 0)
	for key := range raw {
		if isReservedInputKey(key) {
			reserved = append(reserved, key)
		}
	}
	sort.Strings(reserved)
	return reserved
}

func isReservedInputKey(key string) bool {
	switch strings.TrimSpace(key) {
	case "context", "profile", "secrets_file", "region", "endpoint", "trace",
		"execution", "contract_digest", "contract_cache_hint":
		return true
	default:
		return false
	}
}

func schemaMap(value any) (map[string]any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	case contract.JSONSchema:
		return map[string]any(typed), true
	default:
		return nil, false
	}
}

func cloneAnyMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	out := make(map[string]any, len(src))
	for key, value := range src {
		out[key] = cloneJSONValue(value)
	}
	return out
}

func cloneJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneAnyMap(typed)
	case contract.JSONSchema:
		return cloneAnyMap(map[string]any(typed))
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = cloneJSONValue(item)
		}
		return out
	case []byte:
		return append([]byte(nil), typed...)
	default:
		return value
	}
}

func missingRequiredError(missing []string) error {
	sort.Strings(missing)
	if len(missing) == 1 {
		return fmt.Errorf("missing required field: %s", missing[0])
	}
	if len(missing) > 1 {
		return errors.New("missing required fields: " + strings.Join(missing, ", "))
	}
	return nil
}
