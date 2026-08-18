# SERVE — server lifecycle

Servers outlive the TUI and there is no daemon (`docs/cria.md`, principle 3): cria
spawns them detached, records what it did, and any later cria invocation re-attaches
by reading those records back. This spec owns the lifecycle contract — start,
observe, stop, re-attach — and the runtime state records behind it.

## Process model (settled 2026-08-18)

- A server is spawned detached in its own session, stdout and stderr to its log
  file, with `HF_TOKEN` exported when the `hf` token file exists (env, never argv).
  cria is free to exit at any moment; nothing about a running server depends on a
  cria process existing.
- Exit codes of detached servers are unobservable by design (they reparent away);
  the log tail is the crash evidence, never a collected exit status.

## State records

- One JSON record per running server at
  `~/.local/state/cria/servers/<entry-id>.json`, written at spawn: entry id,
  backend, repo (and quant), pid, the pid's own start time, port, host, the full
  composed command line, log path, launch timestamp.
- Records are **self-contained**: status and stop never need the config tree, so
  editing or deleting an entry never confuses its already-running server.
- An entry runs once at a time; its record is replaced on the next start.

## Liveness (settled 2026-08-18)

A record is **live** when its pid exists *and* the process identity matches the
record: the start time agrees exactly (a reused pid never impersonates a dead
server), and the command line agrees on its **arguments** rather than its
program path (amended 2026-08-18: a running process can rewrite its own argv[0]
without becoming another process — macOS re-execs framework Python through
Python.app moments after spawn, which every `mlx_lm.server` does, so a
whole-command comparison read healthy MLX servers as exited; a command with no
arguments falls back to the whole-string comparison). Otherwise the record is
**exited**.

- Exited records are kept — they are the crash report: the TUI and `cria status`
  show "exited" with the log tail one keypress away. An exited record disappears
  when its entry starts again or the user dismisses it. A deliberate stop removes
  the record on confirmed exit; only crashes leave one behind.

## Phases

`starting` → (`downloading` →) `running`, with `unhealthy` and `exited` as the
failure states.

- `starting` — spawned, port not answering yet.
- `downloading` — port not answering *and* the entry's model is not fully present
  in the cache. Progress renders as on-disk cache bytes versus Hub-API sizes
  (filesystem observation; when the Hub API is unreachable, bytes show without a
  total). The server does the fetching; cria only watches the cache.
- `running` — the backend's documented health signal is green: llama-server's
  `/health`; for a backend without a health endpoint, a successful response from
  a documented endpoint (`/v1/models`).
- `unhealthy` — process alive but the health signal is red or the port stopped
  answering.
- Observation is polling by nature (HTTP probes, `ps`); the TUI refreshes on a
  short interval, CLI status is a one-shot probe. Health probes go to loopback
  when the bind is `0.0.0.0`, to the bound address otherwise
  (`docs/specs/CONFIG.md`).

## Start

1. Validate the entry; refuse if its tool is missing (naming the tool and what its
   absence disables) or the entry is already running.
2. Check the port. Held by a live record → refuse: stop that entry first. Held by
   anything else → refuse with the holder's pid, command line and working
   directory (`lsof` + `ps`); the TUI offers the kill.
3. Compose the command (`docs/specs/CONFIG.md`), spawn detached, write the record.
4. `cria start <id>` returns once the record is written; `--wait` blocks until the
   phase settles (`running` → exit 0; `exited`/`unhealthy` → non-zero) — agent
   validation in one command.

## Stop

- SIGTERM, a short grace period, then SIGKILL; the record is removed once the
  process is confirmed gone. The TUI shows "stopping" in between; a kill keybind
  skips the grace.
- `cria stop` with no argument stops the only running server; with several
  running, the entry id is required.

## Logs

- One log file per launch: `~/.local/state/cria/logs/<entry-id>-<timestamp>.log`.
  At each launch, older logs of the same entry are pruned to the newest three —
  retention by count, no rotation machinery. Logs are displayed as a raw tail and
  never parsed (`docs/cria.md`, principle 6).

## Foreign servers (settled 2026-08-18)

- Any `llama-server` or `mlx_lm.server` process without a matching live record is
  foreign: shown with pid, command line and working directory, with an offered
  kill — the forgotten-terminal case. Detection is `ps` with explicit field
  selectors (`docs/TECH-STACK.md`); a busy port is attributed with `lsof`.

## `cria status`

- Human output mirrors the TUI status box: entry, backend, repo:quant, pid, port,
  phase, uptime, memory (RSS) and CPU, health, log path. `--json` emits the same
  facts as one JSON document — the machine contract for agents. Exits zero when at
  least one server is live, non-zero when none is.
