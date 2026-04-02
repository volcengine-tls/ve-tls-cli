package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func runCapabilities(ctx *Context, args []string) (any, error) {
	if hasHelp(args) {
		return nil, &usageError{Text: usageCapabilities(), ExitCode: 0}
	}
	group := ""
	action := ""
	hintsFile := ""
	view := "compact"
	for len(args) > 0 {
		switch args[0] {
		case "--group":
			if len(args) < 2 {
				return nil, errors.New("missing --group value")
			}
			group = args[1]
			args = args[2:]
		case "--action":
			if len(args) < 2 {
				return nil, errors.New("missing --action value")
			}
			action = args[1]
			args = args[2:]
		case "--hints-file":
			if len(args) < 2 {
				return nil, errors.New("missing --hints-file value")
			}
			hintsFile = args[1]
			args = args[2:]
		case "--view":
			if len(args) < 2 {
				return nil, errors.New("missing --view value")
			}
			view = strings.ToLower(strings.TrimSpace(args[1]))
			if view != "compact" && view != "full" && view != "text" && view != "groups" {
				return nil, errors.New("invalid --view: " + args[1])
			}
			args = args[2:]
		default:
			return nil, errors.New("unknown flag: " + args[0])
		}
	}
	doc, err := loadAPICapabilities()
	if err != nil {
		return nil, err
	}
	overrides, err := loadCapabilityHintOverrides(hintsFile)
	if err != nil {
		return nil, err
	}
	doc = enrichCapabilitiesDoc(doc, overrides)
	doc, err = filterCapabilities(doc, group, action)
	if err != nil {
		return nil, err
	}
	if view == "text" {
		return renderCapabilitiesText(doc), nil
	}
	if view == "groups" {
		return renderCapabilitiesGroups(doc), nil
	}
	return applyCapabilitiesView(doc, view), nil
}

