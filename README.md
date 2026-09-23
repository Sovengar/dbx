# dbx

> **Database x** — A modern TUI database client with native AI integration.

dbx is a terminal UI for databases designed for both humans and AI agents.

## Features

> **Keybindings**: press `?` inside dbx for the full, always-current keybind
> reference. The TUI is the single source of truth — this README does not list
> keys.

### Grid

> **Draft model**: All edits, inserts, and deletes are staged locally (with
> color feedback). Dump the generated SQL into the editor for review, or discard
> everything.

- **Draft editing** — Edits, inserts and deletes are staged before hitting the
  database; drafts are color-coded (green insert, blue edit, red delete).
  Dumping the SQL opens it in the editor; running it commits the transaction.
  Discarding restores the original values.
- **Yank** — Copy the selected row (SQL/JSON/CSV) to the clipboard, or export
  multiple selected rows to a file next to `.dbx.toml`.
- **Sort & filter** — Server-side `ORDER BY` cycling ASC → DESC → none, and an
  inline WHERE clause with autocomplete for columns, operators and values.
- **Pagination** — Next/previous page, jump to first/last page, and jump to a
  specific page.
- **Navigation** — Follow foreign keys (loads the referenced table with the FK
  filter and existing WHERE), and go back through navigation history restoring
  cursor and filter.
- **Find column** — Fuzzy search to jump to a column by name.
- **Preview** — A right panel shows the selected row as highlighted JSON (or the
  referenced row when the cursor is on a FK cell), autoclosed on narrow
  terminals. Filter/navigate the JSON with jq expressions, autocomplete and
  persistent history.

#### Grid Modes

The grid displays its current mode in the bottom-left corner:

- **NORMAL** — navigate rows and columns.
- **EDIT** — type to modify a cell, confirm to move on, `Esc` to cancel the
  edit (without discarding any draft).
- **FILTER** — column find.
- **WHERE FILTER** — type a WHERE clause; applying it re-queries the database.

#### Draft-based Editing

All changes (edits, inserts, deletes) are staged locally before being committed
to the database:

| Color  | Meaning                     |
| ------ | --------------------------- |
| 🟢 Green  | Pending insert row          |
| 🔵 Blue   | Modified cell (edit draft)  |
| 🔴 Red    | Row marked for deletion     |

