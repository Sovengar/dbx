---
feature: 0004-fix-dedup-dbx-toml
freshness: 7b50ea5db31a4206de4d73d43ec200a57d781e6d
codegraph: not_initialized
generated_by: codebase-researcher
---

# Context: `.dbx.toml` — panic fix, determinismo y deduplicación

Freshness: commit `7b50ea5` (branch `fix/dedup-dbx-toml`).
codegraph: not_initialized (sin `.codegraph/`; usar este mapa).

Este archivo es la **única entrada de navegación** del executor. No hace falta
glob/grep: los símbolos, líneas y rutas están abajo. Todo el cambio vive en
`internal/config` + tests; no se toca `internal/app` ni el CLI.

## Scope

- **In**: `internal/config/scanner.go` (panic fix, selección determinista, seam
  inyectable, dedup por repo+nombre+DSN, orden estable) y
  `internal/config/scanner_test.go` (nuevo).
- **Out**: exponer todas las conexiones en el picker; `internal/cli/commands.go`
  `findLocalDSN`; root fijo `~/dev` de `internal/app/app.go`; unificar
  `fd --hidden` vs `WalkDir`; migrar estado activo de paths descartados.

## Files to Touch

| Symbol / Area | File | Lines | Why |
|---------------|------|-------|-----|
| `Scanner` struct | internal/config/scanner.go | 10-12 | agregar campo seam `findFiles func(root string) []string` (nil → default fd/WalkDir) |
| `NewScanner` | internal/config/scanner.go | 14-16 | constructor (probablemente sin cambios; el seam se setea en tests o vía opción) |
| `Scan` | internal/config/scanner.go | 18-32 | punto único: descubrir paths → load → dedup → orden `(Path,Name)` → aplicar estado |
| `scanWithFD` | internal/config/scanner.go | 34-54 | refactor: sólo *producir paths* (`fd --hidden --type f .dbx.toml root`); quitar load/append |
| `scanWithWalkDir` | internal/config/scanner.go | 56-77 | refactor: sólo *producir paths*; conservar el `SkipDir` de dirs ocultos |
| `loadProject` | internal/config/scanner.go | 79-96 | **bug crítico**: `return nil, nil` en L95. Devolver `errNoConnections` (centinela) cuando el map está vacío; selección determinista de conexión |
| nuevo: `errNoConnections` | internal/config/scanner.go | — | `var errNoConnections = errors.New(...)`; los callers ya filtran por `err == nil` |
| nuevo: dedup + identidad git | internal/config/scanner.go | — | helpers: `gitRepoIdentity(dir) string`, canonicalización, clave de dedup |
| tests | internal/config/scanner_test.go | nuevo | cubre escenarios de `behavior.feature` con fixtures en `t.TempDir()` |

## Contracts

- `FoundProject{Name, Path, Connection, Active}` — `internal/config/project.go:20-25`.
  Es el contrato de salida de `Scan()`; mantener 1 por archivo.
- `ProjectConnection{Driver, DSN, SSHTunnel}` + `GetDSN()` —
  `internal/config/project.go:14-18` y `:51-53`. **Dedup compara por
  `GetDSN()`** (resuelto), no por `.DSN` crudo.
- `expandEnv(s)` — `internal/config/project.go:41-49`: soporta `${env:VAR}`
  (si el env existe) y cae a `os.ExpandEnv`. No tocar.
- `ProjectConfig.Connections map[string]ProjectConnection` +
  `LoadProjectConfig(path)` — `internal/config/project.go:10-12`, `:27-39`.
  Ya devuelve `*ProjectConfig` no-nil si el TOML parsea; con TOML sin
  `[connections]` el map queda `nil`/vacío → caso del panic.
- `ProjectState` — `internal/config/project_state.go`: `LoadProjectState()`
  L22-44, `IsActive(path)` L62-64, `SetActive` L67-69, `SetInactive` L72-74,
  `Toggle` L77-84, `ProjectStateFile()` L16-18. **El estado es por `Path`**; se
  aplica al path del superviviente.
