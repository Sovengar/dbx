# Plan: Query History per Project

**adr_required**: false

## Resumen

Cambiar el almacenamiento de queries de un archivo global a archivos por proyecto, usando el nombre de conexión de `.dbx.toml` como identificador.

## Cambios

### 1. `internal/app/app.go` — Crear QueryStore después de seleccionar proyecto

**Problema actual**: `QueryStore` se crea en `NewApp()` con una ruta global, antes de que el usuario seleccione proyecto.

**Solución**:
- Mover la creación de `QueryStore` a un nuevo método `initQueryStore(projectName string)`
- Llamarlo después de que el usuario selecciona proyecto en el picker (en el handler de `projectSelectedMsg`)
- La ruta será: `~/.local/state/dbx/projects/{projectName}/query_history.json`

**Flujo**:
```
Usuario selecciona proyecto → initQueryStore(project.Name) → QueryStore listo
```

### 2. Migración automática (opcional, bajo riesgo)

Si existe `~/.local/state/dbx/query_history.json` (formato legacy):
- Copiarlo al directorio del proyecto actual
- Renombrar el original a `.bak`
- Solo migrar la primera vez (si el archivo del proyecto no existe)

**Nota**: Esta migración es de bajo riesgo pero puede postergarse si el usuario prefiere un cambio más simple primero.

### 3. Sin cambios en `internal/store/query_history.go`

El `QueryStore` ya acepta `stateDir` como parámetro. Solo necesita recibir la ruta correcta.

## Archivos afectados

| Archivo | Acción |
|---------|--------|
| `internal/app/app.go` | Modificar — mover creación de QueryStore |
| `internal/store/query_history.go` | Sin cambios |
| `internal/config/config.go` | Sin cambios |

## Riesgos

- **Bajo**: El cambio es mecánico (solo cambia la ruta)
- **Migración**: Opcional, puede hacerse después
- **Backward compatible**: Los usuarios que solo tienen un proyecto no notarán diferencia

## Verificación

1. `go build ./...` — compila sin errores
2. `go vet ./...` — sin warnings
3. `go test ./...` — tests pasan
4. `make install` — instalar binario
5. Probar manualmente:
   - Abrir proyecto A, ejecutar query, verificar que se guarda en `projects/A/`
   - Cambiar a proyecto B, verificar que queryBrowser está vacío
   - Ejecutar query en B, verificar aislamiento
