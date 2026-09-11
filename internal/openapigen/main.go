package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type swaggerDoc struct {
	Paths       map[string]swaggerPathItem `json:"paths"`
	Definitions map[string]swaggerSchema   `json:"definitions"`
}

type swaggerPathItem struct {
	Parameters []swaggerParam `json:"parameters"`
	Get        *swaggerOp     `json:"get"`
	Post       *swaggerOp     `json:"post"`
	Put        *swaggerOp     `json:"put"`
	Delete     *swaggerOp     `json:"delete"`
	Patch      *swaggerOp     `json:"patch"`
	Head       *swaggerOp     `json:"head"`
	Options    *swaggerOp     `json:"options"`
}

type swaggerOp struct {
	Summary    string         `json:"summary"`
	Deprecated bool           `json:"deprecated"`
	Tags       []string       `json:"tags"`
	Parameters []swaggerParam `json:"parameters"`
}

type swaggerParam struct {
	Name        string         `json:"name"`
	In          string         `json:"in"`
	Required    bool           `json:"required"`
	Type        string         `json:"type"`
	Format      string         `json:"format"`
	Description string         `json:"description"`
	Enum        []any          `json:"enum"`
	Pattern     string         `json:"pattern"`
	Minimum     *float64       `json:"minimum"`
	Maximum     *float64       `json:"maximum"`
	MinLength   *int           `json:"minLength"`
	MaxLength   *int           `json:"maxLength"`
	Schema      *swaggerSchema `json:"schema"`
}

type swaggerSchema struct {
	Ref                  string                   `json:"$ref"`
	Type                 string                   `json:"type"`
	Format               string                   `json:"format"`
	Description          string                   `json:"description"`
	Default              any                      `json:"default"`
	Required             []string                 `json:"required"`
	Properties           map[string]swaggerSchema `json:"properties"`
	Items                *swaggerSchema           `json:"items"`
	AdditionalProperties json.RawMessage          `json:"additionalProperties"`
	AllOf                []swaggerSchema          `json:"allOf"`
	Enum                 []any                    `json:"enum"`
	Pattern              string                   `json:"pattern"`
	Minimum              *float64                 `json:"minimum"`
	Maximum              *float64                 `json:"maximum"`
	MinLength            *int                     `json:"minLength"`
	MaxLength            *int                     `json:"maxLength"`
}

type apiDocParam struct {
	Name         string
	In           string
	Type         string
	RequiredText string
	Example      string
	Description  string
}

type apiDocEntry struct {
	GroupTitle       string
	RequestParamsDoc []apiDocParam
}

type toolCatalogOverrides struct {
	Risk             map[string]string
	ErrorRecovery    map[string]string
	OutputPolicy     map[string]string
	UsageConstraints map[string]string
}

func main() {
	spec := flag.String("spec", "", "path to swagger.json")
	outOperationCatalog := flag.String("out-operation-catalog", "internal/contract/generated_catalog.json", "output file for operation catalog v2")
	outOperationCatalogLock := flag.String("out-operation-catalog-lock", "contracts/operation-catalog-v2-lock.json", "output file for operation catalog v2 generation lock")
	internalOperationsPath := flag.String("internal-operation-overrides", "contracts/overrides/internal_operations.json", "path to internal operation overrides")
	mergeInternalOnly := flag.Bool("merge-internal-operations-only", false, "merge internal operation overrides into the checked-in catalog without external source inputs")
	supplementalOperationsPath := flag.String("supplemental-operation-overrides", "contracts/overrides/supplemental_operations.json", "path to supplemental public or internal operation overrides")
	mergeSupplementalOnly := flag.Bool("merge-supplemental-operations-only", false, "merge supplemental operation overrides into the checked-in catalog without external source inputs")
	lockRoot := flag.String("lock-root", ".", "root used to make generation lock input paths relative")
	groupKeyMapping := flag.String("group-key-mapping", "contracts/agentic-stage1/group_key_mapping.yaml", "path to group key mapping yaml")
	swaggerTagMapping := flag.String("swagger-tag-mapping", "repos/日志服务/_swagger_tag_mapping.yaml", "path to swagger tag title mapping yaml")
	apiDocRoot := flag.String("api-doc-root", "repos/日志服务/API 参考", "path to api reference markdown root")
	toolRiskOverridesPath := flag.String("tool-risk-overrides", "contracts/overrides/risk.yaml", "path to tool risk override yaml")
	toolRecoveryOverridesPath := flag.String("tool-recovery-overrides", "contracts/overrides/recovery.yaml", "path to tool error recovery override yaml")
	toolOutputPolicyOverridesPath := flag.String("tool-output-policy-overrides", "contracts/overrides/output_policy.yaml", "path to tool output policy override yaml")
	toolUsageConstraintsPath := flag.String("tool-usage-constraints-overrides", "contracts/overrides/usage_constraints.yaml", "path to tool usage constraints override yaml")
	contractVersion := flag.String("contract-version", "v1", "operation contract version")
	flag.Parse()

	if *mergeInternalOnly && *mergeSupplementalOnly {
		fatal(errors.New("--merge-internal-operations-only and --merge-supplemental-operations-only cannot be used together"))
	}
	if *mergeInternalOnly {
		if err := mergeInternalOperationsIntoCheckedInCatalog(
			*outOperationCatalog,
			*outOperationCatalogLock,
			*internalOperationsPath,
			*lockRoot,
		); err != nil {
			fatal(err)
		}
		return
	}
	if *mergeSupplementalOnly {
		if err := mergeSupplementalOperationsIntoCheckedInCatalog(
			*outOperationCatalog,
			*outOperationCatalogLock,
			*supplementalOperationsPath,
			*lockRoot,
		); err != nil {
			fatal(err)
		}
		return
	}
	if strings.TrimSpace(*spec) == "" {
		fatal(errors.New("missing --spec"))
	}
	doc, err := loadSwagger(*spec)
	if err != nil {
		fatal(err)
	}
	groupKeys, err := loadGroupKeyMapping(*groupKeyMapping)
	if err != nil {
		fatal(err)
	}
	tagTitles, err := loadSimpleYAMLMapping(*swaggerTagMapping)
	if err != nil {
		fatal(err)
	}
	docIndex, err := loadAPIDocIndex(*apiDocRoot)
	if err != nil {
		fatal(err)
	}
	toolOverrides, err := buildToolCatalogOverrides(*toolRiskOverridesPath, *toolRecoveryOverridesPath, *toolOutputPolicyOverridesPath, *toolUsageConstraintsPath)
	if err != nil {
		fatal(err)
	}
	sourceOperations := buildSourceOperations(doc, groupKeys, tagTitles, docIndex, toolOverrides)
	operationCatalog, err := buildOperationCatalogV2FromSource(strings.TrimSpace(*contractVersion), sourceOperations)
	if err != nil {
		fatal(err)
	}
	internalOperations, err := loadInternalOperationOverrides(*internalOperationsPath)
	if err != nil {
		fatal(err)
	}
	operationCatalog, err = mergeInternalOperations(operationCatalog, internalOperations)
	if err != nil {
		fatal(err)
	}
	supplementalOperations, err := loadSupplementalOperationOverrides(*supplementalOperationsPath)
	if err != nil {
		fatal(err)
	}
	operationCatalog, err = mergeSupplementalOperations(operationCatalog, supplementalOperations)
	if err != nil {
		fatal(err)
	}

	lock, err := buildOperationCatalogLock(*lockRoot, "source", operationCatalog, map[string]string{
		"api_doc_root":                      *apiDocRoot,
		"contract_catalog":                  "internal/contract/catalog.go",
		"contract_digest":                   "internal/contract/digest.go",
		"contract_schema":                   "internal/contract/schema.go",
		"contract_types":                    "internal/contract/types.go",
		"generator_catalog_v2":              "internal/openapigen/catalog_v2.go",
		"generator_internal_operations":     "internal/openapigen/internal_operations.go",
		"generator_supplemental_operations": "internal/openapigen/supplemental_operations.go",
		"generator_main":                    "internal/openapigen/main.go",
		"generator_source_operations":       "internal/openapigen/source_operations.go",
		"group_key_mapping":                 *groupKeyMapping,
		"override_internal_operations":      *internalOperationsPath,
		"override_supplemental_operations":  *supplementalOperationsPath,
		"override_output_policy":            *toolOutputPolicyOverridesPath,
		"override_recovery":                 *toolRecoveryOverridesPath,
		"override_risk":                     *toolRiskOverridesPath,
		"override_usage_constraints":        *toolUsageConstraintsPath,
		"spec":                              *spec,
		"swagger_tag_mapping":               *swaggerTagMapping,
	})
	if err != nil {
		fatal(err)
	}
	if err := writeOperationCatalogPair(
		*outOperationCatalog,
		operationCatalog,
		*outOperationCatalogLock,
		lock,
	); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	_, _ = fmt.Fprintln(os.Stderr, err.Error())
	os.Exit(1)
}

