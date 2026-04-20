package cli

import (
	"errors"
	"net/url"
	"strconv"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/util"
)

func runMetricTopic(ctx *Context, args []string) (any, error) {
	return runSubcommandGroup(args, usageMetricTopic(), map[string]struct{}{"prom": {}}, func(command string, commandArgs []string) (any, error) {
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
		case "prom":
			return metricTopicProm(ctx, commandArgs)
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
		return listAllByPageNumber(ctx, "/DescribeMetricTopics", query, "Topics")
	}
	body, _ := util.MustJSON(map[string]any{})
	return ctx.Do("GET", "/DescribeMetricTopics", query, nil, body)
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
	body, _ := util.MustJSON(map[string]any{})
	return ctx.Do("GET", "/DescribeMetricTopic", map[string]string{"TopicId": topicID}, nil, body)
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

	body, err := util.MustJSON(req)
	if err != nil {
		return nil, err
	}
	return ctx.Do("POST", "/CreateMetricTopic", nil, nil, body)
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

	body, err := util.MustJSON(req)
	if err != nil {
		return nil, err
	}
	return ctx.Do("PUT", "/ModifyMetricTopic", nil, nil, body)
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
	body, err := util.MustJSON(map[string]any{"TopicId": topicID})
	if err != nil {
		return nil, err
	}
	return ctx.Do("DELETE", "/DeleteMetricTopic", nil, nil, body)
}

func metricTopicSearch(ctx *Context, args []string) (any, error) {
	req, err := parseSearchLogsArgs(args)
	if err != nil {
		return nil, err
	}
	body, err := util.MustJSON(req)
	if err != nil {
		return nil, err
	}
	return ctx.Do("POST", "/SearchLogs", nil, nil, body)
}

func metricTopicProm(ctx *Context, args []string) (any, error) {
	if len(args) == 0 {
		return nil, &usageError{Text: usageMetricTopicProm(), ExitCode: 1}
	}
	if args[0] == "-h" || args[0] == "--help" {
		return nil, &usageError{Text: usageMetricTopicProm(), ExitCode: 0}
	}
	if hasHelp(args[1:]) {
		return nil, &usageError{Text: usageMetricTopicProm(), ExitCode: 0}
	}
	ctx.Action = "metric-topic.prom." + strings.TrimSpace(args[0])
	switch args[0] {
	case "query":
		return metricTopicPromQuery(ctx, args[1:])
	case "query-range":
		return metricTopicPromQueryRange(ctx, args[1:])
	case "series":
		return metricTopicPromSeries(ctx, args[1:])
	case "labels":
		return metricTopicPromLabels(ctx, args[1:])
	case "label-values":
		return metricTopicPromLabelValues(ctx, args[1:])
	default:
		return nil, errors.New("unknown metric-topic prom command: " + args[0])
	}
}

func metricTopicPromQuery(ctx *Context, args []string) (any, error) {
	var (
		topicID       string
		queryExpr     string
		timeArg       string
		timeout       string
		limit         string
		lookbackDelta string
		method        string
	)
	method = "GET"
	for len(args) > 0 {
		switch args[0] {
		case "--topic-id":
			if len(args) < 2 {
				return nil, errors.New("missing --topic-id value")
			}
			topicID = args[1]
			args = args[2:]
		case "--query":
			if len(args) < 2 {
				return nil, errors.New("missing --query value")
			}
			s, err := util.ReadStringMaybeFile(args[1])
			if err != nil {
				return nil, err
			}
			queryExpr = s
			args = args[2:]
		case "--time":
			if len(args) < 2 {
				return nil, errors.New("missing --time value")
			}
			s, err := util.ReadStringMaybeFile(args[1])
			if err != nil {
				return nil, err
			}
			timeArg = s
			args = args[2:]
		case "--timeout":
			if len(args) < 2 {
				return nil, errors.New("missing --timeout value")
			}
			s, err := util.ReadStringMaybeFile(args[1])
			if err != nil {
				return nil, err
			}
			timeout = s
			args = args[2:]
		case "--limit":
			if len(args) < 2 {
				return nil, errors.New("missing --limit value")
			}
			s, err := util.ReadStringMaybeFile(args[1])
			if err != nil {
				return nil, err
			}
			limit = s
			args = args[2:]
		case "--lookback-delta":
			if len(args) < 2 {
				return nil, errors.New("missing --lookback-delta value")
			}
			s, err := util.ReadStringMaybeFile(args[1])
			if err != nil {
				return nil, err
			}
			lookbackDelta = s
			args = args[2:]
		case "--method":
			if len(args) < 2 {
				return nil, errors.New("missing --method value")
			}
			method = strings.ToUpper(args[1])
			args = args[2:]
		default:
			return nil, errors.New("unknown flag: " + args[0])
		}
	}
	topicID = strings.TrimSpace(topicID)
	queryExpr = strings.TrimSpace(queryExpr)
	if topicID == "" {
		return nil, errors.New("missing --topic-id")
	}
	if queryExpr == "" {
		return nil, errors.New("missing --query")
	}
	t := strings.TrimSpace(timeArg)
	if t == "" {
		t = util.FormatRFC3339Now()
	}
	pt, err := util.ParsePromTime(t)
	if err != nil {
		return nil, err
	}

	params := url.Values{}
	params.Set("query", queryExpr)
	params.Set("time", pt)
	if strings.TrimSpace(timeout) != "" {
		params.Set("timeout", timeout)
	}
	if strings.TrimSpace(limit) != "" {
		params.Set("limit", limit)
	}
	if strings.TrimSpace(lookbackDelta) != "" {
		params.Set("lookback_delta", lookbackDelta)
	}

	basePath := "/topic/" + url.PathEscape(topicID) + "/api/v1/query"
	return promDo(ctx, method, basePath, params)
}

func metricTopicPromQueryRange(ctx *Context, args []string) (any, error) {
	var (
		topicID       string
		queryExpr     string
		startArg      string
		endArg        string
		step          string
		timeout       string
		limit         string
		lookbackDelta string
		method        string
	)
	method = "GET"
	for len(args) > 0 {
		switch args[0] {
		case "--topic-id":
			if len(args) < 2 {
				return nil, errors.New("missing --topic-id value")
			}
			topicID = args[1]
			args = args[2:]
		case "--query":
			if len(args) < 2 {
				return nil, errors.New("missing --query value")
			}
			s, err := util.ReadStringMaybeFile(args[1])
			if err != nil {
				return nil, err
			}
			queryExpr = s
			args = args[2:]
		case "--start":
			if len(args) < 2 {
				return nil, errors.New("missing --start value")
			}
			s, err := util.ReadStringMaybeFile(args[1])
			if err != nil {
				return nil, err
			}
			startArg = s
			args = args[2:]
		case "--end":
			if len(args) < 2 {
				return nil, errors.New("missing --end value")
			}
			s, err := util.ReadStringMaybeFile(args[1])
			if err != nil {
				return nil, err
			}
			endArg = s
			args = args[2:]
		case "--step":
			if len(args) < 2 {
				return nil, errors.New("missing --step value")
			}
			s, err := util.ReadStringMaybeFile(args[1])
			if err != nil {
				return nil, err
			}
			step = s
			args = args[2:]
		case "--timeout":
			if len(args) < 2 {
				return nil, errors.New("missing --timeout value")
			}
			s, err := util.ReadStringMaybeFile(args[1])
			if err != nil {
				return nil, err
			}
			timeout = s
			args = args[2:]
		case "--limit":
			if len(args) < 2 {
				return nil, errors.New("missing --limit value")
			}
			s, err := util.ReadStringMaybeFile(args[1])
			if err != nil {
				return nil, err
			}
			limit = s
			args = args[2:]
		case "--lookback-delta":
			if len(args) < 2 {
				return nil, errors.New("missing --lookback-delta value")
			}
			s, err := util.ReadStringMaybeFile(args[1])
			if err != nil {
				return nil, err
			}
			lookbackDelta = s
			args = args[2:]
		case "--method":
			if len(args) < 2 {
				return nil, errors.New("missing --method value")
			}
			method = strings.ToUpper(args[1])
			args = args[2:]
		default:
			return nil, errors.New("unknown flag: " + args[0])
		}
	}
	topicID = strings.TrimSpace(topicID)
	queryExpr = strings.TrimSpace(queryExpr)
	startArg = strings.TrimSpace(startArg)
	endArg = strings.TrimSpace(endArg)
	step = strings.TrimSpace(step)
	if topicID == "" {
		return nil, errors.New("missing --topic-id")
	}
	if queryExpr == "" {
		return nil, errors.New("missing --query")
	}
	if startArg == "" {
		return nil, errors.New("missing --start")
	}
	if endArg == "" {
		return nil, errors.New("missing --end")
	}
	if step == "" {
		return nil, errors.New("missing --step")
	}
	ps, err := util.ParsePromTime(startArg)
	if err != nil {
		return nil, err
	}
	pe, err := util.ParsePromTime(endArg)
	if err != nil {
		return nil, err
	}

	params := url.Values{}
	params.Set("query", queryExpr)
	params.Set("start", ps)
	params.Set("end", pe)
	params.Set("step", step)
	if strings.TrimSpace(timeout) != "" {
		params.Set("timeout", timeout)
	}
	if strings.TrimSpace(limit) != "" {
		params.Set("limit", limit)
	}
	if strings.TrimSpace(lookbackDelta) != "" {
		params.Set("lookback_delta", lookbackDelta)
	}

	basePath := "/topic/" + url.PathEscape(topicID) + "/api/v1/query_range"
	return promDo(ctx, method, basePath, params)
}

func metricTopicPromSeries(ctx *Context, args []string) (any, error) {
	var (
		topicID  string
		startArg string
		endArg   string
		limit    string
		method   string
		matches  []string
	)
	method = "GET"
	for len(args) > 0 {
		switch args[0] {
		case "--topic-id":
			if len(args) < 2 {
				return nil, errors.New("missing --topic-id value")
			}
			topicID = args[1]
			args = args[2:]
		case "--start":
			if len(args) < 2 {
				return nil, errors.New("missing --start value")
			}
			s, err := util.ReadStringMaybeFile(args[1])
			if err != nil {
				return nil, err
			}
			startArg = s
			args = args[2:]
		case "--end":
			if len(args) < 2 {
				return nil, errors.New("missing --end value")
			}
			s, err := util.ReadStringMaybeFile(args[1])
			if err != nil {
				return nil, err
			}
			endArg = s
			args = args[2:]
		case "--match":
			if len(args) < 2 {
				return nil, errors.New("missing --match value")
			}
			ss, err := util.ReadStringListMaybeFile(args[1])
			if err != nil {
				return nil, err
			}
			matches = append(matches, ss...)
			args = args[2:]
		case "--limit":
			if len(args) < 2 {
				return nil, errors.New("missing --limit value")
			}
			s, err := util.ReadStringMaybeFile(args[1])
			if err != nil {
				return nil, err
			}
			limit = s
			args = args[2:]
		case "--method":
			if len(args) < 2 {
				return nil, errors.New("missing --method value")
			}
			method = strings.ToUpper(args[1])
			args = args[2:]
		default:
			return nil, errors.New("unknown flag: " + args[0])
		}
	}
	topicID = strings.TrimSpace(topicID)
	startArg = strings.TrimSpace(startArg)
	endArg = strings.TrimSpace(endArg)
	if topicID == "" {
		return nil, errors.New("missing --topic-id")
	}
	if startArg == "" {
		return nil, errors.New("missing --start")
	}
	if endArg == "" {
		return nil, errors.New("missing --end")
	}
	if len(matches) == 0 {
		return nil, errors.New("missing --match")
	}
	ps, err := util.ParsePromTime(startArg)
	if err != nil {
		return nil, err
	}
	pe, err := util.ParsePromTime(endArg)
	if err != nil {
		return nil, err
	}

	params := url.Values{}
	params.Set("start", ps)
	params.Set("end", pe)
	for _, m := range matches {
		params.Add("match[]", m)
	}
	if strings.TrimSpace(limit) != "" {
		params.Set("limit", limit)
	}
	basePath := "/topic/" + url.PathEscape(topicID) + "/api/v1/series"
	return promDo(ctx, method, basePath, params)
}

func metricTopicPromLabels(ctx *Context, args []string) (any, error) {
	var (
		topicID  string
		startArg string
		endArg   string
		limit    string
		method   string
		matches  []string
	)
	method = "GET"
	for len(args) > 0 {
		switch args[0] {
		case "--topic-id":
			if len(args) < 2 {
				return nil, errors.New("missing --topic-id value")
			}
			topicID = args[1]
			args = args[2:]
		case "--start":
			if len(args) < 2 {
				return nil, errors.New("missing --start value")
			}
			s, err := util.ReadStringMaybeFile(args[1])
			if err != nil {
				return nil, err
			}
			startArg = s
			args = args[2:]
		case "--end":
			if len(args) < 2 {
				return nil, errors.New("missing --end value")
			}
			s, err := util.ReadStringMaybeFile(args[1])
			if err != nil {
				return nil, err
			}
			endArg = s
			args = args[2:]
		case "--match":
			if len(args) < 2 {
				return nil, errors.New("missing --match value")
			}
			ss, err := util.ReadStringListMaybeFile(args[1])
			if err != nil {
				return nil, err
			}
			matches = append(matches, ss...)
			args = args[2:]
		case "--limit":
			if len(args) < 2 {
				return nil, errors.New("missing --limit value")
			}
			s, err := util.ReadStringMaybeFile(args[1])
			if err != nil {
				return nil, err
			}
			limit = s
			args = args[2:]
		case "--method":
			if len(args) < 2 {
				return nil, errors.New("missing --method value")
			}
			method = strings.ToUpper(args[1])
			args = args[2:]
		default:
			return nil, errors.New("unknown flag: " + args[0])
		}
	}
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return nil, errors.New("missing --topic-id")
	}
	params := url.Values{}
	if strings.TrimSpace(startArg) != "" {
		ps, err := util.ParsePromTime(startArg)
		if err != nil {
			return nil, err
		}
		params.Set("start", ps)
	}
	if strings.TrimSpace(endArg) != "" {
		pe, err := util.ParsePromTime(endArg)
		if err != nil {
			return nil, err
		}
		params.Set("end", pe)
	}
	for _, m := range matches {
		params.Add("match[]", m)
	}
	if strings.TrimSpace(limit) != "" {
		params.Set("limit", limit)
	}
	basePath := "/topic/" + url.PathEscape(topicID) + "/api/v1/labels"
	return promDo(ctx, method, basePath, params)
}