- Consumidores (no modificar, pero condicionan el contrato):
  - `internal/app/app.go:196-197` — `config.NewScanner(rootDir); scanner.Scan()`.
  - `picker.SetProjects(projects)` `internal/ui/components/picker/picker.go:40-43`
    → el picker **respeta el orden del slice**; de ahí la necesidad del orden
    estable por `(Path, Name)`.

## Pattern to Follow

- **Error centinela en vez de `(nil,nil)`**: definir `errNoConnections` a nivel
  package y devolverlo; los callers existentes (`scanner.go:48` y `:66`) ya
  hacen `if p, err := s.loadProject(path); err == nil` → saltean el archivo sin
  desreferenciar. No agregar logger (el paquete `config` no lo tiene; archivo sin
  conexiones es config válida, se ignora en silencio).
- **Selección determinista**: en `loadProject`, juntar los nombres de
  `cfg.Connections` en un slice, `sort.Strings(...)`, tomar `[0]`. No iterar el
  map con `range` para elegir.
- **Seam de descubrimiento inyectable**: `findFiles func(root string) []string`;
  si es `nil`, `Scan` usa el comportamiento actual (`scanWithFD`, y si vacío,
  `scanWithWalkDir`). En tests se setea el campo directo (mismo package) o se
  agrega un setter/opción — preferir campo exportable-no o setter interno para
  no romper `NewScanner`.
- **Identidad git sin subproceso**: desde `filepath.Dir(path)` buscar `.git`
  hacia arriba; si es **directorio** → ese es el common dir; si es **archivo**
  con `gitdir: <X>` → leer `<X>/commondir` (relativo a X; si no existe, usar X).
  Canonicalizar con `filepath.Abs` + `filepath.Clean` + `filepath.EvalSymlinks`
  (si falla, usar el path limpio). Sin `.git` hacia arriba → identidad = path
  propio (no deduplica con nada).
- **Superviviente estable**: agrupar candidatos por
  `(gitCommonDir) + name + GetDSN()`, ordenar por path más corto y luego
  lexicográfico, conservar el primero.
- **Orden de salida**: `sort.Slice` final por `(Path, Name)`.

## Tests

- **Nuevo archivo**: `internal/config/scanner_test.go`, `package config`
  (mismo package para acceder a helpers/seam no exportados).
- **Runner**: `go test ./...`; en CI/Makefile `make test` =
  `go test -race -cover ./...` (`Makefile:test`). No hay Cucumber ni teatest en
  este paquete.
- **Infra**: unit only. Fixtures reales en `t.TempDir()`:
  - `.dbx.toml` escritos con `os.WriteFile` (TOML mínimo: `[connections.main]` +
    `driver`/`dsn`).
  - git simulado: `.git` **directorio**; worktree con `.git` **archivo**
    (`gitdir: <main>/.git/worktrees/wt`) y `<main>/.git/worktrees/wt/commondir`
    conteniendo la ruta del common dir.
  - no-git: dirs sin `.git`.
  - DSN con env: setear env (`t.Setenv`) y comparar `${env:DBX_DSN}` vs valor.
  - Inyectar `findFiles` para devolver los paths en orden **arbitrario** y
    verificar orden estable y no dependencia de `fd`.
- **Estilo de referencia**: `internal/config/keybindings_test.go` — `package
  config`, `testing` stdlib, `t.Fatalf` con want/got.
- **Mapeo escenarios → tests**:
  - (a) sin conexiones no paniquea / sólo sin-conexiones → resultado vacío.
  - (b) multi-conexión `zeta`+`alpha` → siempre `alpha`, repetido N veces.
  - (c) mismo repo+nombre+DSN (main+worktree) → colapsa, sobrevive el path más
    corto (repo principal).
  - (d) mismo repo+nombre, DSN distinto → 2 entradas.
  - (e) no-git con mismo nombre+DSN → 2 entradas (no deduplica).
  - (f) orden de salida estable entre dos scans.
  - (g) `${env:DBX_DSN}` vs valor expandido → colapsa.
  - (h) toggle del superviviente persiste y el siguiente scan lo refleja.
