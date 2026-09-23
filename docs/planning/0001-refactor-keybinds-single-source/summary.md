# Summary: keybinds-single-source

## Metadata
- **Completed:** 2026-09-23 20:34
- **Duration:** ~45 minutes
- **Plan Number:** 0001
- **Branch:** `refactor/keybinds-single-source` (base `main` @ `9924555`)
- **ADR:** `docs/adr/0001-keybind-registry-single-source.md` (Accepted)

## Objetivo

Que la definición de keybinds sea **una sola estructura por acción** (tecla +
metadata) y que de ella se deriven **display** (panel, modal `?`, palette) y
**dispatch** (app + componentes). Eliminar el concepto de "keybind global", la
duplicación `DefaultKeybindings()`/`defaultBindings()`, las listas de display
hardcodeadas y las tablas de docs; la TUI pasa a ser la referencia viva.

## Resultado

- `internal/config` expone un **slice plano de 74 `Action{ID, Keys, Section,
  Description, Contexts, Owner, Pending}`** (`keybindings_actions.go`) y una
  interfaz mínima `Resolver` (`Resolve`, `PrimaryKey`, `KeysFor`, `ActionsFor`,
  `All`) que consumen app y componentes. Display y dispatch no pueden divergir.
- **App**: dispatch unificado en la tabla `map[ActionID]handler` (`appActions()`);
  se eliminaron las comparaciones `key == m.keybinds[...]` y el `switch action`
  hardcodeado de `handlePaletteCommand`.
- **Componentes** (`grid`, `explorer`, `gridpreview`, `explorerpreview`):
  dispatch por `Resolve(key, context)` + switch sobre **actionID**; cada uno
  expone `HandledActions()`.
- **UI**: `StatusBar` → **`KeybindsPane`** (`keybindspane.go`), `renderActions`/
  `renderContextual` derivados de `ActionsFor(context)`; modal `?` derivado de
  `All()`/`ActionsFor` + secciones estáticas de modos; palette derivado de la
  metadata del registry.
- **Eliminado**: `DefaultKeybindings()`, `defaultBindings()`, `Match()`,
  `Flatten()`, `inContext()` y los action IDs `global.*`. `docs/KEYBINDS.md`
  borrado.
- **Docs**: README sin tablas de keybinds (puntero a `?`), `docs/FEATURES.md`
  nuevo (Action Naming Convention, DML Transactions, prosa no-keybind),
  checklist de `AGENTS.md` reescrito a "registry + handler".

## Cambios clave por módulo

| Módulo | Cambio |
|--------|--------|
| `internal/config/keybindings.go` + `keybindings_actions.go` | Registry único, slice de 74 `Action`, `Resolver`. |
| `internal/app/app.go`, `router.go` | Tabla `map[ActionID]handler`; router usa el resolver. |
| `internal/ui/keybindspane.go` | Rename de statusbar; display derivado. |
| `internal/ui/modal.go` | Modal `?` derivado + secciones estáticas de modo. |
| `internal/ui/components/palette/` | Comandos derivados del registry. |
| `internal/ui/components/{grid,explorer,gridpreview,explorerpreview}/` | Dispatch por actionID + `HandledActions()`. |
| `docs/FEATURES.md`, `README.md`, `AGENTS.md` | Referencia viva en la TUI; sin tablas paralelas. |

## Decisiones

- **Modelo A** — la acción declara sus `Contexts`; una sola lista plana, sin
  mapa vista→acciones que pueda desincronizarse. Alternativa B rechazada (ver ADR).
- **`ask` preservado como `Pending: true`** — la feature NL→SQL existe como CLI
  (`internal/cli/ask.go`); su handler de TUI está fuera de alcance. Exento del
  test de cobertura.
- **Teclas de modo no promovidas** — EDIT/FILTER/WHERE, picker y query browser
  son interacción *dentro* de un widget, no acciones configurables; quedan como
  prosa estática en el modal/FEATURES.md.
- **Menos commits de los planeados** — el plan describía 11 pasos atómicos; la
  implementación quedó en 2 commits de refactor (+ fixes y chores), cada uno con
  build verde. No se hizo squash de nada.

## Escenarios (behavior.feature)

| Scenario | Status |
|----------|--------|
| La tecla que se muestra es la que ejecuta la acción | ✅ Passed |
| Customizar una tecla cambia display y ejecución a la vez | ✅ Passed |
| El panel muestra solo las acciones de la vista actual (Explorer) | ✅ Passed |
| Cambiar de vista cambia el contenido del panel | ✅ Passed |
| "q" cierra la app desde las vistas de navegación | ✅ Passed |
| "q" es un carácter literal en el editor, no un atajo de salida | ✅ Passed |
| Una acción deja de aplicar en una vista donde no tiene sentido | ✅ Passed |
| El modal de ayuda cubre todas las secciones operables | ✅ Passed |
| El modal de ayuda es navegable y se cierra | ✅ Passed |
| "Ask AI (NL→SQL)" conserva su binding y su metadata | ✅ Passed |
| Una acción con handler pendiente no rompe la validación | ✅ Passed |
| Toda acción declarada tiene sección y descripción | ✅ Passed |
| No hay colisiones de tecla dentro de una misma vista | ✅ Passed |
| Toda acción tiene handler, salvo las marcadas como pendientes | ✅ Passed |
| La documentación de keybinds deja de duplicar la información | ✅ Passed |
| La prosa que no es de keybinds se conserva en un doc de features | ✅ Passed |
| El checklist de contribución refleja el nuevo flujo | ✅ Passed |

