package toolchain

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gonano-school/toolchain/types"
)

// fakeZim2md installs a shell script named zim2md that records its argv to
// logPath. It returns the directory to prepend to PATH.
func fakeZim2md(t *testing.T) (dir, logPath string) {
	t.Helper()
	dir = t.TempDir()
	logPath = filepath.Join(dir, "argv.log")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"" + logPath + "\"\n"
	if err := os.WriteFile(filepath.Join(dir, "zim2md"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake zim2md: %v", err)
	}
	return dir, logPath
}

func extractManifest() types.Manifest {
	return types.Manifest{
		"datasets/physics/physics.zim": {
			Categories: []string{CategoryPhysics},
			Model:      ModelName(CategoryPhysics),
		},
	}
}

func writeZim(t *testing.T, root, key string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("zim bytes"), 0o644); err != nil {
		t.Fatalf("write zim: %v", err)
	}
}

func readArgv(t *testing.T, logPath string) []string {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read argv log: %v", err)
	}
	return strings.Split(strings.TrimRight(string(data), "\n"), "\n")
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func TestExtractInvokesZim2md(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake zim2md is a shell script")
	}
	fakeDir, logPath := fakeZim2md(t)
	t.Setenv("PATH", fakeDir)

	root := t.TempDir()
	writeZim(t, root, "datasets/physics/physics.zim")
	items := Select(extractManifest(), Filter{})

	if err := Extract(context.Background(), items, ExtractOptions{RootDir: root, Jobs: 1, Workers: 3}); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	args := readArgv(t, logPath)
	markdownDir := filepath.Join(root, "datasets", "physics", "markdown")
	zimPath := filepath.Join(root, "datasets", "physics", "physics.zim")
	for _, want := range []string{"--output", markdownDir, "--workers", "3", "--quiet", "--no-clobber", zimPath} {
		if !containsArg(args, want) {
			t.Errorf("argv missing %q: %#v", want, args)
		}
	}
	if containsArg(args, "--assets") {
		t.Errorf("argv must never contain --assets: %#v", args)
	}
}

func TestExtractSkipsPresent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake zim2md is a shell script")
	}
	fakeDir, logPath := fakeZim2md(t)
	t.Setenv("PATH", fakeDir)

	root := t.TempDir()
	writeZim(t, root, "datasets/physics/physics.zim")
	outDir := filepath.Join(root, "datasets", "physics", "markdown", "physics")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "page.md"), []byte("done"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	items := Select(extractManifest(), Filter{})
	if err := Extract(context.Background(), items, ExtractOptions{RootDir: root, Jobs: 1}); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("zim2md invoked for an already extracted archive")
	}
}

func TestExtractForceReExtracts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake zim2md is a shell script")
	}
	fakeDir, logPath := fakeZim2md(t)
	t.Setenv("PATH", fakeDir)

	root := t.TempDir()
	writeZim(t, root, "datasets/physics/physics.zim")
	outDir := filepath.Join(root, "datasets", "physics", "markdown", "physics")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "page.md"), []byte("stale"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	items := Select(extractManifest(), Filter{})
	if err := Extract(context.Background(), items, ExtractOptions{RootDir: root, Jobs: 1, Force: true}); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("zim2md not invoked with --force: %v", err)
	}
	if _, err := os.Stat(outDir); !os.IsNotExist(err) {
		t.Fatalf("force should have removed the stale output directory")
	}
	args := readArgv(t, logPath)
	if containsArg(args, "--no-clobber") {
		t.Errorf("--force must not pass --no-clobber: %#v", args)
	}
}

func TestExtractMissingZim(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake zim2md is a shell script")
	}
	fakeDir, _ := fakeZim2md(t)
	t.Setenv("PATH", fakeDir)

	root := t.TempDir()
	items := Select(extractManifest(), Filter{})
	err := Extract(context.Background(), items, ExtractOptions{RootDir: root, Jobs: 1})
	if err == nil || !strings.Contains(err.Error(), "zim archive not found") {
		t.Fatalf("expected missing zim error, got %v", err)
	}
}

func TestExtractMissingBinary(t *testing.T) {
	root := t.TempDir()
	items := Select(extractManifest(), Filter{})
	err := Extract(context.Background(), items, ExtractOptions{
		RootDir: root,
		Zim2md:  "definitely-not-a-real-zim2md-binary",
	})
	if err == nil {
		t.Fatal("expected LookPath error")
	}
	if !strings.Contains(err.Error(), "go install github.com/cookiengineer/zim2md@latest") {
		t.Fatalf("error missing install hint: %v", err)
	}
}

func TestExtractDryRun(t *testing.T) {
	root := t.TempDir()
	writeZim(t, root, "datasets/physics/physics.zim")
	items := Select(extractManifest(), Filter{})
	if err := Extract(context.Background(), items, ExtractOptions{RootDir: root, DryRun: true}); err != nil {
		t.Fatalf("Extract dry-run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "datasets", "physics", "markdown")); !os.IsNotExist(err) {
		t.Fatal("dry-run created the markdown directory")
	}
}

func TestExtractKeepGoing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake zim2md is a shell script")
	}
	fakeDir, _ := fakeZim2md(t)
	t.Setenv("PATH", fakeDir)

	root := t.TempDir()
	writeZim(t, root, "datasets/physics/physics.zim")
	manifest := extractManifest()
	manifest["datasets/math/math.zim"] = types.Dataset{
		Categories: []string{CategoryMath},
		Model:      ModelName(CategoryMath),
	}

	items := Select(manifest, Filter{})
	err := Extract(context.Background(), items, ExtractOptions{RootDir: root, Jobs: 1, KeepGoing: true})
	if err == nil || !strings.Contains(err.Error(), "zim archive not found") {
		t.Fatalf("expected aggregated error, got %v", err)
	}
}
