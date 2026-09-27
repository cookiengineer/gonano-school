package toolchain

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// zim2mdInstallHint is appended to LookPath failures so the fix is obvious.
const zim2mdInstallHint = "go install github.com/cookiengineer/zim2md@latest"

// ExtractOptions configures a ZIM to Markdown run.
//
// Jobs is the number of zim2md processes started in parallel; Workers is the
// per-process zim2md worker count. Keeping them separate avoids starting Jobs
// processes that each spawn Workers goroutines and oversubscribe the host.
type ExtractOptions struct {
	RootDir     string // root directory the manifest keys are relative to
	MarkdownDir string // output root; default <RootDir>/datasets/markdown
	Zim2md      string // zim2md binary or path; default "zim2md"
	Jobs        int    // parallel zim2md processes
	Workers     int    // zim2md --workers per process
	Force       bool   // re-extract archives whose output already exists
	KeepGoing   bool   // continue after an archive fails, aggregate errors
	DryRun      bool   // print the plan without invoking zim2md
	Logf        func(format string, args ...any)
}

// Extract converts every selected ZIM archive to Markdown with zim2md. Archives
// whose output directory already exists and is non-empty are skipped unless
// Force is set. zim2md is always run without --assets; only text pages become
// .md files, which is all the trainer consumes.
func Extract(ctx context.Context, items []Item, options ExtractOptions) error {
	if options.RootDir == "" {
		options.RootDir = "."
	}
	if options.MarkdownDir == "" {
		options.MarkdownDir = filepath.Join(options.RootDir, "datasets", "markdown")
	}
	if options.Jobs <= 0 {
		options.Jobs = 1
	}
	if options.Workers <= 0 {
		options.Workers = 1
	}

	binary := options.Zim2md
	if binary == "" {
		binary = "zim2md"
	}
	resolved, err := exec.LookPath(binary)
	if err != nil {
		return fmt.Errorf("extractor: zim2md not found (%s): install with `%s`: %w", binary, zim2mdInstallHint, err)
	}

	if options.DryRun {
		for _, item := range items {
			outDir, err := archiveOutputDir(options, item)
			if err != nil {
				return err
			}
			if !options.Force && outputPresent(outDir) {
				emit(options.Logf, "would skip %s (already extracted)", item.Key)
				continue
			}
			emit(options.Logf, "would extract %s -> %s", item.Key, outDir)
		}
		return nil
	}

	if err := os.MkdirAll(options.MarkdownDir, 0o755); err != nil {
		return fmt.Errorf("extractor: mkdir %s: %w", options.MarkdownDir, err)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan Item)
	errs := make(chan error, 1)
	var mutex sync.Mutex
	var collected []error

	var waitGroup sync.WaitGroup
	for worker := 0; worker < options.Jobs; worker++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for item := range jobs {
				if err := extractOne(ctx, item, options, resolved); err != nil {
					if options.KeepGoing {
						mutex.Lock()
						collected = append(collected, err)
						mutex.Unlock()
						continue
					}
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

	if options.KeepGoing && len(collected) > 0 {
		return errors.Join(collected...)
	}
	select {
	case err := <-errs:
		return err
	default:
		return ctx.Err()
	}
}

func extractOne(ctx context.Context, item Item, options ExtractOptions, binary string) error {
	zimPath := filepath.Join(options.RootDir, filepath.FromSlash(item.Key))
	if info, err := os.Stat(zimPath); err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("extractor: %s: zim archive not found", item.Key)
	}

	outDir, err := archiveOutputDir(options, item)
	if err != nil {
		return err
	}
	if !options.Force && outputPresent(outDir) {
		emit(options.Logf, "skip %s (already extracted)", item.Key)
		return nil
	}
	if options.Force {
		if err := os.RemoveAll(outDir); err != nil {
			return fmt.Errorf("extractor: %s: %w", item.Key, err)
		}
	}

	args := []string{
		"--output", options.MarkdownDir,
		"--workers", strconv.Itoa(options.Workers),
		"--quiet",
	}
	if !options.Force {
		args = append(args, "--no-clobber")
	}
	args = append(args, zimPath)

	command := exec.CommandContext(ctx, binary, args...)
	output, err := command.CombinedOutput()
	if err != nil {
		if message := strings.TrimSpace(string(output)); message != "" {
			return fmt.Errorf("extractor: %s: %w: %s", item.Key, err, message)
		}
		return fmt.Errorf("extractor: %s: %w", item.Key, err)
	}
	emit(options.Logf, "extracted %s -> %s", item.Key, outDir)
	return nil
}

// archiveOutputDir is the directory zim2md writes an archive's Markdown into
// (<MarkdownDir>/<archive-basename>). It fails on a malformed manifest key.
func archiveOutputDir(options ExtractOptions, item Item) (string, error) {
	name := archiveName(item.Key)
	if name == "" {
		return "", fmt.Errorf("extractor: malformed manifest key %q", item.Key)
	}
	return filepath.Join(options.MarkdownDir, name), nil
}

// outputPresent reports whether dir exists and holds at least one entry.
func outputPresent(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) > 0
}
