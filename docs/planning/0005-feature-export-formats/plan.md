---
adr_required: true
adr_title: export-formats-config-and-pipeline
adr_reason: >
  Hay una decisión de diseño con alternativas descartadas: dónde viven el
  delimitador y la compresión (claves de configuración vs. un prompt dentro
  del picker), dónde se corta la compresión (dentro de cada exporter vs. en el
  borde de escritura de handleExport) y las convenciones de extensión del
  delimitado y del fichero comprimido. Modela la superficie de exportación y es
  difícil de cambiar después sin rehacer las firmas de los exporters.
---

# Plan: Export formats — Markdown, delimitadores personalizados y compresión

## Resultado esperado

El picker de exportación del grid (`x`) gana una opción **Markdown** que produce
una tabla; el formato delimitado respeta un **delimitador elegido por el
usuario**; y las exportaciones a **fichero** se pueden **comprimir** (gzip). Los
formatos existentes (SQL, JSON, CSV con coma) conservan byte a byte su salida
actual por defecto. No aparecen subcomandos nuevos en el CLI ni se rediseña el
picker más allá de añadir la opción nueva.

## Enfoque (alto nivel)

1. **Configuración, no picker.** El delimitador y la compresión son
   preferencias, no una elección por exportación. Van a `internal/config` bajo
   `ui`, junto a `ui.yank_max_rows` (que ya gobierna el flujo de export):
   - `ui.export_delimiter` — por defecto `","`.
   - `ui.export_compress` — por defecto `false`.
   Así el comportamiento actual no cambia y la superficie del picker no crece
   con un modal nuevo. Se añaden a `UIConfig`, a `setDefaults` y a
   `docs/CONFIG.md`.

