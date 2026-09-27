// Package types holds the data structures shared across the gonano-school
// toolchain: the dataset manifest and the Kiwix catalog (Atom/OPDS) entries.
//
// It contains no I/O or HTTP logic so that the pipeline packages can depend on
// the shapes without pulling in behavior.
package types

import (
	"sort"
	"strings"
)

// Dataset describes one downloaded ZIM archive.
//
// The manifest key is "datasets/<kiwixCategory>/<file>.zim"; the archive is
// downloaded to that path relative to the repository root.
type Dataset struct {
	URL        string   `json:"url"`        // direct .zim download URL (no .meta4)
	Categories []string `json:"categories"` // exactly one of the toolchain categories today
	Model      string   `json:"model"`      // "gonano-" + Categories[0]
	Size       int64    `json:"size"`       // size in bytes
}

// Manifest maps a dataset key to its Dataset.
type Manifest map[string]Dataset

// Keys returns the manifest keys in deterministic (sorted) order.
func (manifest Manifest) Keys() []string {
	keys := make([]string, 0, len(manifest))
	for key := range manifest {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// TotalSize returns the sum of every dataset size in bytes.
func (manifest Manifest) TotalSize() int64 {
	var total int64
	for _, dataset := range manifest {
		total += dataset.Size
	}
	return total
}

// CategorySet returns the distinct categories used in the manifest.
func (manifest Manifest) CategorySet() map[string]bool {
	set := make(map[string]bool)
	for _, dataset := range manifest {
		for _, category := range dataset.Categories {
			set[category] = true
		}
	}
	return set
}

// KeySite extracts the "<kiwixCategory>" segment of a manifest key
// ("datasets/<kiwixCategory>/<file>.zim"). It returns "" for a malformed key.
func KeySite(key string) string {
	parts := strings.Split(key, "/")
	if len(parts) != 3 || parts[0] != "datasets" {
		return ""
	}
	return parts[1]
}

// KeyFilename extracts the "<file>.zim" segment of a manifest key
// ("datasets/<kiwixCategory>/<file>.zim"). It returns "" for a malformed key.
func KeyFilename(key string) string {
	parts := strings.Split(key, "/")
	if len(parts) != 3 || parts[0] != "datasets" {
		return ""
	}
	return parts[2]
}
