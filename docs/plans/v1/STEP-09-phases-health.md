# Step 9 — phases, health, download progress

**Phase 3 · Status: done (2026-08-18)** — suite green, all gates pass.
Decisions: the starting/unhealthy boundary is first-green memory — per entry
AND pid, per invocation, never persisted: a probe that has never been green
reads starting (503 during model load included); red after green is unhealthy;
a fresh cria that finds a red server calls it starting until it sees green
once. Green = 2xx. Cache walk is lazy-once per Snapshots call and skipped
when probes settle the phase; Hub totals cached per model incl. failures (no
5s timeout on every refresh tick); a failed cache walk fails the observation,
never "nothing to download". Exited records are observed by their facts alone
(no probe/walk/ps — asserted via fakes). Notes for step 10: Status embeds
Record (Identity included) — project the status --json shape deliberately;
snapshot carries EntryID, not config's display name (join in the TUI if
wanted). Known artifact: the step 8 real-spawn detach test is timing-flaky
under -race only (ppid read before reparenting); the declared gate
`go test ./...` is green.

## Intent

`internal/serve`, second half: derive the SERVE.md phase
(`starting`/`downloading`/`running`/`unhealthy`/`exited`) from liveness, the
health probe, and cache completeness; compute download progress from
hubcache bytes vs hubapi sizes.

## Files likely touched

`internal/serve/` (+ tests).

## Decisions made during planning

- Health probe: llama → `GET /health`; mlx → `GET /v1/models` (SERVE.md).
  Loopback when bound `0.0.0.0`, bound address otherwise. Short timeout; one
  probe per status refresh, no retry loops (observation, not control flow).
- Phase derivation is a pure function over (liveness, probe result, cache
  completeness) — fully table-testable with fakes; the probe and walker are
  injected.
- Progress = summed on-disk bytes for the entry's model (including
  `.incomplete`) / hubapi total; without a total, bytes only.
- Status snapshot struct carries everything TUI.md's box and `status --json`
  show — one shape, two renderers (CLI step 10, TUI step 12).

## Acceptance criteria

- Table tests over the full phase matrix, including: alive+cache-incomplete →
  downloading; alive+complete+probe-red before first green → starting;
  probe-red after green → unhealthy; dead → exited.
- Suite result recorded here.
