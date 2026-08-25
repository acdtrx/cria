# STEP 4 — degradations, refusals, and the override

Status: done (2026-08-25) — phase 2 ends here

## Intent

Every path that is not the happy swap says exactly what it can and cannot
know, and the one override exists: the busy gate can be skipped deliberately.

## Files likely touched

- `internal/cli/validate.go`, `validate_test.go`
- `internal/cli/help.go`

## Decisions made during planning

- Busy holder → exit 2: the reason line tells the agent to have the user
  clear or stop the server — validate never queues, never waits for idle
  (a wait would burn the agent's clock invisibly; refusal is honest).
- Unverifiable busy signal (mlx holder; llama without `/slots`) → **warning +
  proceed** with the swap: the warning names the false-negative risk (the
  holder may be mid-generation and its client will see the connection die).
  Proceeding, not refusing, is deliberate: the machine that needs validate
  most runs one server on one port, usually the agent's own, idle at the
  moment the tool call executes.
- `--ignore-busy` skips even a positive busy verdict — the operator's word
  that the generation may die. Exact flag name settled at implementation
  against existing flag idioms in the CLI (there are few; `--wait` is the
  precedent for spelling).
- Foreign port holder → exit 2 with pid + command line (start's refusal
  wording, shared where practical).
- Unknown entry / choice / option → exit 2, reusing start's refusal text
  (which already names the valid ids/choices/options).
- Restore-failure reporting (exit 3) names: what failed, and what is serving
  now (nothing, or the target left running when its stop failed) — the state
  a human must fix, in one line.
- **The agent-facing surfaces teach validate as THE verb** (user-requested
  2026-08-25): `internal/config/docs.go`'s footer and the embedded
  `internal/config/agents.md` ("Validate what you wrote") currently teach the
  start-wait/status/stop choreography — the exact dance that burns the
  calling agent's own model. Both switch to `cria validate <id>
  [choice=option ...]` with the exit-code contract, keeping start/stop
  documented as the manual verbs they remain. The user's existing
  `~/.config/cria/AGENTS.md` is only written when missing — the manual
  refresh (delete it, run cria once, or copy the new text) is stated in the
  step-5 live-proof checklist, per feature-building mode.

## Acceptance criteria

- Tests per refusal/degradation path asserting exit code, the reason line,
  and that refused paths touched nothing.
- The unverifiable-signal warning appears for an mlx holder and for a llama
  holder whose `/slots` probe fails, and the swap proceeds.
- `--ignore-busy` (or its settled spelling) proceeds past a busy verdict.
- Phase 2 ends here: full suite green, committed.

## What was built

**The flag is `--ignore-busy`** (settled at implementation). It is spelled for
the one gate it lifts, because that is the whole of what it does: a caller who
meets it in a shell history or a script must be able to tell that a busy holder
is what it overrides, and not the two refusals cria cannot honour. `--force`
was rejected for promising exactly that (it would read as lifting the foreign
holder and the wrong-port refusal too), and `--now` for reading like "do not
wait" against a command whose every stage blocks. The hyphen is new among the
flags — all of which were natural single words — but not on the surface, where
`wired-limit` already carries one. Parsed with `splitFlag`, so it may come
before or after the id like `--wait`; the flag column in the help page's FLAGS
block widened by three to fit it.

Where it applies: `validate` → `displacementRefusal(…, ignoreBusy)` →
`busyGate(manager, holder, ignoreBusy)`. The gate still *asks* — the verdict is
read and reported as a warning ("gemma is answering a request on port 8080 right
now; --ignore-busy was given, so cria stops it mid-answer") rather than skipped
unasked. One 2s-bounded GET buys an output that never hides a fact cria knows,
and the operator hears what their override cost. The foreign-holder and
already-running-elsewhere refusals are untouched by it.

**The refusal does not name the flag.** The busy line points at a human action
("ask the user to let it finish or to stop gemma"), and stops there: the agent
reading it is the one whose own request would be cut off, and a bypass printed
on the line it reads is a bypass it will take. `--ignore-busy` is documented in
`cria --help` FLAGS, where the person deciding reads — not in `cria docs` or
`AGENTS.md`, which are the agent's pages.

The wording set, reviewed as a whole. Every one of these is the last line of its
exit, under the `cria validate <id>: ` prefix `failWith` puts on it:

    busy → 2
      gemma is answering a request on port 8080 right now, and stopping it would
      cut that answer off; ask the user to let it finish or to stop gemma, then
      validate again

    unverifiable → warning on stderr, swap proceeds
      note: cria cannot tell whether gemma is generating right now (<what stood
      in the way>); validating stops it anyway, so a request in flight would die
      with it

    busy under --ignore-busy → warning on stderr, swap proceeds
      note: gemma is answering a request on port 8080 right now; --ignore-busy
      was given, so cria stops it mid-answer

    target already running on another port → 2
      qwen is already running as pid 4242 on port 9999, which is not the port it
      launches on now (8080); validating would leave that process with nothing
      naming it — `cria stop qwen` first

    foreign holder → 2
      foreignRefusal, start's own text: the pids, their command lines and working
      directories, then the fix

    holder would not stop → 3
      cannot stop gemma on port 8080: <err>; nothing was validated, and gemma may
      already be down — `cria status` says whether it is still serving

    target would not stop → 3
      started it but cannot stop it again: <err>; qwen still holds port 8080 and
      gemma was not put back; `cria stop qwen`, then `cria start gemma`

    restore failed → 3
      cannot put gemma back on port 8080: <err>; nothing is serving on port 8080
      now — `cria start gemma` once that is fixed

Which facts each line names is decided by what the reader does next: the id and
the port everywhere, the pid where the handle is a process rather than an entry
(the foreign holders, the target running elsewhere), and the command that ends
the state cria left behind on every exit 3.

Two shared texts changed their ending, because validate prints them too and both
told the caller to "start again" when the command they ran was `validate`:
`unknownEntry`'s broken-file branch and `foreignRefusal` now end "and try again"
/ "fix that file and try again" — command-neutral and correct for both callers.

**Agent-facing surfaces now teach validate as the verb** (the user's request):

- `internal/config/docs.go` — the VALIDATE WHAT YOU WROTE footer is the one
  command, what it does to the machine, and the four exit codes; start/status/
  stop follow as "the manual verbs … for a server you want left running". The
  schema half of the page is untouched.
- `internal/config/agents.md` — the same, in that page's shorter voice.
- `internal/cli/help.go` — EXIT CODES gained validate's four codes as a second
  paragraph under the existing line (extended, not restructured); FLAGS gained
  `--ignore-busy`; PICKS notes that validate takes the same picks; the agents
  block in CONFIG now names `cria validate <id> [choice=option ...]`.

## Suite

`go test -count=1 ./...` from the worktree root: **all packages ok** (cria, cli,
config, format, hubapi, hubcache, picks, procs, selfupdate, serve, tools, tui).
`gofmt -l .` empty, `go vet ./...` clean.

New and changed tests: `TestIgnoreBusyStopsAHolderMidAnswer` (both flag
positions: the busy holder is stopped anyway, gemma goes back, the warning names
the flag, exit 0) · `TestIgnoreBusyLiftsTheBusyGateAlone` (foreign holder and
target-running-elsewhere still exit 2 with nothing stopped or started) ·
`TestValidateWarnsWhenTheHolderCannotBeAsked` is now a table over the two
origins of an unverifiable signal — an mlx holder and a llama holder whose
`/slots` refused — both warning and proceeding (which server cannot be asked is
proved in `serve`: `TestGeneratingNeverAsksAnMLXServer`,
`TestGeneratingCannotVerifyWhatItCannotRead`) · the refusal table's busy and
wrong-port rows assert the new wordings · the three exit-3 tests assert the
added next-action clauses · `help_test.go` asserts `--ignore-busy` and both
exit-code lines · `scaffold_test.go` and a new `TestDocsTeachesValidateAsTheCheck`
assert that AGENTS.md and `cria docs` teach the verb and its four codes.

Two mutations were checked and each failed its test: `--ignore-busy` also
lifting the foreign refusal, and `busyGate` ignoring the flag.
