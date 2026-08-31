# STEP 5 — engine configs and the three-level merge

Status: done (2026-08-31) — suite green

## Intent

The config cut itself: per-engine config files appear, and a launch composes
across three levels — `engines/<engine>.toml`, the entry, the picked options —
with the tree's `args` staying the server's own flags, token for token.
`cria docs` follows by construction.

The step was first built to a `"key = value"` args shape (OVERVIEW ruling 1 as
ruled at plan review) and reworked the same day when the user reversed that
ruling. What is written below is the step as it stands; the reversal's reasoning
lives in OVERVIEW ruling 1 and in `docs/specs/CONFIG.md`, dated.

## Files likely touched

- `internal/config/`: the flag-group substrate, the merge, the narrowed
  collision rule, loading of `engines/<engine>.toml`, docs generation.
- `internal/serve/command.go`: the composed line is cria's four flags plus the
  merged args, verbatim.
- `internal/tui/serveview.go`: the args block, one flag group to a line.
- `docs/specs/CONFIG.md` in the same edit.

## Decisions made during planning

- **Format**: TOML stays the tree's syntax and `args` stays a list of argv
  tokens — what the server binary takes, copy-pasteable both ways
  (OVERVIEW ruling 1, amended 2026-08-31).
- **Precedence**: engine `[*]` < entry args < picked option args — the entry
  overriding its engine defaults is legal and explicit, mirroring upstream's own
  model-section-over-global rule.
- **Engine config files** live in the tree (`engines/llama.toml`, `mlx`,
  `router`): human/agent-owned like everything there; a missing file means
  empty defaults, never an error. cria's only tree writes remain root +
  `models/` + AGENTS.md.
- `host`/`port` stay the ONLY schema-composed fields (OVERVIEW ruling 3):
  `-c`, `--parallel` and everything else are ordinary passthrough flags — cria
  computes nothing from them.

## Acceptance criteria

- Component tests: the flag-group pairing, merge precedence, override at group
  granularity, within-list repetition, the cross-axis collision refusal, the
  composed-flag refusals, engine-config absence.
- `cria docs` and the schema render from one source, showing the args shape with
  both engines' examples.
- The composed argv for a representative profile is byte-identical to the line
  its files wrote, flat and after extraction into the engine file.
- Suite state recorded.

## Outcome

### The shape: the server's own command line, in TOML strings

```toml
args = [
  "-ngl", "99",
  "-fa", "on",
  # 262144 tokens, the whole window for a single slot
  "-c", "262144",
  "--parallel", "1",
  "--jinja",
]
```

Nothing here is a spelling cria chose. The short aliases are the ones the real
tree already uses, a comment sits beside the value it explains, and a profile
moves between a shell prompt and this file by copy-paste.

### The flag-group substrate

`config.FlagGroups(args) []FlagGroup` (`internal/config/args.go`) is the one
place that reads an args list at all: a **flag group** is a flag token — leading
dashes then a letter, with the 2026-08-22 amendment that `-1` and `-0.5` are
values — plus every token until the next flag. `FlagGroup{Flag, Tokens}` and
`String()` (the tokens joined by spaces) are the whole API.

Three callers, one rule:

- `mergeArgs` overrides by group (below);
- `refuseFlagCollisions` compares options by `Flag`;
- the TUI's detail pane draws `group.String()` one to a line.

Phase 3's router preset is the fourth: a preset line is a group with its flag's
dashes stripped, which is why this is exported substrate rather than a renderer
in the TUI. The derivation itself is **not** built here — no caller until
STEP-7 — and `FlagGroup.String`'s doc comment names it.

### The merge, as landed

- `mergeArgs(engine, entry, picked...)` — least specific first, layering one
  level over the accumulated ones with `override`.
- A flag named at a higher level replaces **every** group carrying it below, at
  the place the first of them stood; a flag the level introduces follows at the
  end. So an override changes what the server receives, never the order the
  files read in — which is what makes lifting a flag into the engine file an
  argv-preserving edit.
- **Within one list, repetition is legal**: a list is a command line, llama
  takes repeated flags (`--override-kv`), and there is no more specific side
  inside one level to prefer. All of a repeated flag's groups move together when
  a higher level overrides it.
