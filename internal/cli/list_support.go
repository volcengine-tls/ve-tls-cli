package cli

import (
	"errors"
	"strconv"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/util"
)

const pageAllDefaultPageSize = 100

func extractBoolFlag(args []string, flag string) ([]string, bool) {
	out := make([]string, 0, len(args))
	found := false
	for _, arg := range args {
		if arg == flag {
			found = true
			continue
		}
		out = append(out, arg)
	}
	return out, found
}

func listAllByPageNumber(ctx *Context, path string, baseQuery map[string]string, listField string) (map[string]any, error) {
	query, pageSize, err := preparePageAllPageNumberQuery(baseQuery)
	if err != nil {
		return nil, errors.New(strings.ReplaceAll(err.Error(), "--page-all", "--all"))
	}

	all := make([]any, 0)
	var last map[string]any
	pageCount := 0
	for page := 1; page <= 1000; page++ {
		query["PageNumber"] = strconv.Itoa(page)
		body, _ := util.MustJSON(map[string]any{})
		out, err := ctx.Do("GET", path, query, nil, body)
		if err != nil {
			return nil, err
		}
		resp, ok := out.(map[string]any)
		if !ok {
			return nil, errors.New("unexpected list response")
		}
		last = resp
		pageCount++
		rows, ok := toAnySlice(resp[listField])
		if !ok {
			return nil, errors.New("unexpected list field: " + listField)
		}
		all = append(all, rows...)
		total, hasTotal := anyToInt(resp["Total"])
		if len(rows) == 0 || len(rows) < pageSize {
			break
		}
		if hasTotal && len(all) >= total {
			break
		}
	}
	if last == nil {
		last = map[string]any{}
	}
	setPaginationMeta(ctx, pageCount, pageSize)
	last[listField] = all
	last["Total"] = len(all)
	return last, nil
}

func runGeneratedActionAll(ctx *Context, op apiActionOp, actionName string, path string, query map[string]string, header map[string]string, body []byte) (any, error) {
	if !supportsGeneratedActionAll(op) {
		return nil, errors.New("--all is only supported for paginated plural Describe actions")
	}
	switch generatedPaginationMode(op) {
	case "page-number":
		return generatedAllByPageNumber(ctx, actionName, path, query, header, body)
	case "cursor":
		return generatedAllByCursor(ctx, actionName, path, query, header, body)
	default:
		return nil, errors.New("--all is only supported for paginated plural Describe actions")
	}
}

func supportsGeneratedActionAll(op apiActionOp) bool {
	action := strings.TrimSpace(op.Cmd.Action)
	if !strings.HasPrefix(action, "Describe") || !strings.HasSuffix(action, "s") {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(op.Cmd.Method), "GET") {
		return false
	}
	return generatedPaginationMode(op) != ""
}

func generatedPaginationMode(op apiActionOp) string {
	hasPageNumber := false
	hasCursor := false
	for _, p := range op.Cmd.Params {
		if !strings.EqualFold(strings.TrimSpace(p.In), "query") {
			continue
		}
		switch strings.TrimSpace(p.Name) {
		case "PageNumber":
			hasPageNumber = true
		case "Cursor":
			hasCursor = true
		}
	}
	if hasPageNumber {
		return "page-number"
	}
	if hasCursor {
		return "cursor"
	}
	return ""
}

func generatedAllByPageNumber(ctx *Context, actionName string, path string, baseQuery map[string]string, header map[string]string, body []byte) (map[string]any, error) {
	query, pageSize, err := preparePageAllPageNumberQuery(baseQuery)
	if err != nil {
		return nil, err
	}

	all := make([]any, 0)
	listField := ""
	var last map[string]any
	pageCount := 0
	for page := 1; page <= 1000; page++ {
		query["PageNumber"] = strconv.Itoa(page)
		out, err := ctx.Do("GET", path, query, header, body)
		if err != nil {
			return nil, err
		}
		resp, ok := out.(map[string]any)
		if !ok {
			return nil, errors.New("unexpected list response")
		}
		last = resp
		pageCount++
		if listField == "" {
			listField = detectGeneratedListField(actionName, resp)
			if listField == "" {
				return nil, errors.New("cannot infer list field for --all")
			}
		}
		rows, ok := toAnySlice(resp[listField])
		if !ok {
			return nil, errors.New("unexpected list field: " + listField)
		}
		all = append(all, rows...)
		total, hasTotal := anyToInt(resp["Total"])
		if len(rows) == 0 || len(rows) < pageSize {
			break
		}
		if hasTotal && len(all) >= total {
			break
		}
	}
	if last == nil {
		last = map[string]any{}
	}
	setPaginationMeta(ctx, pageCount, pageSize)
	last[listField] = all
	if _, hasTotal := last["Total"]; hasTotal {
		last["Total"] = len(all)
	}
	return last, nil
}

