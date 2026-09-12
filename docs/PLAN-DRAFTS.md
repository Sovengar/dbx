# Plan: Draft-based editing model

## Modelo conceptual

```
NORMAL ──[Enter/i]──► EDITING ──[Esc]──► NORMAL
                          │                   │
                          │  (cambios locales) │
                          │  sin SQL executed  │
                          ▼                   │
                     drafts acumulados ◄──────┘
                          │
                   [ctrl+s] ──► ejecuta todo → reload → NORMAL
                   [D]     ──► aviso → descarta todo → NORMAL
```

**Drafts:**
- **Updates**: celdas modificadas (almacenadas localmente, sin UPDATE SQL)
- **Inserts**: filas pending (ya existen en `pendingRows`)
- **Deletes**: filas marcadas para borrar (nuevo `pendingDeletes`)

**ctrl+s**: recoge todos los drafts, genera el SQL, ejecuta, recarga la tabla.

**D**: muestra aviso de confirmación, si se confirma descarta todos los drafts y restaura valores originales.

**Esc**: solo sale del modo edición, NO descarta drafts.

## Modelo visual

```
Fila insertada (pending):    fondo VERDE
Celda modificada (draft):    fondo AZUL  (solo la celda, no la fila)
Fila eliminada (draft):      fondo ROJO  (toda la fila)
```

---

## Cambio 1 — Nuevos tipos y campos en Grid

**Archivo:** `internal/ui/components/grid/table.go`

```go
type PendingUpdate struct {
    RowIdx   int           // índice de fila en g.data.Rows
    ColIdx   int           // índice de columna
    OldValue interface{}   // valor original (para revertir con D)
    NewValue interface{}   // nuevo valor (para SQL con ctrl+s)
}

type PendingDelete struct {
    RowIdx int             // índice de fila en g.data.Rows
    Row    []interface{}   // copia de la fila completa (para WHERE clause)
}
```

Campos nuevos en `Grid`:
```go
pendingUpdates []PendingUpdate
pendingDeletes []PendingDelete
discardPending bool             // awaiting second 'D' to confirm discard
```

Los campos `selectedRows` y `deletePending` se mantienen para la selección visual (multi-select con `space`), pero `d` ya no ejecuta DELETE directamente — almacena en `pendingDeletes`.

---

## Cambio 2 — `commitEdit()` solo almacena draft

**Archivo:** `internal/ui/components/grid/table.go`

`commitEdit()` para filas existentes:

```go
// ANTES: genera SQL + retorna CellEditCommitMsg
row := g.data.Rows[g.editRow]
newVal := g.parseEditValue(g.editValue, row[g.editCol])
g.data.Rows[g.editRow][g.editCol] = newVal
// ... genera UPDATE SQL ...
return func() tea.Msg { return CellEditCommitMsg{...} }

// DESPUÉS: solo almacena draft + actualiza localmente
row := g.data.Rows[g.editRow]
newVal := g.parseEditValue(g.editValue, row[g.editCol])

// Verificar si ya existe un update para esta celda
found := false
for i, u := range g.pendingUpdates {
    if u.RowIdx == g.editRow && u.ColIdx == g.editCol {
        g.pendingUpdates[i].NewValue = newVal
        found = true
        break
    }
}
if !found {
    g.pendingUpdates = append(g.pendingUpdates, PendingUpdate{
        RowIdx:   g.editRow,
        ColIdx:   g.editCol,
        OldValue: row[g.editCol],  // valor original para revertir
        NewValue: newVal,
    })
}
g.data.Rows[g.editRow][g.editCol] = newVal  // actualizar localmente
return nil  // sin SQL, sin cmd
```

Para pending rows (inserting): se mantiene igual — solo actualiza `pendingRows`, sin SQL.

---

## Cambio 3 — Deletes como drafts

**Archivo:** `internal/ui/components/grid/table.go`

`startDelete()` en vez de generar `GridDeleteRowMsg`/`GridBulkDeleteMsg`:

```go
func (g *Grid) startDelete() (tea.Cmd, bool) {
    if g.data == nil || len(g.columns) == 0 {
        return nil, false
    }

    if len(g.selectedRows) > 0 {
        if g.deletePending {
            // Confirmar: almacenar todas las filas seleccionadas en pendingDeletes
            g.deletePending = false
            for i := range g.selectedRows {
                if i >= 0 && i < len(g.data.Rows) {
                    rowCopy := make([]interface{}, len(g.data.Rows[i]))
                    copy(rowCopy, g.data.Rows[i])
                    g.pendingDeletes = append(g.pendingDeletes, PendingDelete{
                        RowIdx: i,
                        Row:    rowCopy,
                    })
                }
            }
            g.selectedRows = make(map[int]bool)
            return nil, true
        }
        g.deletePending = true
        return nil, true
    }

    // Fila individual
    g.deletePending = false
    row := g.SelectedRow()
    if row == nil {
        return nil, false
    }
    rowCopy := make([]interface{}, len(row))
    copy(rowCopy, row)
    g.pendingDeletes = append(g.pendingDeletes, PendingDelete{
        RowIdx: g.cursorRow + g.pager.Offset(),
        Row:    rowCopy,
    })
    return nil, true
}
```

