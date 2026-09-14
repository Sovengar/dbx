# ERE Diagram Tab — Text-based Entity-Relationship diagram in Explorer Preview

**Type:** feature
**Component:** `internal/ui/components/explorerpreview` (tab index 5, keybind `6` / `explorer.tab_ere`)
**Status:** DRAFT — awaiting approval

---

## User Story

**As a** dbx user exploring a PostgreSQL schema in the TUI,
**I want** an Entity-Relationship diagram tab that shows the current table, its columns (PK/FK marked), and its foreign-key relationships to/from other tables,
**So that** I can understand table relationships at a glance without leaving the TUI or running introspection queries manually.

### Sub-stories

- As a user, I want the current table rendered as a box with its columns, so I can see which columns are PK (`🔑`/`PK`) and FK (`FK`).
- As a user, I want relationship lines drawn from the current table to related tables (outgoing FKs and incoming references), so I can see the graph neighborhood.
- As a user, I want to navigate to a related table by pressing Enter on a neighbor box, so the diagram re-centers on that table and shows ITS relationships — enabling schema exploration by walking the graph.
- As a user, I want the diagram to scroll (viewport) when it exceeds the pane size, so large schemas don't break the layout.
- As a user, I want the diagram styled with the existing theme system (Lipgloss), so it matches the rest of the UI.

---

## Answers to Refinement Questions

### 1. Core problem

The ERE tab (keybind `6`) is a dead placeholder. Users currently have to read the FK tab row-by-row and mentally reconstruct the relationship graph. A text diagram turns structured FK data into instant spatial understanding — "what references this table, and what does it reference" — which is the primary mental model when navigating an unfamiliar schema.

### 2. Minimal viable diagram (MVP)

**Scope: 1-hop neighborhood around the current table.** Do NOT attempt full-schema layout.

```
  ┌──────────────────┐         ┌──────────────────┐
  │ users            │         │ orders           │
  ├──────────────────┤   N:1   ├──────────────────┤
  │ 🔑 id         PK │◄────────│ 🔑 id         PK │
  │    email         │         │    user_id    FK │──┐
  │    name          │         │    total         │  │
  └──────────────────┘         └──────────────────┘  │
                               ┌──────────────────┐  │
                               │ products         │  │
                               ├──────────────────┤  │
                               │ 🔑 id         PK │◄─┘
                               │    name          │
                               └──────────────────┘
```

MVP rules:
- **Center box**: current table, full column list with PK/FK badges.
- **Neighbor boxes**: referenced tables (outgoing FKs) and referencing tables (incoming FKs) — table name + only the columns involved in the relationship (keep boxes small).
- **Edge labels**: cardinality (`1:N`, `N:1`, `1:1` inferred from unique constraint on FK column — MVP can default to `N:1`/`1:N` without uniqueness analysis).
- **Layout**: simple vertical stack — current table centered, outgoing-FK tables on one side/column, incoming-FK tables on the other. No force-directed or Sugiyama layout in MVP.
- **Empty state**: table with no FKs in either direction → "No relationships for this table."

### 3. Technical risks

| Risk | Severity | Mitigation |
|------|----------|------------|
| **Layout algorithm complexity** — general graph layout is a rabbit hole | High | MVP restricts to 1-hop star layout around current table; no general graph solver needed |
| **Box width overflow** — long table/column names or wide types break fixed-width boxes | Medium | Compute box width dynamically from content; truncate column names with `…` at a max width |
| **Many neighbors** — hub tables (e.g., 30+ FKs) produce huge diagrams | Medium | Cap rendered neighbors per pass (e.g., first N by name), rely on scrolling; show "+N more" indicator |
| **Scrolling/viewport** — preview pane has fixed height; diagram taller than pane | Medium | Reuse existing scroll pattern (check how other tabs handle overflow); arrow-key/j/k scroll with viewport offset |
| **Unicode width** — box-drawing chars + emoji badges (`🔑`) have ambiguous cell widths in some terminals | Low | Prefer ASCII badges `PK`/`FK` over emoji; test in the target terminal |
| **Re-render cost** — recomputing layout on every resize/keypress | Low | Cache rendered diagram; invalidate only on SetData/resize |

