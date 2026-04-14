package cli

import (
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	tlssdk "github.com/volcengine/volc-sdk-golang/service/tls"
	tlspb "github.com/volcengine/volc-sdk-golang/service/tls/pb"

	"github.com/volcengine-tls/ve-tls-cli/internal/util"
)

const ingestDefaultBatchMaxCount = 500

type logIngestArgs struct {
	TopicID       string
	Input         string
	InputFormat   string
	TimeField     string
	TimeFormat    string
	Source        string
	FileName      string
	Tags          []*tlspb.LogTag
	BatchMaxCount int
	CompressType  string
	HashKey       string
}

func logIngest(ctx *Context, args []string) (any, error) {
	cfg, err := parseLogIngestArgs(args)
	if err != nil {
		return nil, err
	}
	body, err := util.ReadMaybeFile(cfg.Input)
	if err != nil {
		return nil, err
	}
	defaultTime := time.Now().UnixMilli()
	logs, err := buildIngestLogs(cfg, body, defaultTime)
	if err != nil {
		return nil, err
	}
	if len(logs) == 0 {
		return map[string]any{
			"topicId":      cfg.TopicID,
			"inputFormat":  cfg.InputFormat,
			"compressType": ingestCompressionSummary(cfg.CompressType),
			"logs":         0,
			"batches":      0,
		}, nil
	}

	header := map[string]string{}
	maybeSetHeader(header, "x-tls-compresstype", cfg.CompressType)
	maybeSetHeader(header, "x-tls-hashkey", cfg.HashKey)

	batches := 0
	for start := 0; start < len(logs); start += cfg.BatchMaxCount {
		end := start + cfg.BatchMaxCount
		if end > len(logs) {
			end = len(logs)
		}
		list := &tlspb.LogGroupList{
			LogGroups: []*tlspb.LogGroup{
				{
					Source:   cfg.Source,
					FileName: cfg.FileName,
					LogTags:  cloneLogTags(cfg.Tags),
					Logs:     logs[start:end],
				},
			},
		}
		payload, err := util.MustJSON(list)
		if err != nil {
			return nil, err
		}
		if _, err := doPutLogs(ctx, cfg.TopicID, requestFormatJSON, cloneStringMap(header), payload); err != nil {
			return nil, err
		}
		batches++
	}

	return map[string]any{
		"topicId":      cfg.TopicID,
		"inputFormat":  cfg.InputFormat,
		"compressType": ingestCompressionSummary(cfg.CompressType),
		"logs":         len(logs),
		"batches":      batches,
	}, nil
}

