package readonly

import "strings"

var tlsReadOnlyExplicitActions = map[string]struct{}{
	"ConsumeLogs":               {},
	"PreviewDelimiterLog":       {},
	"SearchLogs":                {},
	"Statistics":                {},
	"CreateDownloadTask":        {},
	"CancelDownloadTask":        {},
	"CreateAppSceneMeta":        {},
	"CreateAppInstance":         {},
	"DeleteAppSceneMeta":        {},
	"ModifyAppSceneMeta":        {},
	"CreateSavedSearch":         {},
	"DescribeSavedSearches":     {},
	"DeleteSavedSearch":         {},
	"CreateArchiveSearchTask":   {},
	"DeleteArchiveSearchTask":   {},
	"ArchiveSearch":             {},
	"DisplayResourceByTemplate": {},
}

// IsTLSReadOnlyAction reports whether the TLS IAM action belongs to the
// read-only surface that default volclog should expose to agents.
func IsTLSReadOnlyAction(action string) bool {
	name := strings.TrimSpace(action)
	if name == "" {
		return false
	}
	if strings.HasPrefix(name, "Describe") || strings.HasPrefix(name, "Get") {
		return true
	}
	_, ok := tlsReadOnlyExplicitActions[name]
	return ok
}
