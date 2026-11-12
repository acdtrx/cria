# cria — Project Rules

> Entry point for any coding agent working on this project. One home per topic.
>
> **The rules — read them before any other reading or writing in the project:** this file
> — how we work (working method and hard rails); `docs/cria.md` — why the project
> exists, and the philosophy that binds its design; `docs/CODING-RULES.md` — how code is
> written, tested and debugged (generic principles plus the project's language rules,
> §12); `docs/TECH-STACK.md` — what it is built with, and why; `docs/ARCHITECTURE.md` —
> the system's shape (boundaries, data flow, where state lives, how it runs).
>
> **Read as the work needs them:** `docs/specs/<SUBSYSTEM>.md` — the contract docs, one
> per subsystem; `docs/features/<feature>.md` — work being built; `docs/BACKLOG.md` —
> deferred items; `docs/reviews/` — audits; `docs/BACKENDS.md` — how to install each
> server cria drives.
>
> **Structure:** sections marked `[PROJECT]` are the project's own, filled in per project
> and edited as it grows. Everything else is stable methodology shared with the template
> this structure comes from: change it deliberately. When the project legitimately
> replaces a methodology section with its own version, mark the replacement `[PROJECT]`
> too.

## Interacting with the user

Assume the user has technical knowledge. Answer concisely but clearly. Prefer lists over
long prose. Use plain English, not niche or trendy terminology.

## Project Facts `[PROJECT]`

- Runtime: Go, latest stable toolchain; one module, one static binary, no cgo — see
  `docs/TECH-STACK.md`. Package manager: Go modules; add no other build tooling.
- Commands — add each when it is first implemented; keep this line current. Dev
  `go run .` · Tests `go test ./...` · Lint `gofmt -l .` + `go vet ./...`
- Hard constraints: a single-host tool (one binary per machine, no remote management);
  daemonless — managed servers are detached processes that outlive the TUI; the Hugging
  Face cache is the single source of truth for models; the config tree
  (`~/.config/cria/`) is the interface — cria reads and drives it, and writes there only
  the root, `models/` and `AGENTS.md` when missing, and the create-only entry files
  `cria new` scaffolds; server logs are never parsed for data.

## Small work

A bug fix, rename, config tweak or isolated change (roughly <50 lines, one subsystem): do
it, then run the relevant tests. No feature, no spec change. When unsure whether work is
small, ask.

## Everything else: request → features → build

1. **Request → features.** Discuss a request with the user until its features, and the
   spec changes they need, are written and the user agrees to them. Split very large
   work, a project's first build above all, into a set of features, built in the order
   agreed with the user.
2. **One builder.** From the session that agreed the features, start one subagent, in a
   context of its own, for all of them: name the features to build, in order, and the
   specs to keep in mind. The builder reads this file and follows it. Without subagents,
   build in your own session.
3. **Open points first.** Builder: read the features, their specs and the code they
   touch. Send what they leave open, or where the code contradicts them, back to the
   design session first, each point as its own decision with a recommendation. Design
   session: settle them with the user and pass the answers on. Builder: then build,
   milestone by milestone.
4. **A milestone is done** when each of its acceptance criteria has a test and the full
   suite, e2e included, passes. Builder: then stop and report. Design session: have it
   reviewed, then continue the same builder with the findings and the next milestone.

## Review

