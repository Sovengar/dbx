# Issue: ASK — chat NL→SQL en la TUI

## Contexto / Problema

dbx ya documenta `a` como "Ask (NL→SQL)" en `README.md`, `docs/KEYBINDS.md` y
`internal/config/keybindings.go` (`global.ask = "a"`), y el flujo `dbx ask`
existe en la CLI. Pero **la TUI no tiene handler**: presionar `a` no hace nada.
La promesa documentada está incumplida.

Además, la única forma de ejecutar SQL en la TUI es escribir en el editor o
navegar el grid. Un usuario que no conoce SQL no puede consultar la base.

## User Story

**Como** usuario de dbx que conoce los datos pero no SQL,
**quiero** abrir un panel de chat con `a`, escribir mi pregunta en lenguaje
natural y obtener el resultado en el grid,
**para** consultar la base de datos sin escribir SQL a mano.

## Alcance

- Panel overlay "ASK" abierto con `a` (global, disponible en cualquier pane,
  siempre que no haya otro overlay ni el grid esté editando/filtrando).
- Entrada de texto para la pregunta; `enter` envía.
- El AI genera un `SELECT` completo (contrato `nl2sql.Provider.Generate`).
- El SQL generado se muestra en el transcript para revisión; `enter` confirma y
  ejecuta; `esc` cancela.
- El resultado se renderiza en el **grid** (reemplaza la vista actual), igual
  que al ejecutar desde el editor.
- Cuando hay una tabla cargada en el grid, se pasa `schema.table` + `WHERE`
  actual como **pista de contexto** (no como restricción dura).
- SELECT-only en dos capas: validación del statement en código + ejecución
  dentro de una transacción PostgreSQL `READ ONLY`.
- Transcript persistente por sesión (preguntas + SQL ejecutado) mientras el
  modelo viva.

## Acceptance Criteria

1. [ ] `a` abre el panel ASK; el foco va al input.
2. [ ] `a` funciona sin importar el pane activo (explorer/grid/editor cerrado).
3. [ ] `a` **no** se dispara mientras el grid edita una celda o filtra.
4. [ ] Escribir una pregunta + `enter` dispara la generación NL→SQL.
5. [ ] El SQL generado se muestra en el transcript antes de ejecutar.
6. [ ] `enter` sobre el SQL revisado lo ejecuta; `esc` cierra sin ejecutar.
7. [ ] El resultado se muestra en el grid y el panel se cierra.
8. [ ] Con tabla cargada, el prompt incluye `schema.table` y el `WHERE` actual.
9. [ ] SQL no-SELECT generado por el AI → rechazado, no se ejecuta.
10. [ ] La ejecución corre en transacción `READ ONLY` (DML/DDL falla aunque
        pase la validación).
11. [ ] Error del proveedor AI → mensaje en el transcript, panel abierto.
12. [ ] Sin conexión a la base → error claro al intentar ejecutar.
13. [ ] Sin proveedor AI configurado → toast de error, no abre el panel.
14. [ ] SQL con error de sintaxis → error en el transcript, panel abierto.
15. [ ] `a` con transacción DML pendiente → se rechaza con mensaje claro.
16. [ ] Comando en palette, entrada en help modal y en statusbar.
17. [ ] Logs de debug en `/tmp/dbx_ask_debug.log`.

## Out of scope

- Routing vía servidor local de opencode (HTTP API), salida JSON estructurada,
  header estable `x-opencode-session` para prompt caching.
- Múltiples conversaciones / persistencia en disco del transcript.
- Edición del SQL generado dentro del panel (solo revisar/ejecutar/cancelar).
- Streaming token-por-token de la respuesta del modelo.
- Cambiar el contrato de `nl2sql` (sigue devolviendo un SELECT completo).

## Notas de implementación

- El modelo ya tiene el schema completo en memoria (`m.schemaDetail`), así que
  armar el prompt **no requiere round-trip a la DB**.
- `dbx ask` (`internal/cli/ask.go`) es la referencia funcional; se reutiliza
  `nl2sql.Resolve` y el mismo formato de schema para el LLM.
- El keybind ya está reservado y documentado: sólo falta el handler y la UI.
