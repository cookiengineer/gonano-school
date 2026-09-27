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
