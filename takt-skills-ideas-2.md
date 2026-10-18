# Selección Refinada de Skills Opcionales para Takt AI (v2)

Este documento define la propuesta técnica refinada de skills **opcionales y de activación bajo demanda (*on-demand*)** para Takt AI. 

A diferencia del catálogo base del sistema (que gestiona invariantes internas de memoria, VFS y ciclo de vida como `takt-memory-*` o `takt-vfs-*`), esta selección reúne capacidades metodológicas y de dominio especializadas. Están diseñadas para incluirse por defecto como parte del ecosistema de Takt (con posibilidad de excluirse mediante instalación personalizada), sin sobrecargar el contexto base permanente de ningún agente.

---

## 1. Principios de Arquitectura para Skills en Takt AI

1. **Carga del contenido bajo demanda (*On-demand skill body loading*):** Las skills no alteran `BASELINE`, `PERSONA`, `SOUL`, `OPERATIONS.md` ni `agent.yaml`. OpenCode puede incluir sus identificadores, nombres y descripciones en el contexto inicial. El contenido completo de cada `SKILL.md` se lee únicamente cuando la tarea lo requiere.
2. **Preservación estricta de invariantes y contratos:** Una skill nunca amplía permisos de VFS, no autoriza bypasses de validación ni altera la soberanía de los roles. Complementa el *cómo razonar o estructurar el trabajo*, no el *qué está permitido hacer*.
3. **Activación contextual justificada:** Si el problema no exige la habilidad, la skill permanece inactiva. Cada propuesta define criterios explícitos de activación y salida.

---

## 2. Resumen de la Selección Curada

| Skill Propuesta | Agentes Consumidores | Rol y Propósito | Valor Estratégico en Takt |
| :--- | :--- | :--- | :--- |
| **`takt-c4-modeling`** | `Architect`, `Product Designer` | Modelado y diagramación arquitectónica formal (C4 Model + Mermaid textual). | Reemplaza cajas flotantes y diagramas ambiguos por jerarquías formales basadas en evidencia del repositorio. |
| **`takt-technical-writing`** | `Spec`, `Architect`, `QA`, `TPM` | Redacción técnica rigurosa de especificaciones, ADRs e invariantes. | Asegura precisión, contratos unívocos y separación entre hechos, requisitos y contexto sin prosa innecesaria. |
| **`takt-decision-clarification`** | `PM`, `Architect` | Entrevista estructurada para desambiguar requerimientos (técnica *Grilling* adaptada). | Separa hechos investigables en el código de decisiones de producto; desactiva el "ping-pong" superficial de preguntas. |
| **`takt-differential-review`** | `Judge` (`judge-a`, `judge-b`), `Verify` | Revisión adversarial diferencial enfocada en riesgos, regresiones y seguridad. | Proporciona un marco metódico para evaluar superficies de impacto y deduplicar hallazgos por severidad demostrable. |
| **`takt-data-contracts`** | Futuro agente de Data/Analytics, `Architect`, `QA` | Calidad, contratos e invariantes de datos (grano, nulabilidad, frescura). | Habilita flujos analíticos y ETL/ELT verificables; previene la falsa confianza de los tests unitarios tradicionales, que dejan anomalías de datos reales sin detectar. |
| **`takt-k6-performance`** | `QA`, `Quality` | Pruebas de rendimiento, estrés y carga verificable no funcional. | Permite diseñar y ejecutar pruebas de carga realistas con umbrales medibles en entornos autorizados. |

---

## 3. Detalle de las Skills Seleccionadas

### 1. `takt-c4-modeling`
- **Agentes beneficiados:** `Architect`, `Product Designer`.
- **Problema que resuelve:** Los LLMs tienden a inventar diagramas de arquitectura imprecisos, con conexiones arbitrarias y dependencias que no existen en el código fuente.
- **Enfoque de integración en Takt:**
  - Adopta la semántica oficial del **C4 Model**: Contexto del Sistema, Contenedores, Componentes y Código (con Despliegue como vista ortogonal).
  - Cada relación diagramada exige justificación basada en evidencia observable del código o configuración (compatible con CodeGraph).
  - Emite sintaxis Mermaid estándar compatible con los entornos de renderizado de documentación del proyecto.
  - Genera únicamente los niveles necesarios para el encargo; no produce vistas exhaustivas por defecto.

### 2. `takt-technical-writing`
- **Agentes beneficiados:** `Spec`, `Architect`, `QA`, `TPM`.
- **Problema que resuelve:** Entregables técnicos (especificaciones, Architectural Decision Records - ADRs, matrices de prueba) redactados con lenguaje redundante, suposiciones implícitas o requisitos mezclados con explicaciones.
- **Enfoque de integración en Takt:**
  - Separa explícitamente: **Invariantes/Requisitos**, **Contexto técnico/Evidencia** y **Racional de la decisión**.
  - Mantiene el principio de artefactos canónicos redactados en inglés y con el vocabulario real del código/dominio.
  - Asegura que los criterios de aceptación sean comprobables por herramientas automatizadas o por los roles de verificación (`Verify`, `Judge`).

