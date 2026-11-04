package execution

import (
	"bytes"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
	"github.com/volcengine-tls/ve-tls-cli/internal/jsonx"
)

// ValidateInput enforces the declared operation schema without changing input
// values. An operation without any section schema retains passthrough behavior.
func ValidateInput(operation contract.Operation, input Input) error {
	declared := false
	for _, section := range inputSections {
		if _, ok := schemaMap(operation.InputSchema[section]); ok {
			declared = true
			break
		}
	}
	if !declared {
		return nil
	}

	state := schemaValidation{}
	sections := map[string]map[string]any{"path": input.Path, "query": input.Query, "header": input.Header}
	for _, section := range []string{"query", "path", "header"} {
		value := sections[section]
		schema, ok := schemaMap(operation.InputSchema[section])
		if !ok {
			if len(value) > 0 {
				return fmt.Errorf("input.%s is not declared by the operation", section)
			}
			continue
		}
		if value == nil {
			value = map[string]any{}
		}
		state.validate(schema, value, "input."+section)
	}

	bodySchema, bodyDeclared := schemaMap(operation.InputSchema["body"])
	if !bodyDeclared {
		if nonEmptyBody(input.Body) {
			return fmt.Errorf("input.body is not declared by the operation")
		}
	} else if !specialJSONLBody(operation, input.Body) {
		var body any = input.Body.JSON
		if input.Body.Raw != nil {
			// An empty schema imposes no constraints and leaves raw payloads intact.
			if len(bodySchema) != 0 {
				var err error
				body, err = jsonx.Decode(input.Body.Raw)
				if err != nil {
					return fmt.Errorf("input.body must be valid JSON")
				}
			}
		} else if !input.Body.Present && body == nil {
			// BuildRequest uses an empty object when the body is omitted.
			body = map[string]any{}
		}
		state.validate(bodySchema, body, "input.body")
	}
	if state.err != nil {
		return state.err
	}
	return missingRequiredError(state.missing)
}

func specialJSONLBody(operation contract.Operation, body Payload) bool {
	format := body.Format
	if strings.TrimSpace(string(format)) == "" {
		format = wireBodyFormat(operation.Wire.RequestFormat)
	}
	return strings.EqualFold(strings.TrimSpace(string(format)), string(BodyFormatJSONL)) &&
		(operation.Wire.Codec == contract.CodecPutLogs || operation.Wire.Codec == contract.CodecWebTracks)
}

func nonEmptyBody(body Payload) bool {
	value := body.JSON
	if body.Raw != nil {
		if len(bytes.TrimSpace(body.Raw)) == 0 {
			return false
		}
		var err error
		value, err = jsonx.Decode(body.Raw)
		if err != nil {
			return true
		}
	}
	if isJSONNull(value) {
		return false
	}
	if object, ok := schemaValueObject(value); ok {
		return len(object) > 0
	}
	return true
}

type schemaValidation struct {
	missing []string
	err     error
}

func (v *schemaValidation) fail(path, reason string) {
	if v.err == nil {
		v.err = fmt.Errorf("%s %s", path, reason)
	}
}

func (v *schemaValidation) validateRaw(raw, value any, path string) {
	if v.err != nil {
		return
	}
	if allowed, ok := raw.(bool); ok {
		if !allowed {
			v.fail(path, "is not allowed by the schema")
		}
		return
	}
	schema, ok := schemaMap(raw)
	if !ok {
		v.fail(path, "has an invalid schema")
		return
	}
	v.validate(schema, value, path)
}

func (v *schemaValidation) validate(schema map[string]any, value any, path string) {
	if v.err != nil {
		return
	}
	if declaredType, exists := schema["type"]; exists && !matchesSchemaType(declaredType, value) {
		v.fail(path, "does not match the declared type")
		return
	}
	if constant, exists := schema["const"]; exists && !schemaValuesEqual(value, constant) {
		v.fail(path, "does not match const")
		return
	}
	if rawEnum, exists := schema["enum"]; exists {
		choices, ok := schemaValueArray(rawEnum)
		match := false
		if ok {
			for _, choice := range choices {
				if schemaValuesEqual(value, choice) {
					match = true
					break
				}
			}
		}
		if !match {
			v.fail(path, "does not match an allowed enum value")
			return
		}
	}
	if number, ok := numberValue(value); ok {
		for _, bound := range []struct {
			name  string
			below bool
		}{{"minimum", true}, {"maximum", false}} {
			if raw, exists := schema[bound.name]; exists {
				if limit, valid := numberValue(raw); valid {
					cmp := compareDecimalNumbers(number, limit)
					if (bound.below && cmp < 0) || (!bound.below && cmp > 0) {
						v.fail(path, "violates "+bound.name)
						return
					}
				}
			}
		}
	}
	if text, ok := value.(string); ok {
		if !v.validateLength(schema, utf8.RuneCountInString(text), "minLength", "maxLength", path) {
			return
		}
	}
	if array, ok := schemaValueArray(value); ok {
		if !v.validateLength(schema, len(array), "minItems", "maxItems", path) {
			return
		}
		if itemSchema, exists := schema["items"]; exists {
			for i, item := range array {
				v.validateRaw(itemSchema, item, fmt.Sprintf("%s[%d]", path, i))
				if v.err != nil {
					return
				}
			}
		}
	}
	if object, ok := schemaValueObject(value); ok {
		for _, name := range schemaRequiredFields(schema["required"]) {
			if _, exists := object[name]; !exists {
				v.missing = append(v.missing, path+"."+name)
			}
		}
		properties, _ := schemaMap(schema["properties"])
		keys := make([]string, 0, len(object))
		for name := range object {
			keys = append(keys, name)
		}
		sort.Strings(keys)
		for _, name := range keys {
			child, declared := properties[name]
			if declared {
				v.validateRaw(child, object[name], path+"."+name)
			} else if additional, exists := schema["additionalProperties"]; exists {
				v.validateRaw(additional, object[name], path+"."+name)
			}
			if v.err != nil {
				return
			}
		}
	}
	if rawChoices, exists := schema["oneOf"]; exists {
		choices, ok := schemaValueArray(rawChoices)
		matches := 0
		if ok {
			for _, choice := range choices {
				candidate := schemaValidation{}
				candidate.validateRaw(choice, value, path)
				if candidate.err == nil && len(candidate.missing) == 0 {
					matches++
				}
			}
		}
		if matches != 1 {
			v.fail(path, "must match exactly one oneOf schema")
			return
		}
	}
	if rawSchemas, exists := schema["allOf"]; exists {
		if schemas, ok := schemaValueArray(rawSchemas); ok {
			for _, part := range schemas {
				v.validateRaw(part, value, path)
				if v.err != nil {
					return
				}
			}
		}
	}
	if condition, exists := schema["if"]; exists {
		candidate := schemaValidation{}
		candidate.validateRaw(condition, value, path)
		branch := "else"
		if candidate.err == nil && len(candidate.missing) == 0 {
			branch = "then"
		}
		if selected, ok := schema[branch]; ok {
			v.validateRaw(selected, value, path)
		}
	}
}

