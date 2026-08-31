# STEP 5 — ini-keyed profiles and engine configs

Status: done (2026-08-31) — suite green

## Intent

The config cut itself: profile `args` (and choice option `args`) become
key=value maps; per-engine config files appear; composition merges engine
`[*]` + entry + picked options with upstream's precedence, spelled as argv
for process engines and as preset ini for the router. `cria docs` follows by
construction.

## Files likely touched

- `internal/config/`: schema (args as ordered key=value pairs; the
  key-exact collision rule replacing token heuristics), loading of
  `engines/<engine>.toml`, docs generation.
- `internal/engine/`: composition — merged keys → argv (llama, mlx) with
  the boolean rule mirroring upstream (`key = true` → bare flag), and the
  same merge → preset text (router, consumed in phase 3).
- `internal/serve/command.go` successor wiring; `docs/specs/CONFIG.md` in
  the same edit.

## Decisions made during planning

- **Format**: TOML stays the tree's syntax — args become a TOML
  array-of-strings of `"key = value"` lines or an inline table; settle for
  whichever preserves author order and comments best (author order matters
  only for readability now — composition is order-independent by key).
  The upstream *ini* file is an output cria composes, never the tree's
  input format: humans keep TOML, engines get their native spelling.
- **Precedence**: engine `[*]` < entry keys < picked option keys — the
  entry overriding its engine defaults is now legal and explicit (upstream's
  own model-section-over-global rule); a key colliding *within* one level
  stays a loud load error, and options of one choice still share keys
  freely.
- **Engine config files** live in the tree (`engines/llama.toml`, `mlx`,
  `router`): human/agent-owned like everything there; a missing file means
  empty defaults, never an error. cria's only tree writes remain root +
  AGENTS.md.
- **Booleans**: `true` → bare flag, mirroring upstream's documented rule;
  no other coercions. Values pass verbatim otherwise.
- **Repeatable flags**: inexpressible by design; the schema refuses a
  duplicate key in one part loudly. STEP-6's survey confirms no real
  profile needs repetition (none known at planning time).
- `host`/`port` stay the ONLY schema-composed fields (OVERVIEW ruling 3):
  `c`, `parallel` and everything else are ordinary passthrough keys — cria
  computes nothing from them.
