package runner

import (
	"sort"
	"strings"
)

func ResolveProfile(account string, region string, accountMap map[string]map[string]string, profiles []ProfileInfo) ResolveResult {
	a := strings.TrimSpace(account)
	r := strings.TrimSpace(region)
	if a == "" || r == "" {
		return ResolveResult{Error: ErrValidation}
	}
	if accountMap != nil {
		if mm, ok := accountMap[a]; ok {
			if p, ok := mm[r]; ok && strings.TrimSpace(p) != "" {
				return ResolveResult{Profile: strings.TrimSpace(p)}
			}
		}
	}

	var candidates []ProfileInfo
	for _, p := range profiles {
		if strings.TrimSpace(p.Region) != r {
			continue
		}
		if !matchAccount(a, p.Name) {
			continue
		}
		candidates = append(candidates, p)
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Name < candidates[j].Name })
	if len(candidates) == 0 {
		return ResolveResult{Error: ErrProfileNotFound}
	}
	if len(candidates) == 1 {
		return ResolveResult{Profile: candidates[0].Name}
	}
	return ResolveResult{Error: ErrProfileAmbiguous, Candidates: candidates}
}

func matchAccount(account string, profile string) bool {
	a := strings.TrimSpace(account)
	p := strings.TrimSpace(profile)
	if a == "" || p == "" {
		return false
	}
	if p == a {
		return true
	}
	if strings.HasPrefix(p, a+"-") || strings.HasPrefix(p, a+"_") {
		return true
	}
	return strings.Contains(p, a)
}
