# Referencias externas — Posibles mejoras para explorar

Seis candidatos con beneficio aparente y encaje en los límites actuales de Takt.
Son **hipótesis de mejora, no requisitos, decisiones de adopción ni backlog de release**.
Cada investigación debe demostrar una brecha real frente a lo que Takt y el harness
base ya ofrecen. Si no existe esa diferencia, se descarta.

Fuentes contrastadas el **10 de septiembre de 2026**. Los beneficios para Takt son
inferencias por validar; los reportes externos no son fallos reproducidos en Takt.
Cualquier adopción se formaliza según [el mapa de autoridad del producto](../product/INDEX.md).

## Filtro de compatibilidad

- Takt instala y proyecta capacidades; no sustituye el runtime ni la autenticación LLM del harness base.
- No usamos worktrees. La concurrencia sigue contratos con ownership exclusivo de archivos y el VFS definido por el producto, no ramas o entornos por agente.
- No se introduce una secuencia global de fases ni un segundo controlador de ejecución.
- Se priorizan mecanismos nativos y capacidades ya seleccionadas, sin servicios obligatorios, forks o dependencias complejas adicionales.
- Encajar en un contrato no acredita implementación actual ni amplía el release: las extensiones de runtime se contemplan primero en OpenCode; las preview conservan sus exclusiones.

## Candidatos

Los identificadores permiten investigar cada tema por separado; no expresan prioridad.

| ID | Tema | Beneficio por comprobar |
| --- | --- | --- |
| E1 | Handoffs precisos y contexto de revisión | Menos redescubrimiento sin perder independencia del verificador. |
| E2 | Presets de modelos respaldados por tareas | Menor consumo o latencia sin degradar resultados aceptados. |
| E3 | Consumo atribuible a unidades de trabajo | Localizar gasto evitable y evitar contabilización incompleta. |
| E4 | Recuperación acotada basada en evidencia | Evitar reintentos sin avance y cierres prematuros. |
| E5 | Recuperación de contexto guiada por símbolos | Encontrar código relevante sin inflar el contexto permanente. |
| E6 | Selecciones de stack reutilizables | Repetir una configuración deliberada sin copiar estado privado. |

### E1. Handoffs precisos y contexto de revisión

