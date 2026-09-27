# gonano-school

Dataset toolchain for training small, domain-specialized
[gonano](https://github.com/cookiengineer/gonano) models from
[Kiwix](https://wiki.kiwix.org/wiki/Main_Page) ZIM archives.

The repository prepares a broad "school education" base corpus (`gonano-base`)
and one additional corpus per domain category. Domain models are continued
in their training from the base checkpoint, so a gonano model name maps
directly to the datasets it was trained on.

```
kiwix online catalog -> updater    -> manifest.json
manifest.json        -> downloader -> datasets/<site>/<archive>.zim
datasets/*.zim       -> extractor  -> datasets/markdown/<archive>/*.md
markdown             -> corpus     -> datasets/corpus/<model>/  (links)
corpus + manifest    -> trainer    -> ~/.cache/gonano/domains/<cat>/.../*.gn
```

## Status

| Phase | Contents                                      | State                |
|-------|-----------------------------------------------|----------------------|
|   1   | manifest, Kiwix catalog scrape, downloader    | implemented + tested |
|   2   | ZIM to Markdown (`zim2md`), per-model corpora | implemented + tested |
|   3   | training orchestration, `--init-model`        | implemented + tested |

## Building and Testing

```bash
# Install dependencies
go install github.com/cookiengineer/zim2md@latest;

go build ./...;
go vet ./...;
go test ./...;
```

## Categories

Datasets are tagged with exactly one category, which is also the suffix of the
gonano model that trains on them. The available categories are:

- Base model: `base`
- Educational branches: `science`, `chemistry`, `engineering`, `history`, `math`, `medicine`, `physics`
- Computer science branches: `programming`, `cybersecurity`, `cyberstrategy`

The `gonano-base` model trains from scratch on `base` archives; every other model
is trained from the `gonano-base` checkpoint on its own category.

## Usage

### 1. Update Manifest

Scrapes the Kiwix OPDS catalog (English only, `nopic`/`maxi`/`mini` in that
order of preference) and rewrites `manifest.json`:

```bash
go run ./cmd/updater -out manifest.json;

# tolerate categories that fail upstream:
go run ./cmd/updater -keep-going;
```
### 2. Download Datasets

```bash
# everything (respecting the storage budget of e.g. 6TB)
go run ./cmd/downloader -max-bytes $((6 * 1000 * 1000 * 1000 * 1000));

# one model's corpus
go run ./cmd/downloader -model gonano-physics;

# by category or by Kiwix site
go run ./cmd/downloader -category cybersecurity;
go run ./cmd/downloader -site gutenberg;

# preview without writing
go run ./cmd/downloader -dry-run;
```

Filters combine as a union and repeated flags are allowed. Downloads are
resumable (`.part` + rename) and it skips files whose size already matches.

### 3. Extract ZIM to Markdown

Converts the selected archives with [zim2md](https://github.com/cookiengineer/zim2md)
into `datasets/markdown/<archive>/`. Already-extracted archives are skipped
unless `-force` is given; `-jobs` sets how many `zim2md` processes run in
parallel and `-workers` the per-process worker count. `--assets` is never used,
so only text pages become `.md` files.

```bash
# everything (install zim2md first: go install github.com/cookiengineer/zim2md@latest)
go run ./cmd/extractor;

# one model, 8 parallel processes
go run ./cmd/extractor -model gonano-physics -jobs 8;

# re-extract everything, continue past failures
go run ./cmd/extractor -force -keep-going;

# preview
go run ./cmd/extractor -dry-run;
```

### 4. Assemble Model Corpora

Builds `datasets/corpus/<model>/` as real directories containing one link per
Markdown file, mirroring the per-archive layout. Each model only aggregates its
own category's archives; archives that have not been extracted are reported and
skipped. `-link` selects `symlink` (default, relocatable) or `hardlink`
(requires one filesystem).

```bash
# every model (symlinks)
go run ./cmd/corpus;

# one model, hardlinked
go run ./cmd/corpus -model gonano-physics -link hardlink;

# rebuild a model from scratch (prunes stale links)
go run ./cmd/corpus -model gonano-base -force;
```

The resulting `datasets/corpus/<model>/` is the `--data-dir` for the gonano
trainer.

### 5. Train Models

Orchestrates the gonano pipeline against a gonano source checkout
(`-gonano-dir`, run via `go run ./cmd/...`):

1. train the shared tokenizer once on the base corpus (`tok_train`),
2. train `gonano-base` from scratch into `domains/base/base_checkpoints/<tag>/`,
3. continue-pretrain every selected specialty model from that base checkpoint
   into `domains/<cat>/base_checkpoints/<tag>/` (`--init-model`).

Every command runs with `GOEXPERIMENT=simd`. `gonano-base` is always trained (or
reused) because each specialty continues from it; selecting only
`-model gonano-physics` still ensures base exists.

```bash
# everything: tokenizer + base + all selected domains
go run ./cmd/trainer -gonano-dir ~/Software/cookiengineer/gonano \
  -depth 8 -num-iterations 200;

# one specialty (base is trained first unless -reuse-base)
go run ./cmd/trainer -gonano-dir ~/Software/cookiengineer/gonano \
  -model gonano-physics;

# reuse an existing base checkpoint and tokenizer, only retrain physics
go run ./cmd/trainer -gonano-dir ~/Software/cookiengineer/gonano \
  -model gonano-physics -reuse-base;

# continue past failing domains; preview without running
go run ./cmd/trainer -gonano-dir ~/Software/cookiengineer/gonano -keep-going;
go run ./cmd/trainer -gonano-dir ~/Software/cookiengineer/gonano -dry-run;
```

Defaults are `-vocab-size 32768`, `-depth 8`, `-preset flash`, `-num-iterations
200`; override them for the target host. `-base-dir` defaults to
`$GONANO_BASE_DIR` or `~/.cache/gonano`.

## The `manifest.json`

A JSON object keyed by `datasets/<kiwixCategory>/<file>.zim`:

```json
{
  "datasets/wikipedia/wikipedia_en_physics_nopic_2026-07.zim": {
    "url": "https://lb.download.kiwix.org/zim/wikipedia/wikipedia_en_physics_nopic_2026-07.zim",
    "categories": ["physics"],
    "model": "gonano-physics",
    "size": 52690707456
  }
}
```

ZIM archives are ignored by git; only the `manifest.json` is committed.

