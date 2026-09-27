// Command updater scrapes the Kiwix catalog and regenerates the per-category
// manifest files (datasets/<category>.json). Static files such as
// datasets/reasoning.json are never touched, so hand-maintained corpora
// survive every update.
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
	datasets := flag.String("datasets", "datasets", "directory the per-category manifest files live in")
	lang := flag.String("lang", "eng", "catalog language filter")
	count := flag.Int("count", 100, "catalog page size")
	baseURL := flag.String("base-url", toolchain.DefaultCatalogBase, "catalog base URL")
	keepGoing := flag.Bool("keep-going", false, "skip categories that fail instead of aborting")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	manifest, err := toolchain.Update(ctx, toolchain.UpdateOptions{
		BaseURL:   *baseURL,
		Lang:      *lang,
		Count:     *count,
		KeepGoing: *keepGoing,
		Warnf: func(format string, args ...any) {
			fmt.Fprintf(os.Stderr, "warning: "+format+"\n", args...)
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "updater:", err)
		os.Exit(1)
	}

	// The updater owns exactly the catalog categories; reasoning.json and any
	// other unmanaged file are left alone.
	if err := toolchain.SaveManifests(*datasets, manifest, toolchain.Categories); err != nil {
		fmt.Fprintln(os.Stderr, "updater:", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s/<category>.json: %d datasets, %d bytes\n", *datasets, len(manifest), manifest.TotalSize())
}
