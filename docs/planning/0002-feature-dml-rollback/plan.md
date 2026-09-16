---
adr_required: false
---

# Plan: DML Rollback with U

## Summary

Add transaction mode for DML statements and `U` keybind for rollback. DML (INSERT/UPDATE/DELETE) runs inside a transaction that stays open until the user commits or rolls back.

## Architecture

### Current Flow
```
Editor → executeQuery() → loader.ExecuteRaw() → conn.Query() → autocommit
```

### New Flow
```
Editor → executeQuery()
  ├─ isDML? → BEGIN → ExecuteRaw() → tx stays open
  ├─ isDDL/SELECT? → ExecuteRaw() → autocommit (no change)
  └─ if txInProgress exists → COMMIT first, then proceed

U key → ROLLBACK (if txInProgress)
```

### Key Decision: Transaction Lifecycle

- **Open**: When DML is executed, a pgx.Tx is created via `conn.Begin()`
- **Auto-commit**: Before any new statement (DML or DDL), if txInProgress exists → `tx.Commit()` then clear
- **Manual commit**: `Ctrl+S` from grid commits the pending tx
- **Rollback**: `U` sends `ROLLBACK` and clears txInProgress
- **Disconnect**: If user quits with pending tx → auto-rollback before closing conn

## Implementation Steps

### Step 1: Add `isDML()` function to app.go
- Mirror existing `isDDL()` pattern
- Detect INSERT, UPDATE, DELETE prefixes (case-insensitive)
- Exclude SELECT, WITH (CTEs that may be read-only — treat as DML to be safe)

### Step 2: Add transaction state to Model
- Add `txInProgress pgx.Tx` field to Model struct
- Initialize to nil in NewModel

### Step 3: Modify `executeQuery()` in app.go
- Before executing: check if `txInProgress != nil` → auto-commit first
- After detecting DML: use `conn.Begin()` instead of `loader.ExecuteRaw()`
- Store tx in `m.txInProgress`
- For non-DML: use existing `loader.ExecuteRaw()` path (autocommit)

### Step 4: Add `global.rollback` keybind
- `internal/config/keybindings.go`: add `"global.rollback": "U"` to both DefaultKeybindings and defaultBindings
- No alternatives needed (uppercase U is unique)

### Step 5: Add rollback handler in app.go
- In the key handling switch, match `"global.rollback"`
- If `m.txInProgress != nil` → `tx.Rollback()`, clear field, show toast
- If nil → show info toast "No pending transaction"

### Step 6: Update statusbar
- `internal/ui/statusbar.go`: when `txInProgress != nil`, add "U rollback" to keybinds line
- Add visual indicator (e.g., colored dot or "tx" label) when transaction is pending

### Step 7: Update help modal
- `internal/ui/modal.go`: add `global.rollback` entry in Global section

### Step 8: Update palette
- `internal/ui/components/palette/commands.go`: add "Rollback" command with alias "rollback"

### Step 9: Update documentation
- `docs/KEYBINDS.md`: add `global.rollback` row, update Action Naming Convention
- `README.md`: add rollback to keybinds table, add to Draft-based Editing section

## Critical Details

### pgx Transaction Usage
```go
tx, err := m.conn.Begin(ctx)
// execute DML via tx.Exec() or tx.Query()
// NOT via loader.ExecuteRaw() which uses conn directly

// On rollback:
tx.Rollback(ctx)

// On commit:
tx.Commit(ctx)
```

**Important**: `loader.ExecuteRaw()` uses `s.conn` (the raw connection). For DML in transaction mode, we need to execute via `tx.Exec()` or `tx.Query()` instead. This means we need a new execution path that takes a `pgx.Tx` parameter.

### Error Handling
- If `tx.Commit()` fails → log error, clear txInProgress, show error toast
- If `tx.Rollback()` fails → log error, clear txInProgress anyway
- If connection drops with pending tx → PostgreSQL auto-rolls back (server-side)

### Edge Case: Batch SQL with Mixed Statements
```sql
UPDATE users SET name='a'; CREATE TABLE test (id int); DELETE FROM users WHERE id=1;
```
- Split by `splitSQL()` (already exists)
- Execute each: DML goes to tx, DDL triggers commit of pending tx then executes in autocommit
- If DDL is in the middle, it commits previous DML and executes itself in autocommit, then subsequent DML starts a new tx

## Files Modified

| File | Change |
|------|--------|
| `internal/app/app.go` | Add `isDML()`, `txInProgress` field, modify `executeQuery()`, add rollback handler |
| `internal/drivers/postgres/query.go` | Add `ExecuteQueryWithTx()` that takes pgx.Tx parameter |
| `internal/config/keybindings.go` | Add `global.rollback` with "U" |
| `internal/ui/statusbar.go` | Show rollback keybind when tx pending |
| `internal/ui/modal.go` | Add rollback to help modal |
| `internal/ui/components/palette/commands.go` | Add rollback command |
| `docs/KEYBINDS.md` | Document new action |
| `README.md` | Update keybinds table |

## Testing Strategy

1. **Manual smoke test**: Execute UPDATE → verify tx opens → press U → verify rollback
2. **Batch test**: Execute multiple DML → verify auto-commit between statements
3. **Mixed test**: DML + DDL in same batch → verify DDL commits previous DML
4. **SELECT test**: Verify SELECT doesn't open transaction
5. **Edge case**: U without pending transaction → verify info toast
