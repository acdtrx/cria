# Step 2 — grouped-section computation

Status: done

## Intent

Pure logic that turns (config tree, prefs groups, active backend) into the
ordered sections the serve view will render — plus the pruning helper used at
save time. No rendering changes yet.

## Files likely touched

- `internal/tui/groups.go` (new)
- `internal/tui/groups_test.go` (new)

## Decisions (from planning)

- A section carries: the group name (empty for the ungrouped section), the
  member rows (reusing the existing `row` type from `serveview.go:57`), and
  whether its heading is visible.
- Section order: prefs group order, then ungrouped. Broken entries stay at the
  very bottom of the ungrouped section, both backends, as today
  (`serveview.go:76-85`).
- Membership: an entry is in the section of the group naming its id; entries
  named by no group are ungrouped. Within a section, entries keep tree order
  (alphabetical by id — incidental today, load-bearing now; the comparison is
  simply "tree order", no separate sort).
- Backend filter applies inside each section: only the active backend's
  members are listed, same predicate as today's `rows()`.
- Heading visibility: visible when the section shows ≥1 member in the active
  backend, **or** when the group is effectively empty — no stored id exists in
  the tree at all (covers both an emptied group and one holding only dangling
  ids). Hidden otherwise (group has real members, all in the other backend).
- Ungrouped heading: visible only when at least one group exists in prefs.
- Dangling ids (stored id with no tree entry): skipped when building sections.
- `pruneGroups(groups, tree) []entryGroup`: returns groups with dangling ids
  dropped; groups themselves are never auto-deleted (an empty group is a valid,
  visible thing). Called by later steps at every prefs write.
- The flat entry-row sequence (sections concatenated) is what `rows()` will
  return in step 3 — this step should expose it so selection indexes have one
  source of truth.

## Acceptance criteria

- Unit tests: ordering, backend filtering, visibility rules (incl. the
  other-backend-hidden and effectively-empty cases), dangling skip, pruning,
  broken-last, and the no-groups case producing exactly today's `rows()`
  sequence.
- Nothing else changes: `rows()` untouched this step, suite green (phase 1
  ends here — full suite must pass).

## Result

- `internal/tui/groups.go` adds the `section` type, `entrySections`, the flat
  `entryRows` (what `rows()` returns in step 3) and `pruneGroups`; nothing is
  wired — `rows()` is untouched. `internal/tui/groups_test.go` covers order,
  backend filtering, every visibility case, dangling skip, broken-last, the
  no-groups sequence against the model's own `rows()`, and pruning.
- `go test ./...` green, `gofmt -l .` silent, `go vet ./...` clean: phase 1 ends
  here. Both rules were mutation-checked — flipping the heading rule and pruning
  in place each turn a test red.
- Decided while implementing: `pruneGroups` against a nil tree prunes nothing —
  a tree cria has not read yet knows no id, so pruning against it would empty
  every group on the first write.
- Two edge cases settled with the user afterwards and folded in (both in
  `docs/specs/TUI.md`): a refused entry file counts as existing, so a typo never
  unfiles an entry — `treeIDs` covers `Entries` and `Broken`, and a group whose
  only members are refused files has members with nothing to show, so its
  heading hides rather than standing as an empty group's; and the ungrouped
  heading needs rows under it, since a heading over an empty tail separates
  nothing.