2. **Markdown como formato nuevo, sin tocar los viejos.** Se añade
   `ExportMarkdown` al **final** del enum (los valores numéricos actuales no se
   mueven) y una cuarta `exportOption`. `exportAsMarkdown` /
   `exportRowAsMarkdown` espejan a los de CSV y reutilizan `usableWidth` /
   `rowWidth`. Formato de tabla: cabecera, separador y una fila por registro.
   Se escapa `\` primero, luego `|` como `\|` y los saltos de línea como
   `<br>`; un `NULL` se renderiza como celda vacía. Extensión `.md`.

3. **Delimitador normalizado a `rune`.** Una función pura traduce el valor de
   configuración a un `rune`: un carácter literal, o los alias `tab` / `\t`. Un
   valor vacío, multicarácter o prohibido (salto de línea) cae a `,` sin romper
   la app (documentado). Los exporters del delimitado ganan un parámetro
   `rune` que se asigna a `csv.Writer.Comma`; **no** se reescribe el quoting a
   mano. Extensión coherente: tabulador → `.tsv`, cualquier otro → `.csv`.

4. **Compresión en el borde de escritura, no en los exporters.** Los exporters
   siguen devolviendo `string` puro. Se añade una función pura de gzip sobre
   `[]byte` y `handleExport` se vuelve orquestación fina: serializar →
   (portapapeles | nombre de fichero → comprimir → escribir). Nada de writers
   en streaming. El portapapeles y el yank **nunca** se comprimen.

5. **Nombres de fichero centralizados.** Una función pura decide la extensión
   (`.sql`/`.json`/`.csv`/`.tsv`/`.md`) y añade `.gz` cuando la compresión está
   activa, de modo que el nombre y el contenido no puedan divergir.

6. **Sin cambios en el CLI ni en `startExport`/`startYank`/`yankMaxRows`.** La
   política de selección ("fila única → portapapeles") no se toca.

7. **Documentación en el mismo cambio** (regla de `AGENTS.md`):
   `docs/FEATURES.md`, `docs/CONFIG.md`, la descripción del keybind `export`
   (`internal/config/keybindings_actions.go`) y
   `~/.agents/skills/dbx/SKILL.md`.

## Decisiones clave

- **Config vs. prompt en el picker**: se elige config. Un segundo paso en el
  picker es una máquina de estados nueva, más superficie de test y más riesgo de
  rediseño; el spec pide mantener la superficie actual. Se descarta el prompt
  interactivo.
- **Claves bajo `ui`** (`ui.export_delimiter`, `ui.export_compress`), no una
  sección `[export]` nueva: `ui.yank_max_rows` ya vive ahí y el grid es una
  superficie de UI. Evita dos fronteras de config para el mismo componente.
- **Delimitador como `rune`** en las firmas: `encoding/csv` solo acepta un rune;
  normalizar en config deja a los exporters con un tipo que no admite valores
  inválidos.
- **Fallback a la coma** ante un delimitador inválido en vez de abortar: una
  preferencia mal escrita no debe romper la exportación (coherente con
  `theme.mode`, que también cae a un valor seguro).
- **Compresión pura en memoria** (no streaming): el buffer de bytes no puede
  fallar al escribir, así que los errores internos del writer de gzip se
  ignoran igual que hoy se ignoran los del `csv.Writer` sobre `strings.Builder`;
  el único error real es el `os.WriteFile`, que ya se reporta.
- **Compresión uniforme para todos los formatos de fichero** (SQL, JSON, CSV,
  Markdown), no una lista por formato.
- **Markdown al final del enum**: mantiene estables los valores numéricos
  existentes y evita reinterpretar mensajes en vuelo.
- **Sin dependencias nuevas**: `compress/gzip`, `bytes` y `encoding/csv` de la
  stdlib.

## Riesgos

- **`updateDescriptions()` indexa `options[0..2]` por posición.** Con una cuarta
  opción, las etiquetas/descripciones se desalinean en silencio. Mitigación:
  derivar la descripción del `format`, no del índice (y cubrirlo con test).
- **Cobertura y mutación del gzip.** El buffer en memoria hace inalcanzables
  algunos errores. Mitigación: función pura sin ramas de error internas; un test
  descomprime y compara con el original, de modo que quitar `Write`/`Close` mate
  al mutante.
- **Colisiones de nombres de fichero.** `os.WriteFile` sobrescribe: repetir una
  exportación pisa `schema_table.csv`, y `.gz` puede pisar un `.csv` real. Es el
  comportamiento actual; se documenta, no se cambia (fuera de alcance resolver
  colisiones).
- **Tabla Markdown inválida con filas irregulares.** Si cabecera, separador y
  filas no comparten ancho, la tabla no renderiza. Mitigación: `usableWidth`
  aplicado a las tres partes.
- **`m.config` puede faltar en tests.** Lectura nil-safe con defaults
  (`","` / `false`) para no romper los fixtures que construyen el modelo sin
  configuración.

## Orden de trabajo (grueso)

1. Config: `UIConfig`, `setDefaults`, `docs/CONFIG.md`; normalizador de
   delimitador (puro) + tests.
2. Markdown: enum, opción en el picker con descripciones derivadas del formato,
   exporters + tests.
3. Delimitado: parámetro `rune` en los exporters existentes, quoting y
   extensión `.tsv` para tabulador + tests.
4. Compresión y nombres: función pura de gzip, helper de nombre de fichero, y
   cableado en `handleExport` (fichero sí, portapapeles/yank no) + tests.
5. Docs en el mismo cambio: `docs/FEATURES.md`,
   `internal/config/keybindings_actions.go` (descripción del keybind `export`) y
   `~/.agents/skills/dbx/SKILL.md`.
6. Verificación y despliegue.

## Estrategia de test (ATDD/BDD, tests reales — no Cucumber)

Los escenarios de `behavior.feature` se traducen a tests Go en
`internal/app` (`export_contract_test.go`, `handle_export_contract_test.go`) y
`internal/ui/components/grid/export_picker_test.go`, sin base de datos ni TUI:

- Markdown: estructura de tabla para fila única y multi-fila, escapado de `|`,
  saltos de línea y `NULL`, y ancho constante con filas irregulares.
- Delimitado: cada delimitador permitido, tabulador como byte real, quoting de
  un valor que contiene el delimitador, extensión `.tsv`, y fallback a coma con
  valor inválido.
- Compresión: round-trip gzip (descomprimir y comparar con lo no comprimido),
  sufijo `.gz`, todos los formatos, y que portapapeles/yank salen sin comprimir.
- Compatibilidad: con config por defecto, SQL/JSON/CSV mantienen su salida y sus
  extensiones actuales.

Cobertura: toda línea tocada al 100% (`scripts/diff-coverage.sh` contra el
merge-base; total contra `scripts/coverage-floor` = `100.00`). Mutación:
`scripts/mutate.sh` sobre el diff; supervivientes a `.mutation-allowlist` o
muertos con test; cuelgues a `.mutation-timeouts`.

## Verificación final

`go build ./... && go vet ./... && go test ./...`, `make lint` y
`make mutate-diff`; al terminar, `make install` (el usuario ejecuta
`~/.local/bin/dbx`). Commits atómicos en Conventional Commits sobre
`feature/export-formats`.

## Progress
Phase: execution
| Scenario (behavior.feature) | Status | Commit |
| --- | --- | --- |
| Markdown export of a single row goes to the clipboard as a table | ⬜ | |
| Markdown export of several rows writes a .md table file | ⬜ | |
| Markdown escapes pipes and newlines and renders NULL as an empty cell | ⬜ | |
| Markdown truncates ragged rows so the table stays valid | ⬜ | |
| The delimited export uses the configured delimiter | ⬜ | |
| A tab delimiter writes real tab bytes and a .tsv file | ⬜ | |
| A value containing the delimiter is quoted | ⬜ | |
| An unknown or empty delimiter falls back to a comma | ⬜ | |
| Compression writes a valid gzip file with a .gz name | ⬜ | |
| Compression is off by default and leaves files untouched | ⬜ | |
| Compression applies to every file format | ⬜ | |
| Clipboard and yank exports are never compressed | ⬜ | |
| The existing SQL, JSON and CSV defaults are unchanged | ⬜ | |
