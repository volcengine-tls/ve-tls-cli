package cli

import (
	"errors"
	"strconv"
	"strings"

	"volclog/internal/util"
)

func runTopic(ctx *Context, args []string) (any, error) {
	if len(args) == 0 {
		return nil, &usageError{Text: usageTopic(), ExitCode: 1}
	}
	if args[0] == "-h" || args[0] == "--help" {
		return nil, &usageError{Text: usageTopic(), ExitCode: 0}
	}
	if hasHelp(args[1:]) {
		return nil, &usageError{Text: usageTopic(), ExitCode: 0}
	}
	switch args[0] {
	case "list":
		return topicList(ctx, args[1:])
	case "get":
		return topicGet(ctx, args[1:])
	case "create":
		return topicCreate(ctx, args[1:])
	case "modify":
		return topicModify(ctx, args[1:])
	case "delete":
		return topicDelete(ctx, args[1:])
	default:
		return nil, errors.New("unknown topic command: " + args[0])
	}
}

func topicList(ctx *Context, args []string) (any, error) {
	query := map[string]string{}
	for len(args) > 0 {
		switch args[0] {
		case "--project-id":
			if len(args) < 2 {
				return nil, errors.New("missing --project-id value")
			}
			query["ProjectId"] = args[1]
			args = args[2:]
		case "--topic-name":
			if len(args) < 2 {
				return nil, errors.New("missing --topic-name value")
			}
			query["TopicName"] = args[1]
			args = args[2:]
		case "--topic-id":
			if len(args) < 2 {
				return nil, errors.New("missing --topic-id value")
			}
			query["TopicId"] = args[1]
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
		case "--cursor":
			if len(args) < 2 {
				return nil, errors.New("missing --cursor value")
			}
			query["Cursor"] = args[1]
			args = args[2:]
		case "--region":
			if len(args) < 2 {
				return nil, errors.New("missing --region value")
			}
			query["Region"] = args[1]
			args = args[2:]
		case "--project-name":
			if len(args) < 2 {
				return nil, errors.New("missing --project-name value")
			}
			query["ProjectName"] = args[1]
			args = args[2:]
		case "--fuzzy-search-key":
			if len(args) < 2 {
				return nil, errors.New("missing --fuzzy-search-key value")
			}
			query["FuzzySearchKey"] = args[1]
			args = args[2:]
		case "--description":
			if len(args) < 2 {
				return nil, errors.New("missing --description value")
			}
			query["Description"] = args[1]
			args = args[2:]
		case "--tags":
			if len(args) < 2 {
				return nil, errors.New("missing --tags value")
			}
			s, err := util.ReadStringMaybeFile(args[1])
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(s) != "" {
				query["Tags"] = s
			}
			args = args[2:]
		case "--is-full-name":
			query["IsFullName"] = "true"
			args = args[1:]
		case "--no-is-full-name":
			query["IsFullName"] = "false"
			args = args[1:]
		case "--favourite":
			query["Favourite"] = "true"
			args = args[1:]
		case "--no-favourite":
			query["Favourite"] = "false"
			args = args[1:]
		case "--order-by-project":
			query["OrderByProject"] = "true"
			args = args[1:]
		case "--no-order-by-project":
			query["OrderByProject"] = "false"
			args = args[1:]
		default:
			return nil, errors.New("unknown flag: " + args[0])
		}
	}
	if strings.TrimSpace(query["TopicName"]) != "" && strings.TrimSpace(query["TopicId"]) != "" {
		return nil, errors.New("TopicName and TopicId cannot be provided together")
	}
	body, _ := util.MustJSON(map[string]any{})
	return ctx.Do("GET", "/DescribeTopics", query, nil, body)
}

func topicGet(ctx *Context, args []string) (any, error) {
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
	return ctx.Do("GET", "/DescribeTopic", map[string]string{"TopicId": topicID}, nil, body)
}

