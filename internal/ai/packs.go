package ai

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
)

type Pack struct {
	Name   string         `json:"name"`
	Topic  TopicSpec      `json:"topic"`
	Index  map[string]any `json:"index"`
	Export ExportSpec     `json:"export"`
}

type TopicSpec struct {
	TopicName   string `json:"topic_name"`
	Ttl         int    `json:"ttl"`
	ShardCount  int    `json:"shard_count"`
	AutoSplit   bool   `json:"auto_split"`
	Description string `json:"description"`
}

type ExportSpec struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
	Sort  string `json:"sort"`
}

var builtinPacks = map[string]string{
	"llm-trace-v1": `{
  "name": "llm-trace-v1",
  "topic": {
    "topic_name": "ai-llm-trace",
    "ttl": 30,
    "shard_count": 2,
    "auto_split": true,
    "description": "AI trace/tool-call/prompt logs"
  },
  "index": {
    "FullText": {
      "Delimiter": ",-;| \n\t",
      "CaseSensitive": false,
      "IncludeChinese": true
    },
    "EnableAutoIndex": true
  },
  "export": {
    "query": "*",
    "limit": 100,
    "sort": "desc"
  }
}`,
}

func List() []string {
	names := make([]string, 0, len(builtinPacks))
	for k := range builtinPacks {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

func Load(name string) (Pack, error) {
	n := strings.TrimSpace(name)
	if n == "" {
		return Pack{}, errors.New("empty pack name")
	}
	raw, ok := builtinPacks[n]
	if !ok {
		return Pack{}, errors.New("unknown pack: " + n)
	}
	var p Pack
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return Pack{}, err
	}
	if p.Name == "" {
		p.Name = n
	}
	if p.Export.Limit <= 0 {
		p.Export.Limit = 100
	}
	return p, nil
}
