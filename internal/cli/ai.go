package cli

import (
	"errors"
	"strconv"
	"strings"

	"tlsctl/internal/ai"
	"tlsctl/internal/util"
)

func runAI(ctx *Context, args []string) (any, error) {
	if len(args) == 0 {
		return nil, &usageError{Text: usageAI(), ExitCode: 1}
	}
	if args[0] == "-h" || args[0] == "--help" {
		return nil, &usageError{Text: usageAI(), ExitCode: 0}
	}
	switch args[0] {
	case "list-packs":
		return map[string]any{"packs": ai.List()}, nil
	case "bootstrap":
		return aiBootstrap(ctx, args[1:])
	case "export":
		return aiExport(ctx, args[1:])
	default:
		return nil, errors.New("unknown ai command: " + args[0])
	}
}

func aiBootstrap(ctx *Context, args []string) (any, error) {
	var (
		packName  string
		projectID string
		topicName string
	)
	for len(args) > 0 {
		switch args[0] {
		case "--pack":
			if len(args) < 2 {
				return nil, errors.New("missing --pack value")
			}
			packName = args[1]
			args = args[2:]
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
		default:
			return nil, errors.New("unknown flag: " + args[0])
		}
	}
	projectID = strings.TrimSpace(projectID)
	if strings.TrimSpace(packName) == "" {
		return nil, errors.New("missing --pack")
	}
	if projectID == "" {
		return nil, errors.New("missing --project-id")
	}
	p, err := ai.Load(packName)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(topicName) == "" {
		topicName = p.Topic.TopicName
	}
	if strings.TrimSpace(topicName) == "" {
		return nil, errors.New("empty topic name")
	}

	bodyEmpty, _ := util.MustJSON(map[string]any{})
	q := map[string]string{
		"ProjectId":  projectID,
		"TopicName":  topicName,
		"PageSize":   "1",
		"PageNumber": "1",
	}
	existing, err := ctx.Do("GET", "/DescribeTopics", q, nil, bodyEmpty)
	if err != nil {
		return nil, err
	}
	topicID := extractFirstID(existing, "Topics", "TopicId", "TopicID")
	topicAction := "reused"
	if topicID == "" {
		req := map[string]any{
			"ProjectId":   projectID,
			"TopicName":   topicName,
			"Ttl":         p.Topic.Ttl,
			"Description": p.Topic.Description,
			"ShardCount":  p.Topic.ShardCount,
			"AutoSplit":   p.Topic.AutoSplit,
		}
		b, err := util.MustJSON(req)
		if err != nil {
			return nil, err
		}
		created, err := ctx.Do("POST", "/CreateTopic", nil, nil, b)
		if err != nil {
			return nil, err
		}
		topicID = extractString(created, "TopicId", "TopicID")
		if topicID == "" {
			return nil, errors.New("missing TopicId in create topic response")
		}
		topicAction = "created"
	}

	indexAction := "reused"
	r, err := ctx.DoRaw("GET", "/DescribeIndex", map[string]string{"TopicId": topicID}, nil, bodyEmpty)
	if err != nil {
		return nil, err
	}
	if r.StatusCode == 404 {
		indexAction = "created"
		req := copyMap(p.Index)
		req["TopicId"] = topicID
		b, err := util.MustJSON(req)
		if err != nil {
			return nil, err
		}
		if _, err := ctx.Do("POST", "/CreateIndex", nil, nil, b); err != nil {
			return nil, err
		}
	} else if r.StatusCode >= 200 && r.StatusCode < 300 {
		indexAction = "modified"
		req := copyMap(p.Index)
		req["TopicId"] = topicID
		b, err := util.MustJSON(req)
		if err != nil {
			return nil, err
		}
		if _, err := ctx.Do("PUT", "/ModifyIndex", nil, nil, b); err != nil {
			return nil, err
		}
	} else {
		_, err := decodeResponse(r)
		if err != nil {
			return nil, err
		}
		return nil, errors.New("describe index failed")
	}

	return map[string]any{
		"pack":         p.Name,
		"project_id":   projectID,
		"topic_name":   topicName,
		"topic_id":     topicID,
		"topic_action": topicAction,
		"index_action": indexAction,
	}, nil
}