- Tokens written before any flag are a headless group: they override nothing and
  nothing overrides them, so an unpairable list still reaches the server whole.
- The picked options are **one** level between them (two axes may not set one
  flag, so nothing has to choose between equals).
- The merge allocates its own groups and its own token list — a launch can never
  grow into the loaded tree it was composed from.

### The collision rule, narrowed

Loud at load, and only here: **options of two different choices setting the same
flag**. Both are picked at once, so there is no winner; the refusal names the
flag and both options. Legal and silent: entry over engine, picked option over
entry, repetition within a list, options of one choice sharing flags.

The comparison is by flag token, so `--ctx-size 8192` and `--ctx-size=8192`
collide. `refuseFlagCollisions` no longer takes the entry's args at all — that
pair is now the override the levels exist for.

### Composed flags

`-hf` (llama), `--model` (mlx), `--host`, `--port` may not appear in any args
list — entry, option or engine file — in either the separate-value or
`--flag=value` spelling. The list is read off the backend registry
(`backendFacts{id, modelFlag}`), so the page cannot name a set different from
the one the parser enforces.

### `onlyBackend` → `onlyBackends []Backend`

`key.onlyBackends` names the backends that take a key; empty means all of them.
`takenBy(backend)` is the one predicate (load refusal, `cria docs` example
filter, the docs "llama only —" note, the example-walk test). A phase-3 router
entry taking a quant is `onlyBackends: {BackendLlama, BackendRouter}` — no third
state, refusals still total, and the refusal text for the current single-backend
case is byte-identical to before (`takenByNamed`).

### How `Docs()` lost its last engine knowledge

The prose line naming `-hf`/`--model` is gone. `config`'s backend registry
became `backendFacts{id, modelFlag}` — the flag cria composes that backend's
model reference under — and:

