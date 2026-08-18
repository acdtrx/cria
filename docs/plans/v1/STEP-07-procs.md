# Step 7 — procs: ps/lsof ownership

**Phase 3 · Status: done (2026-08-18)** — suite green (table tests on canned
output + integration tests against processes the suite starts itself), all
gates pass; live probe found a real llama-server and mlx_lm.server with
identity, cwd and port attribution. Findings against reality: `ps -o comm=`
is unusable (a script-installed mlx_lm.server reports its Python interpreter;
relative-path starts truncate to 16 chars) — matching is argv[0] basename or a
path-shaped argv[1]; `lstart` is locale-dependent, so every exec pins
`LC_ALL=C`; `-o command=` does not truncate (`-ww` kept for Linux procps);
`-A` not `-e` (macOS reads `-e` as environment). Deviations accepted: no
build-tag seam (the flag set is common to macOS and Linux — a seam with
nothing behind it); `Listeners` returns all pids on a port (dual-stack and
forked servers genuinely hold several — serve chooses how to report); signal
delivery refuses pid < 1 (kill(2) group semantics).

## Intent

`internal/procs`: the single module that execs `ps` and `lsof`
(CODING-RULES §7). Provides process identity (pid + start time + command line),
resource stats (RSS, %CPU), the foreign-server scan, port attribution, working
directory, and kill.

## Files likely touched

`internal/procs/` (+ tests on canned command outputs).

## Decisions made during planning

- Explicit field selectors only: `ps -o` with named columns, `lsof -F` machine
  format — never the human tables (TECH-STACK).
- Start-time comparison uses `ps -o lstart` parsed once at record time and
  compared as an equality of the same source — no clock math across formats.
- Foreign scan matches process command basenames `llama-server` and
  `mlx_lm.server`; cwd via `lsof -p <pid> -a -d cwd -F`.
- The package exposes an interface consumed by serve, with the exec-backed
  implementation behind it — the seam that makes serve's logic testable without
  real processes. One interface, one real implementation; no speculative
  variants.
- Any darwin-specific flag differences go behind a build-tagged seam; linux keeps
  compiling.

## Acceptance criteria

- Table tests parse canned `ps`/`lsof` outputs: normal rows, missing process,
  weird command lines (spaces, unicode), port with no listener.
- Manual check on the dev Mac against a real process recorded here, with the
  suite result.
