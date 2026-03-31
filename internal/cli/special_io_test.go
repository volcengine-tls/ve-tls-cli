package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	tlssdk "github.com/volcengine/volc-sdk-golang/service/tls"
	tlspb "github.com/volcengine/volc-sdk-golang/service/tls/pb"

	"volclog/internal/output"
	"volclog/internal/tlsapi"
)

func TestPrepareSpecialIORequest_PutLogsJSONEncodesProtobuf(t *testing.T) {
	meta := apiIOMeta{
		Group:         "log",
		Action:        "PutLogs",
		Method:        "POST",
		Path:          "/PutLogs",
		RequestFormat: requestFormatJSON,
	}
	header := map[string]string{
		"x-tls-compresstype": tlssdk.CompressLz4,
	}
	body := []byte(`{
  "LogGroups": [
    {
      "Source": "host-1",
      "Logs": [
        {
          "Time": 1710000000000,
          "Contents": [
            {"Key": "level", "Value": "info"},
            {"Key": "msg", "Value": "hello"}
          ]
        }
      ]
    }
  ]
}`)

	gotHeader, gotBody, _, handled, err := prepareSpecialIORequest(meta, header, body)
	if err != nil {
		t.Fatalf("prepareSpecialIORequest error: %v", err)
	}
	if !handled {
		t.Fatalf("expected request to be handled")
	}
	if gotHeader["Content-Type"] != "application/x-protobuf" {
		t.Fatalf("unexpected content-type: %q", gotHeader["Content-Type"])
	}
	if gotHeader["x-tls-bodyrawsize"] == "" {
		t.Fatalf("missing x-tls-bodyrawsize")
	}

	rawSize := mustParseInt64(t, gotHeader["x-tls-bodyrawsize"])
	logs, err := tlssdk.GetLogGroupList(gotHeader["x-tls-compresstype"], rawSize, gotBody)
	if err != nil {
		t.Fatalf("decode prepared putlogs body error: %v", err)
	}
	if len(logs.LogGroups) != 1 || len(logs.LogGroups[0].Logs) != 1 {
		t.Fatalf("unexpected logs: %+v", logs)
	}
}

func TestPrepareSpecialIORequest_WebTracksJSONLEncodesJSONArray(t *testing.T) {
	meta := apiIOMeta{
		Group:         "log",
		Action:        "WebTracks",
		Method:        "POST",
		Path:          "/WebTracks",
		RequestFormat: requestFormatJSONL,
	}
	body := []byte(`{"Source":"browser-a","Contents":{"event":"click","page":"home"}}` + "\n" +
		`{"Source":"browser-a","Contents":{"event":"view","page":"detail"}}` + "\n")

	gotHeader, gotBody, _, handled, err := prepareSpecialIORequest(meta, nil, body)
	if err != nil {
		t.Fatalf("prepareSpecialIORequest error: %v", err)
	}
	if !handled {
		t.Fatalf("expected request to be handled")
	}
	if gotHeader["Content-Type"] != "application/json" {
		t.Fatalf("unexpected content-type: %q", gotHeader["Content-Type"])
	}
	var payload map[string]any
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if payload["Source"] != "browser-a" {
		t.Fatalf("unexpected source: %#v", payload["Source"])
	}
	logs, ok := payload["Logs"].([]any)
	if !ok || len(logs) != 2 {
		t.Fatalf("unexpected logs payload: %#v", payload["Logs"])
	}
}

func TestDecodeSpecialIOResponse_ConsumeLogsJSONLProducesRecords(t *testing.T) {
	meta := apiIOMeta{
		Group:        "log",
		Action:       "ConsumeLogs",
		Method:       "POST",
		Path:         "/ConsumeLogs",
		OutputFormat: output.FormatJSONL,
		OutputMode:   "stdout",
	}
	list := &tlspb.LogGroupList{
		LogGroups: []*tlspb.LogGroup{
			{
				Source:   "host-1",
				FileName: "app.log",
				LogTags:  []*tlspb.LogTag{{Key: "env", Value: "test"}},
				Logs: []*tlspb.Log{
					{
						Time: 1710000000000,
						Contents: []*tlspb.LogContent{
							{Key: "level", Value: "info"},
							{Key: "msg", Value: "hello"},
						},
					},
				},
			},
		},
	}
	body, rawSize, err := tlssdk.GetPutLogsBody(tlssdk.CompressLz4, list)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	resp := tlsapi.Response{
		StatusCode: 200,
		Header: map[string][]string{
			"Content-Type":      {"application/x-protobuf"},
			"x-tls-count":       {"1"},
			"x-tls-cursor":      {"cursor-1"},
			"x-tls-bodyrawsize": {int64ToString(rawSize)},
		},
		Body: body,
	}
	state := &specialIOState{Compression: tlssdk.CompressLz4}

	out, handled, err := decodeSpecialIOResponse(meta, state, resp)
	if err != nil {
		t.Fatalf("decodeSpecialIOResponse error: %v", err)
	}
	if !handled {
		t.Fatalf("expected response to be handled")
	}
	rows, ok := out.([]map[string]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("unexpected rows: %#v", out)
	}
	if rows[0]["Source"] != "host-1" {
		t.Fatalf("unexpected row source: %#v", rows[0])
	}
}

