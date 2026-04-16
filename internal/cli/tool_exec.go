package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/output"
)

func runToolExec(ctx *Context, args []string) (any, error) {
	if ctx == nil {
		return nil, errors.New("missing cli context")
	}
	if hasHelp(args) {
		return nil, &usageError{Text: usageToolExec(), ExitCode: 0}
	}
	if len(args) == 0 {
		return nil, errors.New("missing tool identity: <group.action>")
	}

	identity := strings.TrimSpace(args[0])
	group, action, ok := parseToolIdentity(identity)
	if !ok {
		return nil, errors.New("invalid tool identity: must be <group.action>")
	}

	contextArg := ""
	inputArg := ""
	pageAllFlag := false
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--context":
			if i+1 >= len(args) {
				return nil, errors.New("missing --context value")
			}
			contextArg = args[i+1]
			i++
		case "--input":
			if i+1 >= len(args) {
				return nil, errors.New("missing --input value")
			}
			inputArg = args[i+1]
			i++
		case "--page-all":
			pageAllFlag = true
		default:
			return nil, errors.New("unknown flag: " + args[i])
		}
	}

	contract, err := resolveToolByIdentity(group, action)
	if err != nil {
		return nil, err
	}
	ctxCfg := toolExecContext{Execution: map[string]any{}}
	if strings.TrimSpace(contextArg) != "" {
		ctxCfg, err = loadToolExecContext(contextArg)
		if err != nil {
			return nil, err
		}
	}
	input := map[string]any{}
	if strings.TrimSpace(inputArg) != "" {
		input, err = readToolJSONObjectFlag("--input", inputArg)
		if err != nil {
			return nil, err
		}
	}
	input, err = normalizeToolExecInput(contract, input)
	if err != nil {
		return nil, err
	}
	if err := validateToolExecInput(contract, input); err != nil {
		return nil, err
	}
	if err := applyToolExecContext(ctx, ctxCfg); err != nil {
		return nil, err
	}

	rawFilter := strings.TrimSpace(ctx.Filter)
	ctx.Filter = ""
	options := resolveToolExecutionOptions(ctxCfg)
	if pageAllFlag {
		options.PageAll = true
	}
	if options.DryRun {
		ctx.DryRun = true
	}
	if options.Artifact {
		ctx.OutputMode = "file"
		if strings.TrimSpace(options.ArtifactPath) != "" {
			ctx.OutputFile = strings.TrimSpace(options.ArtifactPath)
		}
	}

	method, path, query, header, body, err := buildToolExecRequest(contract, input)
	if err != nil {
		return nil, err
	}
	ctx.Action = "tool." + strings.TrimSpace(contract.ID)
	ctx.apiIOMeta = apiIOMeta{
		Group:         group,
		Action:        normalizeActionToken(contract.Action),
		Method:        method,
		Path:          path,
		RequestFormat: requestFormatJSON,
		OutputFormat:  ctx.Format,
		OutputMode:    ctx.OutputMode,
	}

	warnings := toolDigestWarnings(contract, ctxCfg.ContractDigest)
	result, err := runToolExecAction(ctx, contract, method, path, query, header, body, options)
	if err != nil {
		return nil, err
	}
	filteredResult, err := applyToolExecFilters(result, options.Projection, rawFilter)
	if err != nil {
		return nil, err
	}
	filteredResult = stabilizeProjectedToolResult(result, filteredResult, options.Projection, rawFilter)
	env, err := buildAPIEnvelope(ctx, "tool", filteredResult, ctx.OutputMode, ctx.OutputFile, ctx.Format)
	if err != nil {
		return nil, err
	}
	env, err = finalizeToolExecEnvelope(ctx, filteredResult, env, options)
	if err != nil {
		return nil, err
	}
	env["action"] = ctx.Action
	env["contract_digest"] = buildToolContractDigestStatus(contract, ctxCfg.ContractDigest)
	if len(warnings) > 0 {
		env["warnings"] = warnings
	}
	return env, nil
}

func runToolExecAction(ctx *Context, contract toolCatalog, method, path string, query, header map[string]string, body []byte, options toolExecutionOptions) (any, error) {
	if options.PageAll {
		toolID := strings.TrimSpace(contract.ID)
		if !contract.SupportsAll {
			return nil, fmt.Errorf("execution.page.all is not supported for tool: %s", toolID)
		}
		if ctx.DryRun {
			out, err := ctx.Do(method, path, query, header, body)
			if err != nil {
				return nil, err
			}
			return annotateToolDryRunPageAll(out), nil
		}
		ops, ok := loadToolAPIOps(contract)
		if !ok || len(ops) == 0 {
			return nil, fmt.Errorf("tool %s declares page.all support but runtime pagination metadata is unavailable", toolID)
		}
		selected, ok := selectOpByMethod(ops, method)
		if !ok || !supportsGeneratedActionAll(selected) {
			return nil, fmt.Errorf("tool %s declares page.all support but runtime pagination execution is unavailable", toolID)
		}
		return runGeneratedActionAll(ctx, selected, contract.Action, path, query, header, body)
	}
	return ctx.Do(method, path, query, header, body)
}

