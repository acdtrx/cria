# Coding Rules

Generic principles for how code is written, tested and debugged, for any contributor
(human or coding agent) working on this codebase. They are project-independent, except §12
(`[PROJECT]`), the project's language rules.

Specialize a rule for the project by weaving its concrete instance inline ("Applied: …")
under the matching principle: the principle stays general, the application names the
project's case. Give a project rule that no principle covers a bullet of its own in the
section it belongs to. Section numbers are stable: `CODING-RULES §N` comments in the
source refer to them.

---

## 1. Naming and Semantics

- Name functions, files and modules for their **purpose and domain**, not for the
  mechanism or API they call.
- Never write a generic action dispatcher (e.g. `performAction(name, action)`): give each
  operation its own purpose-named function.
- Avoid vague names like `doAction`, `update`, `handle`. Be specific:
  `executeDiskOperation`, `updateField`, `validateInput`.

## 2. Code Sharing — by Purpose, Not by Shape

- Share code only between call sites with the same purpose; keep similar-looking code
  with different purposes separate.
- Split a function whose parameters collect behaviour-changing booleans, rather than add
  another flag.
- Keep a shared helper in one module; never copy it between files.

## 3. Minimal External Dependencies

- Prefer the platform and the framework: don't add a library for what they provide or a
  small function does, and don't rebuild what they already offer.
- Leave adding a dependency to the user: propose it with what it saves. Once agreed, add
  it at its latest stable version, actively maintained, with a line in `TECH-STACK.md`'s
  inventory.
- Keep dependencies current; for a blocked upgrade, record why and when to recheck.

## 4. Structured Data Parsing

- Never parse structured formats (XML, JSON, YAML, HTML, etc.) with regular expressions.
  Use a dedicated parser.
- When modifying a parsed document, extract the existing element and mutate it rather
  than construct a replacement from scratch.
- Read each value from where its format puts it, without generic discovery. When it isn't
  there, fail visibly rather than guess a plausible value.
  Applied: take what a server reports only from its documented HTTP endpoints and the
  filesystem; tail its log raw and never parse it for data (`docs/cria.md`, principle 6).
- Read config strictly: reject unknown keys and wrong types at startup, so a typo fails,
  never silently defaults.
  Applied: cria never edits the config tree (`docs/cria.md`, principle 4), so a change of
  its shape fails loudly on a file of the old shape and names the edit.

## 5. Error Handling

- Make errors **structured**: at minimum a stable `code` and a human-readable `message`.
  The language idiom that carries them (thrown objects, returned error values) is in §12.
- Give API failure responses one shape across the application, unless an external API
  contract dictates another: `{ error, detail? }`, `error` the stable code, always
  present, and `detail` the human-readable message, where it adds value. Use the same
  shape on HTTP and streaming channels.
- Prefer inspecting state up front and choosing the right path once, rather than
  try/catch/retry as control flow.
- Comment every silent `catch {}` block with why the error is intentionally swallowed.

## 6. Async Patterns and Timing

- Never use `sleep` or timer delays to work around race conditions. Use event signals, or
  retry with exponential backoff.
- Timers used for **scheduling** (periodic push updates, reconnect backoff, TTL cleanup,
  cron-like checks) are allowed.
