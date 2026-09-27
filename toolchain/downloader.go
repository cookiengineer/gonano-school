package toolchain

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gonano-school/toolchain/types"
)

// Filter selects manifest entries. Within and across fields the match is a
// union: an entry is selected when it matches any provided value. An empty
// filter selects everything.
type Filter struct {
	Models     []string
	Categories []string
	Sites      []string
}

func (filter Filter) empty() bool {
	return len(filter.Models) == 0 && len(filter.Categories) == 0 && len(filter.Sites) == 0
}

// Match reports whether a manifest entry passes the filter.
func (filter Filter) Match(key string, dataset types.Dataset) bool {
	if filter.empty() {
		return true
	}
	for _, model := range filter.Models {
		if dataset.Model == model {
			return true
		}
	}
	for _, category := range filter.Categories {
		if containsString(dataset.Categories, category) {
			return true
		}
	}
	for _, site := range filter.Sites {
		if dataset.Site == site || types.KeyCategory(key) == site {
			return true
		}
	}
	return false
}

// Item is one selected manifest entry.
type Item struct {
	Key     string
	Dataset types.Dataset
}

// Select returns the manifest entries matching the filter, in key order.
func Select(manifest types.Manifest, filter Filter) []Item {
	items := make([]Item, 0, len(manifest))
	for _, key := range manifest.Keys() {
		if filter.Match(key, manifest[key]) {
			items = append(items, Item{Key: key, Dataset: manifest[key]})
		}
	}
	return items
}

// SelectionSize returns the summed size of the selected items.
func SelectionSize(items []Item) int64 {
	var total int64
	for _, item := range items {
		if item.Dataset.Size > 0 {
			total += item.Dataset.Size
		}
	}
	return total
}

// DownloadOptions configures a download run.
type DownloadOptions struct {
	RootDir    string
	Jobs       int
	MaxBytes   int64
	DryRun     bool
	Retry      int
	RetryDelay time.Duration
	HTTPClient *http.Client
	Logf       func(format string, args ...any)
}

// Download fetches every item into RootDir/<key>. Existing files with the
// expected size are skipped. Files are written to a ".part" sibling and renamed
// on success. MaxBytes aborts the run before any file is written.
func Download(ctx context.Context, items []Item, options DownloadOptions) error {
	if options.RootDir == "" {
		options.RootDir = "."
	}
	if options.Jobs <= 0 {
		options.Jobs = 1
	}
	if options.Retry < 0 {
		options.Retry = 0
	}
	if options.RetryDelay <= 0 {
		options.RetryDelay = time.Second
	}

	total := SelectionSize(items)
	if options.MaxBytes > 0 && total > options.MaxBytes {
		return fmt.Errorf("download: selection is %d bytes, exceeds limit %d bytes", total, options.MaxBytes)
	}
	logf(options, "%d dataset(s), %d bytes", len(items), total)
	if options.DryRun {
		for _, item := range items {
			logf(options, "would download %s (%d bytes)", item.Key, item.Dataset.Size)
		}
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan Item)
	errs := make(chan error, 1)
	var waitGroup sync.WaitGroup
	for worker := 0; worker < options.Jobs; worker++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for item := range jobs {
				if err := downloadOne(ctx, item, options); err != nil {
					select {
					case errs <- err:
						cancel()
					default:
					}
					return
				}
			}
		}()
	}

	for _, item := range items {
		select {
		case jobs <- item:
		case <-ctx.Done():
		}
	}
	close(jobs)
	waitGroup.Wait()

	select {
	case err := <-errs:
		return err
	default:
		return ctx.Err()
	}
}

func downloadOne(ctx context.Context, item Item, options DownloadOptions) error {
	destination := filepath.Join(options.RootDir, filepath.FromSlash(item.Key))
	if info, err := os.Stat(destination); err == nil && info.Mode().IsRegular() {
		if item.Dataset.Size == 0 || info.Size() == item.Dataset.Size {
			logf(options, "skip %s (present, %d bytes)", item.Key, info.Size())
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return fmt.Errorf("download %s: %w", item.Key, err)
	}

	var lastErr error
	for attempt := 0; attempt <= options.Retry; attempt++ {
		if attempt > 0 {
			delay := options.RetryDelay
			for backoff := 1; backoff < attempt; backoff++ {
				delay *= 2
			}
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if err := fetchToFile(ctx, item.Dataset.URL, destination, options.HTTPClient); err != nil {
			lastErr = err
			logf(options, "retry %s: %v", item.Key, err)
			continue
		}
		if item.Dataset.Size > 0 {
			info, err := os.Stat(destination)
			if err != nil || info.Size() != item.Dataset.Size {
				lastErr = fmt.Errorf("size mismatch for %s", item.Key)
				os.Remove(destination)
				continue
			}
		}
		logf(options, "downloaded %s (%d bytes)", item.Key, item.Dataset.Size)
		return nil
	}
	return fmt.Errorf("download %s: %w", item.Key, lastErr)
}

func fetchToFile(ctx context.Context, rawURL, destination string, client *http.Client) error {
	if client == nil {
		client = http.DefaultClient
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", DefaultUserAgent)
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(response.Body, 1<<16))
		return fmt.Errorf("status %d", response.StatusCode)
	}

	tmp := destination + ".part"
	file, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, response.Body); err != nil {
		file.Close()
		os.Remove(tmp)
		return err
	}
	if err := file.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, destination); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func logf(options DownloadOptions, format string, args ...any) {
	if options.Logf != nil {
		options.Logf(format, args...)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
