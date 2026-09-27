// Command extractor converts the ZIM archives selected from manifest.json into
// Markdown with zim2md.
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
	manifestPath := flag.String("manifest", "manifest.json", "manifest path")
	root := flag.String("root", ".", "root directory the manifest keys are relative to")
	markdown := flag.String("markdown", "", "markdown output root (default <root>/datasets/markdown)")
	zim2md := flag.String("zim2md", "zim2md", "zim2md binary or path")
	jobs := flag.Int("jobs", 4, "parallel zim2md processes")
	workers := flag.Int("workers", 1, "zim2md workers per process")
	force := flag.Bool("force", false, "re-extract archives that are already present")
	keepGoing := flag.Bool("keep-going", false, "continue after a failing archive")
	dryRun := flag.Bool("dry-run", false, "print the plan, invoke nothing")
	var models, categories, sites stringList
	flag.Var(&models, "model", "select by model name, e.g. gonano-science (repeatable)")
	flag.Var(&categories, "category", "select by category, e.g. science (repeatable)")
	flag.Var(&sites, "site", "select by kiwix category, e.g. wikipedia (repeatable)")
	flag.Parse()

	manifest, err := toolchain.LoadManifest(*manifestPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "extractor:", err)
		os.Exit(1)
	}

	items := toolchain.Select(manifest, toolchain.Filter{
		Models:     models,
		Categories: categories,
		Sites:      sites,
	})
	if len(items) == 0 {
		fmt.Fprintln(os.Stderr, "extractor: no datasets matched the given filters")
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err = toolchain.Extract(ctx, items, toolchain.ExtractOptions{
		RootDir:     *root,
		MarkdownDir: *markdown,
		Zim2md:      *zim2md,
		Jobs:        *jobs,
		Workers:     *workers,
		Force:       *force,
		KeepGoing:   *keepGoing,
		DryRun:      *dryRun,
		Logf: func(format string, args ...any) {
			fmt.Printf(format+"\n", args...)
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "extractor:", err)
		os.Exit(1)
	}
}
