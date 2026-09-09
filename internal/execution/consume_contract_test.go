package execution

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
)

func TestConsumeEmbeddedContractAcceptsCursorInput(t *testing.T) {
	operation := embeddedConsumeOperation(t)
	if operation.Wire.Method != "POST" || operation.Wire.Path != "/ConsumeLogs" {
		t.Fatalf("wire = %s %s, want POST /ConsumeLogs", operation.Wire.Method, operation.Wire.Path)
	}
	if operation.Wire.Codec != contract.CodecConsumeLogs {
		t.Fatalf("codec = %q, want %q", operation.Wire.Codec, contract.CodecConsumeLogs)
	}

	tests := []struct {
		name string
		raw  map[string]any
	}{
		{
			name: "flat",
			raw: map[string]any{
				"TopicId": "topic-1",
				"ShardId": float64(3),
				"Cursor":  "cursor-1",
			},
		},
		{
			name: "sectioned",
			raw: map[string]any{
				"query": map[string]any{
					"TopicId": "topic-1",
					"ShardId": float64(3),
				},
				"body": map[string]any{
					"Cursor": "cursor-1",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input, err := NormalizeInput(operation, tt.raw)
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
			if request.Method != "POST" || request.Path != "/ConsumeLogs" {
				t.Fatalf("request = %s %s, want POST /ConsumeLogs", request.Method, request.Path)
			}
			var body map[string]any
			if err := json.Unmarshal(request.Body, &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			wantBody := map[string]any{"Cursor": "cursor-1"}
			if !reflect.DeepEqual(body, wantBody) {
				t.Fatalf("body = %#v, want %#v", body, wantBody)
			}
			if _, ok := body["Offset"]; ok {
				t.Fatalf("body unexpectedly contains Offset: %#v", body)
			}
		})
	}
}

func TestConsumeEmbeddedContractReportsRequiredFieldPaths(t *testing.T) {
	operation := embeddedConsumeOperation(t)
	tests := []struct {
		name string
		raw  map[string]any
		want string
	}{
		{
			name: "cursor",
			raw: map[string]any{
				"TopicId": "topic-1",
				"ShardId": float64(3),
			},
			want: "missing required field: input.body.Cursor",
		},
		{
			name: "topic id",
			raw: map[string]any{
				"ShardId": float64(3),
				"Cursor":  "cursor-1",
			},
			want: "missing required field: input.query.TopicId",
		},
		{
			name: "shard id",
			raw: map[string]any{
				"TopicId": "topic-1",
				"Cursor":  "cursor-1",
			},
			want: "missing required field: input.query.ShardId",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input, err := NormalizeInput(operation, tt.raw)
			if err != nil {
				t.Fatalf("NormalizeInput: %v", err)
			}
			if err := ValidateInput(operation, input); err == nil || err.Error() != tt.want {
				t.Fatalf("ValidateInput error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestConsumeEmbeddedContractRejectsFlatOffset(t *testing.T) {
	operation := embeddedConsumeOperation(t)
	_, err := NormalizeInput(operation, map[string]any{
		"TopicId": "topic-1",
		"ShardId": float64(3),
		"Cursor":  "cursor-1",
		"Offset":  float64(0),
	})
	if err == nil || err.Error() != "flat input contains unknown fields: Offset" {
		t.Fatalf("NormalizeInput error = %v, want flat unknown Offset error", err)
	}
}

func embeddedConsumeOperation(t *testing.T) contract.Operation {
	t.Helper()
	catalog, err := contract.LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}
	for _, operation := range catalog.Operations {
		if operation.ID == "log.consume" {
			return operation
		}
	}
	t.Fatal("embedded catalog is missing log.consume")
	return contract.Operation{}
}
