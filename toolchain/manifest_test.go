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
		"datasets/wikipedia/b.zim": {URL: "https://example/b.zim", Categories: []string{CategoryBase}, Model: "gonano-base", Size: 2},
		"datasets/wikipedia/a.zim": {URL: "https://example/a.zim", Categories: []string{CategoryMath}, Model: "gonano-math", Size: 1},
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
	if keys := loaded.Keys(); keys[0] != "datasets/wikipedia/a.zim" {
		t.Fatalf("Keys[0] = %q", keys[0])
	}
	if site := types.KeySite("datasets/wikipedia/a.zim"); site != "wikipedia" {
		t.Fatalf("KeySite = %q, want wikipedia", site)
	}
	if site := types.KeySite("not-a-key"); site != "" {
		t.Fatalf("KeySite malformed = %q, want empty", site)
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
