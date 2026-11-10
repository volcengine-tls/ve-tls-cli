package cli

import (
	"sort"
	"strconv"
	"strings"
)

// toolContextValidationError carries a path and a fixed reason without input values.
type toolContextValidationError struct {
	Path   string
	Reason string
}

func (e *toolContextValidationError) Error() string {
	return "invalid context " + e.Path + ": " + e.Reason
}

type toolContextFieldValidator func(any, string) error

func validateToolExecContext(raw map[string]any) error {
	if err := validateToolContextObject(raw, "$", map[string]toolContextFieldValidator{
		"profile":         validateToolContextString,
		"secrets_file":    validateToolContextString,
		"region":          validateToolContextString,
		"endpoint":        validateToolContextString,
		"trace":           validateToolContextTrace,
		"execution":       validateToolContextExecution,
		"contract_digest": validateToolContextString,
	}); err != nil {
		return err
	}
	if execution, ok := raw["execution"].(map[string]any); ok {
		return validateToolContextExecutionAliases(execution)
	}
	return nil
}

func validateToolContextObject(value any, path string, fields map[string]toolContextFieldValidator) error {
	object, ok := value.(map[string]any)
	if !ok {
		return &toolContextValidationError{Path: path, Reason: "must be an object"}
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fieldPath := toolContextFieldPath(path, key)
		validate, ok := fields[key]
		if !ok {
			return &toolContextValidationError{Path: fieldPath, Reason: "unknown field"}
		}
		if err := validate(object[key], fieldPath); err != nil {
			return err
		}
	}
	return nil
}

func toolContextFieldPath(parent, key string) string {
	if key != "" {
		identifier := true
		for i, r := range key {
			if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || (i > 0 && r >= '0' && r <= '9')) {
				identifier = false
				break
			}
		}
		if identifier {
			return parent + "." + key
		}
	}
	return parent + "[" + strconv.Quote(key) + "]"
}

func validateToolContextString(value any, path string) error {
	if _, ok := value.(string); !ok {
		return &toolContextValidationError{Path: path, Reason: "must be a string"}
	}
	return nil
}

func validateToolContextBool(value any, path string) error {
	if _, ok := value.(bool); !ok {
		return &toolContextValidationError{Path: path, Reason: "must be a boolean"}
	}
	return nil
}

func validateToolContextTrace(value any, path string) error {
	switch value.(type) {
	case bool, string:
		return nil
	case map[string]any:
		return validateToolContextObject(value, path, map[string]toolContextFieldValidator{
			"enabled": validateToolContextBool,
			"dir":     validateToolContextString,
			"redact":  validateToolContextTraceRedact,
		})
	default:
		return &toolContextValidationError{Path: path, Reason: "must be a boolean, string, or object"}
	}
}

func validateToolContextExecution(value any, path string) error {
	return validateToolContextObject(value, path, map[string]toolContextFieldValidator{
		"allow_write":     validateToolContextBool,
		"non_interactive": validateToolContextBool,
		"timeout":         validateToolContextTimeout,
		"dry_run":         validateToolContextBool,
		"artifact":        validateToolContextArtifact,
		"output_mode":     validateToolContextOutputMode,
		"output":          validateToolContextOutput,
		"page":            validateToolContextPage,
		"page_all":        validateToolContextBool,
		"projection":      validateToolContextProjection,
	})
}

func validateToolContextTimeout(value any, path string) error {
	text, ok := value.(string)
	if !ok {
		return &toolContextValidationError{Path: path, Reason: "must be a positive duration string"}
	}
	if _, err := parseInvocationTimeout(text); err != nil {
		return &toolContextValidationError{Path: path, Reason: err.Error()}
	}
	return nil
}

func validateToolContextTraceRedact(value any, path string) error {
	text, ok := value.(string)
	if !ok {
		return &toolContextValidationError{Path: path, Reason: "must be a string"}
	}
	if err := validateTraceRedactValue(text); err != nil {
		return &toolContextValidationError{Path: path, Reason: err.Error()}
	}
	return nil
}

