# gonano-school

Dataset toolchain for training small, domain-specialized
[gonano](https://github.com/cookiengineer/gonano) models from
[Kiwix](https://wiki.kiwix.org/wiki/Main_Page) ZIM archives.

The repository prepares a broad "school education" base corpus (`gonano-base`)
and one additional corpus per domain category. Domain models are continued
in their training from the base checkpoint, so a gonano model name maps
directly to the datasets it was trained on.

```
kiwix online catalog -> updater.go -> updates manifest.json

manifest.json -> downloader.go -> downloads datasets/*.zim

# this will create symbolic links from the 1 corpus model back to the n markdown datasets
datasets/{name}.zim -> zim2md -> exports dataset/markdown/${name} and datasets/corpus/$model}
```

## Status

| Phase | Contents                                      | State                |
|-------|-----------------------------------------------|----------------------|
|   1   | manifest, Kiwix catalog scrape, downloader    | implemented + tested |
|   2   | ZIM to Markdown (`zim2md`), per-model corpora | planned              |
|   3   | training orchestration, `--init-model`        | planned              |

## Building and Testing

```bash
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
go run ./cmd/downloader -model gonano-physics;;

# by category or by Kiwix site
go run ./cmd/downloader -category cybersecurity;
go run ./cmd/downloader -site gutenberg;

# preview without writing
go run ./cmd/downloader -dry-run;
```

Filters combine as a union and repeated flags are allowed. Downloads are
resumable (`.part` + rename) and it skips files whose size already matches.

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