func loadSwagger(path string) (swaggerDoc, error) {
	b, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return swaggerDoc{}, err
	}
	var doc swaggerDoc
	if err := json.Unmarshal(b, &doc); err != nil {
		return swaggerDoc{}, err
	}
	if doc.Paths == nil {
		doc.Paths = map[string]swaggerPathItem{}
	}
	if doc.Definitions == nil {
		doc.Definitions = map[string]swaggerSchema{}
	}
	return doc, nil
}

func canonicalToolVerbKey(group, verb string) string {
	g := normalizeIdentityToken(group)
	v := normalizeIdentityToken(verb)
	if g == "" || v == "" {
		return ""
	}
	return g + "." + v
}

func normalizeIdentityToken(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func buildToolCatalogOverrides(riskPath, recoveryPath, outputPolicyPath, usageConstraintsPath string) (toolCatalogOverrides, error) {
	risk, err := loadToolCatalogOverrideMap(riskPath)
	if err != nil {
		return toolCatalogOverrides{}, err
	}
	recovery, err := loadToolCatalogOverrideMap(recoveryPath)
	if err != nil {
		return toolCatalogOverrides{}, err
	}
	outputPolicy, err := loadToolCatalogOverrideMap(outputPolicyPath)
	if err != nil {
		return toolCatalogOverrides{}, err
	}
	usage, err := loadToolCatalogOverrideMap(usageConstraintsPath)
	if err != nil {
		return toolCatalogOverrides{}, err
	}
	return toolCatalogOverrides{
		Risk:             risk,
		ErrorRecovery:    recovery,
		OutputPolicy:     outputPolicy,
		UsageConstraints: usage,
	}, nil
}

func loadToolCatalogOverrideMap(path string) (map[string]string, error) {
	p := strings.TrimSpace(path)
	if p == "" {
		return map[string]string{}, nil
	}
	return loadSimpleYAMLMapping(p)
}

func buildToolInputSchema(params []swaggerParam, defs map[string]swaggerSchema, docParams []apiDocParam) map[string]any {
	locs := []string{"query", "path", "header", "body"}
	grouped := map[string][]swaggerParam{}
	for _, p := range params {
		loc := strings.ToLower(strings.TrimSpace(p.In))
		switch loc {
		case "query", "path", "header", "body":
			grouped[loc] = append(grouped[loc], p)
		}
	}
	out := map[string]any{}
	docGrouped := map[string][]apiDocParam{}
	for _, p := range docParams {
		loc := strings.ToLower(strings.TrimSpace(p.In))
		if !isTopLevelParamLocation(loc) {
			continue
		}
		docGrouped[loc] = append(docGrouped[loc], p)
	}
	for _, loc := range locs {
		list := grouped[loc]
		docList := docGrouped[loc]
		if len(list) == 0 && len(docList) == 0 {
			continue
		}
		if loc == "body" {
			bodySchema := buildToolBodyInputSchema(list, defs, docList)
			if bodySchema != nil {
				out[loc] = bodySchema
			}
			continue
		}
		schema := map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}
		props := schema["properties"].(map[string]any)
		required := map[string]struct{}{}
		for _, p := range list {
			name := strings.TrimSpace(p.Name)
			if name == "" {
				continue
			}
			if loc == "header" && shouldFilterManagedToolHeader(name) {
				continue
			}
			props[name] = toolParamSchema(p, defs)
			if p.Required {
				required[name] = struct{}{}
			}
		}
		for _, p := range docGrouped[loc] {
			name := strings.TrimSpace(p.Name)
			if name == "" {
				continue
			}
			if loc == "header" && shouldFilterManagedToolHeader(name) {
				continue
			}
			if _, exists := props[name]; exists {
				if isDocParamRequired(p.RequiredText) {
					required[name] = struct{}{}
				}
				continue
			}
			props[name] = buildDocParamSchema(p)
			if isDocParamRequired(p.RequiredText) {
				required[name] = struct{}{}
			}
		}
		if len(required) > 0 {
			req := make([]string, 0, len(required))
			for name := range required {
				req = append(req, name)
			}
			sort.Strings(req)
			schema["required"] = req
		}
		if len(props) == 0 {
			continue
		}
		out[loc] = schema
	}
	return out
}

