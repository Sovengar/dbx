# Summary: fix-dedup-dbx-toml

## Metadata
- **Completed:** 2026-09-23 23:10
- **Duration:** ~90 minutes (22:42 → 23:10)
- **Plan Number:** 0004
- **Branch:** `fix/dedup-dbx-toml` (base `main` @ `ce73c2c`)
- **ADR:** `docs/decisions/0002-dbx-toml-project-dedup-identity.md` (Accepted)
- **Commits:** 5 ahead of `main` (plan → impl → review fixes → docs → tests)

## Objetivo

El scan de proyectos de la TUI (`.dbx.toml`) debía dejar de crashear con un
archivo sin conexiones, elegir siempre la misma conexión cuando un archivo define
varias, y colapsar en una sola entrada las copias del mismo
repo+conexión+DSN (p. ej. worktrees), con un orden de salida estable.

## Resultado

- **Panic fix**: `loadProject` deja de devolver `(nil, nil)`; cuando no hay
  conexiones devuelve el centinela `errNoConnections`. Los callers ya filtraron
  por `err == nil`, así que el archivo se saltea sin desreferenciar nil. Se ignora
  **en silencio** (el paquete `config` no tiene logger; un archivo sin conexiones
  es configuración válida, no un error).
- **Selección determinista**: al elegir la conexión de un archivo se ordenan los
  nombres lexicográficamente y se toma el primero. Sigue habiendo **1
  `FoundProject` por archivo**.
- **Seam de descubrimiento inyectable**: `Scanner` gana `findFiles func(root)
  []string` (nil → `fd`, fallback `WalkDir`). `fd`/`WalkDir` solo *producen
  paths*; load, dedup y orden son lógica común y testeable sin `fd`.
- **Dedup en `Scanner.Scan()`**: colapsa por clave
  `(identidad de repo git, nombre de conexión, driver, DSN resuelto, ssh_tunnel)`.
  - **Identidad de repo** sin invocar `git`: desde el dir del `.dbx.toml` se sube
    buscando `.git`, **acotado a `RootDir` (inclusive)** — nunca se inspeccionan
    ancestros del root. `.git` directorio → common dir (checkout principal);
    `.git` archivo (`gitdir:`) → `<X>/commondir` (fallback `X`), cubriendo
    worktrees. Canonicalización abs+clean+symlinks con fallback al path limpio.
    Sin `.git` hasta el root (no-git) → identidad = path propio, así **no-git no
    deduplica**.
  - **Superviviente**: primero el **checkout principal** (`.git` directorio),
    aunque el worktree tenga path más corto; empate o sin principal → path más
    corto, luego lexicográfico. Independiente del estado activo → reproducible.
  - **DSN resuelto**: `GetDSN()` expande `${env:...}`; se compara el valor
    resuelto.
- **Orden de salida estable**: `Scan()` ordena por `(Path, Name)`.
- **Estado activo/inactivo**: el dedup corre *antes* del marcado; el estado se
  aplica al path del superviviente. El estado huérfano de un path descartado
  queda en `project_state.json` pero es inocuo (documentado, no migrado).
- **Sin dependencias nuevas**: solo stdlib (`os`, `path/filepath`, `sort`,
  `strings`).

## Escenarios (behavior.feature) — 11/11

