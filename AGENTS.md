# dbx

> **Database x** — TUI database client with native AI integration.

## Build & Install

```bash
make build    # Compile to .local/bin/dbx
make install  # Copy to ~/.local/bin/dbx
make test     # Run tests
make lint     # Run linter
```

## Paso crucial tras cualquier cambio de código

**Desplegar el binario** (los tests/smoke con `go run` no actualizan el
instalado; el usuario ejecuta el bin de `~/.local/bin`, no el repo):

```bash
make install
```

Sin este paso, cualquier verificación que haga el usuario sobre la TUI usa la
versión vieja. Ejecutarlo SIEMPRE al terminar una tarea de código, después de
la verificación (`go build ./... && go vet ./... && go test ./...`).

**No es necesario cerrar la TUI** — en Linux el binario se reemplaza en disco
mientras el proceso sigue corriendo con la copia en memoria. La próxima vez que
abra dbx usará la nueva versión.

## Stack

- **Go 1.22+** with Bubbletea v2 (TUI)
- **Lipgloss v2** for styling
- **pgx v5** for PostgreSQL
- **Cobra** for CLI
- **Viper** for config

## Structure

```
cmd/dbx/          # CLI entry (cobra)
internal/
  app/            # App lifecycle, router, messages
  config/         # Viper config, keybindings
  theme/          # Theme system, styles
  ui/components/  # explorer, grid, editor, palette
  drivers/postgres/ # PostgreSQL driver
  ai/             # session logs, NL→SQL, context
  cli/            # CLI commands
pkg/client/       # Go library for agents
```

## Keybind Changes — Checklist obligatorio

Cuando se añade, modifica o elimina un keybind, SIEMPRE actualizar TODOS
estos lugares (no solo el código):

1. **Código**: `internal/config/keybindings.go` — `DefaultKeybindings()` + `defaultBindings()`
2. **Bottom bar (global)**: `internal/ui/statusbar.go` — `renderActions()` (acciones always-visible)
3. **Bottom bar (contextual)**: `internal/ui/statusbar.go` — `renderContextual()` (acciones por pane)
4. **Help modal `?`**: `internal/ui/modal.go` — sección correspondiente (Global/Explorer/Grid/Editor)
5. **Palette `:`**: `internal/ui/components/palette/commands.go` — si es un action nuevo
6. **README.md** — tabla de keybinds + tabla "Navigation & UI"
7. **docs/KEYBINDS.md** — Action Naming Convention + Global Keybinds + Context-Sensitive Display

**No olvidar**: comprobar que la tecla no choque con otra acción en el mismo
contexto (ej: `e` no puede ser cycle panes Y export al mismo tiempo).

## Conventions

- Use `charm.land/*` import paths for bubbletea, lipgloss, bubbles v2
- View() returns `tea.View` struct, not string
- Mouse mode: `v.MouseMode = tea.MouseModeCellMotion`
- Key events: `tea.KeyPressMsg`

## Debug Logging

- **NEVER remove debug logging** — always keep it, and add MORE when investigating bugs
- Debug logs go to `/tmp/dbx_*.log` files (grid_debug, cell_debug, explorer_debug, app_debug, load_debug)
- Always log: message types received, data counts (rows, columns, widths), key events, state transitions
- Format: `fmt.Fprintf(f, "ComponentName: key=%q value=%d\n", key, value)`
