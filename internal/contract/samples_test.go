package contract

import (
	"reflect"
	"strings"
	"testing"
)

func TestRequestSamplesMatchEmbeddedOperations(t *testing.T) {
	catalog, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	operations := make(map[OperationID]Operation, len(catalog.Operations))
	for _, operation := range catalog.Operations {
		operations[operation.ID] = operation
	}

	for operationID := range requestSamples {
		operation, ok := operations[operationID]
		if !ok {
			t.Fatalf("sample operation %q is absent from catalog", operationID)
		}
		for _, mode := range []TemplateMode{TemplateRequired, TemplateFull} {
			sample, ok, err := RequestSample(operation, mode)
			if err != nil {
				t.Fatalf("RequestSample(%s, %s): %v", operationID, mode, err)
			}
			if !ok || len(sample) == 0 {
				t.Fatalf("RequestSample(%s, %s) = %#v, %v", operationID, mode, sample, ok)
			}
		}
	}
}

func TestRequestSampleReturnsIndependentCopy(t *testing.T) {
	catalog, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	var operation Operation
	for _, candidate := range catalog.Operations {
		if candidate.ID == "index.create" {
			operation = candidate
			break
		}
	}
	first, ok, err := RequestSample(operation, TemplateRequired)
	if err != nil || !ok {
		t.Fatalf("first sample: ok=%v err=%v", ok, err)
	}
	delete(first, "FullText")
	second, ok, err := RequestSample(operation, TemplateRequired)
	if err != nil || !ok {
		t.Fatalf("second sample: ok=%v err=%v", ok, err)
	}
	if _, ok := second["FullText"]; !ok {
		t.Fatal("mutating returned sample changed the registry")
	}
}

func TestRequestSampleRejectsSchemaDrift(t *testing.T) {
	operation := Operation{
		ID: "index.create",
		InputSchema: JSONSchema{
			"body": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
	}
	_, _, err := RequestSample(operation, TemplateRequired)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("err = %v, want unknown sample field", err)
	}
}

func TestIndexRequestSamplesExcludeLegacyUnsupportedFields(t *testing.T) {
	for _, mode := range []TemplateMode{TemplateRequired, TemplateFull} {
		sample := requestSamples["index.create"][mode]
		for _, field := range []string{
			"EnablePhraseIndex",
			"LogReduce",
			"LogReduceBlackList",
			"LogReduceWhiteList",
		} {
			if _, ok := sample[field]; ok {
				t.Errorf("%s sample contains unsupported field %q", mode, field)
			}
		}
	}
	if reflect.DeepEqual(
		requestSamples["index.create"][TemplateRequired],
		requestSamples["index.create"][TemplateFull],
	) {
		t.Fatal("required and full index samples should differ")
	}
}
