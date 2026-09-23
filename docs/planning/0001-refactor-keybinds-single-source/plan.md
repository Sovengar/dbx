# Plan — Keybinds: una sola fuente de verdad (display + dispatch)

- **Slug**: `keybinds-single-source`
- **Branch**: `refactor/keybinds-single-source`
- **Nivel**: PIPELINE
- **adr_required**: `true`
  - **Razón**: hay una decisión de arquitectura con alternativas descartadas (cómo modelar acción↔vista y cómo hacer que dispatch y display compartan la fuente) y un cambio de contratos internos (constructores que hoy reciben `map[string]string` pasan a recibir el registry/resolver).
  - **Título propuesto del ADR**: `keybind-registry-single-source`

## Objetivo

Que la definición de keybinds sea **una sola estructura por acción** (clave + metadata) y que de ella se deriven **display y dispatch**. Eliminar el concepto de "keybind global", la duplicación `DefaultKeybindings()`/`defaultBindings()`, las listas de display hardcodeadas y las tablas de docs. La TUI (`?` + panel) pasa a ser la referencia viva.

## Diagnóstico real (verificado)

Hay **cuatro** fuentes paralelas, no tres:

1. **Registry duplicado**: `DefaultKeybindings()` (primaria) y `defaultBindings()` (todas las teclas) en `internal/config/keybindings.go` repiten la misma información.
2. **Display hardcodeado**: `renderActions()`/`renderContextual()` (statusbar), `modal.go`, `palette/commands.go`, y las tablas de `README.md`/`docs/KEYBINDS.md`.
3. **Dispatch de app**: `app.go` compara `m.keybinds["<action>"]` directo y despacha con un `switch action` hardcodeado; `KeybindRegistry.Match()` **no se usa en producción** (solo en tests).
4. **Dispatch de componentes**: `grid`, `explorer`, `gridpreview` y `explorerpreview` despachan con **strings de teclas crudos** (`case "j", "down":`, `case "esc":`), usando el map del registry solo parcialmente (`keybinds["grid.yank"]`).

Consecuencia: `defaultBindings()` **ya contiene** las listas multi-tecla que los componentes replican a mano (flechas, `]`/`[`, `ctrl+*`). Ese es el punto de unificación.

Caso especial: `global.ask` ("a") no es basura — la feature Ask AI/NL→SQL existe como **CLI** (`internal/cli/ask.go` + `internal/ai/nl2sql/`); falta el **handler de TUI**. Se preserva como acción con **handler pendiente**.

---

## 1. Decisión de diseño: Modelo A vs Modelo B

Pregunta: cómo modelar la pertenencia **acción↔vista** sin copiar teclas a mano.

### Modelo A — la acción declara sus vistas (`Contexts []string`)

Una única lista plana de acciones; cada acción lista las vistas donde aplica.

- `quit`: `Contexts: [explorer, grid, grid-preview, explorer-preview]` → **no** incluye `editor`.
- `copy_sql` (`Ctrl+Y`): `Contexts: [editor]` → no aparece ni dispara en grid.
- Una acción puede vivir en varias vistas sin duplicar teclas (`help`, `palette` en todas).

| Dimensión | Evaluación |
|---|---|
| Ergonomía al agregar/quitar una vista | Media: hay que tocar el `Contexts` de cada acción que aplique a la vista nueva. Para una vista nueva normalmente son acciones nuevas, así que en la práctica es bajo. |
| Validación de colisiones | **Alta**: una sola pasada agrupando por `(context, key)`. |
| Duplicación | **Nula**: todo vive en un solo slice; las teclas se declaran una vez. |
| Testabilidad | **Alta**: se itera la lista plana para sección/descripción y se agrupa por contexto para colisiones. |
| Fuente única | **Sí**: una sola estructura. |
| Orden de display | Se deriva del orden del slice + agrupación por `Section`. |
| Caso `q` no en editor | Trivial: excluir `editor` del `Contexts`. |
| Caso `Ctrl+Y` no en grid | Trivial: `Contexts: [editor]`. |

### Modelo B — la vista declara sus acciones (`map[view][]actionID`)

Registry con acciones + un mapa vista→lista de acciones.

