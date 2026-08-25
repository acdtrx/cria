# STEP 3 — the command: parse, sequence, exit

Status: not started

## Intent

`cria validate <entry-id> [choice=option ...]` in `internal/cli`: the full
blocking sequence over the step-1/2 mechanisms, with the four exit codes and
stage lines an agent reads.

Sequence: resolve entry+picks → displacement resolution → busy gate →
hold+stop holder (if any) → start target → await green (the start `--wait`
machinery: health primary, listener attribution corroborates, lazy-load
warm-wait included) → prove → stop target → restore holder (if any) →
verdict.

## Files likely touched

- `internal/cli/validate.go` (new), `validate_test.go` (new)
- `internal/cli/cli.go` — command registration and dispatch.
- `internal/cli/start.go` — extract the await-green path for both callers
  (start keeps its output; validate reuses the mechanism, CODING-RULES §2).
- `internal/cli/help.go` — the verb in help output.

## Decisions made during planning

- Picks parse with `splitPicks` — validate's `[choice=option ...]` is start's
  syntax, deliberately.
- Exit codes (OVERVIEW): 0 validated · 1 target failed (restored) · 2 refused
  before touching anything · 3 restore failed. Every non-zero exit prints one
  concise reason as its last line — the agent contract.
- **Restore is unconditional**: target start failed, await red, prove failed,
  or the operator interrupted — the restore path still runs. SIGINT is
  caught for the duration of the protocol; on interrupt the current stage is
  abandoned, restore runs best-effort, and the exit reports what state the
  machine was left in.
- Stage lines are short, one per transition (`stopping qwen38-27 (holding its
  record)`, `starting lfm25-26b-q8…`, `proving…`, `restoring qwen38-27…`),
  stdout; the verdict line is last. No spinners, no JSON.
- An already-green target on its own port is still displaced and restarted —
  the same protocol; the point may be validating a new combination (settled
  2026-08-23).

## Acceptance criteria

- CLI-level tests with fake manager/tree: the happy swap, self-validation,
  no-holder validation, target-fails → exit 1 with restore having run,
  restore-fails → exit 3 naming what serves now, refusals → exit 2 with
  nothing touched (fake records assert no stop happened).
- Interrupt path tested: SIGINT mid-await → restore ran, exit non-zero,
  honest last line.
- Suite state at step end recorded here.
