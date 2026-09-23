# Summary: ask-nl2sql-chat

## Metadata
- **Completed:** 2026-09-23 20:41
- **Duration:** ~105 minutes (18:57 → 20:41)
- **Plan Number:** 0003
- **Branch:** `feat/ask-nl2sql-chat` (base `main` @ `9924555`)
- **ADR:** `docs/decisions/0001-ask-nl2sql-generic-execution.md` (Accepted)
- **Commits:** 29 ahead of `main` (planning → feature → review fixes → testcontainers → CI)

## Objetivo

Cumplir la promesa ya documentada de `a` ("Ask (NL→SQL)") en la TUI: que un
usuario que conoce los datos pero no SQL abra un panel con `a`, escriba su
pregunta en lenguaje natural, revise el `SELECT` generado y lo ejecute
obteniendo el resultado en el grid — sin tocar la base de forma insegura.

## Resultado

- **Panel overlay ASK** (`internal/ui/components/ask/`), abierto con `a`,
  moldeado sobre `querybrowser`: input de la pregunta + transcript de turnos
  (pregunta → SQL → estado). `enter` en `typing` envía; el SQL generado se
  muestra para **revisión**; `enter` confirma y ejecuta; `esc` cancela sin
  ejecutar. El transcript persiste mientras el modelo viva.
- **Generación NL→SQL**: el proveedor (`nl2sql.Provider`) se resuelve una vez en
  `NewModel` (`nl2sql.Resolve`) como campo **inyectable** (fake en tests), y la
  generación corre en un `tea.Cmd` async. El prompt se arma con el schema **en
  memoria** (`m.schemaDetail`) — sin round-trip a la DB.
- **Pista de contexto**: si el grid tiene una tabla cargada, se agrega
  `schema.table` + el `WHERE` actual al prompt como *pista*, nunca como
  restricción (ver ADR).
- **Ejecución READ ONLY + validación**: dos capas de defensa (ver abajo).
- **Resultado al grid**: en éxito `grid.SetData(result, "", "query")` + foco al
  grid + toast con el conteo; el panel se cierra (consistente con el editor). En
  error el panel queda abierto con el mensaje en el transcript.
- **Descubribilidad**: comando en palette, entrada en help modal `?`, acción en
  el statusbar; README y docs actualizados.
- **Historial**: las queries ejecutadas vía ASK se registran en el historial.

## Modelo de seguridad (defensa en profundidad)

1. **Validación de código** (`selectOnlyViolation`, `internal/app/txn.go`):
   acepta un único `SELECT` o `WITH … SELECT`; tolera comentarios iniciales, `;`
   sueltos, dollar-quoting y escapes `E''`; rechaza CTEs que mutan y keywords
   `INTO` / `FOR` (row locks) / `nextval` con una razón legible. Es **feedback**,
   no la garantía.
2. **Transacción READ ONLY** (`statementRunner.executeReadOnly`, `AccessMode:
   pgx.ReadOnly`), siempre con `Rollback` — nada se commitea nunca. PostgreSQL
   rechaza cualquier mutación que sortee la validación. **Esta es la garantía
   autoritativa.**
