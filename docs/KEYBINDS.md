# dbx Keybinds Reference

## Overview

dbx supports three keybinding modes:
- **Vim** (default): h/j/k/l navigation, modal editing
- **Modern**: Arrow keys, intuitive shortcuts
- **Emacs**: Ctrl-based shortcuts

All modes share the same **action-based keybinds** where the primary key is the first letter of the action.

## Action Naming Convention

Keybinds follow the **first-letter rule**:
- `a` = **a**sk (NL→SQL)
- `d` = **d**elete
- `e` = **e**xplorer toggle
- `x` = e**x**port
- `f` = **f**ilter
- `g` = **g**o to (first/last)
- `i` = **i**nsert
- `n` = **n**ext
- `o` = **o**pen FK reference
- `p` = **p**revious
- `q` = **q**uit
- `r` = **r**efresh
- `s` = **s**ort
- `v` = **v**iew DDL
- `y` = **y**ank (copy)

## Global Keybinds

These work everywhere:

| Key | Action | Description |
|-----|--------|-------------|
| `q` | Quit | Exit dbx |
| `Ctrl+C` | Quit | Force quit |
| `?` | Help | Show keybinds modal |
| `:` | Palette | Open command palette |
| `e` | Toggle Explorer | Toggle Explorer pane |
| `E` | Toggle Editor | Toggle Editor pane |
| `a` | Ask AI | Open NL→SQL prompt |
| `x` | Export | Export current data |

## Explorer Pane

Schema tree navigation:

| Key | Vim | Modern | Action |
|-----|-----|--------|--------|
| Move Down | `j` | `↓` | Select next node |
| Move Up | `k` | `↑` | Select previous node |
| Expand | `l` | `→` | Expand node |
| Collapse | `h` | `←` | Collapse / go to parent |
| Open/Load | `Enter` | `Enter` | Load table data |
| First | `g` | `Home` | Jump to first node |
| Last | `G` | `End` | Jump to last node |
| Filter | `/` | `Ctrl+F` | Filter tables |
| New Table | `n` | `Ctrl+N` | Create new table |
| Drop | `d` | `Delete` | Drop selected table |
| View DDL | `v` | `Ctrl+V` | View table DDL |
| Refresh | `r` | `F5` | Refresh tree |

### Mouse Actions

| Action | Effect |
|--------|--------|
| Left Click | Select node |
| Double Click | Expand/Collapse |
| Right Click | Context menu |
| Scroll | Navigate tree |

## Grid Pane

### NORMAL mode

| Key | Vim | Modern | Action |
|-----|-----|--------|--------|
| Row Down | `j` | `↓` | Next row |
| Row Up | `k` | `↑` | Previous row |
| Col Right | `l` | `→` | Next column |
| Col Left | `h` | `←` | Previous column |
| First Row | `gg` | `Home` | Jump to first row |
| Last Row | `G` | `End` | Jump to last row |
| Half Page Up | `Ctrl+U` | `PageUp` | Scroll up half page |
| Half Page Down | `Ctrl+D` | `PageDown` | Scroll down half page |
| Next Page | `n` or `]` | `Ctrl+Right` | Next page |
| Prev Page | `p` or `[` | `Ctrl+Left` | Previous page |
| First Page | `P` | `Shift+Left` | Jump to first page |
| Last Page | `N` | `Shift+Right` | Jump to last page |
| Go to Page | `F1`-`F9` | `F1`-`F9` | Jump to page 1-9 |
| Tab Records | `1` | `1` | Show records tab |
| Tab Columns | `2` | `2` | Show columns tab |
| Tab Constraints | `3` | `3` | Show constraints tab |
| Tab Foreign Keys | `4` | `4` | Show foreign keys tab |
| Tab Indexes | `5` | `5` | Show indexes tab |
| Focus Preview | `Tab` | `Tab` | Focus preview pane |
| Edit Cell | `enter` | `Enter` | Enter EDIT mode |
| Delete Row | `d` | `Delete` | Delete row |
| Insert Row | `i` | `Ctrl+I` | Insert new row |
| Select Row | `space` | `Space` | Toggle row selection |
| Sort | `s` | `Ctrl+S` | Cycle sort (asc/desc/none) |
| Filter | `/` | `Ctrl+F` | Filter by column (WHERE) |
| Find Column | `f` | `Ctrl+F` | Jump to column by name |
| Yank | `y` | `Ctrl+C` | Export (SQL/JSON/CSV) |
| Open FK | `o` | `Ctrl+O` | Open referenced table |
| Go Back | `H` | `Shift+Backspace` | Return to previous table |
| Refresh | `r` | `F5` | Refresh data (re-execute query) |
| Commit | `Ctrl+S` | `Ctrl+S` | Commit pending inserts |
| Discard | `D` | `Shift+D` | Discard all draft changes |

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

| Key | Vim | Modern | Action |
|-----|-----|--------|--------|
| Scroll Up | `k` | `↑` | Scroll up |
| Scroll Down | `j` | `↓` | Scroll down |
| First | `g` | `Home` | Jump to top |
| Last | `G` | `End` | Jump to bottom |
| Half Page Up | `Ctrl+U` | `PageUp` | Scroll up half page |
| Half Page Down | `Ctrl+D` | `PageDown` | Scroll down half page |
| Focus Explorer | `e` | `Ctrl+E` | Focus explorer pane |

## Editor Pane

SQL editor:

| Key | Vim | Modern | Action |
|-----|-----|--------|--------|
| Execute | `Ctrl+Enter` | `F5` | Run query |
| Clear | `Ctrl+U` | `Ctrl+Delete` | Clear editor |
| History Prev | `Ctrl+P` | `Alt+Up` | Previous query |
| History Next | `Ctrl+N` | `Alt+Down` | Next query |
| Complete | `Tab` | `Tab` | Autocomplete |

### Mouse Actions

| Action | Effect |
|--------|--------|
| Left Click | Position cursor |
| Right Click | Context menu |
| Scroll | Scroll editor |

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
| `↑/↓` | Navigate results |
| `j/k` | Navigate results (vim) |

## Help Modal

Keybinds reference:

| Key | Action |
|-----|--------|
| `j/k` or `↑/↓` | Scroll |
| `g/G` | First/Last |
| `Esc` or `?` | Close |

## Custom Keybinds

Edit `~/.config/dbx/config.toml`:

```toml
[keybindings]
mode = "vim"  # or "modern" or "emacs"

[keybindings.custom]
# Override any action
"global.ask" = "ctrl+a"
"grid.edit_cell" = ["enter", "i", "ctrl+e"]
```

## Context-Sensitive Display

The statusbar shows relevant keybinds based on current context:

```
[Explorer-focused]
/ filter · n new · d drop · v DDL · r refresh · Enter Open table data · Space View columns

[Grid-focused]
1-5 tabs · / filter · n/p N/P page · F1-9 goto · s sort · f find column · r refresh
Enter edit · d delete · i insert · space select · y export · o FK nav · H go back · Tab preview

[Grid Preview-focused]
Tab back · j/k scroll · g/G first/last · e explorer

[Editor-focused]
Ctrl+Enter execute · Ctrl+U clear · Ctrl+P/N history
```

## Default Mode: Vim

If no mode is specified, Vim mode is used. Vim mode includes:

- **Normal mode**: Navigation and commands
- **Insert mode**: Text editing (in editor)
- **Visual mode**: Selection (future)

## Mode Switching

Switch modes via:

1. **Config**: `keybindings.mode = "modern"`
2. **Command palette**: `:theme modern`
3. **Runtime**: Future (keybind to cycle modes)
