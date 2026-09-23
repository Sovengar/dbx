# Context: ASK — panel de chat NL→SQL

Freshness: commit `9924555` (`main`), branch `feat/ask-nl2sql-chat`.
codegraph: not_initialized (proyecto sin grafo; usar este mapa).

Este archivo es la **única entrada de navegación** del executor. No hace falta
glob/grep: los símbolos y rutas están abajo.

## 1. Archivos a tocar

### Nuevo: `internal/ui/components/ask/ask.go` (+ `ask_test.go`)
Componente overlay nuevo. Patrón a copiar: `internal/ui/components/querybrowser/querybrowser.go`
(401 líneas) — `Show/Hide/IsVisible/SetWidth/SetHeight/Update/View`, mensajes
`QuerySelectedMsg`/`QueryBrowserClosedMsg`, y `qbDebugLog` a
`/tmp/dbx_qb_debug.log`. El nuevo componente debe exponer:
- `New(styles *theme.Styles) *Ask`, `Show()`, `Hide()`, `IsVisible()`,
  `SetWidth(int)`, `SetHeight(int)`, `Update(tea.Msg) (tea.Cmd, bool)`, `View() string`.
- Estado del transcript: lista de turnos `{Question, SQL, Status, Err}`; input
  actual; estado del panel (`typing|generating|review|executing|error`).
- `SetGeneratedSQL(sql string)`, `SetError(err)`, `SetResultSummary(...)`,
  `AddQuestion(q string)`, `SetContextHint(schema, table, where string)`.
- Mensajes emitidos: `AskSubmittedMsg{Question string}` (enter en `typing`),
  `AskConfirmMsg{SQL string}` (enter en `review`), `AskClosedMsg` (esc).
- Debug log a `/tmp/dbx_ask_debug.log` (crear `askDebugLog` análogo a `qbDebugLog`).

### `internal/app/app.go` (2607 líneas)
- **Model struct** (líneas ~57-103): agregar campos `ask *ask.Ask`,
  `askOpen bool`, `aiProvider nl2sql.Provider`, `askSchemaText string`.
- **`NewModel`** (línea ~107): construir `ask.New(t.Styles())`; resolver el
  proveedor con `nl2sql.Resolve(nl2sql.Config{Provider: cfg.AI.Provider, Model:
  cfg.AI.Model, Providers: <map>})` y guardarlo (nil si falla). Ver
  `internal/cli/ask.go` `toProviderConfigs` (líneas ~125-135) para el mapeo
  `config.AIProviderConf` → `nl2sql.ProviderConfig`.
- **Key handling `StateMain`** (líneas ~1647-1847): insertar el guard del overlay
  ASK junto a los de palette/querybrowser (arriba de la línea 1589-1601), y el
  handler de `m.keybinds["global.ask"]` en el bloque de teclas globales
  (cerca de `global.help`/`global.palette`, líneas ~1703-1711). Importante: debe
  quedar **después** de los guards de `grid.IsFiltering()` / `IsWhereFiltering()`
  / `IsEditing()` (líneas ~1675-1701) para no robar la tecla al editar/filtrar.
- **`executeQuery`** (línea ~891) y **handler `queryExecutedMsg`** (líneas
  ~1366-1395): modelo de cómo se ejecuta y cómo se puebla el grid
  (`m.grid.SetData(msg.result, "", "query")`, `m.router.FocusPane(FocusGrid)`,
  toast de filas). El flujo ASK debe reusar ese comportamiento en éxito.
- **`View`** (línea ~2259) y **`renderMainView`** (línea ~2311): agregar el
  render del overlay ASK con `overlay(content, askView, m.width, m.height)`
  (helper en línea ~2542), junto a palette/querybrowser (líneas ~2287-2303).
- **Guards de mouse/scroll** (líneas ~1488-1490 y ~1537-1538): agregar
  `m.askOpen` a las condiciones que ignoran clicks/wheel cuando hay overlay.
