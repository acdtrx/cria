# Step 6 — Hub API client and token resolution

**Phase 2 · Status: done (2026-08-18) — phase 2 ends green.** Suite green
(incl. -race on the new packages), all gates pass; verified against the real
Hub API (exact byte matches per quant, sharded BF16 summed, pagination via
Link headers forced with &limit and handled). Decisions: token resolution
gained the XDG step huggingface_hub itself uses; only `Total` and `Token` are
public (listing stays internal — Hub browsing is backlog); unpublished quant
and unfollowable pagination are "unavailable", never a short sum; the Hub
answers 401 (not 404) for invisible repos and lists gated repos fine.
Defect found and fixed across steps 5+6: unsloth `UD-` tags are part of the
quant label (llama.cpp's -hf manifest serves the documented spelling), with
exact-first / unique-tolerant lookup shared by Presence and Total via one
exported rule (`hubcache.GGUFItem`/`MatchQuant`).

## Intent

`internal/hubapi`: repo file listings with sizes (the download-progress
denominator) and HF token resolution (`HF_TOKEN` env, else the hf token file) —
`net/http` only.

## Files likely touched

`internal/hubapi/` (+ tests).

## Decisions made during planning

- Endpoint: the models tree API (`/api/models/<repo>/tree/main?recursive=true`),
  sizes summed for the files an entry needs (the quant's files for llama, the
  whole repo for mlx).
- Token resolution order: `HF_TOKEN` env var, then `$HF_HOME/token`, then
  `~/.cache/huggingface/token`; absent token is not an error (public repos).
- Unreachable API degrades gracefully: progress without a total (SERVE.md) —
  the client returns a typed "unavailable" result, never blocks a start.
- Short timeout; token sent via `Authorization` header only.

## Acceptance criteria

- Tests via `httptest`: listing parsed, sizes summed per quant and per repo,
  auth header present iff a token resolved, timeout/unreachable → typed
  unavailable.
- **Phase 2 ends here**: suite green, committed; on the dev Mac the walker output
  matches the real cache and a real repo listing resolves. Result recorded here.
