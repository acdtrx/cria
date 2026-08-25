# STEP 1 — displacement resolution and the busy gate

Status: done (2026-08-25)

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

## What was built

`internal/serve/validate.go` — two mechanisms, no CLI:

- `Displaced(entry config.Entry) (Displacement, error)` — the target's own
  resolved port read through `PortUse` (no attribution machinery duplicated),
  in a swap's vocabulary: `Displacement{Port, Holder *Record, Foreign
  []Holder}` with `Free()`. A live record is the server to displace; processes
  cria did not start come back as `Foreign` for the caller to refuse over;
  target == holder is the same path as any other.
- `Generating(record Record) Generation` — the busy gate. `Generation{Busy,
  Detail}` over `Busy` = `BusyIdle` / `BusyGenerating` / `BusyUnverifiable`.
  llama: `GET /slots` at the probe's address rule, decoded into a minimal
  `slot` struct (`is_processing` only, unknown fields ignored), busy when any
  slot is working. mlx is never asked at all. Unreachable, non-2xx (the
  server's own refusal text quoted via `warm.go`'s `refusal`), a payload that
  is not a slot listing, and an empty listing are all `BusyUnverifiable` with
  a detail naming what stood in the way — never idle.
- Seam `slotsReader` on the Manager (`slots`), matching `probe`/`warm`/`bench`,
  wired to `newHTTPSlots()` in `New`.

Decision made while implementing: an empty `/slots` listing is unverifiable
rather than idle — an answer with no evidence under it is the one shape this
gate must not take (CODING-RULES §4).

## Suite

`go test ./...` from the worktree root: **all packages ok**, no expected reds.
`gofmt -l .` empty, `go vet ./...` clean. Nine new tests in
`internal/serve/validate_test.go` (holder by port · self-validation · exited
record ignored · free port · foreign holder with pid/command/dir · the slot
URL rule · busy/idle over real payloads · the three unverifiable payload
cases · unreachable · mlx never asked).
