# Plan: v1 build

Build the full v1 surface of `docs/cria.md` — config-driven serving of
`llama-server` and `mlx_lm.server`, cache visibility and surgery, lifecycle
subcommands, and the TUI — as one static binary. The six specs in `docs/specs/`
are the contract; this plan sequences the work.

## Scope

Everything CONFIG, TOOLS, CACHE, SERVE, CLI and TUI define. Nothing else.

## Out of scope

Everything in `docs/BACKLOG.md`: `cria new`, Hub browsing, the profile layer,
cache-view growth, Linux support beyond compiling, remote hosts, richer telemetry.

## Constraints

- Approved dependencies only: bubbletea/bubbles/lipgloss v2 and go-toml/v2
  (`docs/TECH-STACK.md`); anything further needs a user yes first. Each enters at
  latest stable when its step needs it, not at scaffold time.
- `linux/amd64` must keep compiling; platform-specific code behind build-tagged
  seams. `darwin/arm64` is the only tested target.
- Steps are implemented by Opus subagents in the worktree
  (`.claude/worktrees/v1`, branch `v1`); the main session briefs each step,
  reviews against its acceptance criteria, and never writes the implementation
  itself (`CLAUDE.md`, Plans).
- Tag main immediately before implementation begins (anchor tag names the world
  the plan started from).

## Package layout (planned)

`main.go` does the wiring; each subsystem is a package under `internal/` with a
single public entry point, dependency graph acyclic (CODING-RULES §7):

- `internal/config` — tree load, schema definitions, docs generation, first-run scaffold
- `internal/tools` — tool detection, llama-server cache check
- `internal/hubcache` — cache walk, sizes, surgery
- `internal/hubapi` — Hub API client, token resolution
- `internal/procs` — sole owner of `ps`/`lsof` exec (identity, rss/cpu, foreign scan, port attribution)
- `internal/serve` — records, spawn, liveness, phases, stop
- `internal/cli` — start/stop/status/docs subcommands
- `internal/tui` — the bubbletea app

Module path: `cria` (local module; no remote).

## Phases

- **Phase 1 — config is the interface** (steps 1–3): module scaffold and CLI
  dispatch; schema + strict loader; `cria docs` + first-run scaffold. Goal: an
  agent can write a valid tree and prove it parses.
- **Phase 2 — seeing the host** (steps 4–6): tools detection and the cache-age
  check; cache walker with true sizes; Hub API client + token resolution. Goal:
  cria reports the host truthfully, read-only.
- **Phase 3 — lifecycle** (steps 7–10): procs; serve core (records, spawn,
  liveness, stop, logs); phases/health/download progress; CLI start/stop/status.
  Goal: the full serving loop scriptable with no TUI — this is also where the
  agent-validation story first works end to end.
- **Phase 4 — cache surgery** (step 11): deletion with reclaim reporting and the
  serving guard.
- **Phase 5 — the TUI** (steps 12–14): shell + status box + keybar + prefs;
  serve view; cache view + tools pane.
- **Phase 6 — finish** (step 15): end-to-end validation on the dev Mac,
  `ARCHITECTURE.md`, version tag.

Suite green and committed at every phase end; within a phase, each step runs the
suite and records the result (expected reds named with the step that clears them).

## Risks

- The llama-server hub-cache threshold build is unknown → research task inside
  step 4; the pinned build lands in `docs/specs/TOOLS.md` in the same edit.
- Detached-process behavior on macOS (session leaders, orphaning, signal
  delivery) → step 8 proves it with a real spawn before anything builds on it.
- bubbletea v2 API and `charm.land` import paths → verified against latest
  stable at step 12, the first step that imports them.
- mlx_lm.server may lack `/health` → SERVE.md already covers it (`/v1/models`).
- HF cache layout edge cases (shared blobs, shards, partials, single-file
  repos) → fixture-driven table tests in steps 5 and 11.

## End-to-end verification (after phase 6)

On the dev Mac: `gofmt -l .` empty, `go vet ./...` clean, `go test ./...` green,
`GOOS=linux GOARCH=amd64 go build` succeeds. Then the scripted manual pass of
step 15: serve both backends, the one-port swap workflow, `status --json` sanity,
a real quant deletion checked against `du`, the foreign-server drill, and a full
TUI session touching every screen. Results recorded in the step file.
