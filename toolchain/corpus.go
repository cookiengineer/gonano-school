package toolchain

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gonano-school/toolchain/types"
)

// LinkMode selects how a corpus file references its extracted Markdown source.
type LinkMode string

const (
	// LinkSymlink creates a symbolic link per .md file (the default). It works
	// across filesystems and is the only mode that survives moving the corpus.
	LinkSymlink LinkMode = "symlink"
	// LinkHardlink creates a hard link per .md file. It uses no extra symlink
	// target storage but requires the corpus and Markdown trees on one
	// filesystem.
	LinkHardlink LinkMode = "hardlink"
)

// CorpusOptions configures corpus assembly.
type CorpusOptions struct {
	RootDir     string   // default "."
	MarkdownDir string   // default <RootDir>/datasets/markdown
	CorpusDir   string   // default <RootDir>/datasets/corpus
	Link        LinkMode // default LinkSymlink
	Force       bool     // rebuild each selected model from scratch
	DryRun      bool     // print the plan without writing anything
	Logf        func(format string, args ...any)
}

// Corpus assembles datasets/corpus/<model>/ as real directories containing one
// link per extracted .md file, mirroring the per-archive Markdown layout. Each
// model only aggregates its own category's archives. Archives that have not
// been extracted are reported and skipped, never fatal, so a corpus can be
// built incrementally.
func Corpus(ctx context.Context, items []Item, options CorpusOptions) error {
	if options.RootDir == "" {
		options.RootDir = "."
	}
	if options.MarkdownDir == "" {
		options.MarkdownDir = filepath.Join(options.RootDir, "datasets", "markdown")
	}
	if options.CorpusDir == "" {
		options.CorpusDir = filepath.Join(options.RootDir, "datasets", "corpus")
	}
	if options.Link == "" {
		options.Link = LinkSymlink
	}
	if options.Link != LinkSymlink && options.Link != LinkHardlink {
		return fmt.Errorf("corpus: unknown link mode %q", options.Link)
	}

	byModel := map[string][]Item{}
	basenames := map[string]map[string]string{}
	for _, item := range items {
		name := archiveName(item.Key)
		if name == "" {
			return fmt.Errorf("corpus: malformed manifest key %q", item.Key)
		}
		model := item.Dataset.Model
		if model == "" {
			return fmt.Errorf("corpus: %s: missing model", item.Key)
		}
		if basenames[model] == nil {
			basenames[model] = map[string]string{}
		}
		if existing, ok := basenames[model][name]; ok && existing != item.Key {
			return fmt.Errorf("corpus: model %s: %s and %s share basename %q", model, existing, item.Key, name)
		}
		basenames[model][name] = item.Key
		byModel[model] = append(byModel[model], item)
	}

	models := make([]string, 0, len(byModel))
	for model := range byModel {
		models = append(models, model)
	}
	sort.Strings(models)

	for _, model := range models {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := corpusModel(ctx, model, byModel[model], options); err != nil {
			return err
		}
	}
	return nil
}

func corpusModel(ctx context.Context, model string, items []Item, options CorpusOptions) error {
	modelDir := filepath.Join(options.CorpusDir, model)
	if options.Force && !options.DryRun {
		if err := os.RemoveAll(modelDir); err != nil {
			return fmt.Errorf("corpus: %s: %w", model, err)
		}
	}
	if !options.DryRun {
		if err := os.MkdirAll(modelDir, 0o755); err != nil {
			return fmt.Errorf("corpus: %s: %w", model, err)
		}
	}

	linked := 0
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := archiveName(item.Key)
		source := filepath.Join(options.MarkdownDir, name)
		if !outputPresent(source) {
			emit(options.Logf, "corpus: warn: %s: %s not extracted, skipping", model, item.Key)
			continue
		}
		files, err := collectMarkdownFiles(source)
		if err != nil {
			return fmt.Errorf("corpus: %s: %s: %w", model, item.Key, err)
		}
		if len(files) == 0 {
			emit(options.Logf, "corpus: warn: %s: %s has no markdown, skipping", model, item.Key)
			continue
		}
		if options.DryRun {
			emit(options.Logf, "would link %d file(s) from %s into %s", len(files), item.Key, filepath.Join(modelDir, name))
			linked++
			continue
		}

		for _, file := range files {
			relative, err := filepath.Rel(source, file)
			if err != nil {
				return fmt.Errorf("corpus: %s: %w", model, err)
			}
			destination := filepath.Join(modelDir, name, relative)
			if err := linkFile(file, destination, options.Link); err != nil {
				return fmt.Errorf("corpus: %s: %w", model, err)
			}
		}
		emit(options.Logf, "corpus: linked %d file(s) from %s into %s", len(files), item.Key, filepath.Join(modelDir, name))
		linked++
	}

	if linked == 0 && !options.DryRun {
		// Nothing usable for this model; drop the empty directory so training
		// cannot silently run on zero documents.
		os.Remove(modelDir)
	}
	return nil
}

// linkFile links source at destination. It is idempotent: an existing link that
// already resolves to source is left untouched, and anything else is replaced.
// Symlink targets are stored relative to the destination so the corpus tree
// stays relocatable.
func linkFile(source, destination string, mode LinkMode) error {
	absSource, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	absDir, err := filepath.Abs(filepath.Dir(destination))
	if err != nil {
		return err
	}
	target, err := filepath.Rel(absDir, absSource)
	if err != nil {
		return err
	}

	if info, err := os.Lstat(destination); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			if existing, err := os.Readlink(destination); err == nil && existing == target {
				return nil
			}
		} else if srcInfo, err := os.Stat(absSource); err == nil && os.SameFile(srcInfo, info) {
			return nil
		}
		if err := os.Remove(destination); err != nil {
			return err
		}
	}

	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	if mode == LinkHardlink {
		return os.Link(absSource, destination)
	}
	return os.Symlink(target, destination)
}

// collectMarkdownFiles recursively gathers .md files below root, following
// symlinked directories and files. Visited real directories are tracked so a
// symlink cycle terminates. The result is sorted for determinism.
func collectMarkdownFiles(root string) ([]string, error) {
	var files []string
	visited := map[string]bool{}

	var walk func(dir string) error
	walk = func(dir string) error {
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			if visited[real] {
				return nil
			}
			visited[real] = true
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			path := filepath.Join(dir, entry.Name())
			info, err := os.Stat(path) // follows symlinks
			if err != nil {
				continue // broken link
			}
			if info.IsDir() {
				if err := walk(path); err != nil {
					return err
				}
				continue
			}
			if strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
				files = append(files, path)
			}
		}
		return nil
	}

	if err := walk(root); err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

// archiveName returns the ZIM basename without its extension, which is also the
// directory zim2md writes that archive's Markdown into. It returns "" for a
// malformed manifest key.
func archiveName(key string) string {
	return strings.TrimSuffix(types.KeyFilename(key), ".zim")
}