| Scenario | Status | Test(s) |
|----------|--------|---------|
| A `.dbx.toml` without connections does not crash | ✅ Passed | `TestScanner_NoConnectionsDoesNotPanicAndSkips` |
| Only files without connections exist | ✅ Passed | `TestScanner_OnlyNoConnectionsIsEmpty` |
| A file with several connections always picks the same one | ✅ Passed | `TestScanner_MultipleConnectionsPicksLexicographicallyFirst` |
| Same repo, same name and same DSN collapse to one entry | ✅ Passed | `TestScanner_DedupSameRepoAndConnection` |
| Same repo and name but different resolved DSN are both kept | ✅ Passed | `TestScanner_SameRepoDifferentDSNKept` |
| Different connection name in the same repo does not collapse | ✅ Passed | `TestScanner_DifferentConnectionNameKept` |
| Git worktrees share one repo identity | ✅ Passed | `TestScanner_WorktreeCollapsesToMainRepo`, `TestScanner_MainRepoSurvivesEvenWhenWorktreePathIsShorter` |
| Directories that are not a git repo do not deduplicate | ✅ Passed | `TestScanner_NonGitDoesNotDeduplicate` |
| DSN compared after environment expansion | ✅ Passed | `TestScanner_DSNComparedAfterEnvExpansion` |
| Scan output order is stable | ✅ Passed | `TestScanner_OutputOrderIsStable` |
| Toggling state still works after dedup | ✅ Passed | `TestScanner_StateAppliesAfterDedup` |

**Tests extra (más allá de los escenarios):** `TestScanner_DifferentReposDoNotDeduplicate`
(dos repos git con mismo nombre+DSN no colapsan), `TestScanner_DifferentDriverKept`
y `TestScanner_DifferentSSHTunnelKept` (clave ampliada en review),
`TestScanner_WalkDoesNotEscapeRoot` y `TestFindDotGit_DoesNotEscapeRoot` y
`TestScanner_ProjectOutsideRootNotAbsorbed` (walk acotado al root).

## Cambios clave por módulo

| Módulo | Cambio |
|--------|--------|
| `internal/config/scanner.go` | Centinela `errNoConnections`; seam `findFiles`; selección lexicográfica; `dedupeProjects`/`survivor`/`isBetterSurvivor`; orden final `(Path, Name)`. |
| `internal/config/repoid.go` (nuevo) | Identidad de repo git por parseo: `gitRepoIdentity`, `findDotGit`, `pathWithin`, `readGitdirFile`, `readCommonDir`, `canonicalPath`. |
| `internal/config/scanner_test.go` | 18 tests con fixtures git reales en `t.TempDir()` (`.git` dir y `.git` file + `worktrees/<n>/commondir`). |
| `docs/decisions/0002-dbx-toml-project-dedup-identity.md` | ADR (Accepted). |
| `docs/planning/0004-fix-dedup-dbx-toml/*` | issue/plan/context/behavior.feature/diagrams. |

## Decisiones

- **Parseo de `.git` en vez de `git rev-parse`**: hermético, sin subproceso ni
  dependencia de `git` en PATH, determinista y testeable con fixtures.
- **Identidad = common git dir** (no el worktree gitdir): es lo que comparten main
  y sus worktrees.
- **Walk de `.git` acotado al scan root** (aprobado en review): evita que un repo
  que solo *contiene* el root (dotfiles `$HOME`, `~/dev`) absorba proyectos no-git.
- **Superviviente = checkout principal, luego path**: sobrevive el repo main
  aunque el worktree tenga path más corto, y no depende de la sesión.
- **Clave de dedup ampliada** a `(repo, nombre, driver, DSN, ssh_tunnel)` (aprobado
  en review): no colapsar conexiones que difieren en driver o túnel.
- **No-git no deduplica**: clave = path único.
- **Dedup en `Scanner.Scan()`**: único punto de consumo de la TUI; el CLI queda
  fuera de alcance.

## Verificación (evidencia)

```
go build ./...                      ✅
go vet ./...                        ✅
go test -race ./internal/config/... ✅  ok github.com/buble/dbx/internal/config 1.029s
go test ./...                       ✅  (todos los paquetes ok; internal/app 2.929s)
make install                        ✅  (binario desplegado en ~/.local/bin/dbx)
```

## Commits

- `cfa94b1` chore(planning): add fix-dedup-dbx-toml plan
- `9328012` fix(config): harden .dbx.toml scan against crashes and duplicates
- `5b66e20` fix(config): keep main checkout, bound git walk, widen dedup key
- `e1e8929` docs: align dedup behavior, plan and ADR with review decisions
- `a5bb5eb` test(config): cover driver/ssh_tunnel dedup key; harden root bound

