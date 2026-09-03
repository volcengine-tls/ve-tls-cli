package contract

import (
	"reflect"
	"strings"
	"testing"
)

func TestSupplementalTLSOperationsMatchBackendContracts(t *testing.T) {
	catalog, err := LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}
	byID := make(map[OperationID]Operation, len(catalog.Operations))
	for _, operation := range catalog.Operations {
		byID[operation.ID] = operation
	}

	wantRoutes := map[OperationID]string{
		"account.active":                 "POST /ActiveTlsAccount",
		"collector.extract":              "POST /ExtractLogSample",
		"collector.generate-begin-regex": "POST /GenerateBeginRegex",
		"collector.generate-log-regex":   "POST /GenerateLogRegex",
		"collector.parse-path":           "POST /ParsePath",
		"collector.parse-time":           "POST /ParseTime",
		"collector.split":                "POST /SplitWithQuote",
		"log.describe-latest-log":        "POST /DescribeLatestLog",
		"log.preview":                    "POST /PreviewDelimiterLog",
		"log-back-flow.create":           "POST /CreateLogBackFlowTask",
		"log-back-flow.delete":           "DELETE /DeleteLogBackFlowTask",
		"log-back-flow.describe":         "GET /DescribeLogBackFlowTasks",
		"log-back-flow.modify":           "PUT /ModifyLogBackFlowTask",
		"processor.exec-processor":       "POST /ExecProcessor",
		"shard.merge":                    "POST /ManualMergeShard",
	}
	for id, wantRoute := range wantRoutes {
		operation, ok := byID[id]
		if !ok {
			t.Fatalf("missing supplemental operation %q", id)
		}
		if got := operation.Wire.Method + " " + operation.Wire.Path; got != wantRoute {
			t.Fatalf("%s route=%q, want %q", id, got, wantRoute)
		}
		if operation.Visibility != "public" {
			t.Fatalf("%s visibility=%q, want public", id, operation.Visibility)
		}
		if operation.Wire.Codec != CodecJSON || operation.Wire.RequestFormat != "json" {
			t.Fatalf("%s wire=%+v, want JSON", id, operation.Wire)
		}
	}

	active := byID["account.active"]
	if len(active.InputSchema) != 0 {
		t.Fatalf("account.active input=%#v, want empty", active.InputSchema)
	}
	if active.Risk.Level != "high" || active.Risk.ErrorRecovery != "high-risk-retry" {
		t.Fatalf("account.active risk=%+v", active.Risk)
	}
	for _, want := range []string{"Do not retry automatically", "account.get"} {
		if !strings.Contains(active.Docs.UsageConstraints, want) {
			t.Fatalf("account.active usage constraints missing %q: %q", want, active.Docs.UsageConstraints)
		}
	}

	mergeShard := byID["shard.merge"]
	assertBodyRequired(t, mergeShard, "TopicId", "ShardId")
	if got := bodyProperty(t, mergeShard, "ShardId")["minimum"]; got != float64(0) {
		t.Fatalf("shard.merge ShardId minimum=%#v, want 0", got)
	}
	if mergeShard.Risk.Level != "high" || mergeShard.Risk.ErrorRecovery != "high-risk-retry" {
		t.Fatalf("shard.merge risk=%+v", mergeShard.Risk)
	}
	for _, want := range []string{"next contiguous readwrite shard", "Do not retry automatically", "shard.describe"} {
		if !strings.Contains(mergeShard.Docs.UsageConstraints, want) {
			t.Fatalf("shard.merge usage constraints missing %q: %q", want, mergeShard.Docs.UsageConstraints)
		}
	}

	assertBodyRequired(t, byID["collector.extract"], "BeginRegex", "LogRegex", "LogSample")
	assertBodyRequired(t, byID["collector.generate-begin-regex"], "LogSample")
	assertBodyRequired(t, byID["collector.generate-log-regex"], "End", "LogSample", "Start")
	assertBodyRequired(t, byID["collector.parse-path"], "PathSample", "Regex")
	assertBodyRequired(t, byID["collector.parse-time"], "TimeFormat", "TimeSample", "TimeZone")
	assertBodyRequired(t, byID["collector.split"], "Delimiter", "LogSample")
	assertBodyRequired(t, byID["log.describe-latest-log"], "topicId")
	assertBodyRequired(t, byID["log.preview"], "delimiter", "log", "topicId")
	assertBodyRequired(t, byID["processor.exec-processor"],
		"DSLContent", "ExecAction", "LogSample", "ProcessorDSLType", "ProcessorType")

	parseTime := byID["collector.parse-time"]
	if !strings.Contains(parseTime.Docs.UsageConstraints, "nanoseconds") ||
		!strings.Contains(parseTime.Docs.UsageConstraints, "milliseconds") {
		t.Fatalf("collector.parse-time does not document output units: %q", parseTime.Docs.UsageConstraints)
	}

	previewLog := bodyProperty(t, byID["log.preview"], "log")
	if got := previewLog["maxLength"]; got != float64(600) {
		t.Fatalf("log.preview log maxLength=%#v, want 600", got)
	}

	exec := byID["processor.exec-processor"]
	if exec.Risk.Level != "low" || exec.Risk.ErrorRecovery != "safe-retry" {
		t.Fatalf("processor.exec-processor risk=%+v", exec.Risk)
	}
	if !strings.Contains(exec.Docs.UsageConstraints, "ExecStatus=failed") {
		t.Fatalf("processor.exec-processor does not document HTTP 200 business failure: %q", exec.Docs.UsageConstraints)
	}
	if got := bodyProperty(t, exec, "ProcessorDSLType")["enum"]; !reflect.DeepEqual(got, []any{"dsl", "spl"}) {
		t.Fatalf("processor.exec-processor ProcessorDSLType enum=%#v", got)
	}

	createBackFlow := byID["log-back-flow.create"]
	assertBodyRequired(t, createBackFlow,
		"BackFlowStartTime", "ETLTaskInfo", "LogBackFlowTaskSource", "TaskName")
	for _, field := range []string{"ETLTaskInfo", "ShipperToAgentLoopInfo", "ShipperToTosInfo"} {
		bodyProperty(t, createBackFlow, field)
	}
	assertSectionOmitsProperty(t, createBackFlow, "body", "ScheduleSqlTaskInfo")

	describeBackFlow := byID["log-back-flow.describe"]
	queryProperty(t, describeBackFlow, "EtlTaskID")
	assertSectionOmitsProperty(t, describeBackFlow, "query", "ScheduleSQLTaskId")
	if got := queryProperty(t, describeBackFlow, "Status")["enum"]; !reflect.DeepEqual(got, []any{
		float64(0), float64(1), float64(2), float64(3), float64(4), float64(5),
	}) {
		t.Fatalf("log-back-flow.describe Status enum=%#v", got)
	}

	modifyBackFlow := byID["log-back-flow.modify"]
	assertBodyRequired(t, modifyBackFlow, "TaskId")
	for _, field := range []string{"ETLTaskInfo", "ShipperToAgentLoopInfo", "ShipperToTosInfo"} {
		bodyProperty(t, modifyBackFlow, field)
	}
	assertSectionOmitsProperty(t, modifyBackFlow, "body", "ScheduleSqlTaskInfo")
}

