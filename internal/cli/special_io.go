package cli

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	tlssdk "github.com/volcengine/volc-sdk-golang/service/tls"
	tlspb "github.com/volcengine/volc-sdk-golang/service/tls/pb"

	"github.com/volcengine-tls/ve-tls-cli/internal/output"
	"github.com/volcengine-tls/ve-tls-cli/internal/tlsapi"
	"github.com/volcengine-tls/ve-tls-cli/internal/util"
)

type requestFormat string

const (
	requestFormatJSON  requestFormat = "json"
	requestFormatJSONL requestFormat = "jsonl"
)

type apiIOMeta struct {
	Group         string
	Action        string
	Method        string
	Path          string
	RequestFormat requestFormat
	OutputFormat  output.Format
	OutputMode    string
}

type specialIOState struct {
	Compression string
}

type rawBinaryOutput struct {
	Data []byte
	Ext  string
}

type specialIOProfile string

const (
	specialIOProfileNone                specialIOProfile = ""
	specialIOProfilePutLogs             specialIOProfile = "putlogs"
	specialIOProfileWebTracks           specialIOProfile = "webtracks"
	specialIOProfileConsumeLogs         specialIOProfile = "consumelogs"
	specialIOProfileConsumeOriginalLogs specialIOProfile = "consumeoriginallogs"
	specialIOProfileConsumeKafkaLogs    specialIOProfile = "consumekafkalogs"
)

func prepareSpecialIORequest(meta apiIOMeta, header map[string]string, body []byte) (map[string]string, []byte, *specialIOState, bool, error) {
	profile := resolveSpecialIOProfile(meta)
	if profile == specialIOProfileNone {
		return cloneStringMap(header), body, nil, false, nil
	}

	outHeader := cloneStringMap(header)
	switch profile {
	case specialIOProfilePutLogs:
		list, err := parsePutLogsInput(meta.RequestFormat, body)
		if err != nil {
			return nil, nil, nil, true, err
		}
		applyPutLogsStatsHeaders(outHeader, list)
		compression := normalizeCompression(outHeader["x-tls-compresstype"])
		encoded, rawSize, err := tlssdk.GetPutLogsBody(compression, list)
		if err != nil {
			return nil, nil, nil, true, err
		}
		outHeader["Content-Type"] = "application/x-protobuf"
		outHeader["x-tls-bodyrawsize"] = strconv.Itoa(rawSize)
		if compression == "" {
			delete(outHeader, "x-tls-compresstype")
		} else {
			outHeader["x-tls-compresstype"] = compression
		}
		return outHeader, encoded, &specialIOState{Compression: compression}, true, nil
	case specialIOProfileWebTracks:
		req, err := parseWebTracksInput(meta.RequestFormat, body)
		if err != nil {
			return nil, nil, nil, true, err
		}
		compression := normalizeCompression(outHeader["x-tls-compresstype"])
		encoded, rawSize, err := tlssdk.GetWebTracksBody(compression, req)
		if err != nil {
			return nil, nil, nil, true, err
		}
		outHeader["Content-Type"] = "application/json"
		outHeader["x-tls-bodyrawsize"] = strconv.Itoa(rawSize)
		if compression == "" {
			delete(outHeader, "x-tls-compresstype")
		} else {
			outHeader["x-tls-compresstype"] = compression
		}
		return outHeader, encoded, &specialIOState{Compression: compression}, true, nil
	case specialIOProfileConsumeLogs, specialIOProfileConsumeOriginalLogs, specialIOProfileConsumeKafkaLogs:
		state, err := parseConsumeRequestState(body)
		if err != nil {
			return nil, nil, nil, true, err
		}
		return outHeader, body, state, true, nil
	default:
		return outHeader, body, nil, false, nil
	}
}