func validateToolContextArtifact(value any, path string) error {
	switch value.(type) {
	case bool, string:
		return nil
	case map[string]any:
		return validateToolContextObject(value, path, map[string]toolContextFieldValidator{
			"path": validateToolContextString,
			"dir":  validateToolContextString,
		})
	default:
		return &toolContextValidationError{Path: path, Reason: "must be a boolean, string, or object"}
	}
}

func validateToolContextOutputMode(value any, path string) error {
	mode, ok := value.(string)
	if !ok {
		return &toolContextValidationError{Path: path, Reason: "must be a string"}
	}
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "stdout", "file", "artifact":
		return nil
	default:
		return &toolContextValidationError{Path: path, Reason: "must be stdout, file, or artifact"}
	}
}

func validateToolContextOutput(value any, path string) error {
	return validateToolContextObject(value, path, map[string]toolContextFieldValidator{
		"mode": validateToolContextOutputMode,
		"dir":  validateToolContextString,
		"path": validateToolContextString,
	})
}

func validateToolContextPage(value any, path string) error {
	return validateToolContextObject(value, path, map[string]toolContextFieldValidator{
		"all": validateToolContextBool,
	})
}

func validateToolContextProjection(value any, path string) error {
	switch projection := value.(type) {
	case string:
		return nil
	case map[string]any:
		return validateToolContextObject(value, path, map[string]toolContextFieldValidator{
			"jmes": validateToolContextString,
		})
	case []any:
		for i, item := range projection {
			if err := validateToolContextProjection(item, path+"["+strconv.Itoa(i)+"]"); err != nil {
				return err
			}
		}
		return nil
	default:
		return &toolContextValidationError{Path: path, Reason: "must be a string, array, or object"}
	}
}

func validateToolContextExecutionAliases(execution map[string]any) error {
	if page, ok := execution["page"].(map[string]any); ok {
		if nested, ok := page["all"].(bool); ok {
			if alias, ok := execution["page_all"].(bool); ok && nested != alias {
				return &toolContextValidationError{Path: "$.execution.page_all", Reason: "conflicts with $.execution.page.all"}
			}
		}
	}

	var intent, intentSet bool
	var artifactPath, artifactDir string
	switch artifact := execution["artifact"].(type) {
	case bool:
		intent, intentSet = artifact, true
	case string:
		artifactPath = strings.TrimSpace(artifact)
		intent, intentSet = artifactPath != "", artifactPath != ""
	case map[string]any:
		intent, intentSet = true, true
		artifactPath = toolContextTrimmedString(artifact["path"])
		artifactDir = toolContextTrimmedString(artifact["dir"])
	}
	if mode := toolContextTrimmedString(execution["output_mode"]); mode != "" {
		modeIntent := strings.ToLower(mode) != "stdout"
		if intentSet && intent != modeIntent {
			return &toolContextValidationError{Path: "$.execution.output_mode", Reason: "conflicts with $.execution.artifact"}
		}
		intent, intentSet = modeIntent, true
	}
	if output, ok := execution["output"].(map[string]any); ok {
		if mode := toolContextTrimmedString(output["mode"]); mode != "" {
			modeIntent := strings.ToLower(mode) != "stdout"
			if intentSet && intent != modeIntent {
				return &toolContextValidationError{Path: "$.execution.output.mode", Reason: "conflicts with another output selector"}
			}
		}
		if path := toolContextTrimmedString(output["path"]); path != "" && artifactPath != "" && path != artifactPath {
			return &toolContextValidationError{Path: "$.execution.output.path", Reason: "conflicts with $.execution.artifact path"}
		}
		if dir := toolContextTrimmedString(output["dir"]); dir != "" && artifactDir != "" && dir != artifactDir {
			return &toolContextValidationError{Path: "$.execution.output.dir", Reason: "conflicts with $.execution.artifact.dir"}
		}
	}
	return nil
}

func toolContextTrimmedString(value any) string {
	str, _ := value.(string)
	return strings.TrimSpace(str)
}
