# gonano-school

Dataset toolchain for training small, domain-specialized
[gonano](https://github.com/cookiengineer/gonano) models from
[Kiwix](https://wiki.kiwix.org/wiki/Main_Page) ZIM archives.

The repository prepares a broad "school education" base corpus (`gonano-base`)
and one additional corpus per domain category. Domain models are continued
in their training from the base checkpoint, so a gonano model name maps
directly to the datasets it was trained on.

```
kiwix online catalog -> updater    -> datasets/<category>.json
datasets/<cat>.json  -> downloader -> datasets/<category>/<archive>.zim
datasets/*/*.zim     -> extractor  -> datasets/<category>/markdown/<archive>/*.md
HuggingFace traces   -> reasoning  -> datasets/<category>/markdown/<slug>/*.md
                                   -> datasets/reasoning/<slug>.jsonl   (SFT)
datasets/<category>/ -> trainer    -> ~/.cache/gonano/domains/<cat>/.../*.gn
```

The reasoning step is a second data source on top of the ZIM pipeline: its
Markdown joins the `gonano-base` corpus (a reasoning *style* prior) while its
JSONL is consumed by gonano's `chat_sft`, which is the step that actually
teaches the `<|think_start|>` trace. See the gonano README section
"Reasoning traces (thinking)" for why the two steps are distinct.

## Status

| Phase | Contents                                      | State                |
|-------|-----------------------------------------------|----------------------|
|   1   | manifest, Kiwix catalog scrape, downloader    | implemented + tested |
|   2   | ZIM to Markdown (`zim2md`), per-category      | implemented + tested |
|   3   | training orchestration, `--init-model`        | implemented + tested |
|   4   | HuggingFace reasoning traces, JSONL for SFT   | implemented + tested |

## Building and Testing

```bash
# Install dependencies
go install github.com/cookiengineer/zim2md@latest;

go build ./...;
go vet ./...;
go test ./...;
```

## Categories

Datasets are tagged with the category whose model trains on them, which is also
the suffix of the gonano model name. The available categories are:

- Base model: `base`
- Educational branches: `science`, `chemistry`, `engineering`, `history`, `math`, `medicine`, `physics`
- Computer science branches: `programming`, `cybersecurity`, `cyberstrategy`

The `gonano-base` model trains from scratch on the `base` archives; every other
model is trained from the `gonano-base` checkpoint on its own category. Ingested
reasoning traces (`cmd/reasoning`) are tagged `base` (plus a `reasoning` filter
tag) and written into the `base` category directory alongside the base archives.

## Usage

### 1. Update Manifests

Scrapes the Kiwix OPDS catalog (English only, `nopic`/`maxi`/`mini` in that
order of preference) and rewrites one manifest per category,
`datasets/<category>.json`. Static files such as `datasets/reasoning.json` are
never touched:

```bash
go run ./cmd/updater;

# write somewhere else:
go run ./cmd/updater -datasets /data/gonano-school/datasets;

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
into `datasets/<category>/markdown/<archive>/`, right next to the archive. This
means `datasets/<category>/` is already the complete training input for that
category's model -- there is no separate corpus/link step. Already-extracted
archives are skipped unless `-force` is given; `-jobs` sets how many `zim2md`
processes run in parallel and `-workers` the per-process worker count.
`--assets` is never used, so only text pages become `.md` files.

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

### 4. Ingest Reasoning Traces

Downloads DeepSeek-R1 reasoning datasets from HuggingFace and flattens them
into the `gonano-base` corpus plus a JSONL conversation set for gonano's SFT
stage. DeepSeek never released its own R1 training traces, so the statically
required sources in `datasets/reasoning.json` are used:

| Key | Source | Licence | What it adds |
|:--|:--|:--|:--|
| `datasets/reasoning/magpie-reasoning-v2` | `Magpie-Align/Magpie-Reasoning-V2-250K` | Llama-3.3 | General reasoning traces |
| `datasets/reasoning/openr1-math` | `open-r1/OpenR1-Math-220k` | Apache-2.0 | Verified math reasoning |
| `datasets/reasoning/mixture-of-thoughts` | `open-r1/Mixture-of-Thoughts` | Apache-2.0 | 350k mixed math/code/science traces |

Three schemas are recognised:

- **Magpie** (`instruction` + flat `response`, trace in `<think>...</think>`).
- **OpenR1-Math** (flat `problem`/`answer` plus a nested `generations`
  `LIST<STRING>`, with `correctness_math_verify` `LIST<BOOL>`). The first
  verified generation is selected, so only correct traces are emitted.
- **Mixture-of-Thoughts** (nested `messages` `LIST<STRUCT<role, content>>`, flat
  `source`, flat `num_tokens`). The last user/assistant turns become the record;
  `-max-tokens` skips traces longer than a token budget.

The dependency-free parquet reader handles flat columns (BYTE_ARRAY, INT32/64,
BOOLEAN), single-level `LIST<STRING>`/`LIST<BOOL>`, and `LIST<STRUCT<...>>`
assembled from its sibling string columns. Deeper nesting is rejected rather
than silently mis-decoded.

```bash
# Ingest every statically required reasoning dataset (from the manifest).
go run ./cmd/reasoning;

# A small run per dataset (downloads only as many shards as needed).
go run ./cmd/reasoning -limit 20000;

# Skip traces longer than 4096 tokens.
go run ./cmd/reasoning -max-tokens 4096;

# Ingest a single dataset explicitly.
go run ./cmd/reasoning -repo open-r1/Mixture-of-Thoughts -config all -slug mixture-of-thoughts;

# Every JSONL file is then read together by gonano:
#   go run ./cmd/chat_sft --data-dir <root>/datasets/reasoning
```

Outputs:

- `datasets/<category>/markdown/<slug>/*.md` -- flattened traces, written into
  the consuming model's directory (`base` by default), so base pretraining sees
  the reasoning style.
- `datasets/reasoning/<slug>.jsonl` -- `{instruction, thinking, response,
  intent, ...}` rows for `go run ./cmd/chat_sft --data-dir <path>`.

The JSONL is where the trace is actually learned: gonano's `chat_sft` renders
`thinking` as a supervised `<|think_start|>...<|think_end|>` block. Pretraining
on the Markdown only teaches the style.

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
  -vocab-size 131072 -depth 20 -num-iterations 200;

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

Defaults are `-vocab-size 131072`, `-depth 20`, `-preset flash`,
`-num-iterations 200`; override them for the target host. `-base-dir` defaults to
`$GONANO_BASE_DIR` or `~/.cache/gonano`. The model size follows from `-depth`
and `-vocab-size`: depth 20 with a 131072 vocabulary targets roughly 5.3B total
/ 1.6B active parameters and needs a large-RAM host; lower `-depth` (and
`-vocab-size`) to fit a smaller machine. Note that `-vocab-size` only takes
effect when the tokenizer is trained, so pass `-force-tokenizer` to change it
after a tokenizer already exists.

## Manifests

Every dataset lives under `datasets/<category>/`, and each category has a
matching manifest file `datasets/<category>.json`, so a key and its path share
the same relative form:

```
datasets/base.json        ->  datasets/base/wikipedia_en_all_nopic_2026-07.zim
datasets/reasoning.json   ->  datasets/reasoning/magpie-reasoning-v2.jsonl
```

A manifest is a JSON object keyed by `datasets/<category>/<name>`:

```json
{
  "datasets/physics/wikipedia_en_physics_nopic_2026-07.zim": {
    "url": "https://lb.download.kiwix.org/zim/wikipedia/wikipedia_en_physics_nopic_2026-07.zim",
    "categories": ["physics"],
    "model": "gonano-physics",
    "site": "wikipedia",
    "size": 52690707456
  }
}
```

`toolchain.LoadManifests(dir)` merges every `*.json` in the directory, so
callers see one manifest regardless of the split. The `site` field preserves the
Kiwix source category for the `-site` filter, now that keys are category-based.

The updater writes exactly the catalog categories (`toolchain.Categories`) and
removes stale files among them; any category it does not manage (notably
`reasoning`) is left untouched. ZIM archives are ignored by git; the
`datasets/*.json` manifests are committed.

### Static reasoning manifest (`datasets/reasoning.json`)

Reasoning corpora are hand-maintained, so their file is committed and the
updater never writes it (`reasoning` is not a catalog category). Entries use
`"kind": "reasoning"` and carry the Hub coordinates (`repo`, `config`, `split`)
instead of a `.zim` URL. The downloader and extractor skip them (`OnlyZIM`),
because `cmd/reasoning` fetches and flattens them instead.

```json
{
  "datasets/reasoning/mixture-of-thoughts": {
    "categories": ["base", "reasoning"],
    "model": "gonano-base",
    "kind": "reasoning",
    "site": "reasoning",
    "repo": "open-r1/Mixture-of-Thoughts",
    "config": "all",
    "split": "train"
  }
}
```

The `reasoning` tag is a filter target (`-category reasoning`), but unlike the
other categories it is not a model suffix: these corpora feed `gonano-base`, so
their `model` stays `gonano-base`.

