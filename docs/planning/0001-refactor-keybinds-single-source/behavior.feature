# language: en
Feature: Keybinds con una sola fuente de verdad (display + dispatch)

  Como usuario de la TUI
  Quiero que la definición de keybinds sea única y que la propia TUI sea la referencia
  Para no mantener documentación ni listas paralelas que se desincronizan

  # ---------------------------------------------------------------------------
  # Fuente única: la misma entrada alimenta display y dispatch
  # ---------------------------------------------------------------------------

  Scenario: La tecla que se muestra es la que ejecuta la acción
    Given la acción "Explorer: Filter Tables" declarada con la tecla "/"
    When el usuario está en la vista Explorer
    Then el panel de keybinds muestra "/" junto a "Filter Tables"
    And al presionar "/" se ejecuta exactamente "Explorer: Filter Tables"
    And no existe ninguna otra definición de esa tecla para esa vista

  Scenario: Customizar una tecla cambia display y ejecución a la vez
    Given el usuario configura en su config que "explorer.filter" usa la tecla "F"
    When el usuario está en la vista Explorer
    Then el panel de keybinds muestra "F" junto a "Filter Tables"
    And al presionar "F" se ejecuta "Explorer: Filter Tables"
    And al presionar "/" ya no se ejecuta "Explorer: Filter Tables"

  # ---------------------------------------------------------------------------
  # Sin concepto de "global": cada vista declara sus acciones
  # ---------------------------------------------------------------------------

  Scenario: El panel muestra solo las acciones de la vista actual (Explorer)
    Given el usuario está en la vista Explorer
    Then el panel de keybinds lista las acciones de Explorer
    And no muestra acciones exclusivas de Grid ni de Editor
    And no muestra ninguna acción bajo el rótulo "Global"

  Scenario: Cambiar de vista cambia el contenido del panel
    Given el usuario está en la vista Explorer
    And el panel lista las acciones de Explorer
    When el usuario mueve el foco a la vista Grid
    Then el panel lista las acciones de Grid
    And ya no lista las acciones exclusivas de Explorer

  Scenario: "q" cierra la app desde las vistas de navegación
    Given el usuario está en la vista Explorer (o Grid)
    When presiona "q"
    Then la aplicación se cierra

  Scenario: "q" es un carácter literal en el editor, no un atajo de salida
    Given el usuario está en la vista Editor
    When presiona "q"
    Then la aplicación NO se cierra
    And el carácter "q" se inserta en el contenido del editor
    And el panel de la vista Editor no anuncia "q" como atajo de salida

  Scenario: Una acción deja de aplicar en una vista donde no tiene sentido
    Given la acción "Editor: Copy SQL" (Ctrl+Y) declarada para la vista Editor
    And el usuario está en la vista Grid
    Then el panel de la vista Grid no muestra "Ctrl+Y copy"
    And presionar "Ctrl+Y" en la vista Grid no dispara "Editor: Copy SQL"

  # ---------------------------------------------------------------------------
  # Modal de ayuda completo (la referencia viva)
  # ---------------------------------------------------------------------------

  Scenario: El modal de ayuda cubre todas las secciones operables
    Given el usuario presiona "?"
    Then el modal muestra, como mínimo, secciones para: Explorer, Grid, Grid Preview, Explorer Preview, Editor, Query Browser y Connection Picker
    And muestra las acciones de los modos EDIT, FILTER y WHERE FILTER
    And muestra las acciones de mouse (click, wheel, doble click) donde correspondan

  Scenario: El modal de ayuda es navegable y se cierra
    Given el modal de ayuda está abierto
    When el usuario hace scroll con "j"/"k" o la rueda del mouse
    Then el contenido se desplaza sin salir de los límites
    When presiona "Esc" o "?"
    Then el modal se cierra

  # ---------------------------------------------------------------------------
  # Acciones con handler pendiente
  # ---------------------------------------------------------------------------

  Scenario: "Ask AI (NL→SQL)" conserva su binding y su metadata
    Given la acción "Ask AI (NL→SQL)" con la tecla "a"
    Then la acción aparece en el panel/modal de la(s) vista(s) donde aplica, con su descripción
    And la tecla "a" sigue reservada para esa acción (no queda libre ni reasignada)

  Scenario: Una acción con handler pendiente no rompe la validación
    Given una acción declarada explícitamente como "handler pendiente"
    Then la validación acepta que no tenga handler de TUI todavía
    And no se reporta como error de configuración

  # ---------------------------------------------------------------------------
  # Validaciones que evitan la regresión
  # ---------------------------------------------------------------------------

  Scenario: Toda acción declarada tiene sección y descripción
    Given cualquier acción del registry
    Then tiene una sección no vacía
    And tiene una descripción no vacía

  Scenario: No hay colisiones de tecla dentro de una misma vista
    Given dos acciones que aplican a la misma vista
    When ambas declaran la misma tecla
    Then la validación reporta una colisión indicando vista, tecla y las acciones en conflicto

  Scenario: Toda acción tiene handler, salvo las marcadas como pendientes
    Given cualquier acción del registry que NO esté marcada como pendiente
    Then existe un handler de dispatch para esa acción
    And no existe ningún handler de dispatch que no corresponda a una acción declarada

  # ---------------------------------------------------------------------------
  # Documentación: la TUI es la referencia
  # ---------------------------------------------------------------------------

  Scenario: La documentación de keybinds deja de duplicar la información
    Given el repositorio
    Then no existe el archivo "docs/KEYBINDS.md"
    And el README no contiene tablas de keybinds
    And el README indica al usuario que presione "?" para ver los atajos

  Scenario: La prosa que no es de keybinds se conserva en un doc de features
    Given el contenido no-keybind que vivía en "docs/KEYBINDS.md"
    Then existe "docs/FEATURES.md" con esa prosa (p. ej. DML Transactions, Action Naming Convention)

  Scenario: El checklist de contribución refleja el nuevo flujo
    Given el archivo "AGENTS.md"
    Then su checklist de keybinds ya no lista múltiples lugares de documentación a tocar
    And indica que la única fuente a tocar es el registry (acción + su metadata)
