# Plan: ERE Diagram Viewer

**adr_required: false**

## Summary

Replace the ERE tab placeholder with a functional text-based Entity-Relationship diagram using a 1-hop star layout. The diagram shows the current table centered with its columns (PK/FK badges), connected to neighbor tables via FK relationships. Navigation allows re-centering on any neighbor.

## Approach

### Phase 1: Data Plumbing

Wire existing schema-wide FK data into `ExplorerPreview`. `ListForeignKeysBySchema` is already called at schema load (`app.go:246`) — the result just needs to flow to the preview component.

- Add `SetSchemaForeignKeys(map[string][]ForeignKeyInfo)` to `ExplorerPreview`
- In `app.go`, pass the schema FK map when loading explorer preview data
- Add a `centerTable` / `centerSchema` field to track the currently centered table (separate from the explorer's selected table)

**Critical**: The schema-wide FK map is already in memory at schema load time. No new SQL queries.

### Phase 2: Graph Model

Build an in-memory relationship graph from FK data:

- Classify outgoing edges (current table's FKs → other tables)
- Classify incoming edges (other tables' FKs → current table)
- Infer PK columns from constraints data (already available)
- Handle cross-schema references by prepending schema name

**Files**: New `ere.go` in `explorerpreview/` with `ERDiagram`, `TableBox`, `Relationship` structs.

### Phase 3: Renderer

Text-based renderer using box-drawing characters:

- Dynamic box width: `max(len(col_name) + len(data_type))` clamped to [20, 50]
- Center box positioned left, neighbors stacked vertically right
- Edge routing: horizontal lines from center FK column to neighbor referenced column
- Cardinality labels on edges: `N:1` (outgoing), `1:N` (incoming)
- Theme styling via `theme.Styles` — no hardcoded colors

**Risk**: Layout math for edge routing. Keep it simple — straight horizontal lines with vertical connectors, no complex pathfinding.

### Phase 4: Navigation & Scrolling

- Cursor state tracks which neighbor is selected (or none = center)
- `↑`/`↓` moves cursor; `j`/`k` as alternatives
- `Enter` on selected neighbor → update `centerTable`, regenerate diagram, update explorer tree selection
- Viewport: scroll offset when diagram height > pane height
- `g`/`G` for top/bottom jump
- Scroll reset on table change

**Risk**: Must not steal keybinds from other preview tabs. Navigation keys only active when ERE tab is focused.

### Phase 5: Edge Cases & Polish

- Empty state: "No relationships for this table"
- Hub cap: max 10 neighbors per direction, "+N more" indicator
- Truncation: column names > 30 chars, table names > 40 chars
- Cross-schema: show `schema.table` format

## Key Files

| File | Change |
|------|--------|
| `internal/ui/components/explorerpreview/preview.go` | Add `SetSchemaForeignKeys`, `centerSchema`, update `renderERE()` |
| `internal/ui/components/explorerpreview/ere.go` | **New**: diagram model, renderer, layout, navigation |
| `internal/app/app.go` | Wire schema FK map to preview component |
| `internal/app/messages.go` | Add schema FKs to `explorerPreviewDataMsg` |
| `internal/drivers/postgres/schema.go` | No changes (existing queries sufficient) |

## Testing Strategy

- **Unit**: Graph builder (outgoing/incoming classification), box width computation, truncation
- **Golden**: Render fixture schema (2 tables + self-ref FK + cross-schema FK) → snapshot output
- **Manual**: Open dbx against a schema with hub table; verify scroll, resize, theme switching, navigation

## Commit Strategy

1. `feat(ere): wire schema-wide FKs to ExplorerPreview` — data plumbing
2. `feat(ere): add graph model and star layout` — model + layout algorithm
3. `feat(ere): implement ERE diagram renderer` — box/edge rendering
4. `feat(ere): add navigation and scrolling` — cursor, Enter, viewport
5. `feat(ere): handle edge cases` — empty state, hub cap, truncation
