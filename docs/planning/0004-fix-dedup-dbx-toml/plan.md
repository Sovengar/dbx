---
adr_required: true
adr_title: dbx-toml-project-dedup-identity
adr_reason: >
  Decisión de arquitectura con alternativas descartadas: cómo derivar la
  identidad de repo git sin depender del binario `git` (parseo de .git/commondir
  vs `git rev-parse`), qué política de superviviente usar (path vs estado activo)
  y dónde vive el dedup (Scanner vs caller). Modela la identidad de un proyecto
  y es difícil de cambiar sin rehacer la clave de dedup.
---

# Plan: `.dbx.toml` — panic fix, determinismo y deduplicación

## Resultado esperado

El scan de proyectos de la TUI nunca crashea por un `.dbx.toml` sin conexiones,
elige siempre la misma conexión cuando un archivo define varias, y colapsa en
una sola entrada las copias del mismo repo+conexión+DSN (p.ej. worktrees). El
orden de salida es estable.

## Enfoque (alto nivel)

1. **Fix del panic (mínimo y explícito)**. `loadProject` deja de devolver
   `(nil, nil)`: cuando no hay conexiones devuelve un error centinela
   (`errNoConnections`). Los callers ya filtran por `err == nil`, así que el
   archivo se saltea sin desreferenciar nil. Se decide **ignorar en silencio**
   (no log): el paquete `config` no tiene logger y un archivo sin conexiones es
   una configuración válida, no un error a gritar.

2. **Selección determinista**. Al elegir la conexión de un archivo, ordenar los
   nombres de conexión lexicográficamente y tomar el primero. Se mantiene **1
   `FoundProject` por archivo**.

3. **Seam de descubrimiento inyectable**. `Scanner` gana un campo interno
   (`findFiles func(root) []string`, nil → comportamiento actual: `fd` y
   fallback `WalkDir`). El scan pasa a operar sobre esa lista de paths, lo que
   hace `Scan()` testeable sin depender de `fd` ni del orden de `fd`. Se
   refactoriza para que `fd`/`WalkDir` solo *produzcan paths*, y toda la lógica
   (load, dedup, orden) sea común.

4. **Dedup dentro de `Scanner.Scan()`** (así lo consume la TUI; el CLI no usa el
   scanner). Clave de identidad por entrada:
   `(identidad de repo git) + nombre de conexión + driver + DSN resuelto + ssh tunnel`.
   - **DSN resuelto**: `ProjectConnection.GetDSN()` (expande `${env:...}`). Se
     deduplica por el valor resuelto, no por el crudo.
   - **Identidad de repo git sin `git` en PATH**: resolver por parseo. Dado el
     directorio del `.dbx.toml`, buscar hacia arriba un `.git`, **acotando el walk
     a `Scanner.RootDir` (inclusive)**: nunca se inspeccionan ancestros del root.
     Esto evita que un repo que sólo *contiene* el root (p.ej. `$HOME` con
     yadm/dotfiles, o `~/dev`) absorba proyectos no-git y rompa "no-git no
     deduplica".
      - `.git` **directorio** → ese es el common dir (checkout principal).
      - `.git` **archivo** (`gitdir: <X>`) → leer `<X>/commondir` (relativo a X)
        para obtener el common dir; si no existe, usar X. Esto cubre worktrees
        (el worktree comparte el common dir del repo principal).
      - Canonicalizar (abs + clean + symlinks) para que main y worktree coincidan.
      - **Sin `.git` hacia arriba (hasta el root, no-git)** → la identidad es el
        propio path del proyecto; como es único, no deduplica con nada (ni con
        otro no-git).
   - **Superviviente estable**: **primero el checkout principal** (aquel cuyo
     `.git` resuelto es un **directorio**; las linked worktrees tienen `.git`
     archivo). Empate, o ninguna principal → path más corto, luego lexicográfico.
     Así sobrevive el repo main aunque el worktree tenga un path más corto. No
     depende del estado activo, así que el resultado es reproducible.

5. **Orden de salida estable**. Al final de `Scan()`, ordenar los resultados por
   `(Path, Name)`. Así el picker es reproducible aunque `fd` devuelva paths en
   orden arbitrario.

