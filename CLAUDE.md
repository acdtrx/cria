# cria — Project Rules

> Entry point for any coding agent working on this project.
>
> **If `docs/<project>.md` exists, read it first** — the philosophy there binds design
> decisions; these rules assume it.
>
> **What lives where:** this file — working method and hard rails; `docs/CODING-RULES.md`
> — generic coding principles, always applicable; `docs/TECH-STACK.md` — the stack and
> the reasoning behind it; `docs/specs/<SUBSYSTEM>.md` — the contract docs, one per
> subsystem; `docs/plans/` — plans for large work; `docs/BACKLOG.md` — deferred items;
> `docs/reviews/` — audits.
>
> **Structure:** sections marked `[PROJECT]` are the project's own — filled in per
> project and edited as it grows. Everything else is stable methodology shared with the
> template this structure comes from: change it deliberately, not casually. When a
> methodology section is legitimately replaced by a project-specific version (e.g. a
> project with no test suite replaces Testing & Verification with its own verification
> model), the replacement is marked `[PROJECT]` too. The markers are the seam for
> template syncs: unmarked sections diff mechanically against the template — divergence
> there is either drift to fix or a lesson to backport; marked sections are expected to
> differ.

## Interacting with the user

Assume the user has technical knowledge. Use concise but clear responses. Prefer lists of
items over long prose paragraphs. Avoid terminology that is niche or trendy, prefer plain
english.

## Project Facts `[PROJECT]`

- Runtime: Go, latest stable toolchain; one module, one static binary, no cgo — see
  `docs/TECH-STACK.md`. Package manager: Go modules — no other build tooling.
- Dev: `go run .` · Tests: `go test ./...` · Lint: `gofmt -l .` + `go vet ./...`
- Hard constraints: single-host tool (one binary per machine, no remote management);
  daemonless — managed servers are detached processes that outlive the TUI; the
  Hugging Face cache is the single source of truth for models; the config tree
  (`~/.config/cria/`) is the interface — cria reads and drives it, creating files only
  through `cria new` scaffolding; server logs are never parsed for data.
- Do not add new dependencies without asking first. Every dependency enters at its latest
  stable version, verified maintained (`docs/CODING-RULES.md` §3).
- Full stack details and the reasoning behind them: `docs/TECH-STACK.md`.

## Triage

Classify every request before acting:

- **Small** — bug fix, rename, config tweak, isolated change (roughly <50 LOC, one
  subsystem): implement directly, then run the relevant tests. No plan files, no ceremony.
- **Large** — new feature, refactor, anything spanning multiple subsystems or sessions:
  follow Pre-Implementation Analysis and the Plans workflow below.

When unsure, ask — a one-line question is cheaper than a wrong plan or a sprawling "small" fix.

## Pre-Implementation Analysis (large work)

1. **Read first.** `docs/CODING-RULES.md` always; the matching `docs/specs/` entry and the
   relevant `docs/TECH-STACK.md` sections for the subsystem being touched; the relevant
   source.
2. **Validate the request.** Restate it, list ambiguities and assumptions surfaced by the
   codebase, and confirm with the user before planning.
3. **Implement following documented patterns.** Where the docs and the code disagree, trust
   the code and flag the discrepancy.

## Plans

For large work, create `docs/plans/<topic>/`:

- `OVERVIEW.md` — goal, scope, out-of-scope, constraints, risks, the step list grouped
  into **phases** (each phase names its goal and its steps: phase 1 = steps 1–4, …), and
  how the finished feature will be verified end to end.
- `STEP-N-<name>.md` — one file per step. Each states **intent, files likely touched,
  decisions made during planning, and acceptance criteria** — not implementation code.
  Each step must be independently verifiable.

Plans capture the *thinking* so a future session (or another agent) can pick up mid-stream.
After completing a step, record its status in the step file and commit before moving on.

Steps group into **phases** by goal. The full suite must be green at **phase ends**, not
after every step — forcing green mid-phase bends rework out of shape (code kept a step
longer only so a test passes, then deleted). Within a phase a step still runs the suite
and records the result in its step file: expected reds are named, each with the phase
step that clears them — never left silent. A phase ends committed and green, a
legitimate stopping point for the plan.

