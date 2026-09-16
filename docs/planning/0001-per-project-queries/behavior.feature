Feature: Query History per Project
  Como usuario de dbx
  quiero que cada proyecto tenga su propio historial de queries
  para mantener las consultas aisladas por contexto de trabajo

  Background:
    Given dbx está ejecutándose con un proyecto seleccionado

  Scenario: Query se guarda en el historial del proyecto actual
    When ejecuto la query "SELECT * FROM users"
    Then la query se guarda en "~/.local/state/dbx/projects/{project_name}/query_history.json"
    And la query tiene timestamp actual
    And la query no está marcada como favorita

  Scenario: QueryBrowser muestra solo queries del proyecto actual
    Given el proyecto "mydb" tiene 3 queries guardadas
    And el proyecto "otherdb" tiene 2 queries guardadas
    When abro el QueryBrowser con "Q"
    Then veo solo las 3 queries del proyecto "mydb"

  Scenario: Favoritos se mantienen por proyecto
    Given el proyecto "mydb" tiene una query favorita "SELECT count(*) FROM orders"
    When cambio al proyecto "otherdb"
    And abro el QueryBrowser con "Q"
    Then no veo la query favorita del proyecto "mydb"

  Scenario: Migración de historial global a proyecto
    Given existe un archivo "~/.local/state/dbx/query_history.json" con 5 queries
    When selecciono un proyecto por primera vez
    Then las 5 queries se migran a "~/.local/state/dbx/projects/{project_name}/query_history.json"
    And el archivo global se renombra a "~/.local/state/dbx/query_history.json.bak"

  Scenario: Nombre de proyecto válido para directorio
    Given el proyecto tiene conexión name "my-project_db"
    When creo el directorio de almacenamiento
    Then el directorio es "~/.local/state/dbx/projects/my-project_db/"
