//go:build human

package cli

import (
	"errors"
	"strconv"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/util"
)

func runLog(ctx *Context, args []string) (any, error) {
	return runSubcommandGroup(args, usageLog(), nil, shortcutCommandHelpLookup("log"), func(command string, commandArgs []string) (any, error) {
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
		case "ingest":
			return logIngest(ctx, commandArgs)
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
	return ctx.Do("POST", "/DescribeHistogramV1", nil, nil, body)
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
	return doPutLogs(ctx, topicID, requestFmt, header, body)
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
