package cli

import (
	"errors"
	"strings"
	"unicode/utf8"
)

type apiDescribeGuidance struct {
	ListGroup           string   `json:"list_group"`
	Describe            string   `json:"describe"`
	DryRun              string   `json:"dry_run,omitempty"`
	Execute             string   `json:"execute,omitempty"`
	Filter              string   `json:"filter,omitempty"`
	ShortcutFirst       []string `json:"shortcut_first,omitempty"`
	FallbackDiscovery   string   `json:"fallback_discovery,omitempty"`
	FallbackAPIDescribe string   `json:"fallback_api_describe,omitempty"`
}

func paramDocKey(in string, name string) string {
	return strings.ToLower(strings.TrimSpace(in)) + "\x00" + strings.TrimSpace(name)
}

func mergeParamsWithDoc(params []apiCapParam, doc []apiCapDocParam) []apiCapParam {
	if len(params) == 0 {
		return nil
	}
	docByKey := make(map[string]apiCapDocParam, len(doc))
	for _, item := range doc {
		docByKey[paramDocKey(item.In, item.Name)] = item
	}
	out := make([]apiCapParam, 0, len(params))
	for _, param := range params {
		cp := param
		if item, ok := docByKey[paramDocKey(param.In, param.Name)]; ok {
			if s := strings.TrimSpace(item.In); s != "" {
				cp.In = strings.ToLower(s)
			}
			if s := strings.TrimSpace(item.Type); s != "" && strings.TrimSpace(cp.Type) == "" {
				cp.Type = s
			}
			if s := strings.TrimSpace(item.RequiredText); s != "" && strings.TrimSpace(cp.RequiredText) == "" {
				cp.RequiredText = s
				if !cp.Required && requiredFromText(s) {
					cp.Required = true
				}
			}
			if s := conciseFieldDescription(item.Description); s != "" {
				cp.Description = chooseShorterDescription(cp.Description, s)
			}
			if s := strings.TrimSpace(item.Example); s != "" {
				cp.Example = s
			}
		}
		out = append(out, cp)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func requiredFromText(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "是", "yes", "y", "required", "true":
		return true
	default:
		return false
	}
}

func sanitizeRequestParamsDocForOutput(params []apiCapDocParam) []apiCapDocParam {
	if len(params) == 0 {
		return nil
	}
	out := make([]apiCapDocParam, 0, len(params))
	for _, p := range params {
		in := strings.ToLower(strings.TrimSpace(p.In))
		if in != "body" && in != "query" && in != "path" && in != "header" {
			continue
		}
		cp := p
		cp.In = in
		out = append(out, cp)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func splitRequestParamsDocForOutput(params []apiCapDocParam) ([]apiCapDocParam, []apiCapDocParam) {
	sanitized := sanitizeRequestParamsDocForOutput(params)
	if len(sanitized) == 0 {
		return nil, nil
	}
	body := make([]apiCapDocParam, 0, len(sanitized))
	flags := make([]apiCapDocParam, 0, len(sanitized))
	for _, param := range sanitized {
		switch strings.ToLower(strings.TrimSpace(param.In)) {
		case "body":
			body = append(body, param)
		case "query", "path", "header":
			flags = append(flags, param)
		}
	}
	if len(body) == 0 {
		body = nil
	}
	if len(flags) == 0 {
		flags = nil
	}
	return body, flags
}

func chooseShorterDescription(current string, candidate string) string {
	current = conciseFieldDescription(current)
	candidate = conciseFieldDescription(candidate)
	if current == "" {
		return candidate
	}
	if candidate == "" {
		return current
	}
	if utf8.RuneCountInString(candidate) < utf8.RuneCountInString(current) {
		return candidate
	}
	return current
}

func conciseFieldDescription(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		"\r", " ",
		"\n", " ",
		"\t", " ",
		"&nbsp;", " ",
		"```", " ",
		"`", "",
	)
	s = replacer.Replace(s)
	s = strings.Join(strings.Fields(s), " ")
	for _, marker := range []string{
		":::",
		"详细说明请参考",
		"详细说明请参见",
		"详细说明请见",
		"命名规则请参考",
		"设置规则请参考",
		"满足如下任一条件时",
		"标签用于云资源的标识与分类",
	} {
		if idx := strings.Index(s, marker); idx > 0 {
			s = strings.TrimSpace(s[:idx])
		}
	}
	if idx := strings.Index(s, " * "); idx > 0 {
		s = strings.TrimSpace(s[:idx])
	}
	if utf8.RuneCountInString(s) > 72 {
		for idx, r := range s {
			if strings.ContainsRune("。!?；;", r) {
				s = strings.TrimSpace(s[:idx+utf8.RuneLen(r)])
				break
			}
		}
	}
	return strings.TrimSpace(s)
}

func shortcutActionOps(group, action string) ([]apiActionOp, error) {
	doc, err := loadAPICapabilities()
	if err != nil {
		return nil, err
	}
	index := buildAPIIndex(doc)
	actions, ok := index[normalizeToken(group)]
	if !ok {
		return nil, errors.New("api group not found: " + group)
	}
	ops, ok := actions[normalizeActionToken(action)]
	if !ok || len(ops) == 0 {
		return nil, errors.New("api action not found: " + group + "." + action)
	}
	return ops, nil
}
