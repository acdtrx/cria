# TUI — interaction model

The TUI is a viewer and dispatcher over what the config tree declares and the state
records report; it holds no serving state of its own. This spec owns the interaction
model — the contracts of how the user moves through cria. Screens, layout and exact
keybinds get detailed as they are built.

## Settled (2026-08-18)

- **Backends are separate lists, never one mixed list.** One backend is active in the
  UI at a time; a keybind toggles. The active backend persists across launches —
  running llama vs mlx is a deliberate, sticky choice, not a per-session question.
  The key walks the engines cria has and wraps at the end (amended 2026-08-31),
  so every backend is reachable and none is a dead end; their display order and
  the colour each name is drawn in are the TUI's own, keyed by engine id — a
  backend with no colour of its own is drawn as plain text rather than borrowing
  another's.
- **The toggle chooses what the screen is about, not which entries it filters**
  (settled 2026-08-31, when the router became the third engine). Two of the three
  positions are an entry list filtered by the backend those entries declare; the
  router's is a list of the models it holds, because no entry declares the router
  and what it serves is state of its own (`docs/specs/CONFIG.md`). The walk is
  therefore over every engine cria has rather than over the backends an entry may
  name, and a preferences file naming something cria does not serve is refused
  against the same set. Rejected: a separate view with its own key — the router is
  one of the ways this host serves models, and the key that asks "which way" is the
  one that already exists.
- **The entry list is the picker.** It shows the active backend's entries —
  picking an entry picks everything in one gesture: for a flat entry that is
  model, quant and params directly; for an entry with choices its current picks
  complete the gesture (amended 2026-08-22, variations moved inside entries). A
  detail pane shows the highlighted entry's full contents, its current picks, and
  the exact command line cria would launch; names don't need to be memorable, the
  pane carries the truth.
- **A persistent status box shows the running server**, regardless of list
  selection: entry, backend, repo:quant, pid, port, phase (starting / downloading
  with progress / running / unhealthy), uptime, memory (RSS) and CPU via `ps`, plus
  what the backend's documented endpoints expose (`/health`; llama-server's
  `/props`). Everything shown is obtainable from stable interfaces (`docs/cria.md`,
  principle 6); nothing comes from logs — the log itself is available as a raw
  tail. The box sits at the top and appears in **every** view, the cache view
  included; it carries no key hints of its own. When nothing is running it shows
  the last-started server (from UI preferences, so across sessions too) in a
  `stopped` display state — the server-group keys keep a target: restart-last
  always acts on what the box shows.
- **Stop is global, start is scoped.** Stop/kill keybinds act on the running server
  no matter what is selected; only starting requires selecting an entry. A
  restart-last keybind covers the one-keypress swap-back. The one start with no
  row to be scoped to is the router's (amended 2026-09-05): it is composed from
  the whole tree rather than from one entry, so its own view starts it and the
  key sits in the server group with the stop that ends it.
- **All keybinds live in one bottom bar, grouped by scope**: *selection* keys read
  the highlighted item (start; delete in the cache view), *server* keys act on the
  running server from anywhere (stop, log, restart-last; dismiss while an exited
  record shows), *global* keys navigate (backend toggle, view switch, tools,
  quit). The groups make "what works right now" legible without a help screen.
- **The entry list marks each entry cached / not cached** — from the same cache
  walk the cache view uses — so starting reads as "serve now" vs "download first"
  before the keypress.
- **The tools report is a pane toggled by a global keybind**, hidden by default
  (`docs/specs/TOOLS.md` owns its content).
