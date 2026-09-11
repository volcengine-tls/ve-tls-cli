package execution

import (
	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
)

// ValidateInput intentionally preserves the legacy tool-exec validation
// contract: required-field presence is checked recursively, while type, enum
// and range validation remains service-side.
func ValidateInput(operation contract.Operation, input Input) error {
	sections := map[string]map[string]any{
		"path":   input.Path,
		"query":  input.Query,
		"header": input.Header,
	}
	if body, ok := schemaMap(input.Body.JSON); ok {
		sections["body"] = body
	} else {
		sections["body"] = map[string]any{}
	}

	missing := make([]string, 0)
	for _, section := range []string{"query", "path", "header", "body"} {
		schema, ok := schemaMap(operation.InputSchema[section])
		if !ok || len(schema) == 0 {
			continue
		}
		value := sections[section]
		if value == nil {
			value = map[string]any{}
		}
		collectRequiredFields(schema, value, "input."+section, &missing)
	}
	return missingRequiredError(missing)
}

func collectRequiredFields(schema map[string]any, input map[string]any, path string, missing *[]string) {
	for _, name := range schemaRequiredFields(schema["required"]) {
		value, ok := input[name]
		if !ok || value == nil {
			*missing = append(*missing, path+"."+name)
		}
	}

	properties, _ := schemaMap(schema["properties"])
	for name, raw := range properties {
		childSchema, ok := schemaMap(raw)
		if !ok {
			continue
		}
		childInput, ok := schemaMap(input[name])
		if !ok {
			continue
		}
		collectRequiredFields(childSchema, childInput, path+"."+name, missing)
	}
}

func schemaRequiredFields(raw any) []string {
	switch typed := raw.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}
