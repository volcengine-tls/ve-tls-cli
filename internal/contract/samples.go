package contract

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"
)

var requestSamples = map[OperationID]map[TemplateMode]map[string]any{
	"index.create": indexRequestSamples(),
	"index.modify": indexRequestSamples(),
	"log.put": {
		TemplateRequired: putLogsRequestSample(false),
		TemplateFull:     putLogsRequestSample(true),
	},
}

// RequestSample returns a curated request body for operations whose generic
// schema projection is not useful enough as an example. The sample is checked
// against the current Operation schema before it is returned.
func RequestSample(operation Operation, mode TemplateMode) (map[string]any, bool, error) {
	if mode != TemplateRequired && mode != TemplateFull {
		return nil, false, fmt.Errorf("unsupported request template mode %q", mode)
	}
	modes, ok := requestSamples[operation.ID]
	if !ok {
		return nil, false, nil
	}
	sample, ok := modes[mode]
	if !ok {
		return nil, false, nil
	}
	rawBody, ok := operation.InputSchema["body"]
	if !ok {
		return nil, false, fmt.Errorf("operation %q has a request sample but no body schema", operation.ID)
	}
	body, ok := templateSchemaMap(rawBody)
	if !ok {
		return nil, false, fmt.Errorf("operation %q body schema must be an object", operation.ID)
	}
	if err := validateRequestValue(sample, body, "input_schema.body"); err != nil {
		return nil, false, fmt.Errorf("operation %q request sample: %w", operation.ID, err)
	}
	return cloneValue(sample).(map[string]any), true, nil
}

func indexRequestSamples() map[TemplateMode]map[string]any {
	useful := map[string]any{
		"TopicId": "",
		"FullText": map[string]any{
			"CaseSensitive":  false,
			"Delimiter":      " \t\n",
			"IncludeChinese": true,
		},
		"KeyValue": []any{
			map[string]any{
				"Key": "",
				"Value": map[string]any{
					"ValueType":     "text",
					"CaseSensitive": false,
					"SqlFlag":       true,
				},
			},
		},
	}
	full := cloneValue(useful).(map[string]any)
	full["EnableAutoIndex"] = false
	full["MaxTextLen"] = float64(2048)
	full["UserInnerKeyValue"] = []any{
		map[string]any{
			"Key": "",
			"Value": map[string]any{
				"ValueType":     "text",
				"CaseSensitive": false,
				"SqlFlag":       true,
			},
		},
	}
	return map[TemplateMode]map[string]any{
		TemplateRequired: useful,
		TemplateFull:     full,
	}
}

func putLogsRequestSample(full bool) map[string]any {
	group := map[string]any{
		"Source":   "",
		"FileName": "",
		"Logs": []any{
			map[string]any{
				"Time": float64(1710374400000),
				"Contents": []any{
					map[string]any{"Key": "", "Value": ""},
				},
			},
		},
	}
	if full {
		group["ContextFlow"] = ""
		group["LogTags"] = []any{
			map[string]any{"Key": "", "Value": ""},
		}
		group["Logs"].([]any)[0].(map[string]any)["TimeNs"] = float64(0)
	}
	return map[string]any{"LogGroups": []any{group}}
}

