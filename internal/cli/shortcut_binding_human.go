//go:build human

package cli

import (
	"fmt"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
)

type shortcutKind string

const (
	shortcutKindOperation shortcutKind = "operation"
	shortcutKindWorkflow  shortcutKind = "workflow"
	shortcutKindSpecial   shortcutKind = "special"
)

type shortcutBindingRole string

const (
	shortcutBindingSchema        shortcutBindingRole = "schema"
	shortcutBindingRequest       shortcutBindingRole = "request"
	shortcutBindingMeta          shortcutBindingRole = "meta"
	shortcutBindingTransport     shortcutBindingRole = "transport"
	shortcutBindingPassthrough   shortcutBindingRole = "passthrough"
	shortcutBindingWorkflowGroup shortcutBindingRole = "workflow-group"
)

// shortcutBinding is the human-only mapping between a shortcut flag and the
// canonical operation/workflow input. It intentionally stores UX metadata, not
// a second copy of the operation schema.
type shortcutBinding struct {
	Name         string
	Aliases      []string
	Location     string
	SchemaPath   string
	Role         shortcutBindingRole
	Required     bool
	RequiredText string
	Type         string
	Format       string
	Description  string
	Example      string
	Enum         []string
	Pattern      string
	Minimum      *float64
	Maximum      *float64
	MinLength    *int
	MaxLength    *int
	TriState     bool
	DefaultID    string
	ValidatorIDs []string
	TransformID  string

	HiddenInPresentation bool
	PresentationLocation string
}

type shortcutDefault struct {
	ID      string
	Binding string
	Source  string
	Value   any
}

type shortcutValidator struct {
	ID       string
	Bindings []string
}

type shortcutResultTransform struct {
	ID string
}

const (
	shortcutDefaultProfileRegion       = "profile-region"
	shortcutDefaultCreateTTL30         = "create-ttl-30"
	shortcutDefaultCreateShardCount2   = "create-shard-count-2"
	shortcutDefaultSearchLimit100      = "search-limit-100"
	shortcutDefaultExportLimit500      = "export-limit-500"
	shortcutDefaultExportMaxPages100   = "export-max-pages-100"
	shortcutValidatorTopicNameID       = "topic-name-id-exclusive"
	shortcutValidatorTimeKeyFormatPair = "time-key-format-pair"
	shortcutValidatorAutoSplitMax      = "auto-split-requires-max"
	shortcutValidatorHotTTLSum         = "hot-ttl-sum"
	shortcutValidatorClearDescription  = "clear-description-exclusive"
	shortcutValidatorIndexBody         = "index-body"
	shortcutValidatorAnalysisFlags     = "analysis-query-flags"
	shortcutValidatorPureSearch        = "pure-search-query"
	shortcutValidatorAnalysisQuery     = "analysis-query-required"
	shortcutTransformPageNumberList    = "page-number-list-merge"
)

var shortcutDefaultRegistry = map[string]struct{}{
	shortcutDefaultProfileRegion:     {},
	shortcutDefaultCreateTTL30:       {},
	shortcutDefaultCreateShardCount2: {},
	shortcutDefaultSearchLimit100:    {},
	shortcutDefaultExportLimit500:    {},
	shortcutDefaultExportMaxPages100: {},
}

var shortcutValidatorRegistry = map[string]struct{}{
	shortcutValidatorTopicNameID:       {},
	shortcutValidatorTimeKeyFormatPair: {},
	shortcutValidatorAutoSplitMax:      {},
	shortcutValidatorHotTTLSum:         {},
	shortcutValidatorClearDescription:  {},
	shortcutValidatorIndexBody:         {},
	shortcutValidatorAnalysisFlags:     {},
	shortcutValidatorPureSearch:        {},
	shortcutValidatorAnalysisQuery:     {},
}

var shortcutResultTransformRegistry = map[string]struct{}{
	shortcutTransformPageNumberList: {},
}

type shortcutPresentation struct {
	SupportsTemplate bool
	TemplateOmit     []string
}

type shortcutTarget struct {
	Method       string
	Path         string
	APIGroup     string
	APIAction    string
	Operation    contract.Operation
	Workflow     workflowCatalog
	IsOperation  bool
	IsWorkflow   bool
	HasOperation bool
}

func shortcutBindingParam(name, cliFlag, in string, required bool, typ, description string) shortcutBinding {
	location := strings.ToLower(strings.TrimSpace(in))
	role := shortcutBindingSchema
	switch {
	case location == "meta":
		role = shortcutBindingMeta
	case location == "group":
		role = shortcutBindingWorkflowGroup
	case strings.EqualFold(strings.TrimSpace(name), "request"):
		role = shortcutBindingRequest
	case isShortcutTransportBinding(name):
		role = shortcutBindingTransport
	}
	aliases := shortcutFlagAliases(cliFlag)
	return shortcutBinding{
		Name:        strings.TrimSpace(name),
		Aliases:     aliases,
		Location:    location,
		SchemaPath:  shortcutBindingSchemaPath(role, location, name),
		Role:        role,
		Required:    required,
		Type:        strings.TrimSpace(typ),
		Description: strings.TrimSpace(description),
		TriState:    shortcutAliasesAreTriState(aliases),
	}
}

