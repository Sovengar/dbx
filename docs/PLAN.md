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

## 2. Session logging

**Status:** implemented.

`internal/ai/session/logger.go` writes and reads a daily NDJSON log under
`session.dir`. The writer is instantiated by the TUI (`app.NewModel`) and by the
CLI — `dbx query`, `dbx pipe` and `dbx ask` log the statements they execute — so
`dbx replay` has real logs to read. `session.enabled` (default true) turns it off
and `session.retention_days` (default 30) prunes old files; a log that cannot be
opened disables logging instead of failing the command. `--sql-only`, `--dry-run`
and a declined `--confirm` execute nothing and therefore write nothing.

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

**Status:** implemented.

Grid export covers **SQL, JSON, CSV and Markdown** (to the clipboard for a
single row, to a file for several). The delimited format honours
`ui.export_delimiter` — one literal character, or `tab` / `\t` for a real tab
byte, which names the file `.tsv`; an unusable value falls back to a comma so a
mistyped preference cannot empty an export. `ui.export_compress` gzips every
**file** export and appends `.gz` after the format extension; clipboard copies
and yanks are never compressed.

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

**Status:** implemented.

`theme.mode = "system"` (the default) resolves the palette from the environment.
The terminal wins first: `COLORFGBG`, whose last field is the background index,
separates the dark half of the ANSI palette (0–6, 8) from the light half (7,
9–15). When the terminal does not answer, the desktop is asked —
`defaults read -g AppleInterfaceStyle` on macOS, where the missing key *is* light
mode, and `gsettings get org.gnome.desktop.interface color-scheme` on Linux.
Anything else falls back to the dark palette, so an environment that answers
nothing keeps the colours it had before. The detected theme reuses the designed
dark/light palettes and keeps the name `system`.

The probes (`getenv`, `run`, `goos`) are a `systemProbe` value rather than direct
`os.Getenv`/`exec.Command` calls, so the suite injects a fake environment instead
of reading the developer's terminal. The named themes (dark, light, nord, gruvbox,
catppuccin) never run detection.

---

## 8. Explorer fuzzy filter

**Status:** substring only.

The explorer filter does a case-insensitive **substring** match; the original
plan called for fuzzy matching. Nice-to-have.

---

## 9. Keybind modes

**Status:** dropped — decision made 2026-10-10.

No keybind modes. The registry already covers vim-style navigation, and a mode
switch would add surface without solving a real problem. This item is closed.

---

## 10. AI: CLI review parity

**Status:** done — 2026-10-18.

The TUI ASK pane is a multi-turn chat with an explicit review step before
executing. The CLI `dbx ask` gained both halves of the parity: `--dry-run`
connects, reads the real schema, generates and prints the SQL without executing
it, and `--confirm` prompts `Execute this query? [y/N]` before running. Default
behaviour is unchanged (generate and execute), so existing scripts keep working.
NL→SQL itself is done: Anthropic, OpenAI, DeepSeek, Qwen plus OpenAI-compatible
providers (opencode-go, pi, hermes, jcode), config- and env-driven.

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
| 2026-10-18 | Closed §2 (session logging) | The writer existed but only the TUI called it; the CLI now logs too |