3. **Transacción DML pendiente → ASK rehúsa ejecutar** ("commit o rollback
   primero"): pgx no permite una segunda transacción sobre la misma conexión, y
   commitear drafts del usuario en silencio sería pérdida de datos.
4. **Estado busy concurrency-safe**: el flag read-only usa `atomic.Bool` y el
   pending tx está guardado con mutex (se escriben desde goroutines de `tea.Cmd`,
   se leen desde `Update`); mientras una query read-only está en vuelo se
   rehúsan commit/rollback directos y los loaders de schema/preview/tabla.

## Escenarios (behavior.feature) — 19/19

| Scenario | Status | Test(s) |
|----------|--------|---------|
| `a` opens the ASK pane | ✅ Passed | `TestAsk_KeyOpensPane` |
| ASK is globally available regardless of focus | ✅ Passed | `TestAsk_OpensRegardlessOfFocus`, `_OpensWithGridFocus`, `_OpensWithGridPreviewFocus` |
| `a` does not open while editing or filtering | ✅ Passed | `TestAsk_DoesNotOpenWhileGridEditing/Filtering/WhereFiltering/EditorOpen` |
| `esc` closes without executing | ✅ Passed | `TestAsk_EscClosesWithoutExecuting` |
| Asking generates SQL and shows it for review | ✅ Passed | `TestAsk_QuestionGeneratesSQLForReview` |
| Confirming executes and populates the grid | ✅ Passed | `TestAsk_ConfirmExecutesAndPopulatesGrid` |
| Transcript keeps question and executed SQL | ✅ Passed | `TestAsk_TranscriptKeepsExecutedQuery` |
| Table + WHERE passed as context hint | ✅ Passed | `TestAsk_PassesTableAndWhereAsContextHint` |
| ASK works with no table loaded | ✅ Passed | `TestAsk_NoTableLoadedUsesFullSchema` |
| Non-SELECT rejected before execution | ✅ Passed | `TestAsk_NonSelectRejected` |
| Server rejects DML even if validation bypassed | ✅ Passed | `TestAsk_ReadOnlyTxRejectsDML`, `_ReadOnlyTxRejectsSelectBasedMutation` (testcontainers) |
| AI provider failure | ✅ Passed | `TestAsk_ProviderError` |
| Invalid SQL from the AI | ✅ Passed | `TestAsk_InvalidSQL` |
| No database connection | ✅ Passed | `TestAsk_NoConnection` |
| No AI provider configured | ✅ Passed | `TestAsk_NoProvider`, `_ProviderResolutionErrorSurfaced` |
| Pending DML transaction blocks ASK execution | ✅ Passed | `TestAsk_PendingTxBlocksExecution` |
| ASK command in the palette | ✅ Passed | `palette/commands_test.go` |
| ASK action in the help modal | ✅ Passed | `modal_test.go`, `TestAsk_HelpModal...` |
| ASK keybind in the statusbar | ✅ Passed | `statusbar_test.go` |

**Tests extra (más allá de los escenarios):** resultados de generación/ejecución
stale ignorados por request token (`_StaleGenerationIgnored`,
`_StaleExecutionIgnored`), registro en historial (`_ExecutedQueryRecordedInHistory`),
sin pista de contexto tras un resultado de query (`_NoContextHintAfterQueryResult`),
gating de loaders y commit del grid mientras hay read-only en vuelo
(`ask_select_test.go`), y unit del componente (`ask_test.go`).

## Cambios clave por módulo

| Módulo | Cambio |
|--------|--------|
| `internal/ui/components/ask/` | Componente overlay nuevo + tests (8 tests unit). |
| `internal/app/app.go` | Wiring de `a`, generación async inyectable, prompt con contexto, ejecución, guards, busy gating. |
| `internal/app/txn.go` | `executeReadOnly`, `selectOnlyViolation` + scanning de strings/comentarios/keywords, `readOnlyActive`/mutex. |
| `internal/drivers/postgres/schema.go` | Helper de texto de schema para el prompt. |
| `internal/cli/ask.go`, `internal/config/config.go` | Helpers de config de proveedor compartidos. |
| `internal/ui/components/grid/table.go` | Accessor de contexto (schema/tabla/where) para la pista. |
| `internal/ui/{modal.go,statusbar.go}`, `palette/commands.go` | Descubribilidad (help, statusbar, palette). |
| `.github/workflows/ci.yml`, `go.mod/go.sum` | Go `1.26.x` + testcontainers-go v0.44.0. |
| `README.md`, `docs/decisions/` | Verificación READ ONLY mandatoria + ADR. |

## Decisiones

- **SELECT completo vs WHERE-only** — se ejecuta el SELECT completo; el contexto
  del grid es pista, no restricción. Motiva el ADR (WHERE-only no puede responder
  agregados/joins, justo la clase de pregunta del usuario objetivo).
- **Garantía en el servidor, no en el cliente** — la transacción READ ONLY es la
  autoridad; la validación es feedback inmediato. Un modelo puede emitir cualquier
  cosa.
- **Rehusar con DML pendiente** — en vez de commitear drafts en silencio.
- **Proveedor inyectable en el modelo** — tests deterministas sin red.
- **Verificación con testcontainers automática** — los dos tests autoritativos
  levantan `postgres:16-alpine` solos cuando `DBX_TEST_DSN` no está seteado;
  `DBX_TEST_DSN` queda como override (fast path sin contenedor).

## Estrategia de test

- **Unit del componente** (`ask_test.go`): transiciones de estado, `enter`/`esc`,
  render del transcript, persistencia.
- **App-level con provider fake** (`ask_test.go`, 30 tests): apertura/guards,
  generación → revisión → ejecución, rechazo no-SELECT, todos los caminos de
  error, pista de contexto, DML pendiente, requests stale, historial.
- **Integración READ ONLY vía testcontainers** (`testdb_test.go`): arranca un
  PostgreSQL real compartido; los dos tests autoritativos corren automáticamente
  en cualquier entorno con Docker (incluido CI ubuntu). Si no hay Docker, se
  saltan con mensaje claro.
- **Regresión de keybinds/docs**: palette, modal y statusbar incluyen ASK.

## Verificación (evidencia)

```
go build ./...                ✅
go vet ./...                  ✅
go test ./...                 ✅  (todos los paquetes ok; internal/app 3.1s)
go test -run 'TestAsk_ReadOnlyTxRejects' -v -count=1
  --- PASS: TestAsk_ReadOnlyTxRejectsDML (3.11s)                     # testcontainers postgres:16-alpine
  --- PASS: TestAsk_ReadOnlyTxRejectsSelectBasedMutation (0.01s)
  ok  github.com/buble/dbx/internal/app  3.426s
make install                  ✅  (binario desplegado en ~/.local/bin/dbx)
```

`docker info` disponible; los contenedores se levantaron y terminaron
limpiamente en la corrida de verificación.

## Commits

- `ee1afba` chore: add ask-nl2sql-chat plan
- `e5a941a` feat(ask): add NL→SQL chat overlay component
- `d4c108e` feat(ask): add read-only execution path and SELECT-only validation
- `05f09dc` feat(ask): wire ASK pane into the app with read-only execution
- `1e00065` refactor(ai): share schema text and provider config helpers
- `c4ba1a5` test(ask): cover 'a' while the grid is filtering
- `f22c248` style(ask): fix test formatting after edits
- `a449718` fix(ask): drop bogus context hint for synthetic query results
- `6537ea2` fix(ask): surface provider resolution error in the toast
- `313fb1e` fix(ask): handle dollar-quoting and E-string escapes in SQL scanning
- `44b70da` fix(ask): reject SELECT INTO/FOR/nextval client-side with a clear reason
- `1a3eda3` fix(ask): ignore stale generation results by request token
- `7c5a650` feat(ask): record executed ASK queries in history
- `44cebdb` refactor(ask): drop unused result summary field
- `a836f3b` refactor(grid): drop test-only EditValue accessor
- `813734e` test(ask): assert help modal renders the ask keybind next to its action
- `96da728` test(ask): cover where-filter/editor guards and extra focus panes
- `9fd09a8` docs(ask): mark DBX_TEST_DSN READ ONLY verification as mandatory
- `85a8fe7` test(ask): prove READ ONLY tx rejects a SELECT-based mutation via ASK wiring
- `2d03dec` docs(ask): add ADR for generic SELECT execution and READ ONLY enforcement
- `2235b16` fix(ask): harden SELECT-only validator and block DB commands during read-only runs
- `34e51d4` fix(ask): tag ASK execution results and assert read-only rejection cause
- `fc3dd11` test: run authoritative READ ONLY tests via testcontainers
- `4f6b2e2` docs: note automatic testcontainers verification with DBX_TEST_DSN override
- `4cc4007` ci: bump Go to stable to match go.mod and enable testcontainers
- `68e6269` fix(ask): make read-only busy state concurrency-safe and gate all DB commands
- `d94114b` test(ask): assert grid commit is refused while a read-only query is in flight
- `c279638` docs: document testcontainers dependency tradeoff
- `5fd22cc` ci: pin Go to the minor matching go.mod

## Files

- **Created**: `internal/ui/components/ask/{ask.go,ask_test.go}`,
  `internal/app/{ask_test.go,ask_select_test.go,testdb_test.go}`,
  `internal/ui/components/grid/edit_value_test.go`,
  `docs/decisions/0001-ask-nl2sql-generic-execution.md`,
  `docs/planning/0003-feature-ask-nl2sql-chat/{issue,plan,context,behavior.feature,diagrams/*}`.
- **Modified**: `internal/app/{app.go,txn.go,dml_rollback_helpers_test.go}`,
  `internal/cli/ask.go`, `internal/config/config.go`,
  `internal/drivers/postgres/schema.go`, `internal/ui/components/grid/table.go`,
  `internal/ui/{modal.go,statusbar.go}`,
  `internal/ui/components/palette/commands.go` (+ tests de palette/modal/statusbar/grid),
  `README.md`, `.github/workflows/ci.yml`, `go.mod`, `go.sum`.

## Tests

- **Added:** ~48 tests ASK-foco (30 app + 10 read-only/gating + 8 unit componente)
  más adiciones de regresión en palette/modal/statusbar/grid.
- **System Tests:** ✅ Passed — `go build/vet/test` verde; los dos tests
  autoritativos READ ONLY corren automáticamente vía testcontainers.

## Documentation

- **Changelog:** ❌ No se actualizó — el repo **no mantiene `CHANGELOG.md`**
  (verificado: no existe en `main` ni en la rama). Sin convención de changelog.
- **Docs:** README (ADR index + sección de verificación READ ONLY), ADR creado.
- **ADR:** ✅ Creado (`docs/decisions/0001-ask-nl2sql-generic-execution.md`).

## Code Review Issues

Hallazgos resueltos durante la implementación (reconstruidos desde los commits de
fix):

- **H1 — Validación SELECT-only laxa**: comentarios, dollar-quoting y escapes
  `E''` podían evadir el escaneo, y `SELECT … INTO/FOR/nextval` pasaba.
  Resuelto en `313fb1e` y `44b70da` (masking de no-código + rechazo explícito).
- **H2 — Estado read-only no concurrency-safe**: flag escrito desde goroutines
  de `tea.Cmd` sin sincronización, y comandos de DB no gateados durante un
  read-only en vuelo. Resuelto en `68e6269` (`atomic.Bool` + mutex +
  `refuseIfBusy`), con tests de gating.
- **M1 — Resultados stale**: una generación/ejecución vieja podía pisar el estado
  nuevo. Resuelto en `1a3eda3` (request token).
- **M2 — Pista de contexto espuria** tras un resultado de query (tabla sintética
  `query`). Resuelto en `a449718`.
- **M3 — Error de resolución del proveedor silenciado**: ahora se muestra en el
  toast (`6537ea2`).
- **Critical Found:** 0 pendientes
- **High Found:** 0 pendientes
- **User Decision:** aprobado continuar y cerrar

## Tradeoffs / Limitaciones conocidas

- **testcontainers-go** agrega ~50 módulos indirectos Go para una necesidad
  test-only y requiere Docker en runtime; sin Docker los tests se saltan con
  mensaje claro (documentado en ADR y README).
- **La validación cliente sobre-rechaza** algunos statements válidos con keywords
  de mutación (p. ej. `SELECT … FOR UPDATE`); fail-safe, la garantía es la
  transacción.
- **Conexión única compartida**: ASK no ejecuta con una tx DML pendiente, y otros
  comandos de DB se rehúsan mientras hay un read-only en vuelo.
- **Sin edición del SQL generado** dentro del panel (solo revisar/ejecutar/
  cancelar); sin streaming token-por-token; transcript no persistido a disco
  (out of scope del plan).

## Convención de cierre

El repo **no archiva** las carpetas de planificación en `docs/planning/archived/`:
el cierre previo (`0001-refactor-keybinds-single-source`, commit `b562360`) agregó
`summary.md` **in place** dentro de la carpeta del plan. Este cierre sigue esa
misma convención (no se crea `archived/`, `.ignore` ni `AGENTS.md` guard). No hay
CHANGELOG en el repo.

## Next Step

Pushear `feat/ask-nl2sql-chat` a origin y abrir PR → `main`. **Pendiente de
autorización explícita del usuario.**