func shortcutPassthroughBindingParam(name, cliFlag, in string, required bool, typ, description string) shortcutBinding {
	binding := shortcutBindingParam(name, cliFlag, in, required, typ, description)
	binding.Role = shortcutBindingPassthrough
	binding.SchemaPath = strings.ToLower(strings.TrimSpace(in)) + "." + strings.TrimSpace(name)
	return binding
}

func shortcutHiddenBindingParam(name, cliFlag, in string, required bool, typ, description string) shortcutBinding {
	binding := shortcutBindingParam(name, cliFlag, in, required, typ, description)
	binding.HiddenInPresentation = true
	return binding
}

func shortcutBindingParamWithPresentationLocation(name, cliFlag, in, presentationLocation string, required bool, typ, description string) shortcutBinding {
	binding := shortcutBindingParam(name, cliFlag, in, required, typ, description)
	binding.PresentationLocation = strings.ToLower(strings.TrimSpace(presentationLocation))
	return binding
}

func isShortcutTransportBinding(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "content-md5", "x-tls-compresstype":
		return true
	default:
		return false
	}
}

func shortcutBindingSchemaPath(role shortcutBindingRole, location, name string) string {
	if role != shortcutBindingSchema {
		return ""
	}
	location = strings.ToLower(strings.TrimSpace(location))
	name = strings.TrimSpace(name)
	if location == "" || name == "" {
		return ""
	}
	return location + "." + name
}

func shortcutFlagAliases(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '/' || r == ','
	})
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		alias := strings.TrimSpace(part)
		if alias == "" {
			continue
		}
		key := strings.ToLower(alias)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, alias)
	}
	return out
}

func shortcutAliasesAreTriState(aliases []string) bool {
	hasPositive := false
	hasNegative := false
	for _, alias := range aliases {
		name := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(alias)), "--")
		if strings.HasPrefix(name, "no-") || strings.HasPrefix(name, "disable-") || strings.HasPrefix(name, "clear-") {
			hasNegative = true
		} else {
			hasPositive = true
		}
	}
	return hasPositive && hasNegative
}

func shortcutParams(bindings []shortcutBinding) []apiCapParam {
	if len(bindings) == 0 {
		return nil
	}
	out := make([]apiCapParam, 0, len(bindings))
	for _, binding := range bindings {
		if binding.HiddenInPresentation {
			continue
		}
		location := binding.Location
		if strings.TrimSpace(binding.PresentationLocation) != "" {
			location = binding.PresentationLocation
		}
		out = append(out, apiCapParam{
			Name:         binding.Name,
			CLIFlag:      strings.Join(binding.Aliases, "/"),
			In:           location,
			Required:     binding.Required,
			RequiredText: binding.RequiredText,
			Type:         binding.Type,
			Format:       binding.Format,
			Description:  binding.Description,
			Example:      binding.Example,
			Enum:         append([]string(nil), binding.Enum...),
			Pattern:      binding.Pattern,
			Minimum:      binding.Minimum,
			Maximum:      binding.Maximum,
			MinLength:    binding.MinLength,
			MaxLength:    binding.MaxLength,
		})
	}
	return out
}

func resolveShortcutTarget(spec shortcutCommandSpec) (shortcutTarget, error) {
	switch spec.Kind {
	case shortcutKindOperation:
		id := strings.TrimSpace(spec.OperationID)
		operation, ok := loadToolOperation(id)
		if !ok {
			return shortcutTarget{}, fmt.Errorf("shortcut %s.%s references unknown operation %q", spec.Group, spec.Command, id)
		}
		return shortcutTarget{
			Method:       operation.Wire.Method,
			Path:         operation.Wire.Path,
			APIGroup:     operation.Group,
			APIAction:    operation.Action,
			Operation:    operation,
			IsOperation:  true,
			HasOperation: true,
		}, nil
	case shortcutKindWorkflow:
		id := strings.TrimSpace(spec.WorkflowID)
		for _, workflow := range workflowCatalogSource() {
			if strings.TrimSpace(workflow.ID) != id {
				continue
			}
			operation, err := resolveWorkflowOperation(workflow)
			if err != nil {
				return shortcutTarget{}, fmt.Errorf("shortcut %s.%s: %w", spec.Group, spec.Command, err)
			}
			return shortcutTarget{
				Method:       operation.Wire.Method,
				Path:         operation.Wire.Path,
				APIGroup:     operation.Group,
				APIAction:    operation.Action,
				Operation:    operation,
				Workflow:     workflow,
				IsWorkflow:   true,
				HasOperation: true,
			}, nil
		}
		return shortcutTarget{}, fmt.Errorf("shortcut %s.%s references unknown workflow %q", spec.Group, spec.Command, id)
	case shortcutKindSpecial:
		return shortcutTarget{}, fmt.Errorf("shortcut %s.%s has no special target resolver", spec.Group, spec.Command)
	default:
		return shortcutTarget{}, fmt.Errorf("shortcut %s.%s has unsupported kind %q", spec.Group, spec.Command, spec.Kind)
	}
}

