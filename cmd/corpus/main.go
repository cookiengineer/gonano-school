// Command corpus assembles per-model link trees from the extracted Markdown,
// one directory per gonano model under datasets/corpus/.
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
	markdown := flag.String("markdown", "", "markdown source root (default <root>/datasets/markdown)")
	corpus := flag.String("corpus", "", "corpus output root (default <root>/datasets/corpus)")
	link := flag.String("link", "symlink", "link mechanism: symlink|hardlink")
	force := flag.Bool("force", false, "rebuild selected model directories from scratch")
	dryRun := flag.Bool("dry-run", false, "print the plan, write nothing")
	var models, categories, sites stringList
	flag.Var(&models, "model", "select by model name, e.g. gonano-science (repeatable)")
	flag.Var(&categories, "category", "select by category, e.g. science (repeatable)")
	flag.Var(&sites, "site", "select by kiwix category, e.g. wikipedia (repeatable)")
	flag.Parse()

	manifest, err := toolchain.LoadManifest(*manifestPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "corpus:", err)
		os.Exit(1)
	}

	items := toolchain.Select(manifest, toolchain.Filter{
		Models:     models,
		Categories: categories,
		Sites:      sites,
	})
	if len(items) == 0 {
		fmt.Fprintln(os.Stderr, "corpus: no datasets matched the given filters")
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err = toolchain.Corpus(ctx, items, toolchain.CorpusOptions{
		RootDir:     *root,
		MarkdownDir: *markdown,
		CorpusDir:   *corpus,
		Link:        toolchain.LinkMode(*link),
		Force:       *force,
		DryRun:      *dryRun,
		Logf: func(format string, args ...any) {
			fmt.Printf(format+"\n", args...)
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "corpus:", err)
		os.Exit(1)
	}
}
