package cli

import (
	"errors"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/util"
)

func runIndex(ctx *Context, args []string) (any, error) {
	return runSubcommandGroup(args, usageIndex(), nil, shortcutCommandHelpLookup("index"), func(command string, commandArgs []string) (any, error) {
		ctx.Action = "index." + strings.TrimSpace(command)
		if out, handled, err := maybeHandleShortcutMeta("index", command, commandArgs); handled {
			return out, err
		}
		switch command {
		case "get":
			return indexGet(ctx, commandArgs)
		case "create":
			return indexCreate(ctx, commandArgs)
		case "modify":
			return indexModify(ctx, commandArgs)
		default:
			return nil, errors.New("unknown index command: " + command)
		}
	})
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
		case "--request":
			if len(args) < 2 {
				return nil, errors.New("missing --request value")
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
	if err := validateIndexBody(path, m); err != nil {
		return nil, err
	}
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
