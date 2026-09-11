package contract

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

type TemplateMode string

const (
	TemplateRequired TemplateMode = "required"
	TemplateFull     TemplateMode = "full"

	maxRequestTemplateStringLength = 64 * 1024
	maxRequestTemplateArrayLength  = 1024
)

// RequestTemplate projects an operation's JSON body schema into a reusable
// request object. Required mode includes only recursively required fields;
// full mode includes every declared property.
func RequestTemplate(operation Operation, mode TemplateMode) (map[string]any, error) {
	if mode != TemplateRequired && mode != TemplateFull {
		return nil, fmt.Errorf("unsupported request template mode %q", mode)
	}
	rawBody, ok := operation.InputSchema["body"]
	if !ok {
		return map[string]any{}, nil
	}
	body, ok := templateSchemaMap(rawBody)
	if !ok {
		return nil, fmt.Errorf("input_schema.body must be an object schema")
	}
	if err := validateSchema(body, map[string]any{}, "input_schema.body"); err != nil {
		return nil, err
	}
	if schemaType, _ := body["type"].(string); schemaType != "" && schemaType != "object" {
		return nil, fmt.Errorf("input_schema.body type must be object")
	}
	value, err := requestValueTemplate(body, mode, "input_schema.body")
	if err != nil {
		return nil, err
	}
	template, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("input_schema.body template must be an object")
	}
	if err := validateRequestValue(template, body, "input_schema.body"); err != nil {
		return nil, fmt.Errorf("generated request template: %w", err)
	}
	return template, nil
}

func requestObjectTemplateSeeded(
	schema map[string]any,
	mode TemplateMode,
	path string,
	seed map[string]any,
) (map[string]any, error) {
	properties, err := templateProperties(schema, path)
	if err != nil {
		return nil, err
	}
	var required []string
	if rawRequired, ok := schema["required"]; ok {
		required, err = requiredStringSlice(rawRequired)
		if err != nil {
			return nil, fmt.Errorf("%s required must be an array of strings", path)
		}
	}
	requiredSet := make(map[string]struct{}, len(required))
	for _, name := range required {
		if _, ok := properties[name]; !ok {
			return nil, fmt.Errorf("%s required field %q is absent from properties", path, name)
		}
		requiredSet[name] = struct{}{}
	}

	names := make([]string, 0, len(properties))
	for name := range properties {
		if mode == TemplateFull {
			names = append(names, name)
			continue
		}
		if _, ok := requiredSet[name]; ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	template := make(map[string]any, len(names))
	for _, name := range names {
		field, ok := templateSchemaMap(properties[name])
		if !ok {
			return nil, fmt.Errorf("%s.%s must be a schema object", path, name)
		}
		seedValue, seeded := seed[name]
		value, err := requestValueTemplateSeeded(field, mode, path+"."+name, seedValue, seeded && seedValue != nil)
		if err != nil {
			return nil, err
		}
		template[name] = value
	}
	return template, nil
}

func requestValueTemplate(schema map[string]any, mode TemplateMode, path string) (any, error) {
	return requestValueTemplateSeeded(schema, mode, path, nil, false)
}

func requestValueTemplateSeeded(
	schema map[string]any,
	mode TemplateMode,
	path string,
	seed any,
	seeded bool,
) (any, error) {
	if !seeded {
		seed, seeded = preferredTemplateValue(schema)
	}
	if rawChoices, ok := schema["oneOf"]; ok {
		choices, ok := templateSlice(rawChoices)
		if !ok || len(choices) == 0 {
			return nil, fmt.Errorf("%s oneOf must be a non-empty array", path)
		}
		choice, ok := templateSchemaMap(choices[0])
		if !ok {
			return nil, fmt.Errorf("%s oneOf[0] must be a schema object", path)
		}
		return requestValueTemplateSeeded(choice, mode, path, seed, seeded)
	}

	schemaType, _ := schema["type"].(string)
	switch schemaType {
	case "object":
		var objectSeed map[string]any
		if seeded {
			var ok bool
			objectSeed, ok = templateSchemaMap(seed)
			if !ok {
				return nil, fmt.Errorf("%s template seed must be an object", path)
			}
		}
		return requestObjectTemplateSeeded(schema, mode, path, objectSeed)
	case "array":
		return requestArrayTemplate(schema, mode, path, seed, seeded)
	case "integer":
		if seeded {
			return cloneValue(seed), nil
		}
		return templateInteger(schema), nil
	case "number":
		if seeded {
			return cloneValue(seed), nil
		}
		return templateNumber(schema), nil
	case "boolean":
		if seeded {
			return cloneValue(seed), nil
		}
		return false, nil
	case "string":
		if seeded {
			return cloneValue(seed), nil
		}
		length, err := templateStringLength(schema, path)
		if err != nil {
			return nil, err
		}
		return strings.Repeat("x", length), nil
	case "null":
		return nil, nil
	case "":
		switch {
		case schema["properties"] != nil:
			var objectSeed map[string]any
			if seeded {
				var ok bool
				objectSeed, ok = templateSchemaMap(seed)
				if !ok {
					return nil, fmt.Errorf("%s template seed must be an object", path)
				}
			}
			return requestObjectTemplateSeeded(schema, mode, path, objectSeed)
		case schema["items"] != nil:
			return requestArrayTemplate(schema, mode, path, seed, seeded)
		default:
			if seeded {
				return cloneValue(seed), nil
			}
			return "", nil
		}
	default:
		return nil, fmt.Errorf("%s has unsupported schema type %q", path, schemaType)
	}
}

func requestArrayTemplate(
	schema map[string]any,
	mode TemplateMode,
	path string,
	seed any,
	seeded bool,
) ([]any, error) {
	var seedItems []any
	if seeded {
		var ok bool
		seedItems, ok = templateSlice(seed)
		if !ok {
			return nil, fmt.Errorf("%s template seed must be an array", path)
		}
		if len(seedItems) > maxRequestTemplateArrayLength {
			return nil, fmt.Errorf(
				"%s template array length %d exceeds safety limit %d",
				path,
				len(seedItems),
				maxRequestTemplateArrayLength,
			)
		}
	}
	item, ok := templateSchemaMap(schema["items"])
	if !ok {
		if schema["items"] == nil {
			items := make([]any, len(seedItems))
			for i, value := range seedItems {
				items[i] = cloneValue(value)
			}
			return items, nil
		}
		return nil, fmt.Errorf("%s items must be a schema object", path)
	}

	count, err := templateArrayLength(schema, path)
	if err != nil {
		return nil, err
	}
	if len(seedItems) > count {
		count = len(seedItems)
	}
	if count > maxRequestTemplateArrayLength {
		return nil, fmt.Errorf(
			"%s template array length %d exceeds safety limit %d",
			path,
			count,
			maxRequestTemplateArrayLength,
		)
	}
	items := make([]any, count)
	for i := range items {
		var (
			value any
			err   error
		)
		if i < len(seedItems) && seedItems[i] != nil {
			value, err = requestValueTemplateSeeded(item, mode, path+"[]", seedItems[i], true)
		} else {
			value, err = requestValueTemplate(item, mode, path+"[]")
		}
		if err != nil {
			return nil, err
		}
		items[i] = value
	}
	return items, nil
}

func preferredTemplateValue(schema map[string]any) (any, bool) {
	for _, key := range []string{"example", "default"} {
		if value, ok := schema[key]; ok && value != nil {
			return value, true
		}
	}
	if values, ok := templateSlice(schema["enum"]); ok && len(values) > 0 {
		return values[0], true
	}
	return nil, false
}

func templateProperties(schema map[string]any, path string) (map[string]any, error) {
	raw, ok := schema["properties"]
	if !ok {
		return map[string]any{}, nil
	}
	properties, ok := templateSchemaMap(raw)
	if !ok {
		return nil, fmt.Errorf("%s properties must be an object", path)
	}
	return properties, nil
}

func templateSchemaMap(value any) (map[string]any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	case JSONSchema:
		return map[string]any(typed), true
	default:
		return nil, false
	}
}

