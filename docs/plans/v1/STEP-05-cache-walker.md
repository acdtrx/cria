# Step 5 — cache walker

**Phase 2 · Status: not started**

## Intent

`internal/hubcache` (read side): walk the hub cache into the CACHE.md view
model — repos with kind tags, GGUF quants as items, true blob-deduped sizes,
partial downloads — plus the "is this entry's model fully cached" answer that
serve (downloading phase) and the TUI (cached dots) both need.

## Files likely touched

`internal/hubcache/` (walker, size accounting, tests + fixture-tree builder).

## Decisions made during planning

- Kind heuristic: any `.gguf` in the snapshot → `gguf`; else safetensors with a
  `config.json` carrying mlx-lm's `quantization` key → `mlx`; else `other`.
  If the mlx marker proves unreliable on real repos, fall back to repo-org
  naming and record the change in CACHE.md.
- GGUF quant grouping: shard suffix `-NNNNN-of-NNNNN` folds into one item; the
  quant label is the filename token matching the known quant patterns
  (`Q*`, `IQ*`, `F16`, `BF16`, …); a file with no recognizable label is its own
  single-file item named by basename.
- Sizes from blob stat, each blob counted once (CACHE.md); `.incomplete` files
  reported separately as reclaimable.
- Fixtures are built by a test helper that constructs real snapshot/blob/symlink
  trees in a temp dir — shared blobs, shards, partials, single-file repos all
  covered.

## Acceptance criteria

- Table tests: multi-quant GGUF repo, sharded quant, blob shared across two
  snapshots (counted once), MLX repo, "other" repo, partial download, empty
  cache.
- Walker total equals `du`-style byte count of the fixture tree in a test.
- Suite result recorded here.
