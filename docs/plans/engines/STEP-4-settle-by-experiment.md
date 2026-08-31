# STEP 4 — settle by experiment: context semantics and the alias lever

Status: not started

## Intent

Two confined live probes, no cria code, whose outcomes become rulings the
rest of phase 2 encodes. Both were flagged by the 2026-08-31 router probe.

## The experiments

1. **Context × parallel on this build.** Same model, three runs, `/slots`
   read each time: explicit `-c C --parallel N` (probe showed divide:
   C/N per slot); `-c C` with no parallel (probe showed 4 slots × full C —
   multiply-up?); and whatever the build's unified-KV switch is, if
   documented. The ruling to settle: what cria's `context` (per-conversation)
   and `parallel` schema fields compose into `-c`/`--parallel`, per engine,
   on the build cria requires — and what the engine composer must do when
   upstream's default shifts under it (the engine module owns the mapping;
   the profile's per-conversation meaning must survive either way).
2. **The alias lever.** A preset section carrying `alias = <entry-id>`:
   does `GET /models` list it, does the `model` field route by it, does
   `?model=` accept it? If yes, router-included entries are addressed by
   entry id and the quant-tag normalization wrinkle is closed. If no, the
   fallback is recorded: cria's router state maps entry id → listed id, and
   clients send the listed id (pi's config names the model string anyway).

## Files likely touched

- This file (the record), `docs/plans/engines/OVERVIEW.md` (rulings
  restated), possibly `docs/specs/CONFIG.md` notes staged for STEP-5.
- Scratch preset files only; no repo code.

## Decisions made during planning

- Machine time: small models on a spare port, same care protocol as the
  router probe (qwen stopped if headroom demands, restored after; user
  cleared or present).
- The context ruling is the user's to confirm once the data is in — the
  recommendation on record (multiply-up, engine-composed) stands unless the
  experiment contradicts it.

## Acceptance criteria

- Both experiments run, raw observations recorded here (commands, `/slots`
  and `/models` outputs summarized).
- The two rulings written, dated, user-confirmed; STEP-5 unblocked.
- Machine restored; scratch files cleaned.
