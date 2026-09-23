---
adr_required: true
adr_title: ask-nl2sql-generic-execution
adr_reason: >
  Decisión de arquitectura con tradeoffs reales: ejecutar un SELECT completo
  generado por el AI (en vez de restringir a filtros WHERE) y cómo garantizar
  SELECT-only (transacción READ ONLY + validación del statement). Afecta la
  postura de seguridad y es difícil de revertir sin rehacer el flujo.
---

# Plan: ASK — panel de chat NL→SQL

## Resultado esperado

Un overlay "ASK" abierto con `a` donde el usuario escribe una pregunta en
lenguaje natural; el AI genera un `SELECT` completo, se muestra para revisión,
y al confirmar se ejecuta en una transacción `READ ONLY` y el resultado se
renderiza en el grid. El transcript conserva pregunta + SQL ejecutado.

## Enfoque (alto nivel)

1. **Componente overlay nuevo (`ask`)** modelado sobre `querybrowser` /
   `palette`: `Show/Hide/IsVisible`, `SetWidth/SetHeight`, `Update`, `View`, y
   un transcript interno de turnos (pregunta → SQL → estado). Emite mensajes al
   app (`AskSubmittedMsg`) y expone setters para que el app le inyecte el SQL
   generado, los errores y el resultado. Estados internos: `typing` →
   `generating` → `review` → `executing` → `done|error`.

2. **Wiring en el app**: handler de `a` en `StateMain` (después de los guards de
   overlays y de los modos de edición/filtrado del grid, para que no haya
   colisión: editar/filtrar consume la tecla primero). El overlay se renderiza
   con el helper `overlay(...)` existente y se suma a los guards de mouse/scroll,
   igual que palette/querybrowser.

3. **Generación NL→SQL**: se resuelve el proveedor una vez en `NewModel`
   (`nl2sql.Resolve`) y se guarda como campo inyectable del modelo (interface
   `nl2sql.Provider`), para poder sustituirlo por un fake en tests. La
   generación corre en un `tea.Cmd` (async) y vuelve como mensaje. El prompt se
   arma con el schema **en memoria** (`m.schemaDetail`, ya cargado) más la pista
   de contexto; no hay round-trip a la DB para armar el prompt.

4. **Pista de contexto**: si el grid tiene una tabla cargada, se agrega
   `schema.table` y el `WHERE` actual al prompt como *contexto*, nunca como
   restricción. Requiere un accessor exportado en el grid (hoy `schema` es
   privado; `TableName()` y `WhereClause()` ya son públicos).

5. **SELECT-only en dos capas (defensa en profundidad)**:
   - **Validación en código** antes de ejecutar: aceptar solo `SELECT` o
     `WITH ... SELECT` (helper análogo a `isDML`/`isDDL`). Si no, se rechaza con
     error en el transcript y no se ejecuta.
   - **Ejecución en transacción `READ ONLY`**: extender `statementRunner` con un
     camino `executeReadOnly` que abra `BeginTx` con `AccessMode: ReadOnly`,
     ejecute y haga `Rollback` (no hay nada que commitear). El servidor rechaza
     cualquier DML/DDL aunque pase la validación.

6. **Resultado al grid**: en éxito, `m.grid.SetData(result, "", "query")` +
   foco al grid + toast con el conteo de filas, y el panel se cierra —
   consistente con el editor, que también se cierra al ejecutar con éxito. En
   error, el panel queda abierto con el mensaje en el transcript.

7. **Descubribilidad**: comando en palette (`global.ask`), entrada en el help
   modal (sección Global), acción en el statusbar (`a ask`). README y
   `docs/KEYBINDS.md` ya lo documentan; se verifica/ajusta el texto.

## Decisiones clave

- **SELECT completo vs filtro WHERE-only**: se ejecuta el SELECT completo
  (soporta agregados, joins, proyecciones arbitrarias). El contexto del grid es
  una pista, no una restricción. Es la decisión que motiva el ADR.
- **Revisión antes de ejecutar**: el SQL se muestra en el transcript y el
  usuario confirma con `enter` (`esc` cancela). Honra lo ya documentado en
  `README.md` ("shows for review, executes on confirm") y da control antes de
  tocar la base.
- **Transacción READ ONLY + validación**: la validación en código da feedback
  inmediato; la transacción es la garantía real del servidor.
- **Transacción DML pendiente bloquea ASK**: pgx no permite abrir una segunda
  transacción sobre la misma conexión. En vez de commitear drafts del usuario a
  escondidas, se rechaza con un mensaje claro ("commit o rollback primero").
- **Proveedor inyectable en el modelo**: mantiene los tests deterministas sin
  red.

## Riesgos

- **Conexión única**: la ejecución read-only comparte la conexión; sólo es
  problema con una transacción DML pendiente (mitigado rechazando).
- **Límite de filas**: el system prompt pide `LIMIT 100` por defecto; una
  pregunta agregada (COUNT) devuelve una fila. Igual conviene que el grid maneje
  resultados grandes como ya lo hace para queries del editor.
- **Costo/latencia del proveedor**: la generación es async con estado
  "generating" visible; no bloquea la UI.
- **Modelo que ignora el prompt**: mitigado por las dos capas de SELECT-only.

## Orden de trabajo (grueso)

1. Componente `ask` + tests unitarios de estados/keys.
2. Campo de proveedor inyectable + mensajes de generación/ejecución.
3. `statementRunner.executeReadOnly` + validación de statement.
4. Wiring de tecla `a`, render del overlay y guards de mouse/scroll.
5. Accessor de contexto del grid y armado del prompt con pista.
6. Descubribilidad (palette/help/statusbar) + docs.
7. Tests de app end-to-end con provider fake + integración gated por DSN.

## Estrategia de test (ATDD/BDD, tests reales — no Cucumber)

Los escenarios de `behavior.feature` se traducen a tests Go reales:

- **Unit del componente** (`ask`): transiciones de estado, `enter`/`esc`,
  render del transcript.
- **App-level con provider fake**: abrir con `a`; pregunta → SQL en transcript;
  `enter` ejecuta → grid poblado + panel cerrado; SQL no-SELECT rechazado; error
  del proveedor; sin proveedor; sin conexión; pista de contexto presente/ausente;
  transacción DML pendiente rechazada; sin colisión con edición/filtrado.
- **Integración (gated por `DBX_TEST_DSN`)** al estilo del harness existente de
  `dml_rollback`: verificar que un statement mutante falla dentro de la
  transacción read-only, y que un SELECT válido devuelve filas.
- **Regresión de keybinds/docs**: palette, help y statusbar incluyen `global.ask`.

## Verificación final

`go build ./... && go vet ./... && go test ./...` y luego `make install`
(el usuario corre `~/.local/bin/dbx`). No commitear a `main`; commits atómicos
Conventional Commits en la rama `feat/ask-nl2sql-chat`.

La garantía autoritativa de SELECT-only (PostgreSQL rechazando mutaciones
dentro de la transacción READ ONLY) **solo se verifica con base real**: el test
`TestAsk_ReadOnlyTxRejectsDML` está gated por `DBX_TEST_DSN` y es un paso de
verificación **obligatorio** (no opcional):

```bash
DBX_TEST_DSN='postgres://user:pass@localhost:5432/db' go test ./internal/app -run TestAsk -v
```

Sin `DBX_TEST_DSN` esa garantía queda sin verificar.
