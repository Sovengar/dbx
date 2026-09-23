# dbx — Features & Conventions

Non-keybind documentation. For the keybind reference, press `?` inside dbx: the
TUI renders it from the same registry that dispatches keys, so it is always
current.

## Action Naming Convention

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

## DML Transactions

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

## Widget Modes

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

## Mouse

| Action | Effect |
|--------|--------|
| Left click | Select a node/cell, move the cursor, or execute a keybinds-pane action |
| Double click | Expand/collapse a tree node, or view the full cell content |
| Header click | Sort a grid column |
| Wheel | Scroll the focused pane (tree, grid, editor, overlay) |

## Custom Keybinds

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
