package cli

import (
	"errors"
	"strconv"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/util"
)

func runProject(ctx *Context, args []string) (any, error) {
	return runSubcommandGroup(args, usageProject(), nil, shortcutCommandHelpLookup("project"), func(command string, commandArgs []string) (any, error) {
		ctx.Action = "project." + strings.TrimSpace(command)
		if out, handled, err := maybeHandleShortcutMeta("project", command, commandArgs); handled {
			return out, err
		}
		switch command {
		case "list":
			return projectList(ctx, commandArgs)
		case "get":
			return projectGet(ctx, commandArgs)
		case "create":
			return projectCreate(ctx, commandArgs)
		case "modify":
			return projectModify(ctx, commandArgs)
		case "delete":
			return projectDelete(ctx, commandArgs)
		default:
			return nil, errors.New("unknown project command: " + command)
		}
	})
}

func projectList(ctx *Context, args []string) (any, error) {
	args, all := extractBoolFlag(args, "--all")
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
		case "--project-name":
			if len(args) < 2 {
				return nil, errors.New("missing --project-name value")
			}
			query["ProjectName"] = args[1]
			args = args[2:]
		case "--project-id":
			if len(args) < 2 {
				return nil, errors.New("missing --project-id value")
			}
			query["ProjectId"] = args[1]
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
		case "--is-full-name":
			query["IsFullName"] = "true"
			args = args[1:]
		case "--no-is-full-name":
			query["IsFullName"] = "false"
			args = args[1:]
		case "--iam-project-name":
			if len(args) < 2 {
				return nil, errors.New("missing --iam-project-name value")
			}
			query["IamProjectName"] = args[1]
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
		case "--favourite":
			query["Favourite"] = "true"
			args = args[1:]
		case "--no-favourite":
			query["Favourite"] = "false"
			args = args[1:]
		case "--topic-types":
			if len(args) < 2 {
				return nil, errors.New("missing --topic-types value")
			}
			query["TopicTypes"] = args[1]
			args = args[2:]
		default:
			return nil, errors.New("unknown flag: " + args[0])
		}
	}
	if all {
		return listAllByPageNumber(ctx, "/DescribeProjects", query, "Projects")
	}
	body, _ := util.MustJSON(map[string]any{})
	return ctx.Do("GET", "/DescribeProjects", query, nil, body)
}

func projectGet(ctx *Context, args []string) (any, error) {
	var projectID string
	var topicTypes string
	for len(args) > 0 {
		switch args[0] {
		case "--project-id":
			if len(args) < 2 {
				return nil, errors.New("missing --project-id value")
			}
			projectID = args[1]
			args = args[2:]
		case "--topic-types":
			if len(args) < 2 {
				return nil, errors.New("missing --topic-types value")
			}
			topicTypes = args[1]
			args = args[2:]
		default:
			return nil, errors.New("unknown flag: " + args[0])
		}
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, errors.New("missing --project-id")
	}
	body, _ := util.MustJSON(map[string]any{})
	query := map[string]string{"ProjectId": projectID}
	if strings.TrimSpace(topicTypes) != "" {
		query["TopicTypes"] = topicTypes
	}
	return ctx.Do("GET", "/DescribeProject", query, nil, body)
}

func projectCreate(ctx *Context, args []string) (any, error) {
	var (
		name        string
		description string
		iamProject  string
		region      string
		tagsArg     string
		requestArg  string
	)
	for len(args) > 0 {
		switch args[0] {
		case "--project-name":
			if len(args) < 2 {
				return nil, errors.New("missing --project-name value")
			}
			name = args[1]
			args = args[2:]
		case "--description":
			if len(args) < 2 {
				return nil, errors.New("missing --description value")
			}
			description = args[1]
			args = args[2:]
		case "--iam-project-name":
			if len(args) < 2 {
				return nil, errors.New("missing --iam-project-name value")
			}
			iamProject = args[1]
			args = args[2:]
		case "--region":
			if len(args) < 2 {
				return nil, errors.New("missing --region value")
			}
			region = args[1]
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
	if err := ctx.ResolveProfile(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(name) == "" {
		if strings.TrimSpace(requestArg) == "" {
			return nil, errors.New("missing --project-name")
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
	if strings.TrimSpace(name) != "" {
		req["ProjectName"] = name
	}
	if strings.TrimSpace(description) != "" {
		req["Description"] = description
	}
	if strings.TrimSpace(iamProject) != "" {
		req["IamProjectName"] = iamProject
	}
	r := strings.TrimSpace(region)
	if r == "" {
		r = ctx.profile.Region
	}
	if r != "" {
		req["Region"] = r
	}
	if strings.TrimSpace(tagsArg) != "" {
		a, err := util.ReadJSONArrayMaybeFile(tagsArg)
		if err != nil {
			return nil, err
		}
		req["Tags"] = a
	}

	body, err := util.MustJSON(req)
	if err != nil {
		return nil, err
	}
	return ctx.Do("POST", "/CreateProject", nil, nil, body)
}

func projectModify(ctx *Context, args []string) (any, error) {
	var (
		projectID   string
		projectName string
		description string
		favSet      bool
		fav         bool
		requestArg  string
	)
	for len(args) > 0 {
		switch args[0] {
		case "--project-id":
			if len(args) < 2 {
				return nil, errors.New("missing --project-id value")
			}
			projectID = args[1]
			args = args[2:]
		case "--project-name":
			if len(args) < 2 {
				return nil, errors.New("missing --project-name value")
			}
			projectName = args[1]
			args = args[2:]
		case "--description":
			if len(args) < 2 {
				return nil, errors.New("missing --description value")
			}
			description = args[1]
			args = args[2:]
		case "--favourite":
			favSet = true
			fav = true
			args = args[1:]
		case "--no-favourite":
			favSet = true
			fav = false
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
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, errors.New("missing --project-id")
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
	req["ProjectId"] = projectID
	if strings.TrimSpace(projectName) != "" {
		req["ProjectName"] = projectName
	}
	if strings.TrimSpace(description) != "" {
		req["Description"] = description
	}
	if favSet {
		req["Favourite"] = fav
	}
	body, err := util.MustJSON(req)
	if err != nil {
		return nil, err
	}
	return ctx.Do("PUT", "/ModifyProject", nil, nil, body)
}

func projectDelete(ctx *Context, args []string) (any, error) {
	var projectID string
	for len(args) > 0 {
		switch args[0] {
		case "--project-id":
			if len(args) < 2 {
				return nil, errors.New("missing --project-id value")
			}
			projectID = args[1]
			args = args[2:]
		default:
			return nil, errors.New("unknown flag: " + args[0])
		}
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, errors.New("missing --project-id")
	}
	body, err := util.MustJSON(map[string]any{"ProjectId": projectID})
	if err != nil {
		return nil, err
	}
	return ctx.Do("DELETE", "/DeleteProject", nil, nil, body)
}

func atoiDefault(s string, d int) int {
	i, err := strconv.Atoi(s)
	if err != nil {
		return d
	}
	return i
}
