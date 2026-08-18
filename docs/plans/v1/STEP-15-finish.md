# Step 15 — end-to-end validation and finish

**Phase 6 · Status: not started**

## Intent

Prove v1 as a whole on the dev Mac, write `docs/ARCHITECTURE.md`, align README,
tag.

## Files likely touched

`docs/ARCHITECTURE.md` (new), `README.md`, this file (results).

## Decisions made during planning

- The e2e checklist is the OVERVIEW's verification list, run in one sitting and
  transcribed here: both backends served, the one-port swap workflow
  (stop-then-start, agent endpoint unchanged), `status --json` consumed by a
  scripted check, real deletion vs `du`, foreign drill, full TUI tour, first-run
  scaffold on a clean home, `cria docs` → agent-written entry → `start --wait`
  loop. Added from step 10: two small models resident at once on different
  ports, exercising the several-servers no-arg `cria stop` refusal on real
  records.
- `ARCHITECTURE.md` documents the package boundaries and data flow as built
  (with the diagram CLAUDE.md asks for) — written now, at the end, when the
  shape is real.
- Lint/vet/test/cross-compile gates from the OVERVIEW all green.
- Merge: branch rebases onto main, ff merge, `git worktree prune`; annotated
  tag `0.1.0` on main.

## Acceptance criteria

- Checklist transcribed here with outcomes; all gates green; plan marked done.
