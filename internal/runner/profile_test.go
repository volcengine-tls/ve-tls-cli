package runner

import "testing"

func TestResolveProfile_FromMap(t *testing.T) {
	profiles := []ProfileInfo{
		{Name: "acctA-cn", Region: "cn-beijing"},
		{Name: "acctA-sg", Region: "ap-singapore-1"},
	}
	m := map[string]map[string]string{
		"acctA": {"cn-beijing": "acctA-cn"},
	}
	res := ResolveProfile("acctA", "cn-beijing", m, profiles)
	if res.Error != "" {
		t.Fatalf("unexpected error: %s", res.Error)
	}
	if res.Profile != "acctA-cn" {
		t.Fatalf("profile=%s", res.Profile)
	}
}

func TestResolveProfile_Ambiguous(t *testing.T) {
	profiles := []ProfileInfo{
		{Name: "acctA-cn", Region: "cn-beijing"},
		{Name: "acctA-cn2", Region: "cn-beijing"},
	}
	res := ResolveProfile("acctA", "cn-beijing", nil, profiles)
	if res.Error != ErrProfileAmbiguous {
		t.Fatalf("err=%s", res.Error)
	}
	if len(res.Candidates) != 2 {
		t.Fatalf("candidates=%v", res.Candidates)
	}
}

func TestResolveProfile_NotFound(t *testing.T) {
	profiles := []ProfileInfo{
		{Name: "acctB-cn", Region: "cn-beijing"},
	}
	res := ResolveProfile("acctA", "cn-beijing", nil, profiles)
	if res.Error != ErrProfileNotFound {
		t.Fatalf("err=%s", res.Error)
	}
}
