package toolchain

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gonano-school/toolchain/types"
)

// archiveName returns the basename of a manifest key without its ".zim"
// extension, which is also the directory zim2md writes that archive's Markdown
// into. It returns "" for a malformed key.
func archiveName(key string) string {
	return strings.TrimSuffix(types.KeyFilename(key), ".zim")
}

// collectMarkdownFiles recursively gathers .md files below root. The result is
// sorted for determinism.
func collectMarkdownFiles(root string) ([]string, error) {
	var files []string
	var walk func(dir string) error
	walk = func(dir string) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			path := filepath.Join(dir, entry.Name())
			if entry.IsDir() {
				if err := walk(path); err != nil {
					return err
				}
				continue
			}
			if entry.Type().IsRegular() && strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
				files = append(files, path)
			}
		}
		return nil
	}
	if err := walk(root); err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}
