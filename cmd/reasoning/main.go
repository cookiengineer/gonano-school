// Command reasoning downloads HuggingFace reasoning datasets (DeepSeek-R1
// traces) and flattens them into the gonano-base Markdown corpus plus a JSONL
// conversation set for supervised fine-tuning.
//
// With -repo it ingests that single dataset. With no -repo it ingests every
// dataset tagged "reasoning" in the manifest (the statically required ones from
// reasoning.json), so the whole required set is reproducible in one command.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"gonano-school/toolchain"
)

func main() {
	repo := flag.String("repo", "", "single HuggingFace dataset repo; empty ingests every required reasoning dataset from the manifest")
	config := flag.String("config", "", "dataset config (single-repo mode; default: dataset default)")
	split := flag.String("split", "", "dataset split (single-repo mode; default: dataset default)")
	slug := flag.String("slug", "", "output slug (single-repo mode; default: magpie-reasoning-v2)")
	category := flag.String("category", toolchain.CategoryBase, "category whose model consumes the traces (single-repo mode)")
	datasetsDir := flag.String("datasets", "datasets", "directory the per-category manifest files live in (all-mode)")
	root := flag.String("root", ".", "root directory the datasets tree is written under")
	markdown := flag.String("markdown", "", "markdown output directory (default <root>/datasets/markdown/<slug>)")
	jsonl := flag.String("jsonl", "", "JSONL output path (default <root>/datasets/reasoning/<slug>.jsonl)")
	cache := flag.String("cache", "", "shard cache directory (default <root>/datasets/reasoning/<slug>)")
	limit := flag.Int("limit", 0, "maximum records per dataset (0 = all)")
	maxTokens := flag.Int("max-tokens", 0, "skip traces longer than this many tokens (0 = no limit; only checked where the dataset reports token counts)")
	recordsPerFile := flag.Int("records-per-file", 1000, "markdown documents per file")
	keepShards := flag.Bool("keep-shards", false, "keep downloaded parquet shards")
	dryRun := flag.Bool("dry-run", false, "print the plan, download nothing")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logf := func(format string, args ...any) {
		fmt.Printf(format+"\n", args...)
	}
	base := toolchain.ReasoningOptions{
		RootDir:        *root,
		MarkdownDir:    *markdown,
		JSONLPath:      *jsonl,
		CacheDir:       *cache,
		Limit:          *limit,
		MaxTokens:      *maxTokens,
		RecordsPerFile: *recordsPerFile,
		KeepShards:     *keepShards,
		DryRun:         *dryRun,
		Logf:           logf,
	}

	if *repo != "" {
		base.Repo = *repo
		base.Config = *config
		base.Split = *split
		base.Slug = *slug
		base.Category = *category
		if err := runOne(ctx, base); err != nil {
			fmt.Fprintln(os.Stderr, "reasoning:", err)
			os.Exit(1)
		}
		return
	}

	manifest, err := toolchain.LoadManifests(*datasetsDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "reasoning:", err)
		os.Exit(1)
	}
	items := toolchain.ReasoningItems(manifest)
	if len(items) == 0 {
		fmt.Fprintln(os.Stderr, "reasoning: no reasoning datasets in the manifests")
		os.Exit(1)
	}
	for _, item := range items {
		if item.Dataset.Repo == "" {
			fmt.Fprintf(os.Stderr, "reasoning: %s: missing repo\n", item.Key)
			os.Exit(1)
		}
		options := base
		options.Repo = item.Dataset.Repo
		options.Config = item.Dataset.Config
		options.Split = item.Dataset.Split
		options.Slug = toolchain.SlugForReasoningKey(item.Key)
		options.Category = toolchain.ModelCategory(item.Dataset.Model)
		logf("reasoning: ingesting %s (%s)", options.Slug, options.Repo)
		if err := runOne(ctx, options); err != nil {
			fmt.Fprintln(os.Stderr, "reasoning:", err)
			os.Exit(1)
		}
	}
}

func runOne(ctx context.Context, options toolchain.ReasoningOptions) error {
	summary, err := toolchain.RunReasoning(ctx, options)
	if err != nil {
		return err
	}
	if !options.DryRun {
		fmt.Printf("reasoning: wrote %d markdown file(s) and %s\n", len(summary.Markdown), summary.JSONLPath)
	}
	return nil
}
