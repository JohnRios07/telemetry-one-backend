package migrations

import (
	"embed"
	"io/fs"
	"sort"
	"strings"
)

// FS embeds the SQL migrations so the production container can run without
// copying migration files next to the binary.
//
//go:embed *.sql
var FS embed.FS

func UpFiles() ([]string, error) {
	entries, err := fs.ReadDir(FS, ".")
	if err != nil {
		return nil, err
	}

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".up.sql") {
			continue
		}
		files = append(files, entry.Name())
	}
	sort.Strings(files)

	return files, nil
}
