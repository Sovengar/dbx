# dbx — Roadmap

> dbx is a working TUI database client (PostgreSQL). This document tracks the
> work that is **still open**. Completed planning phases were removed; for what
> exists today see [FEATURES.md](FEATURES.md), [CONFIG.md](CONFIG.md) and
> [ARCHITECTURE.md](ARCHITECTURE.md).
>
> Last audit against the code: 2026-09-23.

Items are ordered by impact. Each one states the current state and what it would
take to close it.

---

## 1. Multi-database support

**Status:** not started — PostgreSQL only.

dbx talks to a single driver. There is **no `Driver` interface in the code**
today (it exists only as a sketch in [ARCHITECTURE.md](ARCHITECTURE.md)); the
grid, explorer and CLI call the postgres package directly.

To close it:

- Extract the `Driver` interface from the postgres package and route all data
  access through it (schema listing, select, execute, stats).
- Add drivers, in order of value: **SQLite** (pure Go, trivial local use),
  **MySQL**, then **MongoDB** (very different model: collections + aggregation
  pipeline + document viewer).
- Connection config already carries a `provider` field, so routing is a matter
  of selecting a driver by provider.

This is the largest remaining gap and the main thing that makes dbx
"PostgreSQL-specific".

---

## 2. Session logging — decide: implement or delete

**Status:** half-built and dead. **Worth resolving either way.**

`internal/ai/session/logger.go` implements a JSON Lines logger and reader, but
`NewLogger` has **zero callers** — nothing ever writes a session log. Meanwhile
`dbx replay` reads logs through the same file, so it currently has nothing to
read. The config keys `session.enabled` and `session.retention_days` are inert
(`session.dir` is only used as the replay search path).

Two coherent outcomes:

- **Wire it up.** Instantiate the logger on query execution (TUI and CLI), write
  JSON Lines to `session.dir` (timestamp, action, SQL, duration, rows, error),
  rotate by day, honor `enabled`/`retention_days`. This turns `dbx replay` into a
  real feature and gives agents a durable audit trail.
- **Delete it.** Remove the unused logger and keep `replay` reading the
  per-project query history that already backs the query browser
  (`~/.local/state/dbx/projects/{project}/query_history.json`). Simpler, but
  loses the "replay a session" concept.

Either way, the config surface and the code should agree.

---

## 3. SSH tunnels

**Status:** parsed but unused.

Project config accepts `ssh_tunnel` on a connection (`[connections.<name>]` in
`.dbx.toml`), but nothing reads it. Implement the tunnel: open an SSH connection,
forward to the target host/port, and rewrite the DSN to the local forwarded
address before handing it to the driver.

---

## 4. Bulk data — COPY protocol

**Status:** not implemented.

Writes today go through staged DML (one statement per row) or manual SQL. There
is no bulk path. Use PostgreSQL `COPY FROM`/`COPY TO` for fast import/export of
large tables, exposed from the grid and/or CLI.

---

## 5. Export formats

**Status:** partial.

Grid export covers **SQL, JSON and CSV** (to clipboard or a file). Missing:
Markdown output, custom delimiters, and compression. Lower priority than the
items above.

---

## 6. Pane management

**Status:** not implemented.

Panes are currently **mutually exclusive** (one visible at a time; only the grid
and its sidebar co-exist at width ≥ 100). There is no split/close/swap, no
drag-to-resize, and no layout persistence across runs. If multi-pane layouts are
wanted, this is a structural change to the router and render path, not a
cosmetic one.

---

## 7. Theme: real system detection

**Status:** stubbed.

`theme.mode = "system"` is the default but `detectSystem()` is a stub that
returns a hardcoded dark palette. Implement real terminal background/foreground
detection so `system` actually adapts. The named themes (dark, light, nord,
gruvbox, catppuccin) are complete.

---

## 8. Explorer fuzzy filter

**Status:** substring only.

The explorer filter does a case-insensitive **substring** match; the original
plan called for fuzzy matching. Nice-to-have.

---

## 9. Keybind modes

**Status:** not implemented.

The plan described vim / modern / emacs keybind modes. There is a single
registry (with per-context keys) and no mode field anywhere in the config. Given
that the registry already covers vim-style navigation, decide whether a mode
switch is worth the added surface — otherwise drop it from the roadmap.

---

## 10. AI: CLI review parity

**Status:** minor gap.

The TUI ASK pane is a multi-turn chat with an explicit review step before
executing. The CLI `dbx ask` executes immediately (`--sql-only` skips
execution). Consider adding an interactive confirm (or a `--dry-run`) for
parity. NL→SQL itself is done: Anthropic, OpenAI, DeepSeek, Qwen plus
OpenAI-compatible providers (opencode-go, pi, hermes, jcode), config- and
env-driven.

---

## 11. Release & documentation

**Status:** not started.

- No `.goreleaser.yaml`, no tags, no releases. README already documents install
  from source and from releases, so packaging is the missing half.
- No `docs/CLI.md` (the plan listed it; the CLI is documented in README).
- No contributing guide.

---

## 12. Smaller polish

Low priority, kept for completeness:

- Zebra striping in the grid.
- Hover highlight in the explorer.
- External editor (`ctrl+g`) from the SQL editor.
- Right-click context menu and editor mouse-wheel scroll.
- Naming query-history favorites (the `Name` field exists; no UI).

---

## Changelog

| Date | Change | Reason |
|------|--------|--------|
| 2026-09-11 | Initial plan (phases 0–5) | Start |
| 2026-09-23 | Rewritten as a roadmap | The original phases mixed shipped, fictional and pending work; completed phases removed, remaining work re-scoped against the code |
