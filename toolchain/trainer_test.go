package toolchain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gonano-school/toolchain/types"
)

func trainManifest() types.Manifest {
	return types.Manifest{
		"datasets/phet/base.zim": {
			Categories: []string{CategoryBase},
			Model:      ModelName(CategoryBase),
		},
		"datasets/phet/physics.zim": {
			Categories: []string{CategoryPhysics},
			Model:      ModelName(CategoryPhysics),
		},
	}
}

func writeCorpus(t *testing.T, corpusDir, model string) {
	t.Helper()
	dir := filepath.Join(corpusDir, model)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir corpus: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "page.md"), []byte("# doc"), 0o644); err != nil {
		t.Fatalf("write corpus: %v", err)
	}
}

func argValue(args []string, flag string) string {
	for index, arg := range args {
		if arg == flag && index+1 < len(args) {
			return args[index+1]
		}
	}
	return ""
}

func hasEnv(env []string, want string) bool {
	for _, entry := range env {
		if entry == want {
			return true
		}
	}
	return false
}

// baseCreatingRunner records commands and fakes the base_train base run by
// writing the checkpoint the wrapper then discovers.
func baseCreatingRunner(t *testing.T, record *[]TrainCommand, baseDir, tag string, step int) CommandRunner {
	t.Helper()
	return func(ctx context.Context, command TrainCommand) error {
		*record = append(*record, command)
		if len(command.Args) > 1 && command.Args[1] == "./cmd/base_train" && argValue(command.Args, "--domain") == CategoryBase {
			dir := domainCheckpointDir(baseDir, CategoryBase, tag)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatalf("mkdir checkpoint dir: %v", err)
			}
			if err := os.WriteFile(expectedCheckpoint(dir, step), []byte("ckpt"), 0o644); err != nil {
				t.Fatalf("write checkpoint: %v", err)
			}
		}
		return nil
	}
}

func TestTrainCommandSequence(t *testing.T) {
	root := t.TempDir()
	corpusDir := filepath.Join(root, "datasets", "corpus")
	writeCorpus(t, corpusDir, ModelName(CategoryBase))
	writeCorpus(t, corpusDir, ModelName(CategoryPhysics))
	baseDir := t.TempDir()
	gonanoDir := t.TempDir()

	record := []TrainCommand{}
	items := Select(trainManifest(), Filter{})
	err := Train(context.Background(), items, TrainOptions{
		RootDir:        root,
		CorpusDir:      corpusDir,
		BaseDir:        baseDir,
		GonanoDir:      gonanoDir,
		Depth:          4,
		NumIterations:  5,
		TrainTokenizer: true,
		Run:            baseCreatingRunner(t, &record, baseDir, "d4", 5),
	})
	if err != nil {
		t.Fatalf("Train: %v", err)
	}

	if len(record) != 3 {
		t.Fatalf("ran %d commands, want 3: %#v", len(record), record)
	}

	tokenizer := record[0]
	if tokenizer.Args[1] != "./cmd/tok_train" {
		t.Fatalf("first command = %#v, want tok_train", tokenizer.Args)
	}
	for _, want := range []string{"--data-format", "markdown", "--vocab-size", "32768"} {
		if !containsArg(tokenizer.Args, want) {
			t.Errorf("tok_train argv missing %q: %#v", want, tokenizer.Args)
		}
	}

	base := record[1]
	if base.Args[1] != "./cmd/base_train" || argValue(base.Args, "--domain") != CategoryBase {
		t.Fatalf("second command = %#v, want base_train --domain base", base.Args)
	}
	if containsArg(base.Args, "--init-model") {
		t.Errorf("base run must not pass --init-model: %#v", base.Args)
	}

	specialty := record[2]
	if specialty.Args[1] != "./cmd/base_train" || argValue(specialty.Args, "--domain") != CategoryPhysics {
		t.Fatalf("third command = %#v, want base_train --domain physics", specialty.Args)
	}
	checkpoint := expectedCheckpoint(domainCheckpointDir(baseDir, CategoryBase, "d4"), 5)
	if got := argValue(specialty.Args, "--init-model"); got != checkpoint {
		t.Fatalf("--init-model = %q, want %q", got, checkpoint)
	}

	for _, command := range record {
		if command.Name != "go" || command.Dir != gonanoDir {
			t.Errorf("command %#v not run via go in gonano dir", command)
		}
		if !hasEnv(command.Env, "GOEXPERIMENT=simd") {
			t.Errorf("command %#v missing GOEXPERIMENT=simd", command.Args)
		}
	}
}