func applyPublicSourceInputSchemaOverrides(operation *sourceOperation) {
	if operation == nil {
		return
	}
	switch strings.TrimSpace(operation.Summary) {
	case "PutLogs":
		applyPutLogsBodySchemaOverride(operation)
	case "CreateTraceInstance", "ModifyTraceInstance":
		overrideWeakBodyObjectField(operation.InputSchema, "BackendConfig", map[string]any{
			"type":                 "object",
			"additionalProperties": true,
		})
	case "SearchTraces":
		overrideWeakBodyObjectField(operation.InputSchema, "Query", map[string]any{
			"type":                 "object",
			"additionalProperties": true,
		})
	}
}

func applyPutLogsBodySchemaOverride(operation *sourceOperation) {
	if operation == nil {
		return
	}
	logKV := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"Key":   map[string]any{"type": "string"},
			"Value": map[string]any{"type": "string"},
		},
		"required": []string{"Key", "Value"},
	}
	logEntry := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"Time": map[string]any{
				"type":        "integer",
				"description": "Unix 毫秒时间戳（13 位），表示日志时间。",
			},
			"TimeNs": map[string]any{
				"type":        "integer",
				"description": "Nanosecond fraction within the same second. Use this only when you need sub-millisecond ordering.",
			},
			"Contents": map[string]any{
				"oneOf": []any{
					map[string]any{
						"type":                 "object",
						"additionalProperties": map[string]any{"type": "string"},
					},
					map[string]any{
						"type":  "array",
						"items": logKV,
					},
				},
			},
		},
	}
	groupEntry := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"Source":      map[string]any{"type": "string"},
			"FileName":    map[string]any{"type": "string"},
			"ContextFlow": map[string]any{"type": "string"},
			"LogTags": map[string]any{
				"oneOf": []any{
					map[string]any{
						"type":                 "object",
						"additionalProperties": map[string]any{"type": "string"},
					},
					map[string]any{
						"type":  "array",
						"items": logKV,
					},
				},
			},
			"Logs": map[string]any{
				"type":  "array",
				"items": logEntry,
			},
		},
		"required": []string{"Logs"},
	}
	operation.InputSchema["body"] = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"LogGroups": map[string]any{
				"type":  "array",
				"items": groupEntry,
			},
		},
		"required": []string{"LogGroups"},
	}
}

func overrideWeakBodyObjectField(inputSchema map[string]any, field string, schema map[string]any) {
	if strings.TrimSpace(field) == "" || inputSchema == nil {
		return
	}
	body, ok := inputSchema["body"].(map[string]any)
	if !ok || body == nil {
		return
	}
	props, ok := body["properties"].(map[string]any)
	if !ok || props == nil {
		return
	}
	current, ok := props[field].(map[string]any)
	if !ok || !isWeakBodyObjectSchema(current) {
		return
	}
	props[field] = schema
}

func isWeakBodyObjectSchema(schema map[string]any) bool {
	if len(schema) == 0 {
		return true
	}
	if props, ok := schema["properties"].(map[string]any); ok && len(props) > 0 {
		return false
	}
	if schema["oneOf"] != nil || schema["anyOf"] != nil || schema["allOf"] != nil {
		return false
	}
	if schema["additionalProperties"] != nil {
		return false
	}
	t, _ := schema["type"].(string)
	return strings.TrimSpace(t) == "" || strings.EqualFold(strings.TrimSpace(t), "object")
}

func shouldFilterManagedToolHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "content-length", "content-type", "content-md5", "x-tls-bodyrawsize", "x-tls-compresstype", "accesskey", "secretkey", "region", "servicename":
		return true
	default:
		return false
	}
}

func buildToolBodyInputSchema(params []swaggerParam, defs map[string]swaggerSchema, docParams []apiDocParam) map[string]any {
	schema := map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
	props := schema["properties"].(map[string]any)
	required := map[string]struct{}{}
	hasAny := false

	for _, p := range params {
		swaggerSchema := toolParamSchema(p, defs)
		name := strings.TrimSpace(p.Name)
		s, ok := swaggerSchema.(map[string]any)
		if ok && isObjectJSONSchema(s) {
			mergeToolObjectSchema(schema, s, required)
			hasAny = true
			continue
		}
		if name == "" {
			continue
		}
		props[name] = swaggerSchema
		hasAny = true
		if p.Required {
			required[name] = struct{}{}
		}
	}

	for _, p := range docParams {
		name := strings.TrimSpace(p.Name)
		if name == "" {
			continue
		}
		if !mergeDocBodyFieldToObject(schema, p) {
			props[name] = buildDocParamSchema(p)
		}
		if isDocParamRequired(p.RequiredText) {
			required[name] = struct{}{}
		}
	}

	if !hasAny && len(props) == 0 {
		return nil
	}
	if len(required) > 0 {
		req := make([]string, 0, len(required))
		for name := range required {
			req = append(req, name)
		}
		sort.Strings(req)
		schema["required"] = req
	}
	return schema
}

func mergeDocBodyFieldToObject(schema map[string]any, param apiDocParam) bool {
	_, ok := schema["properties"]
	if !ok {
		return false
	}
	return mergeDocFieldToObject(schema, param)
}

func isObjectJSONSchema(s map[string]any) bool {
	if s == nil {
		return false
	}
	if t, ok := s["type"].(string); ok {
		if strings.EqualFold(strings.TrimSpace(t), "object") {
			return true
		}
	}
	_, ok := s["properties"].(map[string]any)
	return ok
}

