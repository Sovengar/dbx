# dbx - Database x

> **Database x** — Herramienta TUI moderna para bases de datos con integración IA nativa.
> x = advanced, modern tool (siguiendo convención: rgx, fdx, cdx)

## Filosofía del Proyecto

**dbx** nace de la frustración con herramientas como lazysql que tienen:
- Keybinds poco intuitivos (solo Vim, sin discoverability)
- Diseño visual anticuado (funcional pero feo)
- Cero integración con IA (no hay CLI para agentes, no hay NL→SQL)
- Sin soporte mouse (todo por teclado)

**Nuestra visión**: Una herramienta que sea:
1. **Hermosa** — Temas modernos, lipgloss v2, animaciones
2. **Usable** — Keybinds intuitivos (tecla inicial = acción), command palette
3. **AI-native** — Session logs, NL→SQL, CLI para agents
4. **Completa** — Mouse + teclado, multi-panel, multi-db

## Stack Tecnológico

| Componente | Tecnología | Versión | Por qué |
|------------|-----------|---------|---------|
| **Runtime** | Go | 1.22+ | Rendimiento, binario estático, ecosistema |
| **TUI** | Bubbletea | v2.0.8 | The Elm Architecture, declarative views, mouse nativo |
| **Styling** | Lipgloss | v2.0.0-beta.2 | CSS-like para terminal, temas, responsive |
| **Components** | Bubbles | latest | Text input, table, viewport, spinner |
| **Mouse** | BubbleZone | latest | Zero-width zones para click tracking |
| **Animations** | Harmonica | latest | Spring physics para transiciones suaves |
| **PostgreSQL** | pgx | v5 | Performance, COPY protocol, 100% Go |
| **CLI** | Cobra | latest | Comandos CLI estándar |
| **Config** | Viper | latest | TOML, env vars, flags |
| **LLM** | Multi-provider | - | Anthropic, OpenAI, DeepSeek, Qwen |

## Arquitectura

```
dbx/
├── main.go                      # Entry point
├── go.mod
├── Makefile                     # build, test, lint
├── .goreleaser.yaml             # Release automation
│
├── cmd/dbx/
│   └── main.go                  # CLI entry (cobra)
│
├── internal/
│   ├── app/
│   │   ├── app.go               # App lifecycle, Init/Update/View
│   │   ├── router.go            # Focus manager, pane switching
│   │   └── messages.go          # Custom tea.Msg types
│   │
│   ├── config/
│   │   ├── config.go            # Viper config loader
│   │   ├── keybindings.go       # Keybind registry + defaults
│   │   ├── keybindings_vim.go   # Vim mode defaults
│   │   ├── keybindings_modern.go# Modern mode defaults
│   │   └── defaults.go          # All default values
│   │
│   ├── theme/
│   │   ├── theme.go             # Theme interface + resolver
│   │   ├── system.go            # Auto-detect terminal colors
│   │   ├── builtin.go           # Built-in themes (dark, light, nord...)
│   │   ├── palette.go           # Gray scale generation
│   │   └── styles.go            # Lipgloss style definitions
│   │
│   ├── ui/
│   │   ├── app.go               # Main Bubbletea Model
│   │   ├── zones.go             # BubbleZone manager
│   │   ├── statusbar.go         # Bottom contextual bar
│   │   ├── modal.go             # Help modal overlay
│   │   ├── toast.go             # Notifications
│   │   └── components/
│   │       ├── explorer/
│   │       │   ├── tree.go      # Schema tree component
│   │       │   ├── node.go      # Node types (db, schema, table, column)
│   │       │   ├── render.go    # Tree rendering with icons
│   │       │   └── mouse.go     # Click handlers
│   │       ├── grid/
│   │       │   ├── table.go     # Data grid component
│   │       │   ├── cell.go      # Cell rendering + inline edit
│   │       │   ├── header.go    # Column headers + sort indicators
│   │       │   ├── pager.go     # Pagination
│   │       │   └── mouse.go     # Click to edit, scroll, header click
│   │       ├── editor/
│   │       │   ├── sql.go       # SQL editor component
│   │       │   ├── completion.go# Schema-aware autocomplete
│   │       │   └── highlight.go # Syntax highlighting
│   │       └── palette/
│   │           ├── command.go   # Command palette component
│   │           ├── fuzzy.go     # Fuzzy search algorithm
│   │           └── recent.go    # Recent commands
│   │
│   ├── drivers/
│   │   └── postgres/
│   │       ├── driver.go        # Driver interface impl
│   │       ├── schema.go        # Query information_schema
│   │       ├── query.go         # Execute queries
│   │       ├── copy.go          # COPY protocol for bulk ops
│   │       ├── monitor.go       # LISTEN/NOTIFY (future)
│   │       └── tunnel.go        # SSH tunnel support (future)
│   │
│   ├── ai/
│   │   ├── session/
│   │   │   ├── logger.go        # JSON Lines session logger
│   │   │   ├── reader.go        # Read/parse session logs
│   │   │   └── replay.go        # Replay a session
│   │   ├── nl2sql/
│   │   │   ├── provider.go      # Provider interface
│   │   │   ├── anthropic.go     # Claude provider
│   │   │   ├── openai.go        # GPT-4 provider
│   │   │   ├── deepseek.go      # DeepSeek provider
│   │   │   ├── qwen.go          # Qwen/Dashscope provider
│   │   │   └── prompt.go        # System prompts + context
│   │   └── context/
│   │       └── schema.go        # Export schema for LLMs
│   │
│   └── cli/
│       ├── root.go              # Cobra root command
│       ├── query.go             # dbx query "SQL" --json
│       ├── ask.go               # dbx ask "natural language"
│       ├── list.go              # dbx list tables/columns
│       ├── schema.go            # dbx schema table_name
│       ├── export.go            # dbx export table --format csv
│       ├── context.go           # dbx context --json
│       ├── pipe.go              # Pipe mode (stdin/stdout)
│       └── replay.go            # dbx replay session.log
│
├── pkg/
│   └── client/
│       └── client.go            # Go library for agents/skills
│
├── docs/
│   ├── PLAN.md                  # Este archivo
│   ├── ARCHITECTURE.md          # Decisiones de arquitectura
│   ├── KEYBINDS.md              # Reference de keybinds
│   ├── CONFIG.md                # Reference de config
│   └── CLI.md                   # Reference de CLI
│
└── test/
    ├── integration/
    └── fixtures/
```

