# Step 10 — CLI lifecycle commands

**Phase 3 · Status: done (2026-08-18) — phase 3 ends green; first real e2e
passed.** Suite green, all gates pass. E2E on the dev Mac (llama-server
b10450, cached LFM2.5-2.6B Q8_0, port 18080): start --wait green in 2s with
the composed command shown; status human + --json sane (JSON is a deliberate
projection — identity strings excluded, no omitempty, arrays never null);
same-port second entry refused naming the holder; no-arg stop single-server
case; foreign drill refused with pid/command/cwd and recovered after the kill;
status with nothing running exits 1; host left clean (entries removed, no
records, three pruned launch logs remain by design). Decisions: --wait budgets
2min cached / 30min downloading, monotonic once downloading is seen, 2s poll,
30s plain progress lines; serve gained PortUse (attribution the TUI reuses for
its kill offer) and exported LaunchTool (gate order: tool before port); no
`name` in JSON — records stay self-contained, status never reads the config
tree. Deferred to step 15: the several-servers no-arg-stop case on real
processes (two small models).

## Intent

`internal/cli`: real `start` (validation, tool gate, port check with holder
attribution, spawn, `--wait`), `stop` (no-arg single-server form), `status`
(human + `--json`, exit codes) — the whole CLI.md surface over the phase-3
layers.

## Files likely touched

`internal/cli/`, `main.go`.

## Decisions made during planning

- Port check order: live-record holder → "stop <entry> first"; else lsof
  attribution → foreign holder details; else spawn (SERVE.md). The CLI only
  reports foreign holders — kill is a TUI offer.
- `--wait` polls the phase snapshot until `running` (exit 0) or
  `exited`/`unhealthy` (exit 1, log path printed); interval and timeout named
  constants.
- `status --json`: one document, fields mirroring the snapshot struct; humans
  and machines read the same facts (CLI.md).
- Errors name the failing thing and its fix — the strings are part of review.

## Acceptance criteria

- Component tests for argument handling, exit codes, and the port-refusal
  branches (fake procs/serve).
- **Phase 3 ends here — the first real e2e**: on the dev Mac, with a small real
  model: `start --wait` → green, `status --json` sane, second entry on the same
  port refused with the right message, `stop`, foreign drill (hand-started
  llama-server detected and attributed). Transcript recorded here; suite green,
  committed.