func mergeToolObjectSchema(dst, src map[string]any, required map[string]struct{}) {
	dstProps, ok := dst["properties"].(map[string]any)
	if !ok {
		dstProps = map[string]any{}
		dst["properties"] = dstProps
	}
	srcProps, ok := src["properties"].(map[string]any)
	if !ok {
		srcProps = map[string]any{}
	}
	for name, value := range srcProps {
		if _, exists := dstProps[name]; !exists {
			dstProps[name] = value
		}
	}
	for _, req := range jsonStringSlice(src["required"]) {
		req = strings.TrimSpace(req)
		if req == "" {
			continue
		}
		required[req] = struct{}{}
	}
	if additional, ok := src["additionalProperties"]; ok {
		dst["additionalProperties"] = additional
	}
}

func jsonStringSlice(v any) []string {
	switch typed := v.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			s, ok := item.(string)
			if !ok {
				continue
			}
			out = append(out, strings.TrimSpace(s))
		}
		return out
	default:
		return nil
	}
}

func toolParamSchema(p swaggerParam, defs map[string]swaggerSchema) any {
	if p.Schema != nil {
		return toolSchemaForParam(*p.Schema, defs, map[string]bool{}, 0)
	}
	if t := normalizeSwaggerType(strings.TrimSpace(p.Type)); t != "" {
		schema := map[string]any{"type": t}
		applySwaggerParamMetadata(schema, p)
		return schema
	}
	return map[string]any{}
}

func toolSchemaForParam(s swaggerSchema, defs map[string]swaggerSchema, seen map[string]bool, depth int) map[string]any {
	// Real public schemas such as shipper.ContentInfo -> CsvInfo -> Keys[] and
	// ParquetInfo -> Fields[] legitimately nest beyond 8 levels once refs/allOf
	// wrappers are expanded. seen already guards ref cycles, so allow a deeper
	// traversal before falling back to a weak object schema.
	if depth > 16 {
		return map[string]any{"type": "object"}
	}
	ref := strings.TrimSpace(s.Ref)
	if ref != "" {
		if seen[ref] {
			return map[string]any{"type": "object"}
		}
		seen[ref] = true
		s2, ok := resolveSwaggerRef(ref, defs)
		if !ok {
			delete(seen, ref)
			return map[string]any{"$ref": ref}
		}
		defer func() {
			delete(seen, ref)
		}()
		schema := toolSchemaForParam(s2, defs, seen, depth+1)
		applySwaggerSchemaMetadata(schema, s)
		return schema
	}
	if len(s.AllOf) > 0 {
		schema := toolSchemaForAllOf(s.AllOf, defs, seen, depth)
		applySwaggerSchemaMetadata(schema, s)
		return schema
	}
	t := normalizeSwaggerType(strings.TrimSpace(s.Type))
	if t == "" && len(s.Properties) > 0 {
		t = "object"
	}
	switch t {
	case "":
		if child, ok := parseAdditionalProperties(s.AdditionalProperties); ok {
			schema := map[string]any{
				"type":                 "object",
				"additionalProperties": toolSchemaForParam(child, defs, seen, depth+1),
			}
			applySwaggerSchemaMetadata(schema, s)
			return schema
		}
		schema := map[string]any{}
		applySwaggerSchemaMetadata(schema, s)
		return schema
	case "array":
		schema := map[string]any{"type": "array"}
		if s.Items != nil {
			schema["items"] = toolSchemaForParam(*s.Items, defs, seen, depth+1)
		}
		applySwaggerSchemaMetadata(schema, s)
		return schema
	case "object":
		schema := map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}
		props := schema["properties"].(map[string]any)
		keys := make([]string, 0, len(s.Properties))
		for key := range s.Properties {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			prop := s.Properties[key]
			props[key] = toolSchemaForParam(prop, defs, seen, depth+1)
		}
		if len(s.Required) > 0 {
			required := make([]string, len(s.Required))
			copy(required, s.Required)
			sort.Strings(required)
			schema["required"] = required
		}
		if child, ok := parseAdditionalProperties(s.AdditionalProperties); ok {
			schema["additionalProperties"] = toolSchemaForParam(child, defs, seen, depth+1)
		}
		applySwaggerSchemaMetadata(schema, s)
		return schema
	default:
		schema := map[string]any{"type": t}
		applySwaggerSchemaMetadata(schema, s)
		return schema
	}
}

func toolSchemaForAllOf(parts []swaggerSchema, defs map[string]swaggerSchema, seen map[string]bool, depth int) map[string]any {
	if len(parts) == 0 {
		return map[string]any{}
	}
	schemas := make([]map[string]any, 0, len(parts))
	objectLike := false
	for _, part := range parts {
		schema := toolSchemaForParam(part, defs, seen, depth+1)
		if len(schema) == 0 {
			continue
		}
		if isObjectJSONSchema(schema) || schema["additionalProperties"] != nil {
			objectLike = true
		}
		schemas = append(schemas, schema)
	}
	if len(schemas) == 0 {
		return map[string]any{}
	}
	if !objectLike {
		merged := cloneToolSchemaMap(schemas[0])
		for _, schema := range schemas[1:] {
			mergeToolSchemaMetadata(merged, schema)
		}
		return merged
	}

	merged := map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
	required := map[string]struct{}{}
	for _, schema := range schemas {
		if isObjectJSONSchema(schema) || schema["additionalProperties"] != nil {
			mergeToolObjectSchema(merged, schema, required)
		}
		mergeToolSchemaMetadata(merged, schema)
	}
	if len(required) > 0 {
		req := make([]string, 0, len(required))
		for name := range required {
			req = append(req, name)
		}
		sort.Strings(req)
		merged["required"] = req
	}
	return merged
}

func applySwaggerParamMetadata(schema map[string]any, p swaggerParam) {
	if desc := strings.TrimSpace(p.Description); desc != "" {
		schema["description"] = desc
	}
	if format := strings.TrimSpace(p.Format); format != "" {
		schema["format"] = format
	}
	if len(p.Enum) > 0 {
		schema["enum"] = cloneToolSchemaValue(p.Enum)
	}
	if pattern := strings.TrimSpace(p.Pattern); pattern != "" {
		schema["pattern"] = pattern
	}
	if p.Minimum != nil {
		schema["minimum"] = *p.Minimum
	}
	if p.Maximum != nil {
		schema["maximum"] = *p.Maximum
	}
	if p.MinLength != nil {
		schema["minLength"] = *p.MinLength
	}
	if p.MaxLength != nil {
		schema["maxLength"] = *p.MaxLength
	}
}

