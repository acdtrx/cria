# Step 8 — serve core: records, spawn, liveness, stop

**Phase 3 · Status: not started**

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
