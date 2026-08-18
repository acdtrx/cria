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
  principle 6); nothing comes from logs — the log itself is available as a raw tail.
- **Stop is global, start is scoped.** Stop/kill keybinds act on the running server
  no matter what is selected; only starting requires selecting an entry. A
  restart-last keybind covers the one-keypress swap-back.
- **UI preferences are state, not config**: active backend and last-started entry
  live in a small file under `~/.local/state/cria/`. The config tree stays
  human-owned; cria never records preferences there.

## Open

- Foreign-server surfacing, tool-check display, log tail presentation, exact
  keybinds and layout — settled when those screens are designed. The cache view's
  behavior is owned by `docs/specs/CACHE.md`; how it is reached and laid out is
  part of the layout question.
