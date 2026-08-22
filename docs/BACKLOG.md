# Backlog

Deferred bugs and ideas (see `CLAUDE.md`, Scope). Entries that grow graduate to
`docs/plans/<topic>/`. A resolved entry is removed in the same change — git history is
the archive; this file lists only what is still open.

Entry format: a bolded title, what it is in a sentence or two, and the **revisit
trigger** — the observed condition that would make it worth building (features earn
their place). Date rulings inline: `(ruled YYYY-MM-DD: wait to see if needed.)`.
Group entries under headings as themes emerge.

## Dropped from v1

- **Cache-view growth.** Initiating downloads from the view, scaffolding a config
  entry from a cached model, richer model details (GGUF header metadata, Hub-API
  info). (ruled 2026-08-18: the v1 cache view is visibility and cleaning only,
  `docs/specs/CACHE.md`.) Revisit trigger: wanting to pre-warm a model without
  starting a server, or the browse-then-ask-an-agent loop proving clunky in daily
  use.
- **Hub browsing in the TUI.** Searching `mlx-community` for MLX repos, and listing
  a repo's quants with sizes before downloading. (ruled 2026-08-18: repo research is
  the agent's job, on the Hub; in-TUI quant browsing is of the cache, for deletion.
  The Hub API client exists regardless — download progress needs it.) Revisit
  trigger: sizing up a repo before download becomes a felt need the agent flow
  doesn't cover.

## Tools

- **Version-verdict cache.** `llama-server --version` is ~40 ms normally but can
  take seconds on the first exec after a brew upgrade (signature validation +
  dyld closure over ~15 MB of dylibs) or under memory pressure with a model
  resident; a slow check once got SIGKILLed by the old 3s budget and misread as
  an unverifiable build. The verdict is a pure function of the binary, so a
  cache keyed by (path, mtime, size) would run the exec once per installed
  build. (noted 2026-08-18; the ordering fix — already-running checked first —
  plus the 10s budget should make this moot.) Revisit trigger: the version
  check still visibly delays or mislabels a start after those fixes.

## Serve

- **A multimodal repo's first mmproj download is not oid-matched.** hubapi's
  Total sums only the quant's own files, so an mmproj landing first reads as
  "another file's partial" and the phase stays `starting` until the quant
  itself starts landing. (noted 2026-08-19 while fixing re-download
  detection.) Revisit trigger: a vision model's first start visibly sits in
  `starting` while gigabytes of projector download.

