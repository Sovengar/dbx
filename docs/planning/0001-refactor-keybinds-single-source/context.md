# Context — keybinds-single-source

> Entrada de navegación del executor. Todo lo necesario para no tener que buscar en el repo.
> Repo: `/home/buble/dev/projects/dbx` · Branch: `refactor/keybinds-single-source` (al día con `main`).
> `codegraph`: not_initialized (no disponible en esta sesión) — navegación por símbolo/línea abajo.

## Mapa de archivos a tocar

### 1. `internal/config/keybindings.go` (núcleo — reescribir)
- L3-9 `KeybindingsConfig` / `KeybindRegistry` (hoy `bindings map[string][]string`).
- L11-25 `NewKeybindRegistry` (aplica defaults + `Custom`).
- L27-39 `Match(key, context)` — reemplazar por `Resolve`.
- L41-49 `Flatten()` — reemplazar por `PrimaryKey`/`ActionsFor`.
- L51-56 `inContext(action, context)` — **ELIMINAR** (fallback `global.*`).
- L58-148 `DefaultKeybindings()` — **ELIMINAR** (duplica L150-240).
- L150-240 `defaultBindings()` — fuente a migrar al slice de `Action`.
- **Ojo**: L124-129 y L216-221 declaran `explorer.tab_*` (`1`-`6`) DESPUÉS del bloque grid; al aplanar, preservar.

### 2. `internal/app/app.go` (dispatch + wiring)
- L70-71 campos `keybindRegistry *config.KeybindRegistry` y `keybinds map[string]string`.
- L107-148 `NewModel`: L109-110 `kbr := config.NewKeybindRegistry(...)` + `kbs := kbr.Flatten()`; L119, L134-142 construyen componentes con `kbs`.
- L1703-1846 dispatch principal (comparaciones `key == m.keybinds[...]`): L1703 help, L1708 palette, L1713 rollback, L1724 quit, L1729 grid.commit_pending, L1745 cycle_focus, L1760/1765/1770 focus_*, L1781 query_browser, L1792/1802 grid.focus_preview.
- L1813 `key == "tab"` (explorer→preview), L1830 `key == "tab" || key == "esc"` (preview→explorer) — teclas crudas de navegación a promover.
- L1853-1959 `handlePaletteCommand(action string)` — switch hardcodeado; reutilizar handlers en la tabla nueva.
- L2322-2323 `m.statusbar.SetFocus(m.router.Context())` / `SetHeight`; L2380 `m.statusbar.View()`.
- L1999 `m.statusbar.SetTxPending(...)`.
- `SetEditorOpen`/`SetQueryBrowserOpen`/`SetAutocompleteReady` en L1083, 1381, 1402-1403, 1408, 1425, 1432, 1439, 1656, 1739, 1777, 1786, 1871, 1884, 1944, 1953.

### 3. `internal/app/router.go`
- L5-9 struct `Router{keybindings map[string]string}`.
- L11-17 `NewRouter(kb map[string]string)`.
- L51-53 `Context()` → string de vista (valores: `explorer`, `grid`, `grid-preview`, `explorer-preview`).
- L55-60 `KeyFor(action)` con fallback a `config.DefaultKeybindings()` — eliminar el fallback.

### 4. `internal/ui/statusbar.go` → **rename `keybindspane.go`**
- L14-25 `StatusBar` struct; L27-32 `NewStatusBar(styles, keybinds map[string]string)`.
- L48-53 `keyFor(action)` (fallback `"?"`).
- L55-80 `View()` (título hardcodeado `" Keybinds "` en L79).
- L82-100 `renderActions()` — lista/labels hardcodeados (L87 rollback, L91-97).
- L102-133 `renderContextual()` — **strings 100% crudos** (L106 query browser, L109/111 editor, L116 explorer, L118-120 grid, L122 grid-preview, L124 explorer-preview).
- Setters L34-42 (`SetFocus`, `SetEditorOpen`, `SetQueryBrowserOpen`, `SetTxPending`, ...).

### 5. `internal/ui/modal.go`
- L11-25 `HelpModal` + `NewHelpModal(styles, keybinds map[string]string)`.
- L94-234 `View()` con secciones hardcodeadas: Global L111-121, Explorer L123-136, Grid L138-165, Explorer Preview L167-175, Grid Preview L177-187, Editor L189-196, Query Browser L198-205 (strings crudos L199-204).
- L174 y L199-204 usan texto crudo (`"  tab ..."`, `"  j/k ..."`) → mover a secciones estáticas de modo.
- L236-246 `renderKeybind(action, description)`.

### 6. `internal/ui/components/palette/commands.go`
- L12-17 `command{Name, Alias, Action, Section}`.
- L24-51 `DefaultCommands()` — lista hardcodeada (20 comandos).
- L53-63 `BuildCommands(keybindings map[string]string)` — concatena la tecla al alias.
- L5-10 `CommandSection` consts.
- `palette.go` L27 `New(styles, keybindings map[string]string)`.

### 7. `internal/ui/components/explorerpreview/preview.go` (+ `tabbar.go`)
- preview.go L30, L46 `New(styles, keybinds map[string]string)`.
- L166-186 tabs `explorer.tab_*` vía `keybinds[...]`.
- L194-220 dispatch crudo (`case "up","k"`, `case "down","j"`, `case "left","h"`, `case "right","l"`, `case "g"`, `case "G"`, `case "enter"`).
- tabbar.go L23 `NewTabBar(styles, keybinds map[string]string)`.

