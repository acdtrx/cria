# STEP 7 — the router engine: process lifecycle

Status: not started

## Intent

The third Engine implementation's foundation: cria starts, observes and
stops one router process per host, composed from the router engine config
and the included entries' preset — models come in STEP-8; this step is the
supervisor itself.

## Files likely touched

- `internal/engine/router.go` (new) + tests.
- `internal/serve`: the router's record (a server record whose entry is the
  engine, not a model entry — shape to settle), preset composition into
  `~/.local/state/cria/` beside the argv-composed records, health/liveness
  over the router port.
- `docs/specs/SERVE.md` staged additions (landed in STEP-9 with the rest).

## Decisions made during planning

- **The preset file is runtime state**, composed at start into the state
  dir the way argv is composed today — the tree stays human-owned; the
  composed preset is regenerated every start, never edited.
- **State is organized per engine** (OVERVIEW ruling 2): a subfolder per
  engine under `~/.local/state/cria/` holds engine-scoped state — the
  router's folder gets the composed preset, its inclusion + router picks
  (STEP-8), and its records. The exact layout (and whether existing
  llama-engine state moves under its own folder now or stays put) is
  settled in this step's design and recorded here; feature-building mode
  applies if anything moves — loud refusal of the old location, manual fix
  named.
- **Engine config** (`engines/router.toml`): the router port, `models-max`,
  autoload, sleep, and the `[*]` block's keys. Bind rule follows the same
  host doctrine as entries.
- **One router per host** (matching cria's single-host shape); it serves on
  its own port beside process-engine entries — port collisions are the
  existing loud refusals.
- Probe facts encoded: children are upstream's concern; cria's liveness is
  the router pid + its port answering; child states come from `GET /models`
  (STEP-8), never from `ps` against children.
- llama-server capability check: the tools report gains "router mode
  present" (flag probe on the binary, same style as version checks) so a
  too-old build refuses with the honest message instead of a spawn failure.

## Acceptance criteria

- Component tests: preset composition (engine `[*]` + placeholder includes)
  byte-exact against fixtures; start/stop/record/liveness against fakes;
  the too-old-binary refusal.
- A hand-run smoke (dev Mac, spare port): `cria` starts the router from an
  engines/router.toml, health goes green, stop leaves nothing behind.
- Suite state recorded; expected reds named with clearing step.
