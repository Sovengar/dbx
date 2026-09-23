# Issue — Keybinds: una sola fuente de verdad (display + dispatch)

- **Slug**: `keybinds-single-source`
- **Tipo**: refactor
- **Branch**: `refactor/keybinds-single-source`
- **Nivel**: PIPELINE (refactor multi-módulo con cambio de contratos internos)

## Contexto / problema

Hoy la información de keybinds vive en **tres fuentes paralelas** que hay que mantener a mano y que se desincronizan:

1. **Registry** (`internal/config/keybindings.go`): `DefaultKeybindings()` (`map[string]string`, tecla primaria) y `defaultBindings()` (`map[string][]string`, todas las teclas) **duplican la misma información**.
2. **Display**: la barra inferior (`internal/ui/statusbar.go` → `renderActions()` y `renderContextual()`), el modal `?` (`internal/ui/modal.go`), el palette (`internal/ui/components/palette/commands.go`) y las tablas del `README.md` / `docs/KEYBINDS.md` mantienen listas, labels y **strings con teclas crudas** hardcodeados.
3. **Dispatch**: `internal/app/app.go` compara `m.keybinds["<action>"]` directamente y despacha con un `switch action` hardcodeado en `handlePaletteCommand`. El `Match()` del registry **no se usa en producción** (solo en tests).

Consecuencia: agregar o mover una tecla obliga a tocar hasta 8 lugares (ver checklist en `AGENTS.md`) y es fácil dejar uno atrás. Ejemplo real: `global.ask` ("a") está declarado en el registry pero **no tiene handler de TUI ni aparece en las superficies** — es una acción con **handler pendiente** (la feature Ask AI/NL→SQL ya existe como comando CLI en `internal/cli/ask.go` + `internal/ai/nl2sql/`; lo que falta es el handler de TUI). No es basura a borrar: hay que preservarla con metadata correcta.

Además existe el concepto de **"keybind global"**: `inContext()` hace fallback de `global.*` a **todas** las vistas. Eso hace que acciones globales se ejecuten en vistas donde la tecla significa otra cosa (ej. `q` es "quit" global pero debería ser un carácter literal en el editor). El modelo "global + override" es justamente lo que produce colisiones invisibles.

## Resultado esperado

- La **única fuente de verdad** es la tabla acción→keybind en el código, con metadata (sección, descripción, vista(s)).
- **Display y dispatch se alimentan de la misma entrada**: no hay listas de display paralelas a los handlers.
- **Desaparece el concepto de "keybind global"**: cada vista declara sus propias keybinds activas; el panel muestra solo las de la vista actual.
- La **referencia viva es la propia TUI** (modal `?` + panel de keybinds). `docs/KEYBINDS.md` se elimina; el README deja solo un puntero ("press `?`").
- Tests que **previenen la regresión**: toda acción tiene sección+descripción, no hay colisiones de tecla dentro de una vista, y hay cobertura acción↔handler.

## Alcance (in-scope)

1. **Unificar el registry** en una estructura por acción (ej. `Action{ID, Keys[], Section, Description, Contexts/…}`). Eliminar `DefaultKeybindings()` vs `defaultBindings()`.
2. **Fuente única real (display + dispatch)**: la misma entrada alimenta la barra de keybinds, el modal `?` y la ejecución.
3. **Eliminar "keybind global"**: cada vista declara sus acciones; las hoy globales (`q`, `?`, `:`, `c`, `Q`, `a`, `x`, `U`, `e`, `E`) se re-declaran por vista donde tengan sentido y **desaparecen** donde no (ej. `q` NO en editor).
4. **Renombrar `StatusBar` → `KeybindsPane`** y que muestre solo las acciones de la vista actual.
5. **Completar el modal `?`** para cubrir lo que hoy vive en `KEYBINDS.md`: modos EDIT/FILTER/WHERE, mouse actions, picker de conexión, query browser interno, etc.
6. **Panel contextual deriva del registry** (sin strings con teclas crudas).
7. **Borrar `docs/KEYBINDS.md`** y limpiar las tablas de keybinds del README (dejar puntero a `?`).
8. **Crear `docs/FEATURES.md`** con la prosa que no son keybinds (DML Transactions, Action Naming Convention, etc.).
9. **Actualizar el checklist de `AGENTS.md`** (hoy 7 lugares) → "tocás el registry + su metadata".
10. **Tests**: ajustar los existentes; agregar (a) toda acción tiene sección+descripción, (b) detección de colisiones de tecla por vista, (c) cobertura acción↔handler.

## Fuera de alcance (out-of-scope)

- Cambiar la semántica de teclas existentes (el refactor preserva el comportamiento actual, salvo las eliminaciones explícitas por vista).
- Custom keybinds de usuario vía config (`KeybindingsConfig.Custom`): se mantiene el mecanismo, adaptado al nuevo modelo.
- **Implementar el handler de TUI de "Ask AI (NL→SQL)"** (`global.ask` / tecla `a`) y el prompt NL→SQL en la TUI. El refactor **preserva** el binding `a` con su metadata correcta ("Ask AI (NL→SQL)") y lo modela como acción **con handler pendiente**; la implementación del handler queda para otra tarea.

## Decisión abierta a resolver en el plan

Cómo modelar la pertenencia **acción↔vista** sin copiar teclas a mano:
- **Modelo A**: la acción declara sus contextos/vistas (`Contexts []string`).
- **Modelo B**: la vista declara su lista de acciones.

Se evalúan ambos con el código en mano (ergonomía, validación de colisiones, duplicación, testabilidad) y se recomienda uno en el checkpoint del plan.

Y cómo modelar acciones **declaradas con handler intencionalmente pendiente** (caso `global.ask`), para que el test de cobertura acción↔handler (req. 10c) no dé falso positivo. Candidato: un campo `Pending bool` (o equivalente) en la metadata de la acción, excluido de la exigencia de handler.
