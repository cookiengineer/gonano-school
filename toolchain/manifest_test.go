package toolchain

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gonano-school/toolchain/types"
)

func TestManifestRoundTripAndDeterministicOrder(t *testing.T) {
	manifest := types.Manifest{
		"datasets/base/b.zim": {URL: "https://example/b.zim", Categories: []string{CategoryBase}, Model: "gonano-base", Size: 2},
		"datasets/base/a.zim": {URL: "https://example/a.zim", Categories: []string{CategoryMath}, Model: "gonano-math", Size: 1},
	}
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := SaveManifest(path, manifest); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	loaded, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if !reflect.DeepEqual(manifest, loaded) {
		t.Fatalf("round trip mismatch:\n got %#v\nwant %#v", loaded, manifest)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	indexA := strings.Index(string(data), "a.zim")
	indexB := strings.Index(string(data), "b.zim")
	if indexA < 0 || indexB < 0 || indexA > indexB {
		t.Fatalf("keys not sorted in output:\n%s", data)
	}

	if loaded.TotalSize() != 3 {
		t.Fatalf("TotalSize = %d, want 3", loaded.TotalSize())
	}
	if keys := loaded.Keys(); keys[0] != "datasets/base/a.zim" {
		t.Fatalf("Keys[0] = %q", keys[0])
	}
	if category := types.KeyCategory("datasets/base/a.zim"); category != "base" {
		t.Fatalf("KeyCategory = %q, want base", category)
	}
	if category := types.KeyCategory("not-a-key"); category != "" {
		t.Fatalf("KeyCategory malformed = %q, want empty", category)
	}
}

// TestSaveLoadManifests checks the per-category split: one file per category,
// generated categories refreshed, and unmanaged static files left alone.
func TestSaveLoadManifests(t *testing.T) {
	dir := t.TempDir()
	// A static file the updater must never touch.
	static := types.Manifest{
		"datasets/reasoning/magpie": {Kind: types.KindReasoning, Model: "gonano-base"},
	}
	if err := SaveManifest(filepath.Join(dir, "reasoning.json"), static); err != nil {
		t.Fatal(err)
	}

	manifest := types.Manifest{
		"datasets/base/a.zim":    {Model: "gonano-base"},
		"datasets/physics/b.zim": {Model: "gonano-physics"},
	}
	if err := SaveManifests(dir, manifest, []string{CategoryBase, CategoryPhysics, CategoryChemistry}); err != nil {
		t.Fatalf("SaveManifests: %v", err)
	}
	// An empty managed category that has no file is a no-op; create then remove one.
	if err := SaveManifest(filepath.Join(dir, "chemistry.json"), types.Manifest{"datasets/chemistry/x.zim": {Model: "gonano-chemistry"}}); err != nil {
		t.Fatal(err)
	}
	if err := SaveManifests(dir, manifest, []string{CategoryBase, CategoryPhysics, CategoryChemistry}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "chemistry.json")); !os.IsNotExist(err) {
		t.Fatalf("stale chemistry.json not removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "reasoning.json")); err != nil {
		t.Fatalf("static reasoning.json was touched: %v", err)
	}

	loaded, err := LoadManifests(dir)
	if err != nil {
		t.Fatalf("LoadManifests: %v", err)
	}
	if len(loaded) != 3 {
		t.Fatalf("merged manifest = %d entries, want 3 (%#v)", len(loaded), loaded)
	}
	if !loaded["datasets/reasoning/magpie"].IsReasoning() {
		t.Fatal("static reasoning entry missing after load")
	}
}

func TestLoadManifestsMissingDirIsEmpty(t *testing.T) {
	manifest, err := LoadManifests(filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatalf("LoadManifests: %v", err)
	}
	if len(manifest) != 0 {
		t.Fatalf("expected empty manifest, got %d", len(manifest))
	}
}

func TestLoadManifestMissing(t *testing.T) {
	if _, err := LoadManifest(filepath.Join(t.TempDir(), "absent.json")); err == nil {
		t.Fatal("expected error for missing manifest")
	}
}

func TestLoadManifestEmptyIsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, []byte("  \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if len(manifest) != 0 {
		t.Fatalf("expected empty manifest, got %d", len(manifest))
	}
}

func TestLoadManifestInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifest(path); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestCategorySet(t *testing.T) {
	manifest := types.Manifest{
		"datasets/a/x.zim": {Categories: []string{CategoryBase}},
		"datasets/b/y.zim": {Categories: []string{CategoryBase}},
		"datasets/c/z.zim": {Categories: []string{CategoryPhysics}},
	}
	set := manifest.CategorySet()
	if len(set) != 2 || !set[CategoryBase] || !set[CategoryPhysics] {
		t.Fatalf("CategorySet = %#v", set)
	}
}
