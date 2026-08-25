# cria validate — swap, prove, restore

One blocking command an agent can run against the machine it thinks on:

```
cria validate <entry-id> [choice=option ...]
```

Stop the server holding the target's port (keeping its record), start the
target, wait green, prove it with one real completion, stop it, restore the
displaced server from its record's own picks, and exit with the target's
verdict. The agent only infers before and after the command, so its branch
never burns.

Origin: `docs/BACKLOG.md`, Serve — user-designed 2026-08-23, settled points
carried here; port scoping settled 2026-08-25. The backlog entry is removed
when this plan lands.

## Goal

An agent that just wrote or changed a profile can prove it starts, loads and
answers — on the machine that runs its own model — without knowing anything
about what else is running, and with the machine left exactly as found.

## Settled decisions (from the backlog entry, dated there)

- **Port-scoped displacement** (2026-08-25): the one server validate may stop
  is the live-record holder of the target's resolved port. The target entry
  itself carries that knowledge — the agent needs none. Servers on other ports
  are structurally untouchable. On shared-port machines (the config doctrine:
  one `default_port`, swap by stop-one-start-another) the port holder *is* the
  running server. Rejected: refusing when several servers run; any flag naming
  a server to stop; validating on a second port with both resident (the
  machine that needs validate is the one without memory for two).
- **Self-validation is the same path** (2026-08-23): target == displaced is
  not special; restore replays the record's picks either way, one code path.
- **No `--json`** (2026-08-23): the agent contract is the exit code and a
  concise reason. Structured output earns its place only if validate ever
  grows measurements.
- **Busy gate is is-processing, never open-connections** (2026-08-23): the
  validating agent's own client holds an idle keep-alive socket, so counting
  connections refuses on the caller's ghost. llama: `GET /slots` per-slot
  `is_processing` (verified live 2026-08-25: answers 200 on the 0.0.0.0 bind
  with `--slot-save-path` alone). A server where the signal is absent (mlx
  documents no equivalent; a llama build/config where `/slots` is not
  enabled) gets the honest degradation — a warning naming what could not be
  checked — plus an override flag to skip the gate entirely.
- **Restore is unconditional**: the displaced server is restarted even when
  validation fails; a failed restore is its own loud error naming what is
  serving now.
- **Prove = health green + one real completion**: `/health` passes while the
  first request can still die (the q8 speculative-batch death, the mini's
  Metal limit). The completion is the warm's shape — one small real request
  with the record's own model reference, never an empty prompt.

## Decisions made in planning (for review at plan review)

- **Exit codes**: `0` validated (and machine as found) · `1` target failed
  validation (machine as found) · `2` validate refused to run (busy gate,
  foreign port holder, unknown entry/picks — nothing was touched) · `3`
  restore failed (machine NOT as found; the message names what is serving
  now). Four outcomes an agent genuinely branches on; still not JSON.
- **The one-real-completion prove applies to every backend**, llama included —
  this is fit-proofing, not lazy-load warming; it reuses the warm's request
  sender, not its backend gate.
- **Foreign port holder → refuse** (exit 2) with pid and command line, exactly
  as start refuses: validate cannot restore what has no record.
- **Nothing held the port → nothing to displace, nothing to restore**: start,
  prove, stop, exit. "Left as found" includes leaving nothing running.

## Scope

- `internal/serve`: displacement resolution (port → live record), busy-gate
  probe, stop-keeping-record + restore-from-held-record, the prove request.
- `internal/cli`: the `validate` command — parse, orchestrate, report, exit.
- `docs/specs/SERVE.md`: a Validate section, written as the contract lands.
- Component tests throughout; one live e2e on the LFM entry (port 11436).

## Out of scope

- Measurements (tokens/s, memory) — validate answers "does it serve", nothing
  else. No `--json` until measurements exist, per the settled decision.
- The TUI: validate is a CLI/agent verb. The TUI shows its effects through the
  ordinary records it manipulates.
- mlx busy detection beyond the honest warning.

## Constraints & risks

- **Never parse logs** — every signal is an endpoint, the filesystem, or `ps`.
- The displaced record must be held in memory before its stop removes it from
  disk; restore needs the config tree (replay resolves the record's picks
  against it, `tui.replayOf`'s contract) — an entry deleted mid-validate makes
  restore fail loudly (exit 3), which is the honest outcome.
- Code sharing by purpose (§2): the CLI's await-green machinery
  (`cli/start.go, await`) serves start and validate for the same purpose —
  extract for both callers rather than duplicating; same for the warm sender.
- A validate interrupted (SIGINT) mid-protocol must still attempt restore —
  the restore path runs on the way out, best effort, and says what it did.

## Phases and steps

**Phase 1 — the protocol lives in serve** (steps 1–2). Goal: every mechanism
validate composes exists and is tested in `internal/serve`, no CLI yet.

- STEP-1 — displacement: resolve the target's port, find the live-record
  holder, the busy-gate probe (`/slots` is-processing; absent-signal warning),
  foreign-holder refusal.
- STEP-2 — swap mechanics: stop keeping the held record, restore from a held
  record via the tree (replay resolution moves/shares so CLI can use it), the
  prove request (warm's sender, any backend).

**Phase 2 — the command** (steps 3–4). Goal: `cria validate` end to end
against fakes, exit codes settled, output an agent can read.

- STEP-3 — orchestration: parse (`splitPicks`), the full sequence, unconditional
  restore including on interrupt, the four exit codes, concise stage lines.
- STEP-4 — degradations and refusals: mlx/no-slots warning + override flag,
  busy refusal, foreign port, unknown entry/choice/option, restore-failure
  reporting. Help text; `cria docs`/help surfaces stay consistent.

**Phase 3 — contract and live proof** (step 5). Goal: the feature is
documented where it lives and proven on real metal.

- STEP-5 — SERVE.md Validate section; remove the backlog entry; live e2e:
  LFM (port 11436) self-validation swap-prove-restore on the dev Mac while the
  main server keeps serving, plus a deliberate failure case (a broken pick) to
  see exit 1 with restore.

## End-to-end verification

- Full suite green at every phase end.
- The live run: with `qwen38-27` serving on 11434, start LFM on 11436, run
  `cria validate lfm25-26b-q8`, watch it displace/restore only LFM, verify the
  main server's pid and record never change and a completion against 11434
  still answers afterwards. Then one failing validate (bad pick) → exit 2, and
  one fit-failure simulation if arrangeable → exit 1 with restore.