- **The bench pane** (settled 2026-08-19, user-designed): a global keybind opens
  the session's bench log — every completed sweep appended with its per-size
  table, kept for the session only, never persisted. ⏎ inside the pane starts a
  bench: one live server measures immediately, several arm the pick ("which
  server to bench"), one bench at a time. The pane always runs the default
  sweep — sizing flags are CLI territory. Closing the pane leaves a running
  bench going; its completion lands on the notice line, its result in the log.
  The measurement contract is `docs/specs/SERVE.md`'s.
- **The notice line is one permanently reserved row** under the status box
  (settled 2026-08-18): it carries only what the boxes cannot show — refusals,
  errors, outcomes with information (bytes reclaimed, a freed port), and the
  question a server key asks — never a restatement of visible state (started/
  stopped confirmations and in-flight action text are the box's job on the next
  tick). esc dismisses a visible notice; its order is overlay, then notice,
  then back out of the cache view, and the bar names whichever is next.
- **A server key with several eligible targets asks which** (settled
  2026-08-18, user-designed): stop/kill/log/dismiss with more than one
  candidate move the selection into the status box itself — eligible rows only
  (stop/kill: live; dismiss: exited; log: any) — j/k to pick, ⏎ runs the armed
  action, esc cancels; the notice line prompts ("which server to stop") and the
  keyboard returns to the view it left. One candidate acts immediately, as
  before. Restart joins the pick over live servers (amended 2026-08-18); with
  nothing live it still acts on the last-started entry.
- **An action shows in the box at the keypress** (settled 2026-08-18): the
  moment a key acts, the target's phase column carries the verb —
  starting…/stopping…/killing…/restarting…, a fresh start as its own minimal
  row — as display state, and the action's completion triggers an immediate
  observation so the box converges in milliseconds rather than at the next
  tick. Status lives in the box, including the status of what cria is doing
  right now.
- **UI preferences are state, not config**: active backend, last-started entry
  and entry groups live in a small file under `~/.local/state/cria/`. The config
  tree stays human-owned; cria never records preferences there.
- **Entry groups** (settled 2026-08-21, user-designed): named, ordered groups
  partition the entry list under muted headings — organization for a list that
  grows test variations faster than it sheds them. Contracts:
  - Membership and group order are UI preferences (`ui.json`), never the config
    tree and never the entry files. An entry belongs to at most one group. Ids
    whose entry file is gone are skipped on render and pruned on the next prefs
    write; an id whose file is merely refused (broken) keeps its membership —
    a typo must not unfile the entry — and shows in the broken tail until the
    file reads again (amended 2026-08-21, caught in build). With no groups
    defined the list renders exactly as before.
  - Groups span backends: each backend's list shows only its own members under
    a heading; a heading with no members in the active backend is hidden,
    unless the group is entirely empty (so a just-emptied group stays findable).
    Ungrouped entries trail under a muted `ungrouped` heading (shown only when
    groups exist and the tail has rows to show — a heading over nothing is
    noise); broken entry files stay last, ungroupable.
  - The cursor never stops on headings — the entry list stays the picker, and
    within a group entries keep the tree's alphabetical order; only groups are
    manually ordered.
  - **Moving is the front door.** A selection key arms a pick over the group
    headings themselves (all groups shown while armed, plus `ungrouped` for a
    grouped entry and `new group…`); ⏎ files the entry, esc cancels. Groups are
    created only through `new group…` — a group exists because an entry needed
    it, so there is no separate create key. With no groups yet that is the only
    answer, so the key opens the name input directly rather than arming a pick
    of one; esc from that input steps back to the headings, the question it was
    reached from still being up.
  - **Group management is its own small mode** over the headings — every group
    shown while it is up, the ungrouped tail never a stop: reorder by
    pick-up-and-carry — ⏎ takes the group under the cursor, the cursor keys
    carry it (clamped at both ends, every step written as it lands), ⏎ or esc
    sets it down, and rename/disband wait until it is down (amended 2026-08-21
    after live use, user-designed; replaced a shifted-key nudge — moving the
    held group with the same keys that move the cursor is the more natural
    gesture) — rename (the name input opens on the current name), disband
    (members return to ungrouped; the notice line reports the outcome, counting
    the entries that actually come back — an id whose entry file is gone is
    dropped by the same write, not ungrouped). Nothing is destroyed by a
    disband, so nothing confirms it. Every change is written as it lands, so
    leaving the mode is a way out and never a discard; the mode ends when its
    last group is disbanded, leaving the notice behind it. A held group rides
    a band of its own hue — teal, meaning exactly "in your hand" and nothing
    else in cria — so held is never read as selected (settled 2026-08-21,
    user-chosen over a mauve tint from the recolor proposal). Rejected: cursor
    landing on headings with selection keys changing meaning there — it slows
    the main picking gesture for a rare operation.
  - **Names are typed on the notice line** (`new group: qwen-tests▌`) — the
    reserved row keeps the list visible and costs no layout shift; ⏎ confirms,
    esc cancels; empty, duplicate and `ungrouped` names are refused there, in
    place, with the typed name left to correct. Rejected: a confirm-style panel
    — heavier than a one-line name needs.
- **The choice picker** (settled 2026-08-22, user-designed): a selection key on
  an entry with choices opens a box floating centered over the list, sized to
  its rows, the list still visible around it (amended 2026-08-23 after live
  use, user-chosen: the picker configures the entry the cursor is on, and
  standing in the list's pane read as leaving the list rather than configuring
  in place) — one row per choice, the options laid along it; ↑/↓ moves between
  rows, ←/→ picks along one; ⏎ or esc closes. The current pick rides a small
  mauve band — the picked chip — in the picker and the detail pane alike
  (amended 2026-08-23, user-chosen over a star suffix: a background is the
  mark, so the option's name stays the only text; the star remains `cria
  list`'s, whose output draws no colour). Contracts:
  - Every change writes the pick to `choices.json` at the keypress — picks are
    the entry's new defaults "until I change them", so leaving the modal is a
    way out and never a discard (same doctrine as group management). One-shot
    launches are CLI territory; the picker only sets defaults.
  - The detail pane keeps showing the composed command for the current picks —
    picking and seeing the resulting command line are one loop. An entry that
    outgrows the pane loses fact lines behind an ellipsis, never the command
    (settled 2026-08-25).
  - The pane's facts are the launch's, not the file's: the args block reads as
    the effective args — the file's own lines first, then what the current
    picks contribute, in composition order, one flag group to a line — and repo
    and quant are the pair a start would use, shown even when the declared
    value lives only in the options. Everything a pick sourced is drawn in the
    pick's ink — mauve means sourced-from-the-current-pick wherever it appears
    (settled 2026-08-25, user-requested: choices had made their args and quant
    invisible outside the command line).
  - The block is the entry's own files, not the merge (settled 2026-08-31, when
    args gained an engine-wide level): what the machine serves every entry of
    that backend with, and which of two lines for one flag wins, are read off
    the command line under it — the one place the effective launch is spelled
    out in full.
  - Start launches the stored picks; restart-last replays the *record's* picks,
    not the current defaults — a swap-back reproduces what ran, records being
    self-contained (`docs/specs/SERVE.md`). The status box names the running
    combination.
  - A flat entry offers no picker; the key does nothing there.
- **The router's view** (settled 2026-08-31, `docs/specs/SERVE.md` owns what the
  router is): the toggle's third position, laid out exactly as the entry view is —
  a list, a detail pane beside it, the same cursor and the same picker floating
  over the list — because it is the same gesture: stand on a thing and read what
  serving it would come to. Contracts:
  - **The rows are every profile the router could serve** (amended 2026-09-05,
    user-requested: "see all the profiles in the router UI and be able to pick
    which to include"): every llama entry of the tree, under the same group
    headings, in the same order the entry list draws them — the same list asked a
    different question, so a profile sits in the same place in both positions of
    the toggle. An mlx entry is never on it (another program serves it, so the
    mark could not be set); an entry file cria refused is, for the reason it
    appears under both other lists — a file nobody can see is one nobody fixes;
    and an included id the tree no longer declares trails at the end, with the
    reason it can no longer be served. Inclusion is never auto-pruned
    (`docs/specs/SERVE.md`), so that trailing row *is* how a ghost is seen and
    taken out. Replaced: a list of the store's models alone — it answered "what
    would the next start serve" and gave no way to see, or change, what else
    could be in it.
  - **A leading mark says whether the router holds the profile**: ● held, ○ not,
    · a store cria could not read — the entry list's own glyphs asked this list's
    question, with green meaning there what it means there ("the next start
    serves it" against "starting it serves what is on disk"). "Not included" is
    never claimed for a store that could not be read. The mark is fixed width and
    leads the row, so setting it moves nothing beside it.
  - **␣ sets it** (settled 2026-09-05, user-designed — a checkbox before the
    model, toggled by one key). It holds the profile under the cursor, or stops
    holding it, writing `models.json` **at the keypress**: an inclusion is a
    decision "until I change it", so leaving the view is never a discard — the
    same doctrine the picker and the group modes are built on. Including takes
    the entry at its config defaults (`{}` in the store); excluding drops the
    picks the router held it under, which is what exclude means. The bar spells
    the key by the row it is standing on — `␣ include` or `␣ exclude` — the way
    ⏎ spells grab and place. A refused entry file can only come out, never in:
    the key that would say which program serves it is exactly the key that could
    not be read. A store cria could not read offers neither spelling.
  - **`cria router include|exclude` stay** (amended 2026-09-05): the gesture and
    the verb write one store, and the verb is how a coding agent settles an
    inclusion without a terminal to look at. Mechanism and trigger are separate.
  - **`p` edits an inclusion, and only an inclusion.** The store's key *is* the
    inclusion, so opening the picker on a profile the router does not hold would
    include it as a side effect of a key that means "edit the picks". On an
    excluded row the key is not drawn and does nothing — the answer a flat entry
    already gets.
  - **Load and unload stay CLI verbs** (settled 2026-08-31, upheld 2026-09-05
    when inclusion became a gesture): including is a decision with no gate,
    written and done; loading is a request about memory whose refusal — a model
    answering somebody right now — is lifted by `--ignore-busy`, and this screen
    has no vocabulary for an override. That one would need a modal to be honest,
    and a modal is what the picker doctrine exists to avoid.
  - **⏎ starts the router** (settled 2026-09-05, user-found: the bar taught stop
    and never start, so starting one was CLI-only). This screen is about a server
    rather than about a row, so the key that starts things starts it, and it is
    drawn only while there is none running — a server key rather than a selection
    one, reading as the pair of the stop beside it. The sequence and every
    refusal are `cria router start`'s, in the same order. A held port refuses on
    the notice line rather than in the held-port modal: that modal offers to kill
    on one entry's behalf, and the router is not an entry.
  - **Each row carries the router's own word**, and cria's phase is only what
    colours it. Three of the six states a router publishes have no phase in cria's
    vocabulary, so a row with no colour is normal and never missing data. The
    state column is a fixed width — a row that reflows as a model loads is a row
    nobody can read. The word is drawn onto a row whatever the store now says
    (amended 2026-09-05): the store is the next start and the running router is
    right now, so a profile excluded a minute ago still reads `loaded` while that
    router serves it, and one included since the router started reads *not
    served* — with the pane saying the preset is composed at every start — rather
    than vanishing from a list the operator just wrote. A profile neither of them
    holds has an empty state column: the mark in front of it is the whole answer,
    and the column keeps its width.
  - **A model the preset could not carry is a row, not a footnote**: it reads
    `skipped`, and the pane carries the reason in the words whoever included it
    has to act on. A dropped model hidden away is a client's 404 hours later.
  - **The pane is the picking-and-seeing loop in the router's flavour**: the entry
    through the picks the *router* holds it under, and the preset section a start
    would write for it — where the entry view shows the composed command line. `p`
    opens the same picker over the same axes and writes the router's own store, so
    one entry carries two combinations and each is edited where it is used. A
    profile the router does not hold is read through the entry's config defaults —
    what including it would hold it under — with a `not included` state line and
    no preset section, because nothing was composed for it.
  - **An empty list is a tree with no llama entry in it**, so it says where
    entries are written and what prints the schema, exactly as the entry list's
    does. It no longer names `cria router include`: this list is where inclusion
    begins now.
- **The router is a server in the status box** (settled 2026-08-31): cria started
  it, and the box shows what cria started — hiding it would make "what is running
  here" a lie in the one view that answers it. Its row names the engine where an
  entry's names the entry, and no model, because it serves none. The server keys
  reach it as they reach any row: stop, kill, log and dismiss. Two do not —
  **restart** replays the combination one record was composed with and the router
  was composed from no entry, and **bench** measures one model's server where the
  router fronts several — so the bar stops drawing `r` for a box holding only the
  router, rather than offering a key that does nothing.
- **Every text color clears WCAG AA (≥4.5:1) against a dark terminal ground,
  enforced by a palette test** (settled 2026-08-18, after first real use found
  the dim tones illegible): muted tones are muted by hue and saturation, never
  by darkness; selection is a background band whose foreground pairs pass AA on
  the band itself; color marks meaning — phase, backend, keys, field labels —
  not decoration. The palette lives in one table that the styles are tested to
  draw from exclusively. The values are **Catppuccin Mocha** (settled
  2026-08-21, chosen from a side-by-side proposal; Macchiato rejected as
  near-identical in the accents, and cria paints no background where the
  flavors actually differ): accents on the roles by hue family, Surface1 as
  the selection band (amended 2026-08-21: Surface0, picked from the browser
  proposal, read as barely-there on the real terminal — dim red and heading
  ride lit variants on the brighter band), Teal as the carry band, Mauve as
  the picked chip (amended 2026-08-23) — every pair still answering to the AA
  test on the black ground.

## Open

- Foreign-server surfacing, log tail presentation, exact key choices and layout
  details — settled when those screens are designed. The cache view's behavior is
  owned by `docs/specs/CACHE.md`.
