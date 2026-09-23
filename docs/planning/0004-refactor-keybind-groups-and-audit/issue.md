# Issue — Keybinds: agrupar en el panel + cerrar bypasses del registry

- **Slug**: `keybind-groups-and-audit`
- **Tipo**: refactor
- **Branch**: `refactor/keybind-registry-hardening` (base `main` @ `af111b8`)
- **Nivel**: PIPELINE (refactor multi-módulo; toca registry, display y dispatch)
- **Precedente**: `docs/planning/0001-refactor-keybinds-single-source/` + `docs/adr/0001-keybind-registry-single-source.md` (Accepted). Este plan **continúa** ese invariante, no lo reabre.

## Contexto / problema

El registry (`internal/config/keybindings_actions.go` → `defaultActions()`) ya es
la única fuente de verdad: display (panel, modal `?`, palette) y dispatch
(`Resolve` + `appActions()` + `HandledActions()`) derivan de la misma entrada.
Quedan dos problemas.

### Problema 1 — El panel desperdicia espacio: un segmento por tecla

`KeybindsPane.renderLines()` emite un segmento `key + " " + Description` por
**acción**, usando solo `PrimaryKey`. En el grid eso produce una fila larga de
acciones hermanas que conceptualmente son una sola cosa:

```
j Navigate Down · k Navigate Up · h Column Left · l Column Right
g First · G Last · ctrl+u Half Page Up · ctrl+d Half Page Down
f1 Go to Page 1 · f2 Go to Page 2 · … · f9 Go to Page 9
```

El panel muestra **solo la tecla primaria** de cada acción: alias reales como
`next_page` (`n/]/ctrl+right`), `prev_page` (`p/[/ctrl+left`), `quit`
(`q/ctrl+c`) o `preview_back` (`tab/esc`) no aparecen ahí.

El usuario quiere colapsar acciones relacionadas en **un segmento** con las
**teclas primarias** combinadas, sin crear una lista de display paralela. Los
alias/secundarias se reservan al modal `?` (ver "Resultado esperado").

### Problema 2 — Bypasses del registry (auditoría A + C)

Una auditoría de `internal/` encontró manejo de teclas crudas que esquiva el
registry:

**A — duplicaciones (la tecla ya está en el registry y además se maneja cruda):**

- `internal/ui/components/editor/sql.go` (`handleKey` ~156-283): `ctrl+enter`,
  `ctrl+r`, `ctrl+y`, `ctrl+u`, `ctrl+p`, `ctrl+n`, `tab` están cubiertas
  (`execute_query`, `copy_sql`, `clear_editor`, `history_prev`, `history_next`,
  `autocomplete`) pero se manejan crudas. `ctrl+p`/`ctrl+n`/`tab` son bypasses
  **vivos** (el editor los resuelve sin llamar a `Resolve`); `ctrl+enter`/`ctrl+r`
  son **muertas** (la app intercepta antes).
- `internal/ui/components/explorer/tree.go` (`handleKey` ~152-178):
  `j/k/g/G/enter/backspace` duplican el dispatch del Explorer; solo alcanzables
  cuando `keybinds == nil`. Código muerto.
- `internal/ui/modal.go:53`: el modal `?` cierra con `?`/`q`/`esc` crudos, que el
  registry ya bindea (`help`/`quit`/`close_editor`). No hay contexto "modal".

**C — ambiguo/sintético (el usuario aprobó arreglar ambos):**

- `internal/app/app.go`: los handlers de **mouse-wheel** (~1688-1699) y
  **double-click** (~1734) inyectan teclas crudas (`j`/`k`/`enter`) a los
  componentes en vez de resolver `navigate_down`/`navigate_up`/`edit_cell`.
  Rompen en silencio si el usuario reasigna esas teclas.
- `internal/ui/components/grid/table.go` (~639-649): salto de página multi-dígito
  `0-9` crudo, mientras el registry usa `f1-f9` para `goto_page_N`. Display y
  dispatch divergen.

