Status: approved

# Export formats: Markdown, delimitadores personalizados y compresión

- **Slug**: `feature-export-formats`
- **Tipo**: feature (formatos de exportación del grid)
- **Branch**: `feature/export-formats`
- **Nivel**: PIPELINE (feature sobre el picker de export existente; no es refactor ni fix)
- **Cross-reference**: tsk task #31 — "Export formats: Markdown, custom delimiters,
  compression" (`in_progress`).

## Contexto / problema

El spec aprobado `docs/PLAN.md` §5 "Export formats" marca el ítem como
**partial**. Hoy la exportación desde el grid cubre **SQL, JSON y CSV**, hacia
portapapeles o fichero, pero le faltan tres capacidades que el usuario necesita:

1. **Markdown**: no hay forma de exportar las filas como tabla Markdown, que es
   el formato natural para pegar resultados en un issue, un PR, un chat o una
   nota.
2. **Delimitadores personalizados**: el formato delimitado se fija a coma. No se
   puede pedir `;`, tabulador o `|`, que es lo que suelen exigir hojas de
   cálculo locales, exports para Europa y pegado en tablas de texto.
3. **Compresión**: las exportaciones a fichero se escriben sin comprimir. En
   resultados grandes el fichero queda innecesariamente voluminoso y hay que
   comprimirlo a mano (p. ej. `gzip`).

El código relevante, verificado:

- `internal/ui/components/grid/export_picker.go` — enum `ExportFormat`
  (`ExportSQL`/`ExportJSON`/`ExportCSV`), `exportOption` con
  `label`/`description`/`format`, `updateDescriptions()` con tres ramas
  (`yankMode`, `fileMode`, y el `else` de portapapeles), y el
  `ExportSelectedMsg` que viaja al handler. Las etiquetas (`SQL`/`JSON`/`CSV`)
  y sus descripciones están escritas como literales por formato.
- `internal/app/app.go` — `handleExport` (~L2719) y el case
  `grid.ExportSelectedMsg` (~L1669). Decide: fila única → portapapeles;
  multi-fila → portapapeles si `YankMode`, si no fichero. Los exporters
  `exportAsSQL/JSON/CSV` y `exportRowAsSQL/JSON/CSV`, más `usableWidth`/
  `rowWidth`. La generación de nombre de fichero fija las extensiones
  `.sql`/`.json`/`.csv`.
- `internal/ui/components/grid/table.go` — `startExport()` (L1049) elige modo
  fichero vs. portapapeles según selección; `startYank()` usa `yankMaxRows`
  (L1392-1396). `yankMaxRows` es configurable.
- `internal/config/config.go` — `UIConfig` (incluye
  `YankMaxRows mapstructure:"yank_max_rows"`) y `setDefaults` (L187-206, p. ej.
  `v.SetDefault("ui.yank_max_rows", 10)`). **No existe hoy ninguna clave de
  configuración para delimitador ni compresión.**
- `internal/cli` — no expone la exportación del grid. Sus únicos usos de
  "export" son `dbx context` (schema para LLMs). El CLI debe seguir funcionando
  igual.

Regla de repo (`AGENTS.md`): **todo cambio de feature o visible al usuario**
actualiza `docs/FEATURES.md` **y** `~/.agents/skills/dbx/SKILL.md` en el mismo
cambio. El inventario relevante está en `docs/FEATURES.md` (~L165-175, entrada
del grid que hoy dice "SQL/JSON/CSV") y en el skill global, donde las
descripciones de keybinds y formatos forman el contrato de runtime. Además, la
descripción del keybind `export` en
`internal/config/keybindings_actions.go` (`{ID: "export", Keys: ["x"],
Description: "Export (SQL/JSON/CSV)"}`) menciona los formatos actuales y
probablemente deba reflejar los nuevos.

## Resultado esperado

- El picker de exportación del grid (`x`) ofrece una opción de **Markdown**,
  además de SQL/JSON/CSV, tanto en modo portapapeles como en modo fichero.
- El formato delimitado (hoy CSV) acepta un **delimitador elegido por el
  usuario** (`;`, tabulador, `|`, etc.), y la salida generada respeta ese
  delimitador.
