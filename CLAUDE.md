@AGENTS.md

Claude Code reads `CLAUDE.md`, not `AGENTS.md`; the import above bridges the two. All
rules live in `AGENTS.md` — add instructions here only when they apply to Claude Code
alone.

- Build in a git worktree under `.claude/worktrees/<branch>` (gitignored), one per build:
  the main checkout stays on `main`, clean, for discussion, planning and review. Keep a
  worktree disposable: commit as you go, so it never holds anything worth losing. Run
  `git worktree prune` after deleting one.