## Config File Structure

Ubicación: `~/.config/dbx/config.toml`

```toml
# ═══════════════════════════════════════════════════════════════
# dbx Configuration
# ═══════════════════════════════════════════════════════════════

# ─── Theme ─────────────────────────────────────────────────────
[theme]
# Opciones: "system", "dark", "light", "nord", "gruvbox", "catppuccin"
# "system" auto-detecta los colores de tu terminal
mode = "system"

# Override de colores individuales (opcional)
# [theme.colors]
# primary = "#89b4fa"
# background = "#1e1e2e"

# ─── Keybindings ───────────────────────────────────────────────
# Custom keybind overrides (each action maps to a single key string)
[keybindings.custom]
# Global
"global.ask" = "a"
"global.query" = "ctrl+enter"
"global.export" = "e"
"global.refresh" = "r"
"global.help" = "?"
"global.palette" = ":"
"global.quit" = "q"

# Explorer
"explorer.expand" = ["enter", "l"]
"explorer.toggle_columns" = "space"
"explorer.collapse" = ["backspace", "h"]
"explorer.new_table" = "n"
"explorer.drop" = "d"
"explorer.view_ddl" = "v"
"explorer.filter" = "f"

# Grid
"grid.edit_cell" = ["enter", "i"]
"grid.delete_row" = "d"
"grid.insert_row" = "o"
"grid.yank" = "y"
"grid.sort" = "s"
"grid.filter" = "/"
"grid.next_page" = "n"
"grid.prev_page" = "p"

# Editor
"editor.execute" = ["ctrl+enter", "ctrl+r"]
"editor.clear" = "ctrl+u"
"editor.external" = "ctrl+g"
"editor.history_prev" = "ctrl+p"
"editor.history_next" = "ctrl+n"

# ─── Connections ───────────────────────────────────────────────
[[connections]]
name = "local-dev"
provider = "postgres"
host = "localhost"
port = 5432
database = "myapp_dev"
user = "postgres"
# password se lee de env var o keyring
password_env = "PGPASSWORD"

[[connections]]
name = "staging"
provider = "postgres"
url = "postgres://user:pass@staging.example.com:5432/myapp"
read_only = true

# ─── AI ────────────────────────────────────────────────────────
[ai]
# Provider por defecto (auto-detect si no especificado)
# Opciones: "anthropic", "openai", "deepseek", "qwen"
provider = "anthropic"

[ai.providers.anthropic]
api_key_env = "ANTHROPIC_API_KEY"
model = "claude-sonnet-4-20250514"

[ai.providers.openai]
api_key_env = "OPENAI_API_KEY"
model = "gpt-4o"

[ai.providers.deepseek]
api_key_env = "DEEPSEEK_API_KEY"
model = "deepseek-chat"

[ai.providers.qwen]
api_key_env = "DASHSCOPE_API_KEY"
model = "qwen-turbo"

# ─── Session Logs ──────────────────────────────────────────────
[session]
enabled = true
# Directorio para logs
dir = "~/.config/dbx/sessions"
# Retención de logs (días, 0 = infinito)
retention_days = 30

# ─── UI ────────────────────────────────────────────────────────
[ui]
# Mostrar statusbar contextual
statusbar = true
# Mostrar help en statusbar
statusbar_help = true
# Número de líneas de historial de query
history_size = 100
# Default page size para grids
page_size = 100
```