func renderCapabilitiesText(doc apiCapabilitiesDoc) string {
	if len(doc.Commands) == 0 {
		return "No commands matched.\n"
	}
	grouped := map[string][]apiCapabilityCommand{}
	for _, c := range doc.Commands {
		g := normalizeToken(c.Group)
		grouped[g] = append(grouped[g], c)
	}
	groups := make([]string, 0, len(grouped))
	for g := range grouped {
		groups = append(groups, g)
	}
	sort.Strings(groups)
	var b strings.Builder
	for _, g := range groups {
		title := strings.TrimSpace(grouped[g][0].GroupTitle)
		b.WriteString(g)
		if title != "" {
			b.WriteString(" (")
			b.WriteString(title)
			b.WriteString(")")
		}
		b.WriteString(":\n")
		cmds := grouped[g]
		sortCapabilities(cmds)
		for _, c := range cmds {
			b.WriteString("  - ")
			b.WriteString(c.Action)
			if s := strings.TrimSpace(c.Description); s != "" {
				b.WriteString(": ")
				b.WriteString(s)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

func renderCapabilitiesGroups(doc apiCapabilitiesDoc) string {
	if len(doc.Commands) == 0 {
		return "No groups matched.\n"
	}
	grouped := map[string][]apiCapabilityCommand{}
	for _, c := range doc.Commands {
		g := normalizeToken(c.Group)
		grouped[g] = append(grouped[g], c)
	}
	groups := make([]string, 0, len(grouped))
	for g := range grouped {
		groups = append(groups, g)
	}
	sort.Strings(groups)
	var b strings.Builder
	for _, g := range groups {
		cmds := grouped[g]
		sortCapabilities(cmds)
		groupTitle := strings.TrimSpace(cmds[0].GroupTitle)
		b.WriteString(g)
		if groupTitle != "" {
			b.WriteString(" (")
			b.WriteString(groupTitle)
			b.WriteString(")")
		}
		b.WriteString(": ")
		b.WriteString(summarizeGroup(cmds))
		b.WriteByte('\n')
	}
	return b.String()
}

func filterCapabilities(doc apiCapabilitiesDoc, group string, action string) (apiCapabilitiesDoc, error) {
	g := normalizeToken(group)
	a := normalizeToken(action)
	groupExists := false
	out := apiCapabilitiesDoc{
		Version:  doc.Version,
		Meta:     doc.Meta,
		Commands: make([]apiCapabilityCommand, 0, len(doc.Commands)),
	}
	if a != "" && g == "" {
		matched := make([]apiCapabilityCommand, 0, 8)
		for _, c := range doc.Commands {
			if normalizeToken(c.Action) == a {
				matched = append(matched, c)
			}
		}
		if len(matched) == 0 {
			return apiCapabilitiesDoc{}, errors.New("action not found: " + action)
		}
		if len(matched) > 1 {
			return apiCapabilitiesDoc{}, errors.New("action is ambiguous, use --group with --action: " + action)
		}
		out.Commands = append(out.Commands, matched[0])
		sortCapabilities(out.Commands)
		return out, nil
	}
	for _, c := range doc.Commands {
		if g != "" && normalizeToken(c.Group) == g {
			groupExists = true
		}
		if g != "" && normalizeToken(c.Group) != g {
			continue
		}
		if a != "" && normalizeToken(c.Action) != a {
			continue
		}
		out.Commands = append(out.Commands, c)
	}
	if g != "" && a != "" && len(out.Commands) == 0 {
		if groupExists {
			return apiCapabilitiesDoc{}, errors.New("action not found: " + action)
		}
		return apiCapabilitiesDoc{}, errors.New("group not found: " + group)
	}
	if g != "" && len(out.Commands) == 0 {
		return apiCapabilitiesDoc{}, errors.New("group not found: " + group)
	}
	if a != "" && len(out.Commands) == 0 {
		return apiCapabilitiesDoc{}, errors.New("action not found: " + action)
	}
	sortCapabilities(out.Commands)
	return out, nil
}

func enrichCapabilitiesDoc(doc apiCapabilitiesDoc, overrides map[string]capabilityHintRule) apiCapabilitiesDoc {
	if strings.TrimSpace(doc.Meta.SchemaVersion) == "" {
		doc.Meta.SchemaVersion = "v3"
	}
	if strings.TrimSpace(doc.Meta.ContractVersion) == "" {
		doc.Meta.ContractVersion = strings.TrimSpace(doc.Version)
	}
	if strings.TrimSpace(doc.Meta.HintsMode) == "" {
		doc.Meta.HintsMode = "declarative_only"
	}
	if strings.TrimSpace(doc.Meta.ParamDocSource) == "" {
		doc.Meta.ParamDocSource = "official_doc_preferred"
	}
	doc.Meta.SupportsDryRun = true
	if strings.TrimSpace(doc.Meta.OutputModeHint) == "" {
		doc.Meta.OutputModeHint = "envelope"
	}
	for i := range doc.Commands {
		enrichCapabilitySemantics(&doc.Commands[i])
		doc.Commands[i].SupportsDryRun = true
		if strings.TrimSpace(doc.Commands[i].OutputModeHint) == "" {
			doc.Commands[i].OutputModeHint = doc.Meta.OutputModeHint
		}
		doc.Commands[i].RiskLevel = inferCapabilityRisk(doc.Commands[i])
		applyCapabilityHintOverride(&doc.Commands[i], overrides)
	}
	return doc
}

func enrichCapabilitySemantics(cmd *apiCapabilityCommand) {
	requiredFlags := make([]string, 0, 4)
	bodyRequired := false
	hasBody := false
	hasFlags := false
	for _, p := range cmd.Params {
		loc := strings.ToLower(strings.TrimSpace(p.In))
		switch loc {
		case "body":
			hasBody = true
			if p.Required {
				bodyRequired = true
			}
		case "query", "path":
			hasFlags = true
			if p.Required {
				requiredFlags = append(requiredFlags, p.Name)
			}
		}
	}
	cmd.RequiredFlags = uniqueStrings(requiredFlags)
	cmd.BodyRequired = bodyRequired
	cmd.InputMode = inferCapabilityInputMode(hasBody, hasFlags, bodyRequired)
	cmd.Description = inferCapabilityDescription(*cmd)
}

func inferCapabilityInputMode(hasBody bool, hasFlags bool, bodyRequired bool) string {
	switch {
	case hasBody && hasFlags && bodyRequired:
		return "body via --request; query/path via flags"
	case hasBody && hasFlags:
		return "optional body via --request; query/path via flags"
	case hasBody && bodyRequired:
		return "body via --request"
	case hasBody:
		return "optional body via --request"
	case hasFlags:
		return "query/path via flags"
	default:
		return "no required request body; optional flags only"
	}
}

func inferCapabilityDescription(cmd apiCapabilityCommand) string {
	if desc := compactText(bodyParamDescription(cmd.Params)); desc != "" {
		return desc
	}
	if desc := compactText(strings.TrimSpace(cmd.Summary)); desc != "" && !strings.EqualFold(desc, strings.TrimSpace(cmd.Action)) {
		return desc
	}
	return describeActionFallback(strings.TrimSpace(cmd.Action))
}

func bodyParamDescription(params []apiCapParam) string {
	for _, p := range params {
		if !strings.EqualFold(strings.TrimSpace(p.In), "body") {
			continue
		}
		desc := strings.TrimSpace(p.Description)
		if desc == "" || looksLikeParamDescription(desc) {
			continue
		}
		return desc
	}
	return ""
}

func looksLikeParamDescription(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	for _, prefix := range []string{"请求参数", "删除参数", "查询参数", "修改参数", "创建参数"} {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return strings.Contains(s, "字段用途/必填/限制")
}

func compactText(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	replacer := strings.NewReplacer("\n", " ", "\t", " ", "  ", " ")
	s = replacer.Replace(s)
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	for _, sep := range []string{"（", "(", "。", ". "} {
		if idx := strings.Index(s, sep); idx > 0 {
			return strings.TrimSpace(s[:idx])
		}
	}
	if len([]rune(s)) > 120 {
		runes := []rune(s)
		return strings.TrimSpace(string(runes[:120])) + "..."
	}
	return s
}

func describeActionFallback(action string) string {
	action = strings.TrimSpace(action)
	if action == "" {
		return ""
	}
	for _, prefix := range []struct {
		Key  string
		Verb string
	}{
		{Key: "Describe", Verb: "查询"},
		{Key: "Get", Verb: "查询"},
		{Key: "List", Verb: "查询"},
		{Key: "Search", Verb: "检索"},
		{Key: "Create", Verb: "创建"},
		{Key: "Modify", Verb: "修改"},
		{Key: "Update", Verb: "修改"},
		{Key: "Delete", Verb: "删除"},
		{Key: "Consume", Verb: "消费"},
		{Key: "Put", Verb: "写入"},
		{Key: "WebTracks", Verb: "写入"},
		{Key: "Enable", Verb: "启用"},
		{Key: "Disable", Verb: "停用"},
		{Key: "Open", Verb: "开启"},
		{Key: "Close", Verb: "关闭"},
	} {
		if strings.HasPrefix(action, prefix.Key) {
			target := strings.TrimSpace(splitCamelWords(strings.TrimPrefix(action, prefix.Key)))
			if target == "" {
				return prefix.Verb + "接口"
			}
			return prefix.Verb + " " + target
		}
	}
	return "调用 " + action + " 接口"
}

func splitCamelWords(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func uniqueStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func summarizeGroup(cmds []apiCapabilityCommand) string {
	if len(cmds) == 0 {
		return ""
	}
	parts := summarizeGroupTags(cmds)
	title := strings.TrimSpace(cmds[0].GroupTitle)
	if title == "" {
		title = strings.TrimSpace(cmds[0].Group)
	}
	if len(parts) == 0 {
		return title + "相关接口集合"
	}
	return title + "相关接口，主要覆盖" + strings.Join(parts, "、") + "等能力"
}

func summarizeGroupTags(cmds []apiCapabilityCommand) []string {
	verbs := map[string]int{}
	for _, c := range cmds {
		verbs[inferActionVerb(c.Action)]++
	}
	order := []string{"查询", "创建", "修改", "删除", "检索", "写入", "消费", "管理", "调用"}
	parts := make([]string, 0, 6)
	for _, verb := range order {
		if verbs[verb] > 0 {
			parts = append(parts, verb)
		}
	}
	return parts
}

func inferActionVerb(action string) string {
	a := strings.ToLower(strings.TrimSpace(action))
	switch {
	case strings.HasPrefix(a, "describe"), strings.HasPrefix(a, "get"), strings.HasPrefix(a, "list"):
		return "查询"
	case strings.HasPrefix(a, "create"), strings.HasPrefix(a, "open"), strings.HasPrefix(a, "enable"), strings.HasPrefix(a, "active"):
		return "创建"
	case strings.HasPrefix(a, "modify"), strings.HasPrefix(a, "update"), strings.HasPrefix(a, "refresh"), strings.HasPrefix(a, "reset"), strings.HasPrefix(a, "associate"), strings.HasPrefix(a, "disassociate"), strings.HasPrefix(a, "apply"):
		return "修改"
	case strings.HasPrefix(a, "delete"), strings.HasPrefix(a, "close"), strings.HasPrefix(a, "disable"), strings.HasPrefix(a, "cancel"):
		return "删除"
	case strings.HasPrefix(a, "search"), strings.HasPrefix(a, "preview"), strings.HasPrefix(a, "archive"):
		return "检索"
	case strings.HasPrefix(a, "put"), strings.HasPrefix(a, "webtracks"):
		return "写入"
	case strings.HasPrefix(a, "consume"):
		return "消费"
	default:
		return "调用"
	}
}

func inferCapabilityRisk(cmd apiCapabilityCommand) string {
	method := strings.ToUpper(strings.TrimSpace(cmd.Method))
	action := normalizeToken(cmd.Action)

	isReadLike := method == "GET" || method == "HEAD" || method == "OPTIONS" ||
		strings.HasPrefix(action, "describe") ||
		strings.HasPrefix(action, "search") ||
		strings.HasPrefix(action, "list") ||
		strings.HasPrefix(action, "get") ||
		strings.HasPrefix(action, "consume") ||
		strings.HasPrefix(action, "preview") ||
		action == "statistics" ||
		strings.HasPrefix(action, "statistics")
	if isReadLike {
		return "low"
	}

	if method == "PUT" || method == "DELETE" {
		return "high"
	}

	return "high"
}

func applyCapabilitiesView(doc apiCapabilitiesDoc, view string) apiCapabilitiesDoc {
	doc.Version = ""
	doc.Meta.SchemaVersion = ""
	for i := range doc.Commands {
		for j := range doc.Commands[i].Params {
			doc.Commands[i].Params[j].Ref = ""
		}
	}
	if strings.TrimSpace(view) == "full" {
		return doc
	}
	for i := range doc.Commands {
		doc.Commands[i].Method = ""
		doc.Commands[i].Path = ""
		doc.Commands[i].Params = nil
		doc.Commands[i].RequestParamsDoc = nil
	}
	return doc
}

type capabilityHintsDoc struct {
	Rules []capabilityHintRule `json:"rules"`
}

type capabilityHintRule struct {
	Group          string `json:"group"`
	Action         string `json:"action"`
	RiskLevel      string `json:"risk_level,omitempty"`
	SupportsDryRun *bool  `json:"supports_dry_run,omitempty"`
	OutputModeHint string `json:"output_mode_hint,omitempty"`
}

func loadCapabilityHintOverrides(path string) (map[string]capabilityHintRule, error) {
	p := strings.TrimSpace(path)
	if p == "" {
		return map[string]capabilityHintRule{}, nil
	}
	b, err := os.ReadFile(filepath.Clean(p))
	if err != nil {
		return nil, err
	}
	var doc capabilityHintsDoc
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	out := map[string]capabilityHintRule{}
	for _, r := range doc.Rules {
		g := normalizeToken(r.Group)
		a := normalizeToken(r.Action)
		if a == "" {
			continue
		}
		if g == "" {
			g = "*"
		}
		out[g+"."+a] = r
	}
	return out, nil
}

func applyCapabilityHintOverride(cmd *apiCapabilityCommand, overrides map[string]capabilityHintRule) {
	if len(overrides) == 0 {
		return
	}
	group := normalizeToken(cmd.Group)
	action := normalizeToken(cmd.Action)
	r, ok := overrides[group+"."+action]
	if !ok {
		r, ok = overrides["*."+action]
		if !ok {
			return
		}
	}
	if strings.TrimSpace(r.RiskLevel) != "" {
		cmd.RiskLevel = strings.TrimSpace(r.RiskLevel)
	}
	if strings.TrimSpace(r.OutputModeHint) != "" {
		cmd.OutputModeHint = strings.TrimSpace(r.OutputModeHint)
	}
	if r.SupportsDryRun != nil {
		cmd.SupportsDryRun = *r.SupportsDryRun
	}
}

func sortCapabilities(cmds []apiCapabilityCommand) {
	sort.Slice(cmds, func(i, j int) bool {
		gi := strings.ToLower(strings.TrimSpace(cmds[i].Group))
		gj := strings.ToLower(strings.TrimSpace(cmds[j].Group))
		if gi != gj {
			return gi < gj
		}
		ai := strings.ToLower(strings.TrimSpace(cmds[i].Action))
		aj := strings.ToLower(strings.TrimSpace(cmds[j].Action))
		if ai != aj {
			return ai < aj
		}
		mi := strings.ToUpper(strings.TrimSpace(cmds[i].Method))
		mj := strings.ToUpper(strings.TrimSpace(cmds[j].Method))
		if mi != mj {
			return mi < mj
		}
		return strings.TrimSpace(cmds[i].Path) < strings.TrimSpace(cmds[j].Path)
	})
}
