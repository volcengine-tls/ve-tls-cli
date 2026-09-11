//go:build human

package cli

import (
	"errors"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
	"github.com/volcengine-tls/ve-tls-cli/internal/execution"
	"github.com/volcengine-tls/ve-tls-cli/internal/util"
)

func runHostGroup(ctx *Context, args []string) (any, error) {
	return runSubcommandGroup(args, usageHostGroup(), nil, shortcutCommandHelpLookup("host-group"), func(command string, commandArgs []string) (any, error) {
		ctx.Action = "host-group." + strings.TrimSpace(command)
		if out, handled, err := maybeHandleShortcutMeta("host-group", command, commandArgs); handled {
			return out, err
		}
		switch command {
		case "list":
			return hostGroupList(ctx, commandArgs)
		case "get":
			return hostGroupGet(ctx, commandArgs)
		case "create":
			return hostGroupCreate(ctx, commandArgs)
		case "modify":
			return hostGroupModify(ctx, commandArgs)
		case "delete":
			return hostGroupDelete(ctx, commandArgs)
		default:
			return nil, errors.New("unknown host-group command: " + command)
		}
	})
}

func hostGroupList(ctx *Context, args []string) (any, error) {
	args, all := extractBoolFlag(args, "--all")
	query := map[string]string{}
	for len(args) > 0 {
		switch args[0] {
		case "--host-group-id":
			if len(args) < 2 {
				return nil, errors.New("missing --host-group-id value")
			}
			query["HostGroupId"] = args[1]
			args = args[2:]
		case "--host-group-name":
			if len(args) < 2 {
				return nil, errors.New("missing --host-group-name value")
			}
			query["HostGroupName"] = args[1]
			args = args[2:]
		case "--host-identifier":
			if len(args) < 2 {
				return nil, errors.New("missing --host-identifier value")
			}
			query["HostIdentifier"] = args[1]
			args = args[2:]
		case "--iam-project-name":
			if len(args) < 2 {
				return nil, errors.New("missing --iam-project-name value")
			}
			query["IamProjectName"] = args[1]
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
		case "--auto-update":
			query["AutoUpdate"] = "true"
			args = args[1:]
		case "--no-auto-update":
			query["AutoUpdate"] = "false"
			args = args[1:]
		case "--service-logging":
			query["ServiceLogging"] = "true"
			args = args[1:]
		case "--no-service-logging":
			query["ServiceLogging"] = "false"
			args = args[1:]
		case "--hidden":
			query["Hidden"] = "true"
			args = args[1:]
		case "--no-hidden":
			query["Hidden"] = "false"
			args = args[1:]
		default:
			return nil, errors.New("unknown flag: " + args[0])
		}
	}
	if all {
		return executeShortcutOperation(ctx, shortcutExecutionRequest{
			OperationID: "host-group.describe-host-groups-v2",
			Input: execution.Input{
				Query: shortcutQueryInput(query),
				Body:  shortcutEmptyJSONBodyInput(),
			},
			PageAll: true,
			LegacyPageAll: &legacyPageAllPolicy{
				ListField:  "HostGroupHostsRulesInfos",
				ForceTotal: true,
				PaginationOverride: &contract.PaginationSpec{
					Mode:            contract.PaginationPageNumber,
					PageNumberParam: "PageNumber",
					PageSizeParam:   "PageSize",
					ItemsField:      "HostGroupHostsRulesInfos",
					TotalField:      "Total",
					DefaultPageSize: 100,
					MaxPages:        1000,
				},
			},
		})
	}
	return executeShortcutOperation(ctx, shortcutExecutionRequest{
		OperationID: "host-group.describe-host-groups-v2",
		Input: execution.Input{
			Query: shortcutQueryInput(query),
			Body:  shortcutEmptyJSONBodyInput(),
		},
	})
}

func hostGroupGet(ctx *Context, args []string) (any, error) {
	var hostGroupID string
	for len(args) > 0 {
		switch args[0] {
		case "--host-group-id":
			if len(args) < 2 {
				return nil, errors.New("missing --host-group-id value")
			}
			hostGroupID = args[1]
			args = args[2:]
		default:
			return nil, errors.New("unknown flag: " + args[0])
		}
	}
	hostGroupID = strings.TrimSpace(hostGroupID)
	if hostGroupID == "" {
		return nil, errors.New("missing --host-group-id")
	}
	return executeShortcutOperation(ctx, shortcutExecutionRequest{
		OperationID: "host-group.describe-host-group-v2",
		Input: execution.Input{
			Query: shortcutQueryInput(map[string]string{"HostGroupId": hostGroupID}),
			Body:  shortcutEmptyJSONBodyInput(),
		},
	})
}

func hostGroupCreate(ctx *Context, args []string) (any, error) {
	req, err := buildHostGroupBody(args, false)
	if err != nil {
		return nil, err
	}
	return executeShortcutOperation(ctx, shortcutExecutionRequest{
		OperationID: "host-group.create",
		Input: execution.Input{
			Body: shortcutJSONBodyInput(req),
		},
	})
}