---

# Fases de Desarrollo

## Fase 0: Setup y Fundación
**Duración**: 1 día
**Objetivo**: Tener el repo, estructura base, y primer "hola mundo" con Bubbletea v2

### Contexto
Esta fase es puramente mecánica pero crítica. Establece:
- El repositorio donde todo el código vivirá
- La estructura de directorios que escalará
- Las dependencias correctas (Bubbletea v2 es nuevo, hay que usar las API correctas)
- CI/CD básico para catches tempranos

### Tareas

#### 0.1 Crear Repositorio
```bash
# Crear repo privado en GitHub
gh repo create buble/dbx --private \
  --description "Database x - Modern TUI database client with AI integration" \
  --clone

cd dbx
```

#### 0.2 Inicializar Go Module
```bash
go mod init github.com/buble/dbx
```

#### 0.3 Instalar Dependencias Core
```bash
# TUI Framework (v2 latest)
go get charm.land/bubbletea/v2@latest
go get github.com/charmbracelet/lipgloss/v2@latest
go get github.com/charmbracelet/bubbles@latest
go get github.com/charmbracelet/bubblezone@latest
go get github.com/charmbracelet/harmonica@latest

# Database
go get github.com/jackc/pgx/v5@latest

# CLI + Config
go get github.com/spf13/cobra@latest
go get github.com/spf13/viper@latest

# Utilities
go get golang.org/x/term@latest
```

#### 0.4 Crear Estructura de Directorios
```bash
mkdir -p cmd/dbx
mkdir -p internal/{app,config,theme}
mkdir -p internal/ui/components/{explorer,grid,editor,palette}
mkdir -p internal/drivers/postgres
mkdir -p internal/ai/{session,nl2sql,context}
mkdir -p internal/cli
mkdir -p pkg/client
mkdir -p docs
mkdir -p test/{integration,fixtures}
touch internal/.gitkeep
```

#### 0.5 Crear Makefile Básico
```makefile
.PHONY: build test lint run clean

VERSION := $(shell git describe --tags --always --dirty)
LDFLAGS := -ldflags "-X main.Version=$(VERSION)"

build:
	go build $(LDFLAGS) -o bin/dbx .

run:
	go run .

test:
	go test -race -cover ./...

lint:
	golangci-lint run

clean:
	rm -rf bin/
```

#### 0.6 Crear main.go Mínimo
```go
package main

import (
    "fmt"
    "os"
    
    tea "charm.land/bubbletea/v2"
)

func main() {
    p := tea.NewProgram(NewModel(), tea.WithAltScreen())
    if _, err := p.Run(); err != nil {
        fmt.Fprintf(os.Stderr, "Error: %v\n", err)
        os.Exit(1)
    }
}

type model struct{}

func NewModel() model { return model{} }

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    if msg, ok := msg.(tea.KeyPressMsg); ok {
        if msg.String() == "q" || msg.String() == "ctrl+c" {
            return m, tea.Quit
        }
    }
    return m, nil
}

func (m model) View() tea.View {
    v := tea.NewView("¡Hola dbx! Presiona 'q' para salir.\n")
    v.AltScreen = true
    return v
}
```