- **`syncTxStatus`** (línea ~1995) y `handleRollback` (línea ~1962): referencia
  para el chequeo de `m.runner.pending()`.

### `internal/app/txn.go` (statementRunner)
- `newStatementRunner(conn *pgx.Conn)` (línea ~53): agregar campo
  `beginReadOnly func(ctx) (pgx.Tx, error)` inicializado con
  `conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})`.
- Nuevo método `executeReadOnly(ctx, sql) (*postgres.QueryResult, error)`:
  si `r.pending()` → error ("pending DML transaction"); si no, abrir tx
  read-only, `postgres.ExecuteQuery(ctx, tx, sql)`, `tx.Rollback(ctx)`.
  Reusar el patrón de `execute` (línea ~73) y `querierFor` (línea ~103).

### `internal/ui/components/grid/table.go` (2231 líneas)
- `SetData` (línea ~176) guarda `g.schema` y `g.tableName`.
- Ya públicos: `TableName()` (~478), `WhereClause()` (~865), `HasData()` (~291).
- **Falta** accessor de schema: agregar `func (g *Grid) Schema() string { return g.schema }`
  o un combinado `func (g *Grid) ContextHint() (schema, table, where string)`.

### Descubribilidad
- `internal/ui/components/palette/commands.go` (63 líneas): agregar
  `{Name: "Ask AI", Alias: "ask", Action: "global.ask", Section: SectionQuery}`.
  El wiring del action va en `handlePaletteCommand` (`app.go` línea ~1853).
- `internal/ui/modal.go` (246 líneas): sección **Global** (líneas ~96-108),
  agregar `m.renderKeybind("global.ask", "Ask AI (NL→SQL)")`.
- `internal/ui/statusbar.go` (133 líneas): `renderActions` (línea ~75) agregar
  `s.keyFor("global.ask")+" ask"`.
- `README.md` y `docs/KEYBINDS.md`: ya documentan `global.ask`/`a` (README
  líneas 150-151, 222, 341; KEYBINDS líneas 45, 326). Verificar consistencia;
  no hay que inventar el keybind.

## 2. Contratos a respetar / extender

- `nl2sql.Provider` (`internal/ai/nl2sql/provider.go`): interface
  `Name() string` + `Generate(ctx, prompt, schema string) (string, error)`.
  Es lo que se inyecta como fake en tests.
- `nl2sql.Resolve(cfg Config) (Provider, error)` (mismo archivo) — puede fallar
  (sin API key / proveedor desconocido). Manejar nil.
- `nl2sql.BuildSystemPrompt(schema)` (`prompt.go`) ya fuerza SELECT-only,
  `schema.table` y `LIMIT 100`. No tocar.
- `postgres.Querier` (`internal/drivers/postgres/query.go` línea ~90): sólo
  `Query`. `*pgx.Conn` y `pgx.Tx` lo satisfacen. `postgres.ExecuteQuery(ctx, q, sql)`
  (línea ~94) es el ejecutor genérico.
- `postgres.QueryResult{Columns, Rows, Count}` — lo que consume `grid.SetData`.
- Mensajes/estado del app: `FocusPane` y `FocusChangedMsg` en
  `internal/app/messages.go`; `m.router.Focus()`/`Context()` en
  `internal/app/router.go`. `global.ask` YA existe en
  `internal/config/keybindings.go` (`DefaultKeybindings` + `defaultBindings`).

## 3. Patrón a seguir por concern

- **Overlay con input y transcript** → copiar `querybrowser` (Show/Update/View,
  `overlay(...)`, footer con ayudas, debug log).
- **Input de texto + enter/esc/backspace** → ver el manejo de `filterActive` en
  `querybrowser.handleKey` (líneas ~115-144) y `editor/sql.go` para el manejo de
  `tea.KeyPressMsg` (usar `msg.Text` para caracteres, `key == "space"`).
