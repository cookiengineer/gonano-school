// Command updater scrapes the Kiwix catalog and regenerates manifest.json.
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
	out := flag.String("out", "manifest.json", "manifest output path")
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
	if err := toolchain.SaveManifest(*out, manifest); err != nil {
		fmt.Fprintln(os.Stderr, "updater:", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s: %d datasets, %d bytes\n", *out, len(manifest), manifest.TotalSize())
}