#### 0.7 Setup CI (.github/workflows/ci.yml)
```yaml
name: CI
on: [push, pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
      - run: go test -race ./...
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: golangci/golangci-lint-action@v4
```

#### 0.8 Primer Commit
```bash
git add .
git commit -m "feat: initial project setup with Bubbletea v2"
git push -u origin main
```

### Verificación
- [ ] `go build .` compila sin errores
- [ ] `./dbx` muestra "¡Hola dbx!" y se cierra con 'q'
- [ ] `go test ./...` pasa (aunque no haya tests aún)
- [ ] CI pasa en GitHub

---

## Fase 1: Core PostgreSQL
**Duración**: 4-5 semanas
**Objetivo**: TUI funcional para PostgreSQL con explorer, grid, editor, y mouse básico

### Contexto
Esta es la fase más crítica. Aquí construimos los cimientos de toda la aplicación:
- **App structure**: El loop principal de Bubbletea (Init/Update/View)
- **Theme system**: Auto-detectar colores del terminal y generar paleta
- **Componentes UI**: Los bloques de construcción reutilizables
- **Driver PostgreSQL**: La conexión real a la base de datos
- **Keybinds**: El sistema de registro y dispatch

La clave aquí es **iterar rápido pero con calidad**. Cada sprint debe producir algo visible y testeable.

### Sprint 1: Foundation (1 semana)

#### Contexto
Establecemos la estructura base de la aplicación, el theme system, y el manejo de config. Al final de este sprint, tendremos una TUI que se adapta a los colores del terminal y carga config desde disco.

#### Tareas
1. **App structure** (`internal/app/app.go`)
   - Model principal que coordina todos los componentes
   - Message routing entre panes
   - Focus management (qué pane tiene el foco)
   - Alt screen + mouse mode

2. **Theme system** (`internal/theme/`)
   - `system.go`: Detectar background/foreground del terminal
   - `palette.go`: Generar gray scale desde background
   - `builtin.go`: Themes fijos (dark, light, nord, gruvbox, catppuccin)
   - `styles.go`: Estilos Lipgloss reutilizables
   - Config: `[theme]` section

3. **BubbleZone setup** (`internal/ui/zones.go`)
   - Manager global de zonas
   - Helper para marcar zonas en el render
   - Helpers para manejar clicks

4. **Config loader** (`internal/config/`)
   - `config.go`: Viper loader con defaults
   - `defaults.go`: Todos los valores por defecto
   - `keybindings.go`: Estructura base del registry
   - Validación de config

5. **Router/Focus** (`internal/app/router.go`)
   - Enum de panes (Explorer, Grid, Editor)
   - Cycle focus con Tab
   - Direct focus con teclas (1, 2, 3 o e, g, d)

#### Verificación
- [ ] App carga y muestra layout de 3 panes
- [ ] Theme se adapta al terminal (probar en dark y light)
- [ ] Tab cycle funciona entre panes
- [ ] Config se carga desde ~/.config/dbx/config.toml

---

### Sprint 2: Explorer (1 semana)

#### Contexto
El explorer es la columna vertebral de la navegación. Muestra la jerarquía: Database → Schemas → Tables → Columns. El usuario debe poder:
- Navegar con teclado (flechas, vim keys)
- Hacer click para expandir/colapsar
- Hacer click en una tabla para cargar sus datos
- Filtrar tablas con fuzzy search

#### Estructura del Tree
```
📁 mydb
  📁 public
    📄 users (1,234 rows)
    📄 orders (56,789 rows)
    📄 products (89 rows)
  📁 auth
    📄 tokens (456 rows)
    📄 sessions (78 rows)
```

#### Tareas
1. **Node types** (`internal/ui/components/explorer/node.go`)
   ```go
   type NodeType int
   const (
       NodeDatabase NodeType = iota
       NodeSchema
       NodeTable
       NodeColumn
       NodeIndex
       NodeConstraint
   )
   
   type Node struct {
       ID       string
       Type     NodeType
       Name     string
       Parent   *Node
       Children []*Node
       Expanded bool
       Metadata map[string]interface{} // row_count, etc.
   }
   ```

