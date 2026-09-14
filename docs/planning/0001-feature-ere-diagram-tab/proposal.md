# Proposal: ERE Diagram Viewer

## Problem

The ERE tab (keybind `6`) in the Explorer Preview is a placeholder showing "coming soon". Users exploring an unfamiliar schema must mentally reconstruct relationship graphs from the FK tab's row-by-row listing, which is slow and error-prone for complex schemas.

## Intended Outcome

Replace the placeholder with a functional text-based Entity-Relationship diagram that:
- Shows the current table centered with all columns (PK/FK badges)
- Renders neighbor tables connected by FK relationships (outgoing + incoming)
- Allows navigation: Enter on a neighbor re-centers the diagram on that table
- Scrolls when the diagram exceeds the pane height

## Scope

**In:**
- 1-hop star layout around the current table
- Center box: full column list with PK/FK indicators
- Neighbor boxes: table name + FK columns only (small)
- Edge labels: cardinality (`1:N` / `N:1`)
- Navigation: cursor selection on neighbors, Enter to re-center
- Viewport scrolling for oversized diagrams
- Theme-styled rendering via Lipgloss

**Out:**
- Full-schema multi-hop diagram
- Export to SVG/Mermaid
- Zoom/expand neighbor boxes to full columns

## Approach

1. **Data plumbing**: Wire the existing `ListForeignKeysBySchema` result (already loaded at schema init) into `ExplorerPreview` via a new setter. No new SQL needed.
2. **Graph model**: Build an in-memory relationship graph from FK data — classify outgoing (current → other) and incoming (other → current) edges.
3. **Renderer** (`ere.go`): Box-drawing renderer with dynamic width computation, edge routing, and star layout algorithm.
4. **Navigation**: Cursor state + Enter handler that updates the centered table and regenerates the diagram.
5. **Viewport**: Scroll offset with arrow-key/jk scrolling, reset on table change.

## Key Files

- `internal/ui/components/explorerpreview/preview.go` — main component, renderERE()
- `internal/ui/components/explorerpreview/ere.go` — new: diagram renderer
- `internal/app/app.go` — wire schema-wide FKs to preview
- `internal/drivers/postgres/schema.go` — existing FK queries (no changes needed)
