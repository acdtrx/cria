# SERVE — server lifecycle

Servers outlive the TUI and there is no daemon (`docs/cria.md`, principle 3): cria
spawns them detached, records what it did, and any later cria invocation re-attaches
by reading those records back. This spec owns the lifecycle contract — start,
observe, stop, re-attach — and the runtime state records behind it.

**Per-engine knowledge is not the lifecycle's** (settled 2026-08-31): which
program serves an entry, how its model is named on that program's command line,
which endpoints it publishes, whether a green server has already loaded its
weights, and whether a quantization qualifies its model reference are one
implementation per way of serving in `internal/engine`. The lifecycle asks; it
never branches on the backend, and a backend no engine claims is refused at the
first question rather than answered with another engine's endpoints.

## Process model (settled 2026-08-18)

- A server is spawned detached in its own session, stdout and stderr to its log
  file, with `HF_TOKEN` exported when the `hf` token file exists (env, never argv).
  cria is free to exit at any moment; nothing about a running server depends on a
  cria process existing.
- The identity is captured right after the spawn, and re-asked briefly while the
  command line settles (amended 2026-08-18): a script-installed server re-execs
  through its interpreter within milliseconds — a uv shim does it twice — and an
  identity captured mid-exec would read a healthy server as exited forever.
  Capture waits, bounded, until the command names the launched program or the
  pid is gone; only then does the record take the match-nothing zero identity.
- Exit codes of detached servers are unobservable by design (they reparent away);
  the log tail is the crash evidence, never a collected exit status.

## State records

- One JSON record per running server at
  `~/.local/state/cria/servers/<entry-id>.json`, written at spawn: entry id,
  backend, repo (and quant), pid, the pid's own start time, port, host, the full
  composed command line, log path, launch timestamp — and, for an entry with
  choices, the picks the launch composed (settled 2026-08-22): what runs is a
  combination, and the record is where a combination's identity lives.
- **A record says what its server was started to serve, and there are two answers**
  (settled 2026-08-31): one entry's server serves a model, named by `repo` (and
  `quant`); an engine's own server serves the models a composed preset lists, and
  carries `preset` instead, with the engine's id where an entry id stands. Exactly
  one of the two is set — a record with both, or neither, is refused on read.
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
  in the cache, **or a copy of it is landing** (amended 2026-08-19: an upstream
  re-upload makes the server silently re-fetch a changed blob while the old
  copy still reads complete — that start showed as "starting" and got killed as
  stuck). A partial whose blob name matches the entry's current Hub oid is this
  entry's download, and progress then counts the landing copy's bytes; when the
  Hub cannot say, any unfinished download in the entry's repo counts — coarser,
  never louder. An ordinary start of a cached, unchanged model asks the Hub
  nothing. Otherwise progress renders as on-disk cache bytes versus Hub-API
  sizes (filesystem observation; when the Hub API is unreachable, bytes show
  without a total). The server does the fetching; cria only watches the cache.
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

1. Validate the entry and resolve its picks (settled 2026-08-22): explicit
   one-shot picks over stored picks over config defaults, per choice; an unknown
   choice or option refuses up front, naming the valid ones. Refuse if the
   entry's tool is missing (naming the tool and what its absence disables) or
   the entry is already running.
2. Check the port. Held by a live record → refuse: stop that entry first. Held by
   anything else → refuse with the holder's pid, command line and working
   directory (`lsof` + `ps`); the TUI offers the kill.
3. Compose the command (`docs/specs/CONFIG.md`), spawn detached, write the record.
4. `cria start <id>` returns once the record is written; `--wait` blocks until the
   phase settles (`running` → exit 0; `exited`/`unhealthy` → non-zero) — agent
   validation in one command. A green verdict is corroborated by attribution
   (amended 2026-08-18): the pid listening on the port must be the pid cria
   spawned — a green answered by some other process fails the wait naming both
   pids. Attribution that cannot be obtained degrades to a note, never a
   failure: the health signal is primary, `lsof` corroborates.
