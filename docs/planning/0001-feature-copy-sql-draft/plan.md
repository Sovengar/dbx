---
adr_required: false
---

# Plan: Copy SQL to Clipboard from Editor

## Overview

Add `Ctrl+Y` keybind to the SQL Editor that copies its content to the system clipboard with toast feedback. Reuses the existing `copyToClipboard()` helper.

## Changes

### 1. Message type (`internal/app/app.go`)

Add `CopySQLMsg` struct (empty — signal-only). Handle it in `Update()`:
- Guard: `m.editorOpen && m.editor.Content() != ""`
- Call `copyToClipboard(m.editor.Content())`
- On success: toast "SQL copied to clipboard"
- On error: toast "Clipboard not available"

### 2. Editor keybind (`internal/ui/components/editor/sql.go`)

In `handleKey()`, add case for `ctrl+y`:
- Return `CopySQLMsg{}` (emitted to parent)
- Return `nil, true` (consumed)

### 3. Keybind registration (`internal/config/keybindings.go`)

- `DefaultKeybindings()`: add `"editor.copy": "ctrl+y"`
- `defaultBindings()`: add `"editor.copy": {"ctrl+y"}`

### 4. Statusbar (`internal/ui/statusbar.go`)

In `renderContextual()` editor block, append `Ctrl+Y copy` to the existing line.

### 5. Help modal (`internal/ui/modal.go`)

In Editor section, add: `m.renderKeybind("editor.copy", "Copy SQL")`

### 6. Palette (`internal/ui/components/palette/commands.go`)

Add: `{Name: "Copy SQL", Alias: "copy", Action: "editor.copy", Section: SectionQuery}`

### 7. Documentation

- `docs/KEYBINDS.md`: add row to SQL editor table
- `README.md`: add to keybinds table if present

## Verification

1. `go build ./... && go vet ./... && go test ./...`
2. `make install`
3. Manual: open editor with SQL → `Ctrl+Y` → paste somewhere → verify content + toast
4. Manual: open empty editor → `Ctrl+Y` → verify nothing happens
5. Manual: `?` help → Editor section shows Copy SQL
