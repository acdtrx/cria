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

- **Router-aware `cria validate`.** `cria validate <id>` proves an entry serves
  under the **llama** engine: it displaces whatever holds the entry's own port,
  starts that one entry, asks it for a completion and puts the displaced server
  back (`docs/specs/SERVE.md`, Validate). It knows nothing about the router, so
  proving the combination the router holds an entry under means stopping the
  router by hand, starting the entry as itself on some port, and undoing both.
  What a router-aware validate would answer instead: does this entry, in the
  combination `models.json` holds it under, actually load and answer *through the
  running router* — which is a different protocol, since nothing needs displacing
  (the router is already up), the load is `POST /models/load` rather than a spawn,
  and the proof is a completion naming the model's alias. Deliberately out of
  scope when the router landed (`docs/plans/engines/OVERVIEW.md`): the entry-level
  validate had just been settled and user-designed, and a second protocol under
  the same verb wanted its own design pass. Revisit trigger: wanting to validate
  an entry's router combination without hand-stopping the router — most likely
  the first time a profile that serves fine on its own is skipped or fails under
  the router and the difference has to be found by hand.

- **Router discovery exposes the whole cache to clients.** Upstream's
  `GET /models` lists every HF-cached model with no flag to narrow it;
  inclusion controls only the preset and the aliases, so a client can name
  and (within `--models-max`) autoload any cached model — the residency cap
  is the only guard, and nothing upstream is memory-aware. Found in live pi
  use 2026-09-05 (pi listed the full cache; only `/model` filtered to the
  aliased set). Nothing for cria to build today — the listing is upstream's;
  `--models-max` in `router_args` plus `--no-models-autoload` (if the
  client drives explicit loads) are the levers, both documented in
  SERVE.md. Revisit trigger: upstream grows a preset-only/discovery-off
  flag — adopt it in the composed argv the moment it exists.

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
  Home (ruled 2026-08-27, and built into the boundary since): the collector is
  llama-engine territory — engine modules know their engine by design
  (`docs/ARCHITECTURE.md`); the TUI asks the engine what it reports rather than
  special-casing a backend. Under the router the same collector transfers by
  `?model=` addressing (probe-verified 2026-08-31, and already used by the
  unload busy gate), so slots visibility would reach the router's models too. Build after the
  validate plan lands — wanted; the trigger (watching logs to see how the
  server is doing) is being felt.

## Method

- **The contract/structure test triage as a template lesson.** The engines
  refactor ran under a discipline written before the first edit (ruled
  2026-08-27, user-raised: "behavior-preserving" and "test-preserving" are
  different claims): every test classified as *contract* — what the outside sees,
  never red at any step — or *structure* — pins the current shape, may go red
  mid-phase, each named with the step that rewrites it, and never deleted except
  paired with its replacement. It held for nine steps across three phases
  (`docs/plans/engines/TRIAGE.md` is the classification, and each step file
  records its own rewrites and mutation checks). Open question: whether it
  graduates into `CLAUDE.md` as standing methodology, which would bind every
  future refactor. Revisit trigger: the next refactor of comparable size — either
  it is reached for again unprompted, or its absence is felt.
