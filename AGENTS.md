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

## CI y protección de `main`

CI vive en `.github/workflows/ci.yml`, que absorbió `mutation.yml` — la mutación es
un veredicto de CI, no un producto aparte. `ci-fast.yml` sigue **fuera a propósito**: es
el subconjunto que corre por commit (ver abajo), y `ci.yml` es la gate. Este corre en
**todo PR** y en **todo push a `main`** (sin filtros `paths`: un workflow skipeado
deja los required checks en pending para siempre y bloquea todos los PRs). **Cuatro
jobs**, de los que los dos de mutación solo corren en PRs listos:

- **`Lint`**: `make lint` → `fmt-check` (**gofmt**) + golangci-lint **v2.13.2**
  (versión pineada en el `Makefile`; no hay `.golangci.yml`, corre el set por
  defecto: errcheck, **govet**, ineffassign, staticcheck, unused). Antes de
  `fmt-check` no había **ningún** chequeo de formato en el repo: `gofmt -l .` está
  limpio, pero `make lint` sobre `func F(  a int ) int {` devolvía `0 issues`.
  Es el job **más largo** (112 s) y por eso el que fija el reloj.
- **`Test`**: `go test -race -covermode=atomic -coverprofile=… ./...` (suite
  completo, sin `-short`). Los tests de integración de `internal/app` levantan
  PostgreSQL vía **testcontainers** usando el Docker que ya trae el runner
  `ubuntu-24.04`; no hace falta bloque `services:`. Los tests del scanner
  (`internal/config`) usan un seam de discovery, así que **no** requieren `fd`.
  - **Sin `go build`**, que estaba aquí y se quitó: redundante por tres (`go test
    ./...` compila todos los paquetes — eso es lo que significa `[no test files]` —,
    golangci-lint typechequea, y `go build -x ./...` ni siquiera invoca `link`). Y
    estaba en el camino crítico: costaba 14 s.
  - **Sin `go vet`**, a propósito: `govet` corre **una única vez**, dentro de
    golangci-lint. El paso aparte lo ejecutaba dos veces en cada push.
  - Este es el job que fija el reloj (~74 s); `Lint` ~32 s con caché caliente.
- **`Calibration`** y **`Mutation`**: solo en PR **no draft** (`if: github.event_name ==
  'pull_request' && !…draft`). `ready_for_review` está en los `types` del trigger porque
  **no** viene en el conjunto por defecto (`opened, synchronize, reopened`) — sin él, un
  draft puesto en listo no reportaría el check de mutación y, al ser required, bloquearía
  el merge. Tampoco puede ser *solo* ese evento: cualquier push posterior dejaría el check
  sin reportar en el nuevo SHA. El gate no cambia (`Calibration` decide fail-vs-skip). En
  este repo **no es required check**, porque un job con `if:` se salta y un check saltado
  no se puede exigir.

Aparte, `.github/workflows/ci-fast.yml` (`name: CI fast`) corre en **cada push a cualquier rama que no sea
`main`** (WIP incluido): es la única señal que recibe un commit de rama sin PR, porque
`ci.yml` solo cubre PRs y `main`. **No es required** y no sustituye a `ci.yml` — la única
diferencia funcional es `DBX_SKIP_DOCKER=1`, que salta los 43 tests de PostgreSQL real
(`internal/app`: 22,1 s → 4,0 s). Nunca pongas la mutación ahí: necesita el perfil de
cobertura completo, y sobre esta suite reducida marcaría como `NOT_COVERED` a los mutantes
que sí cubre la integración.

**La mutación es un job de `ci.yml`** (antes era `mutation.yml`). Corre la suite
entera otra vez para sacar la cobertura, y por eso **no** vive en `ci-fast.yml`: sobre
esa suite reducida marcaría `NOT_COVERED` a los mutantes que sí cubre la integración,
que es un perfil equivocado y no uno incompleto. En este repo **no es required check**
— un job con `if:` se salta y un check saltado no se puede exigir — y `make mutate-diff`
sigue siendo la vía local.

Reglas de la rama por defecto (ruleset **`protect-main`**, reproducible con
`scripts/setup-repo-protection.sh`; la rama se deriva del default branch real,
no se hardcodea):

- Merge **solo vía PR**, con los dos required checks (`Lint`, `Test`) en verde;
  force-push y borrado de la rama por defecto bloqueados. `Build` **no es
  required** desde que se fundió con `Test`.
- Existe **bypass de admin** y es **deliberado** (aprobado por el usuario): un
  admin *podría* pushear directo, pero la intención de trabajo es siempre el
  camino PR. Ningún actor no-admin puede hacerlo.
- `delete_branch_on_merge=true`: GitHub borra la rama remota al mergear.

Ante un merge: verificar que el workflow `push` de `main` quedó verde y que el
badge del README reporta `passing` (el badge cachea unos segundos).

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
