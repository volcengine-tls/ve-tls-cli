package cli

import (
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/execution"
	"github.com/volcengine-tls/ve-tls-cli/internal/jsonx"
)

// inspectDownloadTaskResult describes task state without making another request.
func inspectDownloadTaskResult(operationID string, input execution.Input, data any) (map[string]any, string, error) {
	switch operationID {
	case "log.create-download-task":
		response, ok := data.(map[string]any)
		if !ok {
			return nil, "", invalidDownloadTaskResponse()
		}
		taskID, ok := nonemptyDownloadTaskString(response["TaskId"])
		if !ok {
			return nil, "", invalidDownloadTaskResponse()
		}
		async := map[string]any{
			"taskId":   taskID,
			"state":    "accepted",
			"terminal": false,
			"match":    map[string]any{"field": "Tasks[].TaskId", "equals": taskID},
			"scope":    "Match Tasks[].TaskId across pages; TaskName is a fuzzy filter.",
		}
		request, _ := input.Body.JSON.(map[string]any)
		if request == nil && len(input.Body.Raw) > 0 {
			_ = jsonx.Unmarshal(input.Body.Raw, &request)
		}
		if topicID, ok := nonemptyDownloadTaskString(request["TopicId"]); ok {
			query := map[string]any{"TopicId": topicID, "PageNumber": 1, "PageSize": 100}
			if taskName, ok := nonemptyDownloadTaskString(request["TaskName"]); ok {
				query["TaskName"] = taskName
			}
			async["next"] = &execution.Continuation{
				OperationID: "log.describe-download-tasks",
				Input:       map[string]any{"query": query},
				PageAll:     false,
			}
		}
		return map[string]any{"async": async}, execution.OutcomeAccepted, nil

	case "log.describe-download-tasks":
		response, ok := data.(map[string]any)
		if !ok {
			return nil, "", invalidDownloadTaskResponse()
		}
		var tasks []any
		switch rows := response["Tasks"].(type) {
		case []any:
			tasks = rows
		case []map[string]any:
			tasks = make([]any, len(rows))
			for i := range rows {
				tasks[i] = rows[i]
			}
		default:
			return nil, "", invalidDownloadTaskResponse()
		}
		asyncTasks := make([]map[string]any, 0, len(tasks))
		for _, rawTask := range tasks {
			task, _ := rawTask.(map[string]any)
			taskID, hasID := nonemptyDownloadTaskString(task["TaskId"])
			status, _ := task["TaskStatus"].(string)
			item := map[string]any{
				"taskId":    nil,
				"rawStatus": task["TaskStatus"],
				"state":     "unknown",
				"terminal":  false,
			}
			if hasID {
				item["taskId"] = taskID
				switch status {
				case "wait":
					item["state"] = "pending"
				case "creating":
					item["state"] = "running"
				case "success":
					item["state"], item["terminal"] = "succeeded", true
					item["next"] = downloadURLContinuation(taskID)
				case "created_cut":
					item["state"], item["terminal"] = "partial", true
					item["resultComplete"] = false
					item["possibleNext"] = downloadURLContinuation(taskID)
					item["possibleNextCondition"] = "Download availability is not guaranteed for created_cut tasks."
				case "fail":
					item["state"], item["terminal"] = "failed", true
				}
			}
			asyncTasks = append(asyncTasks, item)
		}
		return map[string]any{"asyncTasks": asyncTasks}, "", nil
	default:
		return nil, "", nil
	}
}

func nonemptyDownloadTaskString(value any) (string, bool) {
	text, ok := value.(string)
	return text, ok && strings.TrimSpace(text) != ""
}

func downloadURLContinuation(taskID string) *execution.Continuation {
	return &execution.Continuation{
		OperationID: "log.describe-download-url",
		Input:       map[string]any{"query": map[string]any{"TaskId": taskID}},
		PageAll:     false,
	}
}

func invalidDownloadTaskResponse() error {
	return &execution.Failure{
		Kind: "protocol", Code: "InvalidTaskResponse", Stage: "decode",
		Outcome: execution.OutcomeUnknown, Message: "download task response is invalid",
	}
}
