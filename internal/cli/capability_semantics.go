package cli

import (
	"sort"
	"strings"
)

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
