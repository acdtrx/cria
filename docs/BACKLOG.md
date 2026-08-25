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
  machine that needs this is exactly the one without memory for two.
  Settled 2026-08-23: target == the running entry follows the same protocol
  (the point may be validating a new combination of it; restore replays the
  record's picks either way, one code path); no `--json` — the agent's
  contract is the exit code and a concise reason, and structured output earns
  its place only if validate ever grows measurements; the busy gate refuses
  when the running server is mid-generation, and the signal must be
  is-processing, never open-connections — the validating agent's own client
  holds an idle keep-alive socket to the port, so counting connections would
  refuse on the caller's ghost. llama's documented `/slots` endpoint answers
  it (verify at build time: the enable flag, and what the endpoint exposes on
  a 0.0.0.0 bind before cria composes it); mlx documents no equivalent, so
  mlx gets the honest degradation (a warning, or a coarse check that names
  its false positive) plus an override flag. Revisit trigger: the next
  agent-driven profile-writing session against a live server — build it
  before that session; it is wanted.

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

- **A global profile merged into every entry.** Params and choices stable across
  profiles — `-ngl 99`, `-fa on`, an mmproj or slot-saving toggle — live once in
  a global profile that composition merges into each entry: redundancy removal
  and uniformity on purpose-stable settings, nothing more. It is merged with
  profiles, not globally applicable state, and the tree stays human-owned. To
  settle at design time: per-backend scoping (those are llama flags; mlx
  entries must not inherit them); how the loud flag-collision rule extends
  across the merge — entry-overrides-global is an ordering decision, and
  silent both-win is exactly what the collision rule exists to forbid; and
  where a global choice's pick lives relative to per-entry picks. (noted
  2026-08-25.) Revisit trigger: the next uniformity edit across several
  profiles — the same flag changed file by file — or a drift found where one
  profile missed a stable param.

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
  `/models/unload`, `--models-max`, per-model settings via a `--models-preset`
  ini (`[*]` defaults, per-model overrides). Could replace process-per-entry on
  the llama side. Revisit trigger: wanting several GGUF models resident at once,
  or per-entry process management proving to be daily friction — and it half-fired
  2026-08-23: pi's llama.cpp integration (pi-llama-cpp) *requires* router mode
  (it manages models via `GET /models` + load/unload, which single-model servers
  lack). Verify before designing: whether router discovery covers the HF hub
  cache (the announcement names `LLAMA_CACHE`/`~/.cache/llama.cpp` — a private
  cache would collide with the single-source-of-truth constraint), and whether
  preset sections carry the full flag surface entries compose (`-ctk`,
  `--parallel`, `--spec-type`, …) — the README documents the flags, not the ini
  keys. A cria-managed router would compose the ini into the state dir the way
  argv is composed today; the tree stays human-owned. Direction (user-sketched
  2026-08-23): a third backend alongside llama and mlx — it runs one router
  process and shows the *same* llama entries as its source; which entries are
  included, each with its own combination of choices independent of the llama
  backend's picks, is router-scoped state edited like picks are (config
  declares what can vary, state holds what is chosen — the choices doctrine,
  one step further). One combo per included entry; section names = entry ids =
  the `model` field clients send. The hard collision to verify first: argv vs
  ini keys — entries carry passthrough tokens and cria refuses flag knowledge,
  so the design stands only if ini keys are flag-spelled (mechanical strip) or
  router-included entries are required to use long-form args; a short→long
  mapping inside cria is off the table. (noted 2026-08-18: documented and
  maintained upstream, so it clears the bar that log parsing never did.)

## Telemetry

- **Richer server stats** (throughput, slots, memory). Revisit trigger: llama.cpp or
  mlx-lm expose a documented, maintained API for it *and* the raw log tail proves
  insufficient in daily use. (ruled 2026-08-18: log parsing is permanently rejected —
  predecessor llama-runner broke on every llama.cpp release doing this; endpoints
  like `/props` or `/metrics` would qualify, a log format never will.)

- **Context-cache (slots) visibility.** With slot saving enabled
  (`--slot-save-path`, in test on the slottest profile), knowing whether the
  context cache fills up means a view of per-slot fill against the entry's
  context size, plus the sizes of saved slot files on disk. Same rails as the
  ruling above: llama's documented `/slots` endpoint — already slated for
  validate's busy gate; verify at build time what it exposes for context fill —
  plus the filesystem, never logs; mlx documents no equivalent, so this is
  llama-only. (noted 2026-08-25 during the slot save/restore experiment.)
  Revisit trigger: slot saving graduates past the test profile, or a session
  dies to a full context that cria could have shown filling.
