# Issue — `.dbx.toml`: panic fix, selección determinista y deduplicación

- **Slug**: `fix-dedup-dbx-toml`
- **Tipo**: fix + feature (scanner de proyectos)
- **Branch**: `fix/dedup-dbx-toml`
- **Nivel**: PIPELINE (fix de crash + feature de dedup en un mismo módulo)

## Contexto / problema

El escaneo de proyectos de la TUI (`internal/config/scanner.go`) tiene tres
defectos, dos de ellos de robustez y uno de comportamiento:

### 1. Panic con `.dbx.toml` sin conexiones (bug crítico)

`loadProject` (L79-96) devuelve `(nil, nil)` cuando `cfg.Connections` está
vacío (L95: `return nil, nil`). Los callers hacen `*p` confiando en
`err == nil`:

- `scanWithFD` (L48-50): `if p, err := s.loadProject(line); err == nil { projects = append(projects, *p) }`
- `scanWithWalkDir` (L66-68): idéntico.

Resultado: **nil pointer dereference**. Un solo `.dbx.toml` sin `[connections]`
rompe TODO el scan de la TUI, no solo ese proyecto — el usuario queda sin
picker y sin poder conectarse a ningún proyecto.

### 2. Selección de conexión no-determinista

`loadProject` itera `cfg.Connections` (un `map`) y hace `return` en la primera
iteración (L87-93). Go itera maps en orden aleatorio → si un `.dbx.toml` define
varias conexiones, cuál se muestra **cambia entre ejecuciones**. Hoy solo se
expone 1 `FoundProject` por archivo, pero la elección no es reproducible.

### 3. Duplicados por worktrees (feature faltante)

No existe ningún dedup. Con N worktrees que copian el mismo `.dbx.toml`
aparecen N entradas idénticas en el picker: mismo nombre, mismo
`driver:port`, distinto `Path`. Ruido y riesgo de conectar al path equivocado.

## Regla de deduplicación aprobada

Deduplicar dos entradas cuando tienen **(mismo repo git) + (mismo nombre de
conexión) + (mismo DSN resuelto)**; dejar solo 1. Si el nombre coincide pero el
DSN difiere (p.ej. un worktree apuntando a otro puerto), **conservar ambas**.

Se mantiene **1 `FoundProject` por archivo** — no se exponen todas las
conexiones del `.dbx.toml` en el picker (fuera de alcance).

## Resultado esperado

- Un `.dbx.toml` sin conexiones **no crashea**: el archivo se saltea y el scan
  continúa con el resto.
- Con múltiples conexiones en un archivo, la mostrada es **siempre la misma**
  (orden lexicográfico del nombre; se toma la primera).
- Entradas que representan el mismo repo + conexión (mismo nombre + mismo DSN
  resuelto) **colapsan en una**; las que difieren en DSN **se mantienen**.
- El orden de salida del scan es **estable** (independiente del orden de `fd`
  o de `WalkDir`).
- Tests nuevos en `internal/config` que cubren (a) panic fix, (b) determinismo
  multi-conexión, (c) dedup colapsa, (d) mismo nombre distinto DSN no colapsa,
  (e) no-git; sin depender del binario `fd`.

## Alcance (in-scope)

1. **Fix del panic**: `loadProject` deja de devolver `(nil, nil)`; los callers
   nunca desreferencian un nil. Archivos sin conexiones se saltean.
2. **Determinismo**: selección estable de la conexión mostrada (lexicográfica).
3. **Dedup** dentro de `Scanner.Scan()` con clave
   `(identidad de repo git) + nombre + DSN resuelto`, superviviente estable
   (path más corto, luego lexicográfico) y orden de salida estable.
4. **Identidad de repo git** resuelta sin depender de que `git` esté en PATH
   (parseo de `.git` / `commondir`), con comportamiento definido para dirs
   no-git (no deduplican).
5. **Tests** de scanner (`internal/config/scanner_test.go`) con un seam
   inyectable para no depender de `fd`.

## Fuera de alcance (out-of-scope)

- Exponer todas las conexiones de un `.dbx.toml` en el picker (sigue 1 por
  archivo).
- El CLI (`internal/cli/commands.go`): `findLocalDSN` usa `LoadProjectConfig`
  directo, no el scanner; no sufre el panic (map vacío → DSN `""`). Su
  iteración de map también es no-determinista, pero queda fuera.
- El root fijo `~/dev` del scan de la TUI (`internal/app/app.go` L193-194).
- Divergencia `fd --hidden` (incluye dirs ocultos) vs `WalkDir` (los saltea):
  se documenta, no se unifica.
- Migrar el estado activo/inactivo por path de entradas descartadas.
