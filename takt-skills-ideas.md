# Ideas de skills para Takt AI

Este documento reúne únicamente las capacidades que se consideran viables para
integrar o adaptar a Takt AI. Las propuestas complementan a los agentes existentes;
no constituyen una decisión de implementación ni requieren reemplazar las skills
globales del host.

La evaluación se basa en la revisión de contenido local, repositorios upstream y
documentación primaria. No se ejecutaron benchmarks: la afinidad y el contenido de
las candidatas están evaluados documentalmente, pero su mejora efectiva en Takt
todavía debe comprobarse.

## Resumen de candidatas

| Capacidad | Agentes beneficiados | Forma de incorporación propuesta |
| --- | --- | --- |
| Redacción técnica | PM, Spec, Architect, QA y TPM | Adaptar `technical-writing` a los formatos existentes. |
| Redacción de instrucciones para agentes | Autores y revisores del corpus de Takt | Rescatar criterios de `writing-for-agents`. |
| Diseño arquitectónico | Architect; Analyst para investigar alternativas | Guía breve con referencias a patrones y criterios de diseño de módulos. |
| Arquitecturas distribuidas y basadas en eventos | Architect; Dev y Fix para implementación autorizada | Referencias especializadas activadas según el problema. |
| Modelado y diagramas | Architect y Product Designer | Semántica C4 oficial, Mermaid textual y Archify opcional. |
| Revisión de cambios | Judge y Verify, según el encargo | Adaptar `review-agent` y añadir lentes especializadas cuando sean necesarias. |
| Pruebas de rendimiento | QA y Quality | Adaptar las skills oficiales de k6 a la separación de responsabilidades de Takt. |
| Calidad de datos | Architect, QA, Test, Dev y Quality | Criterios neutrales y referencias específicas del stack existente. |
| Diseño de memoria persistente | Architect y Analyst | Adaptar el upstream de `memory-systems` como conocimiento arquitectónico. |
| Evaluación de costos | Analyst y PM; Architect para alternativas técnicas | Cargar referencias de Cloud FinOps según el dominio. |
| Autoría y evaluación de skills | Autores y revisores del corpus de Takt | Método compacto de creación, comparación e iteración. |
| Clarificación de decisiones | PM, Architect y Product Designer | Adaptar la técnica de entrevista de `grilling` a la interlocución de Takt. |

La asignación identifica posibles consumidores. No amplía los permisos, el alcance
ni la autoridad de ningún agente.

## 1. Redacción técnica

### Base: `technical-writing`

La skill aporta criterios de claridad, precisión y organización para documentos
que personas necesitan comprender: especificaciones, informes, ADRs y documentación.
Es una candidata sólida para conservar y adaptar, sin introducir otro formato
canónico de entregables.

La integración debería conservar el vocabulario real del proyecto, separar
requisitos de explicaciones y evitar prosa que obligue a reconstruir la decisión.
Los artefactos canónicos mantienen la regla de Takt de redactarse en inglés.

