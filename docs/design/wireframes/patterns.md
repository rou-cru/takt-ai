# Patrones e interacción

La galería define **composición y estados de referencia**, no lógica de negocio. Los controles se muestran solo si la implementación puede cumplir su contrato.

## Recorridos mínimos

```text
Home → Selección → Revisión → Aplicación → Resultado
                     ↕                       ↕
               Personalización         Diagnóstico / recuperación

Configuración → Edición enfocada → Revisión → Aplicación
                       ↓ Esc con cambios
                Seguir editando / Descartar
```

Personalizar es opcional. No añadir un paso para presentar el logo, una bienvenida o un resumen sin decisión. Un upgrade canónico solicitado no necesita confirmación redundante si no hay consecuencias nuevas.

## Contratos por patrón

| Patrón | Al entrar / foco | Acción y salida | Estado conservado |
|---|---|---|---|
| Home | Primera acción disponible; visible sin espera | Enter abre; Salir o Ctrl+C cierra sin mutación | Al volver, destino previo si sigue disponible |
| Selección | Setup elegido, o primero viable sin inventar selección | Flechas exploran; Enter elige; Tab pasa a acciones; Revisar prepara, no instala | Elección, foco, filtro y scroll al volver de detalle |
| Edición | Primer campo editable | Tab/Shift+Tab cambia región; flechas recorren; Space marca en múltiple; Revisar no aplica | Borrador hasta compromiso o descarte explícito |
| Revisión | Acción no destructiva por defecto si hay pérdida no aceptada | Instalar/Aplicar confirma el alcance visible; Personalizar vuelve al borrador | Cambios, exclusiones y alcance |
| Aplicación | Detalles, sin mover foco por progreso | Ctrl+C solicita interrupción; solo ofrecer Cancelar si es viable | Resultado real, incluidas fases parciales |
| Resultado | Siguiente acción pertinente | Volver al contexto de origen; detalle bajo demanda | Elecciones válidas para el retorno |
| Conflicto | Ver diff, sin preselección destructiva | Conservar bajo incertidumbre si viable; Restaurar indica reemplazo | Decisiones ya tomadas sobre otros objetos |
| Cambios pendientes | Seguir editando | Esc sigue editando; Descartar abandona solo el borrador | Todo lo ajeno a la edición |
| Bloqueo | Editar configuración | No mostrar Aplicar habilitado para una combinación imposible | Configuración editable y razón del bloqueo |

**Revisión con riesgo:** el fixture 06/07 muestra el botón de instalación enfocado como estado alcanzado después de recorrer las acciones, no como foco inicial recomendado. Las consecuencias permanecen visibles al llegar al compromiso. No pedir una confirmación adicional si esta vista ya cumple esa función.

## Teclado común

- Flechas recorren opciones; no cambian automáticamente una elección persistente.
- Enter activa el control enfocado. Space conmuta selección múltiple, no instala.
- Tab y Shift+Tab siguen un orden lógico entre regiones/acciones; dentro de formularios recorren campos.
- Esc vuelve o cancela el contexto. Con borrador modificado abre la decisión de descarte; no sale silenciosamente del TUI.
- Ctrl+C antes de aplicar cancela de forma segura. Durante aplicación solicita interrupción y comunica si debe terminar una fase. No implica rollback.
- Teclas imprimibles escriben en campos; no usar `q` como salida global mientras se escribe.
- `↑↓` tiene fallback `Arriba/Abajo`. Los fixtures no llevan barra de teclas; las acciones permanecen visibles junto a su contexto.

## Selección no es foco

En 04, Setup B tiene el foco y alimenta el panel de detalle; Setup A sigue elegido. Enter sobre B cambia la elección. Revisar utiliza la elección persistente, no simplemente el objeto bajo el cursor. En 15, Ver detalle abre una vista anidada del foco y vuelve a su posición sin cambiar la elección.

En el home, el relleno del control enfocado es énfasis de acción, no una elección de setup. El mapeo debe mantener el marcador en el gutter neutral y distinguir foco de selección cuando ambos coexistan.

## Consistencia de marcador en checklists y selectores

Entre marcado y no marcado (`[x]`/`[ ]`, `(x)`/`( )`) lo único que cambia es el glifo del marcador. Marcado y no marcado usan cada uno su propio color de rol — ninguno hereda un color por defecto ni depende de que el otro esté ausente. Ningún estado se convierte en un bloque resaltado que el otro no tenga: no hay una fila "con fondo" y otra "sin nada". El foco (`>`) sigue siendo un marcador aparte en su propio gutter, según "Selección no es foco" — no se mezcla con el marcador de elección persistente.

## Estados sin inventar más pantallas

| Situación | Adaptación del patrón | Salida |
|---|---|---|
| Carga inicial | Panel de selección: «Cargando setups…»; no opciones ficticias ni porcentaje inventado | Volver; progreso no captura foco |
| Vacío inicial | Panel: «No hay setups disponibles» | Fuente/configuración solo si existe; siempre Volver |
| Sin coincidencias | Wireframe 13 | Limpiar filtro restaura la lista y el foco |
| Error de carga | Panel: «No se pudo cargar la lista» | Reintentar y Volver; diagnóstico bajo demanda |
| Deshabilitado | Control permanece identificable con razón breve y accesible | Ruta de corrección; no usar solo opacidad |
| Error de campo | Mensaje junto al campo; conservar valor | Corregir; revisión no oculta el error |
| Parcial | Wireframe 10: aplicado y pendiente inequívocos | Reintentar solo si se conoce el alcance repetible |
| Estado desconocido | «Resultado no confirmado»; no fingir éxito o “no cambió nada” | Diagnóstico y verificación, sin reintento ciego |
| Ejecución larga | Fase actual y detalles a demanda | Sin timeout visual ni desaparición del resultado |

El fixture parcial presupone que solo falta descargar Memoria. Si no se puede garantizar ese alcance, no copiar el mensaje ni habilitar ese reintento.

## Brevedad sin ocultar consecuencias

- Datos como filas, columnas, valores y controles. Evitar describir en frases lo que ya muestra una selección.
- Mostrar solo categorías de cambio no vacías. `+`, `~`, `=`, `-` significan agregar, modificar, conservar y eliminar; la ayuda contextual debe explicar la leyenda cuando haga falta.
- Nunca unir varios ítems calificados con comas y un paréntesis por ítem en una sola línea (ej. "Harness A (GA), Harness B (preview)"). Un ítem por fila, con su calificador como sufijo simple — no entre paréntesis — igual que `06-review` usa `+`/`~`/`=`/`-` una fila por cambio. Esto vale también para un resumen simple sin diff (alcance, componentes de una instalación), no solo para diffs.
- Un riesgo decisivo no va a un tooltip, al diff cerrado o debajo de un scroll invisible.
- En conflictos, resumen comparativo primero; diff completo en una vista enfocada con retorno.
- Logs y diagnósticos tienen viewport propio; nunca datos privados o credenciales en fixtures.
- Los ejemplos no prometen backups, reversión o capacidades aún no implementadas.
