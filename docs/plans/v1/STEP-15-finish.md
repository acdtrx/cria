# Step 15 — end-to-end validation and finish

**Phase 6 · Status: done (2026-08-18)** — the checklist below ran on the dev Mac
against a real build of the branch; all gates green (265 test functions);
`docs/ARCHITECTURE.md` written, README aligned, version constant `0.1.0`.
Not committed and not tagged — left for review.

One real defect surfaced, and it was the first live MLX serve that surfaced it:
**every `mlx_lm.server` read `exited` seconds after starting** while it was in
fact serving. Mechanism proven before fixing (`ps` sampled every 50 ms from
spawn): macOS re-execs framework Python through
`Python.app/Contents/MacOS/Python` within ~50 ms, so the argv[0] cria recorded at
spawn (`~/.local/share/uv/tools/mlx-lm/bin/python …`) never matches the argv[0]
`ps` reports afterwards — same pid, same start time, rewritten program path.
`procs.Identity.SameProcess` compared the whole command string, so liveness was
permanently false: `status` lied, `stop` could not work, `--wait` reported a crash
for a healthy server. Fixed by judging identity on the start time and the
command's *arguments* rather than its program path, with a fallback to the whole
command when there are no arguments. Verified live against the record the old
binary had already written.

**This changes a contract sentence in `docs/specs/SERVE.md`** ("command line and
process start time agree") — that edit was outside this step's allowed doc set and
is left for the reviewing session.

## Checklist, as run

**1 · First-run scaffold on a clean home.** `HOME=$(mktemp -d) cria docs | head -5`
→ exit 0, 147 lines of schema; the temp home gained exactly
`.config/cria/AGENTS.md` and `.config/cria/models/`. Temp home deleted.

**2 · The agent loop.** Read `cria docs`, wrote
`~/.config/cria/models/lfm-final.toml` from it (llama, `LiquidAI/LFM2.5-2.6B-GGUF`,
`Q8_0`, port 18080, `--ctx-size 4096`). `cria start lfm-final --wait` → *running
after 2s*, `/health` 200 OK, exit 0. `cria status --json` piped into a python
assertion (`phase == running`, `health.green`, `pid > 0`, `port == 18080`,
`broken == []`) → OK, exit 0. `cria stop lfm-final` → exit 0; status then exits 1
with "no servers"; port free.

**3 · The swap workflow.** Second entry `qwen-final.toml`,
`unsloth/Qwen3.8-27B-GGUF:UD-Q2_K_XL`, **same port 18080**. `start lfm-final
--wait` green → `start qwen-final` refused: *port 18080 is already serving
lfm-final (pid 12366); stop lfm-final first* (exit 1). `cria stop` (no argument)
stopped the only server. `start qwen-final --wait` → running after 4s on 18080;
`GET http://127.0.0.1:18080/v1/models` answered
`unsloth/Qwen3.8-27B-GGUF:UD-Q2_K_XL` — the consuming endpoint never changed, only
the model behind it. Stopped.

**4 · MLX and the downloading phase.** Repo chosen against the Hub tree API first:
`mlx-community/Qwen2.5-0.5B-Instruct-4bit`, 289,601,064 bytes (276.2 MiB), not in
the cache. `mlx-final.toml` on port 18081.
- First run exposed the liveness defect above; fixed, rebuilt, and the
  already-running server then read correctly (`running`, `/v1/models` 200 OK, up
  2m58s) from the record the previous binary wrote.
- Repo deleted through cria's own surgery, the download re-run from nothing:
  `--wait` printed `downloading 0 B of 276.2 MiB (0%)` — the Hub total matches the
  tree API byte for byte — then *running after 2s*, `/v1/models` 200 OK, exit 0.
  `status --json` sane. A real completion request returned `'Hi there!'`.
- **Observed limitation, not a contract break:** `mlx_lm.server` binds and answers
  `/v1/models` *before* it fetches the model, so the downloading phase is visible
  only in the moment before the port opens. cria applies the SERVE.md rule
  faithfully (port answering ⇒ not downloading); the phase is fully visible on the
  llama side, where `-hf` fetches before binding. Worth a BACKLOG note if MLX
  first-starts become common.
- Deletion drill through cria's surgery (a throwaway `package main` over
  `hubcache.PlanRepo`/`Execute`, deleted afterwards): plan printed 20 removals +
  6 directories, guard refused while the server was up
  (*mlx-community/Qwen2.5-0.5B-Instruct-4bit cannot be deleted: mlx-final is
  serving it; stop it first*), then after the stop reclaimed **276.2 MiB
  (289,600,212 bytes)**. `du -sk` on the whole cache: 163,783,836 → **163,501,000**,
  exactly the pre-drill baseline.

