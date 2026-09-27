// Command downloader fetches the ZIM archives selected from manifest.json.
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
	var models, categories, sites stringList
	flag.Var(&models, "model", "select by model name, e.g. gonano-science (repeatable)")
	flag.Var(&categories, "category", "select by category, e.g. science (repeatable)")
	flag.Var(&sites, "site", "select by kiwix category, e.g. wikipedia (repeatable)")
	maxBytes := flag.Int64("max-bytes", 0, "abort if the selection exceeds this many bytes (0 = no limit)")
	dryRun := flag.Bool("dry-run", false, "print what would be downloaded, write nothing")
	jobs := flag.Int("jobs", 4, "parallel downloads")
	retry := flag.Int("retry", 4, "download retries")
	flag.Parse()

	manifest, err := toolchain.LoadManifests(*datasetsDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "downloader:", err)
		os.Exit(1)
	}

	// Reasoning datasets are fetched by cmd/reasoning, not as ZIM archives.
	items := toolchain.OnlyZIM(toolchain.Select(manifest, toolchain.Filter{
		Models:     models,
		Categories: categories,
		Sites:      sites,
	}))
	if len(items) == 0 {
		fmt.Fprintln(os.Stderr, "downloader: no datasets matched the given filters")
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err = toolchain.Download(ctx, items, toolchain.DownloadOptions{
		RootDir:  *root,
		Jobs:     *jobs,
		MaxBytes: *maxBytes,
		DryRun:   *dryRun,
		Retry:    *retry,
		Logf: func(format string, args ...any) {
			fmt.Printf(format+"\n", args...)
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "downloader:", err)
		os.Exit(1)
	}
}