5. Lazily-loading backends are **warmed by default** (settled 2026-08-18):
   mlx_lm.server answers green before loading any weights, so a green `--wait`
   sends one minimal completion (`POST /v1/completions`, one token, the
   record's own model reference — never an empty prompt, which wedges the
   server; found by live probing) and reports only after it answers: green
   means loaded, under its own generous budget. The warm first waits for the
   server's health signal within that budget (amended 2026-08-19: the TUI fires
   the warm right after the spawn, and mlx binds its port seconds later — a
   completion sent into that gap read as "connection refused" for a server that
   was loading fine); a pid that dies during the wait ends it silently — the
   box shows exited, the log is the evidence. The TUI fires the same warm in
   the background after an mlx start. llama loads at startup and is never
   warmed; which backends load lazily is their engine's own answer. A no-wait start
   cannot carry the request and says so in a note.

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

## The router (settled 2026-08-31)

One `llama-server` per host, started in router mode: it supervises a child server
per model it holds and proxies all of them on one port (probe-verified
2026-08-31). cria starts, watches and stops that one process and never touches its
children — which models it holds is asked through the router's documented API
(STEP-8).

- **It is not an entry.** No `models/<id>.toml` declares it; it is configured by
  `engines/router.toml` and started as itself (`cria router start`,
  `docs/specs/CLI.md`). One router per host, on a port of its own beside the
  entries' — a busy port refuses exactly as a start's does, naming a managed
  holder to stop or a foreign process to deal with.
- **Engine-scoped state lives in a subfolder per engine** (settled 2026-08-31,
  OVERVIEW ruling 2): `~/.local/state/cria/engines/router/` holds `preset.ini`
  (the composed preset), `server.json` (the record) and `logs/router-<stamp>.log`
  (the newest three, the same retention entries get). Entry records and logs stay
  where they are — an entry's server is still an entry's — so nothing moves and
  nothing needs cleaning up. The router's logs live under its own folder rather
  than beside the entries' so an entry named `router` can neither prune them nor
  be pruned by them.
- **The preset is runtime state, composed at every start and never edited.** The
  tree stays human-owned: `engines/router.toml`'s `args` become the `[*]` section
  of upstream's ini, a flag group with its dashes stripped (`-ngl 99` → `ngl = 99`,
  a bare flag → `key = true`). cria owns no key mapping — upstream canonicalizes
  its own aliases (probe-verified 2026-08-31) — and writes no comments into the
  file, because that parser judges every line it reads. A flag that cannot be one
  key (written twice, carrying more than one value, or tokens before any flag)
  refuses the start by name, before anything on the host has changed.
- **Liveness and phases are every server's** (docs above): the pid is still the
  process cria launched, and its own `/health` answers. A router with no model
  loaded answers green — that is what routing means, and it is why the router
  loads nothing lazily and is never warmed. There is no `downloading` phase for
  it: it holds no weights, and a model's bytes are fetched by the child that
  serves it, when a request asks for one.
- **The tool check answers for router mode** (`docs/specs/TOOLS.md`): the same
  `llama-server`, asked whether this build takes `--models-preset`, so a build that
  predates router mode refuses up front instead of failing at the spawn.
- **Where a record lives is a function of the record** (settled 2026-08-31): an
  entry's server is recorded under its entry, an engine's own under its engine,
  and the record already says which it is. So stop, kill and dismiss take a
  record and need no second answer — the router is ended by the same escalation
  at the same pid as any server, and the file the confirmed exit removes is
  whichever of the two it lives in. Rejected: a stop of its own per engine — one
  call per place a record can sit is a dispatcher the record makes unnecessary.

### The models under the router (settled 2026-08-31)

