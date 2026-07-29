//go:build human

package cli

import (
	"errors"
	"strconv"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/execution"
	"github.com/volcengine-tls/ve-tls-cli/internal/util"
)

func runMetricTopic(ctx *Context, args []string) (any, error) {
	return runSubcommandGroup(args, usageMetricTopic(), nil, shortcutCommandHelpLookup("metric-topic"), func(command string, commandArgs []string) (any, error) {
		ctx.Action = "metric-topic." + strings.TrimSpace(command)
		if out, handled, err := maybeHandleShortcutMeta("metric-topic", command, commandArgs); handled {
			return out, err
		}
		switch command {
		case "list":
			return metricTopicList(ctx, commandArgs)
		case "get":
			return metricTopicGet(ctx, commandArgs)
		case "create":
			return metricTopicCreate(ctx, commandArgs)
		case "modify":
			return metricTopicModify(ctx, commandArgs)
		case "delete":
			return metricTopicDelete(ctx, commandArgs)
		case "search":
			return metricTopicSearch(ctx, commandArgs)
		default:
			return nil, errors.New("unknown metric-topic command: " + command)
		}
	})
}

func metricTopicList(ctx *Context, args []string) (any, error) {
	args, all := extractBoolFlag(args, "--all")
	query, err := parseTopicListQuery(args, false)
	if err != nil {
		return nil, err
	}
	if all {
		return executeShortcutOperation(ctx, shortcutExecutionRequest{
			OperationID: "metric-topic.describe-metric-topics",
			Input: execution.Input{
				Query: shortcutQueryInput(query),
				Body:  shortcutEmptyJSONBodyInput(),
			},
			PageAll: true,
			LegacyPageAll: &legacyPageAllPolicy{
				ListField:  "Topics",
				ForceTotal: true,
			},
		})
	}
	return executeShortcutOperation(ctx, shortcutExecutionRequest{
		OperationID: "metric-topic.describe-metric-topics",
		Input: execution.Input{
			Query: shortcutQueryInput(query),
			Body:  shortcutEmptyJSONBodyInput(),
		},
	})
}

func metricTopicGet(ctx *Context, args []string) (any, error) {
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
	return executeShortcutOperation(ctx, shortcutExecutionRequest{
		OperationID: "metric-topic.describe-metric-topic",
		Input: execution.Input{
			Query: shortcutQueryInput(map[string]string{"TopicId": topicID}),
			Body:  shortcutEmptyJSONBodyInput(),
		},
	})
}

