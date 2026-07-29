//go:build human

package cli

import (
	"errors"
	"strconv"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/util"
)

func runCollector(ctx *Context, args []string) (any, error) {
	return runSubcommandGroup(args, usageCollector(), nil, shortcutCommandHelpLookup("collector"), func(command string, commandArgs []string) (any, error) {
		ctx.Action = "collector." + strings.TrimSpace(command)
		if out, handled, err := maybeHandleShortcutMeta("collector", command, commandArgs); handled {
			return out, err
		}
		switch command {
		case "list":
			return collectorList(ctx, commandArgs)
		case "get":
			return collectorGet(ctx, commandArgs)
		case "create":
			return collectorCreate(ctx, commandArgs)
		case "modify":
			return collectorModify(ctx, commandArgs)
		case "delete":
			return collectorDelete(ctx, commandArgs)
		default:
			return nil, errors.New("unknown collector command: " + command)
		}
	})
}

func collectorList(ctx *Context, args []string) (any, error) {
	args, all := extractBoolFlag(args, "--all")
	query := map[string]string{}
	for len(args) > 0 {
		switch args[0] {
		case "--project-id":
			if len(args) < 2 {
				return nil, errors.New("missing --project-id value")
			}
			query["ProjectId"] = args[1]
			args = args[2:]
		case "--project-name":
			if len(args) < 2 {
				return nil, errors.New("missing --project-name value")
			}
			query["ProjectName"] = args[1]
			args = args[2:]
		case "--iam-project-name":
			if len(args) < 2 {
				return nil, errors.New("missing --iam-project-name value")
			}
			query["IamProjectName"] = args[1]
			args = args[2:]
		case "--rule-id":
			if len(args) < 2 {
				return nil, errors.New("missing --rule-id value")
			}
			query["RuleId"] = args[1]
			args = args[2:]
		case "--rule-name":
			if len(args) < 2 {
				return nil, errors.New("missing --rule-name value")
			}
			query["RuleName"] = args[1]
			args = args[2:]
		case "--topic-id":
			if len(args) < 2 {
				return nil, errors.New("missing --topic-id value")
			}
			query["TopicId"] = args[1]
			args = args[2:]
		case "--topic-name":
			if len(args) < 2 {
				return nil, errors.New("missing --topic-name value")
			}
			query["TopicName"] = args[1]
			args = args[2:]
		case "--log-type":
			if len(args) < 2 {
				return nil, errors.New("missing --log-type value")
			}
			query["LogType"] = args[1]
			args = args[2:]
		case "--rule-type":
			if len(args) < 2 {
				return nil, errors.New("missing --rule-type value")
			}
			query["RuleType"] = args[1]
			args = args[2:]
		case "--page-number":
			if len(args) < 2 {
				return nil, errors.New("missing --page-number value")
			}
			query["PageNumber"] = args[1]
			args = args[2:]
		case "--page-size":
			if len(args) < 2 {
				return nil, errors.New("missing --page-size value")
			}
			query["PageSize"] = args[1]
			args = args[2:]
		case "--pause":
			query["Pause"] = "1"
			args = args[1:]
		case "--no-pause":
			query["Pause"] = "0"
			args = args[1:]
		case "--hidden":
			query["Hidden"] = "true"
			args = args[1:]
		case "--no-hidden":
			query["Hidden"] = "false"
			args = args[1:]
		default:
			return nil, errors.New("unknown flag: " + args[0])
		}
	}
	if all {
		return listAllByPageNumber(ctx, "/DescribeRulesV2", query, "Rules")
	}
	body, _ := util.MustJSON(map[string]any{})
	return ctx.Do("GET", "/DescribeRulesV2", query, nil, body)
}

func collectorGet(ctx *Context, args []string) (any, error) {
	var ruleID string
	for len(args) > 0 {
		switch args[0] {
		case "--rule-id":
			if len(args) < 2 {
				return nil, errors.New("missing --rule-id value")
			}
			ruleID = args[1]
			args = args[2:]
		default:
			return nil, errors.New("unknown flag: " + args[0])
		}
	}
	ruleID = strings.TrimSpace(ruleID)
	if ruleID == "" {
		return nil, errors.New("missing --rule-id")
	}
	body, _ := util.MustJSON(map[string]any{})
	return ctx.Do("GET", "/DescribeRuleV2", map[string]string{"RuleId": ruleID}, nil, body)
}