### 4. Data we need vs. already have

**Already available in `ExplorerPreview`:**
- Current table columns (`[]ColumnInfo`) — for the center box
- Current table constraints (`[]ConstraintInfo`) — PK identification
- Current table FKs (`[]ForeignKeyInfo`) — outgoing edges
- Schema name + table name

**Missing / needs wiring:**
1. **Incoming FKs** (tables that reference the current table): `ListForeignKeysBySchema(ctx, schema)` already exists (`internal/drivers/postgres/schema.go:282`) and returns `map[string][]ForeignKeyInfo` for ALL tables. It is already called in `internal/app/app.go:246` during schema load — but the result is only used for the schema export, **not passed to the preview**. Need to plumb the schema-wide FK map (or a filtered incoming list) into `ExplorerPreview` via a new setter (e.g., `SetSchemaForeignKeys(map[string][]ForeignKeyInfo)`).
2. **Neighbor column lists** (for neighbor boxes): MVP can render neighbor boxes with only the FK columns involved (known from FK data — no extra query). Full column lists for neighbors would require per-table column fetches — defer to a follow-up.
3. **PK columns of neighbor tables**: not needed in MVP since neighbor boxes only show FK columns.

**No new SQL queries required for MVP.** Only new plumbing of existing data.

---

## Acceptance Criteria

- [ ] Pressing `6` (`explorer.tab_ere`) on a table with relationships renders a text ERE diagram using box-drawing characters.
- [ ] Center box shows current table name + all columns with `PK`/`FK` indicators.
- [ ] Outgoing FKs render as edges to referenced tables; incoming FKs render as edges from referencing tables.
- [ ] Edge cardinality labels shown (`1:N` / `N:1`).
- [ ] Diagram is styled via `theme.Styles` (no hardcoded colors).
- [ ] Diagram scrolls when taller/wider than the pane; scroll state resets on table change.
- [ ] Tables with zero relationships show a clean empty-state message.
- [ ] Hub tables (many FKs) render without crashing or overflowing — capped neighbors + "+N more" note.
- [ ] Pressing Enter on a neighbor box re-centers the diagram on that table, showing its relationships.
- [ ] Navigation updates the explorer tree selection to the new table (keeps state in sync).
- [ ] `go build ./... && go vet ./... && go test ./...` pass; `make install` deployed.
- [ ] Keybind checklist honored (Enter for navigation must not collide with existing preview keybinds).

## Task Breakdown

1. **Data plumbing**: add `SetSchemaForeignKeys` (or equivalent) to `ExplorerPreview`; wire from `app.go` where `ListForeignKeysBySchema` result is already available.
2. **Diagram model**: build a `relationship graph` struct (current table + outgoing/incoming edges) from existing FK data.
3. **Renderer**: new `ere.go` in `explorerpreview` — box renderer (width computed from content), edge renderer, star layout, theme styles.
4. **Viewport/scroll**: integrate scrolling for oversized diagrams.
5. **Navigation**: cursor-based selection on neighbor boxes; Enter re-centers diagram on selected table; arrow keys move selection.
6. **Edge cases**: empty state, hub cap, name truncation, cross-schema refs (`RefSchema != current schema` → annotate table as `schema.table`).
7. **Tests**: unit tests for layout math (box widths, edge routing) and graph building; golden-output test for a small fixture schema.
8. **Docs**: update README preview-tabs section + `docs/KEYBINDS.md` if any scroll keys are added.

## Testing Plan

- Unit: graph builder (outgoing/incoming classification, dedup of multi-column FKs), box width computation, truncation.
- Golden: render fixture schema (2 tables, self-ref FK, cross-schema FK) and snapshot output.
- Manual smoke: open dbx against a schema with hub table; verify scroll, resize behavior, theme switching.

## Out of Scope (follow-ups)

- Full-schema diagram (all tables, multi-hop)
- Zoom/expand neighbor boxes to full column lists
- Export diagram to file (SVG/Mermaid)
