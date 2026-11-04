package cli

import (
	"errors"
	"io"
	"os"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/jsonx"
	"github.com/volcengine-tls/ve-tls-cli/internal/util"
)

func inputInvocation(contexts []*Context) *Context {
	if len(contexts) > 0 && contexts[0] != nil {
		return contexts[0]
	}
	return &Context{}
}

func (c *Context) readMaybeFile(value string) ([]byte, error) {
	if err := c.ExecutionContext().Err(); err != nil {
		return nil, validationFailure(err, "InputCancelled")
	}
	if strings.TrimSpace(value) != "-" {
		return util.ReadMaybeFile(value)
	}
	reader, err := newContextFilePromptReader(c.ExecutionContext(), os.Stdin, "provide file:// input when cancellable stdin reads are unavailable")
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(reader)
	if ctxErr := c.ExecutionContext().Err(); ctxErr != nil {
		return nil, validationFailure(ctxErr, "InputCancelled")
	}
	return data, err
}

func (c *Context) readJSONValueMaybeFile(value string) (any, error) {
	data, err := c.readMaybeFile(value)
	if err != nil {
		return nil, err
	}
	return util.UnmarshalJSON(data)
}

func (c *Context) readJSONObjectMaybeFile(value string) (map[string]any, error) {
	v, err := c.readJSONValueMaybeFile(value)
	if err != nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("json must be object")
	}
	return m, nil
}

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