func TestTrainReusesBaseAndTokenizer(t *testing.T) {
	root := t.TempDir()
	corpusDir := filepath.Join(root, "datasets", "corpus")
	writeCorpus(t, corpusDir, ModelName(CategoryBase))
	writeCorpus(t, corpusDir, ModelName(CategoryPhysics))
	baseDir := t.TempDir()

	tokenizerDir := filepath.Join(baseDir, "tokenizer")
	if err := os.MkdirAll(tokenizerDir, 0o755); err != nil {
		t.Fatalf("mkdir tokenizer: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tokenizerDir, "tokenizer.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write tokenizer: %v", err)
	}
	checkpointDir := domainCheckpointDir(baseDir, CategoryBase, "d4")
	if err := os.MkdirAll(checkpointDir, 0o755); err != nil {
		t.Fatalf("mkdir checkpoint: %v", err)
	}
	existing := expectedCheckpoint(checkpointDir, 9)
	if err := os.WriteFile(existing, []byte("ckpt"), 0o644); err != nil {
		t.Fatalf("write checkpoint: %v", err)
	}

	record := []TrainCommand{}
	items := Select(trainManifest(), Filter{})
	err := Train(context.Background(), items, TrainOptions{
		RootDir:        root,
		CorpusDir:      corpusDir,
		BaseDir:        baseDir,
		GonanoDir:      t.TempDir(),
		Depth:          4,
		NumIterations:  5,
		TrainTokenizer: true,
		ReuseBase:      true,
		Run: func(ctx context.Context, command TrainCommand) error {
			record = append(record, command)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Train: %v", err)
	}
	if len(record) != 1 {
		t.Fatalf("ran %d commands, want 1 (specialty only): %#v", len(record), record)
	}
	if got := argValue(record[0].Args, "--init-model"); got != existing {
		t.Fatalf("--init-model = %q, want reused %q", got, existing)
	}
}

func TestTrainRequiresGonanoDir(t *testing.T) {
	err := Train(context.Background(), nil, TrainOptions{})
	if err == nil || !strings.Contains(err.Error(), "--gonano-dir is required") {
		t.Fatalf("expected gonano-dir error, got %v", err)
	}
}

func TestTrainMissingSpecialtyCorpus(t *testing.T) {
	root := t.TempDir()
	corpusDir := filepath.Join(root, "datasets", "corpus")
	writeCorpus(t, corpusDir, ModelName(CategoryBase))
	baseDir := t.TempDir()

	record := []TrainCommand{}
	items := Select(trainManifest(), Filter{})
	err := Train(context.Background(), items, TrainOptions{
		RootDir:        root,
		CorpusDir:      corpusDir,
		BaseDir:        baseDir,
		GonanoDir:      t.TempDir(),
		Depth:          4,
		NumIterations:  5,
		TrainTokenizer: true,
		Run:            baseCreatingRunner(t, &record, baseDir, "d4", 5),
	})
	if err == nil || !strings.Contains(err.Error(), ModelName(CategoryPhysics)) {
		t.Fatalf("expected missing corpus error, got %v", err)
	}
	// tokenizer + base only; the specialty command must not run.
	if len(record) != 2 {
		t.Fatalf("ran %d commands, want 2: %#v", len(record), record)
	}
}

func TestTrainDryRunExecutesNothing(t *testing.T) {
	root := t.TempDir()
	corpusDir := filepath.Join(root, "datasets", "corpus")
	writeCorpus(t, corpusDir, ModelName(CategoryBase))
	baseDir := t.TempDir()

	record := []TrainCommand{}
	items := Select(trainManifest(), Filter{Models: []string{ModelName(CategoryBase)}})
	err := Train(context.Background(), items, TrainOptions{
		RootDir:        root,
		CorpusDir:      corpusDir,
		BaseDir:        baseDir,
		GonanoDir:      t.TempDir(),
		Depth:          4,
		NumIterations:  5,
		TrainTokenizer: true,
		DryRun:         true,
		Run: func(ctx context.Context, command TrainCommand) error {
			record = append(record, command)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Train dry-run: %v", err)
	}
	if len(record) != 0 {
		t.Fatalf("dry-run executed %d commands: %#v", len(record), record)
	}
}

func TestWithEnvReplaces(t *testing.T) {
	env := withEnv([]string{"PATH=/bin", "GOEXPERIMENT=old", "HOME=/root"}, "GOEXPERIMENT", "simd")
	count := 0
	for _, entry := range env {
		if strings.HasPrefix(entry, "GOEXPERIMENT=") {
			count++
			if entry != "GOEXPERIMENT=simd" {
				t.Fatalf("entry = %q, want GOEXPERIMENT=simd", entry)
			}
		}
	}
	if count != 1 {
		t.Fatalf("GOEXPERIMENT appears %d times, want 1", count)
	}
}