Which entries the router serves is **state, not config**: the tree declares
entries, and a store of the router's own says which of them this host's router
holds and under which combination (OVERVIEW ruling 2). The same entry can be a
llama entry with its own picks and a router model with different ones — that is
the point of holding them per engine, and it is why no entry key could express
inclusion (`docs/specs/CONFIG.md`).

- **The store is `~/.local/state/cria/engines/router/models.json`**, entry id →
  the picks it is held under. **The key is the inclusion**: an entry held at its
  config defaults is `{}` rather than absent, which is exactly where this store
  parts from `choices.json` — there, an entry picking nothing and an absent entry
  say the same thing.
- **A store cria cannot read refuses; it does not degrade.** A stale *pick* has
  the config default to fall back on, and nothing stands in for "which entries are
  included" — an empty answer would start a router serving nothing and look like a
  successful start. So a broken store refuses the start naming the path and the
  one-line fix, while a missing file is simply a fresh router.
- **Inclusion is never auto-pruned.** A pick naming an option the entry no longer
  has falls back to the config default; an included id whose entry is gone stays
  in the store and is reported against that id. A rename or a typo must not
  silently empty the router, and `cria router exclude` — which never reads the
  tree, for this reason — is the way out.
- **The preset is one section per included entry**, carrying the entry's args
  merged with the picked options' and **no engine level**. `engines/router.toml`'s
  `args` are the `[*]` block, which upstream applies under every section by its
  own documented precedence (command line > model section > `[*]`); merging them
  into each section would be cria doing upstream's job twice and would make every
  pick change a whole-file diff. Each section is named by the model reference and
  carries `alias = <entry-id>` — upstream normalizes a section's quant tag but
  passes an alias through as written, which is what makes the entry id the name
  clients send.
- **A model that cannot be carried is skipped, never fatal** (settled
  2026-08-31). One entry whose args are not expressible as preset keys would
  otherwise take every other model down with it. The verdict is per model and
  **every surface prints it** — a silently dropped model is a client's 404 hours
  later. Six things skip one model: its args are not writable as preset keys (the
  three refusals above), its args set `alias` (cria writes that key itself, and
  two would hand upstream two answers), two models resolve to the same
  `repo:quant` (one section written twice — the later is skipped naming both
  entries), the tree no longer declares the id, its file no longer loads, or an
  entry served by another program is included (`mlx` entries are served by
  `mlx_lm.server`; only llama entries can be in a llama-server router). The
  engine file's own args remain the one thing that refuses the whole composition:
  that is the router's configuration, not one of its models.
- **The composition is pure**: it reads the store and the tree, writes nothing and
  asks no server, so it answers the same whether or not a router is running —
  what the *next* start would serve. A start returns it beside the record, so the
  caller reports what was actually written rather than composing a second time.

### What the router says its models are doing

Child processes are the router's own. cria never looks for them in the process
table: it asks the router, which publishes one row per model it holds
(`GET /models` — id, aliases, status).

- **Upstream's word is what is shown, and the phase map is deliberately
  partial** (settled 2026-08-31). The states are `downloading`, `downloaded`,
  `unloaded`, `loading`, `loaded`, `sleeping`. Three map onto cria's phases —
  `downloading` → `downloading`, `loading` → `starting`, `loaded` → `running` —
  and three have no true word in it: `downloaded`, `unloaded` and `sleeping` all
  mean the router holds this model with nothing resident, which is not
  `starting` (nothing is starting), not `exited` (nothing was launched) and not
  `running`. Those get **no phase at all**, and every surface prints the router's
  own word, which is what a reader wants anyway. Visible-and-absent beats
  plausible-and-wrong, and a state upstream adds later lands in the same place
  instead of being read as something cria knows. Rejected: widening `Phase` with
  a "held" value — it would reach `cria status --json`, the TUI's tone table and
  this document's phase contract for a state that is not a server's phase.
- **A listing cria could not get is a display fact, not an error** — the same
  shape a health probe has. A router still coming up has nothing to answer with
  yet, and the surfaces say so rather than claiming it holds nothing.