- Use `AbortController` (or the platform's cancellation primitive) for cancellable
  operations instead of boolean flags.
- Give every outbound request and subprocess a timeout; nothing waits forever.
- Prefer streaming over buffering: pipe data through transform streams instead of reading
  an entire payload into memory and then writing it out.

## 7. Architecture Boundaries

- Reach an external system (a database, an API, a daemon) only through the subsystem that
  owns it: no other module imports its client library or opens a connection. Enforce it
  with a lint; meet a new need with one added line, flagged for review, never a way
  around the fence.
- Prefer native/API access over shelling out to CLI tools. Exec a binary only when there
  is no practical programmatic alternative, and confirm first.
  Applied: driving the managed tools (`hf`, `llama-server`, `mlx_lm.server`, `vllm`) is
  cria's job, so exec them without a case-by-case confirmation; `ps` and `lsof` are
  confirmed (`docs/TECH-STACK.md`, External tools). Any other CLI still meets this bar.

## 8. Frontend

In projects with a web UI: `FRONTEND-RULES.md`.

## 9. Security

- Validate and sanitise all user input at the **API boundary** with the one
  schema-validation mechanism named in `TECH-STACK.md`, not ad-hoc checks. Internal
  library functions may assume valid input.
- Pass secrets to subprocesses via stdin or environment variables, never as command-line
  arguments (visible in process lists).
- Sanitise subprocess error output before exposing it to clients (e.g. mask credentials).
- Block requests to private/loopback addresses when the URL originates from user input
  (SSRF).
- Never put auth tokens in URLs (`?token=...`): they get logged. Use cookies or
  `Authorization` headers.
- Never log secrets, tokens, credentials or whole request bodies.

## 10. Code Quality

- Remove all debug logging before committing.
- Keep a small helper used in only one file inline.
- Never commit commented-out code: version control history recovers old code.
- Match existing patterns over personal preference: the file's when editing, existing
  subsystems' when adding one.
- **Comment only what the code can't say:** why, a non-obvious constraint or invariant,
  or a contract callers rely on, in a sentence or two. Never restate the code or document
  a name that already says it. Leave out history, dates, who decided and plan steps:
  design reasoning lives in `docs/`, cited by name. Name a rejected alternative only when
  the obvious approach fails.

## 11. Code Style

- Prefer early returns. Use guard clauses at the top of functions to handle error/edge
  cases and return early, reducing nesting depth.
- Order imports consistently: (1) platform/standard library, (2) third-party packages,
  (3) project modules. Separate groups with a blank line.

## 12. Language Rules `[PROJECT]`

### Go

- Keep the code lint-clean (`TECH-STACK.md`, Build & lint).
- Treat errors as values: wrap with `%w` and context (`fmt.Errorf("load config %s: %w",
  …)`), inspect with `errors.Is` / `errors.As`. Make an error that callers branch on, or
  that crosses the API boundary, a typed error (`type XError struct`) or a sentinel value
  (§5).
- Pass `context.Context` as the first parameter of anything that does network I/O or may
  block; it is the cancellation primitive (§6).
- Give every goroutine an owner that can stop it and waits for it; never fire and forget
  a goroutine.
- Keep package-level variables to constant-like data: lookup tables, embedded assets,
  sentinel errors, the build version. Pass mutable state in; `main` (or `cmd/<name>`)
  builds the graph.
- Make shared live state that is replaced wholesale (config) an immutable snapshot,
  swapped atomically; readers hold the snapshot they started with.
- Put platform-specific code behind **build-tagged files** (`x_darwin.go` /
  `x_other.go`); keep secondary platforms compiling even when untested.

## 13. Prompts and Model-Facing Text

Coding agents read two texts cria writes: the config root's `AGENTS.md` (embedded from
`internal/config/agents.md`) and the `cria docs` output.

- **Prompt fixes generalize.** When a prompt, skill or fragment produces a wrong output in
  a tested case, improve the general definition or accept the variance; never add an
  exception targeting exactly the observed case. A rule per failure is overfit teaching:
  if the general wording cannot be made better, record the miss in the backlog and watch
  it, never patch it.
- **Rule out only what the model would otherwise do.** A "no X" where nothing suggests X
  costs words and puts X in mind.
- **A rejection says what to do instead**: in a message a model reads when its call
  fails, name the step that works.
- **Write it in the imperative, concisely**: say what to do, in the shortest wording that
  keeps the meaning.

## 14. Testing

- Test new behavior: unit tests for logic, client logic included; e2e for user-facing
  flows.
- Call a change verified only after the relevant tests have actually run. Show the
  output.
- Never skip, delete, or weaken a failing test to make the suite green. If a test seems
  wrong, say so and ask.
- Make tests clean up the files, folders and processes they create: a passing run leaves
  nothing behind.
- **Test what is, never what was.** Assert exactly the set of things that exist: that
  proves everything else absent without naming it. Never assert that a removed thing is
  gone; git is the archive of what was removed.

## 15. Debugging

Find the root cause before fixing; never fix the symptoms. Never wrap code in speculative
try/catch.

- **Prove the failure mechanism before designing the fix.** Name the specific failure
  mode, then find the one-line check that proves it (a log tail while reproducing, a CLI
  probe, runtime state). If you can't articulate the check, you don't understand the bug
  yet: keep investigating.
- **No second fallback for the same concern.**

## 16. Stored Data

- **cria's own state carries no shape version, and nothing migrates it** `[PROJECT]`: the
  files under `~/.local/state/cria/` are disposable memory. Losing one costs re-picking a
  choice, and the foreign scan finds running servers again, so a migration runner would
  be the most code for the least data. Handle only the current shape, with no dual
  reads, and never repair a file in the normal path: state a change's manual fix (delete
  this file) instead.
  - Validate each file strictly on read, and report one that doesn't match.
  - For picks (`choices.json`) and UI memory (`ui.json`), apply the defaults and let the
    next write replace the file.
  - For a server record or the router's store, refuse the file and name its one-line
    fix: delete it, and stop the pid a record names if it still runs.
- Run a change that touches more than one record in one transaction.
- Store timestamps in UTC as ISO-8601 strings; convert them to local time only for
  display.