---

## Cambio 4 — `ctrl+s`: commit all drafts

**Archivo:** `internal/ui/components/grid/table.go`

Nuevo tipo de mensaje y función:

```go
type GridCommitAllMsg struct {
    Schema  string
    Table   string
    Queries []string
    Args    [][]interface{}
}

func (g *Grid) CommitAllDrafts() tea.Cmd {
    var queries []string
    var allArgs [][]interface{}

    // 1. INSERTS (de pendingRows)
    for _, row := range g.pendingRows {
        // ... generar INSERT INTO ...
    }

    // 2. UPDATES (de pendingUpdates)
    for _, update := range g.pendingUpdates {
        // ... generar UPDATE ... SET col = $N WHERE pk = $M ...
        // usando g.data.Rows[update.RowIdx] para obtener la fila actual
    }

    // 3. DELETES (de pendingDeletes)
    for _, del := range g.pendingDeletes {
        // ... generar DELETE FROM ... WHERE pk = $N ...
        // usando del.Row (copia original) para el WHERE clause
    }

    if len(queries) == 0 {
        return nil
    }

    // Limpiar drafts
    g.pendingRows = nil
    g.pendingUpdates = nil
    g.pendingDeletes = nil
    g.inserting = false
    g.pager.SetPendingCount(0)

    schema := g.schema
    table := g.tableName
    return func() tea.Msg {
        return GridCommitAllMsg{Schema: schema, Table: table, Queries: queries, Args: allArgs}
    }
}
```

---

## Cambio 5 — `D` key: descarta todos los drafts (con confirmación)

**Archivo:** `internal/ui/components/grid/table.go`

El flujo de `D` es igual al de `d` para deletes: la primera vez muestra aviso, la segunda confirma.

```go
func (g *Grid) handleDiscardKey() (tea.Cmd, bool) {
    // Solo hay drafts que descartar?
    if len(g.pendingUpdates) == 0 && len(g.pendingDeletes) == 0 && len(g.pendingRows) == 0 {
        return nil, false
    }

    if g.discardPending {
        // Segunda vez: confirmar descarte
        g.discardPending = false
        g.DiscardAllDrafts()
        return nil, true
    }

    // Primera vez: mostrar aviso
    g.discardPending = true
    return nil, true
}

func (g *Grid) DiscardAllDrafts() {
    // Revertir updates: restaurar valores originales
    for _, update := range g.pendingUpdates {
        if update.RowIdx >= 0 && update.RowIdx < len(g.data.Rows) {
            g.data.Rows[update.RowIdx][update.ColIdx] = update.OldValue
        }
    }

    // Limpiar todo
    g.pendingUpdates = nil
    g.pendingDeletes = nil
    g.pendingRows = nil
    g.inserting = false
    g.deletePending = false
    g.discardPending = false
    g.selectedRows = make(map[int]bool)
    g.pager.SetPendingCount(0)
}

func (g *Grid) IsDiscardPending() bool {
    return g.discardPending
}
```

El aviso se muestra en el status bar, similar al de delete pending:

```
Press D again to discard N change(s)
```

---

## Cambio 6 — Estilos de colores para drafts

**Archivo:** `internal/theme/styles.go`

Nuevos estilos en `Styles`:
```go
DraftInsert  lipgloss.Style  // verde para filas pending
DraftUpdate  lipgloss.Style  // azul para celdas modificadas
DraftDelete  lipgloss.Style  // rojo para filas eliminadas
```

Inicialización en `NewStyles`:
```go
DraftInsert: lipgloss.NewStyle().
    Background(lipgloss.Color("#2d5a27")).  // verde oscuro
    Foreground(t.Foreground).
    Padding(0, 1),

DraftUpdate: lipgloss.NewStyle().
    Background(lipgloss.Color("#1a3a5c")).  // azul oscuro
    Foreground(t.Foreground).
    Padding(0, 1),

DraftDelete: lipgloss.NewStyle().
    Background(lipgloss.Color("#5c1a1a")).  // rojo oscuro
    Foreground(t.Foreground).
    Padding(0, 1),
```

---

## Cambio 7 — Renderizado con colores de draft

**Archivo:** `internal/ui/components/grid/cell.go`

