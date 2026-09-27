package toolchain

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Defaults for the training wrapper. VocabSize and Depth mirror gonano's own
// defaults (model.DefaultVocabSize and model.DefaultDepth); NumIterations is
// chosen for the gonano-school corpora. All are meant to be overridden on the
// command line.
const (
	DefaultTrainVocabSize       = 131072
	DefaultTrainDepth           = 20
	DefaultTrainPreset          = "flash"
	DefaultTrainMaxSeqLen       = 512
	DefaultTrainNumIterations   = 200
	DefaultTrainDeviceBatchSize = 1
	DefaultTrainMaxChars        = 2000000
)

// GOExperimentValue is the GOEXPERIMENT gonano's simd build tag requires.
const GOExperimentValue = "simd"

// goExperiment is the environment variable gonano's simd package reads.
const goExperiment = "GOEXPERIMENT"

// TrainCommand is one external process the training wrapper runs.
type TrainCommand struct {
	Name string
	Args []string
	Dir  string
	Env  []string
}

// CommandRunner executes a TrainCommand. It is a seam so tests can assert the
// generated argv and environment without spawning gonano.
type CommandRunner func(ctx context.Context, command TrainCommand) error

// TrainOptions configures the orchestration run.
type TrainOptions struct {
	RootDir         string // default "."
	DatasetsDir     string // default <RootDir>/datasets; each model reads <DatasetsDir>/<category>/ directly
	BaseDir         string // gonano cache/checkpoint dir; default $GONANO_BASE_DIR or ~/.cache/gonano
	GonanoDir       string // gonano source checkout (required); commands run via `go run ./cmd/...`
	VocabSize       int
	Depth           int
	ModelTag        string // checkpoint directory name; default d<Depth>
	NumIterations   int
	DeviceBatchSize int
	Preset          string
	MaxSeqLen       int
	MaxChars        int // tok_train character budget
	TrainTokenizer  bool
	ForceTokenizer  bool
	ReuseBase       bool // skip the base run and continue from an existing base checkpoint
	DryRun          bool
	KeepGoing       bool
	Env             []string // base environment; default os.Environ()
	Stdout          io.Writer
	Stderr          io.Writer
	Logf            func(format string, args ...any)
	Run             CommandRunner

	env []string
}