func (v *schemaValidation) validateLength(schema map[string]any, length int, minName, maxName, path string) bool {
	actual, _ := numberValue(length)
	for _, bound := range []struct {
		name  string
		below bool
	}{{minName, true}, {maxName, false}} {
		if raw, exists := schema[bound.name]; exists {
			if limit, valid := numberValue(raw); valid {
				cmp := compareDecimalNumbers(actual, limit)
				if (bound.below && cmp < 0) || (!bound.below && cmp > 0) {
					v.fail(path, "violates "+bound.name)
					return false
				}
			}
		}
	}
	return true
}

func schemaTypeIncludes(raw any, desired string) bool {
	if name, ok := raw.(string); ok {
		return name == desired
	}
	if values, ok := schemaValueArray(raw); ok {
		for _, value := range values {
			if name, ok := value.(string); ok && name == desired {
				return true
			}
		}
	}
	return false
}

func matchesSchemaType(raw, value any) bool {
	for _, name := range []string{"null", "object", "array", "string", "boolean", "number", "integer"} {
		if !schemaTypeIncludes(raw, name) {
			continue
		}
		switch name {
		case "null":
			if isJSONNull(value) {
				return true
			}
		case "object":
			if _, ok := schemaValueObject(value); ok {
				return true
			}
		case "array":
			if _, ok := schemaValueArray(value); ok {
				return true
			}
		case "string":
			if _, ok := value.(string); ok {
				return true
			}
		case "boolean":
			if _, ok := value.(bool); ok {
				return true
			}
		case "number":
			if _, ok := numberValue(value); ok {
				return true
			}
		case "integer":
			if n, ok := numberValue(value); ok && n.integer() {
				return true
			}
		}
	}
	return false
}

func isJSONNull(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Map, reflect.Slice, reflect.Pointer, reflect.Interface:
		return v.IsNil()
	}
	return false
}

func schemaValueObject(value any) (map[string]any, bool) {
	if object, ok := schemaMap(value); ok {
		return object, object != nil
	}
	if value == nil {
		return nil, false
	}
	v := reflect.ValueOf(value)
	if v.Kind() != reflect.Map || v.IsNil() || v.Type().Key().Kind() != reflect.String {
		return nil, false
	}
	object := make(map[string]any, v.Len())
	iter := v.MapRange()
	for iter.Next() {
		object[iter.Key().String()] = iter.Value().Interface()
	}
	return object, true
}

func schemaValueArray(value any) ([]any, bool) {
	if value == nil {
		return nil, false
	}
	v := reflect.ValueOf(value)
	if v.Kind() != reflect.Slice && v.Kind() != reflect.Array {
		return nil, false
	}
	if v.Kind() == reflect.Slice && v.IsNil() {
		return nil, false
	}
	// encoding/json represents []byte as a base64 string rather than an array.
	if v.Kind() == reflect.Slice && v.Type().Elem().Kind() == reflect.Uint8 {
		return nil, false
	}
	out := make([]any, v.Len())
	for i := range out {
		out[i] = v.Index(i).Interface()
	}
	return out, true
}

func schemaValuesEqual(a, b any) bool {
	if isJSONNull(a) || isJSONNull(b) {
		return isJSONNull(a) && isJSONNull(b)
	}
	if numberA, ok := numberValue(a); ok {
		numberB, ok := numberValue(b)
		return ok && compareDecimalNumbers(numberA, numberB) == 0
	}
	if text, ok := a.(string); ok {
		other, ok := b.(string)
		return ok && text == other
	}
	if boolean, ok := a.(bool); ok {
		other, ok := b.(bool)
		return ok && boolean == other
	}
	if object, ok := schemaValueObject(a); ok {
		other, ok := schemaValueObject(b)
		if !ok || len(object) != len(other) {
			return false
		}
		for key, value := range object {
			otherValue, exists := other[key]
			if !exists || !schemaValuesEqual(value, otherValue) {
				return false
			}
		}
		return true
	}
	if array, ok := schemaValueArray(a); ok {
		other, ok := schemaValueArray(b)
		if !ok || len(array) != len(other) {
			return false
		}
		for i := range array {
			if !schemaValuesEqual(array[i], other[i]) {
				return false
			}
		}
		return true
	}
	return false
}

func schemaRequiredFields(raw any) []string {
	values, ok := schemaValueArray(raw)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, item := range values {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}
