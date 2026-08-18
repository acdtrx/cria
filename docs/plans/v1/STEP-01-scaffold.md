# Step 1 — module scaffold and CLI dispatch

**Phase 1 · Status: done (2026-08-18)** — all gates green (build, gofmt, vet,
test, linux cross-compile); manual transcript verified. Decisions taken during
implementation: unknown subcommand exits 2 (usage error, distinct from a failed
operation's 1); no `--help` surface (not in CLI.md); version placeholder
`0.1.0-dev` until step 15 tags.

## Intent

A buildable `cria` binary with the subcommand skeleton: bare `cria` (TUI
placeholder), `start`, `stop`, `status`, `docs` — each a stub that names itself
and exits non-zero as "not implemented". Establishes the layout, the lint
baseline, and the linux cross-compile check.

## Files likely touched

`go.mod`, `main.go`, `internal/cli/` (dispatch only), `.gitignore` (worktrees,
binary).

## Decisions made during planning

- Module path `cria`; imports are `cria/internal/<pkg>`.
- Stdlib `flag`/`os.Args` dispatch — no CLI framework (CODING-RULES §3; the
  surface is five subcommands, CLI.md).
- No dependencies enter at this step; each arrives with the step that needs it.
- A `version` constant in `main.go`, shown in the TUI header and `--version`.

## Acceptance criteria

- `go build` produces a binary; each subcommand prints a "not implemented yet"
  line to stderr and exits 1; unknown subcommands name the valid set.
- `gofmt -l .` empty, `go vet ./...` clean, `go test ./...` green (no tests yet
  is green), `GOOS=linux GOARCH=amd64 go build` succeeds.
