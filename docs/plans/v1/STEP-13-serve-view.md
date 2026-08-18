# Step 13 — serve view

**Phase 5 · Status: not started**

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
