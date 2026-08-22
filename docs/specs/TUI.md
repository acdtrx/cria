# TUI — interaction model

The TUI is a viewer and dispatcher over what the config tree declares and the state
records report; it holds no serving state of its own. This spec owns the interaction
model — the contracts of how the user moves through cria. Screens, layout and exact
keybinds get detailed as they are built.

## Settled (2026-08-18)

- **Backends are separate lists, never one mixed list.** One backend is active in the
  UI at a time; a keybind toggles. The active backend persists across launches —
  running llama vs mlx is a deliberate, sticky choice, not a per-session question.
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
  restart-last keybind covers the one-keypress swap-back.
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
  an entry with choices opens a box floating over the list's corner, sized to
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
    picking and seeing the resulting command line are one loop.
  - Start launches the stored picks; restart-last replays the *record's* picks,
    not the current defaults — a swap-back reproduces what ran, records being
    self-contained (`docs/specs/SERVE.md`). The status box names the running
    combination.
  - A flat entry offers no picker; the key does nothing there.
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
