package auth

import (
	"fmt"
	"strings"
	"unicode"
)

type ErrorKind string

const maxSafeFieldRunes = 256

const (
	ConfigInvalid  ErrorKind = "config_invalid"
	CacheMissing   ErrorKind = "cache_missing"
	CacheCorrupt   ErrorKind = "cache_corrupt"
	ReauthRequired ErrorKind = "reauth_required"
	ProtocolError  ErrorKind = "protocol_error"
)

// Error intentionally stores no raw response body. Cause remains available to
// errors.Is/errors.As, but Error never renders it because it may contain secrets.
// Cause is tagged json:"-" so it can never leak through json.Marshal.
type Error struct {
	Kind        ErrorKind
	Status      int
	RequestID   string
	ServiceCode string
	Description string
	Cause       error `json:"-"`
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	parts := []string{"kind=" + string(e.Kind)}
	if e.Status != 0 {
		parts = append(parts, fmt.Sprintf("status=%d", e.Status))
	}
	if requestID := sanitize(e.RequestID); requestID != "" {
		parts = append(parts, "request_id="+requestID)
	}
	if serviceCode := sanitize(e.ServiceCode); serviceCode != "" {
		parts = append(parts, "service_code="+serviceCode)
	}
	if description := sanitize(e.Description); description != "" {
		parts = append(parts, "description="+description)
	}
	return strings.Join(parts, " ")
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *Error) Is(target error) bool {
	other, ok := target.(*Error)
	return ok && other != nil && other.Kind != "" && e != nil && e.Kind == other.Kind
}

func sanitize(value string) string {
	safe := make([]rune, 0, min(len(value), maxSafeFieldRunes))
	pendingSpace := false
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.Is(unicode.C, r) {
			pendingSpace = len(safe) > 0
			continue
		}
		if pendingSpace && len(safe) < maxSafeFieldRunes {
			safe = append(safe, ' ')
		}
		pendingSpace = false
		if len(safe) >= maxSafeFieldRunes {
			break
		}
		safe = append(safe, r)
	}
	return string(safe)
}
