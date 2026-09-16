# Propuesta: Query History per Project

## Problema

Actualmente dbx usa un único archivo global `~/.local/state/dbx/query_history.json` para almacenar el historial de queries de TODOS los proyectos. Esto causa que las queries se mezclen entre proyectos, dificultando el trabajo con múltiples bases de datos.

## Resultado deseado

Cada proyecto (identificado por su nombre de conexión en `.dbx.toml`) tendrá su propio archivo de historial de queries, manteniendo las consultas aisladas por contexto de trabajo.

## Alcance

- **IN**: Modificar `QueryStore` para aceptar contexto de proyecto, cambiar ruta de almacenamiento
- **OUT**: No se modifica la UI del queryBrowser (solo cambia la fuente de datos)

## Enfoque

Usar subdirectorio por proyecto: `~/.local/state/dbx/projects/{project_name}/query_history.json`

El `project_name` viene del nombre de la conexión en `.dbx.toml`:
```toml
[connections.mydb]  # ← "mydb" es el project_name
driver = "postgres"
dsn = "postgres://..."
```