func metricTopicPromLabelValues(ctx *Context, args []string) (any, error) {
	var (
		topicID   string
		labelName string
		startArg  string
		endArg    string
		limit     string
		method    string
		matches   []string
	)
	method = "GET"
	for len(args) > 0 {
		switch args[0] {
		case "--topic-id":
			if len(args) < 2 {
				return nil, errors.New("missing --topic-id value")
			}
			topicID = args[1]
			args = args[2:]
		case "--label-name":
			if len(args) < 2 {
				return nil, errors.New("missing --label-name value")
			}
			s, err := util.ReadStringMaybeFile(args[1])
			if err != nil {
				return nil, err
			}
			labelName = s
			args = args[2:]
		case "--start":
			if len(args) < 2 {
				return nil, errors.New("missing --start value")
			}
			s, err := util.ReadStringMaybeFile(args[1])
			if err != nil {
				return nil, err
			}
			startArg = s
			args = args[2:]
		case "--end":
			if len(args) < 2 {
				return nil, errors.New("missing --end value")
			}
			s, err := util.ReadStringMaybeFile(args[1])
			if err != nil {
				return nil, err
			}
			endArg = s
			args = args[2:]
		case "--match":
			if len(args) < 2 {
				return nil, errors.New("missing --match value")
			}
			ss, err := util.ReadStringListMaybeFile(args[1])
			if err != nil {
				return nil, err
			}
			matches = append(matches, ss...)
			args = args[2:]
		case "--limit":
			if len(args) < 2 {
				return nil, errors.New("missing --limit value")
			}
			s, err := util.ReadStringMaybeFile(args[1])
			if err != nil {
				return nil, err
			}
			limit = s
			args = args[2:]
		case "--method":
			if len(args) < 2 {
				return nil, errors.New("missing --method value")
			}
			method = strings.ToUpper(args[1])
			args = args[2:]
		default:
			return nil, errors.New("unknown flag: " + args[0])
		}
	}
	topicID = strings.TrimSpace(topicID)
	labelName = strings.TrimSpace(labelName)
	if topicID == "" {
		return nil, errors.New("missing --topic-id")
	}
	if labelName == "" {
		return nil, errors.New("missing --label-name")
	}
	params := url.Values{}
	if strings.TrimSpace(startArg) != "" {
		ps, err := util.ParsePromTime(startArg)
		if err != nil {
			return nil, err
		}
		params.Set("start", ps)
	}
	if strings.TrimSpace(endArg) != "" {
		pe, err := util.ParsePromTime(endArg)
		if err != nil {
			return nil, err
		}
		params.Set("end", pe)
	}
	for _, m := range matches {
		params.Add("match[]", m)
	}
	if strings.TrimSpace(limit) != "" {
		params.Set("limit", limit)
	}
	basePath := "/topic/" + url.PathEscape(topicID) + "/api/v1/label/" + url.PathEscape(labelName) + "/values"
	return promDo(ctx, method, basePath, params)
}

func promDo(ctx *Context, method string, basePath string, params url.Values) (any, error) {
	m := strings.TrimSpace(method)
	if m == "" {
		m = "GET"
	}
	if m != "GET" && m != "POST" {
		return nil, errors.New("unsupported method: " + m)
	}
	if m == "GET" {
		path := basePath
		if enc := params.Encode(); strings.TrimSpace(enc) != "" {
			path = path + "?" + enc
		}
		bodyEmpty, _ := util.MustJSON(map[string]any{})
		return ctx.Do("GET", path, nil, nil, bodyEmpty)
	}
	body := []byte(params.Encode())
	h := map[string]string{"Content-Type": "application/x-www-form-urlencoded"}
	return ctx.Do("POST", basePath, nil, h, body)
}