2. **Tree component** (`tree.go`)
   - Renderizado recursivo con indentación
   - Iconos por tipo (📁 📄 📊 🔑)
   - Highlight del nodo seleccionado
   - Smooth scroll con Harmonica

3. **Mouse zones** (`mouse.go`)
   - Zona por cada nodo del árbol
   - Click izquierdo: toggle expand/collapse
   - Click en tabla: cargar datos en grid
   - Scroll wheel: navegar por el árbol
   - Hover: highlight sutil

4. **Data loading** (`internal/drivers/postgres/schema.go`)
   ```go
   func (d *Driver) ListSchemas(ctx context.Context) ([]Schema, error)
   func (d *Driver) ListTables(ctx context.Context, schema string) ([]Table, error)
   func (d *Driver) ListColumns(ctx context.Context, schema, table string) ([]Column, error)
   ```
   Queries a `information_schema`

5. **Fuzzy filter**
   - Tecla `/` activa filter input
   - Filtra tablas en tiempo real
   - Esc para limpiar filter

6. **Keyboard navigation**
   - `j/k` o `↑/↓`: Mover selección
   - `Enter` o `l`: Expandir / Cargar tabla
   - `Backspace` o `h`: Colapsar / Ir a padre
   - `g/G`: Primer/Último nodo
   - `r`: Refrescar árbol

#### Verificación
- [ ] Se conecta a PostgreSQL real
- [ ] Muestra schemas y tablas
- [ ] Click expande/collapse nodos
- [ ] Click en tabla carga datos (aunque grid esté vacío aún)
- [ ] Fuzzy filter funciona

---

### Sprint 3: Grid (1 semana)

#### Contexto
El grid muestra los datos de una tabla. Debe soportar:
- Miles de filas con scroll suave
- Edición de celdas con click
- Sort por columna con click en header
- Paginación

#### Tareas
1. **Table component** (`internal/ui/components/grid/table.go`)
   - Renderizado de tabla con headers
   - Columnas auto-sizing (o config)
   - Rows alternando colores (zebra)
   - Selected row highlight

2. **Cell rendering** (`cell.go`)
   - Formateo de tipos (NULL, fechas, JSON, etc.)
   - Truncado inteligente (no cortar UTF-8)
   - Click para iniciar edición inline

3. **Header + Sort** (`header.go`)
   - Click en header → sort ascending
   - Click de nuevo → sort descending
   - Click de nuevo → clear sort
   - Indicador visual (↑↓)

4. **Pagination** (`pager.go`)
   - Auto-paginación con page_size del config
   - Botones prev/next
   - Muestra "Page 1 of 10 (100 rows)"

5. **Mouse handlers** (`mouse.go`)
   - Click cell → start edit mode
   - Click header → sort
   - Scroll wheel → navegar filas
   - Click pager → cambiar página

6. **Data loading** (`internal/drivers/postgres/query.go`)
   ```go
   func (d *Driver) Select(ctx context.Context, table string, opts SelectOptions) (*Result, error)
   
   type SelectOptions struct {
       Schema    string
       Where     string
       OrderBy   string
       OrderDir  string // ASC, DESC
       Limit     int
       Offset    int
   }
   ```

7. **Keyboard navigation**
   - `j/k` o `↑/↓`: Mover entre filas
   - `h/l` o `←/→`: Mover entre columnas
   - `g/G`: Primera/Última fila
   - `Ctrl+U/Ctrl+D`: Half page up/down
   - `n/p`: Next/Previous page
   - `s`: Cycle sort en columna actual

#### Verificación
- [ ] Muestra datos de una tabla real
- [ ] Scroll funciona con mouse y teclado
- [ ] Sort funciona con click y tecla
- [ ] Paginación funciona
- [ ] Click en celda inicia edición (aunque no guarde aún)

---

### Sprint 4: Editor + CLI (1 semana)

#### Contexto
El SQL editor permite escribir queries custom. El CLI permite usar dbx desde scripts y agents. Este sprint conecta la UI con la ejecución real de queries.

#### Tareas
1. **SQL Editor** (`internal/ui/components/editor/sql.go`)
   - Multi-line text input
   - Syntax highlighting básico (keywords en color)
   - Tab para autocomplete (básico)
   - Ctrl+Enter para ejecutar

