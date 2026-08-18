# Step 6 — Hub API client and token resolution

**Phase 2 · Status: not started**

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
