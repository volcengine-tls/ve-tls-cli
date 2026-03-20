package cli

import "testing"

func TestParseSearchLogsArgs_SearchDefaultLimit(t *testing.T) {
	req, err := parseSearchLogsArgs([]string{
		"--topic-id", "tid",
		"--query", "*",
		"--from", "1710374400000",
		"--to", "1710378000000",
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if _, ok := req["Limit"]; !ok {
		t.Fatalf("expected Limit in search request: %v", req)
	}
}

func TestParseSearchLogsArgs_AnalysisRejectsLimitFlag(t *testing.T) {
	_, err := parseSearchLogsArgs([]string{
		"--topic-id", "tid",
		"--query", "*|select *",
		"--from", "1710374400000",
		"--to", "1710378000000",
		"--limit", "10",
	})
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestParseSearchLogsArgs_AnalysisOmitsLimit(t *testing.T) {
	req, err := parseSearchLogsArgs([]string{
		"--topic-id", "tid",
		"--query", "*|select * limit 10",
		"--from", "1710374400000",
		"--to", "1710378000000",
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if _, ok := req["Limit"]; ok {
		t.Fatalf("did not expect Limit in analysis request: %v", req)
	}
	if _, ok := req["Context"]; ok {
		t.Fatalf("did not expect Context in analysis request: %v", req)
	}
	if _, ok := req["Sort"]; ok {
		t.Fatalf("did not expect Sort in analysis request: %v", req)
	}
}

func TestParseSearchLogsArgs_PipeInsideQuotesIsNotAnalysis(t *testing.T) {
	req, err := parseSearchLogsArgs([]string{
		"--topic-id", "tid",
		"--query", `a="x|y"`,
		"--from", "1710374400000",
		"--to", "1710378000000",
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if _, ok := req["Limit"]; !ok {
		t.Fatalf("expected Limit in search request: %v", req)
	}
}

func TestParseSearchLogsArgs_PipeNonSQLIsNotAnalysis(t *testing.T) {
	req, err := parseSearchLogsArgs([]string{
		"--topic-id", "tid",
		"--query", `*|stats count()`,
		"--from", "1710374400000",
		"--to", "1710378000000",
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if _, ok := req["Limit"]; !ok {
		t.Fatalf("expected Limit in search request: %v", req)
	}
}