2. **Autocomplete** (`completion.go`)
   - Schema-aware (sabe tablas y columnas)
   - Se activa con Tab o mientras se escribe
   - Lista filtrada por prefix

3. **Query execution**
   - Ejecutar SQL desde editor
   - Mostrar resultados en grid
   - Manejar errores (mostrar toast)
   - Logging a session

4. **CLI commands** (`internal/cli/`)
   - `dbx query "SELECT..." --json`: Query directa
   - `dbx list tables --json`: Listar tablas
   - `dbx schema table_name --json`: Schema de tabla
   - Todos con `--connection` flag

5. **Session logger** (`internal/ai/session/logger.go`)
   - JSON Lines format
   - Log: timestamp, action, sql, duration, rows, error
   - Auto-rotate por día

#### Verificación
- [ ] Puedo escribir SQL y ejecutarlo con Ctrl+Enter
- [ ] Resultados aparecen en el grid
- [ ] Errores se muestran en toast
- [ ] `dbx query "SELECT 1" --json` funciona
- [ ] Session log se escribe

---

### Sprint 5: Polish (1 semana)

#### Contexto
Este sprint integra todo y añade los toques finales: statusbar contextual, command palette, help modal, y themes. El objetivo es que la app se sienta **completa y pulida**.

#### Tareas
1. **Statusbar contextual** (`internal/ui/statusbar.go`)
   - Muestra keybinds relevantes según contexto
   - Cambia al cambiar de pane
   - Click en acciones para ejecutarlas

2. **Help modal** (`internal/ui/modal.go`)
   - Se activa con `?`
   - Lista todos los keybinds agrupados por contexto
   - Scroll con j/k o mouse
   - Esc para cerrar

3. **Command palette** (`internal/ui/components/palette/`)
   - Se activa con `:` o `Ctrl+P`
   - Búsqueda fuzzy de comandos
   - Comandos contextuales
   - Historial de comandos recientes

4. **Keybind modes**
   - Vim mode (default)
   - Modern mode (flechas, intuitivo)
   - Emacs mode (Ctrl combos)
   - Switch con config o tecla

5. **Themes completos**
   - Dark (default dark)
   - Light (default light)
   - Nord
   - Gruvbox
   - Catppuccin
   - System (auto-detect)

6. **Mouse completo**
   - Click en cualquier zona interactiva
   - Scroll wheel en todos los panes
   - Drag para resize de panes (futuro)

#### Verificación
- [ ] Statusbar muestra keybinds correctos por contexto
- [ ] `?` abre help modal con todos los keybinds
- [ ] `:` abre command palette
- [ ] Cambio de theme funciona
- [ ] Mouse funciona en toda la app
- [ ] Vim y Modern mode funcionan

---

## Fase 2: AI Integration
**Duración**: 2-3 semanas
**Objetivo**: Session logs, NL→SQL multi-provider, schema context para LLMs

### Contexto
Esta fase convierte a dbx en una herramienta **AI-native**. No es un feature más, es un diferenciador clave. Los usuarios (humanos y agents) podrán:
- Consultar la DB en lenguaje natural
- Obtener contexto de schema para LLMs
- Re-ejecutar sesiones pasadas
- Usar dbx como CLI desde cualquier script

### Tareas

#### 2.1 Session Logger Completo
- JSON Lines con schema enriquecido
- Rotación automática
- Retención configurable
- Reader para parsear logs

#### 2.2 NL→SQL Multi-Provider
- Interface `Provider` con métodos `Generate(ctx, prompt, schema) (string, error)`
- Providers: Anthropic, OpenAI, DeepSeek, Qwen
- Auto-detect por env vars
- Prompt system con contexto de schema
- Validación de SQL generado (no destructive sin confirm)

#### 2.3 Schema Context Export
- `dbx context --json`: Exporta esquema completo
- Incluye: tablas, columnas, tipos, FKs, índices, estadísticas
- Optimizado para LLMs (tokens mínimos, info máxima)

#### 2.4 CLI Avanzado
- `dbx ask "show me active users"`: NL→SQL + ejecución
- `dbx replay session.log`: Re-ejecutar una sesión
- `dbx pipe`: Pipe mode para stdin/stdout

#### 2.5 Session Replay
- Leer un log file
- Re-ejecutar cada query
- Mostrar diff de resultados
- Útil para debugging y testing

