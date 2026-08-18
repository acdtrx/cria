# TUI — interaction model

The TUI is a viewer and dispatcher over what the config tree declares and the state
records report; it holds no serving state of its own. This spec owns the interaction
model — the contracts of how the user moves through cria. Screens, layout and exact
keybinds get detailed as they are built.

## Settled (2026-08-18)

- **Backends are separate lists, never one mixed list.** One backend is active in the
  UI at a time; a keybind toggles. The active backend persists across launches —
  running llama vs mlx is a deliberate, sticky choice, not a per-session question.
- **The entry list is the picker.** It shows the active backend's entries at quant
  granularity — picking an entry picks model, quant and params in one gesture. A
  detail pane shows the highlighted entry's full contents and the exact command line
  cria would launch; names don't need to be memorable, the pane carries the truth.
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
- **UI preferences are state, not config**: active backend and last-started entry
  live in a small file under `~/.local/state/cria/`. The config tree stays
  human-owned; cria never records preferences there.
- **Every text color clears WCAG AA (≥4.5:1) against a dark terminal ground,
  enforced by a palette test** (settled 2026-08-18, after first real use found
  the dim tones illegible): muted tones are muted by hue and saturation, never
  by darkness; selection is a background band whose foreground pairs pass AA on
  the band itself; color marks meaning — phase, backend, keys, field labels —
  not decoration. The palette lives in one table that the styles are tested to
  draw from exclusively.

## Open

- Foreign-server surfacing, log tail presentation, exact key choices and layout
  details — settled when those screens are designed. The cache view's behavior is
  owned by `docs/specs/CACHE.md`.
