# Step 14 — cache view and tools pane

**Phase 5 · Status: not started**

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