func topicCreate(ctx *Context, args []string) (any, error) {
	var (
		projectID         string
		topicName         string
		description       string
		ttl               int
		shards            int
		autoSplit         bool
		maxSplit          int
		enableTrackingSet bool
		enableTracking    bool
		meteringMode      string
		logPublicIPSet    bool
		logPublicIP       bool
		enableHotTtlSet   bool
		enableHotTtl      bool
		hotTtl            int
		coldTtl           int
		archiveTtl        int
		timeKey           string
		timeFormat        string
		encryptArg        string
		tagsArg           string
		requestArg        string
	)
	ttl = 30
	shards = 2
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
			shards = v
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
			maxSplit = v
			args = args[2:]
		case "--enable-tracking":
			enableTrackingSet = true
			enableTracking = true
			args = args[1:]
		case "--disable-tracking":
			enableTrackingSet = true
			enableTracking = false
			args = args[1:]
		case "--metering-mode":
			if len(args) < 2 {
				return nil, errors.New("missing --metering-mode value")
			}
			meteringMode = args[1]
			args = args[2:]
		case "--log-public-ip":
			logPublicIPSet = true
			logPublicIP = true
			args = args[1:]
		case "--no-log-public-ip":
			logPublicIPSet = true
			logPublicIP = false
			args = args[1:]
		case "--enable-hot-ttl":
			enableHotTtlSet = true
			enableHotTtl = true
			args = args[1:]
		case "--disable-hot-ttl":
			enableHotTtlSet = true
			enableHotTtl = false
			args = args[1:]
		case "--hot-ttl":
			if len(args) < 2 {
				return nil, errors.New("missing --hot-ttl value")
			}
			v, err := strconv.Atoi(args[1])
			if err != nil {
				return nil, err
			}
			hotTtl = v
			args = args[2:]
		case "--cold-ttl":
			if len(args) < 2 {
				return nil, errors.New("missing --cold-ttl value")
			}
			v, err := strconv.Atoi(args[1])
			if err != nil {
				return nil, err
			}
			coldTtl = v
			args = args[2:]
		case "--archive-ttl":
			if len(args) < 2 {
				return nil, errors.New("missing --archive-ttl value")
			}
			v, err := strconv.Atoi(args[1])
			if err != nil {
				return nil, err
			}
			archiveTtl = v
			args = args[2:]
		case "--time-key":
			if len(args) < 2 {
				return nil, errors.New("missing --time-key value")
			}
			timeKey = args[1]
			args = args[2:]
		case "--time-format":
			if len(args) < 2 {
				return nil, errors.New("missing --time-format value")
			}
			timeFormat = args[1]
			args = args[2:]
		case "--encrypt-conf":
			if len(args) < 2 {
				return nil, errors.New("missing --encrypt-conf value")
			}
			encryptArg = args[1]
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
	if (strings.TrimSpace(timeKey) == "") != (strings.TrimSpace(timeFormat) == "") {
		return nil, errors.New("time-key and time-format must be provided together")
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
	if shards > 0 {
		req["ShardCount"] = shards
	}
	if autoSplit {
		req["AutoSplit"] = true
	}
	if maxSplit > 0 {
		req["MaxSplitShard"] = maxSplit
	}
	if enableTrackingSet {
		req["EnableTracking"] = enableTracking
	}
	if strings.TrimSpace(meteringMode) != "" {
		req["MeteringMode"] = meteringMode
	}
	if logPublicIPSet {
		req["LogPublicIP"] = logPublicIP
	}
	if enableHotTtlSet {
		req["EnableHotTtl"] = enableHotTtl
	}
	if hotTtl > 0 {
		req["HotTtl"] = hotTtl
	}
	if coldTtl > 0 {
		req["ColdTtl"] = coldTtl
	}
	if archiveTtl > 0 {
		req["ArchiveTtl"] = archiveTtl
	}
	if strings.TrimSpace(timeKey) != "" {
		req["TimeKey"] = timeKey
		req["TimeFormat"] = timeFormat
	}
	if strings.TrimSpace(encryptArg) != "" {
		m, err := util.ReadJSONObjectMaybeFile(encryptArg)
		if err != nil {
			return nil, err
		}
		req["EncryptConf"] = m
	}
	if strings.TrimSpace(tagsArg) != "" {
		a, err := util.ReadJSONArrayMaybeFile(tagsArg)
		if err != nil {
			return nil, err
		}
		req["Tags"] = a
	}
	if reqAuto, ok := req["AutoSplit"].(bool); ok && reqAuto && maxSplit == 0 {
		if _, ok := req["MaxSplitShard"]; !ok {
			return nil, errors.New("missing --max-split-shard when AutoSplit is true")
		}
	}
	body, err := util.MustJSON(req)
	if err != nil {
		return nil, err
	}
	return ctx.Do("POST", "/CreateTopic", nil, nil, body)
}

func topicModify(ctx *Context, args []string) (any, error) {
	var (
		topicID           string
		topicName         string
		description       string
		ttl               int
		autoSplitSet      bool
		autoSplit         bool
		maxSplit          int
		enableTrackingSet bool
		enableTracking    bool
		favSet            bool
		fav               bool
		meteringMode      string
		logPublicIPSet    bool
		logPublicIP       bool
		enableHotTtlSet   bool
		enableHotTtl      bool
		hotTtl            int
		coldTtl           int
		archiveTtl        int
		timeKey           string
		timeFormat        string
		encryptArg        string
		requestArg        string
	)
	for len(args) > 0 {
		switch args[0] {
		case "--topic-id":
			if len(args) < 2 {
				return nil, errors.New("missing --topic-id value")
			}
			topicID = args[1]
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
		case "--auto-split":
			autoSplitSet = true
			autoSplit = true
			args = args[1:]
		case "--no-auto-split":
			autoSplitSet = true
			autoSplit = false
			args = args[1:]
		case "--max-split-shard":
			if len(args) < 2 {
				return nil, errors.New("missing --max-split-shard value")
			}
			v, err := strconv.Atoi(args[1])
			if err != nil {
				return nil, err
			}
			maxSplit = v
			args = args[2:]
		case "--enable-tracking":
			enableTrackingSet = true
			enableTracking = true
			args = args[1:]
		case "--disable-tracking":
			enableTrackingSet = true
			enableTracking = false
			args = args[1:]
		case "--favourite":
			favSet = true
			fav = true
			args = args[1:]
		case "--no-favourite":
			favSet = true
			fav = false
			args = args[1:]
		case "--metering-mode":
			if len(args) < 2 {
				return nil, errors.New("missing --metering-mode value")
			}
			meteringMode = args[1]
			args = args[2:]
		case "--log-public-ip":
			logPublicIPSet = true
			logPublicIP = true
			args = args[1:]
		case "--no-log-public-ip":
			logPublicIPSet = true
			logPublicIP = false
			args = args[1:]
		case "--enable-hot-ttl":
			enableHotTtlSet = true
			enableHotTtl = true
			args = args[1:]
		case "--disable-hot-ttl":
			enableHotTtlSet = true
			enableHotTtl = false
			args = args[1:]
		case "--hot-ttl":
			if len(args) < 2 {
				return nil, errors.New("missing --hot-ttl value")
			}
			v, err := strconv.Atoi(args[1])
			if err != nil {
				return nil, err
			}
			hotTtl = v
			args = args[2:]
		case "--cold-ttl":
			if len(args) < 2 {
				return nil, errors.New("missing --cold-ttl value")
			}
			v, err := strconv.Atoi(args[1])
			if err != nil {
				return nil, err
			}
			coldTtl = v
			args = args[2:]
		case "--archive-ttl":
			if len(args) < 2 {
				return nil, errors.New("missing --archive-ttl value")
			}
			v, err := strconv.Atoi(args[1])
			if err != nil {
				return nil, err
			}
			archiveTtl = v
			args = args[2:]
		case "--time-key":
			if len(args) < 2 {
				return nil, errors.New("missing --time-key value")
			}
			timeKey = args[1]
			args = args[2:]
		case "--time-format":
			if len(args) < 2 {
				return nil, errors.New("missing --time-format value")
			}
			timeFormat = args[1]
			args = args[2:]
		case "--encrypt-conf":
			if len(args) < 2 {
				return nil, errors.New("missing --encrypt-conf value")
			}
			encryptArg = args[1]
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
	if topicID == "" {
		return nil, errors.New("missing --topic-id")
	}
	if (strings.TrimSpace(timeKey) == "") != (strings.TrimSpace(timeFormat) == "") {
		return nil, errors.New("time-key and time-format must be provided together")
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
	if strings.TrimSpace(topicName) != "" {
		req["TopicName"] = topicName
	}
	if strings.TrimSpace(description) != "" {
		req["Description"] = description
	}
	if ttl > 0 {
		req["Ttl"] = ttl
	}
	if autoSplitSet {
		req["AutoSplit"] = autoSplit
	}
	if maxSplit > 0 {
		req["MaxSplitShard"] = maxSplit
	}
	if enableTrackingSet {
		req["EnableTracking"] = enableTracking
	}
	if favSet {
		req["Favourite"] = fav
	}
	if strings.TrimSpace(meteringMode) != "" {
		req["MeteringMode"] = meteringMode
	}
	if logPublicIPSet {
		req["LogPublicIP"] = logPublicIP
	}
	if enableHotTtlSet {
		req["EnableHotTtl"] = enableHotTtl
	}
	if hotTtl > 0 {
		req["HotTtl"] = hotTtl
	}
	if coldTtl > 0 {
		req["ColdTtl"] = coldTtl
	}
	if archiveTtl > 0 {
		req["ArchiveTtl"] = archiveTtl
	}
	if strings.TrimSpace(timeKey) != "" {
		req["TimeKey"] = timeKey
		req["TimeFormat"] = timeFormat
	}
	if strings.TrimSpace(encryptArg) != "" {
		m, err := util.ReadJSONObjectMaybeFile(encryptArg)
		if err != nil {
			return nil, err
		}
		req["EncryptConf"] = m
	}
	if reqAuto, ok := req["AutoSplit"].(bool); ok && reqAuto && maxSplit == 0 {
		if _, ok := req["MaxSplitShard"]; !ok {
			return nil, errors.New("missing --max-split-shard when AutoSplit is true")
		}
	}

	body, err := util.MustJSON(req)
	if err != nil {
		return nil, err
	}
	return ctx.Do("PUT", "/ModifyTopic", nil, nil, body)
}

func topicDelete(ctx *Context, args []string) (any, error) {
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
	return ctx.Do("DELETE", "/DeleteTopic", nil, nil, body)
}
