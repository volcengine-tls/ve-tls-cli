package execution

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
)

// Exercise the embedded contract through the same normalization, required-field
// validation and request construction used by tool exec. These are local request
// checks, not service-side enum, range or conditional-schema validation.
func TestAlignedContractsBuildAcceptedRequestShapes(t *testing.T) {
	tests := []struct {
		name      string
		id        contract.OperationID
		method    string
		path      string
		body      string
		query     map[string]any
		wantQuery map[string]string
	}{
		{
			name: "processor debugger uses backend enum defaults",
			id:   "processor.exec-processor", method: "POST", path: "/ExecProcessor",
			body: `{"ExecAction":"debug","DSLContent":"test","LogSample":{"message":"test"},"ProcessorType":"ingester","ProcessorDSLType":"dsl"}`,
		},
		{
			name: "processor creation allows empty description and omitted type",
			id:   "processor.create-processor", method: "POST", path: "/CreateProcessor",
			body: `{"ProjectId":"project-1","ProcessorName":"processor-1","DSLContent":"test","Description":""}`,
		},
		{
			name: "Chinese full text creation does not require a delimiter",
			id:   "index.create", method: "POST", path: "/CreateIndex",
			body: `{"TopicId":"topic-1","FullText":{"IncludeChinese":true}}`,
		},
		{
			name: "Chinese full text modification does not require a delimiter",
			id:   "index.modify", method: "PUT", path: "/ModifyIndex",
			body: `{"TopicId":"topic-1","FullText":{"IncludeChinese":true}}`,
		},
		{
			name: "full text case sensitivity can default to false",
			id:   "index.create", method: "POST", path: "/CreateIndex",
			body: `{"TopicId":"topic-1","FullText":{"Delimiter":" "}}`,
		},
		{
			name: "project tag objects preserve empty values",
			id:   "project.create", method: "POST", path: "/CreateProject",
			body: `{"ProjectName":"project-1","Region":"cn-beijing","Tags":[{"Key":"env","Value":""}]}`,
		},
		{
			name: "topic accepts encryption objects and backend shard count default",
			id:   "topic.create", method: "POST", path: "/CreateTopic",
			body: `{"ProjectId":"project-1","TopicName":"topic-1","Ttl":30,"Tags":[{"Key":"env","Value":"test"}],"EncryptConf":{"enable":true,"encrypt_type":"UserCmk","user_cmk_info":{"user_cmk_id":"key-1","trn":"trn-1","region_id":"cn-beijing","from_tls":false}}}`,
		},
		{
			name: "add tags keeps ResourcesList wire spelling",
			id:   "tag.add", method: "POST", path: "/AddTagsToResource",
			body: `{"ResourceType":"topic","ResourcesList":["topic-1"],"Tags":[{"Key":"env","Value":"test"}]}`,
		},
		{
			name: "tag resources keeps ResourcesIds wire spelling",
			id:   "tag.tag-resources", method: "POST", path: "/TagResources",
			body: `{"ResourceType":"topic","ResourcesIds":["topic-1"],"Tags":[{"Key":"env","Value":"test"}]}`,
		},
		{
			name: "tag filters preserve string value arrays",
			id:   "tag.list", method: "POST", path: "/ListTagsForResources",
			body: `{"ResourceType":"topic","MaxResults":10,"TagFilters":[{"Key":"env","Values":["test","prod"]}]}`,
		},
		{
			name: "webtracks logs remain objects rather than flattened key value fields",
			id:   "log.track", method: "POST", path: "/WebTracks",
			body:      `{"Source":"test-source","Logs":[{"message":"hello","env":"test"}]}`,
			query:     map[string]any{"ProjectId": "project-1", "TopicId": "topic-1"},
			wantQuery: map[string]string{"ProjectId": "project-1", "TopicId": "topic-1"},
		},
		{
			name: "processor function language is sent as a query parameter",
			id:   "processor.describe-processor-functions", method: "GET", path: "/DescribeProcessorFunctions",
			body:      `{}`,
			query:     map[string]any{"ProcessorType": "ingester", "ProcessorDSLType": "spl"},
			wantQuery: map[string]string{"ProcessorType": "ingester", "ProcessorDSLType": "spl"},
		},
		{
			name: "context download does not inherit search-only fields",
			id:   "log.create-download-task", method: "POST", path: "/CreateDownloadTask",
			body: `{"TopicId":"topic-1","TaskName":"download-1","TaskType":2,"StartTime":1,"EndTime":2,"Compression":"none","DataFormat":"json","LogContextInfos":{"ContextFlow":"flow-1","Source":"source-1"}}`,
		},
		{
			name: "search download preserves explicit limit and sort",
			id:   "log.create-download-task", method: "POST", path: "/CreateDownloadTask",
			body: `{"TopicId":"topic-1","TaskName":"download-1","TaskType":0,"StartTime":1,"EndTime":2,"Compression":"none","DataFormat":"json","Limit":100,"Sort":"asc"}`,
		},
	}

	catalog := alignmentOperations(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			operation, ok := catalog[tt.id]
			if !ok {
				t.Fatalf("missing operation %q", tt.id)
			}
			wantBody := alignmentJSON(t, tt.body)
			for _, sectioned := range []bool{false, true} {
				name := "flat"
				raw := alignmentJSON(t, tt.body)
				for key, value := range tt.query {
					raw[key] = value
				}
				if sectioned {
					name = "sectioned"
					raw = map[string]any{"body": alignmentJSON(t, tt.body)}
					if tt.query != nil {
						raw["query"] = tt.query
					}
				}
				t.Run(name, func(t *testing.T) {
					input, err := NormalizeInput(operation, raw)
					if err != nil {
						t.Fatalf("NormalizeInput: %v", err)
					}
					if err := ValidateInput(operation, input); err != nil {
						t.Fatalf("ValidateInput: %v", err)
					}
					request, err := BuildRequest(operation, input)
					if err != nil {
						t.Fatalf("BuildRequest: %v", err)
					}
					if request.Method != tt.method || request.Path != tt.path {
						t.Fatalf("wire = %s %s, want %s %s", request.Method, request.Path, tt.method, tt.path)
					}
					if got := alignmentJSON(t, string(request.Body)); !reflect.DeepEqual(got, wantBody) {
						t.Fatalf("body = %#v, want %#v; omitted defaults must remain backend-owned", got, wantBody)
					}
					if len(request.Query) != len(tt.wantQuery) {
						t.Fatalf("query = %#v, want %#v", request.Query, tt.wantQuery)
					}
					for key, want := range tt.wantQuery {
						if request.Query[key] != want {
							t.Fatalf("query[%q] = %q, want %q", key, request.Query[key], want)
						}
					}
				})
			}
		})
	}
}

