# dbx Keybinds Reference

## Overview

dbx uses an **action-based keybind system**. Each action has one or more default keys, all overridable via config.

Actions follow the **first-letter rule** — the primary key is the first letter of the action name.

## Action Naming Convention

| Key | Action |
|-----|--------|
| `a` | **a**sk (NL→SQL) |
| `c` | **c**hange connection |
| `d` | **d**elete |
| `e` | **e**xplorer toggle |
| `x` | e**x**port |
| `f` | **f**ilter |
| `g` | **g**o to (first/last) |
| `i` | **i**nsert |
| `n` | **n**ext page |
| `o` | **o**pen FK reference |
| `p` | **p**revious page |
| `q` | **q**uit |
| `Q` | **Q**uery browser |
| `r` | **r**efresh |
| `s` | **s**ort |
| `v` | **v**iew DDL |
| `y` | **y**ank (copy) |
| `U` | **U**ndo — roll back the pending DML transaction |

## Default Keys

Each action has a primary key and may have alternatives. All are overridable.

### Global

| Action | Default Keys | Description |
|--------|-------------|-------------|
| `global.quit` | `q`, `Ctrl+C` | Exit dbx |
| `global.help` | `?` | Show keybinds modal |
| `global.palette` | `:` | Open command palette |
| `global.cycle_focus` | `e` | Toggle Explorer pane |
| `global.focus_editor` | `E` | Toggle Editor pane |
| `global.ask` | `a` | Open NL→SQL prompt |
| `global.export` | `x` | Export current data |
| `global.query_browser` | `Q` | Open query browser (history + favorites) |
| `global.rollback` | `U` | Roll back the pending DML transaction |
| `global.switch_connection` | `c` | Open picker to switch projects |

### Connection Picker

Shown on startup (when multiple projects exist or inactive projects are present)
and when pressing `c` to switch connections.

| Key | Action |
|-----|--------|
| `j`/`k` or `↑`/`↓` | Navigate up/down |
| `Space` | Toggle active/inactive on selected project |
| `Enter` | Connect to selected project (must be active) |
| `q`/`Ctrl+C` | Quit |

Inactive projects appear grayed out with `[off]` tag. Press `Space` to toggle.
Auto-connect triggers only when exactly 1 project is active and no inactive
projects exist. If any project is inactive, the picker is always shown so you
can manage active/inactive state.

## DML Transactions

DML statements typed in the editor (`INSERT`, `UPDATE`, `DELETE`) run inside a
transaction that **stays open** after execution, so you can still undo them.
SQL dumped from a grid draft with `Ctrl+S` is the exception: running it commits
its transaction as soon as the execution finishes (the editor title shows
`commit on run`), because draft edits are meant to become durable.

| Event | What happens |
|-------|--------------|
| DML executed | `BEGIN` — the statusbar shows `tx pending` and `U rollback` |
| Grid draft (`Ctrl+S`) executed | `BEGIN` + the transaction is **committed** when execution finishes (`commit on run`) |
| `U` pressed | `ROLLBACK` — all statements of the current execution are undone |
| New statement executed | `COMMIT` of the pending transaction, then the new statement runs |
| `DDL`/`SELECT` executed | The pending transaction is committed first; the statement itself runs in autocommit |
| Quit (`q`) | The pending transaction is rolled back before the connection closes |

A single execution is one rollback unit: `UPDATE …; UPDATE …` shares one
transaction, so `U` undoes both. Mixing DDL into a batch commits whatever was
pending before it.

`SELECT` and DDL (`CREATE`/`ALTER`/`DROP`/`TRUNCATE`) never open a transaction.

### Explorer Pane

Schema tree navigation:

| Action | Default Keys | Description |
|--------|-------------|-------------|
| `explorer.down` | `j`, `↓` | Select next node |
| `explorer.up` | `k`, `↑` | Select previous node |
| `explorer.expand` | `Enter`, `l`, `→` | Expand node |
| `explorer.collapse` | `Backspace`, `h`, `←` | Collapse / go to parent |
| `explorer.toggle_columns` | `Space` | Toggle expand: collapse schema (on table) or toggle schema (on schema) |
| `explorer.first` | `g` | Jump to first node |
| `explorer.last` | `G` | Jump to last node |
| `explorer.filter` | `/` | Filter tables |
| `explorer.new` | `n` | Create new table |
| `explorer.drop` | `d` | Drop selected table |
| `explorer.view_ddl` | `v` | View table DDL |
| `explorer.refresh` | `r` | Refresh tree |

