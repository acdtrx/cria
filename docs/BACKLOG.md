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

- **Keyed servers vs cria's own requests.** An entry carrying
  `--api-key-file` makes llama-server 401 everything but `/health`: phases
  and start/stop survive, but the mlx warm, validate's prove, `cria bench`
  and the `/slots` busy gate all send unauthenticated requests — prove
  hard-fails, the gate degrades to unverifiable. If cria is to drive keyed
  servers, the key-file path becomes engine-module knowledge (read the file,
  attach the header) — never the key via argv. (noted 2026-08-28 while
  designing LAN restriction: llama has no IP allowlist, so proxy-only access
  is api-key at llama + pf at the host.) Revisit trigger: a server actually
  gets keyed.

- **A multimodal repo's first mmproj download is not oid-matched.** hubapi's
  Total sums only the quant's own files, so an mmproj landing first reads as
  "another file's partial" and the phase stays `starting` until the quant
  itself starts landing. (noted 2026-08-19 while fixing re-download
  detection.) Revisit trigger: a vision model's first start visibly sits in
  `starting` while gigabytes of projector download.

- **`cria validate <id> [choice=option ...]` — swap, prove, restore.** An agent
  running on the local model cannot validate a profile it just wrote: starting
  it means stopping the model it thinks with. Validate makes that safe as one
  blocking command — stop the server holding the target's port (keeping its
  record; port-scoped, settled 2026-08-25: the target entry itself carries the
  one fact the agent has no prior knowledge of, servers on other ports are
  structurally untouchable, and on shared-port machines — the config doctrine —
  the port holder *is* the running server; rejected: refusing when several
  servers run, and any flag naming a server to stop), start the
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
  it (verified 2026-08-25 against the live server: 200 on the 0.0.0.0 bind
  with `--slot-save-path` alone, per-slot `is_processing` exposed; whether a
  server with no slot flags at all needs `--slots` is the one enable-flag
  case still to verify at build time); mlx documents no equivalent, so
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

## Engines

