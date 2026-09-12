package contract

import (
	"reflect"
	"strings"
	"testing"
)

func TestSupplementalSchemaCorrections(t *testing.T) {
	catalog, err := LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}
	operations := make(map[OperationID]Operation, len(catalog.Operations))
	for _, operation := range catalog.Operations {
		operations[operation.ID] = operation
	}

	logTrack := operations["log.track"]
	assertBodyRequired(t, logTrack, "Logs")
	logTrackBody := schemaObject(t, logTrack.InputSchema["body"])
	if _, ok := logTrackBody["properties"].(map[string]any)["Key"]; ok {
		t.Fatal("log.track still exposes explanatory body property Key")
	}
	if _, ok := logTrackBody["properties"].(map[string]any)["Value"]; ok {
		t.Fatal("log.track still exposes explanatory body property Value")
	}
	logs := schemaProperty(t, logTrackBody, "Logs")
	if logs["type"] != "array" || logs["minItems"] != float64(1) {
		t.Fatalf("log.track Logs schema=%#v, want array minItems=1", logs)
	}
	logsItems := schemaObject(t, logs["items"])
	if logsItems["type"] != "object" || !reflect.DeepEqual(logsItems["additionalProperties"], map[string]any{"type": "string"}) {
		t.Fatalf("log.track Logs.items=%#v, want string-valued object", logsItems)
	}
	if _, ok := logTrackBody["required"].([]any); !ok {
		t.Fatalf("log.track body required=%#v", logTrackBody["required"])
	}

	exec := operations["processor.exec-processor"]
	assertBodyRequired(t, exec, "DSLContent", "ExecAction", "LogSample", "ProcessorType", "ProcessorDSLType")
	assertPropertyDefault(t, bodyProperty(t, exec, "ProcessorDSLType"), "dsl")
	assertPropertyDefault(t, bodyProperty(t, exec, "ProcessorType"), "ingester")
	if strings.Contains(bodyProperty(t, exec, "ProcessorDSLType")["description"].(string), "required") ||
		strings.Contains(bodyProperty(t, exec, "ProcessorType")["description"].(string), "required") {
		t.Fatalf("processor.exec-processor still describes defaults as required: %#v", exec.InputSchema)
	}
	if !strings.Contains(exec.Docs.UsageConstraints, "defaults to dsl") ||
		!strings.Contains(exec.Docs.UsageConstraints, "defaults to ingester") {
		t.Fatalf("processor.exec-processor usage constraints omit defaults: %q", exec.Docs.UsageConstraints)
	}

	for _, id := range []OperationID{"index.create", "index.modify"} {
		fullText := schemaProperty(t, schemaObject(t, operations[id].InputSchema["body"]), "FullText")
		fullTextProperties := schemaObject(t, fullText["properties"])
		assertPropertyDefault(t, schemaObject(t, fullTextProperties["CaseSensitive"]), false)
		if _, ok := fullText["required"]; ok {
			t.Fatalf("%s FullText still has unconditional required=%#v", id, fullText["required"])
		}
		conditions, ok := fullText["allOf"].([]any)
		if !ok || len(conditions) != 1 {
			t.Fatalf("%s FullText missing conditional schema", id)
		}
		condition := schemaObject(t, conditions[0])
		if !reflect.DeepEqual(schemaObject(t, condition["if"])["required"], []any{"IncludeChinese"}) {
			t.Fatalf("%s FullText if.required=%#v, want IncludeChinese", id, condition["if"])
		}
		elseSchema := schemaObject(t, condition["else"])
		elseDelimiter := schemaProperty(t, elseSchema, "Delimiter")
		if !reflect.DeepEqual(elseSchema["required"], []any{"Delimiter"}) || elseDelimiter["minLength"] != float64(1) {
			t.Fatalf("%s FullText else=%#v, want non-empty Delimiter", id, elseSchema)
		}
		thenSchema := schemaObject(t, condition["then"])
		thenDelimiter := schemaProperty(t, thenSchema, "Delimiter")
		if thenDelimiter["maxLength"] != float64(256) {
			t.Fatalf("%s FullText then=%#v, want maxLength 256", id, thenSchema)
		}
		if strings.Contains(fullTextProperties["Delimiter"].(map[string]any)["description"].(string), "不能同时") {
			t.Fatalf("%s FullText.Delimiter retains unconditional Chinese exclusion", id)
		}
		if !strings.Contains(operations[id].Docs.UsageConstraints, "IncludeChinese") {
			t.Fatalf("%s usage constraints omit FullText conditional guidance", id)
		}
	}

	assertTagsObject(t, operations, "project.create", []string{"ProjectName", "Region"})
	assertTagsObject(t, operations, "topic.create", []string{"ProjectId", "TopicName", "Ttl"})
	assertTagsObject(t, operations, "tag.add", []string{"ResourceType", "ResourcesList"})
	assertTagsObject(t, operations, "tag.tag-resources", []string{"ResourceType", "ResourcesIds"})

	topicBody := schemaObject(t, operations["topic.create"].InputSchema["body"])
	encryptConf := schemaProperty(t, topicBody, "EncryptConf")
	if encryptConf["type"] != "object" {
		t.Fatalf("topic.create EncryptConf=%#v, want object", encryptConf)
	}
	encryptProperties := schemaObject(t, encryptConf["properties"])
	for _, field := range []string{"enable", "encrypt_type", "user_cmk_info"} {
		if _, ok := encryptProperties[field]; !ok {
			t.Fatalf("topic.create EncryptConf missing %q", field)
		}
	}
	cmkProperties := schemaObject(t, schemaObject(t, encryptProperties["user_cmk_info"])["properties"])
	for _, field := range []string{"user_cmk_id", "trn", "region_id", "from_tls"} {
		if _, ok := cmkProperties[field]; !ok {
			t.Fatalf("topic.create EncryptConf.user_cmk_info missing %q", field)
		}
	}
	assertPropertyDefault(t, schemaProperty(t, topicBody, "ShardCount"), float64(1))

	tagList := operations["tag.list"]
	assertBodyRequired(t, tagList, "ResourceType", "MaxResults")
	maxResults := bodyProperty(t, tagList, "MaxResults")
	if maxResults["minimum"] != float64(10) || maxResults["maximum"] != float64(100) {
		t.Fatalf("tag.list MaxResults=%#v, want minimum 10 maximum 100", maxResults)
	}
	if strings.Contains(maxResults["description"].(string), "默认20") {
		t.Fatalf("tag.list MaxResults still claims default 20: %q", maxResults["description"])
	}
	tagFilter := bodyProperty(t, tagList, "TagFilters")
	tagFilterProperties := schemaObject(t, schemaObject(t, tagFilter["items"])["properties"])
	if tagFilterProperties["Key"].(map[string]any)["type"] != "string" ||
		tagFilterProperties["Values"].(map[string]any)["type"] != "array" {
		t.Fatalf("tag.list TagFilters=%#v, want Key string and Values string array", tagFilter)
	}
	if tagFilterProperties["Values"].(map[string]any)["items"].(map[string]any)["type"] != "string" {
		t.Fatalf("tag.list TagFilters.Values=%#v", tagFilterProperties["Values"])
	}

	describeFunctions := operations["processor.describe-processor-functions"]
	if got := queryProperty(t, describeFunctions, "ProcessorDSLType")["enum"]; !reflect.DeepEqual(got, []any{"dsl", "spl"}) {
		t.Fatalf("processor.describe-processor-functions ProcessorDSLType=%#v", got)
	}

	createProcessor := operations["processor.create-processor"]
	assertBodyRequired(t, createProcessor, "DSLContent", "ProcessorName", "ProjectId")
	assertPropertyDefault(t, bodyProperty(t, createProcessor, "ProcessorType"), "ingester")
	if _, ok := bodyProperty(t, createProcessor, "Description")["minLength"]; ok {
		t.Fatal("processor.create-processor Description still has minLength")
	}
	assertPropertyDefault(t, bodyProperty(t, createProcessor, "MaxQps"), float64(0))
	assertPropertyDefault(t, bodyProperty(t, createProcessor, "TimeoutMs"), float64(5000))
	if !strings.Contains(bodyProperty(t, createProcessor, "MaxQps")["description"].(string), "默认 0") {
		t.Fatalf("processor.create-processor MaxQps description=%q", bodyProperty(t, createProcessor, "MaxQps")["description"])
	}

	download := operations["log.create-download-task"]
	if download.Action != "CreateDownloadTask" || download.Wire.Method != "POST" || download.Wire.Path != "/CreateDownloadTask" {
		t.Fatalf("log.create-download-task wire/action=%+v/%s", download.Wire, download.Action)
	}
	assertBodyRequired(t, download, "Compression", "DataFormat", "EndTime", "StartTime", "TaskName", "TaskType", "TopicId")
	if _, ok := bodyProperty(t, download, "Sort")["default"]; ok {
		t.Fatal("log.create-download-task Sort still has a default")
	}
	if _, ok := schemaObject(t, download.InputSchema["body"])["allOf"]; !ok {
		t.Fatal("log.create-download-task missing conditional schema")
	}
	conditions, ok := schemaObject(t, download.InputSchema["body"])["allOf"].([]any)
	if !ok || len(conditions) != 2 {
		t.Fatalf("log.create-download-task allOf=%#v", download.InputSchema["body"])
	}
	searchThen := schemaObject(t, schemaObject(t, conditions[0])["then"])
	if schemaProperty(t, searchThen, "Limit")["minimum"] != float64(1) {
		t.Fatalf("log.create-download-task Search/Analyze Limit=%#v", searchThen)
	}
	contextThen := schemaObject(t, schemaObject(t, conditions[1])["then"])
	contextInfo := schemaProperty(t, contextThen, "LogContextInfos")
	contextProperties := schemaObject(t, contextInfo["properties"])
	if contextProperties["ContextFlow"].(map[string]any)["minLength"] != float64(1) ||
		contextProperties["Source"].(map[string]any)["minLength"] != float64(1) {
		t.Fatalf("log.create-download-task LogContextInfos=%#v", contextInfo)
	}
	if !strings.Contains(download.Docs.UsageConstraints, "TaskType") ||
		!strings.Contains(download.Docs.UsageConstraints, "non-empty") ||
		!strings.Contains(download.Docs.UsageConstraints, "service") {
		t.Fatalf("log.create-download-task usage constraints=%q", download.Docs.UsageConstraints)
	}
	for _, field := range []string{"MustComplete", "AllowIncomplete"} {
		bodyProperty(t, download, field)
	}
}

