//go:build human

package cli

import (
	"strings"
)

func readJSONObjectRequestArg(requestArg string, invocation ...*Context) (map[string]any, error) {
	if strings.TrimSpace(requestArg) == "" {
		return map[string]any{}, nil
	}
	return inputInvocation(invocation).readJSONObjectMaybeFile(requestArg)
}

func maybeSetStringField(dst map[string]any, key string, value string) {
	if strings.TrimSpace(value) != "" {
		dst[key] = strings.TrimSpace(value)
	}
}

func maybeSetIntField(dst map[string]any, key string, value int) {
	if value > 0 {
		dst[key] = value
	}
}

func maybeSetBoolField(dst map[string]any, key string, set bool, value bool) {
	if set {
		dst[key] = value
	}
}
