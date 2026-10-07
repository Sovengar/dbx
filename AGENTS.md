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

**Desplegar el binario** al terminar la tarea y después de verificar
(`go build ./... && go vet ./... && go test ./...`):

```bash
make install
```

El usuario ejecuta `~/.local/bin/dbx`, no el repo. No hace falta cerrar la TUI:
en Linux el binario se reemplaza en disco y el proceso sigue con la copia en
memoria hasta el próximo arranque.

## CI y protección de `main`

`.github/workflows/ci.yml` es la gate: corre en **todo PR** y en **todo push a
`main`**, sin filtros `paths`. **Tres jobs**, de los que `Mutation` solo corre en
PR no draft:

- **`Lint`**: `make lint` → `fmt-check` (gofmt) + golangci-lint **v2.13.2** (set
  por defecto: errcheck, govet, ineffassign, staticcheck, unused).
- **`Test`**: `go test -race -count=1 -covermode=atomic -coverprofile=… ./...`
  (sin `-short`), luego `Coverage summary` y `Coverage gate` — 100% de las
  líneas tocadas contra el merge-base y el total contra `scripts/coverage-floor`
  (100.00). `internal/app` levanta PostgreSQL con testcontainers; `fd` se instala
  como dependencia cacheada para el test de integración del scanner.
- **`Mutation`**: `scripts/mutate.sh` mide y decide en un paso — colgantes contra
  `.mutation-timeouts`, supervivientes contra `.mutation-allowlist` — con
  `scripts/watchdog.sh` de supervisor. Los suites del gate (`mutate_test.sh`,
  `watchdog_test.sh`) corren en el step `Shell suites`. Presupuesto: cap 180s,
  workers 4, stall 8m, ceiling 13m, reserva 360s, job 20m. Vía local:
  `make mutate-diff` (mismo script).

`.github/workflows/ci-fast.yml` corre en cada push a una rama que no es `main`.
No es required y no sustituye a `ci.yml`: usa `DBX_SKIP_DOCKER=1` (salta los 43
tests de PostgreSQL real) y no lleva mutación — con esa suite reducida marcaría
`NOT_COVERED` a mutantes que sí cubre la integración.

Ruleset **`protect-main`**, reproducible con `scripts/setup-repo-protection.sh`
(la rama sale del default branch real, no está hardcodeada):

- Merge solo vía PR, con `Lint`, `Test` y `Mutation` en verde. Force-push y
  borrado de la rama por defecto bloqueados. `Build` no es required.
- Existe bypass de admin y es deliberado: el camino de trabajo es siempre el PR.
- `delete_branch_on_merge=true`.

Tras un merge: verificar que el workflow de push de `main` quedó verde y que el
badge del README reporta `passing` (el badge cachea unos segundos).

### Esperar a la CI

Para seguir los checks de un PR, esperar con `gh run watch <run-id> --exit-status`
(o `gh pr checks <n> --watch`). Nunca `sleep` + `gh pr checks`: los runs quedan
stale tras un force-push y hay que volver a pedir el id.

## Stack

- **Go 1.22+** with Bubbletea v2 (TUI)
- **Lipgloss v2** for styling
- **pgx v5** for PostgreSQL
- **Cobra** for CLI
- **Viper** for config

## Structure

```
main.go            # entry point (cobra)
main_test.go       # llama a main() en proceso con --help
internal/
  app/             # App lifecycle, router, messages
  cli/             # CLI commands (ask, pipe, replay…)
  config/          # Viper config, keybindings, scanner
  debuglog/        # escritura de los logs /tmp/dbx_*_debug.log
  drivers/postgres/ # PostgreSQL driver + pgxfake
  ai/              # session logs, NL→SQL, context
  store/           # query history
  theme/           # theme system, styles
  ui/              # bordered + components (explorer, grid, editor, palette, ask…)
  testsupport/     # fixtures y fakes compartidos
```

## Keybind Changes — Checklist obligatorio

La única fuente de verdad de keybinds es el registry: **`internal/config/keybindings_actions.go`** (`defaultActions()`). El panel, el modal `?`, el palette y el dispatch se derivan de esa única entrada.

Cuando se añade, modifica o elimina un keybind:

1. **Registry**: editá `defaultActions()` en `internal/config/keybindings_actions.go` — `ID`, `Keys`, `Section`, `Description`, `Contexts` (vistas donde aplica), `Owner` y `Pending`.
2. **Handler de dispatch**: si es una acción nueva de app, agregá su entrada en la tabla `appActions()` (`internal/app/app.go`). Si la despacha un componente, agregala a su `HandledActions()` y a su switch de `Resolve`.
3. **Nada más**: no hay listas de display paralelas.

Tests que lo protegen: `internal/config/keybindings_test.go` (sección/descripción, colisiones por vista) y `internal/app/keybind_coverage_test.go` (cobertura acción↔handler). El de colisiones falla si dos acciones comparten tecla en la misma vista; el de cobertura falla si una acción no-`Pending` no tiene handler. Para una acción sin handler de TUI todavía, marcá `Pending: true`.

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