func aiExport(ctx *Context, args []string) (any, error) {
	var (
		packName  string
		projectID string
		topicID   string
		fromStr   string
		toStr     string
		queryStr  string
		maxPages  int
	)
	maxPages = 100
	for len(args) > 0 {
		switch args[0] {
		case "--pack":
			if len(args) < 2 {
				return nil, errors.New("missing --pack value")
			}
			packName = args[1]
			args = args[2:]
		case "--project-id":
			if len(args) < 2 {
				return nil, errors.New("missing --project-id value")
			}
			projectID = args[1]
			args = args[2:]
		case "--topic-id":
			if len(args) < 2 {
				return nil, errors.New("missing --topic-id value")
			}
			topicID = args[1]
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
		case "--query":
			if len(args) < 2 {
				return nil, errors.New("missing --query value")
			}
			queryStr = args[1]
			args = args[2:]
		case "--max-pages":
			if len(args) < 2 {
				return nil, errors.New("missing --max-pages value")
			}
			v, err := strconv.Atoi(args[1])
			if err != nil {
				return nil, errors.New("--max-pages must be integer")
			}
			maxPages = v
			args = args[2:]
		default:
			return nil, errors.New("unknown flag: " + args[0])
		}
	}
	if strings.TrimSpace(packName) == "" {
		return nil, errors.New("missing --pack")
	}
	p, err := ai.Load(packName)
	if err != nil {
		return nil, err
	}
	topicID = strings.TrimSpace(topicID)
	projectID = strings.TrimSpace(projectID)
	if topicID == "" {
		if projectID == "" {
			return nil, errors.New("missing --topic-id or --project-id")
		}
		bodyEmpty, _ := util.MustJSON(map[string]any{})
		q := map[string]string{
			"ProjectId":  projectID,
			"TopicName":  p.Topic.TopicName,
			"PageSize":   "1",
			"PageNumber": "1",
		}
		existing, err := ctx.Do("GET", "/DescribeTopics", q, nil, bodyEmpty)
		if err != nil {
			return nil, err
		}
		topicID = extractFirstID(existing, "Topics", "TopicId", "TopicID")
		if topicID == "" {
			return nil, errors.New("topic not found for pack")
		}
	}

	if strings.TrimSpace(fromStr) == "" || strings.TrimSpace(toStr) == "" {
		return nil, errors.New("missing --from/--to")
	}
	start, err := util.ParseUnixMillis(fromStr)
	if err != nil {
		return nil, err
	}
	end, err := util.ParseUnixMillis(toStr)
	if err != nil {
		return nil, err
	}
	q := strings.TrimSpace(queryStr)
	if q == "" {
		q = p.Export.Query
	}
	limit := p.Export.Limit
	if limit <= 0 {
		limit = 100
	}
	req := map[string]any{
		"TopicId":   topicID,
		"Query":     q,
		"StartTime": start,
		"EndTime":   end,
		"Limit":     limit,
	}
	if strings.TrimSpace(p.Export.Sort) != "" {
		req["Sort"] = p.Export.Sort
	}
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

func extractFirstID(v any, listKey string, idKeys ...string) string {
	m, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	items, ok := m[listKey].([]any)
	if !ok || len(items) == 0 {
		return ""
	}
	item, ok := items[0].(map[string]any)
	if !ok {
		return ""
	}
	return extractString(item, idKeys...)
}

func extractString(v any, keys ...string) string {
	m, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	for _, k := range keys {
		if s, ok := m[k].(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func copyMap(m map[string]any) map[string]any {
	n := map[string]any{}
	for k, v := range m {
		n[k] = v
	}
	return n
}
