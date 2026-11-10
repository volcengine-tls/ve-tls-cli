package contract

import (
	"fmt"
	"math"
	"sort"
)

type templateCondition struct {
	condition map[string]any
	then      any
	otherwise any
}

type templateConditionDecision struct {
	condition map[string]any
	matched   bool
}

// projectTemplateConditions applies the catalog's object-level allOf and
// conditional overlays without changing the source schema. Only properties
// already selected by the requested template mode participate in an if test.
func projectTemplateConditions(
	schema map[string]any,
	mode TemplateMode,
	path string,
	seed map[string]any,
) (map[string]any, map[string]any, []templateConditionDecision, bool, error) {
	_, hasAllOf := schema["allOf"]
	_, hasIf := schema["if"]
	if !hasAllOf && !hasIf {
		return schema, seed, nil, false, nil
	}
	projected := cloneMap(schema)
	conditions := make([]templateCondition, 0)
	addCondition := func(node map[string]any) error {
		raw, exists := node["if"]
		if !exists {
			return nil
		}
		condition, ok := templateSchemaMap(raw)
		if !ok {
			return fmt.Errorf("%s if must be a schema object", path)
		}
		conditions = append(conditions, templateCondition{condition: condition, then: node["then"], otherwise: node["else"]})
		return nil
	}
	if err := addCondition(schema); err != nil {
		return nil, nil, nil, false, err
	}
	if hasAllOf {
		parts, ok := templateSlice(schema["allOf"])
		if !ok {
			return nil, nil, nil, false, fmt.Errorf("%s allOf must be an array", path)
		}
		for i, raw := range parts {
			part, ok := templateSchemaMap(raw)
			if !ok {
				return nil, nil, nil, false, fmt.Errorf("%s allOf[%d] must be a schema object", path, i)
			}
			if _, nested := part["allOf"]; nested {
				return nil, nil, nil, false, fmt.Errorf("%s nested allOf projection is unsupported", path)
			}
			if err := addCondition(part); err != nil {
				return nil, nil, nil, false, err
			}
			overlay := make(map[string]any)
			for key, value := range part {
				if key != "if" && key != "then" && key != "else" {
					overlay[key] = value
				}
			}
			if err := mergeTemplateOverlay(projected, overlay, path); err != nil {
				return nil, nil, nil, false, err
			}
		}
	}

	properties, err := templateProperties(projected, path)
	if err != nil {
		return nil, nil, nil, false, err
	}
	required := make(map[string]bool)
	if raw, exists := projected["required"]; exists {
		names, err := requiredStringSlice(raw)
		if err != nil {
			return nil, nil, nil, false, fmt.Errorf("%s required must be an array of strings", path)
		}
		for _, name := range names {
			required[name] = true
		}
	}
	selectorNames := make(map[string]bool)
	for _, item := range conditions {
		names, err := templateConditionFields(item.condition, path)
		if err != nil {
			return nil, nil, nil, false, err
		}
		for _, name := range names {
			selectorNames[name] = true
		}
	}
	names := make([]string, 0, len(selectorNames))
	for name := range selectorNames {
		names = append(names, name)
	}
	sort.Strings(names)
	selectedSeed := cloneMap(seed)
	selector := make(map[string]any, len(names))
	for _, name := range names {
		if mode != TemplateFull && !required[name] {
			continue
		}
		raw, exists := properties[name]
		if !exists {
			continue
		}
		field, ok := templateSchemaMap(raw)
		if !ok {
			return nil, nil, nil, false, fmt.Errorf("%s.%s must be a schema object", path, name)
		}
		seedValue, seeded := seed[name]
		value, err := requestValueTemplateSeeded(field, mode, path+"."+name, seedValue, seeded && seedValue != nil)
		if err != nil {
			return nil, nil, nil, false, err
		}
		selector[name] = value
		selectedSeed[name] = cloneValue(value)
	}

	decisions := make([]templateConditionDecision, 0, len(conditions))
	for _, item := range conditions {
		matched, err := matchesTemplateCondition(item.condition, selector, path)
		if err != nil {
			return nil, nil, nil, false, err
		}
		decisions = append(decisions, templateConditionDecision{condition: item.condition, matched: matched})
		selected := item.otherwise
		if matched {
			selected = item.then
		}
		if selected == nil {
			continue
		}
		overlay, ok := templateSchemaMap(selected)
		if !ok {
			return nil, nil, nil, false, fmt.Errorf("%s conditional branch must be a schema object", path)
		}
		if err := mergeTemplateOverlay(projected, overlay, path); err != nil {
			return nil, nil, nil, false, err
		}
	}
	return projected, selectedSeed, decisions, true, nil
}

