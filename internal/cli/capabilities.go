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
			if view != "compact" && view != "full" {
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
	return applyCapabilitiesView(doc, view), nil
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
		doc.Commands[i].SupportsDryRun = true
		if strings.TrimSpace(doc.Commands[i].OutputModeHint) == "" {
			doc.Commands[i].OutputModeHint = doc.Meta.OutputModeHint
		}
		risk, idem := inferCapabilityHints(doc.Commands[i])
		doc.Commands[i].RiskLevel = risk
		doc.Commands[i].Idempotency = idem
		applyCapabilityHintOverride(&doc.Commands[i], overrides)
	}
	return doc
}

func inferCapabilityHints(cmd apiCapabilityCommand) (riskLevel string, idempotency string) {
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
		return "low", "idempotent"
	}

	if method == "PUT" || method == "DELETE" {
		return "high", "likely_idempotent"
	}

	return "high", "unknown"
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
	Idempotency    string `json:"idempotency,omitempty"`
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
	if strings.TrimSpace(r.Idempotency) != "" {
		cmd.Idempotency = strings.TrimSpace(r.Idempotency)
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