Plan steps are implemented by **Opus subagents only** (Agent tool, `model: opus`): the
main session prepares each step's brief, launches the subagent in the worktree, and
reviews its work against the step's acceptance criteria — it does not write the
implementation itself.

## Testing & Verification

- New behavior needs tests: component tests for logic, e2e for user-facing flows.
- A step is **done** only after the suite has actually been run and its result recorded —
  green, or the expected reds named with the step that clears them; the suite must be
  fully green at every **phase end** (see Plans), and a plan is done only after the full
  suite passes. Show the output. Never claim success without running something.
- Never skip, delete, or weaken a failing test to make the suite green. If a test seems
  wrong, say so and ask.

## Debugging

Find the root cause before fixing. No symptom patches, no speculative try/catch wrapping,
no `setTimeout` to mask races, no "this should fix it" without understanding why it broke.

- **Prove the failure mechanism before designing the fix.** Name the specific failure
  mode, then find the one-line check that proves it (a log tail while reproducing, a
  CLI probe, runtime state). If you can't articulate the check, you don't understand
  the bug yet — keep investigating. "Designing for both cases" usually means this step
  was skipped. When the user asks for manual validation, treat it as a real request —
  it usually catches a leap.
- **No stacked safety nets.** Fix the mechanism and trust events. One reconciler per
  legitimate concern is fine; layering a second polling/probing fallback for the same
  concern is clutter. Fall back to polling only when a real failure mode demands it —
  and ask first.

## Scope

- Don't silently fix or refactor unrelated code mid-session: either it's in scope, or it
  goes to `docs/BACKLOG.md` as a structured entry. The user can also ask to "add this to
  the backlog" directly. Entries that grow graduate to `docs/plans/<topic>/`.
- YAGNI: build what the step requires, nothing speculative.
- **Features earn their place.** Brainstorm freely, build reluctantly: unless there is a
  strong reason to believe a feature helps, find a confined way to test its usefulness
  before building it in. Both the user and the agent hold this line — and the agent should
  invoke this rule out loud when feature imagination runs ahead (the user asked to be
  reminded).
- Match the existing patterns of the file being edited over personal preference.

## Documentation

Document **decisions and contracts, not implementations**. If a doc would merely restate
what the code already says, don't write it — the code is the source of truth and mirrors
go stale.

**Decisions live where their topic lives — inline, dated.** A settled decision is recorded
in the doc that owns it: subsystem behavior in that subsystem's `docs/specs/` file, stack
choices in `docs/TECH-STACK.md`, methodology in this file — tagged `(settled YYYY-MM-DD)`,
with the reasoning, and what was rejected when that matters. There is no separate decision
log: one home per decision, no duplicates — a decision that was merely *discussed* and
already lives in its owning doc is not recorded again anywhere else.

**Comments hold current agreements only.** A comment states the constraint, invariant or
reasoning that is true *now* — never the code's own history: no "since <date>", no
"used to be", no narration of previous iterations. Git is the archive. Naming a rejected
alternative is allowed only when it documents a live constraint (why the obvious
approach fails), not what this code did before. When touching code, bring any
history-narrating comment you meet up to this rule.

- `docs/specs/<SUBSYSTEM>.md` — one per subsystem, **created during the design/planning
  of the feature that first settles that subsystem's behavior** — not extracted later
  from code. It holds the subsystem's purpose, its principles, its genuine contracts
  (external API shapes, invariants, protocol behavior) and its settled decisions, dated.
  Update it **in the same edit** that changes a contract or settles a direction;
  implementation changes that alter no agreement require no doc edit.
- `docs/ARCHITECTURE.md` — module boundaries, data flow, deployment shape, with diagrams
  showing the connections between subsystems and the flow of information. Update when
  the shape changes.
- `docs/BACKLOG.md` — deferred bugs and ideas (see Scope). Each entry names its
  **revisit trigger** — the observed condition that would make it worth building. An
  entry that gets implemented or otherwise resolved is **removed in the same change** —
  git history is the archive; the file lists only what is still open.
