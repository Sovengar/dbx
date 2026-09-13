# dbx Architecture

## Overview

dbx follows a **layered architecture** with clear separation of concerns:

```
┌─────────────────────────────────────────────────────────┐
│                    CLI Layer (Cobra)                     │
│         dbx query, dbx ask, dbx list, etc.              │
├─────────────────────────────────────────────────────────┤
│                   TUI Layer (Bubbletea)                  │
│              Components: Explorer, Grid, Editor           │
│                    + Palette, Modal, StatusBar            │
├─────────────────────────────────────────────────────────┤
│                  Application Layer                       │
│             App, Router, Config, Theme, Keybinds          │
├─────────────────────────────────────────────────────────┤
│                   Driver Layer                           │
│           PostgreSQL | MySQL | SQLite | MongoDB           │
├─────────────────────────────────────────────────────────┤
│                    AI Layer                              │
│         Session Logger | NL→SQL | Context Export          │
└─────────────────────────────────────────────────────────┘
```

## Data Flow

```
User Input (Keyboard/Mouse)
        │
        ▼
┌───────────────┐
│  Bubbletea    │──── tea.Msg
│  Update Loop  │
└───────────────┘
        │
        ▼
┌───────────────┐
│  App Router   │──── Routes to focused component
└───────────────┘
        │
        ▼
┌───────────────┐
│  Component    │──── State update
│  (Explorer/   │
│   Grid/Editor)│
└───────────────┘
        │
        ▼
┌───────────────┐
│  Driver       │──── DB operation
│  (PostgreSQL) │
└───────────────┘
        │
        ▼
┌───────────────┐
│  View()       │──── Re-render
│  (Lipgloss)   │
└───────────────┘
```

## Component Model

Each UI component implements this interface:

```go
type Component interface {
    // Init returns initial command
    Init() tea.Cmd
    
    // Update handles messages
    Update(msg tea.Msg) (Component, tea.Cmd)
    
    // View renders the component
    View() string
    
    // Focus sets focus state
    Focus()
    
    // Blur removes focus
    Blur()
    
    // GetZoneManager returns zones for mouse
    GetZoneManager() *bubblezone.Manager
}
```

## Driver Interface

All database drivers implement:

```go
type Driver interface {
    // Connection
    Connect(ctx context.Context, config ConnectionConfig) error
    Close() error
    Ping(ctx context.Context) error
    
    // Schema
    ListSchemas(ctx context.Context) ([]Schema, error)
    ListTables(ctx context.Context, schema string) ([]Table, error)
    ListColumns(ctx context.Context, schema, table string) ([]Column, error)
    ListIndexes(ctx context.Context, schema, table string) ([]Index, error)
    ListForeignKeys(ctx context.Context, schema, table string) ([]ForeignKey, error)
    
    // Query
    Execute(ctx context.Context, sql string) (*Result, error)
    Select(ctx context.Context, opts SelectOptions) (*Result, error)
    
    // Stats
    GetTableStats(ctx context.Context, schema, table string) (*Stats, error)
    
    // Capabilities
    SupportsFeature(feature Feature) bool
}

type Feature int
const (
    FeatureCopy Feature = iota
    FeatureListenNotify
    FeatureTransactions
    FeatureMultipleStatements
)
```

## Theme System

Themes are resolved at startup:

```
1. Load config theme preference
2. If "system" → detect terminal colors
3. Generate palette from terminal colors
4. Apply to all Lipgloss styles
```

```go
type Theme struct {
    // Base colors
    Background, Foreground string
    Primary, Secondary     string
    
    // Semantic colors
    Success, Warning, Error string
    Info                    string
    
    // UI colors
    Border, BorderActive   string
    BackgroundPanel        string
    BackgroundElement      string
    
    // Text
    Text, TextMuted        string
}

func DetectSystemTheme() *Theme {
    // 1. Get terminal colors via OSC
    // 2. Generate gray scale
    // 3. Map ANSI colors
    // 4. Return theme with "none" for transparent
}
```

## Keybind System

Keybinds are loaded from built-in defaults with optional config overrides:

```
1. Load default bindings (each action has one or more default keys)
2. Apply custom overrides from config
3. Build registry
4. Match incoming key events
```

```go
type KeybindRegistry struct {
    bindings map[string][]string
}

func (r *KeybindRegistry) Match(key, context string) string {
    for action, keys := range r.bindings {
        if !inContext(action, context) {
            continue
        }
        for _, k := range keys {
            if k == key {
                return action
            }
        }
    }
    return ""
}
```

## Mouse Zone System

Mouse handling uses BubbleZone for zone detection:

```go
// In View()
func (m *App) View() tea.View {
    var s strings.Builder
    
    // Mark zones during render
    s.WriteString(m.explorer.View())    // Zones marked internally
    s.WriteString(m.grid.View())
    s.WriteString(m.editor.View())
    
    v := tea.NewView(s.String())
    v.MouseMode = tea.MouseModeAllMotion
    return v
}

// In Update()
func (m *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.MouseClickMsg:
        zone := m.zones.GetZone(msg.Mouse())
        return m.handleMouseClick(zone, msg.Button)
    case tea.MouseWheelMsg:
        return m.handleMouseWheel(msg)
    }
}
```

## Session Log Format

Logs are written in JSON Lines format:

```json
{
    "timestamp": "2026-09-11T14:30:22.123Z",
    "session_id": "abc123",
    "action": "query_executed",
    "sql": "SELECT * FROM users WHERE active = true",
    "table": "users",
    "schema": "public",
    "duration_ms": 125,
    "rows_affected": 42,
    "columns": ["id", "name", "email", "active"],
    "error": null,
    "metadata": {
        "connection": "local-dev",
        "provider": "anthropic",
        "nl2sql": false
    }
}
```

## File Structure Conventions

- **Components** are in `internal/ui/components/`
- **One file per concern**: tree.go, mouse.go, render.go
- **Tests** alongside source: tree_test.go
- **Driver** per database in `internal/drivers/`
- **CLI commands** one file per command in `internal/cli/`

## Error Handling

- User-facing errors: Toast notifications
- Internal errors: Log to session file
- Driver errors: Wrap with context, return to caller
- No panics in production code

## Performance Considerations

- **Lazy loading**: Don't load all schema at startup
- **Virtual scrolling**: Only render visible rows
- **Debounced input**: Filter/search debounced 100ms
- **Connection pooling**: pgx handles this
- **Cached schemas**: Invalidate on explicit refresh
