# STEP 5 — the contract in SERVE.md and the live proof

Status: not started

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