### Verificación
- [ ] `dbx ask "count users by country"` genera y ejecuta SQL correcto
- [ ] `dbx context --json > schema.json` exporta schema completo
- [ ] Session logs se escriben y se pueden leer
- [ ] `dbx replay` funciona
- [ ] Multi-provider funciona (probar con al menos 2)

---

## Fase 3: Multi-DB
**Duración**: 3-4 semanas
**Objetivo**: Soporte para MySQL, SQLite, MongoDB

### Contexto
Con PostgreSQL funcionando,扩展emos a otros SGBD. La clave es el **Driver Interface** que abstracte las diferencias.

### Driver Interface
```go
type Driver interface {
    Connect(ctx context.Context, config ConnectionConfig) error
    Close() error
    ListSchemas(ctx context.Context) ([]Schema, error)
    ListTables(ctx context.Context, schema string) ([]Table, error)
    ListColumns(ctx context.Context, schema, table string) ([]Column, error)
    Execute(ctx context.Context, sql string) (*Result, error)
    Select(ctx context.Context, opts SelectOptions) (*Result, error)
    GetTableStats(ctx context.Context, schema, table string) (*Stats, error)
}
```

### Tareas por Driver

#### MySQL
- Adaptador a `go-sql-driver/mysql`
- `SHOW DATABASES`, `SHOW TABLES`, `DESCRIBE table`
- Multi-statement queries

#### SQLite
- Adaptador a `modernc.org/sqlite` (pure Go)
- Attach databases
- Virtual tables

#### MongoDB
- Adaptador a `go.mongodb.org/mongo-driver`
- Collections browser
- Aggregation pipeline editor
- Document viewer (JSON pretty-print)

### Verificación
- [ ] Cada driver pasa tests de integración
- [ ] Explorer funciona con cada DB
- [ ] Queries funcionan con cada DB
- [ ] Config soporta múltiples connections de diferentes tipos

---

## Fase 4: Advanced Features
**Duración**: 2-3 semanas
**Objetivo**: Features premium que completan la experiencia

### Contexto
Features que añaden valor pero no son core. Se implementan cuando la base es sólida.

### Tareas

#### 4.1 PostgreSQL COPY Protocol
- Export masivo con COPY TO
- Import masivo con COPY FROM
- Mucho más rápido que INSERT individuales

#### 4.2 SSH Tunnel Support
- Config en connections
- Auto-detect SSH key
- Tunnel persistence

#### 4.3 Connection Profiles
- Múltiples profiles por DB
- Switch rápido entre profiles
- Profiles por proyecto

#### 4.4 Query History + Favorites
- Historial persistente
- Búsqueda en historial
- Marcar queries como favoritas
- Nombrar favorites

#### 4.5 Export Avanzado
- CSV, JSON, SQL, Markdown
- Custom delimiters
- Header inclusion
- Compression

#### 4.6 Mouse Avanzado
- Double-click para editar celda
- Drag para resize de panes
- Right-click context menu completo
- Scroll wheel en editor

#### 4.7 Pane Management
- Split vertical/horizontal
- Close pane
- Swap panes
- Layout persistence

### Verificación
- [ ] COPY protocol funciona (test con tabla grande)
- [ ] SSH tunnel funciona
- [ ] Profiles se pueden switchar
- [ ] History y favorites funcionan
- [ ] Export a todos los formatos funciona
- [ ] Mouse avanzado funciona
- [ ] Puedo abrir múltiples panes

---

## Fase 5: Publication (1 semana)
**Objetivo**: Preparar para uso público

### Tareas
- [ ] README completo con screenshots
- [ ] Instalación: `go install`, brew, AUR
- [ ] Docs: ARCHITECTURE.md, KEYBINDS.md, CONFIG.md, CLI.md
- [ ] v0.1.0 release con GoReleaser
- [ ] Config examples
- [ ] Contributing guide

---

## Decisiones de Arquitectura

### Por qué Bubbletea v2
- **Declarative views**: View() retorna tea.View struct, no string
- **Mouse nativo**: Sin plugins, soporte completo
- **Clipboard OSC52**: Copy/paste sobre SSH
- **Cursed Renderer**: Rendimiento mejorado órdenes de magnitud
- **Key disambiguation**: Puedes bindear Ctrl+H sin que colisione con backspace
- **Synchronized updates**: Sin tearing en la pantalla