func applySwaggerSchemaMetadata(schema map[string]any, s swaggerSchema) {
	if desc := strings.TrimSpace(s.Description); desc != "" {
		schema["description"] = desc
	}
	if format := strings.TrimSpace(s.Format); format != "" {
		schema["format"] = format
	}
	if len(s.Enum) > 0 {
		schema["enum"] = cloneToolSchemaValue(s.Enum)
	}
	if pattern := strings.TrimSpace(s.Pattern); pattern != "" {
		schema["pattern"] = pattern
	}
	if s.Minimum != nil {
		schema["minimum"] = *s.Minimum
	}
	if s.Maximum != nil {
		schema["maximum"] = *s.Maximum
	}
	if s.MinLength != nil {
		schema["minLength"] = *s.MinLength
	}
	if s.MaxLength != nil {
		schema["maxLength"] = *s.MaxLength
	}
	if s.Default != nil {
		schema["default"] = cloneToolSchemaValue(s.Default)
	}
}

func mergeToolSchemaMetadata(dst, src map[string]any) {
	for _, key := range []string{"description", "format", "enum", "pattern", "minimum", "maximum", "minLength", "maxLength", "default"} {
		if _, exists := dst[key]; exists {
			continue
		}
		if value, ok := src[key]; ok {
			dst[key] = cloneToolSchemaValue(value)
		}
	}
}

func cloneToolSchemaMap(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for key, value := range src {
		dst[key] = cloneToolSchemaValue(value)
	}
	return dst
}

func cloneToolSchemaValue(src any) any {
	switch typed := src.(type) {
	case map[string]any:
		return cloneToolSchemaMap(typed)
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, cloneToolSchemaValue(item))
		}
		return out
	case []string:
		out := make([]string, len(typed))
		copy(out, typed)
		return out
	default:
		return typed
	}
}

func resolveSwaggerRef(ref string, defs map[string]swaggerSchema) (swaggerSchema, bool) {
	name := strings.TrimPrefix(strings.TrimSpace(ref), "#/definitions/")
	s, ok := defs[name]
	if !ok {
		return swaggerSchema{}, false
	}
	return s, true
}

func isDocParamRequired(requiredText string) bool {
	text := strings.ToLower(strings.TrimSpace(requiredText))
	return text == "是" || strings.HasPrefix(text, "是") || strings.Contains(text, "required") || text == "true"
}

func buildDocParamSchema(p apiDocParam) map[string]any {
	rawType := strings.TrimSpace(p.Type)
	schemaType := mapDocTypeToJSONSchema(rawType)
	schema := map[string]any{
		"type": schemaType,
	}
	if schemaType == "array" {
		schema["items"] = mapDocArrayItemType(strings.TrimPrefix(strings.ToLower(rawType), "array"))
	}
	if p.Description != "" {
		schema["description"] = strings.TrimSpace(p.Description)
	}
	return schema
}

func mapDocTypeToJSONSchema(raw string) string {
	t := strings.TrimSpace(strings.ToLower(raw))
	switch {
	case t == "":
		return "string"
	case strings.Contains(t, "array of"):
		return "array"
	case strings.HasPrefix(t, "array"):
		return "array"
	case t == "string":
		return "string"
	case t == "integer" || t == "int" || t == "int32" || t == "int64" || t == "long":
		return "integer"
	case t == "bool" || t == "boolean":
		return "boolean"
	case t == "number" || t == "float" || t == "double":
		return "number"
	case t == "object" || strings.HasPrefix(t, "map"):
		return "object"
	case t == "any":
		return "object"
	default:
		return "string"
	}
}

func mapDocArrayItemType(raw string) map[string]any {
	t := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case strings.Contains(t, "string"):
		return map[string]any{"type": "string"}
	case strings.Contains(t, "integer") || strings.Contains(t, "int") || strings.Contains(t, "long"):
		return map[string]any{"type": "integer"}
	case strings.Contains(t, "bool"):
		return map[string]any{"type": "boolean"}
	case strings.Contains(t, "number") || strings.Contains(t, "float") || strings.Contains(t, "double"):
		return map[string]any{"type": "number"}
	case strings.Contains(t, "object") || strings.Contains(t, "map"):
		return map[string]any{"type": "object"}
	default:
		return map[string]any{"type": "string"}
	}
}

func mergeDocFieldToObject(obj any, p apiDocParam) bool {
	mp, ok := obj.(map[string]any)
	if !ok || mp == nil {
		return false
	}
	if typeValue, ok := mp["type"]; ok {
		if typeVal, ok := typeValue.(string); ok && typeVal != "object" {
			return false
		}
	}
	props, ok := mp["properties"].(map[string]any)
	if !ok {
		props = map[string]any{}
		mp["properties"] = props
	}
	if mp["type"] == nil {
		mp["type"] = "object"
	}
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return false
	}
	if _, ok := props[name]; ok {
		return true
	}
	schema := map[string]any{
		"type": mapDocTypeToJSONSchema(p.Type),
	}
	if strings.TrimSpace(p.Type) != "" && strings.HasPrefix(strings.ToLower(strings.TrimSpace(p.Type)), "array") {
		schema["type"] = "array"
		schema["items"] = mapDocArrayItemType(strings.TrimSpace(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(p.Type)), "array of")))
	}
	if p.Description != "" {
		schema["description"] = strings.TrimSpace(p.Description)
	}
	props[name] = schema
	return true
}

