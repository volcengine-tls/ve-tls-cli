package cli

import (
	"errors"
	"strconv"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/util"
)

const (
	searchLogsDefaultLimit = 100
	exportLogsDefaultLimit = 500
)

func isAnalysisQuery(cmd string) bool {
	tempCmd := cmd
	for {
		index := strings.Index(tempCmd, "|")
		if index < 0 {
			return false
		}
		before := tempCmd[:index]
		count := strings.Count(before, "\"")
		escapeQuoteCnt := strings.Count(before, "\\\"") - strings.Count(before, "\\\\\"")
		if (count-escapeQuoteCnt)%2 != 0 {
			tempCmd = strings.Replace(tempCmd, "|", ",", 1)
			continue
		}
		analysis := strings.ReplaceAll(cmd[index+1:], "\n", " ")
		analysis = strings.ToLower(strings.TrimSpace(analysis))
		if strings.HasPrefix(analysis, "select") || strings.HasPrefix(analysis, "insert") || strings.HasPrefix(analysis, "with") {
			return true
		}
		return false
	}
}

func runLog(ctx *Context, args []string) (any, error) {
	return runSubcommandGroup(args, usageLog(), nil, func(command string, commandArgs []string) (any, error) {
		ctx.Action = "log." + strings.TrimSpace(command)
		if out, handled, err := maybeHandleShortcutMeta("log", command, commandArgs); handled {
			return out, err
		}
		switch command {
		case "search":
			return logSearch(ctx, commandArgs)
		case "histogram":
			return logHistogram(ctx, commandArgs)
		case "context":
			return logContext(ctx, commandArgs)
		case "put":
			return logPut(ctx, commandArgs)
		case "export":
			return logExport(ctx, commandArgs)
		case "export-analysis":
			return logExportAnalysis(ctx, commandArgs)
		default:
			return nil, errors.New("unknown log command: " + command)
		}
	})
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

func logHistogram(ctx *Context, args []string) (any, error) {
	req, err := parseHistogramArgs(args)
	if err != nil {
		return nil, err
	}
	body, err := util.MustJSON(req)
	if err != nil {
		return nil, err
	}
	return ctx.Do("POST", "/DescribeHistogram", nil, nil, body)
}

func logContext(ctx *Context, args []string) (any, error) {
	req, err := parseLogContextArgs(args)
	if err != nil {
		return nil, err
	}
	body, err := util.MustJSON(req)
	if err != nil {
		return nil, err
	}
	return ctx.Do("POST", "/DescribeLogContext", nil, nil, body)
}

func logPut(ctx *Context, args []string) (any, error) {
	var (
		topicID      string
		requestArg   string
		requestFmt   = requestFormatJSON
		compressType string
		hashKey      string
		contentMD5   string
	)
	for len(args) > 0 {
		switch args[0] {
		case "--topic-id":
			if len(args) < 2 {
				return nil, errors.New("missing --topic-id value")
			}
			topicID = args[1]
			args = args[2:]
		case "--request":
			if len(args) < 2 {
				return nil, errors.New("missing --request value")
			}
			requestArg = args[1]
			args = args[2:]
		case "--request-format":
			if len(args) < 2 {
				return nil, errors.New("missing --request-format value")
			}
			requestFmt = normalizeRequestFormat(requestFormat(args[1]))
			args = args[2:]
		case "--compress-type":
			if len(args) < 2 {
				return nil, errors.New("missing --compress-type value")
			}
			compressType = args[1]
			args = args[2:]
		case "--hash-key":
			if len(args) < 2 {
				return nil, errors.New("missing --hash-key value")
			}
			hashKey = args[1]
			args = args[2:]
		case "--content-md5":
			if len(args) < 2 {
				return nil, errors.New("missing --content-md5 value")
			}
			contentMD5 = args[1]
			args = args[2:]
		default:
			return nil, errors.New("unknown flag: " + args[0])
		}
	}
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return nil, errors.New("missing --topic-id")
	}
	body, err := util.ReadMaybeFile(requestArg)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, errors.New("missing --request")
	}
	header := map[string]string{}
	maybeSetHeader(header, "x-tls-compresstype", compressType)
	maybeSetHeader(header, "x-tls-hashkey", hashKey)
	maybeSetHeader(header, "Content-MD5", contentMD5)
	ctx.apiIOMeta = apiIOMeta{
		Group:         "log",
		Action:        "PutLogs",
		Method:        "POST",
		Path:          "/PutLogs",
		RequestFormat: requestFmt,
		OutputFormat:  ctx.Format,
		OutputMode:    ctx.OutputMode,
	}
	return ctx.Do("POST", "/PutLogs", map[string]string{"TopicId": topicID}, header, body)
}

