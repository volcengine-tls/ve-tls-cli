package bundledskills

import (
	"embed"
	"io/fs"
	"sort"
)

//go:embed skills
var embedded embed.FS

func Root() (fs.FS, error) {
	return fs.Sub(embedded, "skills")
}

func List() ([]string, error) {
	root, err := Root()
	if err != nil {
		return nil, err
	}
	entries, err := fs.ReadDir(root, ".")
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		out = append(out, entry.Name())
	}
	sort.Strings(out)
	return out, nil
}