**5 · Two servers at once.** `qwen-alt.toml` (same repo:quant, port 18082) started
alongside `lfm-final` (18080); both `--wait` green, `cria status` listed two live
servers with distinct pids, ports, RSS and health URLs. `cria stop` with no
argument refused: *2 servers are running (lfm-final, qwen-alt); name the one to
stop* (exit 1). `stop lfm-final`, then no-argument `stop` took the remaining one
(the single-server path, a superset of the planned `stop qwen-alt`).

**6 · Foreign drill.** Hand-started `llama-server` on 18080 outside cria;
`cria start lfm-final` refused with the attribution: *port 18080 is held by a
process cria did not start: pid 14271 /opt/homebrew/bin/llama-server -hf … ·
working directory /private/tmp*, plus the two fixes (stop it, or give the entry its
own port, naming the file). Killed by hand; port free.

**7 · TUI tour** (real pty, 120×40, keys driven and the frame re-rendered from the
escape stream). Serve view on open: three entries with cached dots, detail pane
with the composed command line and *cached yes*. ⏎ → status box went
*lfm-final running llama … pid 14553 :18080 up 2s 2.9 GiB 1.8% cpu 200 OK*, footer
*started lfm-final (pid 14553)*. `s` → *stopped · started last; nothing is running
now*. `v` → cache view: **155.9 GiB, 11 repos, ⚠ 868.9 MiB unfinished**, the nested
quant tree with verbatim tags, details showing *used by entry lfm-final* and
*serving nothing right now*. `t` → tools pane: llama-server (build 10450, hub cache
ok), mlx_lm.server, hf — all green. `t` again, `q` — clean exit, nothing left
running.

**8 · Gates.** `gofmt -l .` empty · `go vet ./...` clean · `go test ./... -count=1`
all nine packages ok · `GOOS=linux GOARCH=amd64 go build ./...` ok ·
`go test ./... -count=1 -short` ok. Re-run after the version bump and the doc
edits: still green.

**9 · Cleanup.** All four drill entries removed; no state records; no
`llama-server`/`mlx_lm.server` processes; 18080/18081/18082 free; the drill repo
gone; the built binary, the temp homes and every scratch script deleted. Log
retention confirmed in passing: `lfm-final` was launched four times and three logs
survived. The HF cache ends at **163,501,000 KB — byte-identical to the baseline**;
its two extra directory entries are symlinks `llama-server` added into an existing
repo's current snapshot when it served `UD-Q2_K_XL` (pointing at blobs already
present, zero bytes, its own bookkeeping in a model the user keeps).

## Intent

Prove v1 as a whole on the dev Mac, write `docs/ARCHITECTURE.md`, align README,
tag.

## Files likely touched

`docs/ARCHITECTURE.md` (new), `README.md`, this file (results).

Actually touched, beyond those: `main.go` (version constant), and
`internal/procs/procs.go` + `internal/procs/ps_test.go` for the liveness defect
above.

## Decisions made during planning

- The e2e checklist is the OVERVIEW's verification list, run in one sitting and
  transcribed here: both backends served, the one-port swap workflow
  (stop-then-start, agent endpoint unchanged), `status --json` consumed by a
  scripted check, real deletion vs `du`, foreign drill, full TUI tour, first-run
  scaffold on a clean home, `cria docs` → agent-written entry → `start --wait`
  loop. Added from step 10: two small models resident at once on different
  ports, exercising the several-servers no-arg `cria stop` refusal on real
  records.
- `ARCHITECTURE.md` documents the package boundaries and data flow as built
  (with the diagram CLAUDE.md asks for) — written now, at the end, when the
  shape is real.
- Lint/vet/test/cross-compile gates from the OVERVIEW all green.
- Merge: branch rebases onto main, ff merge, `git worktree prune`; annotated
  tag `0.1.0` on main.

## Acceptance criteria

- Checklist transcribed here with outcomes; all gates green; plan marked done.
