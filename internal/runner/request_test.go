package runner

import "testing"

func TestRequestFromText_MergesArgs(t *testing.T) {
	req, err := RequestFromText(`account=acctA region=cn-beijing action=log.search topic_id=tid query="error" from_ms=1 to_ms=2 dry_run=true output=json`)
	if err != nil {
		t.Fatal(err)
	}
	if req.Account != "acctA" || req.Region != "cn-beijing" || req.Action != "log.search" {
		t.Fatalf("req=%+v", req)
	}
	if req.DryRun != true || req.Output != "json" {
		t.Fatalf("dry/output: %+v", req)
	}
	if req.Args["topic_id"] != "tid" {
		t.Fatalf("args=%v", req.Args)
	}
}

func TestRequestFromText_DefaultDryRunForDanger(t *testing.T) {
	req, err := RequestFromText(`account=acctA region=cn-beijing action=log.export topic_id=tid query="*" from_ms=1 to_ms=2`)
	if err != nil {
		t.Fatal(err)
	}
	if req.DryRun != true {
		t.Fatalf("dry_run=%v", req.DryRun)
	}
}