- **cria only ever addresses a model the router has just said it holds.** A name
  matches on alias *or* reference, and that check is what makes `?model=`
  addressing honest: `/slots` (and `/props`) transfer to one child by query
  parameter, so the llama engine's per-slot signal works under the router
  unchanged.
- **Load and unload are deliberate verbs** (`POST /models/load|unload`, body
  `{"model": "<name>"}`, under the same generous budget a warm gets). The router
  loads a model on the first request that names it — that autoload is the feature
  clients rely on — and these exist because a mechanism must be invocable on its
  own: a model wanted resident before the first request is a thing to ask for,
  not a thing to arrange by sending a fake completion.
- **Unload refuses a model that is answering a request right now**, with
  `--ignore-busy` as the override — the same gate, the same signal and the same
  spelling validate puts in front of displacing a busy server (Validate, below).
  That refusal is also what gives the per-model `/slots` addressing a real caller.
- **Eviction is upstream's and client-driven** (probe-verified 2026-08-31): at
  `--models-max N` the router keeps N models resident, and a request naming an
  N+1th stops one and starts the other. cria neither triggers nor prevents that —
  it is the swap the whole router mode exists for — and how many may be resident
  is a `router_args` flag rather than anything cria carries a default for
  (`docs/specs/CONFIG.md`). What cria shows is the result: the router's own word
  per model, changing under the refresh.

## Foreign servers (settled 2026-08-18)

- Any `llama-server` or `mlx_lm.server` process without a matching live record is
  foreign: shown with pid, command line and working directory, with an offered
  kill — the forgotten-terminal case. Detection is `ps` with explicit field
  selectors (`docs/TECH-STACK.md`); a busy port is attributed with `lsof`.

## `cria status`

- Human output mirrors the TUI status box: entry, backend, repo:quant, pid, port,
  phase, uptime, memory (RSS) and CPU, health, log path — and the picks the
  server was composed from, when its entry declares choices. `--json` emits the
  same facts as one JSON document — the machine contract for agents. Exits zero
  when at least one server is live, non-zero when none is.
- **The router is one of those servers** (settled 2026-08-31): cria started it and
  holds its record, so a status that hid it would answer "what is running here"
  wrongly, and a live router alone exits zero like any live server. It reads as a
  block of its own rather than a row among the entries' — it serves a preset
  instead of a model, and the models it holds are its own to list, each with the
  router's own word for it. In `--json` it is the `router` key, **null** on a host
  that has no router record: one router per host, so an empty list would deny the
  shape and a zeroed object would read as a router with pid 0. An exited router is
  a crash report and is not asked what it holds.

## Validate (settled 2026-08-23, user-designed)

`cria validate <id> [choice=option ...] [--ignore-busy]` answers one question and
blocks until it can: does this entry, in this combination, serve on this machine?
The expected caller is a coding agent that just wrote the entry and is thinking on
the model this machine serves (`docs/cria.md`, principle 5) — so validation has to
take the port away from that model and give it back. The agent infers before the
command and after it; what it reads is an exit code and one reason line.

The protocol, in order, from the first thing that changes the host:

1. **Stop the server holding the target's port, keeping its record.** The stop is
   the ordinary one, and it removes the record from disk — so the record is taken
   as a value first, picks included: that held copy is the only thing that can put
   the server back.
2. **Start the target and wait for green** — the same wait `start --wait` performs
   (health primary, listener attribution corroborating, the lazy backend's warm
   included).
3. **Prove it with one real completion**, carrying the record's own model
   reference. A green health signal is a model that loaded, and a model that loaded
   can still die on its first request (a speculative batch that does not fit, a
   buffer the machine cannot allocate), so proof is an answer and nothing less.
   Every backend is proved this way: this is fit-proofing, not the lazy-load warm
   (Start 5) — the two share the request, never its backend gate.
4. **Stop the target.**
5. **Restore the displaced server** from the held record: the entry the tree
   declares under that name today, launched with the picks that record carries, not
   whatever the entry's stored picks say now.

