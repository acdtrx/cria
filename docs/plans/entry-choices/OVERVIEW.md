# Entry choices — plan

`[[choice]]` axes inside an entry file: named pick-one sets of passthrough args
(plus `quant`/`repo` overrides), so one model's quants, contexts, layouts and
toggles stop being eleven files. Design settled 2026-08-22 with the user; the
contracts live in `docs/specs/CONFIG.md` (Choices), `docs/specs/SERVE.md`
(record + start + status), `docs/specs/CLI.md` (start picks, list) and
`docs/specs/TUI.md` (the choice picker) — this plan is the build order.

## Goal

An entry declares choices; cria composes the picked options into the launch
without interpreting them. Picks persist per entry in
`~/.local/state/cria/choices.json`, written only by the TUI picker;
`cria start <id> choice=option` is a one-shot override. The record carries the
picks a server was composed from; status (human, `--json`, TUI box) names the
combination; the TUI picker is a modal — one row per choice, ↑/↓ between rows,
←/→ along one, every change written at the keypress. A flat entry behaves
exactly as today.

## Scope

- `internal/config` — `[[choice]]`/`[[choice.option]]` schema, validation,
  collision rules, resolution (picks → effective repo/quant/args), `cria docs`
  by construction.
- `internal/picks` (new) — the `choices.json` store.
- `internal/serve` — start takes a selection, composes with it, records it;
  status carries it.
- `internal/cli` — `start` pick arguments, `list` choices, `status` combo.
- `internal/tui` — detail pane picks + composed command, picker modal,
  restart-last replaying the record's picks.
- Specs — already updated (settled with the design, 2026-08-22).

## Out of scope

- Rewriting the user's 31 entry files — a config-tree activity for user + agent
  after the merge; cria never edits the tree.
- Combo validity (which quant fits which context) — deliberately not cria's
  business; coupling is expressed by factoring flags into the same axis.
- Multi-pick or optional axes — a toggle is a two-option choice; an always-on
  bundle is a one-option choice. One mechanism.
- Mixing backends in one file — `backend` stays per-file; a model family is a
  GGUF file and an MLX file.
- Any write to the config tree (hard rail).

## Constraints

- Args stay passthrough: cria never interprets an option's flags; the only typed
  things an option may set are `quant` (llama only) and `repo`.
- Strict decoding everywhere: unknown keys and wrong types are errors, in the
  entry file and in `choices.json` alike; `choices.json` degrades loudly to
  config defaults when broken (it is cria's own state), and prunes picks whose
  choice/option/entry is gone on the next write.
- `cria docs`, the schema and the `cria new` scaffold render from the same
  definitions the parser uses — a schema change updates all three in one edit
  by construction.
- No new dependencies.

## Risks

- The `servers` interface in `internal/cli/cli.go` and `serve.Manager.Start`
  change signature (selection threading) — mechanical but wide; step 3 owns it
  and keeps every existing test's meaning.
- Collision checking must not enumerate combinations: pairwise over (base args ×
  options) and (options of different choices) is complete and cheap — step 1
  states the rule once and tests it.
- The record gains a field; older records lack it — a missing selection reads as
  "flat entry", never an error (records are transient; no migration).
- The picker modal joins the precedence chains (`press()`, `screen()`,
  `syncEscScope()`) — same care entry-groups' modes needed.

## Phases and steps

- **Phase 1 — config: schema and resolution** (no visible change; suite green
  at end)
  - Step 1: `[[choice]]` schema — parsing, validation, collisions, docs/scaffold.
  - Step 2: resolution — (entry, selection) → effective repo/quant/args.
- **Phase 2 — lifecycle: selection through serve** (suite green at end)
  - Step 3: serve carries a selection — start signature, composition, record,
    status.
  - Step 4: the picks store — `internal/picks`, `choices.json`.
- **Phase 3 — CLI** (suite green at end)
  - Step 5: `cria start <id> choice=option`, `cria list` choices,
    `cria status` combo.
- **Phase 4 — TUI** (suite green at end)
  - Step 6: detail pane picks + composed command; start with stored picks;
    restart-last replays the record.
  - Step 7: the picker modal.

Implementation per `CLAUDE.md`: branch + worktree under `.claude/worktrees/`,
an anchor tag on main immediately before step 1 begins, each step implemented
by an Opus subagent and reviewed against its acceptance criteria.

## End-to-end verification

After the merge, live with the user — **no running server is touched without
their go-ahead** (their q8 daily driver holds the shared port):

1. Rewrite one real family (qwen38-27) into a choices entry with an agent;
   `cria list` shows its axes and defaults; `cria docs` documents the schema.
2. TUI: picker opens on it, ↑/↓/←/→ pick, the detail pane's command line
   follows every keypress; picks survive a cria restart.
3. `cria start qwen38-27 quant=q4 --wait` on a free port (user-picked moment):
   status human + `--json` + TUI box name the combo; a second start
   `quant=q6` refuses on the busy port as before.
4. Bare `cria start qwen38-27` launches the stored picks, proving the one-shot
   pick from (3) persisted nothing.
5. Restart-last replays the record's picks even after the picker's defaults
   moved elsewhere.
6. Break `choices.json` by hand — loud note, config defaults used, next picker
   change rewrites it; remove an option from the TOML that a pick names — the
   pick is skipped and pruned on the next write.