func parseLogIngestArgs(args []string) (logIngestArgs, error) {
	cfg := logIngestArgs{
		InputFormat:   "lines",
		TimeFormat:    "auto",
		BatchMaxCount: ingestDefaultBatchMaxCount,
		CompressType:  tlssdk.CompressLz4,
	}
	var rawTags []string
	for len(args) > 0 {
		switch args[0] {
		case "--topic-id":
			if len(args) < 2 {
				return logIngestArgs{}, errors.New("missing --topic-id value")
			}
			cfg.TopicID = args[1]
			args = args[2:]
		case "--input":
			if len(args) < 2 {
				return logIngestArgs{}, errors.New("missing --input value")
			}
			cfg.Input = args[1]
			args = args[2:]
		case "--input-format":
			if len(args) < 2 {
				return logIngestArgs{}, errors.New("missing --input-format value")
			}
			cfg.InputFormat = normalizeIngestInputFormat(args[1])
			args = args[2:]
		case "--time-field":
			if len(args) < 2 {
				return logIngestArgs{}, errors.New("missing --time-field value")
			}
			cfg.TimeField = strings.TrimSpace(args[1])
			args = args[2:]
		case "--time-format":
			if len(args) < 2 {
				return logIngestArgs{}, errors.New("missing --time-format value")
			}
			raw := strings.TrimSpace(args[1])
			cfg.TimeFormat = normalizeIngestTimeFormat(raw)
			if cfg.TimeFormat == "__invalid__" {
				return logIngestArgs{}, errors.New("unsupported --time-format: " + raw)
			}
			args = args[2:]
		case "--source":
			if len(args) < 2 {
				return logIngestArgs{}, errors.New("missing --source value")
			}
			cfg.Source = strings.TrimSpace(args[1])
			args = args[2:]
		case "--file-name":
			if len(args) < 2 {
				return logIngestArgs{}, errors.New("missing --file-name value")
			}
			cfg.FileName = strings.TrimSpace(args[1])
			args = args[2:]
		case "--tag":
			if len(args) < 2 {
				return logIngestArgs{}, errors.New("missing --tag value")
			}
			rawTags = append(rawTags, args[1])
			args = args[2:]
		case "--batch-max-count":
			if len(args) < 2 {
				return logIngestArgs{}, errors.New("missing --batch-max-count value")
			}
			v, err := strconv.Atoi(args[1])
			if err != nil {
				return logIngestArgs{}, err
			}
			cfg.BatchMaxCount = v
			args = args[2:]
		case "--compress-type":
			if len(args) < 2 {
				return logIngestArgs{}, errors.New("missing --compress-type value")
			}
			cfg.CompressType = normalizeIngestCompression(args[1])
			if cfg.CompressType == "__invalid__" {
				return logIngestArgs{}, errors.New("unsupported --compress-type: " + args[1])
			}
			args = args[2:]
		case "--hash-key":
			if len(args) < 2 {
				return logIngestArgs{}, errors.New("missing --hash-key value")
			}
			cfg.HashKey = strings.TrimSpace(args[1])
			args = args[2:]
		default:
			return logIngestArgs{}, errors.New("unknown flag: " + args[0])
		}
	}
	cfg.TopicID = strings.TrimSpace(cfg.TopicID)
	if cfg.TopicID == "" {
		return logIngestArgs{}, errors.New("missing --topic-id")
	}
	if strings.TrimSpace(cfg.Input) == "" {
		return logIngestArgs{}, errors.New("missing --input")
	}
	if cfg.BatchMaxCount <= 0 {
		return logIngestArgs{}, errors.New("--batch-max-count must be > 0")
	}
	switch cfg.InputFormat {
	case "lines", "jsonl", "json-array":
	default:
		return logIngestArgs{}, errors.New("unsupported --input-format: " + cfg.InputFormat)
	}
	if cfg.InputFormat == "lines" && strings.TrimSpace(cfg.TimeField) != "" {
		return logIngestArgs{}, errors.New("--time-field is only supported for jsonl/json-array input")
	}
	tags, err := parseIngestTags(rawTags)
	if err != nil {
		return logIngestArgs{}, err
	}
	cfg.Tags = tags
	return cfg, nil
}

func buildIngestLogs(cfg logIngestArgs, body []byte, defaultTime int64) ([]*tlspb.Log, error) {
	switch cfg.InputFormat {
	case "lines":
		return buildLineLogs(body, defaultTime), nil
	case "jsonl":
		rows, err := parseJSONLObjects(body)
		if err != nil {
			return nil, err
		}
		return buildStructuredLogs(rows, cfg.TimeField, cfg.TimeFormat, defaultTime)
	case "json-array":
		rows, err := parseJSONArrayObjects(body)
		if err != nil {
			return nil, err
		}
		return buildStructuredLogs(rows, cfg.TimeField, cfg.TimeFormat, defaultTime)
	default:
		return nil, errors.New("unsupported --input-format: " + cfg.InputFormat)
	}
}

func buildLineLogs(body []byte, defaultTime int64) []*tlspb.Log {
	lines := strings.Split(string(body), "\n")
	out := make([]*tlspb.Log, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			continue
		}
		out = append(out, &tlspb.Log{
			Time: defaultTime,
			Contents: []*tlspb.LogContent{
				{Key: "__content__", Value: line},
			},
		})
	}
	return out
}