- **Ejecución async** → patrón `func() tea.Msg { ... }` en `executeQuery`
  (app.go ~891) y `loadTableDataWithSortAndWhere` (~667).
- **Prompt de schema** → `internal/cli/ask.go` `getSchemaForLLM` (líneas
  ~105-155) es la referencia del formato texto; en la TUI se arma desde
  `m.schemaDetail` (ya en memoria) sin round-trip. `buildSchemaExport(m.dbName,
  m.schemaDetail)` (app.go ~318) produce `*aiContext.SchemaExport` si se prefiere
  reusar el exportador.

## 4. Tests afectados + runner/infra

- Runner: `go test ./...` (Go stdlib `testing`). No hay Cucumber/teatest.
- Patrón de test de app: `internal/app/dml_rollback_helpers_test.go` —
  `newRollbackTestModel()` / `newModelWithGrid(t)` construyen el `Model` a mano;
  `runExecuteQuery` dispara el `tea.Cmd` y despacha el mensaje; `pressRollback`
  envía `tea.KeyPressMsg{Code: 'U'}` por el `Update` real; `lastToastContains`.
  Copiar ese estilo para `internal/app/ask_test.go`.
- Harness de integración real: en el mismo helpers, `testDSN(t)` salta salvo
  `DBX_TEST_DSN`; `connectTestDB`, `seedTxTestTable`, `readName`. Usar para
  verificar la transacción READ ONLY.
- Fake de proveedor: implementar un tipo que satisfaga `nl2sql.Provider` en el
  test y asignarlo a `m.aiProvider`.
- Tests existentes que pueden romperse por cambios de statusbar/help/palette:
  `internal/ui/statusbar_test.go`, `internal/ui/modal_test.go`,
  `internal/ui/components/palette/commands_test.go`,
  `internal/config/keybindings_test.go`. Actualizarlos si agregan filas.

## 5. Convenciones y límites de módulo

- `charm.land/bubbletea/v2`, `charm.land/lipgloss/v2`; `View()` devuelve
  `tea.View`; key events `tea.KeyPressMsg`; mouse `tea.MouseModeCellMotion`.
- **Nunca** borrar logging de debug; agregar (`askDebugLog` a
  `/tmp/dbx_ask_debug.log`, formato `"Ask: key=%q ..."`).
- Comentarios/identificadores en inglés; prosa en español.
- Checklist obligatorio de keybinds (AGENTS.md): código, statusbar
  (`renderActions` + `renderContextual`), help modal, palette, README,
  docs/KEYBINDS.md. Aquí el keybind ya existe; sólo se agrega la UI y las
  entradas faltantes.

## 6. Puntos de integración no obvios

- **Orden de guards de teclado**: si el handler de `a` se coloca antes de los
  guards de edición/filtrado del grid, rompe la escritura de `a` en celdas. Debe
  ir después (ver plan).
- **Conexión única**: pgx no permite dos transacciones simultáneas en la misma
  `*pgx.Conn`; por eso ASK rechaza cuando `m.runner.pending()`.
- **El proveedor no necesita DB**: se puede abrir/generar sin conexión; sólo la
  ejecución requiere `m.runner != nil`.
- **El grid cierra el editor en éxito** (`queryExecutedMsg` hace
  `m.editorOpen = false`): el cierre del overlay ASK en éxito es consistente.
- **`m.grid.SetData(result, "", "query")`**: schema/table vacíos para resultados
  de query (no es una tabla); conservar ese contrato.

## 7. Riesgos

- Validación de statement demasiado laxa (comentarios, `;` inicial, CTEs con
  DML). Reusar/endurecer `splitSQL` (app.go ~915) y `hasKeywordPrefix`
  (txn.go ~28) como referencia.
- `LIMIT` por defecto: resultados agregados vs filas; el grid ya maneja paginado.
- Tests que dependan del contenido exacto del statusbar/help pueden requerir
  ajuste al agregar la línea de ASK.
