// Package toolchain implements the gonano-school dataset pipeline: it scrapes
// the Kiwix catalog into a manifest, downloads the selected ZIM archives, and
// (later phases) converts them to Markdown corpora and drives the gonano
// trainer.
//
// The shared data structures live in the toolchain/types subpackage.
package toolchain

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gonano-school/toolchain/types"
)

// LoadManifest reads a manifest file. A missing file is an error; an empty file
// is treated as an empty manifest.
func LoadManifest(path string) (types.Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("manifest: read %s: %w", path, err)
	}
	var manifest types.Manifest
	if len(strings.TrimSpace(string(data))) == 0 {
		return types.Manifest{}, nil
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("manifest: parse %s: %w", path, err)
	}
	if manifest == nil {
		manifest = types.Manifest{}
	}
	return manifest, nil
}

// ManifestSuffix is the extension of the per-category manifest files
// (datasets/<category>.json).
const ManifestSuffix = ".json"

// LoadManifests reads every "<category>.json" file in dir and merges them into
// one manifest. A missing directory yields an empty manifest. Generated
// entries and static files (such as datasets/reasoning.json) are loaded the
// same way.
func LoadManifests(dir string) (types.Manifest, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return types.Manifest{}, nil
		}
		return nil, fmt.Errorf("manifest: read %s: %w", dir, err)
	}
	merged := types.Manifest{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ManifestSuffix) {
			continue
		}
		manifest, err := LoadManifest(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		merged = MergeManifests(merged, manifest)
	}
	return merged, nil
}

// MergeManifests overlays overlay onto base and returns base.
func MergeManifests(base, overlay types.Manifest) types.Manifest {
	if base == nil {
		base = types.Manifest{}
	}
	for key, dataset := range overlay {
		base[key] = dataset
	}
	return base
}

// SaveManifests writes one "<category>.json" file per category present in the
// manifest. Categories listed in managed but absent from the manifest are
// removed, so a catalog update cleans up after itself. Files for unmanaged
// categories (the static reasoning manifest) are never touched.
func SaveManifests(dir string, manifest types.Manifest, managed []string) error {
	byCategory := map[string]types.Manifest{}
	for key, dataset := range manifest {
		category := types.KeyCategory(key)
		if category == "" {
			continue
		}
		if byCategory[category] == nil {
			byCategory[category] = types.Manifest{}
		}
		byCategory[category][key] = dataset
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("manifest: mkdir %s: %w", dir, err)
	}
	for category, categoryManifest := range byCategory {
		if err := SaveManifest(filepath.Join(dir, category+ManifestSuffix), categoryManifest); err != nil {
			return err
		}
	}
	for _, category := range managed {
		if _, ok := byCategory[category]; ok {
			continue
		}
		path := filepath.Join(dir, category+ManifestSuffix)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("manifest: remove %s: %w", path, err)
		}
	}
	return nil
}

// ReasoningItems returns the manifest's reasoning datasets as items, in key
// order.
func ReasoningItems(manifest types.Manifest) []Item {
	items := make([]Item, 0)
	for _, key := range manifest.Keys() {
		dataset := manifest[key]
		if dataset.IsReasoning() {
			items = append(items, Item{Key: key, Dataset: dataset})
		}
	}
	return items
}

// OnlyZIM returns the items that are ZIM archives, dropping reasoning entries so
// the downloader and extractor never treat them as archives.
func OnlyZIM(items []Item) []Item {
	out := make([]Item, 0, len(items))
	for _, item := range items {
		if item.Dataset.IsZIM() {
			out = append(out, item)
		}
	}
	return out
}

// SlugForReasoningKey returns the corpus slug of a reasoning manifest key
// ("datasets/reasoning/<slug>").
func SlugForReasoningKey(key string) string {
	return strings.TrimSuffix(types.KeyFilename(key), ".zim")
}

// SaveManifest writes the manifest atomically (temp file + rename) with stable,
// indented JSON. encoding/json sorts map keys, so the output is deterministic.
func SaveManifest(path string, manifest types.Manifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("manifest: encode: %w", err)
	}
	data = append(data, '\n')
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("manifest: mkdir %s: %w", dir, err)
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("manifest: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("manifest: rename %s: %w", path, err)
	}
	return nil
}