Nothing before step 1 has changed the host, and every refusal lives there. A port
nobody holds means nothing to displace and nothing to restore — start, prove, stop:
"left as found" includes leaving nothing running.

- **Displacement is port-scoped** (settled 2026-08-25). The one server validate may
  stop is the live record holding the target's *resolved* port (the entry's port,
  else `default_port` — no pick can move it, an option replaces a quant, a repo or
  flags). The target entry carries that fact by itself, so the caller needs to know
  nothing about what else runs here, and a server on any other port is structurally
  untouchable. On the shared-port machine the config doctrine describes — one
  `default_port`, swap by stop-one-start-another — the port's holder *is* the
  running server. Rejected: refusing when several servers run (the common machine
  runs one); any flag naming a server to stop (knowledge the caller does not have);
  validating on a second port with both models resident (the machine that needs
  validate is the one without memory for two).
- **Self-validation is the same path** (settled 2026-08-23). A target that already
  holds its own port is displaced and started again like any other holder — the
  point may be proving a new combination of it — and restore replays the held
  record's picks either way. One code path, no special case.
- **The busy gate reads is-processing, never open connections** (settled
  2026-08-23). A holder answering somebody right now is a refusal, not a stop: the
  signal is llama-server's documented `/slots` per-slot `is_processing` (verified
  live 2026-08-25), asked once, at the health probe's address rule. Counting open
  connections was rejected outright — the validating agent's own client holds an
  idle keep-alive socket to that port, so a connection count refuses on the
  caller's ghost.
- **Unverifiable is a third answer, never a cautious idle.** A backend that
  publishes no such signal is not asked at all (mlx_lm.server documents none); a
  llama server whose `/slots` is unreachable, refuses, or answers something that is
  not a slot listing — an empty listing included — is unverifiable too. That case
  warns, naming what could not be checked and what a swap would cost, and proceeds:
  the machine that needs validate runs one server on one port, usually the caller's
  own, idle at the moment the tool call executes.
- **Validate never queues and never waits for idle.** The caller's own turn is
  running while cria blocks, so a wait would burn its clock invisibly; the honest
  answer is the refusal, and the action it names is a human's — let the answer
  finish, or stop that server.
- **`--ignore-busy` lifts the busy gate and nothing else** (settled 2026-08-25).
  It is the operator's word that a generation cut off mid-answer is acceptable; the
  verdict is still asked for and reported as a warning, so the override never hides
  a fact cria knows. The foreign-holder and already-running-elsewhere refusals stand
  under it — neither is about anybody's patience. **The busy refusal deliberately
  does not name the flag**: the agent reading that line is the one whose request
  would be cut off, and a bypass printed on the line it reads is a bypass it will
  take. The flag is documented in `cria --help`, where the person deciding reads,
  and not in `cria docs` or `AGENTS.md` — the agent-facing surfaces stay
  bypass-free.
- **Restore is unconditional.** Whatever the target's verdict — it never started,
  never went green, never answered, or the operator pressed Ctrl-C — the displaced
  server goes back before validate says anything about the entry it was asked
  about. An interrupt ends the stages, not the process: the current stage is
  abandoned at its boundary, the restore runs, and the exit says what the machine
  was left as. The one honest carve-out is a **target that will not stop**: it still
  holds the port, so restoring would spawn the displaced server onto a port nothing
  can bind. That exit names the two commands that undo the state instead
  (`cria stop <target>`, then `cria start <displaced>`).

**Exit codes.** Four outcomes an agent branches on, and nothing finer. Every
non-zero exit prints one concise reason as its last line; with the code, that is the
whole contract.

- `0` — it served and answered; the machine is as validate found it.
- `1` — it did not validate (it would not start, never went green, or did not
  answer), and the machine is as validate found it: the displaced server is back on
  its port. An interrupt whose restore succeeded exits here too — nothing was
  proved, and that is what the reason line says.
