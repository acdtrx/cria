# STEP 4 — degradations, refusals, and the override

Status: not started

## Intent

Every path that is not the happy swap says exactly what it can and cannot
know, and the one override exists: the busy gate can be skipped deliberately.

## Files likely touched

- `internal/cli/validate.go`, `validate_test.go`
- `internal/cli/help.go`

## Decisions made during planning

- Busy holder → exit 2: the reason line tells the agent to have the user
  clear or stop the server — validate never queues, never waits for idle
  (a wait would burn the agent's clock invisibly; refusal is honest).
- Unverifiable busy signal (mlx holder; llama without `/slots`) → **warning +
  proceed** with the swap: the warning names the false-negative risk (the
  holder may be mid-generation and its client will see the connection die).
  Proceeding, not refusing, is deliberate: the machine that needs validate
  most runs one server on one port, usually the agent's own, idle at the
  moment the tool call executes.
- `--ignore-busy` skips even a positive busy verdict — the operator's word
  that the generation may die. Exact flag name settled at implementation
  against existing flag idioms in the CLI (there are few; `--wait` is the
  precedent for spelling).
- Foreign port holder → exit 2 with pid + command line (start's refusal
  wording, shared where practical).
- Unknown entry / choice / option → exit 2, reusing start's refusal text
  (which already names the valid ids/choices/options).
- Restore-failure reporting (exit 3) names: what failed, and what is serving
  now (nothing, or the target left running when its stop failed) — the state
  a human must fix, in one line.
- **The agent-facing surfaces teach validate as THE verb** (user-requested
  2026-08-25): `internal/config/docs.go`'s footer and the embedded
  `internal/config/agents.md` ("Validate what you wrote") currently teach the
  start-wait/status/stop choreography — the exact dance that burns the
  calling agent's own model. Both switch to `cria validate <id>
  [choice=option ...]` with the exit-code contract, keeping start/stop
  documented as the manual verbs they remain. The user's existing
  `~/.config/cria/AGENTS.md` is only written when missing — the manual
  refresh (delete it, run cria once, or copy the new text) is stated in the
  step-5 live-proof checklist, per feature-building mode.

## Acceptance criteria

- Tests per refusal/degradation path asserting exit code, the reason line,
  and that refused paths touched nothing.
- The unverifiable-signal warning appears for an mlx holder and for a llama
  holder whose `/slots` probe fails, and the swap proceeds.
- `--ignore-busy` (or its settled spelling) proceeds past a busy verdict.
- Phase 2 ends here: full suite green, committed.
