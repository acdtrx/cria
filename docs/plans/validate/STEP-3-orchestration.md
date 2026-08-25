# STEP 3 — the command: parse, sequence, exit

Status: done (2026-08-25)

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

## What was built

`internal/cli/validate.go` — the command, in three parts:

- `validate(args)` — parse (`splitPicks`, no flags yet) and every refusal that
  happens before the machine changes: the tree, the entry (`unknownEntry`), the
  picks (`launchPicks`), `config.Resolve`, the state directory, the backend's
  tool, `Displaced`, then `displacementRefusal`. All of them exit 2 —
  `exitRefused = exitUsage`, since both mean cria did nothing to the host.
- `displacementRefusal` — foreign holders (`foreignRefusal`, start's own text),
  a target already running on a port other than the one it now declares (it
  would orphan the first process), and `busyGate`.
- `busyGate(manager, holder) (refusal, warning)` — the one small function step 4
  extends with `--ignore-busy`: `BusyGenerating` → refusal, `BusyUnverifiable` →
  warning on stderr and proceed, `BusyIdle` → silence.
- `swap(...)` — the protocol from the first thing it changes: arm the interrupt
  watch, `Displace` the holder (the record copied out of the displacement),
  `runTarget`, stop the target, `Restore`, verdict. Restore is unconditional; the
  one case it is skipped is a target whose stop failed, because its port is still
  taken — reported as exit 3 with `unrestorable` naming what serves now and the
  two commands that undo it.
- `runTarget(...) (*serve.Record, error)` — start, `awaitGreen`, `Prove`. A nil
  record means the start never happened, so there is nothing to stop.
- `watchInterrupt()` — `signal.Notify(os.Interrupt)` with a latched check and a
  disarm, read at the stage boundaries and between the wait's observations. An
  interrupt abandons the stages, not the process: the restore still runs.

Shared with start rather than duplicated (CODING-RULES §2): `awaitGreen`
(start.go) is the whole wait — phase, listener attribution, the lazy backend's
warm, both budgets — returning `(status, waited, error)`; `await` is now four
lines over it and start's output is byte-identical. `launchPicks`,
`unknownEntry` and `foreignRefusal` came out of start.go the same way, each so
the two commands can print the same text at the exit code each owes its caller.
`app.failWith(code, …)` prints one refusal line at any code; `fail` and `usage`
are one line each over it.

Decisions made while implementing:

- **Exit 3 for a displace that failed.** `serve.Stop` can fail after SIGTERM
  landed, so "nothing was touched" would be a lie; the line says nothing was
  validated and the holder may no longer be serving.
- **Exit 1 for an interrupt** whose restore succeeded — the machine is as found
  and nothing was proved, which is what the reason line says. Four codes stay
  four (no fifth for "interrupted").
- **An interrupt is noticed between stages, never inside one**: a spawn or a
  completion already in flight runs to its own budget. Cancelling those means
  threading a context through serve's HTTP calls — out of this step's scope.
- Stage lines and the verdict on stdout, warnings and reasons on stderr, the
  reason always last.

## Suite

`go test -count=1 ./...` from the worktree root: **all packages ok** (cria,
cli, config, format, hubapi, hubcache, picks, procs, selfupdate, serve, tools,
tui), no expected reds. `gofmt -l .` empty, `go vet ./...` clean. Ten tests in
`internal/cli/validate_test.go` (the happy swap with its stage-line order ·
free port · self-validation replaying the record's own picks · three
target-failure stages → exit 1 with the restore having run · failed restore →
exit 3 · target that would not stop → exit 3, restore not attempted · holder
that would not stop → exit 3 · six refusals → exit 2 touching nothing ·
unverifiable holder → warning and proceed · Ctrl-C mid-wait → restore ran,
exit 1, watch disarmed), plus three routing rows in `cli_test.go`. Four
mutations were checked and each failed its test: the holder going back under
the target's picks, a restore conditional on the verdict, and a busy gate that
does not refuse.

Left for step 4 (as planned): the `--ignore-busy` override in `busyGate`, the
wording of the busy/unverifiable/foreign refusals (placeholders that read
correctly but were not tuned), the help page's EXIT CODES block and agent
section, and `docs/specs/CLI.md`'s surface list — validate is not in it yet,
since phase 3 owns the contract docs.
