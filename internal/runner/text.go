package runner

import (
	"errors"
	"strings"
	"unicode"
)

func ParseTextArgs(s string) (map[string]string, error) {
	v := strings.TrimSpace(s)
	if v == "" {
		return map[string]string{}, nil
	}
	toks, err := splitFieldsQuoted(v)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, tok := range toks {
		i := strings.IndexByte(tok, '=')
		if i <= 0 {
			continue
		}
		k := strings.TrimSpace(tok[:i])
		val := strings.TrimSpace(tok[i+1:])
		if k == "" || val == "" {
			continue
		}
		if !isKey(k) {
			continue
		}
		out[k] = trimQuotes(val)
	}
	return out, nil
}

func splitFieldsQuoted(s string) ([]string, error) {
	var toks []string
	var cur strings.Builder
	var quote rune
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		toks = append(toks, cur.String())
		cur.Reset()
	}
	for _, r := range s {
		if quote != 0 {
			cur.WriteRune(r)
			if r == quote {
				quote = 0
			}
			continue
		}
		switch {
		case r == '"' || r == '\'':
			quote = r
			cur.WriteRune(r)
		case unicode.IsSpace(r):
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	if quote != 0 {
		return nil, errors.New("unterminated quote")
	}
	flush()
	return toks, nil
}

func isKey(s string) bool {
	for i, r := range s {
		if i == 0 {
			if !(unicode.IsLetter(r) || r == '_') {
				return false
			}
			continue
		}
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func trimQuotes(s string) string {
	v := strings.TrimSpace(s)
	if len(v) < 2 {
		return v
	}
	if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
		return v[1 : len(v)-1]
	}
	return v
}
