package main

import (
	"sort"
	"strings"
)

// sourceOperation is the generator's normalized source model. Swagger and API
// docs are traversed once, then each checked-in catalog projects from this
// representation without depending on another catalog's wire DTO.
type sourceOperation struct {
	ID               string
	Group            string
	GroupTitle       string
	Action           string
	Resource         string
	Verb             string
	Family           string
	Method           string
	Path             string
	Visibility       string
	Summary          string
	InputSchema      map[string]any
	OutputPolicy     string
	ErrorRecovery    string
	DocSource        string
	UsageConstraints string
	RiskLevel        string
	SupportsDryRun   bool
	SupportsAll      bool
	IsEnvelopeOutput bool
}

func buildSourceOperations(
	doc swaggerDoc,
	groupKeys map[string]string,
	tagTitles map[string]string,
	docIndex map[string]apiDocEntry,
	overrides toolCatalogOverrides,
) []sourceOperation {
	operations := make([]sourceOperation, 0, 512)
	used := map[string]map[string]string{}
	paths := make([]string, 0, len(doc.Paths))
	for path := range doc.Paths {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		item := doc.Paths[path]
		methods := []struct {
			name string
			op   *swaggerOp
		}{
			{"GET", item.Get},
			{"POST", item.Post},
			{"PUT", item.Put},
			{"DELETE", item.Delete},
			{"PATCH", item.Patch},
			{"HEAD", item.Head},
			{"OPTIONS", item.Options},
		}
		for _, method := range methods {
			if method.op == nil || method.op.Deprecated {
				continue
			}
			summary := strings.TrimSpace(method.op.Summary)
			docEntry, documented := docIndex[summary]
			if len(docIndex) > 0 && !documented {
				continue
			}
			groupTitle := resolveGroupTitle(method.op.Tags, tagTitles, docEntry.GroupTitle)
			group := groupName(groupTitle, groupKeys)
			action := actionName(group, summary, method.name, path)
			if used[group] == nil {
				used[group] = map[string]string{}
			}
			signature := method.name + " " + path
			if previous, ok := used[group][action]; ok && previous != signature {
				action = disambiguateAction(action, path, method.name, used[group])
			}
			used[group][action] = signature

			verb, resource := splitVerbNoun(action)
			verb = strings.ToLower(strings.TrimSpace(verb))
			if verb == "" {
				verb = inferToolActionVerb(action, method.name)
			}
			resource = strings.TrimSpace(resource)
			if resource == "" {
				resource = inferResourceFromPath(path)
			}
			family := group
			if strings.TrimSpace(family) == "" {
				family = resource
			}
			if family == "" {
				family = "misc"
			}
			params := mergeParams(item.Parameters, method.op.Parameters)
			operation := sourceOperation{
				ID:               strings.TrimSpace(group) + "." + toKebab(action),
				Group:            group,
				GroupTitle:       groupTitle,
				Action:           action,
				Resource:         normalizeResourceToken(resource),
				Verb:             verb,
				Family:           family,
				Method:           method.name,
				Path:             path,
				Visibility:       "public",
				Summary:          summary,
				InputSchema:      buildToolInputSchema(params, doc.Definitions, docEntry.RequestParamsDoc),
				OutputPolicy:     inferToolOutputPolicy(action, method.name),
				ErrorRecovery:    inferToolErrorRecovery(action, method.name),
				DocSource:        inferToolDocSource(summary, path, groupTitle, docEntry),
				UsageConstraints: inferToolUsageConstraints(action, group, path),
				RiskLevel:        inferToolRiskLevel(action, method.name),
				SupportsDryRun:   true,
				SupportsAll:      inferSupportsAll(action, method.name, params),
				IsEnvelopeOutput: true,
			}
			applyPublicSourceInputSchemaOverrides(&operation)
			operations = append(operations, operation)
		}
	}
	assignCanonicalSourceOperationIDs(operations)
	for i := range operations {
		operations[i] = applySourceOperationOverrides(operations[i], overrides)
	}
	return operations
}

func assignCanonicalSourceOperationIDs(operations []sourceOperation) {
	verbCounts := map[string]int{}
	for _, operation := range operations {
		key := canonicalToolVerbKey(operation.Group, operation.Verb)
		if key != "" {
			verbCounts[key]++
		}
	}
	for i := range operations {
		group := strings.TrimSpace(operations[i].Group)
		if group == "" {
			continue
		}
		verb := normalizeIdentityToken(operations[i].Verb)
		longID := group + "." + toKebab(operations[i].Action)
		if verb != "" && verbCounts[canonicalToolVerbKey(group, verb)] == 1 {
			operations[i].ID = group + "." + verb
		} else {
			operations[i].ID = longID
		}
	}
}

func applySourceOperationOverrides(operation sourceOperation, overrides toolCatalogOverrides) sourceOperation {
	id := strings.TrimSpace(operation.ID)
	if id == "" {
		return operation
	}
	if value := strings.TrimSpace(overrides.Risk[id]); value != "" {
		operation.RiskLevel = value
	}
	if value := strings.TrimSpace(overrides.ErrorRecovery[id]); value != "" {
		operation.ErrorRecovery = value
	}
	if value := strings.TrimSpace(overrides.OutputPolicy[id]); value != "" {
		operation.OutputPolicy = value
	}
	if value := strings.TrimSpace(overrides.UsageConstraints[id]); value != "" {
		operation.UsageConstraints = value
	}
	return operation
}