func buildStructuredLogs(rows []map[string]any, timeField string, timeFormat string, defaultTime int64) ([]*tlspb.Log, error) {
	out := make([]*tlspb.Log, 0, len(rows))
	for _, row := range rows {
		ts, err := resolveStructuredLogTime(row, timeField, timeFormat, defaultTime)
		if err != nil {
			return nil, err
		}
		out = append(out, &tlspb.Log{
			Time:     ts,
			Contents: buildStructuredContents(row),
		})
	}
	return out, nil
}

func resolveStructuredLogTime(row map[string]any, timeField string, timeFormat string, defaultTime int64) (int64, error) {
	if strings.TrimSpace(timeField) == "" {
		return defaultTime, nil
	}
	raw, ok := row[timeField]
	if !ok {
		return 0, errors.New("missing time field: " + timeField)
	}
	return parseIngestTimeValue(raw, timeFormat)
}

func buildStructuredContents(row map[string]any) []*tlspb.LogContent {
	if len(row) == 0 {
		return []*tlspb.LogContent{}
	}
	keys := make([]string, 0, len(row))
	for k := range row {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]*tlspb.LogContent, 0, len(keys))
	for _, k := range keys {
		out = append(out, &tlspb.LogContent{Key: k, Value: toString(row[k])})
	}
	return out
}

func parseJSONArrayObjects(body []byte) ([]map[string]any, error) {
	trimmed := bytesTrimSpaceLocal(body)
	if len(trimmed) == 0 {
		return []map[string]any{}, nil
	}
	v, err := util.UnmarshalJSON(trimmed)
	if err != nil {
		return nil, err
	}
	items, ok := v.([]any)
	if !ok {
		return nil, errors.New("json-array input must be json array")
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			return nil, errors.New("json-array item must be object")
		}
		out = append(out, row)
	}
	return out, nil
}

func parseIngestTags(raw []string) ([]*tlspb.LogTag, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]*tlspb.LogTag, 0, len(raw))
	for _, item := range raw {
		key, value, ok := strings.Cut(strings.TrimSpace(item), "=")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, errors.New("invalid --tag, expected k=v")
		}
		out = append(out, &tlspb.LogTag{
			Key:   strings.TrimSpace(key),
			Value: strings.TrimSpace(value),
		})
	}
	return out, nil
}

func cloneLogTags(tags []*tlspb.LogTag) []*tlspb.LogTag {
	if len(tags) == 0 {
		return nil
	}
	out := make([]*tlspb.LogTag, 0, len(tags))
	for _, item := range tags {
		if item == nil {
			continue
		}
		out = append(out, &tlspb.LogTag{Key: item.Key, Value: item.Value})
	}
	return out
}

func parseIngestTimeValue(raw any, timeFormat string) (int64, error) {
	value := strings.TrimSpace(toString(raw))
	if value == "" {
		return 0, errors.New("empty time value")
	}
	switch normalizeIngestTimeFormat(timeFormat) {
	case "auto":
		return util.ParseUnixMillis(value)
	case "unix_ms":
		return parseInt64String(value)
	case "unix":
		v, err := parseInt64String(value)
		if err != nil {
			return 0, err
		}
		return v * 1000, nil
	case "rfc3339":
		t, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return 0, err
		}
		return t.UnixMilli(), nil
	default:
		return 0, errors.New("unsupported time format: " + timeFormat)
	}
}

func normalizeIngestInputFormat(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func normalizeIngestTimeFormat(s string) string {
	v := strings.ToLower(strings.TrimSpace(s))
	v = strings.ReplaceAll(v, "-", "_")
	switch v {
	case "", "auto":
		return "auto"
	case "unix_ms", "unixms":
		return "unix_ms"
	case "unix":
		return "unix"
	case "rfc3339":
		return "rfc3339"
	default:
		return "__invalid__"
	}
}

func normalizeIngestCompression(s string) string {
	v := strings.ToLower(strings.TrimSpace(s))
	switch v {
	case "", "lz4":
		return tlssdk.CompressLz4
	case "zlib":
		return tlssdk.CompressZlib
	case "none":
		return ""
	default:
		return "__invalid__"
	}
}

func ingestCompressionSummary(s string) string {
	if strings.TrimSpace(s) == "" {
		return "none"
	}
	return s
}
