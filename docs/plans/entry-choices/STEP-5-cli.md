# Step 5 — CLI surfaces

Status: not started

## Intent

`cria start <id> [choice=option ...]` launches the merged selection one-shot;
`cria list` shows an entry's choices with their current picks; `cria status`
(human and `--json`) names the running combination.

## Files likely touched

- `internal/cli/start.go`, `list.go`, `status.go`, `cli.go` + tests.

## Decisions (from planning)

- Pick arguments: any non-flag argument containing `=` splits at the first `=`
  into choice and option; the id charset forbids `=`, so the parse is
  unambiguous. Several picks allowed; the same choice twice is a usage error.
  Empty choice or option (`=q4`, `quant=`) is a usage error.
- The merge is `picks.Merge(entry, stored, explicit)`: stored picks are *read*
  on the CLI path, never written — a bare `cria start x` after the TUI set
  q6 starts q6; `cria start x quant=q4` starts q4 and leaves the stored pick
  alone (settled 2026-08-22).
- Picks against a flat entry refuse ("has no choices"), through `Resolve`'s own
  message.
- `cria list`: under each choices entry, one muted line per choice —
  `quant: q4* q6 q8` style, the current pick marked — the agent-facing
  vocabulary for start's pick syntax. `--json` is status-only in v1; list stays
  human (unchanged rule).
- `cria status`: the combo joins the human block as one `picks` line
  (`quant=q6 layout=coding`), and `--json` gains the selection map. Pick order:
  the entry's choice order when the tree is readable, sorted as a fallback —
  records must render without the tree (self-contained rule).
- Help page: start's synopsis gains `[choice=option ...]`; `cria docs` already
  documents the schema (step 1).

## Acceptance criteria

- Component tests: start with explicit picks composes them (fake servers);
  bare start uses stored picks; one-shot proven (fake store asserts no write);
  duplicate/malformed/unknown picks refuse with their messages; list renders
  the axes with the current pick marked; status human + `--json` carry the
  selection; flat entries render exactly as today in all three.
- Phase 3 ends here: full suite green, gofmt silent, vet clean.
