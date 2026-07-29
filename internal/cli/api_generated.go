package cli

import (
	"encoding/json"
	"strings"
	"sync"
)

type apiCapabilitiesDoc struct {
	Version  string                 `json:"version,omitempty"`
	Meta     apiCapabilitiesMeta    `json:"meta,omitempty"`
	Commands []apiCapabilityCommand `json:"commands"`
}

type apiCapabilitiesMeta struct {
	SchemaVersion   string `json:"schema_version,omitempty"`
	ContractVersion string `json:"contract_version,omitempty"`
	HintsMode       string `json:"hints_mode,omitempty"`
	ParamDocSource  string `json:"param_doc_source,omitempty"`
	SupportsDryRun  bool   `json:"supports_dry_run,omitempty"`
	OutputModeHint  string `json:"output_mode_hint,omitempty"`
}

type apiCapabilityCommand struct {
	Group            string           `json:"group"`
	GroupTitle       string           `json:"group_title,omitempty"`
	Action           string           `json:"action"`
	Summary          string           `json:"summary,omitempty"`
	Description      string           `json:"description,omitempty"`
	Method           string           `json:"method,omitempty"`
	Path             string           `json:"path,omitempty"`
	Params           []apiCapParam    `json:"params,omitempty"`
	RequestParamsDoc []apiCapDocParam `json:"request_params_doc,omitempty"`
	InputMode        string           `json:"input_mode,omitempty"`
	RequiredFlags    []string         `json:"required_flags,omitempty"`
	BodyRequired     bool             `json:"body_required,omitempty"`
	SupportsDryRun   bool             `json:"supports_dry_run,omitempty"`
	OutputModeHint   string           `json:"output_mode_hint,omitempty"`
	RiskLevel        string           `json:"risk_level,omitempty"`
	AgentEntrypoint  string           `json:"agent_entrypoint,omitempty"`
	AgentNextStep    string           `json:"agent_next_step,omitempty"`
	RelatedShortcuts []string         `json:"related_shortcuts,omitempty"`
}

type apiCapParam struct {
	Name         string   `json:"name"`
	CLIFlag      string   `json:"cli_flag,omitempty"`
	In           string   `json:"in"`
	Required     bool     `json:"required,omitempty"`
	RequiredText string   `json:"required_text,omitempty"`
	Type         string   `json:"type,omitempty"`
	Format       string   `json:"format,omitempty"`
	Ref          string   `json:"ref,omitempty"`
	Description  string   `json:"description,omitempty"`
	Example      string   `json:"example,omitempty"`
	Enum         []string `json:"enum,omitempty"`
	Pattern      string   `json:"pattern,omitempty"`
	Minimum      *float64 `json:"minimum,omitempty"`
	Maximum      *float64 `json:"maximum,omitempty"`
	MinLength    *int     `json:"min_length,omitempty"`
	MaxLength    *int     `json:"max_length,omitempty"`
}

type apiCapDocParam struct {
	Name         string `json:"name"`
	In           string `json:"in,omitempty"`
	Type         string `json:"type,omitempty"`
	RequiredText string `json:"required_text,omitempty"`
	Example      string `json:"example,omitempty"`
	Description  string `json:"description,omitempty"`
}

type apiActionOp struct {
	Cmd        apiCapabilityCommand
	ParamFlags map[string]apiCapParam
}

var (
	apiCapabilitiesOnce sync.Once
	cachedAPICapDoc     apiCapabilitiesDoc
	cachedAPICapErr     error
)

func loadAPICapabilities() (apiCapabilitiesDoc, error) {
	apiCapabilitiesOnce.Do(func() {
		var doc apiCapabilitiesDoc
		if err := json.Unmarshal([]byte(generatedCapabilitiesJSON), &doc); err != nil {
			cachedAPICapErr = err
			return
		}
		if doc.Commands == nil {
			doc.Commands = []apiCapabilityCommand{}
		}
		doc = normalizeLoadedAPICapabilities(doc)
		cachedAPICapDoc = doc
	})
	if cachedAPICapErr != nil {
		return cachedAPICapDoc, cachedAPICapErr
	}
	return cachedAPICapDoc, nil
}

func normalizeLoadedAPICapabilities(doc apiCapabilitiesDoc) apiCapabilitiesDoc {
	filtered := make([]apiCapabilityCommand, 0, len(doc.Commands))
	for i := range doc.Commands {
		if summary := strings.TrimSpace(doc.Commands[i].Summary); summary != "" {
			doc.Commands[i].Action = summary
		}
		if !isPublishedOfficialCommand(doc.Commands[i]) {
			continue
		}
		enrichCapabilitySemantics(&doc.Commands[i])
		filtered = append(filtered, doc.Commands[i])
	}
	doc.Commands = filtered
	return doc
}

func isPublishedOfficialCommand(cmd apiCapabilityCommand) bool {
	if len(sanitizeRequestParamsDocForOutput(cmd.RequestParamsDoc)) > 0 {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(cmd.Path)) {
	case "/activetlsaccount", "/getaccountstatus", "/describecursortime", "/describeprocessorfunctions":
		return true
	default:
		return false
	}
}

func buildAPIIndex(doc apiCapabilitiesDoc) map[string]map[string][]apiActionOp {
	index := map[string]map[string][]apiActionOp{}
	for _, c := range doc.Commands {
		group := normalizeToken(c.Group)
		action := normalizeActionToken(c.Action)
		if group == "" || action == "" {
			continue
		}
		if index[group] == nil {
			index[group] = map[string][]apiActionOp{}
		}
		op := apiActionOp{
			Cmd:        c,
			ParamFlags: map[string]apiCapParam{},
		}
		for _, p := range c.Params {
			loc := strings.ToLower(strings.TrimSpace(p.In))
			if loc != "query" && loc != "path" {
				continue
			}
			flag := "--" + toKebab(p.Name)
			if _, exists := op.ParamFlags[flag]; exists {
				flag = flag + "-" + loc
			}
			op.ParamFlags[flag] = p
		}
		index[group][action] = append(index[group][action], op)
	}
	return index
}

func selectOpByMethod(ops []apiActionOp, method string) (apiActionOp, bool) {
	for _, op := range ops {
		if strings.EqualFold(strings.TrimSpace(op.Cmd.Method), method) {
			return op, true
		}
	}
	return apiActionOp{}, false
}

func normalizeToken(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func normalizeActionToken(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func toKebab(s string) string {
	runes := []rune(strings.TrimSpace(s))
	if len(runes) == 0 {
		return ""
	}
	var b strings.Builder
	prevDash := false
	for i, r := range runes {
		switch {
		case r >= 'A' && r <= 'Z':
			if i > 0 && !prevDash && shouldInsertDashBeforeUpper(runes, i) {
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
	out = strings.ReplaceAll(out, "--", "-")
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	return out
}

func shouldInsertDashBeforeUpper(runes []rune, i int) bool {
	prev := runes[i-1]
	if (prev >= 'a' && prev <= 'z') || (prev >= '0' && prev <= '9') {
		return true
	}
	if prev >= 'A' && prev <= 'Z' && i+1 < len(runes) {
		next := runes[i+1]
		return next >= 'a' && next <= 'z'
	}
	return false
}