func applyToolExecFilters(result any, expressions ...string) (any, error) {
	out := result
	for _, expr := range expressions {
		filter := strings.TrimSpace(expr)
		if filter == "" {
			continue
		}
		next, err := output.ApplyFilter(out, filter)
		if err != nil {
			return nil, err
		}
		out = next
	}
	return out, nil
}

func stabilizeProjectedToolResult(raw, filtered any, expressions ...string) any {
	if !hasToolProjectionExpression(expressions...) {
		return filtered
	}
	if !toolProjectionNeedsEnvelopeWrap(filtered) {
		return filtered
	}
	out := map[string]any{
		"items": filtered,
	}
	for key, value := range extractToolProjectionMetadata(raw) {
		out[key] = value
	}
	return out
}

func hasToolProjectionExpression(expressions ...string) bool {
	for _, expr := range expressions {
		if strings.TrimSpace(expr) != "" {
			return true
		}
	}
	return false
}

func toolProjectionNeedsEnvelopeWrap(v any) bool {
	switch v.(type) {
	case []any, []map[string]any:
		return true
	default:
		return false
	}
}

func extractToolProjectionMetadata(raw any) map[string]any {
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	out := map[string]any{}
	for _, key := range []string{"Total", "Count", "HasMore", "ListOver", "Cursor", "NextToken", "PrevToken", "PageNumber", "PageSize"} {
		if value, ok := obj[key]; ok {
			out[key] = value
		}
	}
	return out
}

func annotateToolDryRunPageAll(result any) any {
	plan, ok := result.(map[string]any)
	if !ok {
		return result
	}
	plan["page_all"] = map[string]any{
		"requested": true,
		"mode":      "dry_run",
		"note":      "dry-run validates the first request shape; real execution will iterate all supported pages",
	}
	return plan
}

func loadToolAPIOps(contract toolCatalog) ([]apiActionOp, bool) {
	doc, err := loadAPICapabilities()
	if err != nil {
		return nil, false
	}
	index := buildAPIIndex(doc)
	group := normalizeToken(contract.Group)
	action := normalizeActionToken(contract.Action)
	actions, ok := index[group]
	if !ok {
		return nil, false
	}
	ops := actions[action]
	if len(ops) == 0 {
		return nil, false
	}
	return ops, true
}

func buildToolExecRequest(contract toolCatalog, input map[string]any) (string, string, map[string]string, map[string]string, []byte, error) {
	method := strings.ToUpper(strings.TrimSpace(contract.Method))
	if method == "" {
		method = "GET"
	}
	path := strings.TrimSpace(contract.Path)
	query := sectionStringMap(input["query"])
	header := sectionStringMap(input["header"])
	pathValues := sectionStringMap(input["path"])

	bodyValue, bodyExists := input["body"]
	if !bodyExists && !hasToolInputSections(input) {
		bodyValue = input
		bodyExists = len(input) > 0
	}
	body := []byte("{}")
	if bodyExists {
		raw, err := json.Marshal(bodyValue)
		if err != nil {
			return "", "", nil, nil, nil, err
		}
		body = raw
	}
	for key, value := range pathValues {
		path = strings.ReplaceAll(path, "{"+key+"}", value)
	}
	if strings.Contains(path, "{") && strings.Contains(path, "}") {
		return "", "", nil, nil, nil, errors.New("path still contains unresolved params")
	}
	return method, path, query, header, body, nil
}

func normalizeToolExecInput(contract toolCatalog, input map[string]any) (map[string]any, error) {
	if len(input) == 0 || hasToolInputSections(input) || len(contract.InputSchema) == 0 {
		return input, nil
	}

	sectionOrder := []string{"path", "query", "header", "body"}
	presentSections := make([]string, 0, len(sectionOrder))
	sectionProps := map[string]map[string]any{}
	hasBody := false
	bodyAllowsLooseFields := false
	for _, section := range sectionOrder {
		raw, ok := contract.InputSchema[section].(map[string]any)
		if !ok || len(raw) == 0 {
			continue
		}
		presentSections = append(presentSections, section)
		props, _ := raw["properties"].(map[string]any)
		sectionProps[section] = props
		if section == "body" {
			hasBody = true
			bodyAllowsLooseFields = toolSchemaAllowsLooseFields(raw)
		}
	}
	if len(presentSections) == 0 {
		return input, nil
	}

	normalized := map[string]any{}
	ambiguous := make([]string, 0, 1)
	unknown := make([]string, 0, 1)
	for key, value := range input {
		matches := matchingToolInputSections(sectionProps, key)
		switch len(matches) {
		case 0:
			if hasBody && (len(presentSections) == 1 || bodyAllowsLooseFields) {
				assignToolInputSection(normalized, "body", key, value)
				continue
			}
			unknown = append(unknown, key)
		case 1:
			assignToolInputSection(normalized, matches[0], key, value)
		default:
			ambiguous = append(ambiguous, fmt.Sprintf("%s(%s)", key, strings.Join(matches, ",")))
		}
	}
	if len(ambiguous) > 0 {
		sort.Strings(ambiguous)
		return nil, fmt.Errorf("flat input has ambiguous fields: %s; use nested input sections {query,path,header,body}", strings.Join(ambiguous, ", "))
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("flat input contains unknown fields: %s", strings.Join(unknown, ", "))
	}
	return normalized, nil
}

