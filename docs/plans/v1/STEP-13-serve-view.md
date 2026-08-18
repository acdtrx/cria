# Step 13 — serve view

**Phase 5 · Status: done (2026-08-18)** — suite green (66 tui tests), all
gates pass; the full loop verified live in the real TUI with a real
llama-server: list with cached dots and a broken entry shown dimmed with its
key error, ⏎ start → starting → running on the ticker, raw log tail, stop,
restart-from-stopped, foreign drill (modal with pid/command/cwd, k kill →
port re-checked and freed, deliberate second ⏎), crash drill (kill -9 →
exited crash report → d dismiss), stacked layout at 80 columns, empty-state
pointer at `cria docs`. Decisions: config tree re-read every tick (an agent
writing entries while the TUI is open is the expected flow); cache walk only
when the serve view is visible or a download runs; tool check execs once, a
start asks fresh; serve exports ComposedCommand (detail pane shows exactly
what spawns) and Manager.KillHolder (refuses pids of live records — those are
stopped by entry); modal/log hold the keyboard and the keybar swaps scope;
refused entry files appear under both tabs (their backend key is what failed);
log view is a 200-line tail, no scrollback (deliberate).

## Intent

The main screen per TUI.md and the mockups: backend tabs, entry list with
cached dots, detail pane with the composed command line, start/stop/restart/
dismiss wired through serve, the port-busy and foreign-holder modals, and the
raw log tail screen.

## Files likely touched

`internal/tui/` (serve view, modals, log view).

## Decisions made during planning

- Cached dots come from the step 5 walker's completeness answer, refreshed with
  the status ticker — no separate mechanism.
- The foreign-holder modal is the one place the kill offer lives (SERVE.md).
- Log tail: follow the log file by polling reads on the ticker — display only,
  no line interpretation (principle 6). Simple last-N-lines view; fancier
  scrollback only if daily use demands it (features earn their place).
- Restart-last and dismiss act on the status box target (TUI.md).

## Acceptance criteria

- Manual pass on the dev Mac recorded here: browse both tabs, start from
  selection, watch downloading → running on a fresh model, tail the log, stop,
  restart-last, crash a server (kill -9 externally) and see the exited record +
  dismiss, port-busy both flavors (ours and foreign, incl. kill).
- Suite green; result recorded here.
