package execution

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"unicode"

	"github.com/volcengine-tls/ve-tls-cli/internal/jsonx"
)

// UnparseableBodyPreview replaces bodies that cannot be safely inspected.
const UnparseableBodyPreview = "[unparseable body omitted]"

// UnparseableQueryPreview replaces JSON-like query values that cannot be safely inspected.
const UnparseableQueryPreview = "[unparseable query omitted]"

const unparseableURLPreview = "[unparseable URL omitted]"

// RedactRequestPreview clones a request preview and masks known sensitive fields.
// JSON-encoded query objects and arrays are inspected without changing ordinary
// strings or the request used by the transport.
func RedactRequestPreview(preview map[string]any) (map[string]any, []string) {
	if preview == nil {
		return nil, nil
	}
	var fields []string
	cloned := redactPreviewValue(preview, "", false, false, &fields).(map[string]any)
	sort.Strings(fields)
	return cloned, fields
}

func redactPreviewValue(value any, path string, query, keyValue bool, fields *[]string) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		redactValue := sensitiveKeyValueField(typed, keyValue)
		for key, item := range typed {
			childPath := previewFieldPath(path, key)
			if sensitivePreviewKey(key) || redactValue && normalizePreviewKey(key) == "value" {
				out[key] = "***"
				*fields = append(*fields, childPath)
				continue
			}
			childQuery := query || path == "" && key == "query"
			childKeyValue := isKeyValueCollection(key)
			out[key] = redactPreviewValue(item, childPath, childQuery, childKeyValue, fields)
		}
		return out
	case map[string]string:
		out := make(map[string]string, len(typed))
		redactValue := false
		if keyValue {
			for key, item := range typed {
				if normalizePreviewKey(key) == "key" && sensitivePreviewKey(item) {
					redactValue = true
				}
			}
		}
		for key, item := range typed {
			childPath := previewFieldPath(path, key)
			if sensitivePreviewKey(key) || redactValue && normalizePreviewKey(key) == "value" {
				out[key] = "***"
				*fields = append(*fields, childPath)
				continue
			}
			out[key] = redactPreviewValue(item, childPath, query, false, fields).(string)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = redactPreviewValue(item, fmt.Sprintf("%s[%d]", path, i), query, keyValue, fields)
		}
		return out
	case []map[string]any:
		out := make([]map[string]any, len(typed))
		for i, item := range typed {
			out[i] = redactPreviewValue(item, fmt.Sprintf("%s[%d]", path, i), query, keyValue, fields).(map[string]any)
		}
		return out
	case []map[string]string:
		out := make([]map[string]string, len(typed))
		for i, item := range typed {
			out[i] = redactPreviewValue(item, fmt.Sprintf("%s[%d]", path, i), query, keyValue, fields).(map[string]string)
		}
		return out
	case []string:
		out := make([]string, len(typed))
		for i, item := range typed {
			out[i] = redactPreviewValue(item, fmt.Sprintf("%s[%d]", path, i), query, false, fields).(string)
		}
		return out
	case string:
		if !query {
			return typed
		}
		trimmed := strings.TrimSpace(typed)
		if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
			return typed
		}
		var decoded any
		if jsonx.Unmarshal([]byte(trimmed), &decoded) != nil {
			*fields = append(*fields, path)
			return UnparseableQueryPreview
		}
		switch decoded.(type) {
		case map[string]any, []any:
		default:
			return typed
		}
		before := len(*fields)
		redacted := redactPreviewValue(decoded, path, true, false, fields)
		if len(*fields) == before {
			return typed
		}
		encoded, err := json.Marshal(redacted)
		if err != nil {
			return "***"
		}
		return string(encoded)
	default:
		return value
	}
}

func sensitiveKeyValueField(item map[string]any, keyValue bool) bool {
	if !keyValue {
		return false
	}
	for key, value := range item {
		if normalizePreviewKey(key) == "key" {
			name, ok := value.(string)
			if ok && sensitivePreviewKey(name) {
				return true
			}
		}
	}
	return false
}

func isKeyValueCollection(key string) bool {
	switch normalizePreviewKey(key) {
	case "contents", "logtags":
		return true
	default:
		return false
	}
}

func normalizePreviewKey(key string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, key)
}

func sensitivePreviewKey(key string) bool {
	normalized := normalizePreviewKey(key)
	if normalized == "ak" || normalized == "sk" {
		return true
	}
	for _, term := range []string{"password", "passwd", "secret", "token", "accesskey", "authorization", "credential", "privatekey"} {
		if strings.Contains(normalized, term) {
			return true
		}
	}
	return false
}

func previewFieldPath(parent, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}

// RedactPreviewURL removes information that should not appear in endpoint
// diagnostics. Invalid URLs are replaced with a fixed safe placeholder.
func RedactPreviewURL(value string) (string, bool) {
	parsed, err := url.Parse(value)
	if err != nil {
		return unparseableURLPreview, true
	}
	// Without a scheme, credentials can be parsed as an opaque URL or as a
	// relative path instead of User. Do not echo an ambiguous authority.
	if parsed.User == nil && (strings.Contains(parsed.Opaque, "@") ||
		parsed.Scheme == "" && parsed.Host == "" && strings.Contains(strings.SplitN(parsed.Path, "/", 2)[0], "@")) {
		return unparseableURLPreview, true
	}
	if parsed.User == nil && parsed.RawQuery == "" && !parsed.ForceQuery && !strings.Contains(value, "#") {
		return value, false
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	parsed.RawFragment = ""
	return parsed.String(), true
}

// RedactPreviewPath preserves ordinary query parameters while masking known
// sensitive query values. It never changes the request's transport path.
func RedactPreviewPath(value string) (string, []string) {
	parsed, err := url.Parse(value)
	if err != nil {
		return unparseableURLPreview, []string{"path"}
	}
	var fields []string
	if parsed.User != nil {
		parsed.User = nil
		fields = append(fields, "path.userinfo")
	}
	if strings.Contains(value, "#") {
		parsed.Fragment = ""
		parsed.RawFragment = ""
		fields = append(fields, "path.fragment")
	}
	if parsed.RawQuery != "" {
		values, err := url.ParseQuery(parsed.RawQuery)
		if err != nil {
			return unparseableURLPreview, []string{"path"}
		}
		query := make(map[string]any, len(values))
		for key, items := range values {
			if len(items) == 1 {
				query[key] = items[0]
			} else {
				query[key] = items
			}
		}
		before := len(fields)
		redacted := redactPreviewValue(query, "path.query", true, false, &fields).(map[string]any)
		if len(fields) != before {
			for key, item := range redacted {
				switch typed := item.(type) {
				case string:
					values[key] = []string{typed}
				case []string:
					values[key] = typed
				}
			}
			parsed.RawQuery = values.Encode()
		}
	}
	if len(fields) == 0 {
		return value, nil
	}
	sort.Strings(fields)
	return parsed.String(), fields
}