func toolSchemaAllowsLooseFields(schema map[string]any) bool {
	if len(schema) == 0 {
		return false
	}
	if schema["additionalProperties"] != nil {
		return true
	}
	props, _ := schema["properties"].(map[string]any)
	return len(props) == 0
}

func matchingToolInputSections(sectionProps map[string]map[string]any, key string) []string {
	out := make([]string, 0, 2)
	for _, section := range []string{"path", "query", "header", "body"} {
		props := sectionProps[section]
		if len(props) == 0 {
			continue
		}
		if _, ok := props[key]; ok {
			out = append(out, section)
		}
	}
	return out
}

func assignToolInputSection(dst map[string]any, section, key string, value any) {
	section = strings.TrimSpace(section)
	if section == "" {
		return
	}
	current, _ := dst[section].(map[string]any)
	if current == nil {
		current = map[string]any{}
		dst[section] = current
	}
	current[key] = value
}

func validateToolExecInput(contract toolCatalog, input map[string]any) error {
	inputSchema := contract.InputSchema
	if len(inputSchema) == 0 {
		return nil
	}
	sectioned := hasToolInputSections(input)
	for _, section := range []string{"query", "path", "header", "body"} {
		sectionSchema, ok := inputSchema[section].(map[string]any)
		if !ok || len(sectionSchema) == 0 {
			continue
		}

		var sectionInput map[string]any
		if sectioned {
			sectionInput, _ = input[section].(map[string]any)
		} else if section == "body" {
			sectionInput = input
		}
		if sectionInput == nil {
			sectionInput = map[string]any{}
		}
		if err := validateToolSchemaRequiredFields(sectionSchema, sectionInput, "input."+section); err != nil {
			return err
		}
	}
	return nil
}

func validateToolSchemaRequiredFields(schema map[string]any, input map[string]any, path string) error {
	for _, name := range toolRequiredFields(schema["required"]) {
		value, ok := input[name]
		if !ok || value == nil {
			return fmt.Errorf("missing required field: %s.%s", path, name)
		}
	}

	props, _ := schema["properties"].(map[string]any)
	for name, raw := range props {
		childSchema, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		childInput, ok := input[name].(map[string]any)
		if !ok {
			continue
		}
		if err := validateToolSchemaRequiredFields(childSchema, childInput, path+"."+name); err != nil {
			return err
		}
	}
	return nil
}

func hasToolInputSections(input map[string]any) bool {
	for _, key := range []string{"query", "header", "path", "body"} {
		if _, ok := input[key]; ok {
			return true
		}
	}
	return false
}

func sectionStringMap(v any) map[string]string {
	src, ok := v.(map[string]any)
	if !ok || src == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(src))
	for key, value := range src {
		out[strings.TrimSpace(key)] = stringifyToolValue(value)
	}
	return out
}

func stringifyToolValue(v any) string {
	switch value := v.(type) {
	case string:
		return value
	case bool:
		if value {
			return "true"
		}
		return "false"
	case nil:
		return ""
	default:
		raw, err := json.Marshal(value)
		if err == nil && len(raw) > 0 && string(raw) != "null" {
			if raw[0] == '"' {
				var decoded string
				if err := json.Unmarshal(raw, &decoded); err == nil {
					return decoded
				}
			}
			return string(raw)
		}
		return fmt.Sprint(value)
	}
}

func toolDigestWarnings(contract toolCatalog, expected string) []map[string]any {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return nil
	}
	actual := toolContractForDigest(contract)
	if strings.EqualFold(expected, actual) {
		return nil
	}
	return []map[string]any{
		{
			"kind":     "contract_digest_mismatch",
			"expected": expected,
			"actual":   actual,
			"policy":   "soft",
			"message":  "contract digest mismatch is advisory; execution continues",
		},
	}
}

func buildToolContractDigestStatus(contract toolCatalog, expected string) map[string]any {
	actual := toolContractForDigest(contract)
	status := map[string]any{
		"value":  actual,
		"policy": "soft",
	}
	if strings.TrimSpace(expected) != "" {
		status["expected"] = strings.TrimSpace(expected)
		status["matched"] = strings.EqualFold(strings.TrimSpace(expected), actual)
	}
	return status
}
