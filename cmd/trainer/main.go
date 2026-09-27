// Command trainer orchestrates the gonano training pipeline for the selected
// models: shared tokenizer, gonano-base from scratch, then one
// continued-pretrain run per specialty category from the base checkpoint.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"gonano-school/toolchain"
)

// stringList collects a repeatable string flag.
type stringList []string

func (list *stringList) String() string { return strings.Join(*list, ",") }

func (list *stringList) Set(value string) error {
	*list = append(*list, value)
	return nil
}

func main() {
	datasetsDir := flag.String("datasets", "datasets", "directory the per-category manifest files live in")
	root := flag.String("root", ".", "root directory the manifest keys are relative to")
	gonanoDir := flag.String("gonano-dir", "", "gonano source checkout (required); commands run via `go run ./cmd/...`")
	baseDir := flag.String("base-dir", "", "gonano cache/checkpoint dir (default $GONANO_BASE_DIR or ~/.cache/gonano)")
	modelTag := flag.String("model-tag", "", "checkpoint directory name (default d<depth>)")
	vocabSize := flag.Int("vocab-size", toolchain.DefaultTrainVocabSize, "tokenizer vocabulary size")
	depth := flag.Int("depth", toolchain.DefaultTrainDepth, "base transformer depth (complexity dial)")
	numIterations := flag.Int("num-iterations", toolchain.DefaultTrainNumIterations, "optimization steps per model")
	deviceBatchSize := flag.Int("device-batch-size", toolchain.DefaultTrainDeviceBatchSize, "per-step batch size")
	preset := flag.String("preset", toolchain.DefaultTrainPreset, "base architecture preset")
	maxSeqLen := flag.Int("max-seq-len", toolchain.DefaultTrainMaxSeqLen, "base context length")
	maxChars := flag.Int("max-chars", toolchain.DefaultTrainMaxChars, "tokenizer training character budget")
	trainTokenizer := flag.Bool("train-tokenizer", true, "train the shared tokenizer once on the base corpus")
	forceTokenizer := flag.Bool("force-tokenizer", false, "retrain the tokenizer even if one already exists")
	reuseBase := flag.Bool("reuse-base", false, "skip the base run and continue from an existing base checkpoint")
	keepGoing := flag.Bool("keep-going", false, "continue after a specialty model fails")
	dryRun := flag.Bool("dry-run", false, "print the plan, run nothing")
	var models, categories, sites stringList
	flag.Var(&models, "model", "select by model name, e.g. gonano-physics (repeatable)")
	flag.Var(&categories, "category", "select by category, e.g. physics (repeatable)")
	flag.Var(&sites, "site", "select by kiwix category, e.g. phet (repeatable)")
	flag.Parse()

	manifest, err := toolchain.LoadManifests(*datasetsDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "trainer:", err)
		os.Exit(1)
	}

	items := toolchain.Select(manifest, toolchain.Filter{
		Models:     models,
		Categories: categories,
		Sites:      sites,
	})
	if len(items) == 0 {
		fmt.Fprintln(os.Stderr, "trainer: no datasets matched the given filters")
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err = toolchain.Train(ctx, items, toolchain.TrainOptions{
		RootDir:         *root,
		DatasetsDir:     *datasetsDir,
		BaseDir:         *baseDir,
		GonanoDir:       *gonanoDir,
		VocabSize:       *vocabSize,
		Depth:           *depth,
		ModelTag:        *modelTag,
		NumIterations:   *numIterations,
		DeviceBatchSize: *deviceBatchSize,
		Preset:          *preset,
		MaxSeqLen:       *maxSeqLen,
		MaxChars:        *maxChars,
		TrainTokenizer:  *trainTokenizer,
		ForceTokenizer:  *forceTokenizer,
		ReuseBase:       *reuseBase,
		KeepGoing:       *keepGoing,
		DryRun:          *dryRun,
		Logf: func(format string, args ...any) {
			fmt.Printf(format+"\n", args...)
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "trainer:", err)
		os.Exit(1)
	}
}
