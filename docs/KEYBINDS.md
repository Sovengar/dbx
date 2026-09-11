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
- `c` = **c**hange (edit)
- `d` = **d**elete
- `e` = **e**xport
- `f` = **f**ilter
- `g` = **g**o to (first/last)
- `i` = **i**nsert
- `n` = **n**ext
- `o` = **o**pen/insert row
- `p` = **p**revious
- `q` = **q**uit
- `r` = **r**efresh
- `s` = **s**ort
- `u` = **u**pdate
- `v` = **v**iew DDL
- `y` = **y**ank (copy)

## Global Keybinds

These work everywhere:

| Key | Action | Description |
|-----|--------|-------------|
| `q` | Quit | Exit dbx |
| `?` | Help | Show keybinds modal |
| `:` | Palette | Open command palette |
| `Ctrl+P` | Palette | Open command palette (alt) |
| `Tab` | Focus Next | Cycle to next pane |
| `Shift+Tab` | Focus Prev | Cycle to previous pane |
| `1` | Focus Explorer | Jump to explorer pane |
| `2` | Focus Grid | Jump to grid pane |
| `3` | Focus Editor | Jump to editor pane |
| `a` | Ask AI | Open NL→SQL prompt |
| `e` | Export | Export current data |
| `r` | Refresh | Refresh current view |
| `Ctrl+C` | Quit | Force quit |

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

Data table:

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
| Next Page | `n` | `Ctrl+Right` | Next page |
| Prev Page | `p` | `Ctrl+Left` | Previous page |
| Edit Cell | `i` | `Enter` | Edit cell inline |
| Delete Row | `d` | `Delete` | Delete row |
| Insert Row | `o` | `Ctrl+I` | Insert new row |
| Yank | `y` | `Ctrl+C` | Copy cell value |
| Sort | `s` | `Ctrl+S` | Cycle sort (asc/desc/none) |
| Filter | `/` | `Ctrl+F` | Filter by column |

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

## Editor Pane

SQL editor:

| Key | Vim | Modern | Action |
|-----|-----|--------|--------|
| Execute | `Ctrl+Enter` | `F5` | Run query |
| Clear | `Ctrl+U` | `Ctrl+Delete` | Clear editor |
| External | `Ctrl+G` | `Ctrl+E` | Open in $EDITOR |
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
[Enter] Open  [d] Drop  [n] New  [v] DDL  [/] Filter  [r] Refresh

[Grid-focused]
[i] Edit  [d] Delete  [o] Insert  [y] Copy  [s] Sort  [/] Filter  [e] Export

[Editor-focused]
[Ctrl+Enter] Run  [Ctrl+U] Clear  [Ctrl+G] External  [Tab] Complete
```

## Default Mode: Vim

If no mode is specified, Vim mode is used. Vim mode includes:

- **Normal mode**: Navigation and commands
- **Insert mode**: Text editing (in editor)
- **Visual mode**: Selection (future)

### Vim Motions in Grid

| Motion | Description |
|--------|-------------|
| `w` | Next word |
| `b` | Previous word |
| `0` | Start of line |
| `$` | End of line |
| `gg` | First line |
| `G` | Last line |
| `Ctrl+D` | Half page down |
| `Ctrl+U` | Half page up |

## Mode Switching

Switch modes via:

1. **Config**: `keybindings.mode = "modern"`
2. **Command palette**: `:theme modern`
3. **Runtime**: Future (keybind to cycle modes)
