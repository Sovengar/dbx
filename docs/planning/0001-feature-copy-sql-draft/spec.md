# Spec: Copy SQL to Clipboard from Editor

## Requirements

### Functional
1. When the SQL Editor is focused, pressing `Ctrl+Y` copies the entire editor content to the system clipboard
2. A toast notification "SQL copied to clipboard" appears after successful copy
3. If the editor is empty, no action is taken (no toast, no clipboard write)
4. If clipboard tools are unavailable (no pbcopy/wl-copy/xclip/xsel), show error toast "Clipboard not available"
5. The editor content remains unchanged after copying (non-destructive)

### Keybind Checklist (6 surfaces)
1. `internal/config/keybindings.go` — add `editor.copy` in `DefaultKeybindings()` + `defaultBindings()`
2. `internal/ui/statusbar.go` — add `Ctrl+Y copy` to editor contextual line
3. `internal/ui/modal.go` — add `editor.copy` entry in Editor section of help modal
4. `internal/ui/components/palette/commands.go` — add "Copy SQL" command
5. `docs/KEYBINDS.md` — add row to SQL editor table
6. `README.md` — add to keybinds table if present

### Architecture
- `SQLEditor` emits `CopySQLMsg` (new tea.Msg) when `Ctrl+Y` is pressed
- `app.go` handles `CopySQLMsg` → calls `copyToClipboard(m.editor.Content())` → toast
- No changes to `copyToClipboard()` — reuses existing helper

## Acceptance Criteria

- [ ] `Ctrl+Y` in editor copies SQL to clipboard
- [ ] Toast "SQL copied to clipboard" appears on success
- [ ] Empty editor → no action
- [ ] All 6 keybind surfaces updated
- [ ] No conflict with existing `Ctrl+Y` in any context