**Fuente:** [Technical Writing en el catálogo de Cursor](https://github.com/cursor/plugins/blob/main/pstack/skills/technical-writing/SKILL.md).

### Complemento: `writing-for-agents`

Esta candidata se dirige a otro lector: el agente que consume instrucciones.
Aporta criterios para referencias condicionales, pasos, finalización, carga de
contexto y mantenimiento de una única fuente de cada regla.

Conviene rescatar esos criterios para escribir skills, `OPERATIONS` y referencias
de Takt. No sustituye la redacción técnica para personas y no debe duplicar reglas
que ya pertenecen al contrato base.

**Fuente:** [Writing for Agents](https://github.com/mattpocock/skills/blob/main/skills/productivity/writing-for-agents/SKILL.md).

## 2. Diseño arquitectónico

### Bases: `architecture-patterns` y `codebase-design`

`architecture-patterns` ofrece referencias sobre límites de dominio, dependencias,
puertos, adaptadores y separación de responsabilidades. `codebase-design` añade
un criterio complementario: interfaces pequeñas que encapsulan comportamiento
significativo y facilitan cambios localizados.

Para Takt, la propuesta es una guía breve que ayude a elegir según el problema,
los contratos existentes y la evidencia del repositorio. Los patrones concretos
quedarían en referencias consultadas bajo demanda.

La adaptación debería:

- Justificar cada separación por una necesidad real.
- Conservar el vocabulario y las tecnologías del proyecto.
- Hacer explícitas las interfaces, sus invariantes y sus errores observables.
- Distinguir una mejora estructural de una modificación de comportamiento.
- Evitar que un patrón se convierta en requisito por el mero hecho de estar disponible.

**Fuentes:** [Architecture Patterns](https://github.com/wshobson/agents/blob/main/plugins/backend-development/skills/architecture-patterns/SKILL.md), [Codebase Design](https://github.com/mattpocock/skills/blob/main/skills/engineering/codebase-design/SKILL.md).

### Especialización: microservicios

`microservices-patterns` puede conservarse como referencia para sistemas que
realmente requieren límites de servicios, comunicación distribuida y gestión de
datos entre componentes independientes.

Su activación depende del objetivo y del contexto. La referencia no implica
extraer servicios, introducir mensajería ni abandonar una arquitectura monolítica.

**Fuente:** [Microservices Patterns](https://github.com/wshobson/agents/blob/main/plugins/backend-development/skills/microservices-patterns/SKILL.md).

### Especialización: persistencia de eventos y CQRS

Las recetas concretas de `event-store-design` y `cqrs-implementation` son buenas
bases para el conocimiento especializado de Architect. Otras referencias sobre
proyecciones o sagas pueden consultarse cuando el diseño las necesita.

El contenido útil incluye persistencia de eventos, orden, concurrencia,
idempotencia, evolución y reconstrucción de estado. Cada mecanismo debe responder
a requisitos propios; disponer de estas referencias no obliga a adoptar todos.

**Fuentes:** [Event Store Design](https://github.com/wshobson/agents/blob/main/plugins/backend-development/skills/event-store-design/SKILL.md), [CQRS Implementation](https://github.com/wshobson/agents/blob/main/plugins/backend-development/skills/cqrs-implementation/SKILL.md).

## 3. Modelado y diagramas

### C4 como base semántica

C4 es viable como conocimiento de modelado, utilizando su definición oficial:
contexto, contenedores, componentes y código. Deployment es una vista
complementaria. Se producen únicamente los niveles y las vistas que aportan valor
al objetivo y a su audiencia.

La integración debería separar la decisión sobre qué representar de la elección
del renderizador. Los diagramas que describen código existente necesitan evidencia
del repositorio; una conexión dibujada no demuestra por sí misma una relación real.

**Fuente:** [Diagramas del modelo C4](https://c4model.com/diagrams).

### Mermaid para documentación textual

Mermaid resulta útil para diagramas mantenibles junto con documentación textual.
La sintaxis elegida debe ser compatible con el renderizador que recibe el
entregable. Su soporte específico de C4 continúa documentado como experimental,
por lo que no se presupone estabilidad entre versiones o clientes.

**Fuente:** [C4 en Mermaid](https://mermaid.js.org/syntax/c4.html).

### Archify para presentación interactiva

Archify es una candidata para producir HTML interactivo cuando ese formato forma
parte del encargo. Su representación JSON tipada y sus esquemas permiten validar
la estructura antes de generar la presentación.

La adaptación a Takt conservaría esa validación y la trazabilidad al contenido
fuente. La ubicación de salida, las exportaciones y las comprobaciones se ajustan
al alcance autorizado; no se generan entregables adicionales por defecto.

**Fuentes:** [Archify](https://github.com/tt-a1i/archify/blob/main/archify/SKILL.md), [esquemas JSON](https://github.com/tt-a1i/archify/blob/main/archify/schemas/README.md).

## 4. Revisión de cambios

### Base: `review-agent`

La candidata local `~/.codex/skills/.system/review-agent/SKILL.md` aporta una base
ligera: revisión de solo lectura, defectos demostrables, referencias concretas y
ausencia de modificaciones o delegación desde el revisor.

La adaptación debe conservar el alcance de la revisión y los formatos de hallazgos
y resultados de Takt. Una revisión del diff puede detectar regresiones, pero no
sustituye la aceptación completa del objetivo ni la evidencia ejecutable que
corresponde a Verify.

### Especialización: revisión diferencial de seguridad

`differential-review`, de Trail of Bits, aporta análisis basado en riesgo, historia
del cambio, alcance de impacto y evidencia de escenarios de fallo. Es útil cuando
la revisión requiere esa especialidad.

La profundidad, las lecturas permitidas y el mecanismo de entrega deben ajustarse
al encargo de Takt. No se impone un informe adicional cuando el resultado canónico
ya conserva la evidencia necesaria.

**Fuente:** [Differential Review](https://github.com/trailofbits/skills/blob/main/plugins/differential-review/skills/differential-review/SKILL.md).

### Criterios para revisión adversarial independiente

La revisión por varios jueces puede aprovechar criterios de independencia,
objetivo común, alcance estable y correcciones acotadas. La coordinación pertenece
a Takt; cada juez devuelve sus hallazgos sin asumir el workflow completo.

Para consolidar resultados son útiles las reglas de deduplicación de
`multi-reviewer-patterns`. La gravedad se justifica por impacto y evidencia, no
solo por adoptar la valoración más alta. Un hallazgo sustentado de un solo juez
debe poder validarse aunque el otro no lo haya detectado.

**Fuente:** [Multi-Reviewer Patterns](https://github.com/wshobson/agents/blob/main/plugins/agent-teams/skills/multi-reviewer-patterns/SKILL.md).

## 5. Pruebas de rendimiento

### Base: `k6`, de Grafana

La skill oficial de Grafana es una candidata más concreta para esta integración:
incluye ejemplos por protocolo, ejecutores, métricas, umbrales y consulta de
documentación, sin depender de un agente específico.

La adaptación a Takt debe respetar la separación entre planificación, autoría y
ejecución. QA define riesgos, cargas y criterios; Quality evalúa las características
no funcionales seleccionadas. Las rutas de scripts y resultados se ajustan al
ámbito autorizado.

La autorización del objetivo y de la carga es explícita. La presencia de una skill
no autoriza a ejecutar pruebas contra producción, servicios ajenos ni infraestructura
de pago. Una inspección del script no equivale a haber medido el sistema bajo carga.

**Fuente:** [k6 Script Generation](https://github.com/grafana/skills/blob/main/skills/grafana-k6/k6/SKILL.md).

### Especialización opcional: sitios web

`k6-perf-test-website` aporta flujos reales, pruebas híbridas y supervisión del
generador de carga. Es viable cuando el objetivo es un sitio web y su entorno
dispone de las herramientas necesarias; no es el camino general para CLI o TUI.

**Fuente:** [k6 Performance Test Website](https://github.com/grafana/skills/blob/main/skills/grafana-k6/k6-perf-test-website/SKILL.md).

## 6. Calidad de datos

La propuesta viable combina criterios neutrales de calidad con referencias
específicas del stack del proyecto. El núcleo describe qué debe verificarse:
completitud, unicidad, validez, consistencia, exactitud y vigencia, con umbrales y
evidencia adecuados al objetivo.

La implementación utiliza herramientas existentes y APIs de su versión real:

- **dbt:** las skills oficiales ofrecen instrucciones para modelos, pruebas y documentación.
- **Great Expectations:** su documentación actual sirve de base para expectations y validaciones.
- **Soda:** su skill de CLI aporta contratos, verificaciones y resultados estructurados cuando el proyecto utiliza Soda.

La adaptación no introduce un framework por defecto. También distingue pruebas
unitarias con entradas simuladas de validación de datos reales: una no demuestra
automáticamente la otra.

**Fuentes:** [dbt Agent Skills](https://github.com/dbt-labs/dbt-agent-skills), [ejemplo actual de GX Core](https://docs.greatexpectations.io/docs/core/introduction/try_gx/), [Soda CLI](https://github.com/sodadata/soda-cli/blob/main/skills/soda-cli/SKILL.md).

## 7. Diseño de memoria persistente

### Base: upstream de `memory-systems`

El upstream ofrece límites de activación explícitos para memoria semántica
persistente, seguimiento de entidades y recuperación entre sesiones. Distingue
ese problema de scratchpads, compacción y optimización del contexto.

Es viable como conocimiento de Architect y Analyst al investigar o diseñar sistemas
de memoria. La adaptación prioriza la solución más sencilla que satisface las
necesidades de recuperación y exige justificar estructuras adicionales.

Esta capacidad no sustituye Engram ni cambia el protocolo operativo de memoria de
los agentes de Takt. Las comparaciones de sistemas y benchmarks se verifican contra
sus fuentes antes de fundamentar una decisión.

**Fuente:** [Memory Systems](https://github.com/muratcankoylan/Agent-Skills-for-Context-Engineering/blob/main/skills/memory-systems/SKILL.md).

## 8. Evaluación de costos

### Base: Cloud FinOps, de OptimNow

Cloud FinOps es una candidata para analizar costos de inferencia, herramientas de
desarrollo, infraestructura y valor económico. Su organización por referencias
permite consultar solo el dominio pertinente.

Analyst puede comparar alternativas, PM relacionar gasto con valor y Architect
incorporar consecuencias económicas a decisiones técnicas. La skill no autoriza
cambios de proveedor, modelo, infraestructura o compromisos comerciales.

Las cifras deben indicar fuente, fecha y supuestos; no se reutilizan precios de
referencias como si fueran actuales. Antes de copiar y redistribuir contenido
adaptado deben revisarse las condiciones de atribución y distribución del material,
que declara licencia CC BY-SA 4.0.

**Fuentes:** [Cloud FinOps](https://github.com/OptimNow/cloud-finops-skills/blob/main/plugins/cloud-finops/skills/cloud-finops/SKILL.md), [licencia](https://github.com/OptimNow/cloud-finops-skills/blob/main/LICENSE.md).

## 9. Autoría y evaluación de skills

La propuesta es un método compacto para crear y mejorar skills de Takt, combinando
criterios útiles de las candidatas locales con evaluación de resultados.

De `skill-creator`, `skill-improver` y `skill-judge` se pueden conservar la concisión,
las referencias locales, la claridad de activación y la auditoría sin modificación
automática. La valoración estática sirve para detectar problemas de instrucciones;
no demuestra por sí misma que una skill mejore al agente.

El método de Anthropic aporta una base complementaria:

1. Definir tareas representativas y resultados esperados.
2. Comparar la candidata con la versión anterior o sin skill adicional.
3. Evaluar cumplimiento y calidad, incluyendo comparación ciega cuando aporte valor.
4. Revisar costos, fallos y activaciones indebidas.
5. Iterar sobre evidencia, preservando la intención y el contrato de Takt.

Se rescata el método, no la dependencia de un runtime particular. Las herramientas
que presuponen `claude -p` necesitan adaptación al harness seleccionado por el usuario.

**Fuentes:** [Skill Creator de Anthropic](https://github.com/anthropics/skills/blob/main/skills/skill-creator/SKILL.md), [comparador ciego](https://github.com/anthropics/skills/blob/main/skills/skill-creator/agents/comparator.md).

## 10. Clarificación de decisiones

### Base: `grilling`

La técnica de entrevista del upstream es una candidata para PM, Architect y Product
Designer cuando mantienen interlocución con el usuario. Distingue hechos que el
agente puede investigar de decisiones que corresponden al usuario, y organiza las
preguntas según sus dependencias.

La adaptación a Takt debería:

- Preguntar por incertidumbres que cambian materialmente el entregable.
- Investigar hechos por los canales permitidos, sin pedir al usuario que los adivine.
- Separar alternativas, recomendaciones y decisiones aceptadas.
- No delegar desde especialistas ni ampliar sus permisos.
- Respetar decisiones aprobadas y terminar cuando existe comprensión suficiente para el encargo.
- Devolver bloqueos por el canal de Takt cuando el especialista no mantiene el chat.

La intensidad y el agrupamiento de preguntas se ajustan a la conversación. El
objetivo es aclarar la intención, no imponer una entrevista exhaustiva a toda tarea.

**Fuente:** [Grilling](https://github.com/mattpocock/skills/blob/main/skills/productivity/grilling/SKILL.md).

## Criterios comunes de integración

Una candidata complementa las responsabilidades del agente; no reemplaza los
contratos de Takt. Su adaptación conserva los permisos, el alcance, la independencia
de la verificación y los mecanismos canónicos de resultados y memoria.

Las referencias y los recursos se cargan cuando el encargo los requiere. Antes de
incorporar material externo se comprueban su procedencia, licencia, dependencias y
compatibilidad con el harness y las herramientas elegidas.

Para decidir si una adaptación aporta valor, la comparación utiliza las mismas
tareas, modelo y harness. Se observan cumplimiento, errores, invasión de alcance,
calidad de resultados y costo de contexto. Se incluyen casos donde la skill no
debe activarse, sin imponer una evaluación extensa antes de conocer los primeros
resultados.

**Referencia:** [Evaluación de calidad de skills](https://agentskills.io/skill-creation/evaluating-skills).
