# Step 9 — phases, health, download progress

**Phase 3 · Status: not started**

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