func normalizeSwaggerType(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "string", "integer", "number", "boolean", "array", "object":
		return strings.ToLower(strings.TrimSpace(raw))
	case "float", "double":
		return "number"
	case "int", "int32", "int64", "long":
		return "integer"
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

func defaultToolContextSchema() map[string]any {
	execSchema := defaultToolExecutionSchema()
	execProps, _ := execSchema["properties"].(map[string]any)
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"region": map[string]any{
				"type":           "string",
				"description":    "Override the TLS region for this tool execution.",
				"when_to_use":    "Set this when the target resource is not in your configured default region.",
				"default":        "Use the configured default region or environment when omitted.",
				"runtime_effect": "Writes ctx.defaults.Region before the request is built.",
			},
			"profile": map[string]any{
				"type":           "string",
				"description":    "Select the named local profile to use for this tool execution.",
				"when_to_use":    "Set this when you want tool exec to read credentials and defaults from a specific saved profile.",
				"default":        "Use the active CLI profile when omitted.",
				"runtime_effect": "Loads credentials and defaults from the selected profile. If global --profile is also set, conflicting selectors fail fast instead of silently overriding each other.",
			},
			"secrets_file": map[string]any{
				"type":           "string",
				"description":    "Load credentials and defaults from a secrets file before applying the rest of the context.",
				"when_to_use":    "Set this when credentials are stored in a file instead of relying on the active profile or environment variables.",
				"default":        "Do not load an extra secrets file.",
				"runtime_effect": "Resolves profile/secrets selectors first; if this secrets_file wins, runtime loads supported VOLCENGINE_* values from the file before the request is built.",
			},
			"endpoint": map[string]any{
				"type":           "string",
				"description":    "Override the TLS API endpoint for this tool execution.",
				"when_to_use":    "Set this when you need to call a custom endpoint, private endpoint, or a non-default region endpoint.",
				"default":        "Use the configured endpoint for the selected region/profile.",
				"runtime_effect": "Writes ctx.defaults.Endpoint before the request is built.",
			},
			"trace": map[string]any{
				"description":    "Control request/response tracing for tool execution.",
				"when_to_use":    "Set this when you need transport traces for debugging, auditing, or acceptance verification.",
				"default":        false,
				"runtime_effect": "Enables trace capture, chooses the trace directory, and normalizes legacy strict/default redact inputs to the current on/off setting.",
				"oneOf": []any{
					map[string]any{
						"type":        "boolean",
						"description": "true enables tracing with an auto-selected directory; false keeps tracing disabled.",
					},
					map[string]any{
						"type":        "string",
						"description": "String form sets the trace directory directly.",
					},
					map[string]any{
						"type":        "object",
						"description": "Object form enables tracing and lets you control directory and redaction behavior.",
						"properties": map[string]any{
							"enabled": map[string]any{"type": "boolean"},
							"dir":     map[string]any{"type": "string"},
							"redact":  map[string]any{"type": "string"},
						},
					},
				},
			},
			"contract_digest": map[string]any{
				"type":           "string",
				"description":    "Optional advisory digest copied from tool describe output.",
				"when_to_use":    "Set this when you want tool exec to warn if the contract changed between describe and exec.",
				"default":        "",
				"runtime_effect": "Compared advisory-only during tool exec; mismatch adds a warning but does not block execution.",
			},
			"execution": map[string]any{
				"type":           "object",
				"description":    "Nested execution controls for tool exec.",
				"when_to_use":    "Set this when you need dry-run, projection, artifact output, or page.all behavior.",
				"default":        map[string]any{},
				"runtime_effect": "The tool runtime reads this nested object from context.execution; do not pass execution as a separate file.",
				"properties":     execProps,
			},
		},
	}
}

func defaultToolExecutionSchema() map[string]any {
	return map[string]any{
		"type":        "object",
		"description": "Execution controls that must be nested under context.execution in the --context JSON file.",
		"properties": map[string]any{
			"projection": map[string]any{
				"description": "Filter the raw tool result before the CLI envelope is built.",
				"oneOf": []any{
					map[string]any{
						"type":        "string",
						"description": "Single JMES expression.",
					},
					map[string]any{
						"type":        "array",
						"description": "Array form; the runtime uses the first non-empty expression.",
						"items":       map[string]any{"type": "string"},
					},
					map[string]any{
						"type":        "object",
						"description": "Object form with an explicit jmes field.",
						"properties": map[string]any{
							"jmes": map[string]any{"type": "string"},
						},
					},
				},
			},
			"page": map[string]any{
				"type":        "object",
				"description": "Pagination execution controls.",
				"properties": map[string]any{
					"all": map[string]any{
						"type":        "boolean",
						"description": "When supported, iterate all pages instead of a single request.",
					},
				},
			},
			"page_all": map[string]any{
				"type":        "boolean",
				"description": "Compatibility alias for page.all. Prefer the nested page.all form in new context files.",
			},
			"artifact": map[string]any{
				"description": "Write tool output to a file-oriented artifact flow instead of stdout-only output.",
				"oneOf": []any{
					map[string]any{
						"type":        "boolean",
						"description": "true enables artifact/file mode using the default output path policy.",
					},
					map[string]any{
						"type":        "string",
						"description": "String form enables artifact mode and uses the string as the output path.",
					},
					map[string]any{
						"type":        "object",
						"description": "Object form enables artifact mode with an explicit path field.",
						"properties": map[string]any{
							"path": map[string]any{"type": "string"},
						},
					},
				},
			},
			"dry_run": map[string]any{
				"type":        "boolean",
				"description": "Validate and render the request without issuing a mutating remote call.",
			},
		},
	}
}

func inferToolDocSource(summary, path, groupTitle string, docEntry apiDocEntry) string {
	if strings.TrimSpace(groupTitle) != "" {
		return strings.TrimSpace(groupTitle)
	}
	if strings.TrimSpace(docEntry.GroupTitle) != "" {
		return strings.TrimSpace(docEntry.GroupTitle)
	}
	if strings.TrimSpace(summary) != "" {
		return strings.TrimSpace(summary)
	}
	if strings.TrimSpace(path) != "" {
		return strings.TrimSpace(path)
	}
	return "swagger"
}

func inferToolOutputPolicy(action, method string) string {
	if strings.TrimSpace(strings.ToLower(method)) == "get" && strings.Contains(strings.ToLower(action), "describe") {
		return "envelope"
	}
	if strings.TrimSpace(strings.ToLower(method)) == "get" {
		return "stream"
	}
	return "full"
}

