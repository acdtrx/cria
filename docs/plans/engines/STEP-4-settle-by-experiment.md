# STEP 4 — settle by experiment: the alias lever

Status: done (2026-08-31) — the lever holds; ruling below

## Probe record (2026-08-31, build 10450, port 11437, qwen untouched on 11434)

Preset: one section, `[LiquidAI/LFM2.5-2.6B-GGUF:Q8_0]` carrying
`alias = lfm25-26b-q8` (an entry id's exact shape). Observed:

- `GET /models` lists the alias: `aliases: ['lfm25-26b-q8']` on the row.
- A chat completion with `"model": "lfm25-26b-q8"` routed and answered
  (the response's own `model` field reports the canonical id — clients that
  echo it show the model reference, not the entry id; cosmetic, noted).
- `GET /slots?model=lfm25-26b-q8` → 200 with slot data.
- `GET /props?model=lfm25-26b-q8` → 200.

**Ruling (settled 2026-08-31): router-included entries are addressed by
entry id.** Each composed preset section carries `alias = <entry-id>`; the
section name stays the model reference; clients (pi) send entry ids; cria's
status and stats address children by the same ids. The quant-tag
normalization wrinkle from the router probe is closed — normalization
applies to section names, and the alias sidesteps it entirely. STEP-8
encodes this; no fallback mapping is needed.

## Intent

One confined live probe, no cria code, whose outcome becomes the ruling
STEP-8 encodes: how router-included entries get their client-facing names.

(As planned this step also carried a context-semantics experiment; dropped
2026-08-31 by OVERVIEW ruling 3 — context and parallel are passthrough
keys, so cria has no emit rule to verify. The auto-parallel observation from
the router probe stays in the backlog's git history as author guidance.)

## The experiment

**The alias lever.** A preset section carrying `alias = <entry-id>`: does
`GET /models` list it, does the `model` field route by it, does `?model=`
accept it? Background (probe 2026-08-31): section quant tags normalize
(`UD-Q4_K_XL` → listed id `Q4_K_XL`), so section names are not verbatim
client names, and each child already receives `--alias <id>`.

- If yes: router-included entries are addressed by entry id — section name
  stays the model reference, `alias` carries the entry id, clients (pi)
  send entry ids. The normalization wrinkle closes.
- If no: the fallback is recorded — cria's router state maps entry id →
  listed id, and clients send the listed id (pi's config names the model
  string anyway).

## Files likely touched

- This file (the record) and `docs/plans/engines/OVERVIEW.md` (the ruling
  restated). Scratch preset files only; no repo code.

## Decisions made during planning

- Machine time: one small model on a spare port, same care protocol as the
  router probe (user cleared or present); minutes, not hours.

## Acceptance criteria

- The probe run, raw observations recorded here (`/models` listing, a
  routed completion, `?model=` by alias).
- The naming ruling written, dated; STEP-8 unblocked.
- Machine restored; scratch files cleaned.
