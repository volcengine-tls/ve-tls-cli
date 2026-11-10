//go:build human

package cli

import (
	"errors"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/jsonx"
)

func (c *Context) readJSONArrayMaybeFile(value string) ([]any, error) {
	v, err := c.readJSONValueMaybeFile(value)
	if err != nil {
		return nil, err
	}
	a, ok := v.([]any)
	if !ok {
		return nil, errors.New("json must be array")
	}
	return a, nil
}

func (c *Context) readStringMaybeFile(value string) (string, error) {
	data, err := c.readMaybeFile(value)
	return strings.TrimSpace(string(data)), err
}

func (c *Context) readStringListMaybeFile(value string) ([]string, error) {
	data, err := c.readMaybeFile(value)
	if err != nil {
		return nil, err
	}
	data = []byte(strings.TrimSpace(string(data)))
	if len(data) == 0 {
		return []string{}, nil
	}
	var values []string
	if data[0] == '[' {
		var decoded []any
		if err := jsonx.Unmarshal(data, &decoded); err != nil {
			return nil, err
		}
		for _, item := range decoded {
			s, ok := item.(string)
			if !ok {
				return nil, errors.New("json array must contain strings")
			}
			values = append(values, s)
		}
	} else {
		values = strings.Split(string(data), "\n")
	}
	out := []string{}
	for _, s := range values {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out, nil
}