func templateSlice(value any) ([]any, bool) {
	switch typed := value.(type) {
	case []any:
		return typed, true
	case []string:
		items := make([]any, len(typed))
		for i, item := range typed {
			items[i] = item
		}
		return items, true
	default:
		return nil, false
	}
}

func templateInteger(schema map[string]any) float64 {
	if minimum, ok := templateNumberValue(schema["minimum"]); ok {
		return math.Ceil(minimum)
	}
	if maximum, ok := templateNumberValue(schema["maximum"]); ok && maximum < 0 {
		return math.Floor(maximum)
	}
	return 0
}

func templateNumber(schema map[string]any) float64 {
	if minimum, ok := templateNumberValue(schema["minimum"]); ok {
		return minimum
	}
	if maximum, ok := templateNumberValue(schema["maximum"]); ok && maximum < 0 {
		return maximum
	}
	return 0
}

func templateNumberValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	default:
		return 0, false
	}
}

func templateStringLength(schema map[string]any, path string) (int, error) {
	minimum, ok := templateNumberValue(schema["minLength"])
	if !ok {
		return 0, nil
	}
	return boundedTemplateLength(minimum, maxRequestTemplateStringLength, path, "string")
}

func templateArrayLength(schema map[string]any, path string) (int, error) {
	minimum, ok := templateNumberValue(schema["minItems"])
	if !ok {
		return 1, nil
	}
	length, err := boundedTemplateLength(minimum, maxRequestTemplateArrayLength, path, "array")
	if err != nil {
		return 0, err
	}
	if length < 1 {
		return 1, nil
	}
	return length, nil
}

func boundedTemplateLength(value float64, limit int, path, kind string) (int, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || math.Trunc(value) != value {
		return 0, fmt.Errorf("%s %s minimum length must be a finite non-negative integer", path, kind)
	}
	if value > float64(limit) {
		return 0, fmt.Errorf(
			"%s %s minimum length %v exceeds safety limit %d",
			path,
			kind,
			value,
			limit,
		)
	}
	return int(value), nil
}
