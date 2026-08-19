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
- Partial downloads are surfaced as reclaimable, and deletable on their own.
  **Both suffixes count** (settled 2026-08-18, from the dev Mac's own cache):
  huggingface_hub leaves `.incomplete` blobs, llama-server — which fetches every
  GGUF cria serves — leaves `.downloadInProgress` ones. Reading only `.incomplete`
  would miss every partial the llama path produces, which is most of them.
- **Names are the provider's** (settled 2026-08-18): a repo, a file and a quant tag
  render exactly as Hugging Face spells them — unsloth's `UD-Q4_K_XL` keeps its
  prefix, a file cria reads no tag off shows under its own file name, extension
  included. cria never normalizes, strips or prettifies provider naming, and never
  matches it tolerantly: an entry's `quant` finds the item whose tag it spells and
  no other, so a tag written differently is honestly absent rather than quietly
  resolved to something near it. Case is the single exception, because llama.cpp's
  own `-hf repo:TAG` resolution is case-insensitive.
- **Superseded copies are visible and deletable** (settled 2026-08-19, from a
  live Unsloth re-upload): when the same file name resolves to different blobs
  in different snapshots, the copies not reachable from the current revision —
  `refs/main` when it resolves (llama-server maintains it), else the newest
  snapshot holding that name — are superseded, rendered as their own sub-rows
  under their item, and deletable as a unit. An item's bytes are its current
  copy; superseded bytes ride their own rows; the repo total still counts
  everything (du-honesty). Blobs shared with the current revision are never
  superseded, and a name only old snapshots hold is not superseded — cria
  never offers to delete the only copy of a quant. The serving guard exempts
  superseded copies: a running server maps the current inode, so unlinking the
  old blob returns the space at once (a server started before the re-upload
  still holds the old inode until it stops — the delete then frees nothing and
  breaks nothing).
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