- **Engine config + shared model profiles; router as a third engine.**
  (direction settled in discussion 2026-08-27; supersedes the global-profile
  and router-mode entries — git holds their history.) The cut: model profiles
  keep only what is true of the model wherever it runs — repo, quant, name,
  sampling, fit choices — shared verbatim by the llama and router engines;
  per-engine config (`engines/llama.toml`, `engines/router.toml`,
  `engines/mlx.toml`) owns how this machine runs that engine — `ngl`, `fa`,
  ports, `models-max`, parallelism. The TUI's backend toggle becomes an
  engine toggle. Rejected: separate router profiles — the model facts would
  fork and drift; the "global profile" framing — those params were never
  global, they were engine-scoped, which also settles that entry's
  per-backend-scoping question.
  **Args become ini-style keys** (`key = value`, upstream's own preset
  spelling): key→argv is mechanical without arity knowledge (`c = 65536` →
  `-c 65536`, `jinja = true` → `--jinja`), composition is order-independent
  with exact key collisions (retiring the token heuristics), override
  precedence is upstream's documented model-section > engine `[*]` rather
  than cria's invention, and the router's `--models-preset` ini composes
  near-verbatim into the state dir — one section per included entry plus its
  router-scoped picks; section names = entry ids = the `model` field clients
  send. (Router half-fired 2026-08-23: pi-llama-cpp *requires* router mode —
  it manages models via `GET /models` + load/unload, which single-model
  servers lack.)
  **Context semantics** (open — user still pondering 2026-08-27;
  recommendation recorded): the model profile declares *per-conversation*
  context, the engine declares parallelism, and the engine's composer
  computes the pool (`-c = context × parallel`) — not llama's pool-spelling:
  the written number should be what a client experiences (the current tree
  needs comments to explain the division), and raising parallel then costs
  memory loudly at start (validate's exact job) instead of silently halving
  every conversation. `context`/`parallel` would join `host`/`port` as
  schema fields composed by the engine module — not an exception but the
  layer's job (ruled 2026-08-27, user): cria is a llama-and-mlx runner, not
  a generic process manager. The *tree's* args stay flag-agnostic
  passthrough; each **engine module knows its engine by design** — schema
  fields, endpoint knowledge (`/health`, `/slots`, the completion shape —
  all already engine knowledge living in serve), phase semantics, and stats
  collection (the slots-visibility entry's collector is llama-engine
  territory; mlx answers "nothing" honestly; router reuses the llama
  family's with `?model=` addressing — confirm `/slots` takes it during the
  live probe). Upstream drift (unified KV) is then contained in the engine
  module that owns the semantics.
  **Refactor discipline for the extraction** (ruled 2026-08-27, user-raised:
  test-preservation must not become the target in place of the new
  architecture — "behavior-preserving" and "test-preserving" are different
  claims): step zero classifies every test as *contract* (what the outside
  sees — CLI output and exit codes, TUI frames, files, HTTP requests made;
  the refactor's definition of behavior) or *structure* (pins the current
  shape — seams, fakes, signatures). Contract tests never go red at any
  step; structure tests may go red mid-phase, each named with the step that
  rewrites it against the Engine interface — CLAUDE.md's phase rule,
  sharpened to say which tests it licenses. The interface is designed from
  the seams inventory before the first edit; steps move seams behind it,
  never shuffle-until-green. No shim survives a phase end (code whose only
  caller is an old-shape test is a named red in disguise). Fakes are
  regenerated against the interface — an awkward fake is interface feedback,
  not a reason to adapt. Deleting a structure test of a removed shape is
  legitimate only paired, in the step file, with its replacement asserting
  the same concern against the new shape; contract tests keep the full
  never-delete protection. If this discipline proves out here, it graduates
  to CLAUDE.md as a template lesson.
  Open rulings: repeatable flags are inexpressible as keys (upstream's
  preset shares the limit) — entries needing them stay argv-only or the
  limit is accepted; booleans mirror upstream exactly; router inclusion
  stays router-scoped state per the 2026-08-23 ruling, pending one
  confirmation now that engines exist; migration is a manual rewrite of
  every profile's args block (feature-building mode, ~15 files).
  **Shape of the build** (ruled 2026-08-27, user): an Engine interface with
  one implementation per engine, extracted from the seams the code already
  has — today the backend is a string enum dispatched at ~10 named
  predicates/switches (command+tool composition, health endpoint,
  LoadsLazily, publishesSlots, record validation, hub/cache presence
  semantics, schema fields, TUI toggle, scaffold). Phase 1 of the plan is
  that extraction, behavior-preserving, llama+mlx only — the suite validates
  it — then router lands as the third implementation instead of an eleventh
  if-site. The interface's altitude is the open design question the probe
  informs: not "give me argv" but make-this-entry-serve / stop-serving /
  what-is-its-state — process spawn/kill for llama and mlx, load/unload API
  calls against one resident process for the router, preset-ini
  materialization instead of argv composition.
  Router facts docs-verified 2026-08-27 (see git history of the router
  entry for the full list): HF-cache discovery, on-demand autoload,
  `--models-max` residency with idle sleep, `GET /models` +
  `POST /models/load|unload`. Revisit trigger: the confined live probe — a
  hand-written preset run on a machine with room, proving discovery sees
  the cached quants and sections accept the keys the entries actually use
  (`ctk`, `spec-type`, …), and how load/unload, sleep and `?model=`
  addressing behave — build nothing before it passes; graduates to
  `docs/plans/` after.

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

## Telemetry

- **Richer server stats** (throughput, slots, memory). Revisit trigger: llama.cpp or
  mlx-lm expose a documented, maintained API for it *and* the raw log tail proves
  insufficient in daily use. (ruled 2026-08-18: log parsing is permanently rejected —
  predecessor llama-runner broke on every llama.cpp release doing this; endpoints
  like `/props` or `/metrics` would qualify, a log format never will.)

- **Context-cache (slots) visibility.** With slot saving live (a `slots`
  choice on the main profile), knowing whether the context cache fills up
  means a view of per-slot fill against the slot's context, plus the sizes of
  saved slot files on disk. Same rails as the ruling above: llama's documented
  `/slots` endpoint plus the filesystem, never logs; mlx documents no
  equivalent, so this is llama-only. The data is verified there (2026-08-25,
  live probe): per-slot `n_ctx`, `n_prompt_tokens`, `n_prompt_tokens_cache`,
  `is_processing`. Direction (user-sketched 2026-08-25): `is_processing`
  gates the label — a busy slot shows live data, an idle one shows what it
  last processed. `/slots` carries counts, not rates (verified same probe);
  tok/sec comes from cria's own poll deltas — `n_decoded` and
  `n_prompt_tokens_processed` across ticks give live decode/prefill rates,
  remembered as the idle slot's "last" rate. Rejected for now: `--metrics`
  (Prometheus) — lifetime averages, needs a flag in every profile (the
  running server answers 501), and poll deltas answer the actual question.
  Home (ruled 2026-08-27): the collector is llama-engine territory — engine
  modules know their engine by design (see Engines); the TUI asks the engine
  what it reports rather than special-casing a backend. Build after the
  validate plan lands — wanted; the trigger (watching logs to see how the
  server is doing) is being felt.
