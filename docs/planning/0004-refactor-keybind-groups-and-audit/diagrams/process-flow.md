# Process flow — keybind-groups-and-audit

Feature-aware planning flow for THIS change (slug `keybind-groups-and-audit`).

```mermaid
flowchart TD
    A["Issue: grouping + audit A/C<br/>(0004-refactor-keybind-groups-and-audit)"] --> B["Checkpoint issue ✅<br/>rev2: pane=primary only, modal=all keys"]
    B --> C["brainstormer + architect (parallel)"]
    C --> D["behavior.feature<br/>+ digit-jump removal"]
    D --> E["Checkpoint behavior ✅<br/>user: remove grid 0-9"]
    E --> F["plan.md<br/>adr_required = true"]
    F --> G["ADR 0002<br/>groups + widget-local boundary"]
    G --> H["context.md<br/>(codebase-researcher)"]
    H --> I["Checkpoint plan ⏸"]
    I --> J["swe-executor"]
```

## Decisions made for this change

```mermaid
flowchart LR
    D1["Group + GroupLabel on Action"] --> R1["aggregator in config,<br/>shared by pane + modal"]
    D2["No new Resolver method"] --> R2["derive from ActionsFor(context)"]
    D3["Editor gains Resolver"] --> R3["dead raw cases deleted,<br/>tab keeps dual behavior"]
    D4["Mouse -> action ID"] --> R4["never synthesize KeyPressMsg"]
    D5["Grid 0-9 removed"] --> R5["intentional behavior change<br/>F-keys only"]
    D6["Modal ?/q/esc"] --> R6["documented overlay exception"]
```

- **Scope cuts**: widget text-entry keys (B) untouched; no Section grouping; no modal
  registry context; no mouse configurability.
- **ADR**: `0002-keybind-groups-and-widget-local-boundary` (additive `Action` change +
  boundary decisions with discarded alternatives).
- **Numbering note**: `0001` and `0003` already existed, so this plan is `0004`.