- `docs/reviews/<YYYY-MM-DD>/AUDIT.md` — code/security audits; add `IMPLEMENTATION.md`
  when an audit yields substantial follow-up work, grouped by severity, each item with
  file, line, and fix sketch.

## Git

- Conventional-ish commits, one logical change per commit.
- **Development happens in a git worktree under `.claude/worktrees/`** (e.g.
  `.claude/worktrees/<branch>`, gitignored), one per build — the harness-blessed
  location, no permission prompts. The main checkout stays on `main`, clean, for
  discussion, assessment, planning and review. Worktrees stay disposable by discipline:
  commit at step boundaries — a worktree must never hold anything worth losing. Run
  `git worktree prune` after deleting one.
- Large work (plans) gets a branch; the branch **rebases onto main before the ff
  merge**, so main never freezes — small unrelated work keeps landing on main while a
  plan is in flight. Same-file collisions with an active step are the one case to wait
  out.
- Small work gets done on main directly.
- Commit at step boundaries; the suite is green at phase boundaries (see Plans).
- Never push, force-push, or rewrite history without being asked.
- **Tags are anchors.** Annotated semver tags on main (`0.5.1`), tagged after notable
  merges and **always immediately before a large plan's implementation begins** — the
  tag names the world the plan started from, for diffing and for bailing out. The
  message says what the anchor holds in one line. Tags stay local for now — the project
  has no remote; decide tag routing when one is added. `[PROJECT]`

## Deployability `[PROJECT]`

- cria ships as **one static binary per host**. "Deploy" is `go build` on the dev Mac
  and `scp` to the target — Go ad-hoc-signs darwin/arm64 binaries and scp sets no
  quarantine xattr, so the copy just runs. No installers, no services, no packaging
  until someone outside the household asks for a binary (`docs/BACKLOG.md`).
- cria writes to exactly two places: config in `~/.config/cria/` (human/agent-edited,
  scaffolded by `cria new`) and runtime state in `~/.local/state/cria/` (pidfiles,
  server state records, server logs). The Hugging Face cache is **managed, not owned** —
  cria mutates it only through deliberate cache operations, never as a side effect.
- The host provides the tools (`llama-server`, `mlx_lm.server`, `hf`); cria detects and
  reports what is missing, and never installs anything.

## Feature-Building Mode (No Backwards Compatibility) `[PROJECT]`

Until this project ships to real users:

- No automatic upgrade paths between intentional versions, no dual-read of deprecated
  schemas, no legacy-cleanup code — **not even for artifacts produced earlier in the
  same session**. New behavior targets the current schema only; when replacing an
  approach leaves stale state behind, state the manual cleanup clearly (one `rm`, one
  restart) instead of encoding it.
- Fixing state produced by past bugs is normal engineering, not "migrations."
- There is no database. The shapes that can break are the config tree and the runtime
  state records; both are validated loudly on read (unknown keys and wrong types are
  errors, never silent defaults), and a breaking change states its manual fix — edit
  this key, delete that state file — instead of encoding an upgrade path.

Replace this section with a migration policy when the project graduates.

## Project-Specific Rules `[PROJECT]`

> Rules unique to this project as they emerge — domain naming conventions, architectural
> boundaries (e.g. "only `lib/db/` may import the database driver"), required error shapes.
> Generic principles belong in `docs/CODING-RULES.md`, not here.

- **Exec is the product** (CODING-RULES §7, applied): cria's job is driving external
  binaries — `hf`, `llama-server`, `mlx_lm.server`. Shelling out to *these managed
  tools* is the design and needs no per-case justification; any other CLI dependency
  still meets the §7 bar (prefer native/API access, confirm first).
- **Never parse server logs for data** (settled 2026-08-18). llama-runner, this
  project's predecessor, mined llama-server's log stream for live stats and broke on
  every llama.cpp release. Logs are displayed raw (tail); information comes from
  documented HTTP endpoints and the filesystem only.
- **cria reads the config tree, it never edits it.** Humans and coding agents own those
  files — comments and formatting included; cria's only writes there are new files
  scaffolded by `cria new` from embedded templates.
- **Schema and docs are one source.** `cria docs` prints the config schema for coding
  agents; it is generated from the same definitions the parser uses, so a schema change
  updates the docs in the same edit by construction.
