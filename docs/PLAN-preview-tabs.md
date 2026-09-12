# Plan: Row Preview (JSON) + Tab Bar

## Decisiones

| Aspecto | Decisión |
|---------|----------|
| Preview | Read-only, JSON comprimido, un campo por línea |
| Tabs 2-5 | Solo visualización |
| Sin tabla seleccionada | Preview y tabs ocultos |
| Teclas tabs | `1-5`, configurables via keybindings |
| Teclas goto page | `F1-F9` (reemplaza `1-9` actual) |
| Conflictos | Resueltos: tabs y goto page en keysets distintos |

## Layout

```
┌─ Explorer (25%) ──┬─ Grid Area (50%) ──────────────┬─ Preview (25%) ──┐
│ > mydb            │ [1]Records [2]Columns [3]Cons..│ {                │
│   > public        │ WHERE Enter a WHERE clause...  │   "id": 25,     │
│     > users       │ ┌────┬─────────┬──────┬─────┐  │   "name": "CAD  │
│       id          │ │ id │ name    │ code │ ... │  │    PARAL·LEL",  │
│       name        │ ├────┼─────────┼──────┼─────┤  │   "adreca":     │
│       email       │ │ 25 │ CAD...  │ 01   │ ... │  │    "AVDA..."    │
│     > orders      │ │ 26 │ SERVEI. │ 02   │ ... │  │ }               │
│                   │ └────┴─────────┴──────┴─────┘  │                 │
│                   │ 1-78 of 78 rows                 │                 │
└───────────────────┴─────────────────────────────────┴─────────────────┘
```

---

## Fase 1: TabBar Component

**Crear:** `internal/ui/components/grid/tabbar.go`

```go
type Tab struct {
    ID    string // "records", "columns", "constraints", "foreign_keys", "indexes"
    Label string // "Records", "Columns", ...
    Key   string // "1", "2", ... (from keybindings)
}

type TabBar struct {
    tabs      []Tab
    active    int
    styles    *theme.Styles
    width     int
}
```

Renderizado:
```
 Records [1] │ Columns [2] │ Constraints [3] │ Foreign Keys [4] │ Indexes [5]
```

- Tab activa: fondo `Primary`, texto blanco, bold
- Tab inactiva: texto `TextMuted`, sin fondo
- Separador `│` entre tabs
- Methods: `SetActive(i)`, `NextTab()`, `PrevTab()`, `ActiveID() string`
- Las tabs se construyen pasando los keybindings correspondientes

---

## Fase 2: Integrar TabBar en Grid

**Modificar:** `internal/ui/components/grid/table.go`

Nuevos campos en struct `Grid`:
```go
tabBar         *TabBar
activeTab      int // 0=Records, 1=Columns, 2=Constraints, 3=FK, 4=Indexes
constraintsData []ConstraintInfo
foreignKeysData []ForeignKeyInfo
indexesData     []IndexInfo
```

Cambios en `View()`:
- Tab bar se renderiza arriba de todo (si hay datos cargados)
- `activeTab == 0` → vista Records actual (WHERE filter, header, rows, pager)
- `activeTab == 1` → vista Columns: tabla con Name, Type, Nullable, Default
- `activeTab == 2` → vista Constraints: tabla con Name, Type, Columns
- `activeTab == 3` → vista Foreign Keys: tabla con Name, Column, Ref Table, Ref Column
- `activeTab == 4` → vista Indexes: tabla con Name, Columns, Unique

Nuevos métodos de renderizado:
- `renderTabBar() string`
- `renderColumnsView() string`
- `renderConstraintsView() string`
- `renderForeignKeysView() string`
- `renderIndexesView() string`

Nuevos mensajes:
```go
type GridTabChangeMsg struct {
    Tab    int
    Schema string
    Table  string
}
```

En `handleKey()`, capturar `1-5` (via keybinds) para cambiar tab:
```go
if key == g.keybinds["grid.tab_records"] {
    g.setActiveTab(0)
    return func() tea.Msg { return GridTabChangeMsg{...} }, true
}
```

Las teclas `1-9` actuales para `goto_page` se reemplazan por `F1-F9`.

---

## Fase 3: Queries de Metadata

**Modificar:** `internal/drivers/postgres/schema.go`

Nuevos structs:
```go
type ConstraintInfo struct {
    Name    string
    Type    string // PRIMARY KEY, UNIQUE, CHECK, FOREIGN KEY
    Columns string // "col1, col2"
}

type ForeignKeyInfo struct {
    Name      string
    Column    string
    RefTable  string
    RefColumn string
}

type IndexInfo struct {
    Name    string
    Columns string
    Unique  bool
    Def     string
}
```

Nuevos métodos:
```go
func (s *SchemaLoader) ListConstraints(ctx, schema, table string) ([]ConstraintInfo, error)
func (s *SchemaLoader) ListForeignKeys(ctx, schema, table string) ([]ForeignKeyInfo, error)
func (s *SchemaLoader) ListIndexes(ctx, schema, table string) ([]IndexInfo, error)
```

Queries:

**Constraints:**
```sql
SELECT tc.constraint_name, tc.constraint_type,
  string_agg(DISTINCT kcu.column_name, ', ' ORDER BY kcu.ordinal_position)
FROM information_schema.table_constraints tc
JOIN information_schema.key_column_usage kcu
  ON tc.constraint_name = kcu.constraint_name
  AND tc.table_schema = kcu.table_schema
WHERE tc.table_schema = $1 AND tc.table_name = $2
GROUP BY tc.constraint_name, tc.constraint_type
ORDER BY tc.constraint_name
```

