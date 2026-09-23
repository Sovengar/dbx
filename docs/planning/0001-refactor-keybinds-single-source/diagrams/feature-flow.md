# Feature flow — Keybinds single source (display + dispatch)

Derivado de `behavior.feature`: la misma entrada alimenta las dos rutas.

```mermaid
flowchart TD
    R[("Registry unificado<br/>Action{ID, Keys[], Section,<br/>Description, Contexts, Owner, Pending}")]

    subgraph DISPLAY["Ruta de display"]
        R --> AF["ActionsFor(context)"]
        AF --> PANE["KeybindsPane<br/>(solo acciones de la vista actual)"]
        AF --> MODAL["Modal ?<br/>(todas las secciones)"]
    end

    subgraph DISPATCH["Ruta de dispatch"]
        K["KeyPressMsg"] --> RES["Resolve(key, context)"]
        R --> RES
        RES -->|"actionID"| H{"¿Owner?"}
        H -->|"app"| HA["tabla app:<br/>map[ActionID]handler"]
        H -->|"grid / explorer /<br/>gridpreview / explorerpreview"| HC["handler del componente"]
        H -->|"Pending"| HP["sin handler (ask)"]
    end

    PANE -.->|"misma tecla que ejecuta"| RES

    subgraph VIEWS["Contextos (Modelo A)"]
        V1["explorer"]
        V2["grid"]
        V3["grid-preview"]
        V4["explorer-preview"]
        V5["editor"]
    end
    V1 & V2 & V3 & V4 & V5 -.-> AF
```

## Casos clave del comportamiento

```mermaid
stateDiagram-v2
    [*] --> Vista
    Vista --> Explorer: foco
    Vista --> Grid: foco
    Vista --> Editor: editorOpen

    state Explorer {
        [*] --> navE
        navE: q cierra · ? ayuda · : palette
        navE: / filter · n new · d drop · v DDL
    }
    state Grid {
        [*] --> navG
        navG: q cierra · Ctrl+Y NO aplica
        navG: j/k · n/p · Ctrl+S · u · D
    }
    state Editor {
        [*] --> editE
        editE: q = carácter literal (NO cierra)
        editE: Ctrl+Enter ejecuta · Ctrl+Y copy
    }
```

- `q` → presente en Explorer/Grid/GridPreview/ExplorerPreview; **ausente en Editor** (se inserta literal).
- `Ctrl+Y` (`copy_sql`) → solo en Editor; ausente en Grid.
- `ask` (`a`) → declarada con `Pending: true`; conserva tecla y metadata, sin handler de TUI.
- Sin rótulo "Global": el panel muestra únicamente `ActionsFor(vista actual)`.