func decodeSpecialIOResponse(meta apiIOMeta, state *specialIOState, resp tlsapi.Response) (any, bool, error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, false, nil
	}
	profile := resolveSpecialIOProfile(meta)
	switch profile {
	case specialIOProfileConsumeLogs, specialIOProfileConsumeOriginalLogs, specialIOProfileConsumeKafkaLogs:
	default:
		return nil, false, nil
	}

	if isTrueValue(headerValue(resp.Header, "x-tls-is-kafka-records")) {
		if strings.ToLower(strings.TrimSpace(meta.OutputMode)) != "file" {
			return nil, true, errors.New("kafka record payload requires --output-mode file")
		}
		return rawBinaryOutput{
			Data: append([]byte(nil), resp.Body...),
			Ext:  ".bin",
		}, true, nil
	}

	if isTrueValue(headerValue(resp.Header, "x-tls-original")) {
		rawList, logs, err := decodeOriginalLogList(resp.Body)
		if err != nil {
			return nil, true, err
		}
		if meta.OutputFormat == output.FormatJSONL {
			return flattenLogGroupList(logs), true, nil
		}
		rawObj, err := toGenericJSON(rawList)
		if err != nil {
			return nil, true, err
		}
		return map[string]any{
			"Cursor":              strings.TrimSpace(headerValue(resp.Header, "x-tls-cursor")),
			"Count":               parseIntHeader(headerValue(resp.Header, "x-tls-count")),
			"RawLogGroupListList": rawObj,
		}, true, nil
	}

	compression := ""
	if state != nil {
		compression = normalizeCompression(state.Compression)
	}
	rawSize, err := parseRawBodySize(headerValue(resp.Header, "x-tls-bodyrawsize"), compression)
	if err != nil {
		return nil, true, err
	}
	logs, err := tlssdk.GetLogGroupList(compression, rawSize, resp.Body)
	if err != nil {
		return nil, true, err
	}
	if meta.OutputFormat == output.FormatJSONL {
		return flattenLogGroupList(logs), true, nil
	}
	logObj, err := toGenericJSON(logs)
	if err != nil {
		return nil, true, err
	}
	return map[string]any{
		"Cursor": strings.TrimSpace(headerValue(resp.Header, "x-tls-cursor")),
		"Count":  parseIntHeader(headerValue(resp.Header, "x-tls-count")),
		"Logs":   logObj,
	}, true, nil
}

func resolveSpecialIOProfile(meta apiIOMeta) specialIOProfile {
	actionKey := normalizeToken(meta.Group) + "." + normalizeActionToken(meta.Action)
	switch actionKey {
	case "log.putlogs":
		return specialIOProfilePutLogs
	case "log.webtracks":
		return specialIOProfileWebTracks
	case "log.consumelogs":
		return specialIOProfileConsumeLogs
	case "log.consumeoriginallogs":
		return specialIOProfileConsumeOriginalLogs
	case "log.consumekafkalogs", "log.consumeoriginalkafkalogs":
		return specialIOProfileConsumeKafkaLogs
	}

	switch strings.ToUpper(strings.TrimSpace(meta.Method)) + ":" + strings.ToLower(strings.TrimSpace(meta.Path)) {
	case "POST:/putlogs":
		return specialIOProfilePutLogs
	case "POST:/webtracks":
		return specialIOProfileWebTracks
	case "POST:/consumelogs":
		return specialIOProfileConsumeLogs
	case "POST:/consumeoriginallogs":
		return specialIOProfileConsumeOriginalLogs
	case "POST:/consumekafkalogs", "POST:/consumeoriginalkafkalogs":
		return specialIOProfileConsumeKafkaLogs
	default:
		return specialIOProfileNone
	}
}

func normalizeRequestFormat(f requestFormat) requestFormat {
	switch requestFormat(strings.ToLower(strings.TrimSpace(string(f)))) {
	case requestFormatJSONL:
		return requestFormatJSONL
	default:
		return requestFormatJSON
	}
}

func parsePutLogsInput(format requestFormat, body []byte) (*tlspb.LogGroupList, error) {
	if normalizeRequestFormat(format) == requestFormatJSONL {
		rows, err := parseJSONLObjects(body)
		if err != nil {
			return nil, err
		}
		out := &tlspb.LogGroupList{LogGroups: make([]*tlspb.LogGroup, 0, len(rows))}
		for _, row := range rows {
			group := &tlspb.LogGroup{
				Source:      stringField(row, "Source"),
				FileName:    stringField(row, "FileName"),
				ContextFlow: stringField(row, "ContextFlow"),
				LogTags:     parseLogTagsField(row["LogTags"]),
			}
			contents, err := parseLogContentsField(row["Contents"])
			if err != nil {
				return nil, err
			}
			logItem := &tlspb.Log{
				Time:     int64Field(row, "Time"),
				Contents: contents,
			}
			if timeNs, ok := uint32Field(row, "TimeNs"); ok {
				logItem.OptionalTimeNs = &tlspb.Log_TimeNs{TimeNs: timeNs}
			}
			group.Logs = []*tlspb.Log{logItem}
			out.LogGroups = append(out.LogGroups, group)
		}
		return out, nil
	}
	out := &tlspb.LogGroupList{}
	if len(bytesTrimSpaceLocal(body)) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return nil, err
	}
	return out, nil
}

