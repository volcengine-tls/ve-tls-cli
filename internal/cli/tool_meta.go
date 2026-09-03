package cli

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
)

type toolDescribeView string

const (
	toolDescribeViewCompact toolDescribeView = "compact"
	toolDescribeViewFull    toolDescribeView = "full"
)

var (
	operationCatalogOnce sync.Once
	cachedCatalog        contract.Catalog
	cachedOperations     map[string]contract.Operation
	cachedCatalogErr     error
)

func loadOperationCatalog() (contract.Catalog, error) {
	operationCatalogOnce.Do(func() {
		catalog, err := contract.LoadEmbedded()
		if err != nil {
			cachedCatalogErr = err
			return
		}
		operations := make(map[string]contract.Operation, len(catalog.Operations))
		for _, operation := range catalog.Operations {
			operations[string(operation.ID)] = operation
		}
		cachedOperations = operations
		cachedCatalog = catalog
	})
	return cachedCatalog, cachedCatalogErr
}

func loadToolOperation(id string) (contract.Operation, bool) {
	if _, err := loadOperationCatalog(); err != nil {
		return contract.Operation{}, false
	}
	operation, ok := cachedOperations[strings.TrimSpace(id)]
	return operation, ok
}

func loadToolByIdentity(group, action string) (contract.Operation, bool) {
	tool, err := resolveToolByIdentity(group, action)
	if err != nil {
		return contract.Operation{}, false
	}
	return tool, true
}

func resolveToolByIdentity(group, action string) (contract.Operation, error) {
	catalog, err := loadOperationCatalog()
	if err != nil {
		return contract.Operation{}, err
	}
	g := normalizeToken(group)
	a := normalizeToken(action)
	if g == "" || a == "" {
		return contract.Operation{}, fmt.Errorf("unknown tool: %s.%s", strings.TrimSpace(group), strings.TrimSpace(action))
	}
	exact := make([]contract.Operation, 0, 1)
	verbAliases := make([]contract.Operation, 0, 2)
	for _, operation := range catalog.Operations {
		if operation.Visibility != "public" || normalizeToken(operation.Group) != g {
			continue
		}
		if toolIdentityMatchesAlias(operation, a) {
			exact = append(exact, operation)
			continue
		}
		if normalizeToken(operation.Verb) == a {
			verbAliases = append(verbAliases, operation)
		}
	}
	if len(exact) == 1 {
		return exact[0], nil
	}
	if len(exact) > 1 {
		return contract.Operation{}, ambiguousToolIdentityError(group, action, exact)
	}
	if len(verbAliases) == 1 {
		return verbAliases[0], nil
	}
	if len(verbAliases) > 1 {
		return contract.Operation{}, ambiguousToolIdentityError(group, action, verbAliases)
	}
	return contract.Operation{}, fmt.Errorf("unknown tool: %s.%s", strings.TrimSpace(group), strings.TrimSpace(action))
}

func toolIdentityMatchesAlias(operation contract.Operation, actionToken string) bool {
	idAction := toolIdentityAction(string(operation.ID))
	if normalizeToken(idAction) == actionToken {
		return true
	}
	legacyLongAction := toKebab(strings.TrimSpace(operation.Action))
	if normalizeToken(legacyLongAction) == actionToken {
		return true
	}
	if normalizeToken(operation.Action) == actionToken {
		return true
	}
	return false
}

func toolIdentityAction(identity string) string {
	parts := strings.SplitN(strings.TrimSpace(identity), ".", 2)
	return strings.TrimSpace(parts[len(parts)-1])
}

