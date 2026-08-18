# Step 11 — cache surgery

**Phase 4 · Status: done (2026-08-18) — phase 4 ends green.** Suite green, all
gates pass. Real-cache drill: a purpose-downloaded 88 MB repo planned,
executed and verified — reclaimed bytes equal the walk delta exactly and the
`du` delta within block rounding; the before/after walk diff is precisely the
deleted repo's three lines; the user's models and the pre-existing 911 MB
partial untouched. Decisions: refcounting re-scans snapshot entries (the
walk's blob-keyed Files would under-count aliased names); drift protection =
re-derive the plan at execute time and refuse on the first difference;
directories via os.Remove deepest-first, never RemoveAll; the serving guard
runs at Plan AND again inside Execute with execute-time state (TOCTOU closed);
partials are guarded per repo (a running fetch is indistinguishable from a
stale leftover); a quant-less llama server guards its whole repo; PlanRepo
works for GGUF repos too (same mechanism, same guard). Backlog: aliased blobs
display as one row in the view (deletion handles them; display is lossy).

## Intent

`internal/hubcache` (write side): the CACHE.md deletion contract — GGUF quant
deletion (symlinks + unshared blobs + shards), MLX/other repo deletion, partial
cleanup, reclaim measurement, and the serving guard.

## Files likely touched

`internal/hubcache/` (+ tests reusing the step 5 fixture builder).

## Decisions made during planning

- Deletion is a two-step API by construction: plan (what would be removed, bytes
  reclaimed) then execute (remove exactly the planned set, report actual bytes) —
  the confirm dialog renders the plan; nothing else computes it.
- Shared-blob rule: a blob is removed only when no remaining snapshot references
  it; the plan states shared blobs it is leaving behind.
- Deleting a repo's last quant removes the repo skeleton (snapshots, refs, empty
  dirs).
- The serving guard takes the running-entries list as an argument — surgery
  doesn't import serve; the caller (CLI-less in v1: the TUI) wires it
  (CODING-RULES §7, acyclic).

## Acceptance criteria

- Table tests: quant with exclusive blobs, quant sharing a blob with another
  snapshot, sharded quant, last-quant repo removal, MLX repo, partials-only
  cleanup; each asserts planned set == removed set == measured bytes.
- **Phase 4 ends here**: suite green, committed; a real small-quant deletion on
  the dev Mac verified against `du`, recorded here.
