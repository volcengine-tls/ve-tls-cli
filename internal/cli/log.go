package cli

import (
	"errors"
	"strconv"
	"strings"

	"tlsctl/internal/util"
)

func runLog(ctx *Context, args []string) (any, error) {
	if len(args) == 0 {
		return nil, &usageError{Text: usageLog(), ExitCode: 1}
	}
	if args[0] == "-h" || args[0] == "--help" {
		return nil, &usageError{Text: usageLog(), ExitCode: 0}
	}
	switch args[0] {
	case "search":
		return logSearch(ctx, args[1:])
	case "export":
		return logExport(ctx, args[1:])
	default:
		return nil, errors.New("unknown log command: " + args[0])
	}
}

func logSearch(ctx *Context, args []string) (any, error) {
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

func logExport(ctx *Context, args []string) (any, error) {
	req, err := parseSearchLogsArgs(args)
	if err != nil {
		return nil, err
	}
	maxPages := 100
	if v, ok := req["__max_pages"].(int); ok && v > 0 {
		maxPages = v
	}
	delete(req, "__max_pages")

	var all []any
	for page := 0; page < maxPages; page++ {
		body, err := util.MustJSON(req)
		if err != nil {
			return nil, err
		}
		out, err := ctx.Do("POST", "/SearchLogs", nil, nil, body)
		if err != nil {
			return nil, err
		}
		m, ok := out.(map[string]any)
		if !ok {
			return nil, errors.New("unexpected search response")
		}
		if logs, ok := m["Logs"].([]any); ok {
			all = append(all, logs...)
		}
		listOver, _ := m["ListOver"].(bool)
		nextCtx, _ := m["Context"].(string)
		if listOver || strings.TrimSpace(nextCtx) == "" {
			break
		}
		req["Context"] = nextCtx
	}
	return all, nil
}

func parseSearchLogsArgs(args []string) (map[string]any, error) {
	var (
		topicID      string
		query        string
		fromStr      string
		toStr        string
		limit        int
		contextStr   string
		sortStr      string
		highlight    bool
		accurateSet  bool
		accurate     bool
		mustSet      bool
		mustComplete bool
		offset       int
		maxPages     int
		requestArg   string
	)
	limit = 100
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
			query = args[1]
			args = args[2:]
		case "--from":
			if len(args) < 2 {
				return nil, errors.New("missing --from value")
			}
			fromStr = args[1]
			args = args[2:]
		case "--to":
			if len(args) < 2 {
				return nil, errors.New("missing --to value")
			}
			toStr = args[1]
			args = args[2:]
		case "--limit":
			if len(args) < 2 {
				return nil, errors.New("missing --limit value")
			}
			v, err := strconv.Atoi(args[1])
			if err != nil {
				return nil, err
			}
			limit = v
			args = args[2:]
		case "--context":
			if len(args) < 2 {
				return nil, errors.New("missing --context value")
			}
			contextStr = args[1]
			args = args[2:]
		case "--sort":
			if len(args) < 2 {
				return nil, errors.New("missing --sort value")
			}
			sortStr = args[1]
			args = args[2:]
		case "--highlight":
			highlight = true
			args = args[1:]
		case "--accurate-query":
			accurateSet = true
			accurate = true
			args = args[1:]
		case "--no-accurate-query":
			accurateSet = true
			accurate = false
			args = args[1:]
		case "--must-complete":
			mustSet = true
			mustComplete = true
			args = args[1:]
		case "--no-must-complete":
			mustSet = true
			mustComplete = false
			args = args[1:]
		case "--offset":
			if len(args) < 2 {
				return nil, errors.New("missing --offset value")
			}
			v, err := strconv.Atoi(args[1])
			if err != nil {
				return nil, err
			}
			offset = v
			args = args[2:]
		case "--max-pages":
			if len(args) < 2 {
				return nil, errors.New("missing --max-pages value")
			}
			v, err := strconv.Atoi(args[1])
			if err != nil {
				return nil, err
			}
			maxPages = v
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
	topicID = strings.TrimSpace(topicID)
	query = strings.TrimSpace(query)
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

	if topicID != "" {
		req["TopicId"] = topicID
	}
	if query != "" {
		req["Query"] = query
	}
	if strings.TrimSpace(fromStr) != "" {
		start, err := util.ParseUnixMillis(fromStr)
		if err != nil {
			return nil, err
		}
		req["StartTime"] = start
	}
	if strings.TrimSpace(toStr) != "" {
		end, err := util.ParseUnixMillis(toStr)
		if err != nil {
			return nil, err
		}
		req["EndTime"] = end
	}
	if limit > 0 {
		req["Limit"] = limit
	}
	if strings.TrimSpace(contextStr) != "" {
		req["Context"] = contextStr
	}
	if strings.TrimSpace(sortStr) != "" {
		req["Sort"] = sortStr
	}
	if highlight {
		req["HighLight"] = true
	}
	if accurateSet {
		req["AccurateQuery"] = accurate
	}
	if mustSet {
		req["MustComplete"] = mustComplete
	}
	if offset > 0 {
		req["Offset"] = offset
	}
	if maxPages > 0 {
		req["__max_pages"] = maxPages
	}

	if _, ok := req["TopicId"]; !ok {
		return nil, errors.New("missing --topic-id or request.TopicId")
	}
	if _, ok := req["Query"]; !ok {
		return nil, errors.New("missing --query or request.Query")
	}
	if _, ok := req["StartTime"]; !ok {
		return nil, errors.New("missing --from or request.StartTime")
	}
	if _, ok := req["EndTime"]; !ok {
		return nil, errors.New("missing --to or request.EndTime")
	}
	return req, nil
}
