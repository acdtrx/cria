# STEP 2 — stop signals the spawned server's process group

Status: done (2026-09-24) — unit criteria met; live checks deferred (see Result)

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

- [x] Unit: a stop of a spawned record signals the group (SIGTERM, then SIGKILL on
  grace expiry); a foreign-holder kill signals the pid; both pinned by tests.
- [ ] Live on the dev Mac *when cleared*: llama entry and the router stop as before;
  a router stop leaves no child llama-server behind.
- [x] Live on dgx (STEP-6, 2026-09-24): `cria stop` and the TUI `K` on a loaded vLLM
  left no `VLLM::EngineCore`; memory back to 118 GB available.
- [x] Suite run and recorded.

## Result

- `procs.Host` sends signals three ways: `TerminateGroup` / `KillGroup`
  (`kill(-pgid)`) for servers cria spawned, `Kill` (one pid) for a foreign port
  holder. The pid-directed `Terminate` is gone — nothing sends SIGTERM to a lone
  pid. The guard refuses pid < 1 and, for a group, pgid < 2 (`-1` would be
  "everything this user can signal").
- `serve.Stop` / `serve.Kill` (and so the router stop, validate's displace, the
  TUI's stop and kill keys) signal the record's group; liveness and the wait stay
  on the recorded pid. An ESRCH from a group signal reads as gone and the record
  is removed after the usual confirmation.
- `serve.KillHolder` stays `Kill(pid)`; its doc comment says why.
- Tests: the fake host records `TERM group N` / `KILL group N` apart from
  `KILL N`; the escalation, router-stop and displace tests pin the group path,
  the foreign-holder test pins the pid path, and a new test covers the empty
  group (ESRCH) for stop and kill. In procs, a real-process test proves a group
  kill/terminate ends a shell helper's child while a pid kill leaves it running;
  the refusal test covers group 1.
- `docs/specs/SERVE.md`: Stop section amended (2026-09-24); the router
  paragraph no longer says cria "never touches" the children.
- Suite: `go test ./...` all packages ok; `gofmt -l .` empty; `go vet ./...`
  clean.
- **Deferred:** the live checks — dev Mac llama/router stop leaving no child
  llama-server, and the dgx kill during a loaded vLLM leaving no
  `VLLM::EngineCore` — run in STEP-6 or on a cleared machine; no real server
  was touched for this step.