Have any work beyond small work reviewed independently by a subagent. Give it the change
and the feature or spec behind it, not your reasoning, to check against them and the
rules (this file's header). It reads whatever code it needs to follow what the change
touches, and reports only on the change and what it affects: the whole app is an audit's
job (`docs/reviews/`). Without subagents, ask the user to run the review in a fresh
session, given the same. Fix or backlog each finding.

## Scope

- Never silently fix or refactor unrelated code mid-session: if it isn't in scope, add it
  to `docs/BACKLOG.md` as a structured entry. The user can also ask to "add this to the
  backlog" directly.
- YAGNI: build what the task requires, nothing speculative.

## Documentation

Document **decisions and contracts, not implementations**: the code is the source of
truth, and a doc that mirrors it goes stale.

**The spec is the user's.** `docs/cria.md`, `docs/specs/` and `docs/features/` record
what was agreed with the user. Never edit them to match what was built: fix a divergence
in the code or raise it with the user, and change a spec only once the user agrees.

**Decisions live where their topic lives — inline, dated.** Record a settled decision in
the doc that owns its topic (see the header), tagged `(settled YYYY-MM-DD)`, with its
reasoning, and what was rejected when that matters. One home per decision; no separate
decision log. Record only decisions that settle a spec or a change: an idea turned down
while brainstorming or speccing goes nowhere, unless the user asks for a backlog entry.
What a code comment may say: `docs/CODING-RULES.md` §10.

- `docs/specs/<SUBSYSTEM>.md` — one per subsystem, **written with the first feature that
  settles its behavior**, never extracted later from code. It holds the subsystem's
  purpose, its principles, its genuine contracts (external API shapes, invariants,
  protocol behavior) and its settled decisions. An implementation change that alters no
  agreement needs no doc edit.
- `docs/features/<feature>.md` — one per piece of work bigger than a small change: what
  it is for, which specs it adds or changes, and its **milestones** in order. A milestone
  is the smallest piece of the work after which something works end to end. Give each its
  acceptance criteria: observable, checked by running something, never a quality
  judgement. A feature that fits in one go has one milestone. Never put a spec's content
  in a feature: contracts live in the specs. Remove the feature once its last milestone
  is reviewed, like a backlog entry.
- `docs/ARCHITECTURE.md` — update it in the change that alters the shape.
- `docs/BACKLOG.md` — deferred bugs and ideas (see Scope). Remove a resolved entry **in
  the same change**: git history is the archive.
- `docs/reviews/<YYYY-MM-DD>/AUDIT.md` — code/security audits; add `IMPLEMENTATION.md`
  when an audit yields substantial follow-up work, grouped by severity, each item with
  file, line, and fix sketch.

## Git

- Use git as you go, with the usual conventions: conventional-ish commit messages, one
  logical change per commit, each committed as it works, not one commit at the end.
- Build features on a branch, and **rebase it onto main before the ff merge**, so main
  never freezes: small unrelated work keeps landing on main while feature work is in
  flight. Wait out only a same-file collision with the branch.
- Never push, force-push, or rewrite main's history without being asked.
- **Tags are anchors.** Tag main with an annotated semver tag (`0.5.1`) after notable
  merges and **always immediately before work on features begins**: the tag names the
  world the work started from, for diffing and for bailing out. Say in the message, in
  one line, what the anchor holds. Push routine work, main and anchor tags, to **`cala`,
  the default remote**; push to GitHub only on demand and for releases. A tag chooses to
  ship by its prefix: a **v-prefixed** tag (`v0.2.2`) pushed to GitHub builds a release,
  a bare tag (`0.2.3`) is an anchor only (settled 2026-08-18). `[PROJECT]`

## Deployability

- Land fixes for deployment or setup issues in the codebase or `scripts/`, never as notes
  about manual steps.
- cria ships as **one static binary per host**, with no installer and no service.
  `[PROJECT]`
  - In the household, deploy is `go build` on the dev Mac and `scp` to the target: Go
    ad-hoc-signs darwin/arm64 binaries and scp sets no quarantine xattr, so the copy just
    runs.
  - For everyone else (settled 2026-08-18): pushing a v-prefixed tag to GitHub builds a
    release (darwin/arm64, linux/amd64, linux/arm64), and `install.sh` fetches the latest
    into `~/.local/bin` (`CRIA_BIN_DIR` overrides it). curl sets no quarantine xattr
    either, so no notarization is needed.
  - `cria update` replaces the running binary with the latest release
    (`docs/specs/CLI.md`).