func inferToolErrorRecovery(action, method string) string {
	if strings.TrimSpace(strings.ToLower(method)) == "get" {
		return "safe-retry"
	}
	if strings.Contains(strings.ToLower(action), "create") || strings.Contains(strings.ToLower(action), "modify") || strings.Contains(strings.ToLower(action), "delete") {
		return "high-risk-retry"
	}
	return "retry"
}

func inferToolUsageConstraints(action, group, path string) string {
	_ = action
	_ = group
	_ = path
	return ""
}

func inferToolRiskLevel(action, method string) string {
	m := strings.ToUpper(strings.TrimSpace(method))
	if isReadLikeAction(action) || m == "GET" || m == "HEAD" || m == "OPTIONS" {
		return "low"
	}
	if m == "POST" || m == "PUT" || m == "DELETE" || m == "PATCH" {
		return "high"
	}
	return "medium"
}

func inferToolActionVerb(action, method string) string {
	verb, _ := splitVerbNoun(strings.TrimSpace(action))
	verb = strings.ToLower(strings.TrimSpace(verb))
	if verb != "" {
		return verb
	}
	return inferToolActionVerbFromMethod(method)
}

func inferToolActionVerbFromMethod(method string) string {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case "GET", "HEAD", "OPTIONS":
		return "describe"
	case "POST":
		return "create"
	case "PUT":
		return "modify"
	case "DELETE", "PATCH":
		return "delete"
	default:
		return toKebab(method)
	}
}

func inferSupportsAll(action, method string, params []swaggerParam) bool {
	if !strings.EqualFold(strings.TrimSpace(method), "GET") {
		return false
	}
	action = strings.TrimSpace(action)
	if !strings.HasPrefix(action, "Describe") || !strings.HasSuffix(action, "s") {
		return false
	}
	hasPageNumber := false
	hasCursor := false
	for _, param := range params {
		if !strings.EqualFold(strings.TrimSpace(param.In), "query") {
			continue
		}
		switch strings.TrimSpace(param.Name) {
		case "PageNumber":
			hasPageNumber = true
		case "Cursor":
			hasCursor = true
		}
	}
	return hasPageNumber || hasCursor
}

func isReadLikeAction(action string) bool {
	a := strings.ToLower(strings.TrimSpace(action))
	switch {
	case strings.HasPrefix(a, "describe"), strings.HasPrefix(a, "get"), strings.HasPrefix(a, "list"), strings.HasPrefix(a, "search"):
		return true
	default:
		return false
	}
}

func normalizeResourceToken(s string) string {
	out := toKebab(strings.TrimSpace(s))
	out = strings.TrimSuffix(out, "-s")
	return out
}

func inferResourceFromPath(path string) string {
	p := strings.Trim(strings.TrimSpace(path), "/")
	if p == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(p), "batch") {
		p = strings.TrimPrefix(p, "batch")
		p = strings.Trim(p, "/")
	}
	parts := strings.Split(p, "/")
	if len(parts) == 0 {
		return ""
	}
	for i := len(parts) - 1; i >= 0; i-- {
		part := strings.TrimSpace(parts[i])
		if part == "" || strings.HasPrefix(part, "{") || strings.HasSuffix(part, "}") {
			continue
		}
		return part
	}
	return ""
}

func loadAPIDocIndex(root string) (map[string]apiDocEntry, error) {
	out := map[string]apiDocEntry{}
	err := filepath.WalkDir(filepath.Clean(root), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		action := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		groupTitle := filepath.Base(filepath.Dir(path))
		out[action] = apiDocEntry{
			GroupTitle:       strings.TrimSpace(groupTitle),
			RequestParamsDoc: parseDocRequestParamsMarkdown(string(b)),
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func parseDocRequestParamsMarkdown(md string) []apiDocParam {
	lines := strings.Split(md, "\n")
	inRequest := false
	currentLoc := ""
	lastIdx := -1
	out := make([]apiDocParam, 0, 16)
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "## "):
			title := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			if title == "请求参数" {
				inRequest = true
				currentLoc = ""
				lastIdx = -1
				continue
			}
			if inRequest {
				return out
			}
		case !inRequest:
			continue
		case strings.HasPrefix(line, "### "):
			currentLoc = normalizeDocParamLocation(strings.TrimSpace(strings.TrimPrefix(line, "### ")))
			lastIdx = -1
		case strings.HasPrefix(line, "|"):
			cells := parseMarkdownTableRow(line)
			if len(cells) < 5 || isMarkdownTableSeparator(cells) || isMarkdownTableHeader(cells) {
				continue
			}
			if !isTopLevelParamLocation(currentLoc) {
				continue
			}
			name := strings.TrimSpace(cells[0])
			if name == "" {
				if lastIdx >= 0 {
					extra := strings.TrimSpace(stripMarkdownText(cells[4]))
					if extra != "" {
						prev := strings.TrimSpace(out[lastIdx].Description)
						if prev == "" {
							out[lastIdx].Description = extra
						} else {
							out[lastIdx].Description = prev + " " + extra
						}
					}
				}
				continue
			}
			out = append(out, apiDocParam{
				Name:         name,
				In:           currentLoc,
				Type:         strings.TrimSpace(stripMarkdownText(cells[1])),
				RequiredText: strings.TrimSpace(stripMarkdownText(cells[2])),
				Example:      strings.TrimSpace(stripMarkdownText(cells[3])),
				Description:  strings.TrimSpace(stripMarkdownText(cells[4])),
			})
			lastIdx = len(out) - 1
		}
	}
	return out
}

func parseMarkdownTableRow(line string) []string {
	trimmed := strings.Trim(line, "|")
	parts := strings.Split(trimmed, "|")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		out = append(out, strings.TrimSpace(part))
	}
	return out
}

func isMarkdownTableSeparator(cells []string) bool {
	for _, cell := range cells {
		cell = strings.Trim(cell, "-: ")
		if cell != "" {
			return false
		}
	}
	return true
}

func isMarkdownTableHeader(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	first := stripMarkdownText(cells[0])
	second := ""
	if len(cells) > 1 {
		second = stripMarkdownText(cells[1])
	}
	return strings.Contains(first, "参数") && strings.Contains(second, "类型")
}

func normalizeDocParamLocation(title string) string {
	switch strings.ToLower(strings.TrimSpace(title)) {
	case "body":
		return "body"
	case "query":
		return "query"
	case "path":
		return "path"
	case "header":
		return "header"
	default:
		return strings.ToLower(strings.TrimSpace(title))
	}
}

