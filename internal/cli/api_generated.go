package cli

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"

	"github.com/volcengine-tls/ve-tls-cli/internal/util"
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
	Method           string           `json:"method"`
	Path             string           `json:"path"`
	Params           []apiCapParam    `json:"params,omitempty"`
	RequestParamsDoc []apiCapDocParam `json:"request_params_doc,omitempty"`
	SupportsDryRun   bool             `json:"supports_dry_run,omitempty"`
	OutputModeHint   string           `json:"output_mode_hint,omitempty"`
	RiskLevel        string           `json:"risk_level,omitempty"`
	Idempotency      string           `json:"idempotency,omitempty"`
}

type apiCapParam struct {
	Name         string   `json:"name"`
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
	for i := range doc.Commands {
		if summary := strings.TrimSpace(doc.Commands[i].Summary); summary != "" {
			doc.Commands[i].Action = summary
		}
	}
	return doc
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

func collectGroupTitles(doc apiCapabilitiesDoc) map[string]string {
	out := map[string]string{}
	for _, c := range doc.Commands {
		group := normalizeToken(c.Group)
		if group == "" {
			continue
		}
		if _, exists := out[group]; exists {
			continue
		}
		if v := strings.TrimSpace(c.GroupTitle); v != "" {
			out[group] = v
		}
	}
	return out
}

func listAPIGroups(index map[string]map[string][]apiActionOp, titles map[string]string) string {
	if len(index) == 0 {
		return "  (none)\n"
	}
	groups := make([]string, 0, len(index))
	for g := range index {
		groups = append(groups, g)
	}
	sort.Strings(groups)
	var b strings.Builder
	for _, g := range groups {
		b.WriteString("  - ")
		b.WriteString(g)
		if title := strings.TrimSpace(titles[g]); title != "" {
			b.WriteString("  (")
			b.WriteString(title)
			b.WriteString(")")
		}
		b.WriteString(" (")
		b.WriteString(intToString(len(index[g])))
		b.WriteString(" actions)\n")
	}
	return b.String()
}

func listGroupActions(group string, groupTitle string, actions map[string][]apiActionOp) string {
	if len(actions) == 0 {
		return "No actions found.\n"
	}
	var b strings.Builder
	b.WriteString("Group: ")
	b.WriteString(group)
	if strings.TrimSpace(groupTitle) != "" {
		b.WriteString(" (")
		b.WriteString(strings.TrimSpace(groupTitle))
		b.WriteString(")")
	}
	b.WriteString("\nActions:\n")
	keys := make([]string, 0, len(actions))
	for a := range actions {
		keys = append(keys, a)
	}
	sort.Strings(keys)
	for _, a := range keys {
		ops := actions[a]
		if len(ops) == 0 {
			continue
		}
		op := ops[0]
		b.WriteString("  - ")
		actionName := strings.TrimSpace(op.Cmd.Action)
		if actionName == "" {
			actionName = a
		}
		b.WriteString(actionName)
		b.WriteString("  ")
		b.WriteString(strings.ToUpper(strings.TrimSpace(op.Cmd.Method)))
		b.WriteString(" ")
		b.WriteString(strings.TrimSpace(op.Cmd.Path))
		if s := strings.TrimSpace(op.Cmd.Summary); s != "" {
			b.WriteString("  # ")
			b.WriteString(s)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func groupTitleFromActions(actions map[string][]apiActionOp) string {
	for _, ops := range actions {
		for _, op := range ops {
			if v := strings.TrimSpace(op.Cmd.GroupTitle); v != "" {
				return v
			}
		}
	}
	return ""
}

func parseGeneratedCallArgs(args []string, ops []apiActionOp) (string, string, map[string]string, map[string]string, []byte, requestFormat, error) {
	if len(ops) == 0 {
		return "", "", nil, nil, nil, "", errors.New("no matched operation")
	}
	selected := ops[0]
	method := strings.ToUpper(strings.TrimSpace(selected.Cmd.Method))
	if method == "" {
		method = "GET"
	}
	path := strings.TrimSpace(selected.Cmd.Path)
	query := map[string]string{}
	header := map[string]string{}
	pathValues := map[string]string{}
	bodyArg := ""
	reqFormat := requestFormatJSON

	for len(args) > 0 {
		a := args[0]
		switch a {
		case "--method":
			if len(args) < 2 {
				return "", "", nil, nil, nil, "", errors.New("missing --method value")
			}
			m := strings.ToUpper(strings.TrimSpace(args[1]))
			if m == "" {
				return "", "", nil, nil, nil, "", errors.New("empty --method")
			}
			matched, ok := selectOpByMethod(ops, m)
			if !ok {
				return "", "", nil, nil, nil, "", errors.New("method not supported for action: " + m)
			}
			selected = matched
			method = m
			path = strings.TrimSpace(selected.Cmd.Path)
			args = args[2:]
		case "--request":
			if len(args) < 2 {
				return "", "", nil, nil, nil, "", errors.New("missing --request value")
			}
			bodyArg = args[1]
			args = args[2:]
		case "--request-format":
			if len(args) < 2 {
				return "", "", nil, nil, nil, "", errors.New("missing --request-format value")
			}
			reqFormat = normalizeRequestFormat(requestFormat(args[1]))
			args = args[2:]
		case "--query":
			if len(args) < 2 {
				return "", "", nil, nil, nil, "", errors.New("missing --query value")
			}
			k, v, ok := strings.Cut(args[1], "=")
			if !ok {
				return "", "", nil, nil, nil, "", errors.New("invalid --query, expected k=v")
			}
			query[strings.TrimSpace(k)] = v
			args = args[2:]
		case "--header":
			if len(args) < 2 {
				return "", "", nil, nil, nil, "", errors.New("missing --header value")
			}
			k, v, ok := strings.Cut(args[1], "=")
			if !ok {
				return "", "", nil, nil, nil, "", errors.New("invalid --header, expected k=v")
			}
			header[strings.TrimSpace(k)] = v
			args = args[2:]
		default:
			if !strings.HasPrefix(a, "--") {
				return "", "", nil, nil, nil, "", errors.New("unknown arg: " + a)
			}
			p, ok := selected.ParamFlags[a]
			if !ok {
				return "", "", nil, nil, nil, "", errors.New("unknown flag: " + a)
			}
			if len(args) < 2 {
				return "", "", nil, nil, nil, "", errors.New("missing value for " + a)
			}
			v := args[1]
			loc := strings.ToLower(strings.TrimSpace(p.In))
			switch loc {
			case "query":
				query[p.Name] = v
			case "path":
				pathValues[p.Name] = v
			default:
				return "", "", nil, nil, nil, "", errors.New("unsupported generated flag location: " + p.In)
			}
			args = args[2:]
		}
	}

	for _, p := range selected.Cmd.Params {
		if !p.Required {
			continue
		}
		loc := strings.ToLower(strings.TrimSpace(p.In))
		switch loc {
		case "body":
			if strings.TrimSpace(bodyArg) == "" {
				return "", "", nil, nil, nil, "", errors.New("missing required body: --request")
			}
		case "query":
			if strings.TrimSpace(query[p.Name]) == "" {
				return "", "", nil, nil, nil, "", errors.New("missing required query: " + p.Name)
			}
		case "path":
			if strings.TrimSpace(pathValues[p.Name]) == "" {
				return "", "", nil, nil, nil, "", errors.New("missing required path param: " + p.Name)
			}
		}
	}

	for k, v := range pathValues {
		path = strings.ReplaceAll(path, "{"+k+"}", v)
	}
	if strings.Contains(path, "{") && strings.Contains(path, "}") {
		return "", "", nil, nil, nil, "", errors.New("path still contains unresolved params")
	}

	body, err := util.ReadMaybeFile(bodyArg)
	if err != nil {
		return "", "", nil, nil, nil, "", err
	}
	if len(body) == 0 {
		body = []byte("{}")
	}
	return method, path, query, header, body, reqFormat, nil
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

func intToString(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	n := v
	for n > 0 {
		i--
		buf[i] = byte('0' + (n % 10))
		n /= 10
	}
	return string(buf[i:])
}
