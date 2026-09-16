# dbx

> **Database x** — A modern TUI database client with native AI integration.

dbx is a terminal UI for databases designed for both humans and AI agents.

## Features

### Grid

> **Draft model**: All edits, inserts, and deletes are staged locally (with color feedback). Press `Ctrl+S` to dump the generated SQL into the editor for review/edit/copy. Press `D` to discard.

| Action                   | Behavior                                                                                          |
| ------------------------ | ------------------------------------------------------------------------------------------------- |
| Yank one row (`y`)       | Copies the row in the selected format (SQL, JSON, CSV) to clipboard                               |
| Yank multiple rows (`y`) | Exports to file in the same directory as `.dbx.toml` (`<schema>_<table>.<ext>`)                   |
| Sort (`s`)               | Cycles ASC → DESC → none on current column (server-side `ORDER BY`)                               |
| Filter (`/`)             | Inline WHERE clause with autocomplete for columns, operators, and values                          |
| Edit cell (`enter`)      | Inline editing with type-aware parsing (int, float, bool), stores draft locally (blue cell)       |
| Insert row (`i`)         | Staged insert: creates a pending row in the grid (shown in green)                                 |
| Delete row (`d`)         | Stages row for deletion (red)                                                                      |
| Dump SQL to editor (`Ctrl+S`) | Generates SQL for all drafts and opens it in the editor for review/edit/copy; running it commits the transaction |
| Discard all (`D`)        | Discards all pending changes, restores original values (press twice to confirm)                   |
| Multi-select (`space`)   | Toggles row selection for bulk operations                                                         |
| FK navigate (`o`)        | Follow foreign key: loads referenced table with FK filter AND existing WHERE, syncs explorer     |
| Go back (`H`)            | Return to previous table in navigation history, restores cursor and filter                       |
| Find column (`f`)        | Fuzzy search to jump to a column by name                                                          |
| Pagination               | `n`/`p` next/prev page, `N`/`P` first/last page, `F1`–`F9` jump, digit keys for multi-digit pages |
| Preview                  | Right panel shows selected row as highlighted JSON, autoclosed when width < 100                   |
| Preview FK cell          | If the cursor is on a FK cell, the preview shows the referenced row instead of the current one     |
| JQ Filter (`/`)          | Filter/navigate JSON in preview with jq expressions, autocomplete, persistent history             |

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

- **`Ctrl+S`** — Generates SQL for all drafts (INSERTs + UPDATEs + DELETEs) and opens it in the editor. You can modify, review, and execute from there; executing it commits the transaction (the editor title shows `commit on run`).
- **`D`** — Discard all pending changes. Press twice to confirm. Restores original values.
- **`Esc`** — Exits edit mode but does **NOT** discard drafts. Drafts persist with their colors.

No database changes occur until you execute the SQL from the editor with `Ctrl+Enter`.