func logExport(ctx *Context, args []string) (any, error) {
	req, err := parseExportSearchLogsArgs(args)
	if err != nil {
		return nil, err
	}
	if q, ok := req["Query"].(string); ok && isAnalysisQuery(q) {
		return nil, errors.New("log export does not support analysis query; use log export-analysis (or log search)")
	}
	maxPages := 100
	if v, ok := req["__max_pages"].(int); ok && v > 0 {
		maxPages = v
	}
	delete(req, "__max_pages")

	streamWriter, err := maybeNewStreamedLogFileWriter(ctx, "log")
	if err != nil {
		return nil, err
	}
	if streamWriter != nil {
		defer streamWriter.abort()
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
			if streamWriter != nil {
				if err := streamWriter.WriteRows(logs); err != nil {
					return nil, err
				}
			} else {
				all = append(all, logs...)
			}
		}
		listOver, _ := m["ListOver"].(bool)
		nextCtx, _ := m["Context"].(string)
		if listOver || strings.TrimSpace(nextCtx) == "" {
			break
		}
		req["Context"] = nextCtx
	}
	if streamWriter != nil {
		return streamWriter.Commit()
	}
	return all, nil
}

func logExportAnalysis(ctx *Context, args []string) (any, error) {
	req, err := parseSearchLogsArgs(args)
	if err != nil {
		return nil, err
	}
	if _, ok := req["__max_pages"]; ok {
		return nil, errors.New("--max-pages is not supported for log export-analysis; use SQL offset/limit in --query")
	}
	if q, ok := req["Query"].(string); !ok || !isAnalysisQuery(q) {
		return nil, errors.New("log export-analysis requires analysis query (e.g. '*|select ...'); use log export for pure search")
	}

	streamWriter, err := maybeNewStreamedLogFileWriter(ctx, "log")
	if err != nil {
		return nil, err
	}
	if streamWriter != nil {
		defer streamWriter.abort()
	}

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
	ar, ok := m["AnalysisResult"].(map[string]any)
	if !ok {
		return nil, errors.New("missing AnalysisResult (use log export for Logs)")
	}
	data, _ := ar["Data"].([]any)
	var rows []map[string]any
	for _, r := range data {
		row, ok := r.(map[string]any)
		if !ok {
			return nil, errors.New("invalid AnalysisResult.Data row")
		}
		if streamWriter != nil {
			if err := streamWriter.WriteObjectRows([]map[string]any{row}); err != nil {
				return nil, err
			}
		} else {
			rows = append(rows, row)
		}
	}
	if streamWriter != nil {
		return streamWriter.Commit()
	}
	return rows, nil
}

func parseSearchLogsArgs(args []string) (map[string]any, error) {
	return parseSearchLogsArgsWithDefaultLimit(args, searchLogsDefaultLimit)
}

func parseExportSearchLogsArgs(args []string) (map[string]any, error) {
	return parseSearchLogsArgsWithDefaultLimit(args, exportLogsDefaultLimit)
}

func parseSearchLogsArgsWithDefaultLimit(args []string, defaultLimit int) (map[string]any, error) {
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
		limitSet     bool
		contextSet   bool
		sortSet      bool
		offsetSet    bool
	)
	limit = defaultLimit
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
			limitSet = true
			args = args[2:]
		case "--context":
			if len(args) < 2 {
				return nil, errors.New("missing --context value")
			}
			contextStr = args[1]
			contextSet = true
			args = args[2:]
		case "--sort":
			if len(args) < 2 {
				return nil, errors.New("missing --sort value")
			}
			sortStr = args[1]
			sortSet = true
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
			offsetSet = true
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
	if highlight {
		req["HighLight"] = true
	}
	if accurateSet {
		req["AccurateQuery"] = accurate
	}
	if mustSet {
		req["MustComplete"] = mustComplete
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

	q, _ := req["Query"].(string)
	analysisMode := isAnalysisQuery(q)
	if analysisMode {
		if limitSet || contextSet || sortSet || offsetSet {
			return nil, errors.New("for analysis query, do not use --limit/--context/--sort/--offset; use SQL limit/offset in --query (analysis does not support Context pagination)")
		}
		if _, ok := req["Limit"]; ok {
			return nil, errors.New("for analysis query, request.Limit is not effective; use SQL limit in request.Query")
		}
		if _, ok := req["Context"]; ok {
			return nil, errors.New("for analysis query, request.Context is not supported")
		}
		if _, ok := req["Sort"]; ok {
			return nil, errors.New("for analysis query, request.Sort is not effective")
		}
		if _, ok := req["Offset"]; ok {
			return nil, errors.New("for analysis query, request.Offset is not effective; use SQL offset in request.Query")
		}
		return req, nil
	}

	if limitSet {
		req["Limit"] = limit
	} else if _, ok := req["Limit"]; !ok && limit > 0 {
		req["Limit"] = limit
	}
	if strings.TrimSpace(contextStr) != "" {
		req["Context"] = contextStr
	}
	if strings.TrimSpace(sortStr) != "" {
		req["Sort"] = sortStr
	}
	if offset > 0 {
		req["Offset"] = offset
	}
	return req, nil
}