| Dimensión | Evaluación |
|---|---|
| Ergonomía al agregar/quitar una vista | **Alta**: se escribe/edita una sola lista por vista. |
| Validación de colisiones | Media: hay que hacer join (vista→IDs→keys) antes de comparar. |
| Duplicación | **Sí**: los IDs de acciones compartidas se repiten en cada lista de vista; el orden de display se duplica como dato. |
| Testabilidad | Alta, pero con más indirección (detectar huérfanas: acción sin vista, vista con ID inexistente). |
| Fuente única | **No del todo**: dos estructuras (acción + mapeo vista→acciones) que pueden desincronizarse. |
| Caso `q` no en editor | Trivial: omitirlo de la lista de editor. |
| Caso `Ctrl+Y` no en grid | Trivial: omitirlo. |

### Recomendación: **Modelo A**

Razones decisivas:
1. **Cumple "única fuente" de verdad**: el Modelo B reintroduce una segunda estructura (vista→acciones) que es exactamente el problema que estamos eliminando (dos lugares que se desincronizan).
2. **Colisiones y validaciones más simples**: agrupar por `(context, key)` en una sola pasada, sin joins.
3. **Sin duplicación de IDs** para acciones compartidas (`help`, `palette`, `q`).
4. Los dos casos críticos (`q` no en editor, `Ctrl+Y` no en grid) son exclusiones explícitas en un solo campo.

El único punto a favor de B (editar una vista en un solo lugar) se mitiga con un **índice derivado**: el registry puede exponer `ActionsFor(context)` para debug/inspección, sin que eso sea una fuente paralela (es una vista calculada).

---

## 2. Diseño del registry unificado

### Forma de la estructura `Action` (descripción, no código)

Un slice plano de acciones; cada `Action` con:

- **ID**: identificador semántico estable y **agnóstico de vista** (ej. `quit`, `help`, `palette`, `navigate_down`, `copy_sql`). Deja de usarse el prefijo como contexto (`grid.down`); el contexto va en `Contexts`.
- **Keys []string**: **todas** las teclas de la acción (primaria + alias: flechas, `]`/`[`, `ctrl+*`). Absorbe lo que hoy está en `defaultBindings()`.
- **Section**: agrupación de display (ej. `Navigation`, `Data`, `Query`, `UI`).
- **Description**: texto mostrado en panel y modal.
- **Contexts []string**: vistas donde aplica (Modelo A).
- **Owner**: módulo que despacha la acción (ej. `app`, `grid`, `explorer`, `gridpreview`, `explorerpreview`, `editor`, `modal`). Habilita el test acción↔handler.
- **Pending bool**: acción declarada con handler intencionalmente pendiente (caso `ask`). Exenta del test de cobertura de handler.

### Eliminación de la duplicación

`DefaultKeybindings()` y `defaultBindings()` se **reemplazan** por el slice de `Action`. Se mantiene, si hace falta compatibilidad transitoria, un método derivado `PrimaryKey(action)` (equivalente a `Keys[0]`) para no tocar todo de golpe; `DefaultKeybindings()` desaparece al final del refactor.

### API del registry (reemplaza `Match`/`Flatten`/`inContext`)

- `ActionsFor(context) []Action`: acciones activas en una vista (para panel/modal).
- `Resolve(key, context) (ActionID, bool)`: tecla→acción en una vista (para dispatch). Reemplaza `Match` y elimina el fallback `global.*`.
- `PrimaryKey(action) string` / `KeysFor(action) []string`.
- `All() []Action`: para validaciones y para el modal completo.
- `inContext` y el concepto "global" **se eliminan**.

### Custom keybinds (`KeybindingsConfig.Custom`)

Se mantiene `map[string]string` **keyed por action ID**. Semántica preservada: el override **reemplaza todas las teclas** de la acción por la única tecla configurada (hoy `r.bindings[k] = []string{v}`). Se aplica sobre el slice de acciones al construir el registry, y como `Resolve`/`ActionsFor` derivan del registry, display y dispatch cambian juntos.

### Dispatch como fuente única