func TestAlignedContractsStillRejectMissingRequiredFields(t *testing.T) {
	catalog := alignmentOperations(t)
	for _, tt := range []struct {
		id   contract.OperationID
		raw  string
		want string
	}{
		{"log.track", `{"ProjectId":"project-1","TopicId":"topic-1"}`, "missing required field: input.body.Logs"},
		{"tag.list", `{"ResourceType":"topic"}`, "missing required field: input.body.MaxResults"},
		{"processor.exec-processor", `{"ExecAction":"debug","LogSample":{}}`, "missing required fields: input.body.DSLContent, input.body.ProcessorDSLType, input.body.ProcessorType"},
		{"index.create", `{"FullText":{"IncludeChinese":true}}`, "missing required field: input.body.TopicId"},
		{"topic.create", `{"ProjectId":"project-1","Ttl":30}`, "missing required field: input.body.TopicName"},
	} {
		t.Run(string(tt.id), func(t *testing.T) {
			operation, ok := catalog[tt.id]
			if !ok {
				t.Fatalf("missing operation %q", tt.id)
			}
			input, err := NormalizeInput(operation, alignmentJSON(t, tt.raw))
			if err != nil {
				t.Fatalf("NormalizeInput: %v", err)
			}
			if err := ValidateInput(operation, input); err == nil || err.Error() != tt.want {
				t.Fatalf("ValidateInput error = %v, want %q", err, tt.want)
			}
		})
	}
}

func alignmentOperations(t *testing.T) map[contract.OperationID]contract.Operation {
	t.Helper()
	catalog, err := contract.LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}
	operations := make(map[contract.OperationID]contract.Operation, len(catalog.Operations))
	for _, operation := range catalog.Operations {
		operations[operation.ID] = operation
	}
	return operations
}

func alignmentJSON(t *testing.T, raw string) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	return value
}
