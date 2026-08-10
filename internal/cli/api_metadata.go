package cli

import "strings"

type apiCapParam struct {
	Name         string
	CLIFlag      string
	In           string
	Required     bool
	RequiredText string
	Type         string
	Format       string
	Ref          string
	Description  string
	Example      string
	Enum         []string
	Pattern      string
	Minimum      *float64
	Maximum      *float64
	MinLength    *int
	MaxLength    *int
}

type apiCapDocParam struct {
	Name         string
	In           string
	Type         string
	RequiredText string
	Example      string
	Description  string
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
