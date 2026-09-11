package contract

import (
	"errors"
	"fmt"
	"strings"
)

func ExpandContextSchema(contextSchema, executionSchema JSONSchema) (JSONSchema, error) {
	document := map[string]any{
		"context_schema":   contextSchema,
		"execution_schema": executionSchema,
	}
	expanded, err := expandSchemaValue(contextSchema, document, map[string]bool{})
	if err != nil {
		return nil, err
	}
	out, ok := expanded.(map[string]any)
	if !ok {
		return nil, errors.New("expanded context schema is not an object")
	}
	return JSONSchema(out), nil
}

func Validate(catalog Catalog) error {
	if catalog.SchemaVersion != CatalogV2SchemaVersion {
		return fmt.Errorf("unsupported schema_version %q", catalog.SchemaVersion)
	}
	if catalog.DigestAlgorithm != CatalogV2DigestAlgorithm {
		return fmt.Errorf("unsupported digest_algorithm %q", catalog.DigestAlgorithm)
	}
	if strings.TrimSpace(catalog.ContractVersion) == "" {
		return errors.New("contract_version is required")
	}
	if len(catalog.ContextSchema) == 0 || len(catalog.ExecutionSchema) == 0 {
		return errors.New("context_schema and execution_schema are required")
	}
	if len(catalog.Operations) == 0 {
		return errors.New("operations are required")
	}
	document := map[string]any{
		"context_schema":   catalog.ContextSchema,
		"execution_schema": catalog.ExecutionSchema,
	}
	for name, schema := range map[string]JSONSchema{
		"context_schema":   catalog.ContextSchema,
		"execution_schema": catalog.ExecutionSchema,
	} {
		if err := validateSchema(schema, document, name); err != nil {
			return err
		}
		if schema["type"] != "object" {
			return fmt.Errorf("%s type must be object", name)
		}
	}

	ids := make(map[string]struct{}, len(catalog.Operations))
	wires := make(map[string]string, len(catalog.Operations))
	routes := make(map[string]string, len(catalog.Operations))
	for i, operation := range catalog.Operations {
		prefix := fmt.Sprintf("operation[%d]", i)
		id := strings.TrimSpace(string(operation.ID))
		group := strings.TrimSpace(operation.Group)
		action := strings.TrimSpace(operation.Action)
		if id == "" || group == "" || action == "" {
			return fmt.Errorf("%s identity id/group/action are required", prefix)
		}
		if strings.TrimSpace(operation.GroupTitle) == "" {
			return fmt.Errorf("%s group_title is required", prefix)
		}
		for name, value := range map[string]string{
			"resource": operation.Resource,
			"verb":     operation.Verb,
			"family":   operation.Family,
		} {
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("%s %s is required", prefix, name)
			}
		}
		if !oneOf(operation.Visibility, "public", "internal") {
			return fmt.Errorf("%s unsupported visibility %q", prefix, operation.Visibility)
		}
		if _, ok := ids[id]; ok {
			return fmt.Errorf("duplicate operation id %q", id)
		}
		ids[id] = struct{}{}
		routeKey := group + "\x00" + action
		if previous, ok := routes[routeKey]; ok {
			return fmt.Errorf("duplicate operation route %s.%s for %q and %q", group, action, previous, id)
		}
		routes[routeKey] = id

		method := strings.ToUpper(strings.TrimSpace(operation.Wire.Method))
		path := strings.TrimSpace(operation.Wire.Path)
		if method == "" || path == "" {
			return fmt.Errorf("%s wire method/path are required", prefix)
		}
		if !oneOf(method, "GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS") {
			return fmt.Errorf("%s unsupported method %q", prefix, operation.Wire.Method)
		}
		if !strings.HasPrefix(path, "/") {
			return fmt.Errorf("%s wire path must start with /", prefix)
		}
		wireKey := method + " " + path
		if previous, ok := wires[wireKey]; ok {
			return fmt.Errorf("duplicate wire %q for %q and %q", wireKey, previous, id)
		}
		wires[wireKey] = id
		if !oneOf(operation.Wire.RequestFormat, "json", "jsonl") {
			return fmt.Errorf("%s unsupported request_format %q", prefix, operation.Wire.RequestFormat)
		}
		if !supportedCodec(operation.Wire.Codec) {
			return fmt.Errorf("%s unsupported codec %q", prefix, operation.Wire.Codec)
		}
		if err := validatePagination(operation); err != nil {
			return fmt.Errorf("%s: %w", prefix, err)
		}
		if operation.InputSchema == nil {
			return fmt.Errorf("%s input_schema is required", prefix)
		}
		for section, value := range operation.InputSchema {
			if !oneOf(section, "path", "query", "header", "body") {
				return fmt.Errorf("%s input_schema has unsupported section %q", prefix, section)
			}
			schema, ok := value.(map[string]any)
			if !ok || schema["type"] != "object" {
				return fmt.Errorf("%s input_schema.%s must be an object schema", prefix, section)
			}
		}
		if err := validateSchema(operation.InputSchema, document, prefix+".input_schema"); err != nil {
			return err
		}
		if !oneOf(operation.Output.Policy, "envelope", "full", "stream") {
			return fmt.Errorf("%s unsupported output policy %q", prefix, operation.Output.Policy)
		}
		if strings.TrimSpace(operation.Docs.Summary) == "" {
			return fmt.Errorf("%s docs summary is required", prefix)
		}
		if strings.TrimSpace(operation.Docs.Source) == "" {
			return fmt.Errorf("%s docs source is required", prefix)
		}
		if !oneOf(operation.Risk.Level, "low", "medium", "high") {
			return fmt.Errorf("%s unsupported risk level %q", prefix, operation.Risk.Level)
		}
		if !oneOf(operation.Risk.ErrorRecovery, "safe-retry", "retry", "high-risk-retry") {
			return fmt.Errorf("%s unsupported error recovery %q", prefix, operation.Risk.ErrorRecovery)
		}
	}
	return nil
}

