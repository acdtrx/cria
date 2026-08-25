# STEP 5 — the contract in SERVE.md and the live proof

Status: docs half done (2026-08-25) — the live proof is pending the user's go

## Intent

The feature is documented where its topic lives, the backlog entry retires,
and the protocol is proven on real metal without touching the main server.

## Files likely touched

- `docs/specs/SERVE.md` — a `## Validate` section: the protocol, port
  scoping, the busy gate and its degradation, the exit codes, restore's
  unconditionality. Settled dates carried from the backlog entry.
- `docs/BACKLOG.md` — the validate entry removed (resolved entries are
  removed in the same change; git is the archive).
- `docs/plans/validate/*` — statuses closed out.

## Decisions made during planning

- The live e2e is run manually with the user present (serving-machine care):
  1. `qwen38-27` keeps serving on 11434 throughout — its pid and record are
     noted before and verified identical after, and a completion against
     11434 answers afterwards.
  2. `cria start lfm25-26b-q8` (port 11436), wait green.
  3. `cria validate lfm25-26b-q8` — self-validation: watch the stage lines
     displace, prove and restore only LFM; exit 0.
  4. `cria validate lfm25-26b-q8 nosuch=thing` → exit 2, nothing touched.
  5. A failure case for exit 1 if cheaply arrangeable (e.g. a scratch entry
     whose args refuse to start); otherwise exit 1 stays covered by the
     component tests and that is recorded here honestly.
  6. `cria stop lfm25-26b-q8` — the machine as the session found it.
  7. Refresh `~/.config/cria/AGENTS.md` to the new embedded text (cria writes
     it only when missing): move the old one aside and run any cria command
     once, or copy the text over — user's call, stated not encoded.
- Timing: the run needs the user's go — builds and the LFM model load share
  the machine with the live model session.

## Acceptance criteria

- SERVE.md section reads as contract, not implementation mirror.
- Backlog entry gone in the same commit the spec section lands.
- The live run's outcomes recorded in this file (commands, exits, the
  before/after pid check), reds-if-any named.
- Phase 3 = plan end: full suite green, branch rebased onto main, ff-merged,
  worktree pruned.

## What was written (docs half)

- `docs/specs/SERVE.md` — a `## Validate` section between `cria status` and
  Benchmarking: the five-stage protocol (displace keeping the record → start →
  await green → prove with one real completion → stop → restore from the held
  record's own picks), then the settled points as dated bullets — port-scoped
  displacement (2026-08-25) with the three rejections, self-validation as the
  same path (2026-08-23), the busy gate on is-processing and never open
  connections (2026-08-23, `/slots` verified live 2026-08-25), unverifiable as a
  third answer, never queueing or waiting for idle, `--ignore-busy` lifting that
  one gate with the refusal deliberately not naming it (2026-08-25), restore
  unconditional including on interrupt with the will-not-stop carve-out — and
  the four exit codes with what each says about the machine. `no --json`
  (2026-08-23) closes the section.
- `docs/specs/CLI.md` — `cria validate <id> [choice=option ...]
  [--ignore-busy]` in the v1 surface list after `status`, pointing at SERVE.md
  for the protocol; the flags line gains `--ignore-busy` and drops its
  now-false "nothing else speaks machine or takes options"; the Rules block
  gains validate's extension of the exit-code rule (1 adds "machine as found",
  2 = refused-touched-nothing = usage, 3 = machine left changed) and its
  `--json` line is corrected to status **and bench** with validate's
  no-document contract named.
- `docs/BACKLOG.md` — the `cria validate` entry under `## Serve` removed; its
  decisions now live in SERVE.md. The slots-stats entry that names the validate
  plan as its trigger is left as it stands.

Suite after the docs change: `go test -count=1 ./...` all packages ok,
`gofmt -l .` empty, `go vet ./...` clean — no test reads the spec files, so
nothing here could move; run for the record.

## What remains

The live e2e above, in full, with the user present (serving-machine care) —
including step 7, the `~/.config/cria/AGENTS.md` refresh, since cria writes that
file only when it is missing. Its outcomes get recorded in this file, and the
plan is closed only then.
