# Step 8 — serve core: records, spawn, liveness, stop

**Phase 3 · Status: done (2026-08-18)** — suite green, all gates pass.
Detachment proven for real: a launcher under an actual pty started a server via
the real Start path, the pty closed, the server survived (ppid=1, no
controlling tty, session leader), and a fresh Manager re-attached from the
record alone and stopped it; also probed: an unreaped `<defunct>` child reads
as exited via identity mismatch, so liveness and stop confirmation are zombie-
safe. Decisions: XDG_STATE_HOME not consulted (recorded in TECH-STACK);
identity is captured only when it names the launched program — a server dead
at capture time records the zero identity, which matches nothing (exited, log
as crash report); pruning runs pre-launch to logsKept−1 so an untidiable log
dir fails the start, and a failed spawn deletes its empty log; stop of an
already-exited server removes the record and succeeds; procs.Identity gained
JSON tags (records persist it).

## Intent

`internal/serve`, first half: the SERVE.md record and process contract — write
and load records, spawn detached with logs, liveness via procs, stop with
TERM→grace→KILL escalation, dismiss, log pruning.

## Files likely touched

`internal/serve/` (+ tests), state-dir helpers.

## Decisions made during planning

- Spawn: `exec.Cmd` with `Setsid`, stdout+stderr to the launch's log file,
  `HF_TOKEN` in env when resolved (step 6). Record written after spawn with pid
  and the pid's `lstart` captured immediately.
- Records at `~/.local/state/cria/servers/<entry-id>.json`, logs at
  `.../logs/<entry-id>-<timestamp>.log`, pruned to newest 3 at launch
  (SERVE.md). State root injectable for tests, like the config root.
- Liveness = pid exists ∧ command line matches ∧ start time matches (procs
  interface); component tests drive all combinations through a fake procs.
- Grace period a named constant (10s); kill skips it.
- **Prove the detachment mechanism** (Debugging rule): a real spawn of a
  long-running stub must survive its parent's exit; the one-line check (`ps`
  after parent death) is part of this step, recorded here.

## Acceptance criteria

- Component tests: record round-trip, replace-on-restart, liveness matrix
  (live / dead pid / reused pid), stop escalation ordering, deliberate stop
  removes record while crash keeps it, log pruning to 3.
- Real-spawn detachment check on the dev Mac recorded here with the suite
  result.
