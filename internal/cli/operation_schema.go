package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
)

func resolveWorkflowOperation(workflow workflowCatalog) (contract.Operation, error) {
	if len(workflow.OperationIDs) != 1 {
		return contract.Operation{}, fmt.Errorf("workflow %s must declare exactly one primary operation", workflow.ID)
	}
	id := strings.TrimSpace(workflow.OperationIDs[0])
	operation, ok := loadToolOperation(id)
	if !ok {
		return contract.Operation{}, fmt.Errorf("workflow %s references unknown operation %q", workflow.ID, id)
	}
	if !strings.EqualFold(strings.TrimSpace(workflow.Method), strings.TrimSpace(operation.Wire.Method)) ||
		strings.TrimSpace(workflow.Path) != strings.TrimSpace(operation.Wire.Path) ||
		normalizeToken(workflow.APIGroup) != normalizeToken(operation.Group) ||
		normalizeActionToken(workflow.APIAction) != normalizeActionToken(operation.Action) {
		return contract.Operation{}, fmt.Errorf("workflow %s interface metadata drifts from operation %s", workflow.ID, id)
	}
	return operation, nil
}

func shortcutOperationDocParams(operation contract.Operation) []apiCapDocParam {
	out := make([]apiCapDocParam, 0)
	for _, sectionName := range []string{"path", "query", "header", "body"} {
		section, ok := operation.InputSchema[sectionName].(map[string]any)
		if !ok {
			continue
		}
		properties, _ := section["properties"].(map[string]any)
		required := shortcutSchemaRequiredSet(section)
		names := sortedShortcutSchemaNames(properties)
		for _, name := range names {
			field, _ := properties[name].(map[string]any)
			requiredText := "否"
			if _, ok := required[name]; ok {
				requiredText = "是"
			}
			out = append(out, apiCapDocParam{
				Name:         name,
				In:           sectionName,
				Type:         shortcutSchemaType(field),
				RequiredText: requiredText,
				Description:  shortcutSchemaString(field, "description"),
				Example:      shortcutSchemaExample(field),
			})
		}
	}
	return out
}

func sortedShortcutSchemaNames(properties map[string]any) []string {
	names := make([]string, 0, len(properties))
	for name := range properties {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func shortcutSchemaType(schema map[string]any) string {
	if schemaType, ok := schema["type"].(string); ok {
		return schemaType
	}
	if _, ok := schema["oneOf"]; ok {
		return "json"
	}
	return ""
}

func shortcutSchemaString(schema map[string]any, key string) string {
	value, _ := schema[key].(string)
	return value
}

func shortcutSchemaExample(schema map[string]any) string {
	value, ok := schema["example"]
	if !ok {
		return ""
	}
	return fmt.Sprint(value)
}

func shortcutSchemaRequiredSet(schema map[string]any) map[string]struct{} {
	out := map[string]struct{}{}
	switch required := schema["required"].(type) {
	case []any:
		for _, item := range required {
			if name, ok := item.(string); ok {
				out[name] = struct{}{}
			}
		}
	case []string:
		for _, name := range required {
			out[name] = struct{}{}
		}
	}
	return out
}