func validateShortcutSpecs(specs map[string]shortcutCommandSpec) error {
	for key, spec := range specs {
		wantKey := normalizeToken(spec.Group) + "\x00" + normalizeToken(spec.Command)
		if key != wantKey {
			return fmt.Errorf("shortcut %q key does not match identity %q", key, wantKey)
		}
		target, err := resolveShortcutTarget(spec)
		if err != nil {
			return err
		}
		if err := validateShortcutBindings(spec, target); err != nil {
			return err
		}
		if spec.Presentation.SupportsTemplate && !target.HasOperation {
			return fmt.Errorf("shortcut %s.%s template support requires a backing operation", spec.Group, spec.Command)
		}
	}
	return nil
}

func validateShortcutBindings(spec shortcutCommandSpec, target shortcutTarget) error {
	seenAliases := map[string]string{}
	seenBindings := map[string]struct{}{}
	for _, binding := range spec.Bindings {
		name := strings.TrimSpace(binding.Name)
		if name == "" {
			return fmt.Errorf("shortcut %s.%s has a binding with empty name", spec.Group, spec.Command)
		}
		seenBindings[name] = struct{}{}
		for _, alias := range binding.Aliases {
			key := strings.ToLower(strings.TrimSpace(alias))
			if key == "" {
				return fmt.Errorf("shortcut %s.%s binding %s has an empty alias", spec.Group, spec.Command, name)
			}
			if previous, ok := seenAliases[key]; ok {
				return fmt.Errorf("shortcut %s.%s alias %s is shared by %s and %s", spec.Group, spec.Command, alias, previous, name)
			}
			seenAliases[key] = name
		}
		if binding.Role == shortcutBindingWorkflowGroup {
			if spec.Kind != shortcutKindWorkflow {
				return fmt.Errorf(
					"shortcut %s.%s binding %s uses workflow-group outside a workflow",
					spec.Group,
					spec.Command,
					name,
				)
			}
			continue
		}
		if !target.HasOperation || binding.Role != shortcutBindingSchema {
			if binding.Role == shortcutBindingPassthrough && !allowedShortcutPassthrough(spec, binding) {
				return fmt.Errorf(
					"shortcut %s.%s binding %s uses unapproved passthrough path %q",
					spec.Group,
					spec.Command,
					name,
					binding.SchemaPath,
				)
			}
			continue
		}
		schema, ok := shortcutBindingTargetSchema(target.Operation, binding)
		if !ok {
			return fmt.Errorf(
				"shortcut %s.%s binding %s points to missing schema path %q",
				spec.Group,
				spec.Command,
				name,
				binding.SchemaPath,
			)
		}
		if !shortcutBindingTypeCompatible(binding.Type, schema) {
			return fmt.Errorf(
				"shortcut %s.%s binding %s type %q is incompatible with schema path %q",
				spec.Group,
				spec.Command,
				name,
				binding.Type,
				binding.SchemaPath,
			)
		}
	}
	return validateShortcutBehaviorBindings(spec, seenBindings)
}

func allowedShortcutPassthrough(spec shortcutCommandSpec, binding shortcutBinding) bool {
	switch spec.OperationID {
	case "topic.create":
		return binding.SchemaPath == "body.TimeKey" || binding.SchemaPath == "body.TimeFormat"
	case "host-group.modify-host-group":
		return binding.SchemaPath == "body.IamProjectName"
	case "collector.modify-rule":
		return binding.SchemaPath == "body.TopicId"
	default:
		return false
	}
}

func shortcutBindingTargetSchema(operation contract.Operation, binding shortcutBinding) (map[string]any, bool) {
	path := strings.Split(strings.TrimSpace(binding.SchemaPath), ".")
	if len(path) != 2 {
		return nil, false
	}
	section, ok := operation.InputSchema[path[0]].(map[string]any)
	if !ok {
		return nil, false
	}
	properties, ok := section["properties"].(map[string]any)
	if !ok {
		return nil, false
	}
	schema, ok := properties[path[1]].(map[string]any)
	return schema, ok
}