- `composedFlags()` (the parser's refusal list) and `composedFlagsNote()` (the
  page's "args may not restate them: -hf (llama), --model (mlx), --host,
  --port") both read it, so the page cannot name a different set than the
  parser enforces;
- `internal/engine`'s `TestEveryEnginesModelFlagIsTheFlagTheTreeRefuses` holds
  every engine's `ModelArgs()[0]` to `config.ModelFlag(engine.ID())`. The
  duplication the import direction forces is now *checked* duplication, the same
  way `TestTheEnginesAreExactlyTheBackendsTheTreeMayDeclare` holds the backend
  set.

Rejected: passing engine strings into `Docs()` as an argument (pushes the wiring
into `cli` for one sentence), and moving the flag itself into `config` (would
make `config` more engine-knowledgeable, not less).

### Contract-test changes

| File | Change | Why |
|---|---|---|
| `config/scaffold_test.go::TestAgentsPagePointsAtTheBinary` | added `engines/<backend>.toml` to the required mentions | The embedded page now has to name where machine-wide flags live |
| `config/docs_test.go` (4 tests) | walk `engineSchema` too; `onlyBackend` → `takenBy`; the examples tree writes the engine files and asserts they reach the entries | The page grew a schema; the tests read definitions, never values |

The TUI's two detail-pane contract tests assert exactly what they asserted
before this plan — the args block draws `--ctx-size 16384` and `--jinja` — since
the shape they pin is unchanged. Everything else that mentions args is a
**fixture**, and every composed-argv assertion in `serve`, `cli`, `tui` and
`picks` is byte-identical to before: that is the point.

### The argv-identity proof

`serve/command_test.go::TestAProfilesArgsReachTheServerVerbatim` — a real tree
written to a temp dir, loaded, resolved and composed, against the line its files
wrote:

```
llama-server -hf unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL --host 0.0.0.0 --port 8080
  -ngl 99 -fa on -c 262144 --parallel 1 --jinja --n-cpu-moe 24
```

Two trees reach it, token for token: the flat profile, and the same profile with
`-ngl`/`-fa`/`-c` in `engines/llama.toml` and the entry overriding `-c`. The
second is the one that matters — it proves an extraction is argv-preserving even
when the entry takes a flag back from the engine, because the override lands
where the engine's group stood.

### Decisions and deviations

- **Engine files: `args` only.** YAGNI — the router's own settings land in
  STEP-7. An unknown key there is refused loudly.
- **A broken engine file fails the load**, like `config.toml`. Rejected:
  marking every entry of that backend broken — it reports one file's mistake
  once per entry and points the reader at the wrong file. Recorded in CONFIG.md.
- **`engines/` is not scaffolded.** cria's tree writes stay root + `models/` +
  `AGENTS.md`.
- **The engine examples on the docs page use `-ngl 99`, `-fa on`.** The short
  aliases are what the machine actually passes, and the page teaching them is
  the reversal made visible.
- **The preset spelling was not built** — no caller until phase 3, and
  `FlagGroup` is already the shape it derives from. No dead code shipped.
- **The detail pane shows the two file levels, not the merge.** Engine-level
  flags and which of two lines wins are read off the command row under it.
  TUI.md records that, dated; a third ink for the engine level is a phase-3
  question, not a drive-by.

### Mutation checks

- `override` keeping the lower level's groups: reddens
  `config::TestResolveMergesTheLevelsBySpecificity`,
  `TestAnOverriddenFlagIsPassedOnce` and
  `serve::TestAProfilesArgsReachTheServerVerbatim`.
- `flagToken` treating any dash as a flag (the `-1` amendment dropped): reddens
  `config::TestFlagGroupsPairEachFlagWithItsValues` and the merge test's
  negative-value case.
- `override` replacing only the first occurrence below: reddens the merge test's
  "an override replaces every occurrence" case.
- llama's `modelFlag` drifting to `-hff`: reddens
  `engine::TestEveryEnginesModelFlagIsTheFlagTheTreeRefuses/llama` and
  `config::TestEntryRulesReject/args_may_not_restate_-hf` — the registry and the
  engine are held together in both directions.
- `refuseFlagCollisions` returning nil: reddens `TestFlagCollisionNamesBothOptions`,
  `TestEntryRulesReject/options_of_two_different_choices…` and
  `TestLoadIsolatesABrokenChoice`.
- The detail pane drawing one token per line instead of one group: reddens
  `tui::TestDetailPaneCarriesThePickedArgs`.

Every production file was restored and the suite re-run green.

### Suite

`gofmt -l .` empty; `go vet ./...` clean; `go test -count=1 ./...`:

```
ok  cria 0.395s · cria/internal/cli 5.803s · cria/internal/config 0.931s
ok  cria/internal/engine 2.115s · cria/internal/format 1.357s
ok  cria/internal/hubapi 2.956s · cria/internal/hubcache 1.947s
ok  cria/internal/picks 1.102s · cria/internal/procs 1.826s
ok  cria/internal/selfupdate 2.410s · cria/internal/serve 5.749s
ok  cria/internal/tools 3.980s · cria/internal/tui 7.498s
```

No expected reds. The real tree loads against this binary unchanged — the levels
are new, the args shape is not.

### Docs updated in this step

- `docs/specs/CONFIG.md` — args are verbatim tokens (and why the key shape was
  reversed), the flag group, the three-level merge, the narrowed collision rule,
  `engines/<engine>.toml`, the tree-wide failure rule, the model-flag registry
  and key applicability as a set.
- `docs/specs/TUI.md` — what the args block reads, and that it is the files' two
  levels rather than the merge.
- `docs/ARCHITECTURE.md` — `FlagGroups` on the config row.
- `internal/config/agents.md` and `README.md` — cria's own surfaces.

### What STEP-6 must know

1. **Nothing has to be rewritten to load.** Every existing profile parses as it
   stands; the session is extraction only.
2. **Extraction is argv-preserving when the flag is not repeated across
   levels** — and when it is, the entry's group lands at the engine's position,
   so the line still matches. Diff every profile's composed line anyway.
3. **A flag the engine sets and no entry overrides moves to the front** of the
   composed line, since the engine level composes first. Compare those profiles
   as flag/value pairs, not as strings.
4. `args = []` is legal, and so is a flag written twice.
5. The repeatable-flag question (OVERVIEW ruling 4) is closed for the tree; it
   returns only for the router preset in phase 3.
