# Process flow — plan `keybinds-single-source`

Flujo de planeación específico de este cambio (slug real, decisiones tomadas).

```mermaid
flowchart TD
    S["idea-refiner → issue.md<br/>10 puntos de alcance"] --> C1{{"⏸️ CHECKPOINT issue"}}
    C1 -->|"aprobado + 2 correcciones:<br/>ask no es fantasma · flag Pending"| BR["brainstormer + architect<br/>(inline)"]
    BR --> BF["behavior.feature<br/>19 escenarios"]
    BF --> C2{{"⏸️ CHECKPOINT behavior"}}
    C2 -->|aprobado| P["plan.md"]
    P --> C3{{"⏸️ CHECKPOINT plan"}}

    P -.-> D1["Decisión central: Modelo A vs B"]
    D1 --> D1a["Modelo A (action declara Contexts) ✅ recomendado"]
    D1 --> D1b["Modelo B (view declara actions) ❌ reintroduce 2da fuente"]

    P -.-> D2["adr_required: TRUE<br/>título: keybind-registry-single-source"]
    P -.-> D3["Riesgos: blast radius constructores ·<br/>regresión dispatch grid (~30 casos) ·<br/>agregación HandledActions()"]
    P -.-> D4["Scope cut: teclas de MODO (EDIT/FILTER/WHERE,<br/>scroll modal, picker) NO se promueven al registry"]

    C3 -->|aprobado| CTX["context.md<br/>(símbolo/línea por archivo)"]
    CTX --> DG["diagrams/ (feature-flow + process-flow)"]
    DG --> CMT["commit: chore: add keybinds-single-source plan"]
    CMT --> EX["swe-executor → make install"]
```

## Decisiones registradas
- **Modelo A** elegido: una sola lista plana de acciones; `Contexts` inline; sin duplicación de IDs.
- **Dispatch table-driven** en `app` + `Resolve` en componentes; display desde `ActionsFor`.
- **`Pending bool`** para `ask` (evita falso positivo en el test acción↔handler).
- **Docs**: borrar `KEYBINDS.md`, limpiar README, crear `FEATURES.md`, actualizar checklist `AGENTS.md`.