func hostGroupModify(ctx *Context, args []string) (any, error) {
	req, err := buildHostGroupBody(args, true)
	if err != nil {
		return nil, err
	}
	return executeShortcutOperation(ctx, shortcutExecutionRequest{
		OperationID: "host-group.modify-host-group",
		Input: execution.Input{
			Body: shortcutJSONBodyInput(req),
		},
	})
}

func hostGroupDelete(ctx *Context, args []string) (any, error) {
	var hostGroupID string
	for len(args) > 0 {
		switch args[0] {
		case "--host-group-id":
			if len(args) < 2 {
				return nil, errors.New("missing --host-group-id value")
			}
			hostGroupID = args[1]
			args = args[2:]
		default:
			return nil, errors.New("unknown flag: " + args[0])
		}
	}
	hostGroupID = strings.TrimSpace(hostGroupID)
	if hostGroupID == "" {
		return nil, errors.New("missing --host-group-id")
	}
	return executeShortcutOperation(ctx, shortcutExecutionRequest{
		OperationID: "host-group.delete-host-group",
		Input: execution.Input{
			Body: shortcutJSONBodyInput(map[string]any{"HostGroupId": hostGroupID}),
		},
	})
}

func buildHostGroupBody(args []string, modify bool) (map[string]any, error) {
	var (
		hostGroupID     string
		hostGroupName   string
		hostGroupType   string
		hostIdentifier  string
		updateStartTime string
		updateEndTime   string
		iamProjectName  string
		hostIPListArg   string
		requestArg      string
		autoUpdateSet   bool
		autoUpdate      bool
		serviceLogSet   bool
		serviceLogging  bool
	)
	for len(args) > 0 {
		switch args[0] {
		case "--host-group-id":
			if len(args) < 2 {
				return nil, errors.New("missing --host-group-id value")
			}
			hostGroupID = args[1]
			args = args[2:]
		case "--host-group-name":
			if len(args) < 2 {
				return nil, errors.New("missing --host-group-name value")
			}
			hostGroupName = args[1]
			args = args[2:]
		case "--host-group-type":
			if len(args) < 2 {
				return nil, errors.New("missing --host-group-type value")
			}
			hostGroupType = args[1]
			args = args[2:]
		case "--host-ip-list":
			if len(args) < 2 {
				return nil, errors.New("missing --host-ip-list value")
			}
			hostIPListArg = args[1]
			args = args[2:]
		case "--host-identifier":
			if len(args) < 2 {
				return nil, errors.New("missing --host-identifier value")
			}
			hostIdentifier = args[1]
			args = args[2:]
		case "--auto-update":
			autoUpdateSet = true
			autoUpdate = true
			args = args[1:]
		case "--no-auto-update":
			autoUpdateSet = true
			autoUpdate = false
			args = args[1:]
		case "--update-start-time":
			if len(args) < 2 {
				return nil, errors.New("missing --update-start-time value")
			}
			updateStartTime = args[1]
			args = args[2:]
		case "--update-end-time":
			if len(args) < 2 {
				return nil, errors.New("missing --update-end-time value")
			}
			updateEndTime = args[1]
			args = args[2:]
		case "--service-logging":
			serviceLogSet = true
			serviceLogging = true
			args = args[1:]
		case "--no-service-logging":
			serviceLogSet = true
			serviceLogging = false
			args = args[1:]
		case "--iam-project-name":
			if len(args) < 2 {
				return nil, errors.New("missing --iam-project-name value")
			}
			iamProjectName = args[1]
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
	if modify {
		maybeSetStringField(req, "HostGroupId", hostGroupID)
		if strings.TrimSpace(requestArg) == "" && strings.TrimSpace(hostGroupID) == "" {
			return nil, errors.New("missing --host-group-id")
		}
	} else if strings.TrimSpace(requestArg) == "" {
		for _, pair := range []struct {
			name  string
			value string
		}{
			{"--host-group-name", hostGroupName},
			{"--host-group-type", hostGroupType},
		} {
			if strings.TrimSpace(pair.value) == "" {
				return nil, errors.New("missing " + pair.name)
			}
		}
	}
	maybeSetStringField(req, "HostGroupName", hostGroupName)
	maybeSetStringField(req, "HostGroupType", hostGroupType)
	maybeSetStringField(req, "HostIdentifier", hostIdentifier)
	maybeSetStringField(req, "UpdateStartTime", updateStartTime)
	maybeSetStringField(req, "UpdateEndTime", updateEndTime)
	maybeSetStringField(req, "IamProjectName", iamProjectName)
	maybeSetBoolField(req, "AutoUpdate", autoUpdateSet, autoUpdate)
	maybeSetBoolField(req, "ServiceLogging", serviceLogSet, serviceLogging)
	if strings.TrimSpace(hostIPListArg) != "" {
		ips, err := util.ReadStringListMaybeFile(hostIPListArg)
		if err != nil {
			return nil, err
		}
		req["HostIpList"] = ips
	}
	return req, nil
}
