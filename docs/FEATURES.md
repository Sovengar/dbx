# dbx — Features & Conventions

dbx is a TUI database client with native AI integration. This document describes
what dbx does, organized by surface:

- **Part 1 — CLI**: the non-interactive commands, for scripting and agents.
- **Part 2 — TUI**: the interactive client, pane by pane.
- **Part 3 — Conventions**: cross-cutting rules that apply everywhere.

Keybinds are deliberately **not** a reference here: the TUI renders them from
the same registry that dispatches keys (press `?`), so the help overlay is the
single source of truth and always current. Keys are mentioned below only to
explain behavior; custom overrides are covered in Part 3.

## Part 1 — CLI

### Connection resolution

Every command resolves its target connection the same way: a `.dbx.toml` in the
current working directory takes precedence; otherwise a connection name or the
global config is used. `-c/--connection` forces a specific one. Commands that
return data accept `-j/--json` for machine consumption.

### `dbx` — launch the TUI

No flags. The optional positional argument does **not** select a connection: the
target is chosen inside the TUI (connection picker). On quit, a pending DML
transaction is rolled back before the connection closes.

### `dbx ask [question]` — natural language → SQL

Turns a question into SQL and runs it. Flags: `-c`, `-j`, `-s/--sql-only`.
`--sql-only` needs no database and just prints the generated SQL. Otherwise dbx
connects, builds a schema string (system schemas excluded) for the model, prints
the SQL to **stderr**, and executes it in autocommit. There is **no confirmation
step** in the CLI — the review flow belongs to the TUI ASK pane. Output is a
text table or JSON `{sql, columns, rows, count}`. Intended for agents/scripting.

### `dbx query [sql]` — run one statement

Executes a single statement in autocommit (no transaction, no rollback). Text
table or JSON `{columns, rows, count}`.

### `dbx list` — schema listing

Grouping command with two subcommands:

- `dbx list tables` — every table across schemas; text `schema.name (TYPE, N rows)`
  or JSON.
- `dbx list columns [table]` — the first schema where the table exists;
  `Table: schema.table` plus columns, or JSON.

Read-only. Note these are **subcommands**: there is no top-level `dbx tables` or
`dbx columns`.

### `dbx schema [table]` — a table's columns

Prints the table's columns. It is effectively the same data as
`dbx list columns`, differing only in formatting; despite the name it does **not**
include constraints, indexes or foreign keys. Read-only.

### `dbx context` — schema export for LLMs

Emits the whole schema as indented JSON — schemas (system schemas excluded),
each table with its columns, indexes and foreign keys. `-o/--output` writes to a
file (mode 0644) instead of stdout; `-j` defaults to true. Built for agents.

### `dbx replay [session-file]` — re-run a session log

Reads a newline-delimited JSON session file from the session directory
(`/tmp/dbx/sessions` by default) and re-executes every entry with
`level == "query"` and a non-empty `sql`, in autocommit. Progress goes to
stderr; JSON reports `{session, total, errors, queries:[…]}`. An entry needs at
least `level` and `sql` (optionally `duration_ms`, `rows`, `error`, `metadata`,
`timestamp`); malformed lines are skipped.

> **Not implemented**: nothing in dbx *writes* these session logs today — the
> logger exists but is never instantiated, and the `session.enabled` /
> `retention_days` config values are inert. `replay` only works on externally
> produced files.

### `dbx pipe` — SQL from stdin

Reads SQL from stdin and executes it in autocommit. Refuses when stdin is a TTY
(`echo "SELECT 1" | dbx pipe`); empty input is an error. Text table or JSON.
Designed for pipelines.

## Part 2 — TUI

### Layout & focus

The main view renders **one pane at a time**, not a fixed split. Depending on
state and focus it shows: the grid preview, the explorer preview, the explorer
(with the editor overlaid when open), or the grid — the grid additionally shows
a narrow **sidebar preview** on the right when it has data, is on tab 0, and the
terminal is at least 100 columns wide.

Focus lives in the router and cycles between **explorer** and **grid** (`e`
toggles, `E` toggles the editor overlay). The editor, help, palette, query
browser, export picker and ASK are **overlays**: they capture input while open.
Mouse zones exist only for the visible explorer/grid/sidebar panes; clicks are
ignored while an overlay is open.

A guard refuses new database commands while an ASK read-only query is in flight.

### Connection picker

Purpose: choose which project/connection to open.

Shown on startup, via `c`, or after `esc` from an error/loading state. Lists the
`.dbx.toml` projects found by scanning `~/dev`, each with its driver:port and
config path, plus an `[off]` tag for inactive ones. `space` toggles
active/inactive (persisted immediately) and `enter` connects — only for active
projects; pressing it on an inactive one does nothing. `j`/`k` move, `q` quits.
Startup auto-connects when exactly one active project exists and there are no
inactive ones.

### Explorer

Purpose: browse the schema tree and drive table operations.