- Las exportaciones a **fichero** pueden **comprimirse** (p. ej. gzip) con una
  ruta de código y un nombre de fichero coherentes con el formato elegido.
- La lógica de exportación sigue siendo determinista y testeable (funciones
  puras), y la exportación a portapapeles conserva su comportamiento actual.
- La exportación del grid sigue siendo el único punto de entrada: **no aparecen
  subcomandos nuevos en el CLI** y los formatos existentes **no se rediseñan**.
- `docs/FEATURES.md` y `~/.agents/skills/dbx/SKILL.md` quedan actualizados en el
  mismo cambio, junto con la descripción del keybind `export` si su etiqueta se
  amplía.

## Alcance (in-scope)

1. **Markdown**: nuevo formato de exportación que produce una tabla Markdown a
   partir de las filas y columnas, con su entrada en el picker.
2. **Delimitadores personalizados**: parametrizar el formato delimitado para
   aceptar un delimitador elegido por el usuario; la salida y su nombre/extension
   deben ser coherentes.
3. **Compresión**: comprimir la salida cuando la exportación va a fichero,
   incluyendo el manejo del nombre de fichero y de los errores de escritura.
4. **Integración en la superficie del grid**: registrar el nuevo formato en
   `ExportFormat`, en las opciones del picker (con sus tres ramas de
   descripción), en el `ExportSelectedMsg` y en las ramas de `handleExport`
   (fila única y multi-fila), más la generación de nombre de fichero.
5. **Tests**: cobertura de las funciones de exportación nuevas y de las ramas
   tocadas, sin depender de una base de datos ni de la TUI (mismo estilo que
   `internal/app/export_contract_test.go` y
   `internal/ui/components/grid/export_picker_test.go`).
6. **Documentación**: actualizar `docs/FEATURES.md`,
   `~/.agents/skills/dbx/SKILL.md` y, si aplica, `docs/CONFIG.md`/`README.md`.

## Fuera de alcance (out-of-scope)

- **Nuevos subcomandos del CLI**: la exportación vive en el grid y en su picker;
  `internal/cli` no crece. `dbx context` (schema export) no se toca.
- **Rediseño de los formatos existentes**: SQL, JSON y CSV conservan su
  comportamiento y su semántica actuales; solo se parametriza el delimitado.
- Nuevos formatos más allá de Markdown (XML, Excel, Parquet, etc.).
- Rediseño del flujo de selección/portapapeles (`startExport`, `startYank`,
  `yankMaxRows`) o de la política "fila única → portapapeles".
- Cambios estructurales en el router, paneles o keybinds ajenos a `export`.

## Preguntas abiertas para el plan (no se resuelven aquí)

- **Dónde se configuran** el delimitador y la compresión: ¿clave de
  `internal/config` (p. ej. bajo `ui.`), interacción en el propio picker, flag,
  o combinación? El spec no lo decide.
- **Formato del delimitador**: cómo se representa `tab` (carácter literal,
  `\t`, nombre simbólico).
- **Nombre de fichero comprimido**: convención de extensión (p. ej.
  `tabla.csv.gz`) y si la compresión aplica a todos los formatos o solo a los
  delimitado/tabular.
- **Ubicación de la compresión** en el pipeline (al serializar o al escribir) y
  cómo se refleja en la firma de los exporters.

## Restricciones de aceptación (gates del proyecto)

- **Cobertura**: toda línea tocada debe quedar al 100% (`scripts/diff-coverage.sh`
  contra el merge-base; el total contra `scripts/coverage-floor`, que está en
  `100.00`).
- **Lint**: `make lint` en verde (gofmt + golangci-lint v2.13.2).
- **Mutación**: los mutantes del diff deben medirse con `scripts/mutate.sh`;
  cualquier superviviente va a `.mutation-allowlist` y cualquier cuelgue a
  `.mutation-timeouts`, o se mata con un test.
- **Verificación y despliegue**: `go build ./... && go vet ./... && go test ./...`
  antes de cerrar, y `make install` al terminar (el usuario ejecuta
  `~/.local/bin/dbx`).
- **Docs en el mismo cambio**: `docs/FEATURES.md` y
  `~/.agents/skills/dbx/SKILL.md` (regla de `AGENTS.md`).