- `2` — validate refused and touched nothing: unknown entry, choice or option; the
  backend's tool missing; a foreign process on the port (refused with pid, command
  line and working directory, exactly as start refuses — validate cannot restore
  what has no record); the target already running on a port other than the one it
  launches on now (starting it again would leave that process with nothing naming
  it); a busy holder. This is the same code an unroutable command line gets,
  deliberately: both mean cria did nothing to the host, which is the only
  distinction a caller can act on.
- `3` — the machine is **not** as validate found it: the holder would not stop, the
  target would not stop, or the restore failed. The reason line names what is
  serving now and the command that ends the state — the one outcome a person has to
  act on.

**No `--json`** (settled 2026-08-23): the agent contract is the exit code plus a
concise reason line, and validate reports no measurements. Structured output earns
its place only if it ever grows some.

## Benchmarking (settled 2026-08-19)

`cria bench` measures a running server's prefill and decode rates per prompt
size, through its OpenAI HTTP endpoint — a deliberate user action against a
documented interface, uniform across backends by construction:

- **Client-side measurement only.** Prefill = the server's own
  `usage.prompt_tokens` over time-to-first-token; decode = streamed tokens over
  the first-to-last-token window (the first token closes the prefill and is not
  counted in decode). llama-server's proprietary `timings` field is never read —
  asymmetric data would break the llama-vs-mlx comparison the bench exists for.
- **Streaming facts, verified live (2026-08-19)**: `stream_options.include_usage`
  is always sent (mlx_lm.server reports usage only with it; llama-server always
  does); SSE comment lines are skipped (mlx emits `: keepalive` while
  prefilling — read as content it would fake the TTFT); llama enforces its
  context size with HTTP 400 while mlx enforces nothing.
- **Honest numbers**: every run gets a unique prompt (llama caches prompt
  prefixes — a repeated prompt fakes an instant prefill; `cached_tokens` is
  watched and a material share is warned about); the unmeasured warmup doubles
  as chars-per-token calibration so sizes land near their targets, and sizes
  are labeled by the server's actual token count. Runs the model ended early
  are excluded from decode means rather than averaged in as zero, and the
  early-end note fires only when it is material (a run with no decode window,
  or a mean under 3/4 of what was asked).
- **Filler sizes the prefill; a seeded story instruction drives the decode**
  (amended 2026-08-19, from field failures): the prompt is a nonce, English
  filler cut to size, then an instruction to write an unending story with its
  opening sentence already seeded. Each clause is evidence: continuation-style
  prompts let models EOS instantly (zero tokens to time); a bounded ask
  produces short *complete* stories; an unseeded ask lets mlx_lm.server enter a
  spontaneous think block that renders as **empty text** — 256 invisible
  tokens; and realistic story text keeps speculative/MTP decode rates honest,
  where repetitive continuation inflated and destabilized them. Decode is
  monotone-decreasing with prompt size on dense, MTP and mlx models under this
  prompt. The smallest size is 96 tokens — the nonce+instruction alone
  tokenizes to ~66, and no prompt is ever empty (it wedges mlx_lm.server).
- **The decode number is a comparable floor, not a ceiling** (settled
  2026-08-19, measured server-side on an MTP model: 77 t/s on the bench's
  workload, 91 raw-and-free, 123 in chat mode — all the same server). Under
  speculative decoding, speed depends on how predictable the output is, so
  chat-mode use runs meaningfully faster than the bench's fixed raw workload;
  models without speculation are content-blind and match their bench numbers
  in use. The fixed workload is deliberate: comparison across models and
  backends needs everyone measured on the same text, and letting each model
  pick its happiest regime would make the numbers incomparable.
- A size that fails carries its reason and the sweep continues; one bench runs
  at a time. The TUI's bench pane (`docs/specs/TUI.md`) always runs the default
  sweep; sizing flags are CLI-only.