#### Explorer Preview Tabs

| Action | Default Keys |
|--------|-------------|
| `explorer.tab_overview` | `1` |
| `explorer.tab_columns` | `2` |
| `explorer.tab_constraints` | `3` |
| `explorer.tab_foreign_keys` | `4` |
| `explorer.tab_indexes` | `5` |
| `explorer.tab_ere` | `6` |

### Mouse Actions

| Action | Effect |
|--------|--------|
| Left Click | Select node |
| Double Click | Expand/Collapse |
| Right Click | Context menu |
| Scroll | Navigate tree |

## Grid Pane

### Default Keys

| Action | Default Keys | Description |
|--------|-------------|-------------|
| `grid.down` | `j`, `↓` | Next row |
| `grid.up` | `k`, `↑` | Previous row |
| `grid.left` | `h`, `←` | Previous column |
| `grid.right` | `l`, `→` | Next column |
| `grid.first` | `g` | Jump to first row |
| `grid.last` | `G` | Jump to last row |
| `grid.half_up` | `Ctrl+U` | Scroll up half page |
| `grid.half_down` | `Ctrl+D` | Scroll down half page |
| `grid.next_page` | `n`, `]` | Next page |
| `grid.prev_page` | `p`, `[` | Previous page |
| `grid.first_page` | `P` | Jump to first page |
| `grid.last_page` | `N` | Jump to last page |
| `grid.goto_page_1`–`grid.goto_page_9` | `F1`–`F9` | Jump to page 1-9 |
| `grid.focus_preview` | `Tab` | Focus row preview pane |
| `grid.edit_cell` | `Enter` | Enter EDIT mode |
| `grid.delete_row` | `d` | Delete row |
| `grid.insert_row` | `i` | Insert new row |
| `grid.select_row` | `Space` | Toggle row selection |
| `grid.sort` | `s` | Cycle sort (asc/desc/none) |
| `grid.filter` | `/` | Filter by column (WHERE) |
| `grid.find_and_jump_to_column` | `f` | Jump to column by name |
| `grid.yank` | `y` | Yank to Clipboard / File |
| `grid.export` | `x` | Export (SQL/JSON/CSV) |
| `grid.navigate_fk` | `o` | Open referenced table |
| `grid.go_back` | `H` | Return to previous table |
| `grid.refresh` | `r` | Refresh data (re-execute query) |
| `grid.commit_pending` | `Ctrl+S` | Dump draft SQL to editor for review/edit/copy (running it commits the transaction) |
| `grid.discard_all` | `D` | Discard all draft changes |
| `grid.undo` | `u` | Undo draft on selected row (revert edit/insert/delete) |

### EDIT mode

| Key | Action |
|-----|--------|
| `Esc` | Cancel edit (revert value) |
| `Enter` | Commit cell, move to next column |
| `Tab` | Commit cell, move to next column |
| `Up`/`Down` | Move to prev/next row (stays in same column) |
| Any char | Type into cell |

### FILTER mode (column find)

| Key | Action |
|-----|--------|
| `Esc` | Cancel filter |
| `Enter` | Apply filter, jump to first match |
| Any char | Append to filter text |

### WHERE FILTER mode

| Key | Action |
|-----|--------|
| `Esc` | Cancel WHERE filter |
| `Enter` | Apply WHERE clause (re-query DB) |

### Mouse Actions

| Action | Effect |
|--------|--------|
| Left Click Cell | Edit cell |
| Left Click Header | Sort column |
| Double Click | View full cell content |
| Right Click | Context menu |
| Scroll Up | Previous row |
| Scroll Down | Next row |
| Scroll Left | Previous column |
| Scroll Right | Next column |

## Grid Preview Pane

Focused via `Tab` in grid:

| Action | Default Keys | Description |
|--------|-------------|-------------|
| `grid-preview.cursor_up` | `k`, `↑` | Move cursor up |
| `grid-preview.cursor_down` | `j`, `↓` | Move cursor down |
| `grid-preview.expand` | `Enter` | Expand foreign key object / Collapse |
| `grid-preview.first` | `g` | Jump to first line |
| `grid-preview.last` | `G` | Jump to last line |
| `grid-preview.half_up` | `Ctrl+U` | Scroll up half page |
| `grid-preview.half_down` | `Ctrl+D` | Scroll down half page |
| `grid-preview.toggle_explorer` | `e` | Focus explorer pane |
| `grid-preview.jq_filter` | `/` | Filter with jq |

