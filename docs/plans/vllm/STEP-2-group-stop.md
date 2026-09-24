# STEP 2 — stop signals the spawned server's process group

Status: not started

## Intent

A stop or kill of a server cria spawned reaches every process that server
started, not only the one pid in its record. Proven necessary by the 2026-09-24
probe: SIGKILL on vLLM's API server orphaned `VLLM::EngineCore` holding ~84 GB;
SIGTERM to the group cleared it. Engine-agnostic by nature — it corrects the
stop for every multi-process server (vLLM's engine core, the llama router's
children), and changes nothing for a single-process one.

## Files likely touched

- `internal/procs/procs.go` — a group-directed terminate/kill beside the
  pid-directed ones (or a parameter), still the one place signals are sent.
- `internal/serve/stop.go` — records cria spawned are signalled by group.
- `internal/serve/*_test.go`, `internal/procs/*_test.go` — the fake host learns
  the distinction; tests pin which path each stop takes.
- `docs/specs/SERVE.md` — Stop section, same edit.

## Decisions made during planning

- **Group only for records cria spawned.** They are spawned with `Setsid`, so
  each leads its own session and group and pgid = pid; signalling `-pid` is
  exactly "this server and everything it started". A foreign port holder the
  TUI offers to kill stays pid-only: cria does not know it leads a group, and
  `kill(-pid)` on a non-leader would miss or, worse, hit someone else's group.
- **Liveness stays pid-based.** The record's identity is the API server; "gone"
  still means that pid is gone. What the group signal adds is that its children
  do not outlive it. No second reconciler polls for orphans (CLAUDE.md, no
  stacked safety nets).
- **The grace stays 10 s.** The probe measured a clean vLLM SIGTERM at ~1 s.
- A group already gone (`ESRCH`) is the same answer as a pid already gone.

## Acceptance criteria

- Unit: a stop of a spawned record signals the group (SIGTERM, then SIGKILL on
  grace expiry); a foreign-holder kill signals the pid; both pinned by tests.
- Live on the dev Mac *when cleared*: llama entry and the router stop as before;
  a router stop leaves no child llama-server behind.
- Live on dgx (STEP-6): kill during a loaded vLLM leaves no `VLLM::EngineCore`.
- Suite run and recorded.