func shortcutBindingTypeCompatible(bindingType string, schema map[string]any) bool {
	bindingType = strings.ToLower(strings.TrimSpace(bindingType))
	if bindingType == "" || bindingType == "json" || bindingType == "json/jsonl" || bindingType == "path|-" {
		return true
	}
	schemaType, _ := schema["type"].(string)
	if strings.EqualFold(bindingType, schemaType) {
		return true
	}
	if bindingType == "number" && (schemaType == "integer" || schemaType == "number") {
		return true
	}
	if oneOf, ok := schema["oneOf"].([]any); ok {
		for _, candidate := range oneOf {
			candidateSchema, ok := candidate.(map[string]any)
			if ok && shortcutBindingTypeCompatible(bindingType, candidateSchema) {
				return true
			}
		}
	}
	return false
}

func validateShortcutBehaviorBindings(spec shortcutCommandSpec, bindings map[string]struct{}) error {
	for _, item := range spec.Defaults {
		if strings.TrimSpace(item.ID) == "" {
			return fmt.Errorf("shortcut %s.%s has a default with empty id", spec.Group, spec.Command)
		}
		if _, ok := shortcutDefaultRegistry[item.ID]; !ok {
			return fmt.Errorf("shortcut %s.%s has unknown default %q", spec.Group, spec.Command, item.ID)
		}
		if _, ok := bindings[item.Binding]; !ok {
			return fmt.Errorf("shortcut %s.%s default %s references unknown binding %q", spec.Group, spec.Command, item.ID, item.Binding)
		}
		if strings.TrimSpace(item.Source) == "" {
			return fmt.Errorf("shortcut %s.%s default %s has empty source", spec.Group, spec.Command, item.ID)
		}
	}
	for _, item := range spec.Validators {
		if strings.TrimSpace(item.ID) == "" {
			return fmt.Errorf("shortcut %s.%s has a validator with empty id", spec.Group, spec.Command)
		}
		if _, ok := shortcutValidatorRegistry[item.ID]; !ok {
			return fmt.Errorf("shortcut %s.%s has unknown validator %q", spec.Group, spec.Command, item.ID)
		}
		for _, binding := range item.Bindings {
			if _, ok := bindings[binding]; !ok {
				return fmt.Errorf("shortcut %s.%s validator %s references unknown binding %q", spec.Group, spec.Command, item.ID, binding)
			}
		}
	}
	for _, item := range spec.ResultTransforms {
		if strings.TrimSpace(item.ID) == "" {
			return fmt.Errorf("shortcut %s.%s has a result transform with empty id", spec.Group, spec.Command)
		}
		if _, ok := shortcutResultTransformRegistry[item.ID]; !ok {
			return fmt.Errorf("shortcut %s.%s has unknown result transform %q", spec.Group, spec.Command, item.ID)
		}
	}
	return nil
}

func shortcutRequestBodyMetaFromTarget(target shortcutTarget) *apiDescribeRequestBody {
	if !target.HasOperation {
		return nil
	}
	body, ok := target.Operation.InputSchema["body"].(map[string]any)
	if !ok {
		return nil
	}
	return &apiDescribeRequestBody{Required: len(shortcutSchemaRequiredSet(body)) > 0}
}

func shortcutOperationBodyFields(operation contract.Operation) []describeFieldParam {
	body, ok := operation.InputSchema["body"].(map[string]any)
	if !ok {
		return nil
	}
	properties, _ := body["properties"].(map[string]any)
	required := shortcutSchemaRequiredSet(body)
	names := sortedShortcutSchemaNames(properties)
	out := make([]describeFieldParam, 0, len(names))
	for _, name := range names {
		field, _ := properties[name].(map[string]any)
		_, isRequired := required[name]
		out = append(out, describeFieldParam{
			Name:        name,
			In:          "body",
			Required:    isRequired,
			Type:        shortcutSchemaType(field),
			Format:      shortcutSchemaString(field, "format"),
			Description: conciseFieldDescription(shortcutSchemaString(field, "description")),
			Example:     shortcutSchemaExample(field),
			Enum:        shortcutSchemaStringSlice(field["enum"]),
			Pattern:     shortcutSchemaString(field, "pattern"),
			Minimum:     shortcutSchemaNumber(field["minimum"]),
			Maximum:     shortcutSchemaNumber(field["maximum"]),
			MinLength:   shortcutSchemaInteger(field["minLength"]),
			MaxLength:   shortcutSchemaInteger(field["maxLength"]),
		})
	}
	return out
}

func shortcutSchemaStringSlice(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if value, ok := item.(string); ok {
			out = append(out, value)
		}
	}
	return out
}

func shortcutSchemaNumber(value any) *float64 {
	number, ok := value.(float64)
	if !ok {
		return nil
	}
	return &number
}

func shortcutSchemaInteger(value any) *int {
	number, ok := value.(float64)
	if !ok {
		return nil
	}
	integer := int(number)
	return &integer
}