### 3. `takt-decision-clarification`
- **Agentes beneficiados:** `PM`, `Architect` (cuando actúan como interlocutores directos).
- **Problema que resuelve:** Preguntas interminables o triviales al usuario que el agente podría responder leyendo el código, o por el contrario, ejecución a ciegas asumiendo decisiones de producto críticas.
- **Enfoque de integración en Takt (basado en *Grilling*):**
  - **Filtro estricto de incertidumbre:** Solo se interroga al usuario sobre decisiones que alteran materialmente el diseño o el alcance y que no pueden inferirse del repositorio.
  - **Investigación previa obligatoria:** El agente debe verificar hechos en el VFS antes de formular una pregunta.
  - **Estructuración en árbol:** Las preguntas se presentan ordenadas por dependencias, agrupadas para evitar fricción, y ofreciendo alternativas concretas con recomendaciones justificadas.
  - Termina en cuanto se alcanza la comprensión suficiente para formular el plan/invariante.

### 4. `takt-differential-review`
- **Agentes beneficiados:** `Judge` (`judge-a`, `judge-b`), `Verify`.
- **Problema que resuelve:** Revisiones de código superficiales tipo linter o enfocadas en estilo cosmético, ignorando regresiones funcionales sutiles o vectores de riesgo.
- **Enfoque de integración en Takt:**
  - Aplica una lente adversarial sobre el diff: superficie de ataque alterada, manejo de excepciones no cubierto, invariantes preexistentes violados y efectos colaterales de concurrencia.
  - Marco de deduplicación y severidad: cada hallazgo debe estar respaldado por un escenario de fallo reproducible o evidencia estática demostrable.
  - Se adapta a la independencia de los jueces de Takt: el revisor emite hallazgos sin alterar el código ni intentar resolver el workflow de entrega.

### 5. `takt-data-contracts`
- **Agentes beneficiados:** Futuro agente de Data/Analytics, `Architect`, `QA`.
- **Problema que resuelve:** La validación de pipelines y transformaciones de datos suele fallar bajo agentes tradicionales porque los tests unitarios con mocks no detectan anomalías en la distribución, nulabilidad o grano de los datos reales.
- **Enfoque de integración en Takt:**
  - Define contratos conceptuales neutrales basados en dimensiones de calidad de datos: **Unicidad** (grano de clave primaria), **Completitud** (tolerancia de nulos), **Validez** (tipos, rangos, enums), **Consistencia** y **Frescura / SLA**.
  - Conexión pragmática con el stack del proyecto: traduce estos contratos a aserciones ejecutables en herramientas existentes (`dbt test`, `Great Expectations`, `Soda SQL` o scripts en Polars/Pandas/SQL en entornos staging).
  - Prepara el terreno arquitectónico para que los futuros agentes de datos de Takt operen con el mismo rigor de verificación que los agentes de software.

### 6. `takt-k6-performance`
- **Agentes beneficiados:** `QA`, `Quality`.
- **Problema que resuelve:** Generación de scripts de prueba de carga ingenuos que bombardean endpoints sin modelar usuarios concurrentes, etapas de calentamiento (*ramp-up*) ni umbrales (*thresholds*) de latencia/errores.
- **Enfoque de integración en Takt:**
  - Basada en las mejores prácticas de k6: escenarios reproducibles, métricas p95/p99 y thresholds estrictos.
  - Delimitación rigurosa de ejecución: la existencia de la skill no autoriza pruebas contra producción o infraestructura ajena. `QA` define los escenarios y riesgos; `Quality` ejecuta bajo entornos y cuotas explícitamente autorizadas.

---

## 4. Política de Empaquetado y Distribución

1. **Inclusión por Defecto (*Opt-out*):**
   - El instalador de Takt (`takt setup` o instalador interactivo) despliega estas 6 skills en el directorio correspondiente de OpenCode/agentes por conveniencia.
   - En el modo de instalación avanzada/personalizada (`custom install`), el usuario puede desmarcar individual o colectivamente cualquiera de estas capacidades si su flujo no las requiere.
2. **Sin Acoplamiento al Núcleo (*Zero Core Coupling*):**
   - El motor de Takt, sus invariantes y su orquestación central no tienen dependencias directas de estas skills para operar. Si se eliminan todas, Takt continúa funcionando con su catálogo de invariantes base intacto.
3. **Mantenimiento y Licencias:**
   - Cada skill adaptada debe cumplir con la licencia de Takt (AGPL-3.0) o licencias compatibles y atribuir formalmente las fuentes metodológicas de las que se nutre.