func validateRequestValue(value any, schema map[string]any, path string) error {
	if choices, ok := templateSlice(schema["oneOf"]); ok {
		var failures []string
		for i, rawChoice := range choices {
			choice, ok := templateSchemaMap(rawChoice)
			if !ok {
				return fmt.Errorf("%s oneOf[%d] must be a schema object", path, i)
			}
			if err := validateRequestValue(value, choice, path); err == nil {
				return nil
			} else {
				failures = append(failures, err.Error())
			}
		}
		return fmt.Errorf("%s does not match any oneOf schema: %s", path, strings.Join(failures, "; "))
	}
	if values, ok := templateSlice(schema["enum"]); ok && !requestEnumContains(values, value) {
		return fmt.Errorf("%s is not an allowed enum value", path)
	}

	schemaType, _ := schema["type"].(string)
	if schemaType == "" {
		switch {
		case schema["properties"] != nil:
			schemaType = "object"
		case schema["items"] != nil:
			schemaType = "array"
		default:
			return nil
		}
	}
	switch schemaType {
	case "object":
		object, ok := templateSchemaMap(value)
		if !ok {
			return fmt.Errorf("%s must be an object", path)
		}
		properties, err := templateProperties(schema, path)
		if err != nil {
			return err
		}
		required, err := requiredStringSlice(schema["required"])
		if schema["required"] != nil && err != nil {
			return fmt.Errorf("%s required must be an array of strings", path)
		}
		for _, name := range required {
			if _, ok := object[name]; !ok {
				return fmt.Errorf("%s is missing required field %q", path, name)
			}
		}
		names := make([]string, 0, len(object))
		for name := range object {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			fieldValue := object[name]
			rawField, ok := properties[name]
			if !ok {
				return fmt.Errorf("%s contains unknown field %q", path, name)
			}
			field, ok := templateSchemaMap(rawField)
			if !ok {
				return fmt.Errorf("%s.%s must be a schema object", path, name)
			}
			if err := validateRequestValue(fieldValue, field, path+"."+name); err != nil {
				return err
			}
		}
	case "array":
		items, ok := templateSlice(value)
		if !ok {
			return fmt.Errorf("%s must be an array", path)
		}
		if len(items) > maxRequestTemplateArrayLength {
			return fmt.Errorf(
				"%s template array length %d exceeds safety limit %d",
				path,
				len(items),
				maxRequestTemplateArrayLength,
			)
		}
		if err := validateRequestLength(len(items), schema, path, "Items"); err != nil {
			return err
		}
		itemSchema, ok := templateSchemaMap(schema["items"])
		if !ok {
			if schema["items"] == nil {
				return nil
			}
			return fmt.Errorf("%s items must be a schema object", path)
		}
		for i, item := range items {
			if err := validateRequestValue(item, itemSchema, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case "string":
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("%s must be a string", path)
		}
		length := utf8.RuneCountInString(text)
		if length > maxRequestTemplateStringLength {
			return fmt.Errorf(
				"%s template string length %d exceeds safety limit %d",
				path,
				length,
				maxRequestTemplateStringLength,
			)
		}
		if err := validateRequestLength(length, schema, path, "Length"); err != nil {
			return err
		}
	case "integer":
		number, ok := templateNumberValue(value)
		if !ok || math.Trunc(number) != number {
			return fmt.Errorf("%s must be an integer", path)
		}
		if err := validateRequestNumber(number, schema, path); err != nil {
			return err
		}
	case "number":
		number, ok := templateNumberValue(value)
		if !ok {
			return fmt.Errorf("%s must be a number", path)
		}
		if err := validateRequestNumber(number, schema, path); err != nil {
			return err
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s must be a boolean", path)
		}
	case "null":
		if value != nil {
			return fmt.Errorf("%s must be null", path)
		}
	default:
		return fmt.Errorf("%s has unsupported schema type %q", path, schemaType)
	}
	return nil
}

func requestEnumContains(values []any, candidate any) bool {
	candidateNumber, candidateIsNumber := templateNumberValue(candidate)
	for _, value := range values {
		if reflect.DeepEqual(value, candidate) {
			return true
		}
		if enumNumber, ok := templateNumberValue(value); ok && candidateIsNumber && enumNumber == candidateNumber {
			return true
		}
	}
	return false
}

func validateRequestNumber(value float64, schema map[string]any, path string) error {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return fmt.Errorf("%s must be a finite JSON number", path)
	}
	if minimum, ok := templateNumberValue(schema["minimum"]); ok && value < minimum {
		return fmt.Errorf("%s must be at least %v", path, minimum)
	}
	if maximum, ok := templateNumberValue(schema["maximum"]); ok && value > maximum {
		return fmt.Errorf("%s must be at most %v", path, maximum)
	}
	return nil
}

func validateRequestLength(length int, schema map[string]any, path, suffix string) error {
	if minimum, ok := templateNumberValue(schema["min"+suffix]); ok && float64(length) < minimum {
		return fmt.Errorf("%s length must be at least %v", path, minimum)
	}
	if maximum, ok := templateNumberValue(schema["max"+suffix]); ok && float64(length) > maximum {
		return fmt.Errorf("%s length must be at most %v", path, maximum)
	}
	return nil
}