- Feature-building mode: the old `args` token-list shape is refused loudly
  with the manual fix named ("rewrite args as key = value lines; see
  cria docs"), no dual-read.

## Acceptance criteria

- Component tests: merge precedence, key collisions (within-level loud,
  cross-level override), boolean spelling, engine-config absence, old-shape
  refusal message.
- `cria docs` and the schema render from one source, showing the new shape
  with both engines' examples.
- The composed argv for a representative migrated profile is byte-identical
  to its pre-cut composition (fixture-level proof staged for STEP-6's live
  diff).
- Suite state recorded; expected reds named (the real tree is still
  old-shape until STEP-6 — cria's own tests use fixtures, so none are
  expected from that).

## Outcome

### The TOML shape settled: an array of `"key = value"` lines

```toml
args = [
  "gpu-layers = 99",
  "flash-attn = on",
  # 262144 tokens, the whole window for a single slot
  "c = 262144",
  "parallel = 1",
  "jinja = true",
]
```

Chosen over a `[args]` table, which loses two things the tree needs:

- **Author order.** A TOML table decodes into a map; the order is gone. The
  composed command line would then have to be sorted, and the pre/post
  migration argv diff STEP-6 rests on could not be a diff.
- **The value as written.** A table retypes: `temp = 0.70` comes back as a
  float and re-renders as `0.7`, `1e5` as `100000`. cria would be handing the
  server a number it reformatted, which is not passthrough.

The array also puts a comment beside the value it explains — the reason
context arithmetic gets written down at all — and each element is already one
line of the servers' own config format, so the preset spelling phase 3 needs
is `strings.Join` over `config.Arg.String()`, not a second composition.

Rejected alongside the table: keeping dashes in the key (`"--ctx-size =
16384"`) — the dashless key is what makes the same line legal in a preset
section, and refusing a dashed key is what catches the old shape.

### The merge, as landed

- `config.Arg{Key, Value}`; `Entry.Args`, `Entry.EngineArgs`,
  `ChoiceOption.Args`, `Launch.Args` all carry ordered `[]Arg`.
- `config.mergeArgs(engine, entry, picked...)` — least specific first. A key
  set at more than one level takes the **last** level's value and keeps the
  **first** level's position: an override changes the value, not the order the
  file reads in.
- The picked options are **one** level between them (two axes may not set one
  key, so nothing has to choose between equals).
- Refusals, all at load: a key twice in one list (named where it is written);
  the same key in options of two *different* choices (names the key and both
  options); a key cria composes (`hf`, `model`, `host`, `port`); a dashed key;
  a key outside the id charset; a key with no value; an element with no `=`.
- Legal and silent, by design: entry over engine, picked option over entry.
  `refuseFlagCollisions` (entry-args-vs-option) is gone — that pair is now the
  override the levels exist for; `refuseKeyCollisions` keeps the cross-axis half.
- Spelling lives in `internal/engine.Flags`: one letter → one dash, longer →
  two; `value == "true"` → bare flag; every other value passed as its own argv
  element (never glued with `=`, so a value with spaces survives).

### `onlyBackend` → `onlyBackends []Backend`

`key.onlyBackends` names the backends that take a key; empty means all of them.
`takenBy(backend)` is the one predicate (load refusal, `cria docs` example
filter, the docs "llama only —" note, the example-walk test). A phase-3 router
entry taking a quant is `onlyBackends: {BackendLlama, BackendRouter}` — no
third state, refusals still total, and the refusal text for the current
single-backend case is byte-identical to before (`takenByNamed`).

### How `Docs()` lost its last engine knowledge

The prose line naming `-hf`/`--model` is gone. `config`'s backend registry
became `backendFacts{id, modelKey}` — the args key cria composes that
backend's model reference with — and:

- `composedKeys()` (the parser's refusal list) and `composedKeysNote()` (the
  page's "args may not set them: hf (llama), model (mlx), host, port") both
  read it, so the page cannot name a different set than the parser enforces;
- `internal/engine`'s new
  `TestEveryEnginesModelFlagIsTheKeyTheTreeRefuses` holds every engine's
  `ModelArgs()[0]`, dashes stripped, to `config.ModelKey(engine.ID())`. The
  duplication the import direction forces is now *checked* duplication, the
  same way `TestTheEnginesAreExactlyTheBackendsTheTreeMayDeclare` holds the
  backend set.

Rejected: passing engine strings into `Docs()` as an argument (pushes the
wiring into `cli` for one sentence), and moving the flag itself into `config`
(would make `config` more engine-knowledgeable, not less).

### Contract-test changes (schema change those tests exist to follow)

| File | Change | Why |
|---|---|---|
| `tui/serveview_test.go::TestDetailPaneCarriesTheWholeEntry` | `"--ctx-size 16384 --jinja"` → `"ctx-size = 16384"`, `"jinja = true"` | The pane's args block reads the file's lines; the old string now only matched incidentally, on the command row |
| `tui/serveview_test.go::TestDetailPaneCarriesThePickedArgs` | the three styled rows and the block layout string | Same block, new spelling; the command-line assertions are untouched |
| `config/scaffold_test.go::TestAgentsPagePointsAtTheBinary` | added `engines/<backend>.toml` to the required mentions | The embedded page now has to name where machine-wide args live |
| `config/docs_test.go` (4 tests) | walk `engineSchema` too; `onlyBackend` → `takenBy`; the examples tree writes the engine files and asserts they reach the entries | The page grew a schema; the tests read definitions, never values |

Everything else that mentions args changed as a **fixture**, not as an
assertion: `serve`, `cli`, `tui` and `picks` build `[]config.Arg` where they
built `[]string`, and every composed-argv assertion in those packages is
byte-identical to before — that is the point.

### The argv-identity proof

`serve/command_test.go::TestAProfileWrittenAsKeysComposesTheArgvItComposedAsFlags`
— a real tree written to a temp dir, loaded, resolved and composed, against the
literal argv the same profile composed as flags:

```
llama-server -hf unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL --host 0.0.0.0 --port 8080
  --gpu-layers 99 --flash-attn on -c 262144 --parallel 1 --jinja --n-cpu-moe 24
```

Two cases reach it byte for byte: the profile migrated key for key, and the
same profile with `gpu-layers`/`flash-attn` lifted into `engines/llama.toml`.

**It caught the one thing STEP-6 must know**: a key spells exactly one flag.
`c = 262144` is `-c 262144`, not `--ctx-size 262144` — migrate each line to the
key of the flag it *carried*. The first draft of this fixture assumed otherwise
and failed.

### Decisions and deviations

- **Engine files: `args` only.** YAGNI — the router's own settings land in
  STEP-7. An unknown key there is refused loudly.
- **A broken engine file fails the load**, like `config.toml`. Rejected:
  marking every entry of that backend broken — it reports one file's mistake
  once per entry and points the reader at the wrong file. Recorded in CONFIG.md.
- **`engines/` is not scaffolded.** cria's tree writes stay root + `models/` +
  `AGENTS.md`.
- **`key = false` is not special.** `true` → bare flag is upstream's rule and
  the step's; everything else passes verbatim, so `fa = false` composes
  `--fa false` and llama-server refuses it by name. cria inventing a negative
  spelling would be flag knowledge. The docs page says to write the word the
  server takes (`flash-attn = off`).
- **Short aliases of more than one letter are unspellable** (`-ngl`, `-fa`):
  the key would compose as `--ngl`. Documented on the page and in CONFIG.md —
  write the long option. Same class as the repeatable-flag limit.
- **The preset spelling was not built.** It would have no caller until phase 3
  (`Flags`' doc comment names it, and `Arg.String()` already *is* one preset
  line). No dead code shipped.
- **The detail pane shows the two file levels, not the merge.** Engine-level
  keys and which of two lines wins are read off the command row under it.
  TUI.md records that, dated; a third ink for the engine level is a phase-3
  question, not a drive-by.

### Mutation checks

- `Flags` giving every key two dashes: reddens `engine::TestKeysBecomeTheFlagsAServerTakes`
  and `serve::TestAProfileWrittenAsKeysComposesTheArgvItComposedAsFlags`.
- `mergeArgs` keeping the *first* level's value instead of the last: reddens
  `config::TestResolveMergesTheLevelsBySpecificity` and
  `TestAnOverriddenKeyIsOneKey`.
- llama's `modelKey` drifting to `"hff"`: reddens
  `engine::TestEveryEnginesModelFlagIsTheKeyTheTreeRefuses/llama` and
  `config::TestEntryRulesReject/args_may_not_restate_hf` — the registry and the
  engine are held together in both directions.

Every production file was restored and the suite re-run green.

### Suite

`gofmt -l .` empty; `go vet ./...` clean; `go test -count=1 ./...`:

```
ok  cria 0.402s · cria/internal/cli 4.851s · cria/internal/config 1.002s
ok  cria/internal/engine 0.792s · cria/internal/format 1.038s
ok  cria/internal/hubapi 1.849s · cria/internal/hubcache 1.953s
ok  cria/internal/picks 1.303s · cria/internal/procs 2.304s
ok  cria/internal/selfupdate 2.262s · cria/internal/serve 5.199s
ok  cria/internal/tools 3.153s · cria/internal/tui 6.942s
```

No expected reds. The real tree is untouched and is still old-shape — it will
refuse to load against this binary until STEP-6 rewrites it.

### Docs updated in this step

- `docs/specs/CONFIG.md` — args are keys (the shape and why), the merge and its
  precedence, the collision rules, booleans, `engines/<engine>.toml`, the
  tree-wide failure rule, the model-key registry and key applicability as a set.
- `docs/specs/TUI.md` — what the args block reads now.
- `docs/ARCHITECTURE.md` — `engine.Flags` on the engine row.
- `internal/config/agents.md` and `README.md` — cria's own surfaces, moved to
  the new shape.

### What STEP-6 must know

1. **Migrate spelling for spelling.** `-c 262144` → `"c = 262144"`;
   `--ctx-size 262144` → `"ctx-size = 262144"`. Only then is the argv diff byte
   identical.
2. **`-ngl` and `-fa` have no key.** They become `gpu-layers` and `flash-attn`,
   which *does* change those tokens on the line — same option, different
   spelling. Those two profiles' diffs are option-identical, not byte-identical;
   name them explicitly in the migration record.
3. **Extraction to `engines/llama.toml` moves keys to the front** of the
   composed line. Compare extracted profiles as a set of flag/value pairs.
4. `args = []` is legal; a key with an empty value is not.
5. The survey still owes the repeatable-flag check (OVERVIEW ruling 4) — a
   profile passing one flag twice is now inexpressible and needs a decision.
