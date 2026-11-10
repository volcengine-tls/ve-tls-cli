package cli

import (
	"errors"
	"io"
	"os"
	"strings"

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