// Train orchestrates the gonano training pipeline for the selected models:
//
//  1. train the shared tokenizer once on the base corpus (tok_train),
//  2. train gonano-base from scratch (base_train --domain base),
//  3. continue-pretrain every other selected model from the base checkpoint
//     (base_train --domain <cat> --init-model <base.gn>).
//
// gonano-base is always trained (or reused) because every specialty model
// continues from it. All commands run with GOEXPERIMENT=simd.
func Train(ctx context.Context, items []Item, options TrainOptions) error {
	options.applyDefaults()
	if options.GonanoDir == "" {
		return fmt.Errorf("trainer: --gonano-dir is required (path to the gonano source checkout)")
	}

	requested := map[string]bool{}
	for _, item := range items {
		if model := item.Dataset.Model; model != "" {
			requested[model] = true
		}
	}
	baseModel := ModelName(CategoryBase)
	baseCorpus := filepath.Join(options.DatasetsDir, CategoryBase)
	baseCheckpointDir := domainCheckpointDir(options.BaseDir, CategoryBase, options.ModelTag)

	// 1) Shared tokenizer.
	if options.TrainTokenizer {
		tokenizerPath := filepath.Join(options.BaseDir, "tokenizer", "tokenizer.json")
		if _, err := os.Stat(tokenizerPath); err == nil && !options.ForceTokenizer {
			emit(options.Logf, "trainer: tokenizer present, keeping %s", tokenizerPath)
		} else {
			if err := options.checkCorpus(baseCorpus); err != nil {
				return fmt.Errorf("trainer: tokenizer: %w", err)
			}
			command := options.gonanoCommand("tok_train",
				"--data-dir", baseCorpus,
				"--data-format", "markdown",
				"--vocab-size", strconv.Itoa(options.VocabSize),
				"--max-chars", strconv.Itoa(options.MaxChars),
				"--base-dir", options.BaseDir,
			)
			if err := options.run(ctx, command); err != nil {
				return fmt.Errorf("trainer: tok_train: %w", err)
			}
		}
	} else {
		emit(options.Logf, "trainer: tokenizer training disabled; base_train will fall back to a byte-level tokenizer")
	}

	// 2) Base checkpoint.
	baseCheckpoint := ""
	if options.ReuseBase {
		if options.DryRun {
			baseCheckpoint = expectedCheckpoint(baseCheckpointDir, options.NumIterations)
		} else {
			found, err := latestCheckpoint(baseCheckpointDir)
			if err != nil {
				return fmt.Errorf("trainer: reuse-base: %w", err)
			}
			baseCheckpoint = found
		}
		emit(options.Logf, "trainer: reusing base checkpoint %s", baseCheckpoint)
	} else {
		if err := options.checkCorpus(baseCorpus); err != nil {
			return fmt.Errorf("trainer: base: %w", err)
		}
		command := options.gonanoCommand("base_train",
			"--domain", CategoryBase,
			"--data-dir", baseCorpus,
			"--data-format", "markdown",
			"--base-dir", options.BaseDir,
			"--model-tag", options.ModelTag,
			"--depth", strconv.Itoa(options.Depth),
			"--preset", options.Preset,
			"--vocab-size", strconv.Itoa(options.VocabSize),
			"--max-seq-len", strconv.Itoa(options.MaxSeqLen),
			"--device-batch-size", strconv.Itoa(options.DeviceBatchSize),
			"--num-iterations", strconv.Itoa(options.NumIterations),
		)
		if err := options.run(ctx, command); err != nil {
			return fmt.Errorf("trainer: base_train (base): %w", err)
		}
		if options.DryRun {
			baseCheckpoint = expectedCheckpoint(baseCheckpointDir, options.NumIterations)
		} else if found, err := latestCheckpoint(baseCheckpointDir); err == nil {
			baseCheckpoint = found
		} else {
			return fmt.Errorf("trainer: base_train (base): %w", err)
		}
	}

	// 3) Specialty continued-pretrain runs.
	specialties := make([]string, 0, len(requested))
	for model := range requested {
		if model != baseModel {
			specialties = append(specialties, model)
		}
	}
	sort.Strings(specialties)

	var collected []error
	for _, model := range specialties {
		if err := ctx.Err(); err != nil {
			return err
		}
		category := ModelCategory(model)
		corpusDir := filepath.Join(options.DatasetsDir, category)
		if err := options.checkCorpus(corpusDir); err != nil {
			wrapped := fmt.Errorf("trainer: %s: %w", model, err)
			if !options.KeepGoing {
				return wrapped
			}
			emit(options.Logf, "trainer: warn: %v", wrapped)
			collected = append(collected, wrapped)
			continue
		}
		command := options.gonanoCommand("base_train",
			"--domain", category,
			"--init-model", baseCheckpoint,
			"--data-dir", corpusDir,
			"--data-format", "markdown",
			"--base-dir", options.BaseDir,
			"--model-tag", options.ModelTag,
			"--device-batch-size", strconv.Itoa(options.DeviceBatchSize),
			"--num-iterations", strconv.Itoa(options.NumIterations),
		)
		if err := options.run(ctx, command); err != nil {
			wrapped := fmt.Errorf("trainer: %s: %w", model, err)
			if !options.KeepGoing {
				return wrapped
			}
			emit(options.Logf, "trainer: warn: %v", wrapped)
			collected = append(collected, wrapped)
		}
	}
	if len(collected) > 0 {
		return errors.Join(collected...)
	}
	return nil
}

