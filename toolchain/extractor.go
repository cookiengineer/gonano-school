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

	"gonano-school/toolchain/types"
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
	DatasetsDir string // dataset root; default <RootDir>/datasets
	Zim2md      string // zim2md binary or path; default "zim2md"
	Jobs        int    // parallel zim2md processes
	Workers     int    // zim2md --workers per process
	Force       bool   // re-extract archives whose output already exists
	KeepGoing   bool   // continue after an archive fails, aggregate errors
	DryRun      bool   // print the plan without invoking zim2md
	Logf        func(format string, args ...any)
}

// Extract converts every selected ZIM archive to Markdown with zim2md. Each
// archive's Markdown lands beside its archive under
// <DatasetsDir>/<category>/markdown/<archive>/, so a category directory is
// already the complete training input for its model -- there is no separate
// corpus/symlink step. Archives whose output directory already exists and is
// non-empty are skipped unless Force is set. zim2md is always run without
// --assets; only text pages become .md files, which is all the trainer consumes.
func Extract(ctx context.Context, items []Item, options ExtractOptions) error {
	if options.RootDir == "" {
		options.RootDir = "."
	}
	if options.DatasetsDir == "" {
		options.DatasetsDir = filepath.Join(options.RootDir, "datasets")
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
			_, outDir, err := archivePaths(options, item)
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

	outRoot, outDir, err := archivePaths(options, item)
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
	if err := os.MkdirAll(outRoot, 0o755); err != nil {
		return fmt.Errorf("extractor: %s: %w", item.Key, err)
	}

	args := []string{
		"--output", outRoot,
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

// archivePaths returns the zim2md output root for an archive's category and the
// archive's specific Markdown directory. It fails on a malformed manifest key.
func archivePaths(options ExtractOptions, item Item) (root, outDir string, err error) {
	category := types.KeyCategory(item.Key)
	name := archiveName(item.Key)
	if category == "" || name == "" {
		return "", "", fmt.Errorf("extractor: malformed manifest key %q", item.Key)
	}
	root = filepath.Join(options.DatasetsDir, category, "markdown")
	return root, filepath.Join(root, name), nil
}

// outputPresent reports whether dir exists and holds at least one entry.
func outputPresent(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) > 0
}