## Explorer Preview Pane

Full-screen table detail view, focused via `Tab` in explorer:

| Key | Action |
|-----|--------|
| `Tab`/`Esc` | Back to explorer |
| `1` | Overview tab (sizes, stats, vacuum, counts) |
| `2` | Columns tab |
| `3` | Constraints tab |
| `4` | Foreign Keys tab |
| `5` | Indexes tab |
| `6` | ERE Diagram tab (placeholder) |

## Editor Pane

SQL editor:

| Action | Default Keys | Description |
|--------|-------------|-------------|
| `editor.execute` | `Ctrl+Enter`, `Ctrl+R` | Run query |
| `editor.clear` | `Ctrl+U` | Clear editor |
| `editor.copy` | `Ctrl+Y` | Copy SQL to clipboard |
| `editor.autocomplete` | `Tab` | Accept autocomplete suggestion |
| `editor.history_prev` | `Ctrl+P` | Previous query |
| `editor.history_next` | `Ctrl+N` | Next query |

### Mouse Actions

| Action | Effect |
|--------|--------|
| Left Click | Position cursor |
| Right Click | Context menu |
| Scroll | Scroll editor |

## Query Browser

Persistent query history and favorites, isolated per project. Opened with `Q`.

| Action | Default Keys | Description |
|--------|-------------|-------------|
| `global.query_browser` | `Q` | Open query browser (history + favorites) |

#### Inside the Browser

| Key | Action |
|-----|--------|
| `j`/`k` | Navigate down/up |
| `g`/`G` | First/last entry |
| `Enter` | Load selected query into editor |
| `f` | Toggle favorite on selected entry |
| `d` | Delete selected entry |
| `/` | Start filter mode (fuzzy search) |
| `Tab` | Switch between History and Favorites tabs |
| `Esc` | Close browser |

#### Filter Mode

| Key | Action |
|-----|--------|
| Any char | Append to filter text |
| `Backspace` | Delete last character |
| `Enter` | Confirm filter, keep results |
| `Esc` | Cancel filter, clear and close filter |

Every executed query is recorded automatically. Duplicates are deduplicated (moved to top). Capped at 500 entries per project.

Storage: `~/.local/state/dbx/projects/{project_name}/query_history.json`. A one-time migration runs automatically from the legacy global `query_history.json`.

## Statusbar

Context-sensitive action bar at bottom:

| Action | Effect |
|--------|--------|
| Left Click | Execute action |
| Hover | Highlight |

## Command Palette

Fuzzy command finder:

| Key | Action |
|-----|--------|
| `Enter` | Execute selected |
| `Esc` | Close palette |
| `↑`/`↓` | Navigate results |
| `j`/`k` | Navigate results |

## Help Modal

Keybinds reference:

| Key | Action |
|-----|--------|
| `j`/`k` or `↑`/`↓` | Scroll |
| `g`/`G` | First/Last |
| `Esc` or `?` | Close |

## Custom Keybinds

Override any default in `~/.config/dbx/config.toml`:

```toml
[keybindings.custom]
# Each action maps to a single key string
"global.ask" = "ctrl+a"
"grid.edit_cell" = "enter"
"grid.down" = "ctrl+j"
```

Unset a binding by mapping to empty string:

```toml
[keybindings.custom]
"grid.discard_all" = ""
```

## Context-Sensitive Display

The statusbar shows relevant keybinds based on current context:

```
[Transaction pending]  (prepended to the action bar)
U rollback · tx pending

[Picker]
j/k navigate · space toggle · enter select · q quit

[Explorer-focused]
/ filter · n new · d drop · v DDL · Enter Open table data · Space Collapse schema · Tab Preview

[Grid-focused]
/ filter · r refresh · n/p N/P page · F1-9 goto · s sort · f find column
Enter edit · d delete · i insert · space select · y yank · x export · o FK nav
Ctrl+S dump SQL · D discard · u undo · H go back

[Grid Preview-focused]
Tab/Esc back · j/k navigate · Enter expand FK · g/G first/last · e explorer · / jq

[Explorer Preview-focused]
1-6 tabs · Tab/Esc back to explorer

[Editor-focused]
Ctrl+Enter execute · Ctrl+U clear · Ctrl+Y copy · Ctrl+P/N history

[Query Browser-focused]
j/k navigate · Enter load · f favorite · d delete · / filter · Tab switch · Esc close
```
