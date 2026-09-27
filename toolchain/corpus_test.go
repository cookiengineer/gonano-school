package toolchain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gonano-school/toolchain/types"
)

func singleManifest() types.Manifest {
	return types.Manifest{
		"datasets/phet/physics.zim": {
			Categories: []string{CategoryPhysics},
			Model:      ModelName(CategoryPhysics),
		},
	}
}

func writeMarkdown(t *testing.T, root, archive, relative string) string {
	t.Helper()
	path := filepath.Join(root, "datasets", "markdown", archive, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("# doc\n"), 0o644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}
	return path
}

func assertResolvesTo(t *testing.T, destination, source string) {
	t.Helper()
	want, err := filepath.EvalSymlinks(source)
	if err != nil {
		t.Fatalf("EvalSymlinks source: %v", err)
	}
	got, err := filepath.EvalSymlinks(destination)
	if err != nil {
		t.Fatalf("EvalSymlinks destination: %v", err)
	}
	if want != got {
		t.Fatalf("destination resolves to %q, want %q", got, want)
	}
}

func TestCorpusBuildsSelectedModel(t *testing.T) {
	root := t.TempDir()
	source := writeMarkdown(t, root, "physics", "a/one.md")
	writeMarkdown(t, root, "physics", "two.md")
	writeMarkdown(t, root, "math", "three.md")

	manifest := singleManifest()
	manifest["datasets/wikipedia/math.zim"] = types.Dataset{
		Categories: []string{CategoryMath},
		Model:      ModelName(CategoryMath),
	}
	items := Select(manifest, Filter{Models: []string{ModelName(CategoryPhysics)}})

	if err := Corpus(context.Background(), items, CorpusOptions{RootDir: root}); err != nil {
		t.Fatalf("Corpus: %v", err)
	}

	destination := filepath.Join(root, "datasets", "corpus", "gonano-physics", "physics", "a", "one.md")
	info, err := os.Lstat(destination)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected a symlink, got mode %v", info.Mode())
	}
	assertResolvesTo(t, destination, source)

	if _, err := os.Stat(filepath.Join(root, "datasets", "corpus", "gonano-math")); !os.IsNotExist(err) {
		t.Fatal("unselected model directory was created")
	}
}

func TestCorpusHardlink(t *testing.T) {
	root := t.TempDir()
	source := writeMarkdown(t, root, "physics", "one.md")
	items := Select(singleManifest(), Filter{})

	if err := Corpus(context.Background(), items, CorpusOptions{RootDir: root, Link: LinkHardlink}); err != nil {
		t.Fatalf("Corpus: %v", err)
	}

	destination := filepath.Join(root, "datasets", "corpus", "gonano-physics", "physics", "one.md")
	info, err := os.Lstat(destination)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("hardlink mode produced a symlink")
	}
	sourceInfo, err := os.Stat(source)
	if err != nil {
		t.Fatalf("stat source: %v", err)
	}
	if !os.SameFile(sourceInfo, info) {
		t.Fatal("destination is not a hardlink to the source")
	}
}

func TestCorpusMissingSourceSkips(t *testing.T) {
	root := t.TempDir()
	items := Select(singleManifest(), Filter{})

	if err := Corpus(context.Background(), items, CorpusOptions{RootDir: root}); err != nil {
		t.Fatalf("Corpus: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "datasets", "corpus", "gonano-physics")); !os.IsNotExist(err) {
		t.Fatal("model directory created without any extracted source")
	}
}

func TestCorpusForcePrunesStale(t *testing.T) {
	root := t.TempDir()
	writeMarkdown(t, root, "physics", "one.md")
	stale := filepath.Join(root, "datasets", "corpus", "gonano-physics", "stale.md")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(stale, []byte("old"), 0o644); err != nil {
		t.Fatalf("write stale: %v", err)
	}

	items := Select(singleManifest(), Filter{})
	if err := Corpus(context.Background(), items, CorpusOptions{RootDir: root, Force: true}); err != nil {
		t.Fatalf("Corpus: %v", err)
	}
	if _, err := os.Lstat(stale); !os.IsNotExist(err) {
		t.Fatal("force did not prune the stale entry")
	}
	destination := filepath.Join(root, "datasets", "corpus", "gonano-physics", "physics", "one.md")
	if _, err := os.Lstat(destination); err != nil {
		t.Fatalf("rebuilt link missing: %v", err)
	}
}

func TestCorpusDryRun(t *testing.T) {
	root := t.TempDir()
	writeMarkdown(t, root, "physics", "one.md")
	items := Select(singleManifest(), Filter{})

	if err := Corpus(context.Background(), items, CorpusOptions{RootDir: root, DryRun: true}); err != nil {
		t.Fatalf("Corpus dry-run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "datasets", "corpus")); !os.IsNotExist(err) {
		t.Fatal("dry-run created the corpus directory")
	}
}

func TestCorpusBasenameCollision(t *testing.T) {
	items := Select(types.Manifest{
		"datasets/wikipedia/foo.zim": {Categories: []string{CategoryBase}, Model: ModelName(CategoryBase)},
		"datasets/other/foo.zim":     {Categories: []string{CategoryBase}, Model: ModelName(CategoryBase)},
	}, Filter{})

	err := Corpus(context.Background(), items, CorpusOptions{})
	if err == nil || !strings.Contains(err.Error(), "share basename") {
		t.Fatalf("expected basename collision error, got %v", err)
	}
}

func TestCorpusUnknownLinkMode(t *testing.T) {
	err := Corpus(context.Background(), nil, CorpusOptions{Link: LinkMode("copy")})
	if err == nil || !strings.Contains(err.Error(), "unknown link mode") {
		t.Fatalf("expected link mode error, got %v", err)
	}
}
