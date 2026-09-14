# Proposal: Copy SQL to Clipboard from Editor

## Problem

When the SQL Editor opens with a pre-loaded query (e.g., from grid draft commit via Ctrl+S, or from explorer DDL), there's no way to copy that SQL to the clipboard before executing it. Users may want to:
- Paste the generated SQL into an external tool for review
- Save it somewhere else before running it
- Share it with a colleague

Currently the only options are: execute it or clear it (`Ctrl+U`).

## Intended Outcome

Add a `Ctrl+Y` keybind in the SQL Editor that copies the current editor content to the system clipboard, with a toast confirmation.

## Scope

**In:**
- New `editor.copy` action mapped to `Ctrl+Y`
- Reuse existing `copyToClipboard()` helper from `app.go`
- Toast notification "SQL copied to clipboard"
- Full keybind checklist: keybindings.go, statusbar.go, modal.go, palette, KEYBINDS.md, README.md

**Out:**
- Copy individual lines or selections (editor has no selection model)
- Copy query history entries
- Any changes to the execution flow

## Approach

1. Add `editor.copy` to keybindings defaults
2. In `SQLEditor.handleKey()`, detect `ctrl+c` and emit a new `CopySQLMsg` to the parent
3. In `app.go`, handle `CopySQLMsg` → call `copyToClipboard(m.editor.Content())` + toast
4. Update all keybind documentation surfaces (6 places per checklist)