func parseHistogramArgs(args []string) (map[string]any, error) {
	var (
		topicID    string
		query      string
		fromStr    string
		toStr      string
		requestArg string
		interval   int
	)
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
		case "--interval":
			if len(args) < 2 {
				return nil, errors.New("missing --interval value")
			}
			v, err := strconv.Atoi(args[1])
			if err != nil {
				return nil, err
			}
			interval = v
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
	req, err := readJSONObjectRequestArg(requestArg)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(requestArg) == "" {
		if strings.TrimSpace(topicID) == "" {
			return nil, errors.New("missing --topic-id")
		}
		if strings.TrimSpace(query) == "" {
			return nil, errors.New("missing --query")
		}
		if strings.TrimSpace(fromStr) == "" {
			return nil, errors.New("missing --from")
		}
		if strings.TrimSpace(toStr) == "" {
			return nil, errors.New("missing --to")
		}
	}
	maybeSetStringField(req, "TopicId", topicID)
	maybeSetStringField(req, "Query", query)
	if strings.TrimSpace(fromStr) != "" {
		v, err := strconv.ParseInt(fromStr, 10, 64)
		if err != nil {
			return nil, err
		}
		req["StartTime"] = v
	}
	if strings.TrimSpace(toStr) != "" {
		v, err := strconv.ParseInt(toStr, 10, 64)
		if err != nil {
			return nil, err
		}
		req["EndTime"] = v
	}
	maybeSetIntField(req, "Interval", interval)
	return req, nil
}

func parseLogContextArgs(args []string) (map[string]any, error) {
	var (
		topicID       string
		contextFlow   string
		source        string
		requestArg    string
		packageOffset int
		packageSet    bool
		prevLogs      int
		nextLogs      int
	)
	for len(args) > 0 {
		switch args[0] {
		case "--topic-id":
			if len(args) < 2 {
				return nil, errors.New("missing --topic-id value")
			}
			topicID = args[1]
			args = args[2:]
		case "--context-flow":
			if len(args) < 2 {
				return nil, errors.New("missing --context-flow value")
			}
			contextFlow = args[1]
			args = args[2:]
		case "--source":
			if len(args) < 2 {
				return nil, errors.New("missing --source value")
			}
			source = args[1]
			args = args[2:]
		case "--package-offset":
			if len(args) < 2 {
				return nil, errors.New("missing --package-offset value")
			}
			v, err := strconv.Atoi(args[1])
			if err != nil {
				return nil, err
			}
			packageOffset = v
			packageSet = true
			args = args[2:]
		case "--prev-logs":
			if len(args) < 2 {
				return nil, errors.New("missing --prev-logs value")
			}
			v, err := strconv.Atoi(args[1])
			if err != nil {
				return nil, err
			}
			prevLogs = v
			args = args[2:]
		case "--next-logs":
			if len(args) < 2 {
				return nil, errors.New("missing --next-logs value")
			}
			v, err := strconv.Atoi(args[1])
			if err != nil {
				return nil, err
			}
			nextLogs = v
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
	req, err := readJSONObjectRequestArg(requestArg)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(requestArg) == "" {
		for _, pair := range []struct {
			name  string
			value string
		}{
			{"--topic-id", topicID},
			{"--context-flow", contextFlow},
			{"--source", source},
		} {
			if strings.TrimSpace(pair.value) == "" {
				return nil, errors.New("missing " + pair.name)
			}
		}
		if !packageSet {
			return nil, errors.New("missing --package-offset")
		}
	}
	maybeSetStringField(req, "TopicId", topicID)
	maybeSetStringField(req, "ContextFlow", contextFlow)
	maybeSetStringField(req, "Source", source)
	if packageSet {
		req["PackageOffset"] = packageOffset
	}
	maybeSetIntField(req, "PrevLogs", prevLogs)
	maybeSetIntField(req, "NextLogs", nextLogs)
	return req, nil
}

func maybeSetHeader(dst map[string]string, key string, value string) {
	if strings.TrimSpace(value) != "" {
		dst[key] = strings.TrimSpace(value)
	}
}