### Fuera de alcance (decisión explícita del usuario)

- **B — texto literal en modos de entrada** (~118 teclas crudas en EDIT/FILTER/
  WHERE del grid, cuerpo del editor, input JQ, palette, picker, ask, filtro del
  query-browser, estados loading/error). NO se toca.
- **Agrupar por `Section`.** El usuario lo descartó explícitamente.
- Cambiar la semántica de teclas existentes o el mecanismo de overrides
  (`KeybindingsConfig.Custom`).

## Resultado esperado

1. **Un campo `Group` en `Action`** (registry) que colapsa acciones hermanas en
   **un segmento** con sus teclas combinadas. La agrupación sigue derivando de la
   única entrada; **no** hay listas de display paralelas ni agrupación por
   `Section`.
2. Grupos explícitos: `hjkl`, `g/G`, `ctrl+u/ctrl+d`, `f1-f9`.
3. **Separación panel vs. modal** (decisión del usuario):
   - El **panel** muestra **solo la tecla primaria** de cada acción. Un grupo
     muestra las **primarias combinadas** de sus miembros (p.ej. `hjkl Navigate`,
     `g/G First/Last`, `ctrl+u/ctrl+d Half Page`, `f1-f9 Go to Page`). Las
     secundarias/alias **no** aparecen en el panel (p.ej. `next_page` rinde
     `n Next Page`, no `n/]/ctrl+right`).
   - El **modal `?`** es donde se muestran **todas las teclas** (alias incluidos)
     de cada acción.
4. Grupos **parciales**: si solo parte del grupo aplica en el contexto (p.ej.
   `navigate_left/right` son `ContextGrid`-only), el segmento muestra solo las
   teclas primarias activas de ese contexto.
5. El modal `?` **también agrupa**, derivado del mismo campo (recomendado).
6. El cap de `maxKeybindsPerLine` cuenta **grupos, no teclas**.
7. **A**: eliminar las comprobaciones crudas muertas y enrutar las vivas por el
   resolver, preservando el comportamiento local de entrada de texto (B).
8. **C**: mouse-wheel y double-click despachan por **action ID resuelto**;
   `0-9` del grid se alinea con `goto_page_N` display/dispatch.
9. Tests verdes + nuevos tests para agrupación (segmento único con primarias
   combinadas, grupos parciales, panel solo-primarias y modal con todos los alias)
   y para los fixes A/C. Se actualizan los tests existentes donde el formato del
   panel cambie intencionalmente.
10. El **invariante single-source** se preserva y se refuerza: ningún camino nuevo
    puede divergir display de dispatch.

## Preguntas de diseño a resolver en el plan

- Tipo/valor exacto de `Group` y de dónde sale el **label** del grupo (campo
  dedicado vs. derivar de la `Description` de la primera acción).
- Cómo se **combinan** las teclas primarias de un grupo (orden, separador).
- Confirmar que el **panel** usa solo `PrimaryKey` (los alias nunca aparecen) y
  que el **modal** usa `KeysFor` (todos los alias).
- Qué pasa con un grupo **parcialmente activo** en un contexto.
- **Orden de render**: los grupos deben quedar adyacentes; verificar que el orden
  del registry lo permita o si hay que reordenar `defaultActions()`.
- Interacción con el cap de 7 por línea y con los segmentos sintéticos/condicionales
  ya presentes (`tx pending`; `rollback` oculto salvo `txPending`; `autocomplete`
  oculto salvo `ready`).
- Si el modal `?` agrupa y con qué formato (recomendado: sí, mismo campo).
- Comportamiento de las teclas mostradas respecto a tests existentes
  (p.ej. `TestKeybindsPane_ExplorerShowsFilterTablesKey` espera `/ Filter Tables`).
- En A/C: ¿`0-9` se registra como teclas de `goto_page_N` (con reglas de
  colisión), se mapea crudo a las acciones, u otra vía?; ¿el cierre del modal se
  documenta como excepción de overlay o se introduce un contexto "modal"?