Nuevos métodos en `CellRenderer`:
```go
func (cr *CellRenderer) RenderDraftUpdateCell(value interface{}, width int) string {
    raw := cr.FormatValue(value)
    truncated := cr.Truncate(raw, width-2)
    return cr.styles.DraftUpdate.
        PaddingLeft(1).PaddingRight(1).Width(width).
        Render(truncated)
}

func (cr *CellRenderer) RenderDraftDeleteRow(values []interface{}, widths []int) string {
    var cells []string
    for i, val := range values {
        raw := cr.FormatValue(val)
        truncated := cr.Truncate(raw, widths[i]-2)
        cell := cr.styles.DraftDelete.
            PaddingLeft(1).PaddingRight(1).Width(widths[i]).
            Render(truncated)
        cells = append(cells, cell)
    }
    return strings.Join(cells, "")
}
```

**Archivo:** `internal/ui/components/grid/table.go`

En el loop de renderizado de filas, agregar lógica de draft:

```go
// Verificar drafts
isDraftDelete := g.isRowDeleted(i)
isDraftUpdate := make([]bool, len(visCols))
if !isDraftDelete {
    for j, colIdx := range visCols {
        isDraftUpdate[j] = g.isCellModified(i, colIdx)
    }
}

// Renderizado:
if isDraftDelete {
    rendered = g.cells.RenderDraftDeleteRow(visValues, visWidths)
} else if isEditing {
    // ... lógica actual de edición ...
} else if isSelected {
    // ... lógica actual con colores de draft para celdas modificadas ...
}
```

Funciones helper:
```go
func (g *Grid) isRowDeleted(rowIdx int) bool {
    for _, d := range g.pendingDeletes {
        if d.RowIdx == rowIdx {
            return true
        }
    }
    return false
}

func (g *Grid) isCellModified(rowIdx, colIdx int) bool {
    for _, u := range g.pendingUpdates {
        if u.RowIdx == rowIdx && u.ColIdx == colIdx {
            return true
        }
    }
    return false
}
```

Para filas pending (inserts), cambiar `RenderPendingRow` para usar `DraftInsert` en vez de `Pending`.

---

## Cambio 8 — Keybindings

**Archivo:** `internal/config/keybindings.go`

```python
# Mantener ctrl+s para commit
"grid.commit_pending": "ctrl+s",

# Nuevo: D para descartar (con confirmación)
"grid.discard_all": "D",
```

**Archivo:** `internal/app/app.go`

- Eliminar handler `case grid.CellEditCommitMsg:`
- Eliminar handler `case grid.GridDeleteRowMsg:`
- Eliminar handler `case grid.GridBulkDeleteMsg:`
- Agregar handler `case grid.GridCommitAllMsg:` (ejecuta queries + reload)
- Agregar handler tecla `D` en modo normal: `g.grid.handleDiscardKey()` → si `discardPending` true, mostrar aviso en status bar
- Agregar aviso de discard pending en el status bar (línea similar a deletePending)

---

## Cambio 9 — Limpiar drafts en `SetData`

**Archivo:** `internal/ui/components/grid/table.go`

En `SetData()`, limpiar también:
```go
g.pendingUpdates = nil
g.pendingDeletes = nil
g.discardPending = false
```

---

## Resumen de archivos

| Archivo | Cambios |
|---------|---------|
| `internal/ui/components/grid/table.go` | Tipos `PendingUpdate`/`PendingDelete`, campos en `Grid`, `commitEdit()` draft, `startDelete()` draft, `CommitAllDrafts()`, `DiscardAllDrafts()`, `handleDiscardKey()`, helpers `isRowDeleted`/`isCellModified`, limpiar en `SetData` |
| `internal/ui/components/grid/cell.go` | `RenderDraftUpdateCell`, `RenderDraftDeleteRow`, modificar `RenderPendingRow` para usar `DraftInsert` |
| `internal/app/app.go` | Handler `GridCommitAllMsg`, handler tecla `D`, eliminar handlers obsoletos |
| `internal/config/keybindings.go` | Agregar `grid.discard_all: D` |
| `internal/theme/styles.go` | `DraftInsert`, `DraftUpdate`, `DraftDelete` |

## Lo que NO cambia

- `startEdit()` — sigue igual
- `startInsertRow()` — sigue igual
- Navegación Enter/Tab — sigue igual
- `Esc` — solo sale de edición, NO descarta drafts
- `selectedRows` / `deletePending` — se mantienen para multi-select visual
- `ctrl+s` — sigue siendo el keybinding para commit (ahora commitea todo, no solo inserts)

## Verificación

```bash
make build && make install
```

Checks en la TUI:
1. **Enter** en celda → EDIT, modifica → celda en **AZUL** ✓
2. **Tab** navega, sigue en EDIT, celdas modificadas en AZUL ✓
3. **`i`** → fila pending en **VERDE** ✓
4. **`d`** → fila marcada en **ROJO** ✓
5. **ctrl+s** → ejecuta todo → toast → reload ✓
6. **`D`** → muestra aviso → segunda `D` descarta todo → valores restaurados ✓
7. **`Esc`** → sale de EDIT, drafts se mantienen con colores ✓