### 8. `internal/ui/components/grid/table.go` (+ `tabbar.go`)
- table.go L141 campo `keybinds`, L160 `New(styles, pageSize, keybinds)`.
- L579-760 dispatch: crudos L579 `"esc"`, L586 `"i"`, L590 `"s"`, L592 `"/"`, L595 `"f"`, L646-690 (`j/down`, `k/up`, `h/left`, `l/right`, `g`, `G`, `ctrl+u/d`, `N`, `P`, `n/]/ctrl+right`, `p/[/ctrl+left`, `enter`), L760 `"D"`; vía registry L624, L734 yank, L739 export, L744 delete_row, L749 commit_pending, L754 select_row.
- tabbar.go L23 `NewTabBar(styles, keybinds)`.

### 9. `internal/ui/components/gridpreview/preview.go`
- L46, L77 `New(styles, keybinds map[string]string)`.
- L972-1000 dispatch vía registry (`p.keybinds["grid-preview.*"]`).
- L499-607 dispatch de modo/edición crudo (`esc`, `enter`, `tab`, `ctrl+space`, flechas, `home/end`, `backspace`, `delete`, `ctrl+p/n/u`) → **NO promover** (widget-local).

### 10. `internal/ui/components/explorer/explorer.go`
- L40 campo `keybindings`, L47 `New(styles, zones, keybindings map[string]string)`.
- L95, L128, L137, L146, L157, L171, L176 vía registry (`explorer.*`).
- L190-197 crudos `esc`/`enter`/`backspace`.

### 11. Tests a ajustar
- `internal/config/keybindings_test.go` (L1-89): usa `DefaultKeybindings`, `defaultBindings`, `Match`, `Flatten`.
- `internal/ui/statusbar_test.go` (L1-93): `NewStatusBar`, `renderActions`, `renderContextual`, `SetTxPending`.
- `internal/ui/modal_test.go` (L1-51): `NewHelpModal`.
- `internal/ui/components/palette/commands_test.go` (L1-~90): `DefaultCommands`, `BuildCommands`.
- `internal/app/dml_rollback_helpers_test.go` (L84-103): construye `Model` con `NewKeybindRegistry(...).Flatten()` y pasa `kbs` a componentes.

### 12. Docs
- `docs/KEYBINDS.md` (368 líneas) → **borrar**. Contenido no-keybind a rescatar: L9-31 Action Naming Convention, L68-90 DML Transactions, L121-129/191-203/247-254 Mouse Actions, L298-318 Statusbar/Palette/Help Modal.
- `README.md`: L60 link a `KEYBINDS.md#dml-transactions`; L154-167 "Navigation & UI"; L209-316 tablas de keybinds (Global L211-224, Explorer L226-240, Grid L242-...). → limpiar tablas, dejar puntero a `?`.
- `AGENTS.md`: sección "Keybind Changes — Checklist obligatorio" (7 lugares) → "tocás el registry + su metadata".
- Crear `docs/FEATURES.md`.

## Contratos a respetar / extender
- `config.KeybindRegistry` es el punto de verdad; los componentes NO deben importar `config` directamente si se busca evitar ciclos → introducir interfaz resolver mínima (`Resolve`, `PrimaryKey`, `ActionsFor`) y pasarla.
- `Router.Context()` devuelve strings de vista: `explorer`, `grid`, `grid-preview`, `explorer-preview`. Son las claves de `Contexts`. (Nota: `editor` no está en `Router.panes` L14 ni en `FocusByName` L37-49 — es un panel toggle, no un foco de `router`; para `Contexts` se usa `editor` como contexto lógico y el dispatch de app decide por `m.editorOpen`.)
- `FocusPane` enum y `FocusByName` L37-49.

## Patrón a seguir
- Tabla-driven dispatch: `map[ActionID]func(...)` en `app` reutilizando los cuerpos de `handlePaletteCommand` (L1853-1959) y del bloque L1703-1846.
- Un solo slice de `Action` en `config`, con `ActionsFor(context)` para display y `Resolve(key, context)` para dispatch.

## Convensões / reglas del repo (AGENTS.md)
- `make build` / `make install` / `make test` / `make lint`. **El deploy lo hace el executor con `make install`** al terminar; verificación previa: `go build ./... && go vet ./... && go test ./...`.
- Nunca borrar debug logging; los logs van a `/tmp/dbx_*.log`.
- Si se agrega/mueve/elimina un keybind, el checklist de `AGENTS.md` (7 lugares) queda obsoleto por este refactor → actualizarlo a "registry + metadata".
- Al terminar: `make install` (no cerrar la TUI; el binario se reemplaza en disco).

## Puntos de integración no obvios
- `grid.commit_pending` (L1729) tiene lógica inline (drafts→editor), no solo un case del switch.
- `global.export` y `grid.export` comparten handler (L1925) — decidir si son una acción con 2 contextos o dos acciones.
- `global.focus_explorer`/`focus_grid` tienen tecla vacía (`""`) en defaults (L64-65, L156-157): acciones sin tecla primaria, invocables solo por palette → el modelo debe permitir `Keys` vacío.
- `grid.focus_preview` se comporta distinto según foco (L1792 entrada, L1802 salida) — un actionID con dos handlers contextuales o el handler decide por foco.
- `editor` no es foco de `router`: el contexto `editor` se determina por `m.editorOpen`, no por `router.Context()`.

## Riesgos
Ver `plan.md` §6. Los tres mayores: blast radius en constructores, regresión de dispatch en grid (~30 casos), y definición de la agregación de `HandledActions()` para el test de cobertura.