No database changes occur until you execute the generated SQL from the editor.
Executing the SQL dumped from the grid commits its transaction when the
execution finishes, so the draft edits become durable. DML typed manually in the
editor stays inside an open transaction until you roll it back or run the next
statement (which commits it first). See
[DML Transactions](docs/FEATURES.md#dml-transactions).

### Explorer

Schema tree with fuzzy table filtering, create/drop table templates, DDL view,
and expand/collapse. Press `?` for the keys.

### Explorer Preview

Full-screen table detail view with tabbed panels: Overview (sizes, row stats,
vacuum status, counts), Columns, Constraints, Foreign Keys, Indexes, and an
Entity-Relationship diagram. Open it from any table in the explorer.

### Query Browser

Persistent query history and favorites, isolated per project. Browse, favorite,
delete, filter, and load a query into the editor. Every executed query is saved
automatically; duplicates are deduplicated and the list is capped at 500 entries
per project.

Storage: `~/.local/state/dbx/projects/{project_name}/query_history.json`.
Queries are fully isolated between projects.

### SQL Editor

Run SQL against the database with results shown in the grid. Features include
real-time, context-aware autocomplete (keywords, schemas, tables, columns),
previous/next executed-query history, syntax highlighting, and automatic schema
refresh after DDL.

#### Autocomplete Context

Suggestions are derived from the tokens of the current statement, so they follow
the active clause and ignore text inside strings and comments:

| Context | Shows |
|---------|-------|
| After `FROM`/`JOIN`/`INTO`/`UPDATE` (table position) | Schemas + tables |
| `schema_name.` | Tables in that schema |
| `table_name.` / `alias.` / `schema_name.table_name.` | Columns of that table |
| `SELECT` list | `*`, `DISTINCT`, referenced columns and functions |
| `WHERE`/`ON`/`AND`/`HAVING` | Columns, then operators, then values |
| `ORDER BY`/`GROUP BY`/`RETURNING`/`SET` | Columns |
| Default (typing a keyword) | SQL keywords + functions |

Prefix matches are ranked above fuzzy matches, a token you already typed is
never offered back, and accepting a completion replaces that token in place
(accepting a keyword twice never duplicates it).

```toml
[editor]
autocomplete = true
autocomplete_trigger = 1   # min characters before the popup opens on its own
```

### AI (NL → SQL)

Ask a question in natural language and dbx generates SQL for review before
executing. The same flow is available from the terminal via `dbx ask`, with
`--json` and `--sql-only` flags. Supports Anthropic, OpenAI, DeepSeek, Qwen,
OpenCode, and any OpenAI-compatible endpoint.

### Navigation & UI

- **Top bar** — active pane, `schema.table`, row count, WHERE filter, breadcrumbs.
- **Bottom bar** — keybinds of the current view, derived from the registry.
- **Connection picker** — switch between projects, toggling active/inactive.
- **Grid Preview** — focus row preview from the grid.
- **Explorer Preview** — focus table details from the explorer.
- **Command palette** — fuzzy search for any command (refresh, export, execute,
  focus, …).
- **Help** — scrollable overlay with every keybind, including widget modes.
- **Mouse** — click, double-click, scroll wheel, header click to sort.
- **Toast notifications** — success/error/info feedback.
- **Themes** — system, dark, light, nord, gruvbox, catppuccin.

## Dependencies

- **Go 1.22+**
- **PostgreSQL** (via pgx)
- **Clipboard support** (optional, for copy to clipboard):
  - **Linux (Wayland)**: `wl-clipboard` (`wl-copy`/`wl-paste`)
  - **Linux (X11)**: `xclip` or `xsel`
  - **macOS**: built-in `pbcopy`
  - **Windows**: built-in `clip.exe`

## Installation

```bash
# From source
go install github.com/buble/dbx@latest

# Or download from releases
gh release download buble/dbx
```

## Quick Start

```bash
# Connect via DSN
dbx postgres://localhost/mydb

# Or use .dbx.toml in your project
dbx

# Query mode (no TUI)
dbx query "SELECT * FROM users LIMIT 10" --json

# Natural language
dbx ask "show me all active users"
```

## Keybinds

Press `?` inside dbx to open the help overlay. It lists every keybind, grouped
by view, plus the widget modes (EDIT, FILTER, WHERE FILTER, query browser,
connection picker) and mouse actions.

The same registry that renders that overlay also drives execution, so what you
see is exactly what runs. Override any action under `[keybindings.custom]` in
your config, keyed by action ID.

## Configuration

### Project config (`.dbx.toml`)

Place in your project root:

```toml
[connections.local-dev]
driver = "postgres"
dsn = "postgres://localhost/mydb"

[connections.staging]
driver = "postgres"
dsn = "postgres://staging.example.com/mydb"
```

### Global config (`~/.config/dbx/config.toml`)

```toml
[theme]
mode = "system"

[keybindings.custom]
"global.ask" = "ctrl+a"

[ai]
provider = "anthropic"

[session]
enabled = true
retention_days = 30
```

See [docs/CONFIG.md](docs/CONFIG.md) for full reference.

## AI Integration

### Natural Language to SQL

Press `a` or run:

```bash
dbx ask "show me users who signed up last week"
```

dbx generates SQL, shows it for review, then executes on confirmation.

### Session Logs

Every query is logged to `$TMPDIR/dbx/sessions/` (defaults to `/tmp/dbx/sessions`):

```bash
# View session
cat /tmp/dbx/sessions/2026-09-11.jsonl

# Replay session
dbx replay 2026-09-11.jsonl
```

Override in `config.toml`:

```toml
[session]
dir = "/path/to/persistent/sessions"
retention_days = 30
```

### Schema Context for LLMs

Export schema for AI agents:

```bash
dbx context --json > schema.json
```

## CLI Reference

```bash
# Query with JSON output
dbx query "SELECT count(*) FROM users" --json

# List tables
dbx list tables --json

# Export table
dbx export users --format csv --output users.csv

# Pipe mode
echo "SELECT 1" | dbx pipe --json
```

See [docs/CLI.md](docs/CLI.md) for full reference.

## Documentation

- [Features](docs/FEATURES.md) — DML transactions, action naming, non-keybind prose
- [Architecture](docs/ARCHITECTURE.md) — Design decisions and patterns
- [Decisions (ADR)](docs/decisions/) — Architecture decision records
- [Config](docs/CONFIG.md) — Configuration reference
- [CLI](docs/CLI.md) — CLI command reference

## Development

```bash
# Clone
git clone https://github.com/buble/dbx.git
cd dbx

# Build
make build

# Run
make run

# Test
make test

# Lint
make lint

```

### Server-enforced READ ONLY verification

The ASK pane's SELECT-only guarantee is only *authoritative* when PostgreSQL
rejects mutations inside the READ ONLY transaction — the client-side validator
is defense in depth, not the guarantee.

The proving tests (`TestAsk_ReadOnlyTxRejectsDML`,
`TestAsk_ReadOnlyTxRejectsSelectBasedMutation`) run **automatically**: when
`DBX_TEST_DSN` is unset they start a real PostgreSQL via
[testcontainers-go](https://golang.testcontainers.org/) (`postgres:16-alpine`),
so a plain `go test ./...` verifies the guarantee wherever Docker is available
(including CI ubuntu runners, which ship Docker). If Docker is unavailable the
tests skip with a clear message.

To reuse an existing database instead of starting a container, set
`DBX_TEST_DSN` (fast path, no container):

```bash
DBX_TEST_DSN='postgres://user:pass@localhost:5432/db' go test ./internal/app -run TestAsk -v
```

Either way this is a **mandatory verification step**, not optional.