func templateConditionFields(condition map[string]any, path string) ([]string, error) {
	for key := range condition {
		switch key {
		case "required", "properties", "type", "title", "description", "$comment":
		default:
			return nil, fmt.Errorf("%s if keyword %q is unsupported for template projection", path, key)
		}
	}
	if kind, exists := condition["type"]; exists && kind != "object" {
		return nil, fmt.Errorf("%s if type must be object", path)
	}
	fields := make(map[string]bool)
	if raw, exists := condition["required"]; exists {
		names, err := requiredStringSlice(raw)
		if err != nil {
			return nil, fmt.Errorf("%s if required must be an array of strings", path)
		}
		for _, name := range names {
			fields[name] = true
		}
	}
	if raw, exists := condition["properties"]; exists {
		properties, ok := templateSchemaMap(raw)
		if !ok {
			return nil, fmt.Errorf("%s if properties must be an object", path)
		}
		for name, value := range properties {
			field, ok := templateSchemaMap(value)
			if !ok {
				return nil, fmt.Errorf("%s if property %q must be a schema object", path, name)
			}
			for key := range field {
				switch key {
				case "const", "enum", "type", "title", "description", "$comment", "format", "default", "example":
				default:
					return nil, fmt.Errorf("%s if property keyword %q is unsupported for template projection", path, key)
				}
			}
			fields[name] = true
		}
	}
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func matchesTemplateCondition(condition map[string]any, value map[string]any, path string) (bool, error) {
	if _, err := templateConditionFields(condition, path); err != nil {
		return false, err
	}
	if raw, exists := condition["required"]; exists {
		names, _ := requiredStringSlice(raw)
		for _, name := range names {
			if _, exists := value[name]; !exists {
				return false, nil
			}
		}
	}
	properties, _ := templateSchemaMap(condition["properties"])
	for name, raw := range properties {
		candidate, exists := value[name]
		if !exists {
			continue
		}
		field, _ := templateSchemaMap(raw)
		if constant, exists := field["const"]; exists && !requestEnumContains([]any{constant}, candidate) {
			return false, nil
		}
		if rawEnum, exists := field["enum"]; exists {
			choices, ok := templateSlice(rawEnum)
			if !ok {
				return false, fmt.Errorf("%s if enum must be an array", path)
			}
			if !requestEnumContains(choices, candidate) {
				return false, nil
			}
		}
		if err := validateRequestValue(candidate, field, path+"."+name); err != nil {
			return false, nil
		}
	}
	return true, nil
}

// mergeTemplateOverlay intersects the keywords used by catalog condition
// branches. More complex combinations fail explicitly instead of dropping a
// constraint or producing a misleading template.
func mergeTemplateOverlay(dst, overlay map[string]any, path string) error {
	keys := make([]string, 0, len(overlay))
	for key := range overlay {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := overlay[key]
		switch key {
		case "required":
			extra, err := requiredStringSlice(value)
			if err != nil {
				return fmt.Errorf("%s required must be an array of strings", path)
			}
			set := make(map[string]bool)
			if current, exists := dst[key]; exists {
				names, err := requiredStringSlice(current)
				if err != nil {
					return fmt.Errorf("%s required must be an array of strings", path)
				}
				for _, name := range names {
					set[name] = true
				}
			}
			for _, name := range extra {
				set[name] = true
			}
			names := make([]string, 0, len(set))
			for name := range set {
				names = append(names, name)
			}
			sort.Strings(names)
			dst[key] = names
		case "properties":
			extra, ok := templateSchemaMap(value)
			if !ok {
				return fmt.Errorf("%s properties must be an object", path)
			}
			current, err := templateProperties(dst, path)
			if err != nil {
				return err
			}
			if _, exists := dst[key]; !exists {
				current = map[string]any{}
				dst[key] = current
			}
			names := make([]string, 0, len(extra))
			for name := range extra {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				extraField, ok := templateSchemaMap(extra[name])
				if !ok {
					return fmt.Errorf("%s.%s must be a schema object", path, name)
				}
				currentField, exists := current[name]
				if !exists {
					return fmt.Errorf("%s.%s conditional property is absent from base properties", path, name)
				}
				field, ok := templateSchemaMap(currentField)
				if !ok {
					return fmt.Errorf("%s.%s must be a schema object", path, name)
				}
				if err := mergeTemplateOverlay(field, extraField, path+"."+name); err != nil {
					return err
				}
			}
		case "minimum", "minLength", "minItems", "maximum", "maxLength", "maxItems":
			incoming, ok := templateNumberValue(value)
			if !ok || math.IsNaN(incoming) || math.IsInf(incoming, 0) {
				return fmt.Errorf("%s %s must be a finite number", path, key)
			}
			current, exists := dst[key]
			if !exists {
				dst[key] = cloneValue(value)
				continue
			}
			existing, ok := templateNumberValue(current)
			if !ok || math.IsNaN(existing) || math.IsInf(existing, 0) {
				return fmt.Errorf("%s %s must be a finite number", path, key)
			}
			minimum := key == "minimum" || key == "minLength" || key == "minItems"
			if (minimum && incoming > existing) || (!minimum && incoming < existing) {
				dst[key] = cloneValue(value)
			}
		case "enum":
			incoming, ok := templateSlice(value)
			if !ok {
				return fmt.Errorf("%s enum must be an array", path)
			}
			current, exists := dst[key]
			if !exists {
				dst[key] = cloneValue(value)
				continue
			}
			existing, ok := templateSlice(current)
			if !ok {
				return fmt.Errorf("%s enum must be an array", path)
			}
			intersection := make([]any, 0, len(existing))
			for _, candidate := range existing {
				if requestEnumContains(incoming, candidate) {
					intersection = append(intersection, cloneValue(candidate))
				}
			}
			if len(intersection) == 0 {
				return fmt.Errorf("%s conditional enums have no common value", path)
			}
			dst[key] = intersection
		case "type":
			kind, ok := value.(string)
			if !ok {
				return fmt.Errorf("%s conditional type must be a string", path)
			}
			if current, exists := dst[key]; exists {
				existing, ok := current.(string)
				if !ok || existing != kind {
					return fmt.Errorf("%s conditional types conflict", path)
				}
			}
			dst[key] = kind
		case "description", "title", "$comment", "format", "default", "example":
			if _, exists := dst[key]; !exists {
				dst[key] = cloneValue(value)
			}
		default:
			return fmt.Errorf("%s conditional keyword %q is unsupported for template projection", path, key)
		}
	}
	return nil
}
