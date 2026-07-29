//go:build human

package cli

import "strings"

func isDocRequired(v string) bool {
	s := strings.ToLower(strings.TrimSpace(v))
	return s == "是" || s == "true" || s == "required" || s == "yes"
}
