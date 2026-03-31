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
	Required             []string                 `json:"required"`
	Properties           map[string]swaggerSchema `json:"properties"`
	Items                *swaggerSchema           `json:"items"`
	AdditionalProperties json.RawMessage          `json:"additionalProperties"`
	Enum                 []any                    `json:"enum"`
	Pattern              string                   `json:"pattern"`
	Minimum              *float64                 `json:"minimum"`
	Maximum              *float64                 `json:"maximum"`
	MinLength            *int                     `json:"minLength"`
	MaxLength            *int                     `json:"maxLength"`
}

type capabilityDoc struct {
	Version  string              `json:"version"`
	Commands []capabilityCommand `json:"commands"`
}

type capabilityCommand struct {
	Group            string               `json:"group"`
	GroupTitle       string               `json:"group_title,omitempty"`
	Action           string               `json:"action"`
	Summary          string               `json:"summary,omitempty"`
	Method           string               `json:"method"`
	Path             string               `json:"path"`
	Params           []capabilityParam    `json:"params,omitempty"`
	RequestParamsDoc []capabilityDocParam `json:"request_params_doc,omitempty"`
}

type capabilityParam struct {
	Name        string   `json:"name"`
	In          string   `json:"in"`
	Required    bool     `json:"required,omitempty"`
	Type        string   `json:"type,omitempty"`
	Format      string   `json:"format,omitempty"`
	Ref         string   `json:"ref,omitempty"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Pattern     string   `json:"pattern,omitempty"`
	Minimum     *float64 `json:"minimum,omitempty"`
	Maximum     *float64 `json:"maximum,omitempty"`
	MinLength   *int     `json:"min_length,omitempty"`
	MaxLength   *int     `json:"max_length,omitempty"`
}

type capabilityDocParam struct {
	Name         string `json:"name"`
	In           string `json:"in,omitempty"`
	Type         string `json:"type,omitempty"`
	RequiredText string `json:"required_text,omitempty"`
	Example      string `json:"example,omitempty"`
	Description  string `json:"description,omitempty"`
}

type apiDocEntry struct {
	GroupTitle       string
	RequestParamsDoc []capabilityDocParam
}

func main() {
	spec := flag.String("spec", "", "path to swagger.json")
	outCapabilities := flag.String("out-capabilities", "internal/cli/generated_capabilities.go", "output file for capabilities")
	outTemplates := flag.String("out-templates", "internal/cli/generated_request_templates.go", "output file for request templates")
	groupKeyMapping := flag.String("group-key-mapping", "docs/agentic-stage1/group_key_mapping.yaml", "path to group key mapping yaml")
	swaggerTagMapping := flag.String("swagger-tag-mapping", "repos/日志服务/_swagger_tag_mapping.yaml", "path to swagger tag title mapping yaml")
	apiDocRoot := flag.String("api-doc-root", "repos/日志服务/API 参考", "path to api reference markdown root")
	version := flag.String("version", "v1", "capabilities version")
	flag.Parse()

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
	caps := buildCapabilities(doc, strings.TrimSpace(*version), groupKeys, tagTitles, docIndex)
	required, full := buildTemplateMaps(doc)

	if err := writeCapabilitiesGo(*outCapabilities, caps); err != nil {
		fatal(err)
	}
	if err := writeTemplatesGo(*outTemplates, required, full); err != nil {
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

func buildCapabilities(doc swaggerDoc, version string, groupKeys map[string]string, tagTitles map[string]string, docIndex map[string]apiDocEntry) capabilityDoc {
	commands := make([]capabilityCommand, 0, 512)
	used := map[string]map[string]string{}
	paths := make([]string, 0, len(doc.Paths))
	for p := range doc.Paths {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		item := doc.Paths[p]
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
		for _, m := range methods {
			if m.op == nil || m.op.Deprecated {
				continue
			}
			groupTitle := resolveGroupTitle(m.op.Tags, tagTitles, docIndex[strings.TrimSpace(m.op.Summary)].GroupTitle)
			group := groupName(groupTitle, groupKeys)
			action := actionName(group, strings.TrimSpace(m.op.Summary), m.name, p)
			if used[group] == nil {
				used[group] = map[string]string{}
			}
			sign := m.name + " " + p
			if prev, ok := used[group][action]; ok && prev != sign {
				action = disambiguateAction(action, p, m.name, used[group])
			}
			used[group][action] = sign
			params := mergeParams(item.Parameters, m.op.Parameters)
			docEntry := docIndex[strings.TrimSpace(m.op.Summary)]
			commands = append(commands, capabilityCommand{
				Group:            group,
				GroupTitle:       groupTitle,
				Action:           action,
				Summary:          strings.TrimSpace(m.op.Summary),
				Method:           m.name,
				Path:             p,
				Params:           convertParams(params),
				RequestParamsDoc: docEntry.RequestParamsDoc,
			})
		}
	}
	sort.Slice(commands, func(i, j int) bool {
		if commands[i].Group != commands[j].Group {
			return commands[i].Group < commands[j].Group
		}
		if commands[i].Action != commands[j].Action {
			return commands[i].Action < commands[j].Action
		}
		if commands[i].Method != commands[j].Method {
			return commands[i].Method < commands[j].Method
		}
		return commands[i].Path < commands[j].Path
	})
	return capabilityDoc{
		Version:  version,
		Commands: commands,
	}
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

func parseDocRequestParamsMarkdown(md string) []capabilityDocParam {
	lines := strings.Split(md, "\n")
	inRequest := false
	currentLoc := ""
	lastIdx := -1
	out := make([]capabilityDocParam, 0, 16)
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
			out = append(out, capabilityDocParam{
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

func buildTemplateMaps(doc swaggerDoc) (map[string]string, map[string]string) {
	required := map[string]string{}
	full := map[string]string{}
	refs := usedBodyRefs(doc)
	for _, ref := range refs {
		requiredObj, ok := buildTemplateForRef(ref, doc.Definitions, "required", 0, map[string]int{})
		if ok {
			required[ref] = marshalPretty(requiredObj)
		}
		fullObj, ok := buildTemplateForRef(ref, doc.Definitions, "full", 0, map[string]int{})
		if ok {
			full[ref] = marshalPretty(fullObj)
		}
	}
	return required, full
}

func usedBodyRefs(doc swaggerDoc) []string {
	set := map[string]struct{}{}
	for _, item := range doc.Paths {
		ops := []*swaggerOp{item.Get, item.Post, item.Put, item.Delete, item.Patch, item.Head, item.Options}
		for _, op := range ops {
			if op == nil || op.Deprecated {
				continue
			}
			params := mergeParams(item.Parameters, op.Parameters)
			for _, p := range params {
				if strings.EqualFold(strings.TrimSpace(p.In), "body") && p.Schema != nil {
					ref := strings.TrimSpace(p.Schema.Ref)
					if ref != "" {
						set[ref] = struct{}{}
					}
				}
			}
		}
	}
	refs := make([]string, 0, len(set))
	for ref := range set {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	return refs
}

func buildTemplateForRef(ref string, defs map[string]swaggerSchema, mode string, depth int, seen map[string]int) (any, bool) {
	name := strings.TrimPrefix(strings.TrimSpace(ref), "#/definitions/")
	s, ok := defs[name]
	if !ok {
		return nil, false
	}
	return buildTemplateValue(s, defs, mode, depth, seen), true
}

func buildTemplateValue(s swaggerSchema, defs map[string]swaggerSchema, mode string, depth int, seen map[string]int) any {
	if depth > 4 {
		return map[string]any{}
	}
	if strings.TrimSpace(s.Ref) != "" {
		ref := strings.TrimSpace(s.Ref)
		seen[ref]++
		if seen[ref] > 1 {
			return map[string]any{}
		}
		v, ok := buildTemplateForRef(ref, defs, mode, depth+1, seen)
		if !ok {
			return map[string]any{}
		}
		return v
	}
	t := strings.ToLower(strings.TrimSpace(s.Type))
	switch t {
	case "string":
		return ""
	case "integer", "number":
		return 0
	case "boolean":
		return false
	case "array":
		if s.Items == nil {
			return []any{}
		}
		return []any{buildTemplateValue(*s.Items, defs, mode, depth+1, seen)}
	case "object", "":
		if len(s.Properties) == 0 {
			if child, ok := parseAdditionalProperties(s.AdditionalProperties); ok {
				return map[string]any{"key": buildTemplateValue(child, defs, mode, depth+1, seen)}
			}
			return map[string]any{}
		}
		requiredSet := map[string]struct{}{}
		for _, n := range s.Required {
			requiredSet[n] = struct{}{}
		}
		keys := make([]string, 0, len(s.Properties))
		for k := range s.Properties {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if mode == "full" && len(keys) > 40 {
			keys = keys[:40]
		}
		obj := map[string]any{}
		for _, k := range keys {
			if mode == "required" {
				if _, ok := requiredSet[k]; !ok {
					continue
				}
			}
			obj[k] = buildTemplateValue(s.Properties[k], defs, mode, depth+1, seen)
		}
		return obj
	default:
		return ""
	}
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

func convertParams(params []swaggerParam) []capabilityParam {
	out := make([]capabilityParam, 0, len(params))
	for _, p := range params {
		cp := capabilityParam{
			Name:        strings.TrimSpace(p.Name),
			In:          strings.TrimSpace(p.In),
			Required:    p.Required,
			Type:        strings.TrimSpace(p.Type),
			Format:      strings.TrimSpace(p.Format),
			Description: strings.TrimSpace(p.Description),
			Enum:        toStringSlice(p.Enum),
			Pattern:     strings.TrimSpace(p.Pattern),
			Minimum:     p.Minimum,
			Maximum:     p.Maximum,
			MinLength:   p.MinLength,
			MaxLength:   p.MaxLength,
		}
		if p.Schema != nil {
			if cp.Type == "" {
				cp.Type = strings.TrimSpace(p.Schema.Type)
			}
			if cp.Format == "" {
				cp.Format = strings.TrimSpace(p.Schema.Format)
			}
			if len(cp.Enum) == 0 {
				cp.Enum = toStringSlice(p.Schema.Enum)
			}
			if cp.Pattern == "" {
				cp.Pattern = strings.TrimSpace(p.Schema.Pattern)
			}
			if cp.Minimum == nil {
				cp.Minimum = p.Schema.Minimum
			}
			if cp.Maximum == nil {
				cp.Maximum = p.Schema.Maximum
			}
			if cp.MinLength == nil {
				cp.MinLength = p.Schema.MinLength
			}
			if cp.MaxLength == nil {
				cp.MaxLength = p.Schema.MaxLength
			}
			cp.Ref = strings.TrimSpace(p.Schema.Ref)
		}
		out = append(out, cp)
	}
	return out
}

func toStringSlice(v []any) []string {
	if len(v) == 0 {
		return nil
	}
	out := make([]string, 0, len(v))
	for _, item := range v {
		out = append(out, strings.TrimSpace(fmt.Sprint(item)))
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
	prefixes := []string{
		"Describe", "Create", "Modify", "Update", "Delete", "Search", "Export", "Get", "List",
		"Enable", "Disable", "Reset", "Try", "Add", "Remove", "Associate", "Disassociate", "Active", "Encrypt",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(summary, p) {
			return strings.ToLower(p), strings.TrimSpace(summary[len(p):])
		}
	}
	return "", summary
}

func nounMatchesGroup(noun, group string) bool {
	n := normalizeWord(noun)
	g := normalizeWord(group)
	if n == "" || g == "" {
		return false
	}
	if n == g {
		return true
	}
	if strings.TrimSuffix(n, "s") == strings.TrimSuffix(g, "s") {
		return true
	}
	return false
}

func isLikelyPlural(noun string) bool {
	n := normalizeWord(noun)
	return strings.HasSuffix(n, "s")
}

func normalizeWord(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
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

func marshalPretty(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(b)
}

func writeCapabilitiesGo(path string, caps capabilityDoc) error {
	b, err := json.Marshal(caps)
	if err != nil {
		return err
	}
	var out bytes.Buffer
	out.WriteString("// Code generated by internal/openapigen; DO NOT EDIT.\n")
	out.WriteString("package cli\n\n")
	out.WriteString("const generatedCapabilitiesJSON = `")
	out.Write(b)
	out.WriteString("`\n")
	return writeFile(path, out.Bytes())
}

func writeTemplatesGo(path string, required map[string]string, full map[string]string) error {
	keysRequired := make([]string, 0, len(required))
	for k := range required {
		keysRequired = append(keysRequired, k)
	}
	sort.Strings(keysRequired)
	keysFull := make([]string, 0, len(full))
	for k := range full {
		keysFull = append(keysFull, k)
	}
	sort.Strings(keysFull)

	var out bytes.Buffer
	out.WriteString("// Code generated by internal/openapigen; DO NOT EDIT.\n")
	out.WriteString("package cli\n\n")
	out.WriteString("var generatedRequestTemplates = map[string]string{\n")
	for _, k := range keysRequired {
		out.WriteString(fmt.Sprintf("\t%q: %q,\n", k, required[k]))
	}
	out.WriteString("}\n\n")
	out.WriteString("var generatedRequestTemplatesFull = map[string]string{\n")
	for _, k := range keysFull {
		out.WriteString(fmt.Sprintf("\t%q: %q,\n", k, full[k]))
	}
	out.WriteString("}\n")
	return writeFile(path, out.Bytes())
}

func writeFile(path string, data []byte) error {
	p := filepath.Clean(path)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}