## Commits

- `9dff06a` chore(planning): add keybinds single-source plan
- `e9f920d` refactor(keybinds): make the action registry the single source of truth
- `9d5cd00` docs(keybinds): the TUI is the single keybind reference
- `9a615ae` fix(grid): restore the two-step refresh flow for 'r'
- `e12e436` refactor(keybinds): make action<->handler coverage honest
- `8c6cd32` fix(palette): drop the broken 'Preview JQ Filter' command
- `b0e97ea` docs(plan): replace dangling KEYBINDS.md reference with FEATURES.md
- `8da0001` chore(grid): drop unused SetRefreshPending
- `b2df816` chore(grid): remove dead TabBar with dangling action ids

## Files

- **Created**: `internal/config/keybindings_actions.go`, `internal/ui/keybindspane.go`,
  `internal/ui/keybindspane_test.go`, `docs/FEATURES.md`,
  `docs/adr/0001-keybind-registry-single-source.md`,
  `internal/app/{keybind_coverage,keybind_behavior,refresh,docs}_test.go`,
  `internal/ui/components/grid/refresh_test.go`, `handled.go` en cada componente.
- **Modified**: `internal/config/keybindings.go`, `internal/app/app.go`,
  `internal/app/router.go`, `internal/ui/modal.go`, `internal/ui/components/**`,
  `README.md`, `AGENTS.md`, `docs/PLAN.md`, tests existentes.
- **Deleted**: `docs/KEYBINDS.md`, `internal/ui/statusbar.go` (+ test),
  `internal/ui/components/grid/tabbar.go`.

## Tests

- **Added**: sección/descripción y colisiones por vista (`internal/config`),
  cobertura acción↔handler y owner↔handler (`internal/app/keybind_coverage_test.go`),
  `q` literal en editor / `q` sale en explorer (`keybind_behavior_test.go`),
  regresión de refresh de dos pasos (`grid/refresh_test.go` + `app/refresh_test.go`),
  display==dispatch.
- **System Tests**: ✅ Passed — `go build ./... && go vet ./... && go test ./... -count=1` verde.

## Documentation

- **Changelog**: ❌ No se actualizó — el repo **no mantiene `CHANGELOG.md`**
  (verificado: no existe en `main` ni en la rama). No se creó por decisión de
  cierre: no hay convención de changelog en este repo.
- **Docs**: `docs/KEYBINDS.md` borrado; `docs/FEATURES.md` creado; README y
  `AGENTS.md` actualizados.
- **ADR**: ✅ Creado (`docs/adr/0001-keybind-registry-single-source.md`).

## Code Review Issues

Hallazgos del review resueltos (labels reconstruidos desde los commits de fix):

- **H1 — Regresión de refresh en grid (`r`)**: la app interceptaba
  `refresh_data` antes del grid y usaba el handler de palette, que con drafts
  solo marcaba el flag y nunca completaba la recarga. Resuelto en `9a615ae`
  delegando a `Grid.Refresh()` (confirmación de dos pasos), con tests de
  regresión a nivel grid y app.
- **M1 — Cobertura acción↔handler deshonesta**: los componentes declaraban en
  `HandledActions()` acciones que la app intercepta antes (export, refresh_data,
  undo_drafts, commit_drafts, clear_editor). Resuelto en `e12e436` recortando
  `HandledActions()` y moviendo `clear_editor` a `Owner: app`.
- **M2 — Comando de palette roto ("Preview JQ Filter")**: `jq_filter` es un modo
  local de widget sin handler de app; la entrada caía en un toast
  `Command: jq_filter`. Resuelto en `8c6cd32` eliminando el comando del palette.
- **M3 — `grid.TabBar` muerto con action ids colgados**: nunca se instanciaba y
  referenciaba `grid_tab_*` inexistentes, por lo que `PrimaryKey` devolvía `""`.
  Resuelto en `b2df816` eliminándolo.
- **Follow-on chores**: `8da0001` borra `SetRefreshPending` (sin callers tras H1);
  `b0e97ea` reemplaza una referencia colgada a `KEYBINDS.md` en el plan por
  `FEATURES.md`.

- **Critical Found**: 1 (H1) — resuelto
- **High Found**: 0 pendientes
- **User Decision**: aprobado continuar y cerrar

## Deuda conocida restante

- `ask` (`a`) sigue `Pending: true`: falta el handler de TUI para NL→SQL (la CLI
  `internal/cli/ask.go` funciona). Fuera de alcance por decisión del plan.
- Teclas de modo de widgets (EDIT/FILTER/WHERE, picker, query browser) no son
  acciones del registry; se documentan como prosa estática. Decisión consciente.
- `make lint` no corre en este entorno (`golangci-lint` no instalado). No es
  deuda del refactor; `go vet` cubre lo básico.

## Cómo verificar

```bash
cd ~/dev/projects/dbx.refactor-keybinds-single-source
git status                    # limpio
go build ./... && go vet ./...
go test ./... -count=1        # verde
make install                  # desplegar el binario (regla AGENTS.md)
# En la TUI: ? abre el modal; el panel y el palette muestran las mismas teclas
# que se ejecutan; customizar una tecla en config cambia ambos.
```

## Next Step

Abrir PR de `refactor/keybinds-single-source` → `main` (rama ya pusheada).
