# dbx

> **Database x** — A modern TUI database client with native AI integration.

dbx is a terminal UI for databases designed for both humans and AI agents.

## Features

### Grid

> **Draft model**: All edits, inserts, and deletes are staged locally (with color feedback) and committed atomically with `Ctrl+S`. Press `D` to discard.

| Action                   | Behavior                                                                                          |
| ------------------------ | ------------------------------------------------------------------------------------------------- |
| Yank one row (`y`)       | Copies the row in the selected format (SQL, JSON, CSV) to clipboard                               |
| Yank multiple rows (`y`) | Exports to file in the same directory as `.dbx.toml` (`<schema>_<table>.<ext>`)                   |
| Sort (`s`)               | Cycles ASC → DESC → none on current column (server-side `ORDER BY`)                               |
| Filter (`/`)             | Inline WHERE clause with autocomplete for columns, operators, and values                          |
| Edit cell (`enter`)      | Inline editing with type-aware parsing (int, float, bool), stores draft locally (blue cell)       |
| Insert row (`i`)         | Staged insert: creates a pending row in the grid (shown in green)                                 |
| Delete row (`d`)         | Stages row for deletion (red), press `d` again to confirm                                         |
| Commit all (`Ctrl+S`)    | Commits ALL drafts (inserts + updates + deletes) atomically, then reloads the table               |
| Discard all (`D`)        | Discards all pending changes, restores original values (press twice to confirm)                   |
| Multi-select (`space`)   | Toggles row selection for bulk operations                                                         |
| FK navigate (`o`)        | Follow foreign key: loads referenced table with FK filter AND existing WHERE, syncs explorer     |
| Go back (`H`)            | Return to previous table in navigation history, restores cursor and filter                       |
| Find column (`f`)        | Fuzzy search to jump to a column by name                                                          |
| Pagination               | `n`/`p` next/prev page, `N`/`P` first/last page, `F1`–`F9` jump, digit keys for multi-digit pages |
| Tabs                     | `1`–`5` switch between Records, Columns, Constraints, Foreign Keys, Indexes                       |
| Preview                  | Right panel shows selected row as highlighted JSON, autoclosed when width < 100                   |

#### Grid Modes

The grid has two modes displayed in the bottom-left corner:

- **NORMAL** — Default mode. Navigate with `hjkl`, `G`/`g`, `Ctrl+u`/`Ctrl+d`.
- **EDIT** — Active when editing a cell. Type to modify, `enter`/`tab` to confirm cell and move, `Esc` to cancel edit.

#### Draft-based Editing

All changes (edits, inserts, deletes) are staged locally before being committed to the database:

| Color  | Meaning                     |
| ------ | --------------------------- |
| 🟢 Green  | Pending insert row          |
| 🔵 Blue   | Modified cell (edit draft)  |
| 🔴 Red    | Row marked for deletion     |

- **`Ctrl+S`** — Commits all drafts (INSERTs + UPDATEs + DELETEs) atomically, then reloads the table.
- **`D`** — Discard all pending changes. Press twice to confirm. Restores original values.
- **`Esc`** — Exits edit mode but does **NOT** discard drafts. Drafts persist with their colors.

No database changes occur until you commit with `Ctrl+S`.

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
| Grid Preview (`Tab`) | Focus preview pane (full-width), Tab to return                        |
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

### Global

| Key | Action                     |
| --- | -------------------------- |
| `q` | Quit                       |
| `?` | Help                       |
| `:` | Command palette            |
| `e` | Toggle explorer            |
| `E` | Toggle editor              |
| `a` | Ask AI (NL→SQL)            |
| `x` | Export                     |

### Explorer

| Key | Action                     |
| --- | -------------------------- |
| `j`/`k` | Navigate down/up      |
| `g`/`G` | First/last node        |
| `Enter`/`l` | Open table data   |
| `Backspace`/`h` | Collapse / parent |
| `/` | Filter tables              |
| `n` | New table                  |
| `d` | Drop table                 |
| `v` | View DDL                   |
| `r` | Refresh schema             |

### Grid — NORMAL mode

| Key | Action                     |
| --- | -------------------------- |
| `j`/`k` | Row down/up           |
| `h`/`l` | Column left/right     |
| `g`/`G` | First/last row        |
| `Ctrl+U`/`Ctrl+D` | Half page up/down |
| `n`/`p` or `]`/`[` | Next/prev page |
| `P`/`N` | First/last page       |
| `F1`-`F9` | Go to page          |
| `1`-`5` | Tabs (records/columns/constraints/FK/indexes) |
| `Tab` | Focus preview pane         |
| `enter` | Enter EDIT mode          |
| `d` | Delete row                |
| `i` | Insert row                |
| `space` | Select row              |
| `s` | Sort column               |
| `/` | Filter (WHERE)            |
| `f` | Find column               |
| `y` | Export (SQL/JSON/CSV)     |
| `o` | Open FK reference         |
| `H` | Go back                   |
| `r` | Refresh data              |
| `Ctrl+S` | Commit pending        |
| `D` | Discard drafts            |

### Grid — EDIT mode

| Key | Action                     |
| --- | -------------------------- |
| `Esc` | Cancel edit              |
| `Enter`/`Tab` | Commit cell, next col |
| `Up`/`Down` | Prev/next row       |

### Grid Preview

| Key | Action                     |
| --- | -------------------------- |
| `Tab` | Focus preview (from grid) |
| `j`/`k` | Scroll down/up        |
| `g`/`G` | First/last line       |
| `Ctrl+U`/`Ctrl+D` | Half page up/down |
| `e` | Focus explorer             |

### Editor

| Key | Action                     |
| --- | -------------------------- |
| `Ctrl+Enter` | Execute query     |
| `Ctrl+U` | Clear editor          |
| `Tab` | Autocomplete              |
| `Ctrl+P`/`Ctrl+N` | History prev/next |

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
