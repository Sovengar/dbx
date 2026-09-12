# dbx

> **Database x** — A modern TUI database client with native AI integration.

dbx is a terminal UI for databases designed for both humans and AI agents.

## Features

### Grid

| Action                   | Behavior                                                                                          |
| ------------------------ | ------------------------------------------------------------------------------------------------- |
| Yank one row (`y`)       | Copies the row in the selected format (SQL, JSON, CSV) to clipboard                               |
| Yank multiple rows (`y`) | Exports to file in the same directory as `.dbx.toml` (`<schema>_<table>.<ext>`)                   |
| Sort (`s`)               | Cycles ASC → DESC → none on current column (server-side `ORDER BY`)                               |
| Filter (`/`)             | Inline WHERE clause with autocomplete for columns, operators, and values                          |
| Edit cell (`enter`)      | Inline editing with type-aware parsing (int, float, bool), commit with `enter`                    |
| Insert row (`i`)         | Staged insert: creates a pending row in the grid (shown in INSERT mode)                           |
| Commit pending (`Ctrl+s`) | Executes INSERT for all pending rows to the database                                             |
| Delete row (`d`)         | Deletes the selected row                                                                          |
| Multi-select (`space`)   | Toggles row selection for bulk operations                                                         |
| FK navigate (`o`)        | Follow foreign key: loads referenced table with FK filter AND existing WHERE, syncs explorer     |
| Go back (`H`)            | Return to previous table in navigation history, restores cursor and filter                       |
| Find column (`f`)        | Fuzzy search to jump to a column by name                                                          |
| Pagination               | `n`/`p` next/prev page, `N`/`P` first/last page, `F1`–`F9` jump, digit keys for multi-digit pages |
| Tabs                     | `1`–`5` switch between Records, Columns, Constraints, Foreign Keys, Indexes                       |
| Preview                  | Right panel shows selected row as highlighted JSON, autoclosed when width < 100                   |

#### Grid Modes

The grid has two modes displayed in the bottom-left corner:

- **NORMAL** — Default mode. Navigate with `hjkl`, `G`/`g`, `Ctrl+u`/`Ctrl+d`. Press `i` to start insert mode, `enter` to edit a cell, `d` to delete, `y` to yank.
- **INSERT** — Active when inserting a new row. Press `i` to create a staged pending row (shown in green). Navigate columns with `Tab` (wraps). Press `enter` to commit the cell value locally and move on. Press `Esc` to discard all pending rows. Press `Ctrl+s` to execute INSERT statements for all pending rows.

Pending rows are visually distinct (green tint) and stored locally until committed with `Ctrl+s`. No database changes occur until you commit.

### Explorer

| Action                   | Behavior                                             |
| ------------------------ | ---------------------------------------------------- |
| Open table (`enter`)     | Loads table data into grid                           |
| Toggle columns (`space`) | Expand/collapse column list under a table            |
| Filter (`/`)             | Fuzzy filter across all table names                  |
| New table (`n`)          | Opens editor pre-filled with `CREATE TABLE` template |
| Drop table (`d`)         | Opens editor pre-filled with `DROP TABLE`            |
| View DDL (`v`)           | Opens editor with `pg_get_tabledef` query            |

### SQL Editor

| Action                      | Behavior                                                         |
| --------------------------- | ---------------------------------------------------------------- |
| Execute (`ctrl+enter`)      | Runs SQL against database, results shown in grid                 |
| History (`ctrl+p`/`ctrl+n`) | Navigate previous/next executed queries                          |
| Syntax highlighting         | Keywords, strings, numbers, comments, functions, operators       |
| Auto-refresh                | Schema reloads after DDL statements (CREATE/DROP/ALTER/TRUNCATE) |

### AI (NL → SQL)

| Action         | Behavior                                                                        |
| -------------- | ------------------------------------------------------------------------------- |
| Ask (`a`)      | Type natural language, dbx generates SQL, shows for review, executes on confirm |
| CLI `dbx ask`  | Same flow from terminal, with `--json`, `--sql-only` flags                      |
| Multi-provider | Anthropic, OpenAI, DeepSeek, Qwen, OpenCode, and any OpenAI-compatible endpoint |

### Navigation & UI

| Action                | Behavior                                                             |
| --------------------- | -------------------------------------------------------------------- |
| Focus cycling (`e`)  | Toggle Explorer pane                                                  |
| Breadcrumbs           | Navigation history path above grid (`schema.table → schema.table`)   |
| Command palette (`:`) | Fuzzy search for any command (refresh, export, execute, focus, etc.) |
| Help (`?`)            | Overlay showing all keybinds, scrollable                             |
| Mouse                 | Click, double-click, scroll wheel, header click to sort              |
| Toast notifications   | Success/error/info feedback for all operations                       |
| Themes                | System, dark, light, nord, gruvbox, catppuccin                       |

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

dbx uses an **action-based keybind system**:

| Key | Action                           |
| --- | -------------------------------- |
| `a` | Ask AI (Natural language to SQL) |
| `c` | Change (edit cell)               |
| `d` | Delete (row or table)            |
| `x` | Export data                      |
| `f` | Filter                           |
| `g` | Go to (first/last)               |
| `H` | Go back (navigation history)     |
| `i` | Insert                           |
| `n` | Next page                        |
| `o` | Open FK reference                |
| `p` | Previous page                    |
| `q` | Quit                             |
| `r` | Refresh                          |
| `s` | Sort                             |
| `u` | Update                           |
| `v` | View DDL                         |
| `y` | Yank (copy)                      |

Press `?` anywhere to see all keybinds.

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

[keybindings]
mode = "vim"  # or "modern" or "emacs"

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

- [Architecture](docs/ARCHITECTURE.md) — Design decisions and patterns
- [Keybinds](docs/KEYBINDS.md) — Complete keybind reference
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