func isTopLevelParamLocation(loc string) bool {
	switch strings.TrimSpace(loc) {
	case "body", "query", "path", "header":
		return true
	default:
		return false
	}
}

func stripMarkdownText(s string) string {
	replacer := strings.NewReplacer("**", "", "`", "", "\\|", "|", "<br>", " ", "<br/>", " ", "<br />", " ", "&nbsp;", " ")
	s = replacer.Replace(strings.TrimSpace(s))
	for {
		start := strings.Index(s, "[")
		mid := strings.Index(s, "](")
		end := strings.Index(s, ")")
		if start == -1 || mid == -1 || end == -1 || !(start < mid && mid < end) {
			break
		}
		s = s[:start] + s[start+1:mid] + s[end+1:]
	}
	return strings.Join(strings.Fields(s), " ")
}

func parseAdditionalProperties(raw json.RawMessage) (swaggerSchema, bool) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return swaggerSchema{}, false
	}
	var enabled bool
	if err := json.Unmarshal(raw, &enabled); err == nil {
		if enabled {
			return swaggerSchema{Type: "string"}, true
		}
		return swaggerSchema{}, false
	}
	var s swaggerSchema
	if err := json.Unmarshal(raw, &s); err != nil {
		return swaggerSchema{}, false
	}
	return s, true
}

func mergeParams(a []swaggerParam, b []swaggerParam) []swaggerParam {
	out := make([]swaggerParam, 0, len(a)+len(b))
	seen := map[string]struct{}{}
	push := func(p swaggerParam) {
		key := strings.ToLower(strings.TrimSpace(p.In)) + ":" + strings.ToLower(strings.TrimSpace(p.Name))
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, p)
	}
	for _, p := range a {
		push(p)
	}
	for _, p := range b {
		push(p)
	}
	return out
}

func loadSimpleYAMLMapping(path string) (map[string]string, error) {
	out := map[string]string{}
	p := strings.TrimSpace(path)
	if p == "" {
		return out, nil
	}
	b, err := os.ReadFile(filepath.Clean(p))
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key := strings.TrimSpace(k)
		val := strings.TrimSpace(v)
		if key == "" || val == "" {
			continue
		}
		out[key] = val
	}
	return out, nil
}

func loadGroupKeyMapping(path string) (map[string]string, error) {
	return loadSimpleYAMLMapping(path)
}

func resolveGroupTitle(tags []string, tagTitles map[string]string, fallback string) string {
	for _, tag := range tags {
		if v := strings.TrimSpace(tagTitles[strings.TrimSpace(tag)]); v != "" {
			return v
		}
	}
	for _, tag := range tags {
		if v := strings.TrimSpace(tag); v != "" {
			return v
		}
	}
	return strings.TrimSpace(fallback)
}

func groupName(groupTitle string, mapping map[string]string) string {
	groupTitle = strings.TrimSpace(groupTitle)
	if groupTitle == "" {
		return "misc"
	}
	if v := strings.TrimSpace(mapping[groupTitle]); v != "" {
		return v
	}
	if v := toKebab(groupTitle); v != "" {
		return v
	}
	return "misc"
}

func actionName(group, summary, method, path string) string {
	_ = group
	s := strings.TrimSpace(summary)
	if s != "" {
		return s
	}
	return fallbackActionName(path, method)
}

func fallbackActionName(path string, method string) string {
	parts := strings.FieldsFunc(strings.Trim(path, "/"), func(r rune) bool {
		return r == '/' || r == '.' || r == '-' || r == '_'
	})
	if len(parts) == 0 {
		return strings.ToUpper(strings.TrimSpace(method))
	}
	var b strings.Builder
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		runes := []rune(part)
		if len(runes) == 0 {
			continue
		}
		first := runes[0]
		if first >= 'a' && first <= 'z' {
			runes[0] = first - ('a' - 'A')
		}
		b.WriteString(string(runes))
	}
	if b.Len() == 0 {
		return strings.ToUpper(strings.TrimSpace(method))
	}
	return b.String()
}

func splitVerbNoun(summary string) (string, string) {
	prefixes := []struct {
		prefix string
		verb   string
	}{
		{"WebTracks", "track"},
		{"WebTrack", "track"},
		{"Describe", "describe"},
		{"Create", "create"},
		{"Modify", "modify"},
		{"Update", "update"},
		{"Delete", "delete"},
		{"Search", "search"},
		{"Export", "export"},
		{"Get", "get"},
		{"List", "list"},
		{"Consume", "consume"},
		{"Cancel", "cancel"},
		{"Put", "put"},
		{"Enable", "enable"},
		{"Disable", "disable"},
		{"Reset", "reset"},
		{"Try", "try"},
		{"Add", "add"},
		{"Remove", "remove"},
		{"Associate", "associate"},
		{"Disassociate", "disassociate"},
		{"Active", "active"},
		{"Encrypt", "encrypt"},
	}
	for _, p := range prefixes {
		if strings.HasPrefix(summary, p.prefix) {
			return p.verb, strings.TrimSpace(summary[len(p.prefix):])
		}
	}
	return "", summary
}

func disambiguateAction(action, path, method string, used map[string]string) string {
	suffix := pathSuffix(path)
	if suffix == "" {
		suffix = strings.ToLower(method)
	}
	a := action + "-" + suffix
	if _, ok := used[a]; !ok {
		return a
	}
	return a + "-" + strings.ToLower(method)
}

func pathSuffix(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i := len(parts) - 1; i >= 0; i-- {
		p := strings.TrimSpace(parts[i])
		if p == "" || strings.HasPrefix(p, "{") {
			continue
		}
		return toKebab(p)
	}
	return ""
}

func toKebab(s string) string {
	var b strings.Builder
	prevDash := false
	for i, r := range strings.TrimSpace(s) {
		switch {
		case r >= 'A' && r <= 'Z':
			if i > 0 && !prevDash {
				b.WriteByte('-')
			}
			b.WriteRune(r + ('a' - 'A'))
			prevDash = false
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	return out
}

func writeFile(path string, data []byte) error {
	p := filepath.Clean(path)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}
