package sheet

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// CleanPath tidies a file path typed or dragged into the Terminal: it
// removes surrounding quotes, un-escapes spaces ("My\ File" → "My File"),
// and expands a leading "~" to the home folder.
func CleanPath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.Trim(p, `"'`)
	p = strings.ReplaceAll(p, `\ `, " ")
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, p[2:])
		}
	}
	return p
}

// ReadKeyFile reads and checks a downloaded service account key file.
func ReadKeyFile(path string) (ServiceAccountKey, error) {
	data, err := os.ReadFile(CleanPath(path))
	if err != nil {
		return ServiceAccountKey{}, err
	}
	return ParseServiceAccountKey(string(data))
}

// FindKeyFiles lists service account key files in dir (e.g. Downloads),
// newest first, so setup can suggest the one just downloaded.
func FindKeyFiles(dir string) []string {
	matches, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	type file struct {
		path string
		mod  int64
	}
	var found []file
	for _, m := range matches {
		info, err := os.Stat(m)
		if err != nil || info.Size() > 20<<10 {
			continue
		}
		if _, err := ReadKeyFile(m); err == nil {
			found = append(found, file{m, info.ModTime().UnixNano()})
		}
	}
	sort.Slice(found, func(i, j int) bool { return found[i].mod > found[j].mod })
	out := make([]string, len(found))
	for i, f := range found {
		out[i] = f.path
	}
	return out
}
