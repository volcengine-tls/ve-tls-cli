package cli

import (
	"errors"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/util"
)

func parseTopicListQuery(args []string, allowCursor bool) (map[string]string, error) {
	query := map[string]string{}
	for len(args) > 0 {
		switch args[0] {
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
		case "--cursor":
			if !allowCursor {
				return nil, errors.New("unknown flag: " + args[0])
			}
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
	return query, nil
}
