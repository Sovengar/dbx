# 0005 — fix/editor-history-ctrl-pn

Fix: after a successful execution the SQL editor buffer is emptied, so the first
`ctrl+p` immediately recalls the just-executed query instead of appearing to do
nothing (history sentinel `historyIdx = len(history)` assumes an empty prompt).
Error path keeps the SQL. Approved as option A.

## Scenario

| # | Scenario | Commit |
|---|----------|--------|
| 1 | Successful run empties the editor; reopen + first `ctrl+p` recalls the last executed query, second steps back | eea9442aa3df3f63ece76b07cebba8ce0f9a9bf3 |

## Progress

Phase: close
Review: 2026-10-12 — verdict PASS (five-way: behavior + code/security/performance/docs lenses);
0 critical, 0 high, 0 medium, 5 low (all recorded, none blocking). Full `go test ./...` green.
Change opened for integration via `publish-change --open` (change #24).
Close: started 2026-10-12