A tree of database → schema → table → columns/indexes/constraints, with a filter
line (`/`). Focus with `e`. Table-level actions emit editor scaffolds rather than
executing directly: `enter`/`l` loads a table's data into the grid, while `d`
(drop), `v` (view DDL) and `n` (new table) open the editor pre-filled with
`DROP TABLE …`, a DDL-generating query, or a `CREATE TABLE …` scaffold. `r`
reloads the schema, `space` collapses the schema, and `backspace`/`h` collapses a
node or moves to its parent.

### Explorer preview (incl. ERE diagram)

Purpose: tabbed metadata for the selected table.

Opens with `tab` from the explorer when a table is selected; `tab`/`esc` goes
back. Six tabs via `1`–`6`: Overview (name/type/comment, storage sizes, live/dead
rows, vacuum/analyze timestamps, counts), Columns, Constraints, Foreign Keys,
Indexes, and the **ERE** entity-relationship diagram. In the ERE tab, `h/j/k/l`
pan, `g`/`G` jump, and `enter` navigates to a related table (which reloads the
preview). The diagram centers the table's PK/FK columns and shows incoming `1:N`
and outgoing `N:1` relationships, marking junction tables with `*` and capping
hubs at 10 neighbors (`+N more`). Read-only.

### Grid

Purpose: display and edit result data.

Shown when focused; populated by selecting a table, executing an editor query,
or confirming an ASK query. Ad-hoc query results live under the synthetic table
name `query`. The header marks PK (`*`) and FK (`→`) columns and sort arrows; the
footer shows `X-Y of Z · Page a/b` and any pending-draft count. Row loads are
capped at 1000 rows.

Modes: **NORMAL** navigation and paging; **EDIT** (`enter`) inline cell editing;
**column find** (`f`) substring jump; **WHERE filter** (`/`) with context-aware
suggestions that re-queries the server; and an **export picker** (`x`).

Edits, inserts and deletes are **in-memory drafts** until committed. Committing
(`ctrl+s`) opens the editor with the draft SQL and runs it — those statements go
straight to the connection in autocommit, so grid edits are **not** covered by
the editor's rollback transaction (`U`). `D` discards drafts and `u` undoes row
drafts. Export (`x`) writes SQL/JSON/CSV to a file next to the project, or copies
to the clipboard for a single row; `y` always yanks to the clipboard. `o`
navigates an FK to its referenced table, `H` goes back through navigation
history, `r` refreshes, and `s` sorts a column.

### Grid preview

Purpose: JSON inspection of the selected row, full pane.

Opens with `tab` from the grid (needs data and not editing/filtering);
`tab`/`esc` returns. Pretty-prints the row as JSON; FK keys are marked `→` and
expand with `enter` (a dotted-path drill-down that issues a lookup), and `/`
opens a `jq` expression bar with suggestions and history. `jq` history is
persisted to `~/.config/dbx/jq_history.json` (capped at 50). Read-only.

### Grid sidebar preview

Purpose: a narrow JSON preview beside the grid.

Appears automatically (grid tab 0, width ≥ 100) and is not focusable — clicks
are ignored. Shows the current row as JSON, or, when the cursor sits on a
populated FK column, the referenced row instead (looked up and cached, up to 50
entries). Falls back to the selected row if the referenced row is missing.

### Editor (SQL)

Purpose: compose and execute arbitrary SQL.

An overlay, toggled with `E` and also opened by `ctrl+enter`/`ctrl+r` (execute),
`ctrl+y` (copy), explorer scaffolds, or a draft commit. It has syntax
highlighting, a schema-aware autocomplete popup (`tab`, `ctrl+space`), query
history (`ctrl+p`/`ctrl+n`) and clear (`ctrl+u`). Its title reads `commit on run`
when it was opened to commit grid drafts.

Execution semantics: DML (`INSERT`/`UPDATE`/`DELETE`/`WITH`) opens a transaction
that **stays open** for rollback via `U`; non-DML commits any pending transaction
and runs in autocommit. Successful queries are added to the query store (and the
query browser), and DDL triggers a schema reload. It refuses to run while another
query is executing or while an ASK read-only query is in flight. See **DML
Transactions** in Part 3 for the full model.

### Query browser

Purpose: recall past queries and manage favorites.

Opens with `Q`. Two tabs — History and Favorites — with relative timestamps, a
`★` favorite marker and a SQL preview; `/` filters. `enter` loads a query into
the editor, `f` toggles favorite, `d` deletes, `tab` switches tabs. History is
persisted per project (`query_history.json`, capped at 500, deduplicated by SQL);
entries are added on execution, not from the browser.

### Command palette

Purpose: fuzzy launcher for every action.

Opens with `:`. Type to fuzzy-filter commands grouped by section (Database,
Query, UI, Navigation); each row shows its bound key. `enter` runs the command
through the same dispatch path as its keybind, so palette and keys never diverge.
`esc` closes.

