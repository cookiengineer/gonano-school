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

// Kind discriminates the manifest entry types.
const (
	// KindZIM is a Kiwix ZIM archive fetched by the downloader and converted by
	// zim2md. It is the default when Kind is empty.
	KindZIM = "zim"
	// KindReasoning is a HuggingFace reasoning dataset fetched and flattened by
	// cmd/reasoning, not by the ZIM downloader or extractor.
	KindReasoning = "reasoning"
)

// Dataset describes one manifest entry: a downloaded ZIM archive, or a
// static reasoning dataset.
//
// The manifest key is always "datasets/<category>/<name>", so a dataset's key
// and its on-disk location share the same relative path, and one manifest file
// datasets/<category>.json describes everything under datasets/<category>/.
// Reasoning entries use "datasets/reasoning/<slug>" and carry the Hub repo
// coordinates instead of a .zim URL.
type Dataset struct {
	URL        string   `json:"url,omitempty"`        // direct .zim download URL (no .meta4)
	Categories []string `json:"categories,omitempty"` // one or more toolchain categories
	Model      string   `json:"model,omitempty"`      // "gonano-" + category
	Size       int64    `json:"size,omitempty"`       // size in bytes (0 for reasoning)
	Site       string   `json:"site,omitempty"`       // Kiwix source category, e.g. wikipedia
	Kind       string   `json:"kind,omitempty"`       // "", "zim", or "reasoning"
	Repo       string   `json:"repo,omitempty"`       // HuggingFace dataset repo (reasoning)
	Config     string   `json:"config,omitempty"`     // HuggingFace config (reasoning)
	Split      string   `json:"split,omitempty"`      // HuggingFace split (reasoning)
}

// IsReasoning reports whether the entry is a HuggingFace reasoning dataset.
func (dataset Dataset) IsReasoning() bool { return dataset.Kind == KindReasoning }

// IsZIM reports whether the entry is a Kiwix ZIM archive (the default kind).
func (dataset Dataset) IsZIM() bool { return dataset.Kind == "" || dataset.Kind == KindZIM }

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

// KeyCategory extracts the "<category>" segment of a manifest key
// ("datasets/<category>/<name>"). It returns "" for a malformed key.
func KeyCategory(key string) string {
	parts := strings.Split(key, "/")
	if len(parts) != 3 || parts[0] != "datasets" {
		return ""
	}
	return parts[1]
}

// KeyFilename extracts the "<name>" segment of a manifest key
// ("datasets/<category>/<name>"). It returns "" for a malformed key.
func KeyFilename(key string) string {
	parts := strings.Split(key, "/")
	if len(parts) != 3 || parts[0] != "datasets" {
		return ""
	}
	return parts[2]
}
