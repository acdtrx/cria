# Backlog

Deferred bugs and ideas (see `CLAUDE.md`, Scope). Entries that grow graduate to
`docs/plans/<topic>/`. A resolved entry is removed in the same change — git history is
the archive; this file lists only what is still open.

Entry format: a bolded title, what it is in a sentence or two, and the **revisit
trigger** — the observed condition that would make it worth building (features earn
their place). Date rulings inline: `(ruled YYYY-MM-DD: wait to see if needed.)`.
Group entries under headings as themes emerge.

## Dropped from v1

- **Profile layer over entries.** Multiple named param-sets per model with a
  "current" one, instead of flat launchable entries. (ruled 2026-08-18: reality is
  one param-set per model; a variant is just another entry file.) Revisit trigger:
  same-repo entries duplicating args becomes felt friction — e.g. a param change
  that has to touch many files.
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

- **MLX downloads are nearly invisible as a phase.** `mlx_lm.server` binds its
  port and answers `/v1/models` *before* fetching the model, so the
  `downloading` phase (port-not-answering + model-not-cached) shows only for an
  instant; llama's fetch-then-bind shows it fully. cria applies the SERVE.md
  rule faithfully — the phase model would need an mlx-specific signal to do
  better, and no documented one exists. (noted 2026-08-18 during the final e2e.)
  Revisit trigger: MLX first-starts of large models become common and the
  running-but-actually-fetching window proves confusing in daily use.

## Cache view

- **Aliased blobs show as one file.** The walker keys a repo's files by blob, so
  two different snapshot names pointing at the same blob collapse into a single
  row in the cache view; deletion handles the aliasing correctly (it re-scans
  snapshot links itself), only the display is lossy. (noted 2026-08-18 during
  the surgery step.) Revisit trigger: a real repo shows a confusing row where
  two names share bytes, or a deletion plan's "shared blobs left behind" list
  names a file the view never showed.

## Platform & distribution

- **Linux as a supported platform.** `linux/amd64` must keep compiling (`CLAUDE.md`,
  Project Facts) but nothing is tested or distributed. Revisit trigger: a Linux
  machine joins the household, or a friend on Linux asks for a binary.
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
