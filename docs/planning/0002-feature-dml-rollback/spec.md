# Spec: DML Rollback

## Functional Requirements

### FR-1: Transaction Mode para DML
Las queries DML (INSERT, UPDATE, DELETE) se ejecutan dentro de una transacción que se mantiene abierta. La transacción NO se commitea automáticamente.

### FR-2: Keybind U — Rollback
`U` (mayúscula) ejecuta `ROLLBACK` sobre la transacción activa. Si no hay transacción pendiente, muestra toast informativo.

### FR-3: Auto-commit Before New Statement
Antes de ejecutar cualquier nuevo statement (DML o DDL), si hay una transacción pendiente, se ejecuta `COMMIT` automáticamente.

### FR-4: SELECT y DDL — Autocommit
SELECT, DDL (CREATE/ALTER/DROP/TRUNCATE), y statements que no son DML continúan en autocommit. No abren transacción.

### FR-5: Statusbar Indicator
Cuando hay una transacción pendiente, el statusbar muestra un indicador (ej: "tx pending") en la zona de keybinds.

### FR-6: Palette y Help Modal
El comando "Rollback Last Transaction" aparece en la palette con alias "rollback" y en el help modal.

### FR-7: Toast Feedback
- Rollback exitoso → toast verde "Transaction rolled back"
- Rollback sin transacción → toast azul "No pending transaction"
- Commit automático → toast verde "Transaction committed" (solo si había DML pendiente)

## Acceptance Criteria

1. [ ] Ejecutar `UPDATE users SET name='test'` → transacción se abre, cambios no visibles en DB hasta commit
2. [ ] Presionar `U` → rollback, cambios desaparecen
3. [ ] Ejecutar `UPDATE` → luego otro `UPDATE` → primero se commitea automáticamente
4. [ ] Ejecutar `SELECT * FROM users` → no abre transacción, resultado inmediato
5. [ ] Ejecutar `CREATE TABLE test (id int)` → no abre transacción
6. [ ] `U` sin transacción pendiente → toast informativo, sin error
7. [ ] Statusbar muestra indicador cuando hay transacción pendiente
8. [ ] Palette tiene comando "Rollback" con alias "rollback"
9. [ ] Help modal muestra la acción `global.rollback`

## Out of Scope

- Reversal SQL generation (post-commit undo)
- Transaction isolation level configuration
- Savepoint support
- Batch commit from editor
