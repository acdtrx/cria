# CACHE — visibility and surgery over the Hugging Face cache

The hub cache is the single source of truth for model bytes (`docs/cria.md`,
principle 2): cria displays it truthfully and mutates it only through deliberate,
explicit delete operations — never as a side effect. This spec owns the cache view
and the deletion contract. v1 is visibility and cleaning only; growth ideas wait in
`docs/BACKLOG.md`.

## The view (settled 2026-08-18)

- One list of everything in the hub cache: GGUF repos with their quants nested
  under them, MLX repos as single rows, and anything else the cache holds (other
  models, datasets) as plain repo rows — each tagged by kind. The header shows the
  total size of the whole cache; that number matches what `du` would report, so it
  can be trusted for "where did my disk go".
- The selectable unit mirrors serving identity: a GGUF **quant**, an MLX **repo**
  (quants are models on both sides of the app); "other" repos select whole.
- Partial downloads (`.incomplete` blobs) are surfaced as reclaimable, and
  deletable on their own.
- A details panel for the selected item shows what the filesystem already knows:
  snapshot revision, file list with sizes (shards summed), on-disk dates, which
  config entries reference it, and whether it is being served right now. Richer
  info (GGUF header metadata, Hub-API data) is backlog, not v1.

## Sizes

- Sizes are measured from blobs, each counted once: a sharded quant is the sum of
  its parts; a blob shared across snapshots is never double-counted. Item sizes may
  therefore not sum exactly to a repo or cache total — true bytes win over tidy
  arithmetic.

## Deletion (settled 2026-08-18)

- GGUF quant: remove its snapshot symlinks and every blob no other snapshot still
  references, sharded files included; deleting a repo's last quant removes the
  repo's remaining skeleton. MLX and "other" repos: remove the repo directory.
- Every delete confirms first, naming the bytes it will reclaim, and reports what
  was actually reclaimed after.
- The model currently being served cannot be deleted — the refusal names the
  running server; stop first. (The server maps the files; deleting under it frees
  no space and invites confusion.)