func schemaObject(t *testing.T, value any) map[string]any {
	t.Helper()
	schema, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("schema=%#v, want object", value)
	}
	return schema
}

func schemaProperty(t *testing.T, schema map[string]any, name string) map[string]any {
	t.Helper()
	properties := schemaObject(t, schema["properties"])
	return schemaObject(t, properties[name])
}

func assertPropertyDefault(t *testing.T, property map[string]any, want any) {
	t.Helper()
	if got := property["default"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("property default=%#v, want %#v", got, want)
	}
}

func assertTagsObject(t *testing.T, operations map[OperationID]Operation, id OperationID, required []string) {
	t.Helper()
	operation := operations[id]
	assertBodyRequired(t, operation, required...)
	tags := bodyProperty(t, operation, "Tags")
	if tags["type"] != "array" {
		t.Fatalf("%s Tags=%#v, want array", id, tags)
	}
	items := schemaObject(t, tags["items"])
	if items["type"] != "object" {
		t.Fatalf("%s Tags.items=%#v, want object", id, items)
	}
	properties := schemaObject(t, items["properties"])
	key := properties["Key"].(map[string]any)
	if key["type"] != "string" || key["minLength"] != float64(1) || properties["Value"].(map[string]any)["type"] != "string" {
		t.Fatalf("%s Tags.items.properties=%#v, want string Key/Value", id, properties)
	}
	if !reflect.DeepEqual(items["required"], []any{"Key"}) {
		t.Fatalf("%s Tags.items.required=%#v, want Key only", id, items["required"])
	}
}