- **App**: el `switch action` de `handlePaletteCommand` y las comparaciones `m.keybinds[...]` se unifican en una **tabla `map[ActionID]handler`** en `app`. El evento de tecla se traduce una vez con `Resolve(key, context)` → actionID → se invoca el handler. Los handlers de acciones que hoy están en `handlePaletteCommand` se reutilizan.
- **Componentes**: cada componente que hoy compara strings crudos resuelve `Resolve(key, context)` y hace switch sobre el **actionID** (no sobre la tecla). Las listas de teclas quedan únicamente en el registry.
- **Contratos internos**: los constructores que hoy reciben `map[string]string` pasan a recibir un **resolver/view-scoped registry** (interfaz mínima con `Resolve`/`PrimaryKey`/`ActionsFor`), evitando acoplar los componentes a `config`.

---

## 3. Inventario acciones ↔ handlers

Se construye la lista plana deduplicando `defaultBindings()` + la primaria + las listas de display (statusbar/modal/palette/docs). Cada acción queda con `Owner` y `Pending`.

**Reglas de cobertura (implementables como test):**
1. Toda acción tiene `ID`, `Section` y `Description` no vacíos.
2. Toda acción tiene `Owner` no vacío **o** `Pending == true`.
3. Toda acción con `Owner` debe estar en el conjunto de acciones efectivamente despachadas por ese owner.
4. Toda acción despachada debe existir en el registry (no hay handler sin acción declarada).

**Cómo se materializa "despachada por ese owner":**
- App: la tabla `map[ActionID]handler` expone sus claves → conjunto de acciones manejadas por `app`.
- Componentes: cada uno expone `HandledActions() []ActionID` (derivado de su propio switch), agregado por el test en `internal/app` (que ya importa ambos mundos y evita ciclos).
- `Pending` (hoy solo `ask`) se excluye de la regla 3.

### Teclas crudas hoy fuera del registry: qué se promueve y qué no

**Se promueven a acciones declaradas** (aparecen en panel/modal, son configurables y hoy figuran en `KEYBINDS.md`):
- Navegación/paginado de grid (`j/k/h/l`, `g/G`, `ctrl+u/d`, `n/p`, `P/N`, `F1-F9`), edición/borrado/insert/yank/select/sort/filter/find, commit/discard/undo, FK nav/go back, focus preview, refresh, export.
- Explorer: expand/collapse/toggle_columns/first/last/filter/new/drop/view_ddl/refresh y tabs `1-6`.
- Grid preview: cursor up/down, first/last, half up/down, expand, toggle_explorer, jq_filter.
- Editor: execute/clear/copy/autocomplete/history prev/next.
- Nivel app: quit/help/palette/cycle_focus/focus_editor/ask/query_browser/rollback/switch_connection.

**NO se promueven** (quedan locales al widget y se documentan como prosa estática en el modal/FEATURES.md):
- Teclas de **modo** de widgets: EDIT mode del grid (flechas, `backspace`, `delete`, `home/end`, `ctrl+a/e`, `tab`), FILTER y WHERE FILTER (tipeo + `esc`/`enter`), scroll del modal de ayuda (`j/k/g/G/ctrl+u/d`), navegación interna del query browser y tipeo del picker.
- **Justificación**: son interacción *dentro* de un widget enfocado, no acciones de app configurables; promoverlas explotaría el registry con teclas no personalizables y acoplaría modos de edición al modelo. El modal las cubre como secciones estáticas (requirement 5) sin ser fuente de dispatch.
- Nota: `esc`/`tab` de navegación entre paneles (no de modo) sí son acciones de app (back/focus) y se declaran.

---

## 4. Migración por pasos atómicos

Cada paso deja `go build ./...` verde. El detalle de símbolos/líneas vive en `context.md`.

