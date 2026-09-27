package toolchain

import (
	"context"
	"fmt"

	"gonano-school/toolchain/types"
)

// UpdateOptions configures a catalog scrape.
type UpdateOptions struct {
	BaseURL   string
	Lang      string
	Count     int
	KeepGoing bool
	// Warnf, when set, receives non-fatal category errors in KeepGoing mode.
	Warnf func(format string, args ...any)
	// Client optionally overrides the catalog client (used by tests).
	Client *Client
}

// Update scrapes every catalog category and builds a manifest. It returns the
// manifest and an error. In KeepGoing mode, per-category failures are reported
// via Warnf and skipped; otherwise the first failure aborts.
func Update(ctx context.Context, options UpdateOptions) (types.Manifest, error) {
	client := options.Client
	if client == nil {
		client = NewClient()
	}
	if client.BaseURL == "" {
		client.BaseURL = options.BaseURL
	}
	if client.Lang == "" {
		client.Lang = options.Lang
	}
	if client.Count == 0 {
		client.Count = options.Count
	}

	categories, err := client.Categories(ctx)
	if err != nil {
		return nil, fmt.Errorf("updater: categories: %w", err)
	}

	manifest := types.Manifest{}
	for _, category := range categories {
		entries, err := client.Entries(ctx, category)
		if err != nil {
			if !options.KeepGoing {
				return manifest, fmt.Errorf("updater: category %q: %w", category, err)
			}
			warnf(options, "updater: category %q skipped: %v", category, err)
			continue
		}

		kept := make([]types.Entry, 0, len(entries))
		for _, entry := range entries {
			if KeepEntry(category, entry) {
				kept = append(kept, entry)
			}
		}
		for _, entry := range SelectFlavour(kept) {
			key, dataset, ok := BuildDataset(category, entry)
			if !ok {
				continue
			}
			manifest[key] = dataset
		}
	}
	return manifest, nil
}

func warnf(options UpdateOptions, format string, args ...any) {
	if options.Warnf != nil {
		options.Warnf(format, args...)
	}
}