6. **Estado activo/inactivo**. El dedup corre **antes** de marcar activo, y el
   estado se aplica al path del superviviente (el estado ya es por path). El
   estado huérfano de un path descartado queda en `project_state.json` pero es
   inocuo: no se muestra ni afecta a nadie. **Decisión: aceptable, se documenta**
   (no se migra el toggle). Gotcha: si el path descartado era el activo/inactivo
   y el superviviente tiene otro estado, gobierna el del superviviente.

## Decisiones clave

- **Parseo de `.git` en vez de `git rev-parse`**: sin subproceso, sin depender
  de `git` en PATH, determinista y testeable con fixtures. Se descarta ejecutar
  git (fallback innecesario y no hermético).
- **Identidad de repo = common git dir**, no el worktree gitdir: es lo que
  comparten main y sus worktrees.
- **No-git no deduplica**: clave = path único. Evita colapsar proyectos
  homónimos legítimos fuera de git. La búsqueda de `.git` se acota a
  `Scanner.RootDir` para que un repo que sólo contiene el root no los absorba.
- **Superviviente por checkout principal (`.git` directorio), luego path
  (corto→lexicográfico)**, no por estado activo: sobrevive el repo main aunque el
  worktree tenga un path más corto, y el resultado es independiente de la
  sesión/estado del usuario. El estado activo del descartado se sacrifica
  (documentado).
- **Clave de dedup completa**: `(repo, nombre, driver, DSN resuelto, ssh_tunnel)`,
  para no colapsar conexiones que difieren en driver o tunnel.
- **Dedup en `Scanner.Scan()`**: punto único de consumo de la TUI; el CLI queda
  fuera de alcance.
- **Sin dependencias nuevas**: todo con stdlib (`os`, `path/filepath`, `sort`,
  `strings`).

## Riesgos

- **Detección de repo en subdirectorios**: buscar `.git` hacia arriba (acotado al
  root) hace que un `.dbx.toml` en un subdir del repo comparta identidad con la
  raíz. Es lo esperable; se documenta. El acotamiento evita además falsos
  positivos por repos que sólo contienen el root.
- **Fallback fd→WalkDir**: el fallback se dispara por *proyectos cargados*, no
  por paths crudos, para preservar el comportamiento previo cuando `fd` devuelve
  sólo archivos inválidos.
- **Symlinks/permisos**: canonicalizar puede fallar; fallback al path limpio sin
  resolver symlinks (mejor un dedup conservador que un panic).
- **Estado activo del descartado**: ver arriba (aceptado).
- **Divergencia `fd --hidden` vs `WalkDir`**: el fallback saltea dirs ocultos; no
  se unifica (out-of-scope), se documenta en `context.md`.

## Orden de trabajo (grueso)

1. Fix del panic + error centinela + determinismo de conexión.
2. Seam de descubrimiento inyectable y refactor de `Scan()`.
3. Identidad de repo git (parseo) + dedup + superviviente + orden estable.
4. Tests de scanner (panic, determinismo, dedup, DSN distinto, no-git, orden).
5. `go build ./... && go vet ./... && go test ./...` + `make install`.

## Estrategia de test (ATDD/BDD, tests reales — no Cucumber)

Los escenarios de `behavior.feature` se traducen a tests Go en
`internal/config/scanner_test.go`, usando el seam inyectable (nunca el binario
`fd`):

- Fixtures en `t.TempDir()` con archivos `.dbx.toml` reales y estructura git
  simulada: `.git` directorio, y `.git` archivo + `worktrees/<n>/commondir` para
  worktrees.
- (a) archivo sin conexiones no paniquea y se saltea; (b) archivo multi-conexión
  elige el nombre lexicográficamente primero, repetido N veces; (c) dedup colapsa
  main+worktree y sobrevive el main aunque su path sea más largo; (d) mismo
  nombre, DSN distinto → 2 entradas; (e) dirs no-git no deduplican y el walk
  acotado no escapa el root; (f) orden de salida estable; (g) DSN con `${env:...}`
  se compara resuelto; (h) dos repos git distintos con mismo nombre+DSN no
  colapsan; (i) toggle del superviviente persiste.

## Verificación final

`go build ./... && go vet ./... && go test ./...` y luego `make install` (el
usuario corre `~/.local/bin/dbx`). No tocar `main`; commits atómicos
Conventional Commits en `fix/dedup-dbx-toml`.
