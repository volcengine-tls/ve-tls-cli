package cli

import (
	"errors"
	"strings"

	"volclog/internal/util"
)

func runIndex(ctx *Context, args []string) (any, error) {
	if len(args) == 0 {
		return nil, &usageError{Text: usageIndex(), ExitCode: 1}
	}
	if args[0] == "-h" || args[0] == "--help" {
		return nil, &usageError{Text: usageIndex(), ExitCode: 0}
	}
	if hasHelp(args[1:]) {
		return nil, &usageError{Text: usageIndex(), ExitCode: 0}
	}
	switch args[0] {
	case "get":
		return indexGet(ctx, args[1:])
	case "create":
		return indexCreate(ctx, args[1:])
	case "modify":
		return indexModify(ctx, args[1:])
	default:
		return nil, errors.New("unknown index command: " + args[0])
	}
}

func indexGet(ctx *Context, args []string) (any, error) {
	var topicID string
	for len(args) > 0 {
		switch args[0] {
		case "--topic-id":
			if len(args) < 2 {
				return nil, errors.New("missing --topic-id value")
			}
			topicID = args[1]
			args = args[2:]
		default:
			return nil, errors.New("unknown flag: " + args[0])
		}
	}
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return nil, errors.New("missing --topic-id")
	}
	body, _ := util.MustJSON(map[string]any{})
	return ctx.Do("GET", "/DescribeIndex", map[string]string{"TopicId": topicID}, nil, body)
}

func indexCreate(ctx *Context, args []string) (any, error) {
	return indexUpsert(ctx, "/CreateIndex", args)
}

func indexModify(ctx *Context, args []string) (any, error) {
	return indexUpsert(ctx, "/ModifyIndex", args)
}

func indexUpsert(ctx *Context, path string, args []string) (any, error) {
	var (
		topicID string
		bodyArg string
	)
	for len(args) > 0 {
		switch args[0] {
		case "--topic-id":
			if len(args) < 2 {
				return nil, errors.New("missing --topic-id value")
			}
			topicID = args[1]
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
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return nil, errors.New("missing --topic-id")
	}
	raw, err := util.ReadMaybeFile(bodyArg)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, errors.New("missing --body")
	}
	payload, err := util.UnmarshalJSON(raw)
	if err != nil {
		return nil, err
	}
	m, ok := payload.(map[string]any)
	if !ok {
		return nil, errors.New("index body must be JSON object")
	}
	m["TopicId"] = topicID
	body, err := util.MustJSON(m)
	if err != nil {
		return nil, err
	}
	method := "POST"
	if path == "/ModifyIndex" {
		method = "PUT"
	}
	return ctx.Do(method, path, nil, nil, body)
}
