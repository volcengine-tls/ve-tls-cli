package runner

import (
	"encoding/json"
	"os"
	"strings"
)

func loadAccountMap() (map[string]map[string]string, error) {
	p := strings.TrimSpace(os.Getenv("TLSCTL_ACCOUNT_MAP"))
	if p == "" {
		return nil, nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var m map[string]map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}
