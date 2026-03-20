package cli

import (
	"errors"
	"strings"

	"volclog/internal/util"
)

func runAPI(ctx *Context, args []string) (any, error) {
	if len(args) == 0 {
		return nil, &usageError{Text: usageAPI(), ExitCode: 1}
	}
	if args[0] == "-h" || args[0] == "--help" {
		return nil, &usageError{Text: usageAPI(), ExitCode: 0}
	}
	if hasHelp(args[1:]) {
		return nil, &usageError{Text: usageAPI(), ExitCode: 0}
	}
	switch args[0] {
	case "call":
		return apiCall(ctx, args[1:])
	default:
		return nil, errors.New("unknown api command: " + args[0])
	}
}

func apiCall(ctx *Context, args []string) (any, error) {
	method := "GET"
	path := ""
	query := map[string]string{}
	header := map[string]string{}
	bodyArg := ""
	for len(args) > 0 {
		switch args[0] {
		case "--method":
			if len(args) < 2 {
				return nil, errors.New("missing --method value")
			}
			method = strings.ToUpper(args[1])
			args = args[2:]
		case "--path":
			if len(args) < 2 {
				return nil, errors.New("missing --path value")
			}
			path = args[1]
			args = args[2:]
		case "--query":
			if len(args) < 2 {
				return nil, errors.New("missing --query value")
			}
			k, v, ok := strings.Cut(args[1], "=")
			if !ok {
				return nil, errors.New("invalid --query, expected k=v")
			}
			query[k] = v
			args = args[2:]
		case "--header":
			if len(args) < 2 {
				return nil, errors.New("missing --header value")
			}
			k, v, ok := strings.Cut(args[1], "=")
			if !ok {
				return nil, errors.New("invalid --header, expected k=v")
			}
			header[k] = v
			args = args[2:]
		case "--body":
			if len(args) < 2 {
				return nil, errors.New("missing --body value")
			}
			bodyArg = args[1]
			args = args[2:]
		default:
			return nil, errors.New("unknown flag: " + args[0])
		}
	}
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("missing --path")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	body, err := util.ReadMaybeFile(bodyArg)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		body = []byte("{}")
	}
	return ctx.Do(method, path, query, header, body)
}
