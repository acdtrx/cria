# Step 7 — procs: ps/lsof ownership

**Phase 3 · Status: not started**

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