func (options *TrainOptions) applyDefaults() {
	if options.RootDir == "" {
		options.RootDir = "."
	}
	if options.DatasetsDir == "" {
		options.DatasetsDir = filepath.Join(options.RootDir, "datasets")
	}
	if options.BaseDir == "" {
		options.BaseDir = defaultBaseDir()
	}
	if options.VocabSize <= 0 {
		options.VocabSize = DefaultTrainVocabSize
	}
	if options.Depth <= 0 {
		options.Depth = DefaultTrainDepth
	}
	if options.ModelTag == "" {
		options.ModelTag = fmt.Sprintf("d%d", options.Depth)
	}
	if options.NumIterations <= 0 {
		options.NumIterations = DefaultTrainNumIterations
	}
	if options.DeviceBatchSize <= 0 {
		options.DeviceBatchSize = DefaultTrainDeviceBatchSize
	}
	if options.Preset == "" {
		options.Preset = DefaultTrainPreset
	}
	if options.MaxSeqLen <= 0 {
		options.MaxSeqLen = DefaultTrainMaxSeqLen
	}
	if options.MaxChars <= 0 {
		options.MaxChars = DefaultTrainMaxChars
	}
	if options.Stdout == nil {
		options.Stdout = os.Stdout
	}
	if options.Stderr == nil {
		options.Stderr = os.Stderr
	}
	if options.Env == nil {
		options.Env = os.Environ()
	}
	options.env = withEnv(options.Env, goExperiment, GOExperimentValue)
	if options.Run == nil {
		options.Run = execRunner(options.Stdout, options.Stderr)
	}
}

// gonanoCommand builds `go run ./cmd/<tool> <args...>` executed in GonanoDir.
func (options TrainOptions) gonanoCommand(tool string, args ...string) TrainCommand {
	full := append([]string{"run", "./cmd/" + tool}, args...)
	return TrainCommand{
		Name: "go",
		Args: full,
		Dir:  options.GonanoDir,
		Env:  options.env,
	}
}

// run logs a command and executes it, unless this is a dry run.
func (options TrainOptions) run(ctx context.Context, command TrainCommand) error {
	emit(options.Logf, "trainer: %s %s", command.Name, strings.Join(command.Args, " "))
	if options.DryRun {
		return nil
	}
	return options.Run(ctx, command)
}

// checkCorpus verifies that a corpus holds at least one Markdown file. In a dry
// run a missing corpus is only reported, so the plan can be previewed before
// the extraction and corpus stages have run.
func (options TrainOptions) checkCorpus(dir string) error {
	if options.DryRun {
		if !outputPresent(dir) {
			emit(options.Logf, "trainer: warn: corpus %s not present", dir)
		}
		return nil
	}
	return requireCorpus(dir)
}

// requireCorpus returns an error unless dir contains at least one Markdown file
// reachable through the corpus link tree.
func requireCorpus(dir string) error {
	files, err := collectMarkdownFiles(dir)
	if err != nil {
		return fmt.Errorf("corpus %s: %w", dir, err)
	}
	if len(files) == 0 {
		return fmt.Errorf("corpus %s: no markdown files", dir)
	}
	return nil
}

// domainCheckpointDir is where base_train writes a domain's checkpoints.
func domainCheckpointDir(baseDir, domain, tag string) string {
	return filepath.Join(baseDir, "domains", domain, "base_checkpoints", tag)
}

// expectedCheckpoint is the checkpoint path base_train writes after step steps.
func expectedCheckpoint(dir string, step int) string {
	return filepath.Join(dir, fmt.Sprintf("model_%06d.gn", step))
}

// latestCheckpoint returns the highest-step checkpoint in dir. The model_%06d
// naming makes a lexicographic sort equal to a numeric one.
func latestCheckpoint(dir string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "model_*.gn"))
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no checkpoint in %s", dir)
	}
	sort.Strings(matches)
	return matches[len(matches)-1], nil
}

// defaultBaseDir mirrors gonano's data.BaseDir without importing gonano.
func defaultBaseDir() string {
	if dir := os.Getenv("GONANO_BASE_DIR"); dir != "" {
		return dir
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".cache", "gonano")
	}
	return filepath.Join(".", ".cache", "gonano")
}

// withEnv sets key=value, replacing any existing entry.
func withEnv(base []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(base)+1)
	for _, entry := range base {
		if strings.HasPrefix(entry, prefix) {
			continue
		}
		out = append(out, entry)
	}
	return append(out, prefix+value)
}

// execRunner runs commands with inherited stdio.
func execRunner(stdout, stderr io.Writer) CommandRunner {
	return func(ctx context.Context, command TrainCommand) error {
		cmd := exec.CommandContext(ctx, command.Name, command.Args...)
		cmd.Dir = command.Dir
		cmd.Env = command.Env
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s %s: %w", command.Name, strings.Join(command.Args, " "), err)
		}
		return nil
	}
}
