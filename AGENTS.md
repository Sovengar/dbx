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

La única fuente de verdad de keybinds es el registry: **`internal/config/keybindings_actions.go`** (`defaultActions()`). Encima de esa única entrada se derivan el panel, el modal `?`, el palette y el dispatch.

Cuando se añade, modifica o elimina un keybind:

1. **Registry**: editá `defaultActions()` en `internal/config/keybindings_actions.go` — `ID`, `Keys`, `Section`, `Description`, `Contexts` (vistas donde aplica), `Owner` y `Pending`.
2. **Handler de dispatch**: si es una acción nueva de app, agregá su entrada en la tabla `appActions()` (`internal/app/app.go`). Si la despacha un componente, agregala a su `HandledActions()` y a su switch de `Resolve`.
3. **Nada más**: no hay listas de display paralelas. El panel, el modal `?`, el palette y el README se alimentan del registry.

Los tests que protegen esto viven en `internal/config/keybindings_test.go` (sección/descripción, colisiones por vista) y `internal/app/keybind_coverage_test.go` (cobertura acción↔handler).

**No olvidar**: el test de colisiones falla si dos acciones comparten tecla en la misma vista; el de cobertura falla si una acción no-`Pending` no tiene handler. Para una acción deliberadamente sin handler de TUI todavía, marcá `Pending: true`.


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
