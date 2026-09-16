# Proposal: DML Rollback con "U"

## Problem

Cuando el usuario ejecuta DML (INSERT/UPDATE/DELETE) desde el editor, cada statement se ejecuta en autocommit — se commita inmediatamente. Si ejecuta un batch de queries y una falla o produce un resultado inesperado, no hay forma de deshacer los cambios ya commiteados.

El usuario necesita ability para rollback de cambios DML antes de que se hagan permanentes.

## Outcome

Añadir un keybind `U` que ejecute `ROLLBACK` sobre la transacción activa. Las queries DML se ejecutarán dentro de una transacción que se mantiene abierta hasta que:
1. El usuario ejecute otro statement (auto-commit del anterior)
2. El usuario presione `Ctrl+S` (commit manual)
3. El usuario presione `U` (rollback)

SELECT y DDL no se ven afectados — continúan en autocommit.

## Scope

**In:**
- Transaction mode para DML statements (INSERT/UPDATE/DELETE)
- Keybind `U` para rollback (`global.rollback`)
- Auto-commit del statement anterior antes de ejecutar uno nuevo
- Visual indicator en statusbar cuando hay transacción pendiente
- Palette command y help modal

**Out:**
- Reversal SQL generation (post-commit undo)
- Transaction isolation level configuration
- Savepoint support
- Batch commit (commit de múltiples statements juntos)

## Approach

1. Añadir campo `txInProgress *pgx.Tx` al Model
2. Modificar `executeQuery` para detectar DML y envolver en `BEGIN`
3. Añadir `isDML()` — detecta INSERT/UPDATE/DELETE
4. `U` → enviar `ROLLBACK` si hay transacción pendiente
5. Auto-commit antes de cada nuevo statement
6. Statusbar muestra "transaction pending" cuando `txInProgress != nil`
