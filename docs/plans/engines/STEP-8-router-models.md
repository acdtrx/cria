# STEP 8 — models under the router

Status: not started

## Intent

The router serves the *same* llama entries as its source: which entries are
included — each with its own combination of picks, independent of the llama
engine's — is router-scoped state; the preset composes one section per
included entry; load/unload and child status flow through the documented
API.

## Files likely touched

- `internal/engine/router.go`, `internal/picks` (router-scoped store beside
  entry picks — same doctrine, one level up), `internal/serve` status.
- `internal/cli`: surfaces for include/exclude and load/unload verbs
  (spelling settled here against existing CLI idioms).

## Decisions made during planning

- **Inclusion is state, edited like picks** (user ruling 2026-08-23,
  reconfirmed and refined at plan review 2026-08-31): config declares what
  can vary, state holds what is chosen. One combo per included entry, held
  in the router's per-engine state subfolder (STEP-7's layout) — router
  picks for an entry may differ from the llama engine's picks for the same
  entry, by design.
- **Section names / client naming** per STEP-4's alias ruling: entry ids
  become the `model` field if the alias lever holds; otherwise the recorded
  fallback mapping.
- Child phases derive from `GET /models` statuses
  (unloaded/loading/loaded/sleeping/failed) — cria maps them onto its phase
  vocabulary without inventing signals; stats and busy checks reuse the
  llama collector via `?model=` (probe-verified).
- Only llama-backend entries are includable (mlx has no router); the
  refusal names why.
- Autoload stays on (upstream default): pi swaps models by naming them —
  the probe's eviction behavior is the feature. Explicit
  `POST /models/load|unload` are exposed as deliberate verbs anyway
  (mechanism decoupled from trigger).

## Acceptance criteria

- Component tests: preset sections from included entries + their combos
  (collision rule across engine/entry/option levels holds in preset
  spelling too); inclusion state round-trips; status mapping from a fake
  `GET /models`; the mlx-entry refusal.
- Live smoke: two small entries included, both reachable through the router
  by their client names; a pick change for one regenerates the preset on
  next router start and the section diff shows exactly that change.
- Suite state recorded.
