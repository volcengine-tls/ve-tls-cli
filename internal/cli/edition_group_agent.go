//go:build agent

package cli

func runEditionSpecificGroup(_ *Context, _ string, _ []string) (any, bool, error) {
	return nil, false, nil
}
