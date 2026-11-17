# Delete through huggingface_hub's shared blob store

Since hf 2.x, `hf download` keeps Xet files in a cache-wide store
(`<cache>/blobs/<xx>/<hash>`) and links each repo's `blobs/<etag>` to it. A cria
delete of a stored quant or repo removes the links and frees none of those bytes.
This feature makes such a delete free the space, through `hf`, and say the truth
when `hf` is not there.

**Specs:** `docs/specs/CACHE.md` (Deletion: the shared store is hf's to free),
`docs/specs/TOOLS.md` (`hf`).

## Milestones

### 1. An honest plan, and the fallback without `hf`

A delete plan counts a stored file as reclaimed by hf's rule, and with no `hf` the
confirmation says what stays in the store. cria still executes every delete itself.

Acceptance:
- Fixture tests (shared store built as hf 2.4 lays it out): a stored quant no other
  repo links reports its stored bytes as staying in the store and its repo-local
  bytes as reclaimed; a stored file another repo links is kept and the plan names
  that repo's link; a `.refs` line whose link is gone does not keep a file; a
  missing or unreadable manifest never counts a file as freed.
- A TUI test: the confirmation for a stored delete with `hf` not found renders
  *"X GB stays in hf's shared store; `hf cache prune` frees it (it also removes old
  revisions and unfinished downloads)."* with X the bytes this delete leaves
  unreferenced.
- A delete that touches no stored file renders and reclaims exactly as before.

### 2. Through `hf` when it is found

With `hf` resolved (`[tools] hf` or `PATH`), a delete that touches stored files runs
`hf cache rm`; everything else is unchanged.

Acceptance:
- Tests with an injected fake runner: the argv is exactly
  `rm --cache-dir <root> --yes` plus the plan's `hf://` URIs — one per file path for
  a quant, the repo URI for a repo or a repo's last quant; the fake performs hf's
  removal, and the reported bytes equal what the tree lost.
- A fake that exits non-zero: the error carries its stderr, and cria re-reads and
  reports what was freed.
- Superseded and partial deletes never run `hf`; the confirmation of a delete that
  does says it runs through `hf`.
- Live on the dev Mac, no servers involved: into a temporary `HF_HUB_CACHE`,
  `hf download` a small GGUF repo with two quants, delete one in cria's cache view —
  the cache's `du` drops by that quant's size, and `hf cache ls` reports no
  unreferenced shared blobs.