- **`cria validate <id> [choice=option ...]` — swap, prove, restore.** An agent
  running on the local model cannot validate a profile it just wrote: starting
  it means stopping the model it thinks with. Validate makes that safe as one
  blocking command — stop the running server (keeping its record), start the
  target, wait green, send one real completion (fit-proofing: /health passes
  while the first request can still die — the q8 speculative-batch death, the
  mini's Metal limit), stop it, restart the previous server from its record's
  own picks (replayOf's contract), exit with the target's verdict. The agent
  only infers before and after the command, so its branch never burns. Restore
  is unconditional — the machine is left as found even when validation fails —
  and a failed restore is its own loud error naming what is serving now.
  Rejected: validating on a second port with both models resident — the
  machine that needs this is exactly the one without memory for two. To settle
  at build time: target == the running entry (restart-and-prove in place?),
  `--json` for the agent, and the note that other clients of the shared port
  see the window. (noted 2026-08-23, user-designed, deferred by choice.)
  Revisit trigger: the next agent-driven profile-writing session against a
  live server — build it before that session; it is wanted.

## Cache view (orphans)

- **Orphan blobs have no unit.** The real Qwen3.8 repo holds a complete 1.37 GB
  blob (`MTP/mtp-…-Q4_0.gguf`) that no snapshot links — counted in the repo
  total, shown in no row, reclaimable by nothing short of deleting the repo.
  (noted 2026-08-19.) Revisit trigger: orphans show up more than once, or the
  unaccounted gap between a repo's rows and its total confuses in practice.

- **MLX downloads are nearly invisible as a phase.** `mlx_lm.server` binds its
  port and answers `/v1/models` *before* fetching the model, so the
  `downloading` phase (port-not-answering + model-not-cached) shows only for an
  instant; llama's fetch-then-bind shows it fully. cria applies the SERVE.md
  rule faithfully — the phase model would need an mlx-specific signal to do
  better, and no documented one exists. (noted 2026-08-18 during the final e2e.)
  Revisit trigger: MLX first-starts of large models become common and the
  running-but-actually-fetching window proves confusing in daily use.

## Config

- **Seed `cria new` from a URL (`--from <url>`).** Pull an entry TOML from a
  hosted profile collection (e.g. a personal GitHub repo) as the scaffold
  instead of the schema example — still create-only, still opened in the editor
  so pulled args get eyes before they ever reach a server. Deliberately NOT a
  registry (anti-goals; profiles are half machine-specific anyway), and the
  durability half of the original idea needs no feature: the config tree is
  plain TOML and versions perfectly as its own git repo. (noted 2026-08-19,
  idea not fully framed by the user's own account.) Revisit trigger: a profile
  actually gets exchanged between people or machines and re-creating it via
  agent/docs feels like friction.

## Cache view

- **`entriesUsing` matches declared repo/quant, not picks.** The cache view's
  "used by" attribution reads each entry's declared repo/quant; an entry whose
  repo or quant lives in choice options is not matched under those options'
  values (the empty-quant case widens to the whole repo, erring safe). Open
  question first: does "which entries use this model" mean any option, or the
  current pick? (noted 2026-08-23, found in entry-choices step 7.) Revisit
  trigger: a cached quant shows unattributed, or a deletion plan misses a
  choices entry, in real use.
- **Aliased blobs show as one file.** The walker keys a repo's files by blob, so
  two different snapshot names pointing at the same blob collapse into a single
  row in the cache view; deletion handles the aliasing correctly (it re-scans
  snapshot links itself), only the display is lossy. (noted 2026-08-18 during
  the surgery step.) Revisit trigger: a real repo shows a confusing row where
  two names share bytes, or a deletion plan's "shared blobs left behind" list
  names a file the view never showed.

## Platform & distribution

- **Linux as a supported platform.** `linux/amd64` must keep compiling
  (`CLAUDE.md`, Project Facts); release builds exist and a first real
  linux/amd64 smoke test passed (2026-08-18: profile added, download with
  progress, serve, logs, stop). Still short of *supported*: CI runs only the
  short suite there and nobody lives on it. Revisit trigger: a Linux machine
  runs cria daily, or a linux-specific bug arrives.
- **Homebrew tap / browser-download story.** GitHub Releases + the curl
  installer cover distribution (curl sets no quarantine xattr); a binary saved
  through a *browser* still carries it and needs `xattr -d`, which a tap or
  notarization would solve. Revisit trigger: someone actually installs by
  browser download and trips over Gatekeeper.

## Reach

- **Remote-host backend.** One TUI driving other hosts over SSH instead of copying
  the binary per host. Revisit trigger: cria runs on 3+ machines and per-host SSH
  sessions become a felt daily friction. (ruled 2026-08-18: v1 is single-host; keep
  the host-access layer clean enough that a remote backend could slot in.)

## Upstream APIs

- **llama-server router mode.** Recent llama-server hosts several models in one
  process — auto-discovery from the cache, `GET /models`, `POST /models/load` and
  `/models/unload`, `--models-max`. Could replace process-per-entry on the llama
  side. Revisit trigger: wanting several GGUF models resident at once, or per-entry
  process management proving to be daily friction. (noted 2026-08-18: documented and
  maintained upstream, so it clears the bar that log parsing never did.)

## Telemetry

- **Richer server stats** (throughput, slots, memory). Revisit trigger: llama.cpp or
  mlx-lm expose a documented, maintained API for it *and* the raw log tail proves
  insufficient in daily use. (ruled 2026-08-18: log parsing is permanently rejected —
  predecessor llama-runner broke on every llama.cpp release doing this; endpoints
  like `/props` or `/metrics` would qualify, a log format never will.)
