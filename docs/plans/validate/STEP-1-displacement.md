# STEP 1 — displacement resolution and the busy gate

Status: not started

## Intent

Given a target entry (picks resolved), answer: which live record, if any,
holds the port this launch needs — and may it be displaced right now? All of
it in `internal/serve`, pure mechanism, no CLI.

## Files likely touched

- `internal/serve/validate.go` (new) — displacement resolution, busy probe.
- `internal/serve/validate_test.go` (new).
- Possibly `internal/serve/port.go` / `internal/serve/record.go` for small
  shared readers (records by port already exist in some form for start's
  port check — reuse, don't duplicate).

## Decisions made during planning

- The target's port is the entry's resolved port (entry port or
  `default_port`) — the same value start composes with.
- Holder resolution reads live records only (liveness rules of SERVE.md): an
  exited record holding the port is not a holder; a foreign process holding
  the port is a refusal (it has no record to restore), reported with pid and
  command line exactly as start's port check reports it.
- Busy gate: `GET /slots` on the holder, loopback when bound `0.0.0.0` (the
  health-probe rule); busy = any slot `is_processing == true`. HTTP non-200 or
  unreachable ⇒ the signal is absent, not busy: the caller gets
  "unverifiable" distinctly from "busy", and the CLI turns that into the
  warning + override behavior. mlx: no probe attempted — always
  "unverifiable", named as such.
- `/slots` is read with a JSON decoder into a minimal struct (`is_processing`
  only); unknown fields ignored — this is a documented endpoint whose schema
  may grow, and validate reads one flag from it.

## Acceptance criteria

- Unit-tested against fake records + a fake HTTP server: holder found by
  port; no holder; exited record ignored; foreign holder refused with
  pid/command; busy true/false; unreachable and non-200 both "unverifiable";
  mlx "unverifiable" without any HTTP call.
- No log reading anywhere; no new dependencies.
- Suite state at step end recorded here (expected reds named, if any).