func parseWebTracksInput(format requestFormat, body []byte) (*tlssdk.WebTracksRequest, error) {
	if normalizeRequestFormat(format) == requestFormatJSONL {
		rows, err := parseJSONLObjects(body)
		if err != nil {
			return nil, err
		}
		req := &tlssdk.WebTracksRequest{Logs: make([]map[string]string, 0, len(rows))}
		for _, row := range rows {
			source := stringField(row, "Source")
			if req.Source == "" {
				req.Source = source
			}
			if source != "" && req.Source != source {
				return nil, errors.New("jsonl webtracks rows must share the same Source")
			}
			req.Logs = append(req.Logs, stringifyMapField(row["Contents"]))
		}
		return req, nil
	}
	req := &tlssdk.WebTracksRequest{}
	if len(bytesTrimSpaceLocal(body)) == 0 {
		return req, nil
	}
	if err := json.Unmarshal(body, req); err != nil {
		return nil, err
	}
	return req, nil
}

func parseConsumeRequestState(body []byte) (*specialIOState, error) {
	state := &specialIOState{}
	trimmed := bytesTrimSpaceLocal(body)
	if len(trimmed) == 0 || string(trimmed) == "{}" {
		return state, nil
	}
	v, err := util.UnmarshalJSON(trimmed)
	if err != nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("consume request body must be json object")
	}
	if raw, ok := m["Compression"]; ok {
		state.Compression = normalizeCompression(toString(raw))
	}
	return state, nil
}

func parseJSONLObjects(body []byte) ([]map[string]any, error) {
	lines := strings.Split(string(body), "\n")
	out := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		v, err := util.UnmarshalJSON([]byte(line))
		if err != nil {
			return nil, err
		}
		obj, ok := v.(map[string]any)
		if !ok {
			return nil, errors.New("jsonl line must be object")
		}
		out = append(out, obj)
	}
	return out, nil
}

func parseLogContentsField(v any) ([]*tlspb.LogContent, error) {
	switch vv := v.(type) {
	case nil:
		return []*tlspb.LogContent{}, nil
	case map[string]any:
		out := make([]*tlspb.LogContent, 0, len(vv))
		for k, raw := range vv {
			out = append(out, &tlspb.LogContent{Key: k, Value: toString(raw)})
		}
		return out, nil
	case []any:
		out := make([]*tlspb.LogContent, 0, len(vv))
		for _, item := range vv {
			obj, ok := item.(map[string]any)
			if !ok {
				return nil, errors.New("log contents array item must be object")
			}
			out = append(out, &tlspb.LogContent{
				Key:   stringField(obj, "Key"),
				Value: stringField(obj, "Value"),
			})
		}
		return out, nil
	default:
		return nil, errors.New("unsupported log contents type")
	}
}

func parseLogTagsField(v any) []*tlspb.LogTag {
	switch vv := v.(type) {
	case nil:
		return nil
	case map[string]any:
		out := make([]*tlspb.LogTag, 0, len(vv))
		for k, raw := range vv {
			out = append(out, &tlspb.LogTag{Key: k, Value: toString(raw)})
		}
		return out
	case []any:
		out := make([]*tlspb.LogTag, 0, len(vv))
		for _, item := range vv {
			obj, ok := item.(map[string]any)
			if !ok {
				continue
			}
			out = append(out, &tlspb.LogTag{
				Key:   stringField(obj, "Key"),
				Value: stringField(obj, "Value"),
			})
		}
		return out
	default:
		return nil
	}
}

func decodeOriginalLogList(body []byte) (*tlspb.RawLogGroupListList, *tlspb.LogGroupList, error) {
	rawList := &tlspb.RawLogGroupListList{}
	if err := rawList.Unmarshal(body); err != nil {
		return nil, nil, err
	}
	out := &tlspb.LogGroupList{LogGroups: []*tlspb.LogGroup{}}
	for _, item := range rawList.RawLogGroupLists {
		logs, err := tlssdk.GetLogGroupList(normalizeCompression(item.CompressType), int64(item.OriginLen), item.Data)
		if err != nil {
			return nil, nil, err
		}
		out.LogGroups = append(out.LogGroups, logs.LogGroups...)
	}
	return rawList, out, nil
}