func metricTopicCreate(ctx *Context, args []string) (any, error) {
	var (
		projectID     string
		topicName     string
		description   string
		ttl           int
		shardCount    int
		autoSplit     bool
		maxSplitShard int
		tagsArg       string
		requestArg    string
	)
	ttl = 30
	shardCount = 2
	for len(args) > 0 {
		switch args[0] {
		case "--project-id":
			if len(args) < 2 {
				return nil, errors.New("missing --project-id value")
			}
			projectID = args[1]
			args = args[2:]
		case "--topic-name":
			if len(args) < 2 {
				return nil, errors.New("missing --topic-name value")
			}
			topicName = args[1]
			args = args[2:]
		case "--description":
			if len(args) < 2 {
				return nil, errors.New("missing --description value")
			}
			description = args[1]
			args = args[2:]
		case "--ttl":
			if len(args) < 2 {
				return nil, errors.New("missing --ttl value")
			}
			v, err := strconv.Atoi(args[1])
			if err != nil {
				return nil, err
			}
			ttl = v
			args = args[2:]
		case "--shard-count":
			if len(args) < 2 {
				return nil, errors.New("missing --shard-count value")
			}
			v, err := strconv.Atoi(args[1])
			if err != nil {
				return nil, err
			}
			shardCount = v
			args = args[2:]
		case "--auto-split":
			autoSplit = true
			args = args[1:]
		case "--max-split-shard":
			if len(args) < 2 {
				return nil, errors.New("missing --max-split-shard value")
			}
			v, err := strconv.Atoi(args[1])
			if err != nil {
				return nil, err
			}
			maxSplitShard = v
			args = args[2:]
		case "--tags":
			if len(args) < 2 {
				return nil, errors.New("missing --tags value")
			}
			tagsArg = args[1]
			args = args[2:]
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

	projectID = strings.TrimSpace(projectID)
	topicName = strings.TrimSpace(topicName)
	if strings.TrimSpace(requestArg) == "" {
		if projectID == "" {
			return nil, errors.New("missing --project-id")
		}
		if topicName == "" {
			return nil, errors.New("missing --topic-name")
		}
	}

	var req map[string]any
	if strings.TrimSpace(requestArg) != "" {
		m, err := util.ReadJSONObjectMaybeFile(requestArg)
		if err != nil {
			return nil, err
		}
		req = m
	} else {
		req = map[string]any{}
	}
	if projectID != "" {
		req["ProjectId"] = projectID
	}
	if topicName != "" {
		req["TopicName"] = topicName
	}
	if ttl > 0 {
		req["Ttl"] = ttl
	}
	if strings.TrimSpace(description) != "" {
		req["Description"] = description
	}
	if shardCount > 0 {
		req["ShardCount"] = shardCount
	}
	if autoSplit {
		req["AutoSplit"] = true
	}
	if maxSplitShard > 0 {
		req["MaxSplitShard"] = maxSplitShard
	}
	if strings.TrimSpace(tagsArg) != "" {
		a, err := util.ReadJSONArrayMaybeFile(tagsArg)
		if err != nil {
			return nil, err
		}
		req["Tags"] = a
	}
	if reqAuto, ok := req["AutoSplit"].(bool); ok && reqAuto && maxSplitShard == 0 {
		if _, ok := req["MaxSplitShard"]; !ok {
			return nil, errors.New("missing --max-split-shard when AutoSplit is true")
		}
	}

	return executeShortcutOperation(ctx, shortcutExecutionRequest{
		OperationID: "metric-topic.create",
		Input: execution.Input{
			Body: shortcutJSONBodyInput(req),
		},
	})
}

func metricTopicModify(ctx *Context, args []string) (any, error) {
	var (
		topicID       string
		description   string
		clearDesc     bool
		topicName     string
		favSet        bool
		fav           bool
		ttl           int
		autoSplit     *bool
		maxSplitShard int
		requestArg    string
	)
	for len(args) > 0 {
		switch args[0] {
		case "--topic-id":
			if len(args) < 2 {
				return nil, errors.New("missing --topic-id value")
			}
			topicID = args[1]
			args = args[2:]
		case "--description":
			if len(args) < 2 {
				return nil, errors.New("missing --description value")
			}
			description = args[1]
			args = args[2:]
		case "--clear-description":
			clearDesc = true
			args = args[1:]
		case "--topic-name":
			if len(args) < 2 {
				return nil, errors.New("missing --topic-name value")
			}
			topicName = args[1]
			args = args[2:]
		case "--favourite":
			favSet = true
			fav = true
			args = args[1:]
		case "--no-favourite":
			favSet = true
			fav = false
			args = args[1:]
		case "--ttl":
			if len(args) < 2 {
				return nil, errors.New("missing --ttl value")
			}
			v, err := strconv.Atoi(args[1])
			if err != nil {
				return nil, err
			}
			ttl = v
			args = args[2:]
		case "--auto-split":
			v := true
			autoSplit = &v
			args = args[1:]
		case "--no-auto-split":
			v := false
			autoSplit = &v
			args = args[1:]
		case "--max-split-shard":
			if len(args) < 2 {
				return nil, errors.New("missing --max-split-shard value")
			}
			v, err := strconv.Atoi(args[1])
			if err != nil {
				return nil, err
			}
			maxSplitShard = v
			args = args[2:]
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
	if clearDesc && strings.TrimSpace(description) != "" {
		return nil, errors.New("--clear-description and --description cannot be provided together")
	}
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return nil, errors.New("missing --topic-id")
	}

	var req map[string]any
	if strings.TrimSpace(requestArg) != "" {
		m, err := util.ReadJSONObjectMaybeFile(requestArg)
		if err != nil {
			return nil, err
		}
		req = m
	} else {
		req = map[string]any{}
	}
	req["TopicId"] = topicID
	if clearDesc {
		req["Description"] = ""
	} else if strings.TrimSpace(description) != "" {
		if strings.TrimSpace(description) == "-" {
			req["Description"] = ""
		} else {
			req["Description"] = description
		}
	}
	if strings.TrimSpace(topicName) != "" {
		req["TopicName"] = topicName
	}
	if favSet {
		req["Favourite"] = fav
	}
	if ttl > 0 {
		req["Ttl"] = ttl
	}
	if autoSplit != nil {
		req["AutoSplit"] = *autoSplit
	}
	if maxSplitShard > 0 {
		req["MaxSplitShard"] = maxSplitShard
	}
	if reqAuto, ok := req["AutoSplit"].(bool); ok && reqAuto && maxSplitShard == 0 {
		if _, ok := req["MaxSplitShard"]; !ok {
			return nil, errors.New("missing --max-split-shard when AutoSplit is true")
		}
	}

	return executeShortcutOperation(ctx, shortcutExecutionRequest{
		OperationID: "metric-topic.modify",
		Input: execution.Input{
			Body: shortcutJSONBodyInput(req),
		},
	})
}

func metricTopicDelete(ctx *Context, args []string) (any, error) {
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
	return executeShortcutOperation(ctx, shortcutExecutionRequest{
		OperationID: "metric-topic.delete",
		Input: execution.Input{
			Body: shortcutJSONBodyInput(map[string]any{"TopicId": topicID}),
		},
	})
}

func metricTopicSearch(ctx *Context, args []string) (any, error) {
	req, err := parseSearchLogsArgs(args)
	if err != nil {
		return nil, err
	}
	return executeSearchLogsShortcut(ctx, req)
}