**Foreign Keys:**
```sql
SELECT
  tc.constraint_name,
  kcu.column_name,
  ccu.table_name AS ref_table,
  ccu.column_name AS ref_column
FROM information_schema.table_constraints tc
JOIN information_schema.key_column_usage kcu
  ON tc.constraint_name = kcu.constraint_name
  AND tc.table_schema = kcu.table_schema
JOIN information_schema.constraint_column_usage ccu
  ON tc.constraint_name = ccu.constraint_name
  AND tc.table_schema = ccu.table_schema
WHERE tc.constraint_type = 'FOREIGN KEY'
  AND tc.table_schema = $1 AND tc.table_name = $2
ORDER BY tc.constraint_name, kcu.ordinal_position
```

**Indexes:**
```sql
SELECT indexname, indexdef
FROM pg_indexes
WHERE schemaname = $1 AND tablename = $2
ORDER BY indexname
```

---

## Fase 4: Preview Panel (JSON)

**Crear:** `internal/ui/components/preview/preview.go`

```go
type Preview struct {
    styles   *theme.Styles
    lines    []string  // líneas renderizadas del JSON
    width    int
    height   int
    scrollY  int
}
```

Formato de renderizado — JSON comprimido, un campo por línea:
```json
{
  "id": 25,
  "territori_id": 1,
  "ambit_id": 1,
  "codi": "01",
  "descripcio": "CAD PARAL·LEL",
  "adreca": "AVDA. PARAL·LEL, 145.",
  "telefon": "934252244",
  "resinfantil": 0,
  "creaciodata": "2022-10-28T08:30:00Z",
  "eliminaciodata": null
}
```

**Render()** con syntax highlighting:
- Keys (`"id"`, `"name"`): estilo `Primary`
- Strings (`"CAD PARAL·LEL"`): estilo `Success`
- Numbers (`25`, `1`): estilo `Info`
- Null: estilo `TextMuted`
- `{`, `}`, `,`: estilo normal
- Bordes del panel: `NormalBorder()` consistente con theme

**Scroll:** si el JSON tiene más líneas que el alto del panel, scroll con mouse wheel.

---

## Fase 5: Layout en app.go

**Modificar:** `internal/app/app.go`

```go
func (m Model) renderMainView() string {
    // status line...

    hasTableData := m.grid.HasData()
    showPreview := hasTableData && m.grid.ActiveTab() == 0

    explorerW := m.width / 4
    previewW := 0
    if showPreview {
        previewW = m.width / 4
    }
    gridW := m.width - explorerW - previewW
    contentHeight := m.height - 4

    var panes []string
    panes = append(panes, m.renderExplorer(explorerW, contentHeight))
    panes = append(panes, m.renderGrid(gridW, contentHeight))
    if showPreview {
        panes = append(panes, m.renderPreview(previewW, contentHeight))
    }

    // ... resto igual ...
}
```

**Cursor tracking:** Cuando el grid mueve el cursor, necesita notificar al app para actualizar el preview.

Nuevo mensaje:
```go
type GridCursorMovedMsg struct{}
```

Se envía desde `moveUp()`, `moveDown()`, `handleClick()`. El app captura y actualiza:
```go
case grid.GridCursorMovedMsg:
    if row := m.grid.SelectedRow(); row != nil {
        m.preview.SetRow(m.grid.Columns(), row)
    }
```

---

## Fase 6: Keybindings

**Modificar:** `internal/config/keybindings.go`

Defaults nuevos:
```go
"grid.tab_records":      {"1"},
"grid.tab_columns":      {"2"},
"grid.tab_constraints":  {"3"},
"grid.tab_foreign_keys": {"4"},
"grid.tab_indexes":      {"5"},
```

Defaults modificados (goto page):
```go
// ELIMINAR: "grid.goto_page": {"0", "1", "2", ... "9"}
// AGREGAR:
"grid.goto_page_1": {"f1"},
"grid.goto_page_2": {"f2"},
// ... hasta f9
```

---

## Fase 7: Status Bar

**Modificar:** `internal/ui/statusbar.go`

Cambiar hint de `grid`:
```
ANTES: / filter · n/p N/P page · 0-9 goto page · s sort · f find column
AHORA: 1-5 tabs · / filter · n/p N/P page · F1-9 goto · s sort · f find column
```

---

## Resumen de Archivos

| Archivo | Acción | Líneas approx |
|---------|--------|---------------|
| `internal/ui/components/grid/tabbar.go` | **CREAR** | ~80 |
| `internal/ui/components/preview/preview.go` | **CREAR** | ~120 |
| `internal/ui/components/grid/table.go` | MODIFICAR | +80 |
| `internal/drivers/postgres/schema.go` | MODIFICAR | +80 |
| `internal/app/app.go` | MODIFICAR | +40 |
| `internal/app/messages.go` | MODIFICAR | +10 |
| `internal/config/keybindings.go` | MODIFICAR | +15 |
| `internal/ui/statusbar.go` | MODIFICAR | +5 |
| `internal/theme/styles.go` | MODIFICAR | +15 |

**Total:** 2 archivos nuevos (~200 líneas), 7 archivos modificados (~245 líneas)

---

## Orden de Implementación

1. `tabbar.go` → componente aislado
2. `theme/styles.go` → estilos de tab
3. `keybindings.go` → defaults de tabs y F1-F9
4. `table.go` → integrar tabbar, cambiar handleKey, renderizar vistas 1-5
5. `schema.go` → queries de metadata
6. `preview.go` → componente JSON
7. `messages.go` → nuevos mensajes
8. `app.go` → layout 3 paneles, cursor tracking, cargar metadata
9. `statusbar.go` → hints