### ASK (NL→SQL)

Purpose: ask a question in natural language and run the generated SQL.

Opens with `a`; **refused** with a toast when no AI provider is configured. It is
a chat transcript (`You:` / `SQL:` / `Error:` / `(executed)`) with states
*typing → generating → review → executing* (and *error*). You type a question and
submit; the model returns SQL, which is shown for **review** — `enter` confirms
and executes. `esc` closes from any state.

Guarantee: the generated SQL must be a single `SELECT` (or `WITH … SELECT`); a
client-side validator rejects other statements and `FOR UPDATE/SHARE`, and the
query then runs inside a **server-enforced READ ONLY transaction** that is always
rolled back — the client check is defense in depth, PostgreSQL is authoritative.
ASK is globally available (independent of the grid) but passes the current grid
table and WHERE clause to the model as a **context hint**, never a restriction;
results land in the grid under the synthetic table `query` and so never
contribute a hint. It refuses while a DML transaction is pending or another
read-only query is running. See ADR 0001.

### Keybinds pane

Purpose: the bottom status/keybind bar.

Always visible in the main view; not focusable. Renders the actions of the active
context (query browser → editor → focused pane) as `key Description` segments.
It hides contextless actions (e.g. `rollback` unless a transaction is pending)
and appends `tx pending` when applicable.

### Help overlay

Purpose: the complete keybind reference.

Opens with `?`; closes with `esc`/`?`/`q`. Registry-backed sections for the panes
plus static sections for widget modes and mouse. Scrolls with `j`/`k`,
`ctrl+u`/`ctrl+d`, `g`/`G` and the wheel, and swallows input while open.

### Toasts

Transient bottom-right notifications (success/error/info/warning), auto-dismissed
after 3 seconds. Not interactive.

## Part 3 — Conventions

### Action Naming Convention

Every keybind is an **action** with a stable, view-agnostic ID, a primary key
and optional aliases, a display section, a description, and the list of views
where it applies. Actions follow the **first-letter rule** — the primary key is
the first letter of the action name.

| Key | Action |
|-----|--------|
| `a` | **a**sk (NL→SQL) |
| `c` | **c**hange connection |
| `d` | **d**elete |
| `e` | **e**xplorer toggle |
| `x` | e**x**port |
| `f` | **f**ind column (grid) · **f**ilter tables (explorer) |
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

### DML Transactions

DML statements typed in the editor (`INSERT`, `UPDATE`, `DELETE`) run inside a
transaction that **stays open** after execution, so you can still undo them.
SQL dumped from a grid draft is the exception: running it commits its
transaction as soon as the execution finishes (the editor title shows
`commit on run`), because draft edits are meant to become durable.

| Event | What happens |
|-------|--------------|
| DML executed | `BEGIN` — the keybinds pane shows `tx pending` |
| Grid draft executed | `BEGIN` + the transaction is **committed** when execution finishes (`commit on run`) |
| Rollback pressed | `ROLLBACK` — all statements of the current execution are undone |
| New statement executed | `COMMIT` of the pending transaction, then the new statement runs |
| `DDL`/`SELECT` executed | The pending transaction is committed first; the statement itself runs in autocommit |
| Quit | The pending transaction is rolled back before the connection closes |

A single execution is one rollback unit: `UPDATE …; UPDATE …` shares one
transaction, so a rollback undoes both. Mixing DDL into a batch commits whatever
was pending before it.

`SELECT` and DDL (`CREATE`/`ALTER`/`DROP`/`TRUNCATE`) never open a transaction.

### Widget Modes

These keys act *inside* a focused widget and are not configurable actions.
They are documented as prose in the help overlay:

- **Grid EDIT mode** — type to modify the cell; move the cursor, jump to
  start/end, delete characters; confirm to move to the next column; cancel to
  revert the value.
- **Grid FILTER mode** — type to narrow columns; apply to jump to the first
  match; cancel to discard.
- **Grid WHERE FILTER mode** — type a WHERE clause; apply to re-query the
  database; cancel to discard.
- **Help modal** — scroll through the overlay; close it when done.
- **Query browser** — navigate and manage history/favorites entries.
- **Connection picker** — navigate projects and toggle their active state.

### Mouse

| Action | Effect |
|--------|--------|
| Left click | Select a node/cell, move the cursor, or execute a keybinds-pane action |
| Double click | Expand/collapse a tree node, or view the full cell content |
| Header click | Sort a grid column |
| Wheel | Scroll the focused pane (tree, grid, editor, overlay) |

### Custom Keybinds

Override any default in `~/.config/dbx/config.toml`. The override **replaces
every key** of the action, keyed by its action ID:

```toml
[keybindings.custom]
"ask" = "ctrl+a"
"edit_cell" = "enter"
"navigate_down" = "ctrl+j"
```

Unset a binding by mapping it to an empty string. Because display and dispatch
derive from the same registry, the panel, the help overlay and execution all
change together.