Executing the SQL dumped from the grid commits its transaction when the
execution finishes, so the draft edits become durable. DML typed manually in the
editor stays inside an open transaction until you press `U` to roll it back or
run the next statement (which commits it).
See [DML Transactions](docs/KEYBINDS.md#dml-transactions).

### Explorer

| Action                   | Behavior                                             |
| ------------------------ | ---------------------------------------------------- |
| Open table (`enter`)     | Loads table data into grid                           |
| Preview table (`Tab`)    | Opens explorer-preview with table details (full-screen) |
| Toggle schema (`space`)   | Collapse schema (on table) or toggle schema (on schema) |
| Filter (`/`)             | Fuzzy filter across all table names                  |
| New table (`n`)          | Opens editor pre-filled with `CREATE TABLE` template |
| Drop table (`d`)         | Opens editor pre-filled with `DROP TABLE`            |
| View DDL (`v`)           | Opens editor with `pg_get_tabledef` query            |

### Explorer Preview

Full-screen table detail view with tabbed panels. Open with `Tab` from explorer on any table.

| Action                   | Behavior                                             |
| ------------------------ | ---------------------------------------------------- |
| Overview (`1`)           | Dashboard: sizes, row stats, vacuum status, counts   |
| Columns (`2`)            | Column names, types, nullable, defaults              |
| Constraints (`3`)        | PKs, UNIQUEs, CHECKs with column list               |
| Foreign Keys (`4`)       | FK references with column and target table           |
| Indexes (`5`)            | Index names, uniqueness, definitions                 |
| ERE Diagram (`6`)        | Entity-Relationship diagram (placeholder)            |
| Back (`Tab`/`Esc`)       | Return to explorer                                   |

### Query Browser (`Q`)

Persistent query history and favorites, isolated per project.

| Action | Behavior |
|--------|----------|
| Open (`Q`) | Opens the query browser overlay |
| Navigate (`j`/`k`) | Move up/down through the list |
| Jump (`g`/`G`) | First/last entry |
| Load (`Enter`) | Load selected query into the editor |
| Favorite (`f`) | Toggle favorite on the selected entry |
| Delete (`d`) | Delete the selected entry |
| Filter (`/`) | Fuzzy search across all entries |
| Switch tab (`Tab`) | Toggle between History and Favorites |
| Close (`Esc`) | Close the browser |

Every executed query is automatically saved. Duplicates are deduplicated (moved to top). Capped at 500 entries per project.

Storage: `~/.local/state/dbx/projects/{project_name}/query_history.json`. Queries are fully isolated between projects — switching project shows only that project's history.

### SQL Editor

| Action                      | Behavior                                                         |
| --------------------------- | ---------------------------------------------------------------- |
| Execute (`ctrl+enter`)      | Runs SQL against database, results shown in grid                 |
| Autocomplete (real-time)    | Context-aware popup as you type: keywords, schemas, tables, columns |
| Navigate suggestions (`↑`/`↓`) | Move through completion list                               |
| Accept suggestion (`Tab`/`Enter`) | Insert selected completion into editor                    |
| Close suggestions (`Esc`)   | Dismiss autocomplete popup                                       |
| History (`ctrl+p`/`ctrl+n`) | Navigate previous/next executed queries                          |
| Syntax highlighting         | Keywords, strings, numbers, comments, functions, operators       |
| Auto-refresh                | Schema reloads after DDL statements (CREATE/DROP/ALTER/TRUNCATE) |

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
never offered back, and `Tab`/`Enter` replaces that token in place (accepting a
keyword twice never duplicates it).

```toml
[editor]
autocomplete = true
autocomplete_trigger = 1   # min characters before the popup opens on its own
```

### AI (NL → SQL)

| Action         | Behavior                                                                        |
| -------------- | ------------------------------------------------------------------------------- |
| Ask (`a`)      | Type natural language, dbx generates SQL, shows for review, executes on confirm |
| CLI `dbx ask`  | Same flow from terminal, with `--json`, `--sql-only` flags                      |
| Multi-provider | Anthropic, OpenAI, DeepSeek, Qwen, OpenCode, and any OpenAI-compatible endpoint |

### Navigation & UI

| Action                | Behavior                                                             |
| --------------------- | -------------------------------------------------------------------- |
| Top bar               | Shows active pane, `schema.table`, row count, WHERE filter, breadcrumbs |
| Bottom bar            | Global keybinds (always visible) + contextual keybinds per pane      |
| Focus cycling (`e`)  | Toggle Explorer ↔ Grid                                               |
| Query Browser (`Q`) | Browse history & favorites, load into editor                         |
| Grid Preview (`Tab`) | Focus row preview (from grid), Tab/Esc to return                     |
| Explorer Preview (`Tab`) | Focus table details (from explorer), full-screen                  |
| JQ Filter (`/`)     | Filter JSON in preview with jq expressions, autocomplete, history    |
| Command palette (`:`) | Fuzzy search for any command (refresh, export, execute, focus, etc.) |
| Help (`?`)            | Overlay showing all keybinds, scrollable                             |
| DML rollback (`U`)    | DML runs in an open transaction; `U` rolls it back (the next statement commits it first) |
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
| `Q` | Query Browser              |
| `a` | Ask AI (NL→SQL)            |
| `x` | Export                     |
| `U` | Rollback pending transaction |

### Explorer

| Key | Action                     |
| --- | -------------------------- |
| `j`/`k` | Navigate down/up      |
| `g`/`G` | First/last node        |
| `Enter`/`l` | Open table data   |
| `Tab` | Open explorer-preview  |
| `Backspace`/`h` | Collapse / parent |
| `Space` | Collapse schema      |
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
| `Tab` | Focus row preview         |
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
| `Ctrl+S` | Dump drafts SQL to editor |
| `D` | Discard drafts            |
| `u` | Undo draft on selected row |

### Grid — EDIT mode

| Key | Action                     |
| --- | -------------------------- |
| `Esc` | Cancel edit              |
| `Enter`/`Tab` | Commit cell, next col |
| `Up`/`Down` | Prev/next row       |

### Grid Preview

| Key | Action                     |
| --- | -------------------------- |
| `Tab` | Focus row preview (from grid) |
| `Esc` | Back to grid              |
| `j`/`k` or `↑`/`↓` | Navigate down/up (cursor) |
| `Enter` | Expand FK / Collapse     |
| `g`/`G` | First/last line       |
| `Ctrl+U`/`Ctrl+D` | Half page up/down |
| `e` | Focus explorer             |
| `/` | JQ filter (autocomplete)   |
| `Ctrl+P`/`Ctrl+N` | JQ history prev/next |
| `Ctrl+Space` | Toggle autocomplete  |

### Explorer Preview

| Key | Action                     |
| --- | -------------------------- |
| `Tab`/`Esc` | Back to explorer     |
| `1` | Overview tab                |
| `2` | Columns tab                 |
| `3` | Constraints tab             |
| `4` | Foreign Keys tab            |
| `5` | Indexes tab                 |
| `6` | ERE Diagram tab (placeholder) |

### Editor

| Key | Action                     |
| --- | -------------------------- |
| `Ctrl+Enter` | Execute query     |
| `Ctrl+U` | Clear editor          |
| `Ctrl+Y` | Copy SQL to clipboard |
| `Tab`/`Enter` | Accept autocomplete suggestion |
| `↑`/`↓` | Navigate suggestions  |
| `Esc` | Close autocomplete / close editor |
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
