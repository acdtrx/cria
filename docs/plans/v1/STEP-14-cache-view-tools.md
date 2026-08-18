# Step 14 — cache view and tools pane

**Phase 5 · Status: done (2026-08-18) — phase 5 ends green.** Suite green
(92 tui test functions), all gates pass; verified live on the real cache:
header total matches `du -sk` exactly, nested gguf tree with verbatim tags
(UD- quants, mmproj-BF16.gguf), the 869 MiB real partial row, details with
used-by join and serving-now, delete drill on a purpose-downloaded repo
(confirm modal rendering the plan; reclaimed exactly; cache byte-identical
after), served-guard refusals for quant and repo targets from the live
server, global stop working from the cache view, used-by warning in the
confirm, tools pane rendering the real report. Decisions: one walk feeds
both views (model holds *hubcache.Cache; presence derives from it); walk
rule = a list is visible (overlays don't walk) or a download runs; two
cursors, x cache-only, ⏎ serve-only; used-by joins via MatchQuant while
serving-now mirrors the deletion guard so pane and refusal cannot disagree;
ServedError rendered verbatim; confirm counts "paths" not "files" (blobs +
symlinks); tools report fetched fresh on open (deliberate user action).

## Intent

The cache screen per CACHE.md and the mockups: grouped list with kind tags and
sizes, cache total, details pane with used-by cross-reference and serving
guard, the delete-confirm flow rendering the surgery plan, partials row — plus
the tools pane toggled by the global keybind.

## Files likely touched

`internal/tui/` (cache view, delete modal, tools pane).

## Decisions made during planning

- Delete confirm renders the step 11 plan verbatim (bytes, files, shared blobs
  left behind, used-by); after execution the reported reclaim shows in the view.
- Used-by comes from matching cache items against loaded config entries — a pure
  join done in the view model.
- The tools pane renders the step 4 report; toggled from any view (TUI.md).

## Acceptance criteria

- Manual pass on the dev Mac recorded here: totals match `du`, quant-level
  selection, delete an unreferenced quant end to end, serving-guard refusal on
  the running model, partial cleanup, tools pane toggle from both views.
- **Phase 5 ends here**: suite green, committed. Result recorded here.