func TestDecodeSpecialIOResponse_ConsumeOriginalLogsJSONPreservesRawPackages(t *testing.T) {
	meta := apiIOMeta{
		Group:        "log",
		Action:       "ConsumeOriginalLogs",
		Method:       "POST",
		Path:         "/ConsumeOriginalLogs",
		OutputFormat: output.FormatJSON,
		OutputMode:   "stdout",
	}
	list := &tlspb.LogGroupList{
		LogGroups: []*tlspb.LogGroup{
			{
				Source: "host-1",
				Logs: []*tlspb.Log{
					{
						Time: 1710000000000,
						Contents: []*tlspb.LogContent{
							{Key: "msg", Value: "hello"},
						},
					},
				},
			},
		},
	}
	rawData, rawLen, err := tlssdk.GetPutLogsBody(tlssdk.CompressLz4, list)
	if err != nil {
		t.Fatalf("marshal raw body: %v", err)
	}
	rawList := &tlspb.RawLogGroupListList{
		RawLogGroupLists: []*tlspb.RawLogGroupList{
			{
				OriginLen:    int32(rawLen),
				CompressType: tlssdk.CompressLz4,
				Data:         rawData,
			},
		},
	}
	body, err := rawList.Marshal()
	if err != nil {
		t.Fatalf("marshal raw list: %v", err)
	}
	resp := tlsapi.Response{
		StatusCode: 200,
		Header: map[string][]string{
			"Content-Type":   {"application/x-protobuf"},
			"x-tls-count":    {"1"},
			"x-tls-cursor":   {"cursor-raw"},
			"x-tls-original": {"true"},
		},
		Body: body,
	}

	out, handled, err := decodeSpecialIOResponse(meta, &specialIOState{}, resp)
	if err != nil {
		t.Fatalf("decodeSpecialIOResponse error: %v", err)
	}
	if !handled {
		t.Fatalf("expected response to be handled")
	}
	got, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("unexpected output: %#v", out)
	}
	rawObj, ok := got["RawLogGroupListList"].(map[string]any)
	if !ok {
		t.Fatalf("missing raw list: %#v", got)
	}
	items, ok := rawObj["RawLogGroupLists"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("unexpected raw items: %#v", rawObj["RawLogGroupLists"])
	}
}

func TestDecodeSpecialIOResponse_ConsumeKafkaBinaryRequiresFileOutput(t *testing.T) {
	meta := apiIOMeta{
		Group:        "log",
		Action:       "ConsumeKafkaLogs",
		Method:       "POST",
		Path:         "/ConsumeKafkaLogs",
		OutputFormat: output.FormatJSON,
		OutputMode:   "stdout",
	}
	resp := tlsapi.Response{
		StatusCode: 200,
		Header: map[string][]string{
			"Content-Type":             {"application/x-protobuf"},
			"x-tls-count":              {"1"},
			"x-tls-is-kafka-records":   {"true"},
			"x-tls-kafka-records-num":  {"1"},
			"x-tls-kafka-start-offset": {"1"},
		},
		Body: []byte{0x01, 0x02, 0x03},
	}

	_, handled, err := decodeSpecialIOResponse(meta, &specialIOState{}, resp)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !handled {
		t.Fatalf("expected response to be handled")
	}
}

func mustParseInt64(t *testing.T, s string) int64 {
	t.Helper()
	v, err := parseInt64String(s)
	if err != nil {
		t.Fatalf("parse int64 %q: %v", s, err)
	}
	return v
}

func int64ToString(v int) string {
	return mustJSONStringNumber(v)
}

func mustJSONStringNumber(v int) string {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		panic(err)
	}
	return string(bytes.TrimSpace(buf.Bytes()))
}