**Referencia.** Kimchi delega la revisión a un contexto nuevo. Un cambio del proyecto describe subagentes que gastaban 10–14 llamadas redescubriendo rutas y convenciones ya entregadas: aislar contexto también exige transmitir bien lo conocido. [Roles y delegación](https://github.com/getkimchi/kimchi#model-roles), [problema de sobreexploración](https://github.com/getkimchi/kimchi/pull/632).

**Qué explorar.** El contexto mínimo del handoff: objetivo, archivos autorizados, interfaces acordadas, hechos comprobados y límites pendientes. Para revisión, separar evidencia de afirmaciones del autor, sin transferir indiscriminadamente su conversación ni sesgar el veredicto.

**Encaje y límite.** La separación autor/verificador ya existe en [PRD_CREW](../product/meta-harness/crew/PRD_CREW.md), `PR-CRW-12`; no es una funcionalidad nueva. Se investigaría la calidad del handoff bajo `PR-ORQ-7`, `8` y `18` de [PRD_ORCHESTRATION](../product/meta-harness/orchestration/PRD_ORCHESTRATION.md), sin lecturas concurrentes de archivos ajenos ni permiso para que el orquestador implemente por compartir modelo con un ejecutor.

**Prueba que falta.** Comparar handoffs actuales y refinados: exploración repetida, consumo, accesos fuera de contrato y defectos detectados. Descartar una reducción de contexto que reduzca también la calidad de verificación.

### E2. Presets de modelos respaldados por tareas

**Referencia.** Kimchi asigna modelos por rol y capacidad; Plandex ofrece combinaciones con distintos compromisos de calidad, velocidad y coste. Esas opciones no demuestran ahorro para las tareas de Takt. [Kimchi](https://github.com/getkimchi/kimchi#model-roles), [model packs de Plandex](https://github.com/plandex-ai/plandex/blob/main/docs/docs/models/built-in/built-in-packs.md).

**Qué explorar.** Comparar presets simples en tareas representativas de los especialistas, contando también correcciones y revisiones. Determinar si una asignación curada mejora el resultado frente al modelo elegido por el harness.

**Encaje y límite.** [PRD_SETUP](../product/app/PRD_SETUP.md), `PR-SET-4` y `31`, ya contempla presets y un manifiesto canónico. Se investiga su contenido, no nuevas clases de crew. Se conserva la opción de no asignar modelos en OpenCode (`PR-SET-7`/`8`). No se adopta un router, pools dinámicos, escalamiento automático ni credenciales gestionadas por Takt.

**Prueba que falta.** Medir aceptación, reintentos, tiempo y tokens por tarea; coste monetario solo con datos fiables. ¿La mejora compensa mantener presets por plataforma y revisar su vigencia? Si no, conservar la resolución nativa.

### E3. Consumo atribuible a unidades de trabajo

**Referencia.** Kimchi etiqueta requests por modelo y fase, pero tuvo que corregir omisiones de telemetría en Ferment V2. Existe además una propuesta abierta para retirar las fases globales por duplicar estado; no es un cambio ya adoptado. [Tags implementados](https://github.com/getkimchi/kimchi/blob/master/src/extensions/tags.ts), [corrección de telemetría](https://github.com/getkimchi/kimchi/pull/1144), [propuesta de simplificación](https://github.com/getkimchi/kimchi/pull/1093).

**Qué explorar.** Atribuir el consumo disponible al dispatch, especialista, modelo efectivo y unidad de trabajo ya existentes, incluyendo revisión, corrección y reintentos. No crear fases autorreportadas para obtener esa atribución.

**Encaje y límite.** Refinar el mapeo al envelope de [PRD_OBSERVABILITY](../product/meta-harness/observability/PRD_OBSERVABILITY.md), `PR-OBS-ENV-1`..`5`, usando eventos nativos y sin duplicar el Action Journal. Respetar `PR-OBS-DAT-1`: sin prompts, respuestas ni secretos. No añadir dashboards, exportadores obligatorios ni otro bus; las señales ausentes se declaran, no se estiman como observadas.

**Prueba que falta.** ¿Qué consumo exponen realmente los hooks? Conciliar una ejecución con subagentes y reintentos sin duplicar totales; distinguir datos ausentes de cero. Comprobar que la atribución permite localizar un gasto evitable antes de ampliar sensores.

### E4. Recuperación acotada basada en evidencia

**Referencia.** Ferment V2 busca evitar cierres con objetivos pendientes; Plandex realimenta errores de comandos con reintentos acotados. El evaluador separado de Kimchi también introdujo timeouts ante inferencia lenta pero sana. [Motivación de V2](https://github.com/getkimchi/kimchi/pull/879), [debugging de Plandex](https://github.com/plandex-ai/plandex/blob/main/docs/docs/core-concepts/execution-and-debugging.md), [problema del evaluador](https://github.com/getkimchi/kimchi/pull/1139).

**Qué explorar.** Cómo presentar al orquestador la evidencia de una recuperación: resultado binario esperado, intento realizado, comprobación obtenida y presupuesto restante. Evaluar si evita repetir intentos o declarar éxito sin prueba.

**Encaje y límite.** Posible mejora del mecanismo de [PRD_ORCHESTRATION](../product/meta-harness/orchestration/PRD_ORCHESTRATION.md), `PR-ORQ-13`, y [PRD_OBSERVABILITY](../product/meta-harness/observability/PRD_OBSERVABILITY.md), `PR-OBS-PRG-1`..`3`; no un segundo lazo Ferment. Se mantienen tiempo de sesión e intentos como presupuesto; tokens no los sustituyen. No se añade un juez LLM obligatorio ni control autónomo diferido. Builds, suites y linters siguen ejecutándose después de materializar ([PRD_VFS](../product/meta-harness/harness/PRD_VFS.md), `PR-VFS-CSL-6`); no se importa rollback físico o basado en Git.

**Prueba que falta.** Casos con fallo repetido, avance real, herramienta indisponible y respuesta lenta. ¿Se distingue falta de evidencia de fracaso? ¿Se evita repetir una recuperación sin el resultado exigido, respetando presupuesto y escalamiento existentes?

### E5. Recuperación de contexto guiada por símbolos

**Referencia.** Plandex utiliza mapas de símbolos para seleccionar archivos y reutilizar código existente. Reconoce costes de foco, velocidad y consumo en la carga automática; los lenguajes no soportados se listan sin símbolos. [Gestión de contexto](https://github.com/plandex-ai/plandex/blob/main/docs/docs/core-concepts/context-management.md).

**Qué explorar.** Si las herramientas nativas o la integración de exploración ya seleccionada pueden entregar un mapa pequeño y pertinente bajo demanda, en vez de hacer que cada especialista recorra el repositorio o reciba un índice completo.

**Encaje y límite.** [ARCH_CONTEXT](../product/meta-harness/context/ARCH_CONTEXT.md), `RET-1`, `RET-5` y `CTX-1`: primero aprovechar recuperación existente. No construir un indexador tree-sitter, RAG o servicio propio. La consulta debe respetar los archivos autorizados, sin eludir ownership mediante un índice global. No hacer depender el core de una integración opcional ausente.

**Prueba que falta.** Comparar búsqueda actual y recuperación por símbolos: archivos relevantes encontrados, llamadas, tokens y vigencia tras cambios. Descartar si el soporte nativo ya resuelve el caso o si el mapa cuesta más de lo que evita.

### E6. Selecciones de stack reutilizables

**Referencia.** Goose permite distribuciones preconfiguradas y recipes. Advierte del coste de mantener forks; retiró el generador CLI de recipes que podía serializar credenciales desde la configuración de extensiones. [Custom Distributions](https://github.com/aaif-goose/goose/blob/main/CUSTOM_DISTROS.md), [retirada del generador](https://github.com/aaif-goose/goose/pull/11942).

**Qué explorar.** Reutilizar una selección declarativa de capacidades y presets para preparar otra instalación global, referida al catálogo canónico. No exportar una captura del entorno ni una sesión de agentes.

**Encaje y límite.** Posible extensión de [PRD_SETUP](../product/app/PRD_SETUP.md), no obligación de este release. Primero comprobar si el manifiesto de `PR-SET-31` y el registro de [PRD_INSTALLATION](../product/app/installation/PRD_INSTALLATION.md), `PR-INS-45`, bastan. No fijar formato nuevo, registry, forks ni instalación por proyecto. No copiar secretos, rutas privadas o políticas locales; la aplicación en destino seguiría pasando por resolución, revisión y autorización del plan.

**Prueba que falta.** ¿Hay un caso recurrente de repetir selecciones que no cubra la configuración actual? Probar dos entornos con capacidades distintas, sin asumir portabilidad de modelos ni arrastrar credenciales o configuraciones ajenas.

## Qué se retiró y por qué

- **Worktrees, ramificación de staging y backends por agente:** no corresponden al aislamiento virtual y los contratos de archivos de Takt. Cambiar de backend tampoco acredita migración del estado de una ejecución.
- **Gateway propio, proxy de secretos, hosting de harnesses, ACP como nueva plataforma y automatizaciones permanentes:** exigirían ampliar responsabilidades, no mejorar un mecanismo dentro del alcance actual.
- **FSM global por fases y dial genérico de autonomía:** no encajan con `PR-ORQ-16` ni sustituyen las decisiones de autoridad y escalamiento ya definidas.
- **Purga automática y anti-reingestión general de memoria:** no se trasladan sobre memoria append-only; la eliminación pertenece al Deep Dream validado por el usuario, según [ARCH_MEMORY](../product/meta-harness/context/ARCH_MEMORY.md). <-- NOTA: en realidad jamas se ha rechazado la idea de una limpieza realmente autonoma, pero esta tendria que ser sustancialmente mas mecanica que "dream" y ser mas un reordenamiento/compactacion y no eliminacion ya que eso al final solo es seguro si el usuario lo valida como correcto. la ejecucion automatica por tanto seria mas una optimizacion que una limpieza, esto para extender en el tiempo la utilidad de aquello que en realidad aun sigue ahi en lugar de jusgar si en realidad aporta valor como para continuar ahi. se podria decir que "dream" es una operacion conciente de limpieza de datos y la optimizacion de como se guarda aquello preservado para maximizar valor y garantizar que no hay contaminaciones de contexto o saturacion del mismo, mientras que un modo autonomo seria mas como "desfragmentar" un HDD mecanico, no hay perdida, simplemente se reduce el desorden natural que es generado por la escritura de los agentes en el dia a dia ya que esta no es ni determinista ni del todo estructurada(un agente podria ser muy "documentador" y terminar metiendo ruido, mientras otro podria apenas cumplir lo estrictamente requerido y dejar huecos en la informacion)
- **Versionado y separación autor/verificador como novedades:** ya están contemplados. El ownership actual usa `.takt-manifest.json` con versión de esquema, no `ownership.json`; véase [ownership.go](../takt/setup/ownership.go). E1 conserva solo la pregunta adicional sobre calidad del contexto de revisión.
- **Nuevo framework de plugins, cuotas arbitrarias de API y validadores adicionales:** no se ha demostrado una brecha que justifique otra capa frente a las proyecciones y verificadores existentes. Tampoco se importan los fallbacks de edición de Plandex.

## Cómo profundizar en cada candidato

1. Identificar un caso reproducible y comprobar qué resuelven hoy Takt y el harness base.
2. Revisar el mecanismo externo y sus incidencias en una versión o commit concreto.
3. Comparar la opción mínima con el comportamiento actual, incluyendo mantenimiento, fallos y diferencias de plataforma; no solo el recorrido positivo.
4. Concluir **descartar**, **seguir investigando** o **proponer adopción**, con evidencia. Solo la última opción abre una propuesta en el documento de producto correspondiente; esta lista no modifica requisitos ni adelanta elementos diferidos del release.