func validatePagination(operation Operation) error {
	pagination := operation.Pagination
	if pagination == nil {
		return nil
	}
	switch pagination.Mode {
	case PaginationPageNumber:
		if strings.TrimSpace(pagination.PageNumberParam) == "" {
			return errors.New("page_number_param is required")
		}
		if strings.TrimSpace(pagination.PageSizeParam) == "" {
			return errors.New("page_size_param is required")
		}
		if pagination.DefaultPageSize <= 0 {
			return errors.New("default_page_size must be positive")
		}
		if pagination.MaxPages <= 0 {
			return errors.New("max_pages must be positive")
		}
		if strings.TrimSpace(pagination.ItemsField) == "" {
			return errors.New("items_field is required")
		}
		queryProperties := schemaProperties(operation.InputSchema["query"])
		fields := []string{pagination.PageNumberParam, pagination.PageSizeParam}
		if strings.TrimSpace(pagination.CursorParam) != "" {
			fields = append(fields, pagination.CursorParam)
		}
		for _, field := range fields {
			if _, ok := queryProperties[field]; !ok {
				return fmt.Errorf("pagination field %q is absent from input_schema.query.properties", field)
			}
		}
		return nil
	case PaginationCursor:
		if strings.TrimSpace(pagination.CursorParam) == "" {
			return errors.New("cursor_param is required")
		}
		if strings.TrimSpace(pagination.NextCursorField) == "" {
			return errors.New("next_cursor_field is required")
		}
		if strings.TrimSpace(pagination.ItemsField) == "" {
			return errors.New("items_field is required")
		}
		if pagination.MaxPages <= 0 {
			return errors.New("max_pages must be positive")
		}
		queryProperties := schemaProperties(operation.InputSchema["query"])
		if _, ok := queryProperties[pagination.CursorParam]; !ok {
			return fmt.Errorf("pagination field %q is absent from input_schema.query.properties", pagination.CursorParam)
		}
		if pagination.PageSizeParam != "" {
			if _, ok := queryProperties[pagination.PageSizeParam]; !ok {
				return fmt.Errorf("pagination field %q is absent from input_schema.query.properties", pagination.PageSizeParam)
			}
		}
		if pagination.PageNumberParam != "" {
			if _, ok := queryProperties[pagination.PageNumberParam]; !ok {
				return fmt.Errorf("pagination field %q is absent from input_schema.query.properties", pagination.PageNumberParam)
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported pagination mode %q", pagination.Mode)
	}
}

func validateSchema(value any, document map[string]any, path string) error {
	switch schema := value.(type) {
	case JSONSchema:
		return validateSchema(map[string]any(schema), document, path)
	case map[string]any:
		if rawRef, ok := schema["$ref"]; ok {
			ref, ok := rawRef.(string)
			if !ok {
				return fmt.Errorf("%s unresolved schema ref %q", path, rawRef)
			}
			target := resolveLocalRef(document, ref)
			if target == nil {
				return fmt.Errorf("%s unresolved schema ref %q", path, rawRef)
			}
			switch target.(type) {
			case map[string]any, JSONSchema:
			default:
				return fmt.Errorf("%s schema ref %q target is not an object", path, ref)
			}
		}
		if rawRequired, ok := schema["required"]; ok {
			requiredFields, err := requiredStringSlice(rawRequired)
			if err != nil {
				return fmt.Errorf("%s required must be an array of strings", path)
			}
			properties := schemaProperties(schema)
			for _, required := range requiredFields {
				if _, ok := properties[required]; !ok {
					return fmt.Errorf("%s required field %q is absent from properties", path, required)
				}
			}
		}
		for key, child := range schema {
			if err := validateSchema(child, document, path+"."+key); err != nil {
				return err
			}
		}
	case []any:
		for i, child := range schema {
			if err := validateSchema(child, document, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

func expandSchemaValue(value any, document map[string]any, stack map[string]bool) (any, error) {
	switch schema := value.(type) {
	case JSONSchema:
		return expandSchemaValue(map[string]any(schema), document, stack)
	case map[string]any:
		base := map[string]any{}
		if rawRef, ok := schema["$ref"]; ok {
			ref, ok := rawRef.(string)
			if !ok {
				return nil, fmt.Errorf("schema ref has type %T", rawRef)
			}
			if stack[ref] {
				return nil, fmt.Errorf("schema ref cycle at %q", ref)
			}
			target := resolveLocalRef(document, ref)
			if target == nil {
				return nil, fmt.Errorf("unresolved schema ref %q", ref)
			}
			nextStack := cloneBoolMap(stack)
			nextStack[ref] = true
			expanded, err := expandSchemaValue(target, document, nextStack)
			if err != nil {
				return nil, err
			}
			base, ok = expanded.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("schema ref %q target is not an object", ref)
			}
		}
		out := cloneMap(base)
		for key, child := range schema {
			if key == "$ref" {
				continue
			}
			expanded, err := expandSchemaValue(child, document, stack)
			if err != nil {
				return nil, err
			}
			out[key] = expanded
		}
		return out, nil
	case []any:
		out := make([]any, len(schema))
		for i, child := range schema {
			expanded, err := expandSchemaValue(child, document, stack)
			if err != nil {
				return nil, err
			}
			out[i] = expanded
		}
		return out, nil
	default:
		return value, nil
	}
}

func resolveLocalRef(document map[string]any, ref string) any {
	const prefix = "#/"
	if !strings.HasPrefix(ref, prefix) {
		return nil
	}
	var current any = document
	for _, token := range strings.Split(strings.TrimPrefix(ref, prefix), "/") {
		var object map[string]any
		switch typed := current.(type) {
		case map[string]any:
			object = typed
		case JSONSchema:
			object = map[string]any(typed)
		default:
			return nil
		}
		var ok bool
		current, ok = object[strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")]
		if !ok {
			return nil
		}
	}
	return current
}

func supportedCodec(codec CodecID) bool {
	switch codec {
	case CodecJSON, CodecPutLogs, CodecWebTracks, CodecConsumeLogs:
		return true
	default:
		return false
	}
}

func oneOf(value string, allowed ...string) bool {
	value = strings.TrimSpace(value)
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func schemaProperties(value any) map[string]any {
	schema, ok := value.(map[string]any)
	if !ok {
		if typed, ok := value.(JSONSchema); ok {
			schema = map[string]any(typed)
		} else {
			return nil
		}
	}
	properties, _ := schema["properties"].(map[string]any)
	return properties
}

func requiredStringSlice(value any) ([]string, error) {
	switch items := value.(type) {
	case []string:
		return append([]string(nil), items...), nil
	case []any:
		out := make([]string, 0, len(items))
		for _, item := range items {
			text, ok := item.(string)
			if !ok {
				return nil, errors.New("required item is not a string")
			}
			out = append(out, text)
		}
		return out, nil
	default:
		return nil, errors.New("required is not an array")
	}
}

func cloneSchema(source JSONSchema) JSONSchema {
	return JSONSchema(cloneMap(map[string]any(source)))
}

func cloneMap(source map[string]any) map[string]any {
	out := make(map[string]any, len(source))
	for key, value := range source {
		switch typed := value.(type) {
		case JSONSchema:
			out[key] = cloneSchema(typed)
		case map[string]any:
			out[key] = cloneMap(typed)
		case []any:
			items := make([]any, len(typed))
			for i, item := range typed {
				items[i] = cloneValue(item)
			}
			out[key] = items
		case []string:
			out[key] = append([]string(nil), typed...)
		default:
			out[key] = typed
		}
	}
	return out
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case JSONSchema:
		return cloneSchema(typed)
	case map[string]any:
		return cloneMap(typed)
	case []any:
		items := make([]any, len(typed))
		for i, item := range typed {
			items[i] = cloneValue(item)
		}
		return items
	case []string:
		return append([]string(nil), typed...)
	default:
		return typed
	}
}

func cloneBoolMap(source map[string]bool) map[string]bool {
	out := make(map[string]bool, len(source)+1)
	for key, value := range source {
		out[key] = value
	}
	return out
}
