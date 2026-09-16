# Especificación: Query History per Project

## Requisitos funcionales

1. **Almacenamiento por proyecto**: Cada proyecto tendrá su propio archivo de historial de queries en `~/.local/state/dbx/projects/{project_name}/query_history.json`
2. **Aislamiento**: Las queries de un proyecto NO se mezclan con las de otro
3. **Migración automática**: Al abrir un proyecto existente, si ya existe un `query_history.json` global, se migra al primer proyecto
4. **QueryStore reutilizado**: La estructura `QueryStore` no cambia, solo la ruta donde almacena datos

## Criterios de aceptación

1. ✅ Al ejecutar una query en el proyecto "mydb", se guarda en `~/.local/state/dbx/projects/mydb/query_history.json`
2. ✅ Al cambiar de proyecto, el queryBrowser muestra SOLO las queries del proyecto actual
3. ✅ Las queries favoritas y nombres personalizados se mantienen por proyecto
4. ✅ Si existe un `query_history.json` global, se migra al proyecto actual (una sola vez)
5. ✅ No se rompe la funcionalidad existente del queryBrowser

## Cambios necesarios

| Archivo | Cambio |
|---------|--------|
| `internal/app/app.go` | Crear QueryStore después de seleccionar proyecto, no al inicio |
| `internal/store/query_history.go` | Sin cambios (ya acepta `stateDir` como parámetro) |