- **Tests existentes afectados**: ninguno en `internal/config` depende del
  scanner (`keybindings_test.go` no lo toca). `internal/app` no tiene tests del
  scan. No se esperan roturas; igual correr `go test ./...`.

## Conventions & Boundaries

- **Sin dependencias nuevas**: todo con stdlib (`os`, `path/filepath`, `sort`,
  `strings`, `errors`). No usar `git` por subproceso.
- El cambio es **interno al paquete `config`**; no exportar la clave de dedup ni
  los helpers de identidad salvo que haga falta. No cambiar la firma de
  `Scan() []FoundProject` (la TUI la consume tal cual).
- `NewScanner(rootDir string)` debe seguir funcionando sin cambios para
  `internal/app/app.go:196`.
- Comentarios/identificadores en inglés; prosa en español.
- No borrar ni degradar logging de debug existente (no hay en `scanner.go`; si se
  agrega, usar `/tmp/dbx_*.log`).

## Integration Points (non-obvious)

- **Root hardcodeado**: `internal/app/app.go:193-194` fija `rootDir = home + "/dev"`.
  Fuera de alcance; el scanner sólo recibe `RootDir`.
- **Dedup ANTES de aplicar estado**: `Scan` (L26-29) marca `Active` con
  `state.IsActive(results[i].Path)`. El dedup debe correr antes, para que el
  estado se aplique al path del superviviente. El estado huérfano de un path
  descartado queda en `project_state.json` pero es inocuo (no se muestra). Si el
  descartado era el activo/inactivo y el superviviente tiene otro estado, **gobierna
  el del superviviente**.
- **Auto-connect depende de `Active`** (`app.go:1121-1130`): si `activeCount == 1
  && !hasInactive` conecta directo. El dedup puede reducir el conteo y cambiar
  este camino; es el comportamiento deseado.
- **`fd --hidden` vs `WalkDir`**: `scanWithFD` incluye dirs ocultos; el fallback
  `WalkDir` los saltea (`scanner.go:70-72`). Divergencia conocida y aceptada
  (out-of-scope); no unificar, sólo documentar.
- **Orden del picker**: `Picker.View` (`picker.go:114-193`) recorre
  `p.projects` en orden; sin el sort final el picker cambia entre corridas.
- **`projectsScannedMsg` vacío** (`app.go:1103-1108`): si el scan devuelve 0
  proyectos, la app va a `StateError` con "no .dbx.toml files found in ~/dev".
  Escenario "sólo archivos sin conexiones" debe devolver slice vacío (no panic).

## Risks / Assumptions

- **Canonicalización de symlinks**: `EvalSymlinks` puede fallar (permisos/path
  inexistente); fallback a path limpio sin resolver. Preferible un dedup
  conservador a un panic.
- **Detección de repo en subdirs**: buscar `.git` hacia arriba hace que un
  `.dbx.toml` en un subdir comparta identidad con la raíz del repo. Esperado; se
  documenta.
- **Worktree real vs fixture**: el formato `gitdir:`/`commondir` debe respetar
  rutas relativas a X (no al cwd); testear con el fixture antes de confiar.
- **Map de `Connections` nil**: con TOML sin `[connections]` el map puede venir
  nil; `len()` sobre nil es 0, pero igual devolver `errNoConnections`.
- **`fd` no instalado**: hoy `scanWithFD` devuelve nil y cae a `WalkDir`; el
  refactor debe preservar ese fallback cuando el seam es nil.
- **Plan factible contra el código actual**: verificado; los anclajes del issue
  (`loadProject` L79-96, callers L48/L66) coinciden con `scanner.go` actual.