func generatedAllByCursor(ctx *Context, actionName string, path string, baseQuery map[string]string, header map[string]string, body []byte) (map[string]any, error) {
	query := cloneStringMap(baseQuery)
	if strings.TrimSpace(query["Cursor"]) != "" {
		return nil, errors.New("--all cannot be used with Cursor")
	}
	if strings.TrimSpace(query["PageNumber"]) != "" {
		return nil, errors.New("--all cannot be used with PageNumber")
	}

	all := make([]any, 0)
	listField := ""
	var last map[string]any
	pageCount := 0
	for page := 0; page < 1000; page++ {
		out, err := ctx.Do("GET", path, query, header, body)
		if err != nil {
			return nil, err
		}
		resp, ok := out.(map[string]any)
		if !ok {
			return nil, errors.New("unexpected list response")
		}
		last = resp
		pageCount++
		if listField == "" {
			listField = detectGeneratedListField(actionName, resp)
			if listField == "" {
				return nil, errors.New("cannot infer list field for --all")
			}
		}
		rows, ok := toAnySlice(resp[listField])
		if !ok {
			return nil, errors.New("unexpected list field: " + listField)
		}
		all = append(all, rows...)
		nextCursor, _ := resp["Cursor"].(string)
		if len(rows) == 0 || strings.TrimSpace(nextCursor) == "" {
			break
		}
		query["Cursor"] = nextCursor
	}
	if last == nil {
		last = map[string]any{}
	}
	setPaginationMeta(ctx, pageCount, parsePositiveInt(query["PageSize"]))
	last[listField] = all
	if _, hasTotal := last["Total"]; hasTotal {
		last["Total"] = len(all)
	}
	return last, nil
}

func preparePageAllPageNumberQuery(baseQuery map[string]string) (map[string]string, int, error) {
	query := cloneStringMap(baseQuery)
	if strings.TrimSpace(query["PageNumber"]) != "" {
		return nil, 0, errors.New("--page-all cannot be used with PageNumber")
	}
	if strings.TrimSpace(query["Cursor"]) != "" {
		return nil, 0, errors.New("--page-all cannot be used with Cursor")
	}
	pageSize := parsePositiveInt(query["PageSize"])
	if pageSize <= 0 {
		pageSize = pageAllDefaultPageSize
	}
	query["PageSize"] = strconv.Itoa(pageSize)
	return query, pageSize, nil
}

func parsePositiveInt(raw string) int {
	if v, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && v > 0 {
		return v
	}
	return 0
}

func setPaginationMeta(ctx *Context, pageCount int, pageSize int) {
	if ctx == nil || pageCount <= 0 {
		return
	}
	meta := map[string]any{
		"mode":      "page_all",
		"pageCount": pageCount,
		"merged":    true,
	}
	if pageSize > 0 {
		meta["pageSize"] = pageSize
	}
	ctx.PaginationMeta = meta
}

func detectGeneratedListField(actionName string, resp map[string]any) string {
	candidate := strings.TrimPrefix(strings.TrimSpace(actionName), "Describe")
	if candidate != "" {
		if _, ok := toAnySlice(resp[candidate]); ok {
			return candidate
		}
	}
	found := ""
	for key, value := range resp {
		if _, ok := toAnySlice(value); !ok {
			continue
		}
		if found != "" {
			return ""
		}
		found = key
	}
	return found
}

func supportsTableOutput(ctx *Context) bool {
	switch strings.TrimSpace(ctx.Action) {
	case "project.list", "project.get",
		"topic.list", "topic.get",
		"metric-topic.list", "metric-topic.get",
		"index.get", "log.search":
		return true
	default:
		return false
	}
}

func toAnySlice(v any) ([]any, bool) {
	switch vv := v.(type) {
	case []any:
		return vv, true
	case []map[string]any:
		out := make([]any, 0, len(vv))
		for _, item := range vv {
			out = append(out, item)
		}
		return out, true
	default:
		return nil, false
	}
}

func anyToInt(v any) (int, bool) {
	switch vv := v.(type) {
	case int:
		return vv, true
	case int64:
		return int(vv), true
	case float64:
		return int(vv), true
	default:
		return 0, false
	}
}
