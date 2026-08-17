# Backlog

Deferred bugs and ideas (see `CLAUDE.md`, Scope). Entries that grow graduate to
`docs/plans/<topic>/`. A resolved entry is removed in the same change — git history is
the archive; this file lists only what is still open.

Entry format: a bolded title, what it is in a sentence or two, and the **revisit
trigger** — the observed condition that would make it worth building (features earn
their place). Date rulings inline: `(ruled YYYY-MM-DD: wait to see if needed.)`.
Group entries under headings as themes emerge.

## Platform & distribution

- **Linux as a supported platform.** `linux/amd64` must keep compiling (`CLAUDE.md`,
  Project Facts) but nothing is tested or distributed. Revisit trigger: a Linux
  machine joins the household, or a friend on Linux asks for a binary.
- **Distribution beyond scp.** Browser-downloaded binaries carry the quarantine
  xattr; outsiders would need `xattr -d`, a Homebrew tap, or Developer-ID
  notarization. Revisit trigger: the first person outside the household asks for a
  binary.

## Reach

- **Remote-host backend.** One TUI driving other hosts over SSH instead of copying
  the binary per host. Revisit trigger: cria runs on 3+ machines and per-host SSH
  sessions become a felt daily friction. (ruled 2026-08-18: v1 is single-host; keep
  the host-access layer clean enough that a remote backend could slot in.)

## Telemetry

- **Richer server stats** (throughput, slots, memory). Revisit trigger: llama.cpp or
  mlx-lm expose a documented, maintained API for it *and* the raw log tail proves
  insufficient in daily use. (ruled 2026-08-18: log parsing is permanently rejected —
  predecessor llama-runner broke on every llama.cpp release doing this; endpoints
  like `/props` or `/metrics` would qualify, a log format never will.)