func collectorCreate(ctx *Context, args []string) (any, error) {
	req, err := buildCollectorBody(args, false)
	if err != nil {
		return nil, err
	}
	body, err := util.MustJSON(req)
	if err != nil {
		return nil, err
	}
	return ctx.Do("POST", "/CreateRule", nil, nil, body)
}

func collectorModify(ctx *Context, args []string) (any, error) {
	req, err := buildCollectorBody(args, true)
	if err != nil {
		return nil, err
	}
	body, err := util.MustJSON(req)
	if err != nil {
		return nil, err
	}
	return ctx.Do("PUT", "/ModifyRule", nil, nil, body)
}

func collectorDelete(ctx *Context, args []string) (any, error) {
	var ruleID string
	for len(args) > 0 {
		switch args[0] {
		case "--rule-id":
			if len(args) < 2 {
				return nil, errors.New("missing --rule-id value")
			}
			ruleID = args[1]
			args = args[2:]
		default:
			return nil, errors.New("unknown flag: " + args[0])
		}
	}
	ruleID = strings.TrimSpace(ruleID)
	if ruleID == "" {
		return nil, errors.New("missing --rule-id")
	}
	body, err := util.MustJSON(map[string]any{"RuleId": ruleID})
	if err != nil {
		return nil, err
	}
	return ctx.Do("DELETE", "/DeleteRule", nil, nil, body)
}

func buildCollectorBody(args []string, modify bool) (map[string]any, error) {
	var (
		ruleID       string
		topicID      string
		ruleName     string
		logType      string
		pathsArg     string
		requestArg   string
		inputType    int
		inputTypeSet bool
		pause        int
		pauseSet     bool
	)
	for len(args) > 0 {
		switch args[0] {
		case "--rule-id":
			if len(args) < 2 {
				return nil, errors.New("missing --rule-id value")
			}
			ruleID = args[1]
			args = args[2:]
		case "--topic-id":
			if len(args) < 2 {
				return nil, errors.New("missing --topic-id value")
			}
			topicID = args[1]
			args = args[2:]
		case "--rule-name":
			if len(args) < 2 {
				return nil, errors.New("missing --rule-name value")
			}
			ruleName = args[1]
			args = args[2:]
		case "--log-type":
			if len(args) < 2 {
				return nil, errors.New("missing --log-type value")
			}
			logType = args[1]
			args = args[2:]
		case "--paths":
			if len(args) < 2 {
				return nil, errors.New("missing --paths value")
			}
			pathsArg = args[1]
			args = args[2:]
		case "--input-type":
			if len(args) < 2 {
				return nil, errors.New("missing --input-type value")
			}
			v, err := strconv.Atoi(args[1])
			if err != nil {
				return nil, err
			}
			inputType = v
			inputTypeSet = true
			args = args[2:]
		case "--pause":
			pause = 1
			pauseSet = true
			args = args[1:]
		case "--no-pause":
			pause = 0
			pauseSet = true
			args = args[1:]
		case "--request":
			if len(args) < 2 {
				return nil, errors.New("missing --request value")
			}
			requestArg = args[1]
			args = args[2:]
		default:
			return nil, errors.New("unknown flag: " + args[0])
		}
	}
	req, err := readJSONObjectRequestArg(requestArg)
	if err != nil {
		return nil, err
	}
	if modify {
		maybeSetStringField(req, "RuleId", ruleID)
		if strings.TrimSpace(requestArg) == "" && strings.TrimSpace(ruleID) == "" {
			return nil, errors.New("missing --rule-id")
		}
	} else if strings.TrimSpace(requestArg) == "" {
		for _, pair := range []struct {
			name  string
			value string
		}{
			{"--topic-id", topicID},
			{"--rule-name", ruleName},
		} {
			if strings.TrimSpace(pair.value) == "" {
				return nil, errors.New("missing " + pair.name)
			}
		}
	}
	maybeSetStringField(req, "TopicId", topicID)
	maybeSetStringField(req, "RuleName", ruleName)
	maybeSetStringField(req, "LogType", logType)
	if strings.TrimSpace(pathsArg) != "" {
		paths, err := util.ReadStringListMaybeFile(pathsArg)
		if err != nil {
			return nil, err
		}
		req["Paths"] = paths
	}
	if inputTypeSet {
		req["InputType"] = inputType
	}
	if pauseSet {
		req["Pause"] = pause
	}
	return req, nil
}