### Por qué BubbleZone para mouse
- **Zero-width zones**: No afecta el layout
- **API simple**: Mark() para crear, GetZone() para detectar
- **Combinable**: Funciona con cualquier componente de Bubbles

### Por qué Viper para config
- **TOML nativo**: Más legible que JSON/YAML para config
- **Env vars**: Soporte automático con `DBX_` prefix
- **Flags**: CLI flags sobreescriben config
- **Defaults**: Todo tiene default, la app funciona sin config

### Por qué Multi-Provider para LLM
- **Flexibilidad**: El usuario elige su provider
- **Coste**: Providers diferentes tienen costes diferentes
- **Latencia**: Local (Ollama) vs remoto (API)
- **Privacidad**: Algunos prefieren no enviar datos a APIs

---

## Keybinds Reference

### Global
| Key | Acción |
|-----|--------|
| `q` | Quit |
| `?` | Help modal |
| `:` | Command palette |
| `Tab` | Cycle focus |
| `1/2/3` | Focus pane directo |
| `a` | Ask AI (NL→SQL) |
| `e` | Export |
| `r` | Refresh |

### Explorer
| Key | Acción |
|-----|--------|
| `j/k` o `↑/↓` | Navigate |
| `Enter/l` | Expand / Load table |
| `Backspace/h` | Collapse / Go to parent |
| `g/G` | First / Last |
| `f` | Filter |
| `n` | New table |
| `d` | Drop table |
| `v` | View DDL |

### Grid
| Key | Acción |
|-----|--------|
| `j/k` o `↑/↓` | Navigate rows |
| `h/l` o `←/→` | Navigate columns |
| `g/G` | First / Last row |
| `Ctrl+U/D` | Half page up/down |
| `n/p` | Next / Previous page |
| `Enter/i` | Edit cell |
| `d` | Delete row |
| `o` | Insert row |
| `y` | Yank (copy) |
| `s` | Sort |
| `/` | Filter |

### Editor
| Key | Acción |
|-----|--------|
| `Ctrl+Enter` | Execute query |
| `Ctrl+U` | Clear |
| `Ctrl+G` | External editor |
| `Tab` | Autocomplete |
| `Ctrl+P/N` | History prev/next |

### Mouse
| Zona | Click | Scroll |
|------|-------|--------|
| Explorer node | Expand/collapse | Navigate |
| Grid cell | Edit | Scroll rows |
| Grid header | Sort | - |
| Editor | Position cursor | Scroll |
| Statusbar | Execute action | - |

---

## CLI Reference

```bash
# Query directa
dbx query "SELECT * FROM users LIMIT 10" --connection dev --json

# NL→SQL
dbx ask "show me all active users" --connection dev

# Listar
dbx list tables --connection dev --json
dbx list columns --table users --connection dev --json

# Schema
dbx schema users --connection dev --json

# Export
dbx export users --format csv --output users.csv
dbx export users --format json --output users.json
dbx export users --format sql --output users.sql

# Context para LLMs
dbx context --connection dev --json > schema.json

# Pipe mode
echo "SELECT count(*) FROM users" | dbx pipe --connection dev --json

# Session replay
dbx replay ~/.config/dbx/sessions/2026-09-11.log
```

---

## Notas para Implementadores

### Errores Comunes con Bubbletea v2
1. **View() retorna string, no tea.View**: En v2, retorna `tea.View` struct
2. **Mouse mode en View(), no en NewProgram**: `v.MouseMode = tea.MouseModeAllMotion`
3. **KeyMsg ahora es interface**: Usa `msg.String()` para comparar
4. **Clipboard**: Usa `tea.SetClipboard()` y `tea.ReadClipboard()`

### Testing
- Unit tests para drivers (mocks de pgx)
- Integration tests con PostgreSQL real (testcontainers)
- UI tests manuales (no hay framework automatizado para TUIs)

### Performance
- Lazy loading del árbol (no cargar todo de golpe)
- Virtual scrolling en grid (no renderizar filas invisibles)
- Debounce en fuzzy filter
- Cache de schemas (invalidar con refresh)

---

## Changelog del Plan

| Fecha | Cambio | Razón |
|-------|--------|-------|
| 2026-09-11 | Plan inicial | Start |