func assertBodyRequired(t *testing.T, operation Operation, want ...string) {
	t.Helper()
	body, ok := operation.InputSchema["body"].(map[string]any)
	if !ok {
		t.Fatalf("%s body schema=%#v", operation.ID, operation.InputSchema["body"])
	}
	required, ok := body["required"].([]any)
	if !ok {
		t.Fatalf("%s body required=%#v", operation.ID, body["required"])
	}
	got := make([]string, 0, len(required))
	for _, value := range required {
		got = append(got, value.(string))
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s required=%#v, want %#v", operation.ID, got, want)
	}
}

func bodyProperty(t *testing.T, operation Operation, name string) map[string]any {
	t.Helper()
	return sectionProperty(t, operation, "body", name)
}

func queryProperty(t *testing.T, operation Operation, name string) map[string]any {
	t.Helper()
	return sectionProperty(t, operation, "query", name)
}

func sectionProperty(t *testing.T, operation Operation, section, name string) map[string]any {
	t.Helper()
	schema, ok := operation.InputSchema[section].(map[string]any)
	if !ok {
		t.Fatalf("%s %s schema=%#v", operation.ID, section, operation.InputSchema[section])
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("%s %s properties=%#v", operation.ID, section, schema["properties"])
	}
	property, ok := properties[name].(map[string]any)
	if !ok {
		t.Fatalf("%s property %q=%#v", operation.ID, name, properties[name])
	}
	return property
}

func assertSectionOmitsProperty(t *testing.T, operation Operation, section, name string) {
	t.Helper()
	schema, ok := operation.InputSchema[section].(map[string]any)
	if !ok {
		t.Fatalf("%s %s schema=%#v", operation.ID, section, operation.InputSchema[section])
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("%s %s properties=%#v", operation.ID, section, schema["properties"])
	}
	if _, exists := properties[name]; exists {
		t.Fatalf("%s still exposes obsolete %s.%s", operation.ID, section, name)
	}
}