## Files

- **Created**: `internal/config/repoid.go`,
  `docs/decisions/0002-dbx-toml-project-dedup-identity.md`,
  `docs/planning/0004-fix-dedup-dbx-toml/{issue,plan,context,behavior.feature,diagrams/*}`.
- **Modified**: `internal/config/scanner.go`, `internal/config/scanner_test.go`.

## Tests

- **Added:** 18 tests en `internal/config/scanner_test.go` (panic, determinismo,
  dedup, worktrees, DSN, driver/tunnel, walk acotado, orden, estado).
- **System Tests:** ✅ Passed — `go build/vet/test -race` verde; no se requieren
  contenedores (fixtures de filesystem en `t.TempDir()`).

## Documentation

- **Changelog:** ❌ No se actualizó — el repo **no mantiene `CHANGELOG.md`**
  (verificado: no existe en el repo). No se inventó uno.
- **Docs:** `plan.md` y `behavior.feature` alineados con las decisiones de review
  (`e1e8929`); ADR `0002`.
- **ADR:** ✅ Creado (`docs/decisions/0002-dbx-toml-project-dedup-identity.md`,
  Accepted).

## Code Review Issues

Hallazgos resueltos durante la implementación (reconstruidos desde `5b66e20` y
`a5bb5eb`):

- **H1 — Superviviente por path más corto podía elegir el worktree**: un linked
  worktree con path más corto le ganaba al main. Resuelto: superviviente = checkout
  principal (`.git` directorio) primero.
- **H2 — Walk de `.git` sin límite absorbía proyectos no-git**: un repo que solo
  contiene el root (dotfiles `$HOME`, `~/dev`) daba identidad compartida a todos
  los no-git. Resuelto: walk acotado a `RootDir` inclusive (+ tests de escape).
- **M1 — Clave de dedup incompleta**: conexiones que difieren solo en driver o
  ssh_tunnel colapsaban. Resuelto: clave ampliada a
  `(repo, nombre, driver, DSN, ssh_tunnel)` + `TestScanner_DifferentDriverKept`/
  `_DifferentSSHTunnelKept`.
- **Critical Found:** 0 pendientes
- **High Found:** 0 pendientes
- **User Decision:** aprobado continuar y cerrar

## Tradeoffs / limitaciones conocidas

- **Symlink policy del superviviente**: la canonicalización resuelve symlinks
  (`EvalSymlinks`) y cae al path limpio si falla; no hay política explícita de
  elegir el path real vs el simbólico (follow-up).
- **`findLocalDSN` del CLI sigue no-determinista**: itera un map de conexiones
  directo (`LoadProjectConfig`), fuera del scanner y del alcance del plan.
- **Divergencia `fd --hidden` vs `WalkDir`**: `fd` escanea dirs ocultos, el
  fallback `WalkDir` los saltea; se documenta, no se unifica (out-of-scope).
- **Root fijo `~/dev`** del scan de la TUI: sin cambio (out-of-scope).
- **Estado activo del path descartado**: queda huérfano e inerte en
  `project_state.json`; si el superviviente tiene otro estado, gobierna el del
  superviviente.

## Convención de cierre

El repo **no archiva** las carpetas de planificación en `docs/planning/archived/`:
los cierres previos (`0001-refactor-keybinds-single-source`,
`0003-feature-ask-nl2sql-chat`) agregaron `summary.md` **in place** dentro de la
carpeta del plan. Este cierre sigue la misma convención (no se crea `archived/`,
ni `.ignore` ni guard `AGENTS.md`). No hay CHANGELOG en el repo.

## Next Step

Pushear `fix/dedup-dbx-toml` a origin y abrir PR → `main`. **Pendiente de
autorización explícita del usuario.**
