# Feature flow — Keybind grouping + registry hardening

Derived from `behavior.feature`: the registry feeds one grouping path used by both
surfaces, and dispatch never bypasses it.

```mermaid
flowchart TD
    R[("Registry<br/>Action{ID, Keys[], Section,<br/>Description, Contexts, Owner, Pending,<br/>Group, GroupLabel}")]

    subgraph DISPLAY["Display (derived)"]
        AF["ActionsFor(context)"] --> GRP["config.GroupActions()<br/>(pure aggregator)"]
        GRP -->|"primary keys + GroupLabel"| PANE["KeybindsPane<br/>one segment per group<br/>cap counts segments"]
        GRP -->|"all keys incl. aliases"| MODAL["Help modal ?<br/>grouped, full keys"]
    end

    subgraph DISPATCH["Dispatch (never raw)"]
        K["KeyPressMsg"] --> RES["Resolve(key, context)"]
        M["Mouse wheel / double-click"] -->|"semantic action ID"| AID["component action-ID dispatch"]
        R --> RES
        R --> AID
        RES -->|"actionID"| H{"Owner"}
        H -->|"app"| HA["appActions() map"]
        H -->|"editor / grid /<br/>explorer / …"| HC["component handler"]
    end

    PANE -.->|"same key that runs"| RES
    MODAL -.->|"same registry field"| GRP
```

## Grouping behavior

```mermaid
stateDiagram-v2
    [*] --> Collect
    Collect --> Filter: ActionsFor(context)
    Filter --> SkipCond: drop rollback/autocomplete per state
    SkipCond --> Group: GroupActions()
    Group --> Multi: group with >=2 active members
    Group --> Single: group with 1 active member
    Group --> None: group with 0 active members
    Multi --> Segment: combined primary keys + GroupLabel
    Single --> Plain: plain action segment
    None --> Omitted: not rendered
```

## Bypass closure (Feature 2)

```mermaid
flowchart LR
    subgraph BEFORE["Before (bypasses)"]
        B1["editor: raw ctrl+p/ctrl+n/tab"]
        B2["tree: raw j/k/g/G/enter/backspace"]
        B3["mouse: raw j/k/enter injected"]
        B4["grid: raw 0-9 page jump"]
        B5["modal: raw ?/q/esc"]
    end
    subgraph AFTER["After"]
        A1["editor resolves ContextEditor"]
        A2["tree raw switch deleted"]
        A3["mouse -> action ID dispatch"]
        A4["0-9 removed (F-keys only)"]
        A5["documented overlay exception"]
    end
    B1 --> A1
    B2 --> A2
    B3 --> A3
    B4 --> A4
    B5 --> A5
```

- Pane: primary keys only; modal: every key (aliases included).
- `hjkl`, `g/G`, `ctrl+u/ctrl+d`, `f1-f9` collapse into single segments.
- Text-entry keys (B) remain widget-local and never resolve to an action.