func ambiguousToolIdentityError(group, action string, matches []contract.Operation) error {
	ids := make([]string, 0, len(matches))
	seen := map[string]struct{}{}
	for _, operation := range matches {
		id := strings.TrimSpace(string(operation.ID))
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return fmt.Errorf(
		"ambiguous tool identity: %s.%s matched %s",
		strings.TrimSpace(group),
		strings.TrimSpace(action),
		strings.Join(ids, ", "),
	)
}

func loadToolOperations(group, verb, family string) []contract.Operation {
	catalog, err := loadOperationCatalog()
	if err != nil {
		return nil
	}
	g := normalizeToken(group)
	v := normalizeToken(verb)
	f := normalizeToken(family)
	out := make([]contract.Operation, 0, len(catalog.Operations))
	for _, operation := range catalog.Operations {
		if operation.Visibility != "public" {
			continue
		}
		if g != "" && normalizeToken(operation.Group) != g {
			continue
		}
		if v != "" && !toolVerbMatches(operation, v) {
			continue
		}
		if f != "" && normalizeToken(operation.Family) != f {
			continue
		}
		out = append(out, operation)
	}
	sort.Slice(out, func(i, j int) bool {
		if normalizeToken(out[i].Group) == normalizeToken(out[j].Group) {
			return normalizeToken(out[i].Action) < normalizeToken(out[j].Action)
		}
		return normalizeToken(out[i].Group) < normalizeToken(out[j].Group)
	})
	return out
}

func toolGroupExists(group string) bool {
	catalog, err := loadOperationCatalog()
	if err != nil {
		return false
	}
	want := normalizeToken(group)
	if want == "" {
		return false
	}
	for _, operation := range catalog.Operations {
		if operation.Visibility == "public" && normalizeToken(operation.Group) == want {
			return true
		}
	}
	return false
}

func toolVerbMatches(operation contract.Operation, want string) bool {
	target := normalizeToken(want)
	if target == "" {
		return true
	}
	for _, candidate := range toolVerbAliases(operation) {
		if normalizeToken(candidate) == target {
			return true
		}
	}
	return false
}

func toolVerbAliases(operation contract.Operation) []string {
	seen := map[string]struct{}{}
	aliases := make([]string, 0, 2)
	for _, candidate := range []string{strings.TrimSpace(operation.Verb), semanticToolVerb(operation)} {
		key := normalizeToken(candidate)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		aliases = append(aliases, candidate)
	}
	return aliases
}

func semanticToolVerb(operation contract.Operation) string {
	raw := strings.TrimSpace(operation.Verb)
	if !strings.EqualFold(raw, "describe") {
		return raw
	}
	switch strings.ToUpper(strings.TrimSpace(operation.Wire.Method)) {
	case "GET", "HEAD", "OPTIONS":
	default:
		return raw
	}
	action := strings.TrimSpace(operation.Action)
	noun := ""
	if strings.HasPrefix(action, "Describe") {
		noun = strings.TrimSpace(strings.TrimPrefix(action, "Describe"))
	}
	if noun == "" {
		return raw
	}
	if toolNounLooksPlural(noun) {
		return "list"
	}
	return "get"
}

func toolNounLooksPlural(noun string) bool {
	kebab := strings.TrimSpace(toKebab(noun))
	if kebab == "" {
		return false
	}
	kebab = trimToolVersionSuffix(kebab)
	for _, marker := range []string{"-by-", "-for-", "-with-", "-from-", "-under-"} {
		if idx := strings.Index(kebab, marker); idx > 0 {
			kebab = kebab[:idx]
			break
		}
	}
	kebab = strings.ReplaceAll(kebab, "-", "")
	if kebab == "" {
		return false
	}
	switch {
	case strings.HasSuffix(kebab, "ss"), strings.HasSuffix(kebab, "us"), strings.HasSuffix(kebab, "is"):
		return false
	default:
		return strings.HasSuffix(kebab, "s")
	}
}

func trimToolVersionSuffix(noun string) string {
	out := strings.TrimSpace(strings.ToLower(noun))
	for len(out) > 0 {
		last := out[len(out)-1]
		if last < '0' || last > '9' {
			break
		}
		out = out[:len(out)-1]
	}
	out = strings.TrimSuffix(out, "-v")
	return strings.TrimSpace(out)
}

func summarizeTools(tools []contract.Operation) string {
	if len(tools) == 0 {
		return "No tools matched.\n"
	}
	countByGroup := map[string]int{}
	for _, tool := range tools {
		g := normalizeToken(tool.Group)
		if g == "" {
			continue
		}
		countByGroup[g]++
	}
	groups := make([]string, 0, len(countByGroup))
	for g := range countByGroup {
		groups = append(groups, g)
	}
	sort.Strings(groups)
	lines := make([]string, 0, len(groups))
	for _, g := range groups {
		lines = append(lines, "  - "+g+" ("+strconv.Itoa(countByGroup[g])+" actions)")
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n") + "\n"
}

func summarizeToolsForGroup(tools []contract.Operation, group string) string {
	if strings.TrimSpace(group) == "" {
		return "No tools matched.\n"
	}
	matching := make([]string, 0, len(tools))
	seen := map[string]struct{}{}
	for _, tool := range tools {
		id := strings.TrimSpace(string(tool.ID))
		if normalizeToken(id) == "" {
			continue
		}
		if normalizeToken(tool.Group) != normalizeToken(group) {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		matching = append(matching, id)
	}
	if len(matching) == 0 {
		return "No tools matched.\n"
	}
	sort.Strings(matching)
	lines := make([]string, 0, len(matching))
	for _, action := range matching {
		lines = append(lines, "  - "+action)
	}
	if len(lines) == 0 {
		return "No tools matched.\n"
	}
	return strings.Join(lines, "\n") + "\n"
}

type toolDescribeCommon struct {
	inputSchema      map[string]any
	inputFlatSchema  map[string]any
	inputEncoding    map[string]any
	contextSchema    map[string]any
	executionSchema  map[string]any
	execution        map[string]any
	identity         map[string]any
	behavior         map[string]any
	output           map[string]any
	usageNotes       []any
	usageConstraints string
	contractDigest   map[string]any
	contractCache    map[string]any
}

func buildToolDescribeOutput(operation contract.Operation, view toolDescribeView) (map[string]any, error) {
	common, err := buildToolDescribeCommon(operation)
	if err != nil {
		return nil, err
	}
	switch view {
	case toolDescribeViewCompact:
		return buildToolDescribeCompactOutput(operation, common), nil
	default:
		return buildToolDescribeFullOutput(operation, common), nil
	}
}

func buildToolDescribeCommon(operation contract.Operation) (toolDescribeCommon, error) {
	catalog, err := loadOperationCatalog()
	if err != nil {
		return toolDescribeCommon{}, err
	}
	expandedContextSchema, err := contract.ExpandContextSchema(catalog.ContextSchema, catalog.ExecutionSchema)
	if err != nil {
		return toolDescribeCommon{}, err
	}
	supportsAll := operation.Pagination != nil
	supportsDryRun := operation.Runtime.SupportsDryRun
	inputSchema := cloneToolSchema(map[string]any(operation.InputSchema))
	executionSchema := enrichToolExecutionSchema(map[string]any(catalog.ExecutionSchema), supportsAll)
	contextSchema := enrichToolContextSchema(map[string]any(expandedContextSchema), executionSchema, supportsAll)
	execution := cloneToolSchema(executionSchema)
	execution["supports_all"] = supportsAll
	execution["supports_dry_run"] = supportsDryRun
	digestValue := strings.ToLower(toolContractForDigest(operation))
	verb := semanticToolVerb(operation)
	usageNotes := []any{}
	if supportsAll {
		usageNotes = append(usageNotes, "execution.page.all increases completeness and may increase payload size; pair it with execution.artifact or execution.projection for large results.")
	}
	usageNotes = append(usageNotes, toolSpecificUsageNotes(operation)...)
	return toolDescribeCommon{
		inputSchema:     inputSchema,
		inputFlatSchema: buildToolFlatInputSchema(inputSchema),
		inputEncoding:   buildToolInputEncodingHint(inputSchema),
		contextSchema:   contextSchema,
		executionSchema: executionSchema,
		execution:       execution,
		identity: map[string]any{
			"id":         strings.TrimSpace(string(operation.ID)),
			"group":      strings.TrimSpace(operation.Group),
			"action":     strings.TrimSpace(operation.Action),
			"resource":   strings.TrimSpace(operation.Resource),
			"verb":       verb,
			"family":     strings.TrimSpace(operation.Family),
			"method":     strings.ToUpper(strings.TrimSpace(operation.Wire.Method)),
			"path":       strings.TrimSpace(operation.Wire.Path),
			"visibility": strings.TrimSpace(operation.Visibility),
			"summary":    strings.TrimSpace(operation.Docs.Summary),
		},
		usageNotes:       usageNotes,
		usageConstraints: strings.TrimSpace(operation.Docs.UsageConstraints),
		behavior: map[string]any{
			"execution_embedded_at": "context.execution",
			"verb":                  verb,
			"supports_dry_run":      supportsDryRun,
			"supports_all":          supportsAll,
			"is_envelope_output":    operation.Output.IsEnvelopeOutput,
		},
		output: map[string]any{
			"policy":      strings.TrimSpace(operation.Output.Policy),
			"is_envelope": operation.Output.IsEnvelopeOutput,
		},
		contractDigest: map[string]any{
			"value":   digestValue,
			"policy":  "soft",
			"warning": "contract digest mismatch is advisory; continue execution if mismatch",
		},
		contractCache: map[string]any{
			"safe_scope": "Safe to reuse within the same CLI build while contract_digest.value stays unchanged.",
			"refresh_when": []any{
				"contract_digest changes",
				"CLI version or build changes",
				"execution returns unknown field, contract mismatch, or nearby usage errors for this action",
			},
		},
	}, nil
}

func toolSpecificUsageNotes(operation contract.Operation) []any {
	switch strings.TrimSpace(string(operation.ID)) {
	case "shard.merge":
		return []any{
			"The selected profile must have endpoint and region configured. The explicit --region and --endpoint overrides in the commands below keep the target environment unambiguous.",
			"Preflight: run volclog --profile <profile> --region <region> --endpoint <tls-endpoint> tool exec shard.describe --input '{\"TopicId\":\"<topic-id>\"}' --page-all; select a non-final readwrite shard whose next shard is contiguous and readwrite.",
			"Plan without mutation: run volclog --profile <profile> --region <region> --endpoint <tls-endpoint> --dry-run tool exec shard.merge --input '{\"TopicId\":\"<topic-id>\",\"ShardId\":0}' after replacing the placeholders and ShardId.",
			"Execute once after reviewing the plan: run volclog --profile <profile> --region <region> --endpoint <tls-endpoint> tool exec shard.merge --input '{\"TopicId\":\"<topic-id>\",\"ShardId\":0}'.",
			"Dry-run validates request planning and required-field presence only; TopicId UUID syntax, ShardId range, and shard mergeability remain service-side validation.",
			"On success, read status first and then the resulting shard list from data.Shards. If the result is ambiguous, preserve error.requestId and run shard.describe again; do not retry shard.merge automatically.",
		}
	case "log.search":
		return []any{
			"SearchLogs Query supports both plain search syntax and SQL/analysis syntax such as '* | select ...'.",
			"HitCount is only the count returned in the current response window of SearchLogs; do not treat it as the whole-window total hit count.",
			"Use log.describe-histogram-v1 only for pure search queries when you need time-distribution preview or a better whole-window hit estimate before narrowing or widening a search window; Histogram.TotalCount is the better whole-window hit count in that pure-search case.",
			"For SQL/analysis queries, body fields such as Context, Sort, Limit, and Offset do not page analysis rows; use SQL limit/offset inside Query instead.",
			"If SearchLogs returns ResultStatus=incomplete, the service returned only a partial scan; this can happen for both search and analysis queries, so narrow the time range and rerun before trusting counts, rows, or absence of hits.",
			"Prefer log.search for interactive analysis, quick previews, and smaller result sets; switch to workflow log.export-analysis when analysis rows may exceed stdout or token budget.",
		}
	case "log.describe-histogram-v1":
		return []any{
			"DescribeHistogramV1 is for time distribution preview before deciding whether to narrow, widen, or export a search window.",
			"Use DescribeHistogramV1 only for pure search queries; for search+analysis or pure analysis queries, do not treat histogram counts as analysis result counts.",
			"Histogram.TotalCount is the better whole-window hits count for pure search; use it when HitCount from SearchLogs only reflects the current response window.",
			"Start with log.describe-histogram-v1 to find hot buckets for pure search, then re-run log.search on a narrower time range for row preview or switch to workflow export when results stay large.",
			"If ResultStatus=incomplete, the service returned only a partial scan; narrow the time range and rerun before trusting bucket counts or total hits.",
			"Omit Interval unless you need a stable bucket width; when omitted, the server derives bucket size from the requested time range.",
		}
	default:
		return nil
	}
}

func buildToolDescribeFullOutput(operation contract.Operation, common toolDescribeCommon) map[string]any {
	target := map[string]any{
		"identity":            common.identity,
		"input":               common.inputSchema,
		"context":             common.contextSchema,
		"execution":           common.execution,
		"input_schema":        common.inputSchema,
		"context_schema":      common.contextSchema,
		"execution_schema":    common.executionSchema,
		"usage_notes":         common.usageNotes,
		"usage_constraints":   common.usageConstraints,
		"behavior":            common.behavior,
		"output":              common.output,
		"output_policy":       strings.TrimSpace(operation.Output.Policy),
		"risk":                strings.TrimSpace(operation.Risk.Level),
		"recovery":            strings.TrimSpace(operation.Risk.ErrorRecovery),
		"source":              strings.TrimSpace(operation.Docs.Source),
		"contract_digest":     common.contractDigest,
		"contract_cache_hint": common.contractCache,
	}
	if common.inputFlatSchema != nil {
		target["input_flat_schema"] = common.inputFlatSchema
	}
	if common.inputEncoding != nil {
		target["input_encoding_hint"] = common.inputEncoding
	}
	return target
}

func buildToolDescribeCompactOutput(operation contract.Operation, common toolDescribeCommon) map[string]any {
	compactInputSchema := compactToolInputSchema(common.inputSchema)
	target := map[string]any{
		"identity": map[string]any{
			"id":     common.identity["id"],
			"group":  common.identity["group"],
			"action": common.identity["action"],
			"method": common.identity["method"],
			"path":   common.identity["path"],
		},
		"context_schema":      compactToolContextSchema(common.contextSchema),
		"execution_schema":    compactToolExecutionSchema(common.executionSchema),
		"usage_notes":         common.usageNotes,
		"usage_constraints":   common.usageConstraints,
		"behavior":            common.behavior,
		"risk":                strings.TrimSpace(operation.Risk.Level),
		"recovery":            strings.TrimSpace(operation.Risk.ErrorRecovery),
		"output_policy":       strings.TrimSpace(operation.Output.Policy),
		"source":              strings.TrimSpace(operation.Docs.Source),
		"contract_digest":     common.contractDigest,
		"contract_cache_hint": common.contractCache,
	}
	if len(compactInputSchema) > 0 && !compactToolPrefersFlatInputSchema(common.inputSchema, common.inputFlatSchema) {
		target["input_schema"] = compactInputSchema
	}
	if common.inputFlatSchema != nil {
		target["input_flat_schema"] = common.inputFlatSchema
	}
	if common.inputEncoding != nil {
		target["input_encoding_hint"] = common.inputEncoding
	}
	return target
}

func compactToolInputSchema(inputSchema map[string]any) map[string]any {
	out := map[string]any{}
	for _, section := range []string{"body", "query", "path", "header"} {
		raw, ok := inputSchema[section].(map[string]any)
		if !ok {
			continue
		}
		filtered, keep := compactToolInputSection(raw)
		if !keep {
			continue
		}
		out[section] = filtered
	}
	return out
}

func compactToolPrefersFlatInputSchema(inputSchema, flatSchema map[string]any) bool {
	if len(flatSchema) == 0 {
		return false
	}
	sections := make([]string, 0, 4)
	for _, section := range []string{"body", "query", "path", "header"} {
		raw, ok := inputSchema[section].(map[string]any)
		if !ok || len(raw) == 0 {
			continue
		}
		props, _ := raw["properties"].(map[string]any)
		if len(props) == 0 {
			continue
		}
		sections = append(sections, section)
	}
	return len(sections) == 1 && sections[0] != "body"
}

func buildToolFlatInputSchema(inputSchema map[string]any) map[string]any {
	if !toolNeedsInputEncodingDocs(inputSchema) {
		return nil
	}
	props := map[string]any{}
	requiredSet := map[string]struct{}{}
	locations := map[string][]string{}
	for _, section := range []string{"body", "query", "path", "header"} {
		raw, ok := inputSchema[section].(map[string]any)
		if !ok || len(raw) == 0 {
			continue
		}
		required := toolRequiredFields(raw["required"])
		requiredLookup := map[string]struct{}{}
		for _, item := range required {
			requiredLookup[item] = struct{}{}
		}
		sectionProps, _ := raw["properties"].(map[string]any)
		for name, value := range sectionProps {
			field, _ := value.(map[string]any)
			locations[name] = appendToolSchemaLocation(locations[name], section)
			if _, exists := props[name]; !exists {
				props[name] = compactToolFieldSchemaAtDepth(field, 1)
			}
			if _, ok := requiredLookup[name]; ok {
				requiredSet[name] = struct{}{}
			}
		}
	}
	if len(props) == 0 {
		return nil
	}
	keys := make([]string, 0, len(props))
	for name := range props {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	flatProps := map[string]any{}
	for _, name := range keys {
		field, _ := props[name].(map[string]any)
		field = cloneToolSchema(field)
		sections := append([]string(nil), locations[name]...)
		sort.Strings(sections)
		if len(sections) == 1 {
			field["in"] = sections[0]
		} else if len(sections) > 1 {
			items := make([]any, 0, len(sections))
			for _, section := range sections {
				items = append(items, section)
			}
			field["sections"] = items
		}
		flatProps[name] = field
	}
	out := map[string]any{
		"type":       "object",
		"properties": flatProps,
	}
	if len(requiredSet) > 0 {
		required := make([]string, 0, len(requiredSet))
		for name := range requiredSet {
			required = append(required, name)
		}
		sort.Strings(required)
		items := make([]any, 0, len(required))
		for _, name := range required {
			items = append(items, name)
		}
		out["required"] = items
	}
	return out
}

func buildToolInputEncodingHint(inputSchema map[string]any) map[string]any {
	if !toolNeedsInputEncodingDocs(inputSchema) {
		return nil
	}
	sections := make([]string, 0, 4)
	overlaps := make([]string, 0, 2)
	fieldCounts := map[string]int{}
	for _, section := range []string{"body", "query", "path", "header"} {
		raw, ok := inputSchema[section].(map[string]any)
		if !ok || len(raw) == 0 {
			continue
		}
		sections = append(sections, section)
		props, _ := raw["properties"].(map[string]any)
		for name := range props {
			fieldCounts[name]++
		}
	}
	if len(sections) == 0 {
		return nil
	}
	for name, count := range fieldCounts {
		if count > 1 {
			overlaps = append(overlaps, name)
		}
	}
	sort.Strings(overlaps)
	hint := map[string]any{
		"transport": "--input accepts file://req.json, -, or inline JSON object.",
		"sectioned": "Nested form stays authoritative: {\"query\": {...}, \"path\": {...}, \"header\": {...}, \"body\": {...}}.",
	}
	switch {
	case len(sections) == 1 && sections[0] == "body":
		hint["recommended"] = "flat input is the natural form for this tool; top-level keys map directly to the request body."
	case len(sections) == 1:
		hint["recommended"] = "flat input is accepted for this tool; top-level keys map directly to " + sections[0] + " fields."
	default:
		hint["recommended"] = "flat input is accepted when each key maps unambiguously to one section; use nested sections when you want explicit query/path/header/body placement."
	}
	if len(overlaps) > 0 {
		items := make([]any, 0, len(overlaps))
		for _, name := range overlaps {
			items = append(items, name)
		}
		hint["overlapping_fields"] = items
	}
	return hint
}

func toolNeedsInputEncodingDocs(inputSchema map[string]any) bool {
	sections := 0
	nonBodySections := 0
	for _, section := range []string{"body", "query", "path", "header"} {
		raw, ok := inputSchema[section].(map[string]any)
		if !ok || len(raw) == 0 {
			continue
		}
		sections++
		if section != "body" {
			nonBodySections++
		}
	}
	return sections > 1 || nonBodySections > 0
}

func appendToolSchemaLocation(existing []string, section string) []string {
	for _, item := range existing {
		if item == section {
			return existing
		}
	}
	return append(existing, section)
}

func compactToolInputSection(schema map[string]any) (map[string]any, bool) {
	props, _ := schema["properties"].(map[string]any)
	if len(props) == 0 {
		return nil, false
	}
	out := map[string]any{}
	if fieldType := strings.TrimSpace(strings.ToLower(toolAnyToString(schema["type"]))); fieldType != "" {
		out["type"] = fieldType
	}
	required := toolRequiredFields(schema["required"])
	if len(required) > 0 {
		out["required"] = append([]string(nil), required...)
	}
	keys := make([]string, 0, len(props))
	for name := range props {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	keptProps := map[string]any{}
	for _, name := range keys {
		field, _ := props[name].(map[string]any)
		keptProps[name] = compactToolFieldSchemaAtDepth(field, 0)
	}
	if len(keptProps) > 0 {
		out["properties"] = keptProps
	}
	return out, true
}

func filterToolSchemaToRequired(schema map[string]any, depth int) (map[string]any, bool) {
	props, _ := schema["properties"].(map[string]any)
	required := toolRequiredFields(schema["required"])
	if len(required) == 0 {
		return nil, false
	}
	out := map[string]any{}
	if fieldType := strings.TrimSpace(strings.ToLower(toolAnyToString(schema["type"]))); fieldType != "" {
		out["type"] = fieldType
	}
	out["required"] = append([]string(nil), required...)
	keptProps := map[string]any{}
	for _, name := range required {
		field, _ := props[name].(map[string]any)
		keptProps[name] = compactToolFieldSchemaAtDepth(field, depth+1)
	}
	if len(keptProps) > 0 {
		out["properties"] = keptProps
	}
	return out, true
}

func compactToolFieldSchemaAtDepth(schema map[string]any, depth int) map[string]any {
	if len(schema) == 0 {
		return map[string]any{}
	}
	out := map[string]any{}
	if fieldType := strings.TrimSpace(strings.ToLower(toolAnyToString(schema["type"]))); fieldType != "" {
		out["type"] = fieldType
	}
	for _, key := range []string{"format", "default", "enum", "pattern", "minimum", "maximum", "minLength", "maxLength"} {
		if value, ok := schema[key]; ok {
			out[key] = deepCloneToolValue(value)
		}
	}
	if shouldKeepCompactToolFieldDescription(schema, depth) {
		if description := strings.TrimSpace(toolAnyToString(schema["description"])); description != "" {
			out["description"] = description
		}
	}
	if oneOf, ok := schema["oneOf"].([]any); ok && len(oneOf) > 0 {
		out["oneOf"] = deepCloneToolValue(oneOf)
	}
	if items, ok := schema["items"].(map[string]any); ok && len(items) > 0 {
		out["items"] = compactToolFieldSchemaAtDepth(items, depth+1)
	}
	if props, ok := schema["properties"].(map[string]any); ok && len(props) > 0 {
		if shouldKeepCompactToolAllChildren(schema, depth) {
			if required := toolRequiredFields(schema["required"]); len(required) > 0 {
				out["required"] = append([]string(nil), required...)
			}
			out["properties"] = compactToolFieldProperties(props, depth+1)
		} else if child, keep := filterToolSchemaToRequired(schema, depth); keep {
			if childType, ok := child["type"]; ok {
				out["type"] = childType
			}
			if childRequired, ok := child["required"]; ok {
				out["required"] = childRequired
			}
			if childProps, ok := child["properties"]; ok {
				out["properties"] = childProps
			}
		} else if shouldKeepCompactToolOptionalChildren(schema, depth) {
			out["properties"] = compactToolFieldProperties(props, depth+1)
		}
	}
	if len(out) == 0 {
		return map[string]any{}
	}
	return out
}

func shouldKeepCompactToolFieldDescription(schema map[string]any, depth int) bool {
	description := strings.TrimSpace(toolAnyToString(schema["description"]))
	if description == "" {
		return false
	}
	if depth > 0 {
		return true
	}
	if hasToolOneOf(schema) {
		return true
	}
	if items, ok := schema["items"].(map[string]any); ok && len(items) > 0 {
		return true
	}
	if props, ok := schema["properties"].(map[string]any); ok && len(props) > 0 {
		return true
	}
	return false
}

func shouldKeepCompactToolAllChildren(schema map[string]any, depth int) bool {
	props, _ := schema["properties"].(map[string]any)
	if len(props) == 0 {
		return false
	}
	// Preserve small nested objects in compact mode even when they mix required
	// and optional fields; otherwise agent-relevant knobs disappear from the
	// default view.
	return depth >= 0 && len(props) <= 10
}

func shouldKeepCompactToolOptionalChildren(schema map[string]any, depth int) bool {
	props, _ := schema["properties"].(map[string]any)
	if len(props) == 0 {
		return false
	}
	if len(toolRequiredFields(schema["required"])) > 0 {
		return false
	}
	// Keep small nested objects usable in compact mode without fully expanding
	// large optional structures.
	return depth >= 0 && len(props) <= 5
}

func compactToolFieldProperties(props map[string]any, depth int) map[string]any {
	keys := make([]string, 0, len(props))
	for name := range props {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	compactProps := make(map[string]any, len(keys))
	for _, name := range keys {
		field, _ := props[name].(map[string]any)
		compactProps[name] = compactToolFieldSchemaAtDepth(field, depth)
	}
	return compactProps
}

func compactToolContextSchema(schema map[string]any) map[string]any {
	out := map[string]any{}
	if fieldType := strings.TrimSpace(strings.ToLower(toolAnyToString(schema["type"]))); fieldType != "" {
		out["type"] = fieldType
	}
	props, _ := schema["properties"].(map[string]any)
	if len(props) == 0 {
		return out
	}
	compactProps := make(map[string]any, len(props))
	for name, raw := range props {
		field, _ := raw.(map[string]any)
		if normalizeToken(name) == "execution" {
			compactProps[name] = compactToolExecutionContextFieldSchema(field)
			continue
		}
		compactProps[name] = compactToolDocFieldSchema(field)
	}
	out["properties"] = compactProps
	return out
}

func compactToolExecutionContextFieldSchema(schema map[string]any) map[string]any {
	out := compactToolDocFieldSchema(schema)
	out["schema_ref"] = "execution_schema"
	delete(out, "properties")
	return out
}

func compactToolExecutionSchema(schema map[string]any) map[string]any {
	out := map[string]any{}
	if fieldType := strings.TrimSpace(strings.ToLower(toolAnyToString(schema["type"]))); fieldType != "" {
		out["type"] = fieldType
	}
	props, _ := schema["properties"].(map[string]any)
	if len(props) == 0 {
		return out
	}
	compactProps := make(map[string]any, len(props))
	for name, raw := range props {
		field, _ := raw.(map[string]any)
		compactProps[name] = compactToolExecutionFieldSchema(field)
	}
	out["properties"] = compactProps
	return out
}

func compactToolExecutionFieldSchema(schema map[string]any) map[string]any {
	out := compactToolDocFieldSchema(schema)
	props, _ := schema["properties"].(map[string]any)
	if len(props) == 0 || hasToolOneOf(schema) {
		return out
	}
	compactProps := make(map[string]any, len(props))
	for name, raw := range props {
		field, _ := raw.(map[string]any)
		compactProps[name] = compactToolDocFieldSchema(field)
	}
	out["properties"] = compactProps
	return out
}

func compactToolDocFieldSchema(schema map[string]any) map[string]any {
	if len(schema) == 0 {
		return map[string]any{}
	}
	out := map[string]any{}
	if fieldType := strings.TrimSpace(strings.ToLower(toolAnyToString(schema["type"]))); fieldType != "" {
		out["type"] = fieldType
	}
	for _, key := range []string{"description", "when_to_use", "default", "runtime_effect", "discover_values"} {
		if value, ok := schema[key]; ok {
			out[key] = deepCloneToolValue(value)
		}
	}
	if accepts := compactToolAcceptedForms(schema); len(accepts) > 0 {
		out["accepts"] = accepts
	}
	return out
}

func compactToolAcceptedForms(schema map[string]any) []string {
	oneOf, _ := schema["oneOf"].([]any)
	if len(oneOf) == 0 {
		return nil
	}
	forms := make([]string, 0, len(oneOf))
	seen := map[string]struct{}{}
	for _, raw := range oneOf {
		candidate, _ := raw.(map[string]any)
		form := strings.TrimSpace(compactToolForm(candidate))
		if form == "" {
			continue
		}
		if _, ok := seen[form]; ok {
			continue
		}
		seen[form] = struct{}{}
		forms = append(forms, form)
	}
	if len(forms) == 0 {
		return nil
	}
	return forms
}

func compactToolForm(schema map[string]any) string {
	switch strings.TrimSpace(strings.ToLower(toolAnyToString(schema["type"]))) {
	case "boolean", "string", "number", "integer":
		return strings.TrimSpace(strings.ToLower(toolAnyToString(schema["type"])))
	case "array":
		items, _ := schema["items"].(map[string]any)
		itemType := strings.TrimSpace(strings.ToLower(toolAnyToString(items["type"])))
		if itemType == "" {
			return "array"
		}
		return "array[" + itemType + "]"
	case "object":
		props, _ := schema["properties"].(map[string]any)
		if len(props) == 0 {
			return "object"
		}
		names := make([]string, 0, len(props))
		for name := range props {
			names = append(names, strings.TrimSpace(name))
		}
		sort.Strings(names)
		parts := make([]string, 0, len(names))
		for _, name := range names {
			field, _ := props[name].(map[string]any)
			fieldType := strings.TrimSpace(strings.ToLower(toolAnyToString(field["type"])))
			if fieldType == "" {
				fieldType = "any"
			}
			parts = append(parts, name+":"+fieldType)
		}
		return "object{" + strings.Join(parts, ", ") + "}"
	default:
		return ""
	}
}

func hasToolOneOf(schema map[string]any) bool {
	oneOf, _ := schema["oneOf"].([]any)
	return len(oneOf) > 0
}

func cloneToolSchema(src map[string]any) map[string]any {
	if src == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(src))
	for key, value := range src {
		out[key] = deepCloneToolValue(value)
	}
	return out
}

func deepCloneToolValue(src any) any {
	switch v := src.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, value := range v {
			out[key] = deepCloneToolValue(value)
		}
		return out
	case []any:
		out := make([]any, 0, len(v))
		for _, value := range v {
			out = append(out, deepCloneToolValue(value))
		}
		return out
	default:
		return v
	}
}

func enrichToolContextSchema(base map[string]any, executionSchema map[string]any, supportsAll bool) map[string]any {
	ctx := cloneToolSchema(base)
	if strings.TrimSpace(strings.ToLower(toolAnyToString(ctx["type"]))) == "" {
		ctx["type"] = "object"
	}
	props := ensureToolSchemaProperties(ctx)
	execProps, _ := executionSchema["properties"].(map[string]any)
	executionDescription := "Execution controls for dry-run, projection, artifact export, and pagination."
	if !supportsAll {
		executionDescription = "Execution controls for dry-run, projection, and artifact export."
	}
	contextDocs := map[string]map[string]any{
		"profile": {
			"type":            "string",
			"description":     "Saved local profile name used to resolve credentials and defaults for this tool execution.",
			"when_to_use":     "Set this when you need a non-default account/profile or want to switch tenant/environment without changing input.",
			"default":         "active CLI profile",
			"discover_values": "Run `volclog configure list` to inspect saved profile names; omit profile to use the active CLI profile.",
			"runtime_effect":  "Runtime loads credentials and defaults from the selected profile. If global --profile is also set, conflicting selectors fail fast instead of silently overriding each other.",
		},
		"secrets_file": {
			"type":           "string",
			"description":    "Path to a secrets file that provides supported VOLCENGINE_* credentials and defaults.",
			"when_to_use":    "Set this when credentials/defaults are stored in a file instead of the active profile or environment.",
			"default":        "Do not load an extra secrets file.",
			"runtime_effect": "Runtime resolves profile/secrets selectors first; if this secrets_file wins, it loads supported VOLCENGINE_* values from the file before the request is built.",
		},
		"region": {
			"type":           "string",
			"description":    "Target region for the TLS OpenAPI endpoint.",
			"when_to_use":    "Set this to choose a region explicitly or override defaults.",
			"default":        "resolved from profile or environment",
			"runtime_effect": "Runtime uses this value as the request signing region. Endpoint remains independently explicit.",
		},
		"endpoint": {
			"type":           "string",
			"description":    "Explicit TLS OpenAPI endpoint URL.",
			"when_to_use":    "Set this to choose the TLS service address explicitly.",
			"default":        "resolved from explicit runtime configuration",
			"runtime_effect": "Runtime sends requests to this endpoint; it is never derived from region.",
		},
		"trace": {
			"description":    "Trace configuration for request/response diagnostics.",
			"when_to_use":    "Set this when you need troubleshooting artifacts or transport traces.",
			"default":        false,
			"runtime_effect": "Runtime enables trace capture, chooses the trace directory, and normalizes legacy strict/default redact inputs to the current on/off setting.",
		},
		"contract_digest": {
			"type":           "string",
			"description":    "Expected contract digest for this tool execution.",
			"when_to_use":    "Set this when you want to pin describe/exec to a known contract snapshot.",
			"default":        "empty",
			"runtime_effect": "Runtime compares digest in soft mode and emits advisory mismatch warnings only.",
		},
		"execution": {
			"type":           "object",
			"properties":     execProps,
			"description":    executionDescription,
			"when_to_use":    "Set this to change execution behavior without altering business request payload.",
			"default":        map[string]any{},
			"runtime_effect": "Runtime reads execution controls from context.execution; do not pass execution as a separate file.",
		},
	}
	for key, doc := range contextDocs {
		props[key] = mergeToolFieldDoc(props[key], doc)
	}
	if traceField, ok := props["trace"].(map[string]any); ok {
		if _, exists := traceField["oneOf"]; !exists {
			traceField["oneOf"] = []any{
				map[string]any{"type": "boolean"},
				map[string]any{"type": "string"},
				map[string]any{
					"type": "object",
					"properties": map[string]any{
						"enabled": map[string]any{"type": "boolean"},
						"dir":     map[string]any{"type": "string"},
						"redact":  map[string]any{"type": "string"},
					},
				},
			}
		}
	}
	return ctx
}

func enrichToolExecutionSchema(base map[string]any, supportsAll bool) map[string]any {
	exec := cloneToolSchema(base)
	if strings.TrimSpace(strings.ToLower(toolAnyToString(exec["type"]))) == "" {
		exec["type"] = "object"
	}
	props := ensureToolSchemaProperties(exec)
	executionDocs := map[string]string{
		"dry_run":    "When true, runtime validates and builds request envelope but does not call remote API.",
		"projection": "Projection filter for response payload; supports string, string array, or {\"jmes\": \"...\"}.",
		"artifact":   "Artifact export control; supports boolean, output path string, or object with path.",
	}
	if supportsAll {
		executionDocs["page"] = "Pagination execution controls. page.all requests auto-pagination for supported actions."
		executionDocs["page_all"] = "Compatibility alias for page.all. Prefer page.all in new context files, but runtime still accepts page_all."
	} else {
		delete(props, "page")
		delete(props, "page_all")
	}
	for key, desc := range executionDocs {
		field, _ := props[key].(map[string]any)
		field = cloneToolSchema(field)
		if key == "page" && strings.TrimSpace(strings.ToLower(toolAnyToString(field["type"]))) == "" {
			field["type"] = "object"
		}
		field["description"] = desc
		props[key] = field
	}
	return exec
}

func ensureToolSchemaProperties(schema map[string]any) map[string]any {
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		props = map[string]any{}
		schema["properties"] = props
	}
	return props
}

func mergeToolFieldDoc(existing any, doc map[string]any) map[string]any {
	field, _ := existing.(map[string]any)
	field = cloneToolSchema(field)
	for key, value := range doc {
		if key == "type" || key == "properties" || key == "oneOf" {
			if _, exists := field[key]; exists {
				continue
			}
		}
		field[key] = deepCloneToolValue(value)
	}
	return field
}

func toolRequiredFields(raw any) []string {
	switch v := raw.(type) {
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if strings.TrimSpace(item) == "" {
				continue
			}
			out = append(out, strings.TrimSpace(item))
		}
		return out
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s := strings.TrimSpace(toolAnyToString(item))
			if s == "" {
				continue
			}
			out = append(out, s)
		}
		return out
	default:
		return nil
	}
}

func toolAnyToString(v any) string {
	s, _ := v.(string)
	return s
}

func toolContractForDigest(operation contract.Operation) string {
	catalog, err := loadOperationCatalog()
	if err != nil {
		return ""
	}
	legacy, err := contract.RebuildLegacyToolV1(catalog, operation)
	if err != nil {
		return ""
	}
	return contract.LegacyToolDigestV1(legacy)
}