1. **Introducir el registry nuevo sin cambiar comportamiento** (`internal/config`): agregar `Action` y el slice plano, con `ActionsFor`/`Resolve`/`PrimaryKey`/`All`. Mantener `DefaultKeybindings()`/`defaultBindings()` como *wrappers derivados* temporalmente.
2. **Unificar y borrar la duplicación** (`internal/config`): eliminar `DefaultKeybindings()`/`defaultBindings()`; migrar `inContext` fuera de uso. Ajustar `keybindings_test.go` al nuevo API.
3. **`KeybindsPane` (rename)** (`internal/ui`): renombrar `StatusBar`→`KeybindsPane`, archivo `statusbar.go`→`keybindspane.go`; reemplazar `renderActions`/`renderContextual` por derivación de `ActionsFor(context)`. Renombrar/ajustar `statusbar_test.go`→`keybindspane_test.go`.
4. **Modal `?` derivado** (`internal/ui/modal.go`): secciones desde `All()`/`ActionsFor`; agregar secciones estáticas de modos EDIT/FILTER/WHERE, mouse, picker y query browser. Ajustar `modal_test.go`.
5. **Palette derivado** (`internal/ui/components/palette`): `DefaultCommands`/`BuildCommands` se derivan de la metadata del registry (nombre/alias/tecla). Ajustar `commands_test.go`.
6. **Contratos de consumidores** (`internal/app`, `internal/ui/components/*`): reemplazar `map[string]string` por el resolver en `grid`, `gridpreview`, `explorerpreview` (+ tabbars), `explorer`, `palette`, `modal`, `KeybindsPane`, `router`.
7. **Dispatch de componentes → actionID** (`explorer`, `gridpreview`, `explorerpreview`, `grid`): reemplazar comparaciones de teclas crudas por `Resolve` + switch sobre actionID.
8. **Dispatch de app → tabla** (`internal/app/app.go` + `router.go`): tabla `map[ActionID]handler`; eliminar comparaciones `m.keybinds[...]` y unificar `handlePaletteCommand`. Marcar `ask` como `Pending`.
9. **Tests de validación** (`internal/config` + `internal/app`): sección/descripción, colisiones por vista, cobertura acción↔handler.
10. **Docs**: borrar `docs/KEYBINDS.md`; limpiar tablas de keybinds del `README.md` (dejar puntero "press `?`"); crear `docs/FEATURES.md` (DML Transactions, Action Naming Convention, y prosa no-keybind); actualizar el checklist de keybinds en `AGENTS.md`.
11. **Verificación**: `go build ./... && go vet ./... && go test ./...`; luego el executor corre `make install` (regla de `AGENTS.md`: el deploy lo hace el executor, no se buildea "de más" durante el refactor salvo para verificar).

---

## 5. Estrategia de tests

- **(a) Sección + descripción**: iterar `All()` y fallar si `Section`/`Description` vacíos. Barato y detecta acciones nuevas sin metadata.
- **(b) Colisiones por vista**: agrupar por `(context, key)`; fallar si dos acciones distintas comparten tecla en la misma vista. Ignorar `Pending` solo si se decide (por defecto sí se validan sus teclas).
- **(c) Cobertura acción↔handler**: unir `handledActions` de app (claves de la tabla) + `HandledActions()` de cada componente; fallar si una acción no-`Pending` no está cubierta, o si un handler no corresponde a una acción declarada. `ask` queda exenta por `Pending`.
- **Ajustes de existentes**: `keybindings_test.go`, `statusbar_test.go` (→`keybindspane_test.go`), `modal_test.go`, `palette/commands_test.go`, `dml_rollback_helpers_test.go` (construcción del modelo con el nuevo registry).
- **Golden/regresión**: mantener los tests actuales de comportamiento de grid/explorer para asegurar que la migración de dispatch no cambia semántica; agregar un test que garantice que la tecla mostrada en el panel coincide con la que `Resolve` devuelve para esa acción (display == dispatch).

---

## 6. Riesgos y rollback

- **Blast radius en constructores** (muchos consumidores de `map[string]string`): mitigar introduciendo una **interfaz resolver** mínima y migrando consumidor por consumidor con build verde entre pasos.
- **Regresión semántica en dispatch de componentes** (grid tiene ~30 casos): mitigar con los tests existentes + golden de teclas; migrar en pasos separados por componente.
- **Explosión del registry** si se promueven teclas de modo: mitigado por la decisión explícita de sección 3.
- **Cobertura acción↔handler difícil de mantener** si los componentes no exponen `HandledActions()`: mitigado centralizando el test en `internal/app`.
- **Rollback**: es una rama sin migración de datos; revertir commits por paso es seguro. No hay pérdida de datos ni cambios de esquema.

## Orden de ejecución para el executor

`config` (1-2) → `ui` display (3-5) → contratos consumidores (6) → dispatch componentes (7) → dispatch app (8) → tests de validación (9) → docs (10) → verificación (11). El ADR `keybind-registry-single-source` se redacta tras el paso 2 (decisión ya validada por el código).