func flattenLogGroupList(list *tlspb.LogGroupList) []map[string]any {
	rows := make([]map[string]any, 0)
	if list == nil {
		return rows
	}
	for _, group := range list.LogGroups {
		if group == nil {
			continue
		}
		for _, logItem := range group.Logs {
			if logItem == nil {
				continue
			}
			row := map[string]any{
				"Contents": flattenContents(logItem.Contents),
			}
			if group.Source != "" {
				row["Source"] = group.Source
			}
			if group.FileName != "" {
				row["FileName"] = group.FileName
			}
			if group.ContextFlow != "" {
				row["ContextFlow"] = group.ContextFlow
			}
			if len(group.LogTags) > 0 {
				row["LogTags"] = flattenTags(group.LogTags)
			}
			if logItem.Time != 0 {
				row["Time"] = logItem.Time
			}
			if timeNs := logItem.GetTimeNs(); timeNs != 0 {
				row["TimeNs"] = timeNs
			}
			rows = append(rows, row)
		}
	}
	return rows
}

func flattenContents(items []*tlspb.LogContent) map[string]any {
	out := map[string]any{}
	for _, item := range items {
		if item == nil {
			continue
		}
		out[item.Key] = item.Value
	}
	return out
}

func flattenTags(items []*tlspb.LogTag) map[string]any {
	out := map[string]any{}
	for _, item := range items {
		if item == nil {
			continue
		}
		out[item.Key] = item.Value
	}
	return out
}

func toGenericJSON(v any) (any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return util.UnmarshalJSON(b)
}

func parseRawBodySize(raw string, compression string) (int64, error) {
	if strings.TrimSpace(raw) == "" {
		if compression == "" {
			return 0, nil
		}
		return 0, errors.New("missing x-tls-bodyrawsize")
	}
	return parseInt64String(raw)
}

func parseInt64String(s string) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(s), 10, 64)
}

func parseIntHeader(s string) int {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return v
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func applyPutLogsStatsHeaders(header map[string]string, list *tlspb.LogGroupList) {
	count, earliest, latest, ok := summarizePutLogs(list)
	header["log-count"] = strconv.Itoa(count)
	if !ok {
		delete(header, "earliest-log-time")
		delete(header, "latest-log-time")
		return
	}
	header["earliest-log-time"] = strconv.FormatInt(earliest, 10)
	header["latest-log-time"] = strconv.FormatInt(latest, 10)
}

func summarizePutLogs(list *tlspb.LogGroupList) (count int, earliest int64, latest int64, ok bool) {
	if list == nil {
		return 0, 0, 0, false
	}
	for _, group := range list.LogGroups {
		if group == nil {
			continue
		}
		for _, logItem := range group.Logs {
			if logItem == nil {
				continue
			}
			count++
			if !ok || logItem.Time < earliest {
				earliest = logItem.Time
			}
			if !ok || logItem.Time > latest {
				latest = logItem.Time
			}
			ok = true
		}
	}
	return count, earliest, latest, ok
}

func normalizeCompression(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case tlssdk.CompressLz4:
		return tlssdk.CompressLz4
	case tlssdk.CompressZlib:
		return tlssdk.CompressZlib
	default:
		return ""
	}
}

func stringifyMapField(v any) map[string]string {
	out := map[string]string{}
	obj, ok := v.(map[string]any)
	if !ok {
		return out
	}
	for k, raw := range obj {
		out[k] = toString(raw)
	}
	return out
}

func stringField(m map[string]any, key string) string {
	return toString(m[key])
}

func int64Field(m map[string]any, key string) int64 {
	switch v := m[key].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case json.Number:
		n, _ := v.Int64()
		return n
	default:
		n, _ := parseInt64String(toString(v))
		return n
	}
}

func uint32Field(m map[string]any, key string) (uint32, bool) {
	if _, ok := m[key]; !ok {
		return 0, false
	}
	v := int64Field(m, key)
	if v <= 0 {
		return 0, false
	}
	return uint32(v), true
}

func toString(v any) string {
	switch vv := v.(type) {
	case nil:
		return ""
	case string:
		return vv
	case json.Number:
		return vv.String()
	case float64:
		return strconv.FormatFloat(vv, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(vv), 'f', -1, 32)
	case int:
		return strconv.Itoa(vv)
	case int64:
		return strconv.FormatInt(vv, 10)
	case int32:
		return strconv.FormatInt(int64(vv), 10)
	case uint64:
		return strconv.FormatUint(vv, 10)
	case uint32:
		return strconv.FormatUint(uint64(vv), 10)
	case bool:
		if vv {
			return "true"
		}
		return "false"
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

func isTrueValue(s string) bool {
	return strings.EqualFold(strings.TrimSpace(s), "true")
}

func headerValue(h map[string][]string, key string) string {
	for k, vv := range h {
		if !strings.EqualFold(strings.TrimSpace(k), strings.TrimSpace(key)) {
			continue
		}
		if len(vv) == 0 {
			return ""
		}
		return vv[0]
	}
	return ""
}
