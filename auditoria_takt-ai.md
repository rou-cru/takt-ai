# Auditoría de contexto y sistema en lazo cerrado de Takt AI

Se analizó la sesión completa como un único sistema distribuido: el crew, OpenCode, los plugins de Takt, el harness en Go, los MCP instalados, la guía de skills y los mensajes de bloqueo. Los síntomas observados (crecimiento del DAG sin convergencia, avance sin verificación real, saturación de contexto del orquestador y solicitudes de aprobación humana en modo autónomo) obedecen a causas concretas en el código y en las especificaciones operativas.

*   **Commit base de auditoría:** 25899d9 (trunk)
*   **Fecha:** 2026-10-29
*   **Auditoría:** 36 hallazgos verificados (28 confirmados, 8 plausibles)
*   **Sistema objetivo:** 6 planos invariantes - 37 fuentes empíricas

---

## TL;DR Seis causas raíz

1.  **Nada se ejecuta hasta el final del slice y los fallos no se registran:** `dev`, `fix` y `test` tienen prohibido ejecutar checks. El trabajo se consolida a disco al entregarse, sin compuerta previa. El historial de ejecución marca toda delegación como `completed` aunque Verify falle. Así, la TUI y el orquestador registran progreso donde solo existe código sin compilar (P1, H2).
2.  **Un slice no verificado cuenta como aceptado:** Si la verificación de extremo a extremo no puede ejecutarse (por ejemplo, dependencias pendientes o GUI), Verify lo reporta como `unverified`. QA solo desvía los fallos explícitos y blockers, permitiendo que el flujo continúe. El nodo de integración carece de ejecuciones de verificación y el entorno no tiene dueño asignado (P2, P3, P14).
3.  **Bucles sin cota y presupuestos reiniciables:** El ciclo Accept -> fix -> re-Accept tras consolidar no tiene límite determinista. Los presupuestos de recuperación y de QA utilizan cadenas de texto libre como clave de identidad. Los planes carecen de tope de nodos y cualquier ajuste técnico en el control point pasa por la historia, realineando en cascada los invariantes (P9, H7, P6).
4.  **Aprobaciones humanas en ejecución ordinaria:** Todo comando de shell en los roles de planificación solicita confirmación interactiva (`ask`). `pnpm install local` se clasifica como acción externa al workspace y cada commit de milestone exige aprobación explícita. La especificación del orquestador ordena escalar ante presupuestos agotados a pesar de existir salidas autónomas (H3, H4, H5, P7, P18).
5.  **El orquestador recibe el estado acumulado completo de la recolección de basura:** Cada llamada dispatch devuelve el objeto Coordinator serializado al completo, incorporando el historial acumulado de comandos y ciclos de GC entre sesiones. A esto se suman recargas obligatorias de skills en cada commit o fase y la relectura total de documentos de planificación (H1, P4, P5).
6.  **Especialistas ciegos o sobrecargados:** Bajo cualquier claim de escritura activo, operaciones como grep, glob y CodeGraph se deniegan globalmente, induciendo al agente a emitir un blocker. El cierre de Verify concentra múltiples informes, re-ejecución y documentación de QA en un único agente, mientras WBS y matrices monolíticas se leen íntegras por cada rol (H8, P13, P12).

---

## SÍNTOMAS: Cadena causal por síntoma

| SÍNTOMA OBSERVADO | CADENA CAUSAL | HALLAZGOS |
| :--- | :--- | :--- |
| **DAG crece sin entregar valor (caso MVP 2D)** | Planificación global y serial previa al primer slice -> detección tardía de integración -> múltiples nodos de fix -> ciclos re-Accept y revisiones sin límite -> ajustes técnicos reabren historias y realinean la cascada -> par ciego y re-planificación continua. | P1, P11, P9, H7, P17 |
| **Se reporta avance con integración rota y fallos obvios** | Los ejecutores no realizan checks computacionales -> consolidación a disco sin compuerta -> veredictos ausentes en el historial (DAG reporta completed) -> resultado unverified aceptado como válido -> el nodo de integración no ejecuta pruebas. | P1, H2, P14, P2, P3 |
| **Saturación de contexto y compactaciones del orquestador** | Serialización del Coordinator con el historial acumulado de GC -> recarga reiterada de verification-consolidation en cada commit -> lectura completa de especificaciones para verificar coherencia -> reintentos de claim_assign y mensajes de error extensos. | H1, P4, H13, P5, P16 |
| **Aprobación humana en modo autónomo** | Comandos de shell configurados en ask para roles de planificación -> instalación de dependencias locales clasificada erróneamente como externa -> commits por milestone interactivos -> escalado obligatorio al agotar presupuesto en lugar de resolución autónoma. | H3, H4, P18, H5, P7 |

---

## SIMULACIÓN: Orquestador ante "MVP de plataformas 2D", modo autónomo

Se simuló la sesión siguiendo las especificaciones operativas del catálogo. Los valores de tokens corresponden a estimaciones estándar (palabras x1.35), excluyendo el prompt base del entorno y esquemas MCP no verificables en repositorio. El límite de ~200k representa el umbral típico de compactación.

| FASE | DELEGACIONES | COMMITS | VERIFY / PARADAS HUMANAS | CONTEXTO ACUMULADO |
| :--- | :--- | :--- | :--- | :--- |
| Estático y arranque | 1 | 1 | 0 | ~23k |
| Planificación de invariantes | ~11 | 1-2 | 2-3 / 1 prompt por comando shell, 0 | ~58k |
| Arranque SDD (TPM, DAG) | 1 | 2 | 0 | ~78k |
| 5 vertical slices (~22k por slice) | ~55 | 10-15 | 15-20 / 1 commit por slice + escalados | ~180-210k (Excede umbral) |
| Cola QA y cierre | 6-10 | ~3 | dispatch exception al límite / 3-4 | ~210-240k |

*En un escenario con 3 vertical slices y QA en historias críticas, el DAG resultante abarca de 63 a 109 nodos (47-70 en trayectorias sin fallos). Esto demanda entre 220 y 380 llamadas del orquestador, 11-20 revisiones de plan y 9-21 ejecuciones de Verify, frente a las 3 contempladas nominalmente en PR-ORQ-34.*

---

## DETALLE DE HALLAZGOS

### 01 Progreso falso e integración no demostrada

| ID / Conf. | HALLAZGO Y EVIDENCIA | CORRECCIÓN MÍNIMA |
| :--- | :--- | :--- |
| **P1** (alta conf.) | Los ejecutores tienen prohibido correr verificaciones locales, pero se les exige evidencia empírica. El trabajo se consolida directamente en disco al entregarse. `takt-dev/OPERATIONS.md:5 Run no checks against your staged work` frente a `17 Mark nothing complete without evidences` y `BASELINE.md:29 demonstrate executable work with pertinent checks`, `takt-verification-consolidation:18 everything else consolidates on delivery`. | **CATÁLOGO / VFS:** Permitir `acceptance_check` acotado sobre la vista staged, restringir únicamente suites globales y definir evidencia como dicha salida. |
| **H2** (alta conf.) | Veredictos y resultados de aceptación no se registran en el historial; el DAG reporta `completed` incluso ante fallos de Verify. `dispatch_contest` resulta denegado en la mayoría de escenarios. `dispatch/dispatch.go:142 outcome = history.OutcomeCompleted`. `AuthorVerification` en `history.go:64` no tiene usos activos: colisión con PR-DAG-REP-5 y PR-OBS-PRG-3. | **GO:** Registrar veredictos como entradas vinculantes. Dar a `AuthorVerification` precedencia sobre la terminación de la delegación. |
| **P2** (alta conf.) | Cuando Accept concluye con extremo a extremo `unverified`, el control point lo procesa como aceptado. El verificador tiene prohibido instalar dependencias y el entorno carece de dueño. `takt-qa-workflow Accept Nest` solo bifurca ante fallos explícitos o blockers, `takt-verification-conduct:38 Install or update dependencies... Report the check as unverified`, `takt-sdd-workflow:192` no contempla el caso. | **CATÁLOGO:** Condicionar la aceptación a la verificación efectiva (`verified`). Clasificar `unverified` como tarea de entorno explícita en WBS. |
| **P14** (alta conf.) | Historias críticas `intermediate-fail` fluyen de Accept a Critique, Close y Report, terminando la ejecución sin retornar al control point de SDD. `takt-qa-workflow:82` solo retorna si la historia no es crítica; Report (86) concluye con `Audit/report closed`. | **CATÁLOGO:** Hacer que Close por historia retorne al control point; reservar Report exclusivamente para el cierre formal del objetivo. |
| **P3** (media conf.) | El nodo de integración `i` carece de dueño asignado, no ejecuta checks computacionales y duplica la coordinación ya efectuada por TPM. `takt-sdd-workflow:318 reads for loose ends... it runs no checks`, `topology-reference.md:38 single integration owners`, ausencia de asignación en tabla de dueños de OPERATIONS. | **CATÁLOGO:** Eliminar el nodo `i` y trasladar su comprobación de consistencia al Verify del vertical slice. |

### 02 Paradas humanas en modo autónomo

| ID / Conf. | HALLAZGO Y EVIDENCIA | CORRECCIÓN MÍNIMA |
| :--- | :--- | :--- |
| **P7** (alta conf.) | OPERATIONS estipula que un presupuesto expirado obliga a escalar y aguardar respuesta, mientras QA y el harness especifican avanzar hacia Close/Report. `OPERATIONS:118` frente a `takt-qa-workflow:186`. `dispatch.go:121` colisión entre IR-15 y PR-ORQ-20. | **CONTRATO / CATÁLOGO:** Excluir de IR-18 las cotas con resolución autónoma reglada; reservar escalado en OPERATIONS únicamente a fallos de recuperación críticos. |
| **P10** (alta plaus.) | En modo autónomo, `pm` emite blockers ante consultas no resueltas sin umbral de tolerancia; `product-designer` bloquea ante variaciones de identidad compartida en proyectos nuevos. `takt-pm/OPERATIONS.md:3`, `takt-product-designer/OPERATIONS.md:11`, `OPERATIONS:106-108`. | **CATÁLOGO:** Adoptar por defecto supuestos mínimos verificables de MVP en ejecución autónoma; bloquear únicamente ante divergencias del objetivo global. |
| **H3** (media conf.) | En roles de planificación (analyst, spec, qa, tpm, pm, architect, designer), cualquier invocación de shell está forzada a `ask`, incluso comandos informativos como `go version`. `renderer.go:387 ("shell", allResources, "ask")`, `takt-analyst/OPERATIONS.md:1`. | **GO / RENDERER:** Permitir ejecución desatendida para subconjuntos de inspección de solo lectura o forzar en catálogo el uso de primitivas nativas. |
| **H4** (media conf.) | El analizador de shell evalúa palabras clave descontextualizadas: comandos válidos como `pnpm install local` o `grep "go install"` se bloquean como externos al workspace. `vfs/shell.go:492-493` segmenta tokens sin respetar comillas ni argumentos. | **GO / VFS:** Clasificar con base en el ejecutable y sus banderas; permitir instalaciones locales registrando el delta sobre el workspace. |
| **H5** (baja conf.) | Cada commit de milestone generado por el orquestador solicita aprobación explícita sin advertencia previa en las especificaciones operativas. `vfs/vfs.go:568 orchestratorGatedGit` PR-VFS-61 / PR-VFS-62. | **PRODUCTO:** Preautorizar commits sobre ramas de trabajo locales al iniciar el objetivo dentro del límite del sandbox. |

### 03 Crecimiento del DAG sin convergencia

| ID / Conf. | HALLAZGO Y EVIDENCIA | CORRECCIÓN MÍNIMA |
| :--- | :--- | :--- |
| **P9** (alta conf.) | El ciclo Accept -> fix -> re-Accept -> re-crítica posterior a la consolidación carece de cota superior. El límite por mismo autor deja de operar una vez consolidado el cambio. `takt-qa-workflow:125`, `takt-failure-handling:85` contradicción con `OPERATIONS:60` PR-DRQ-33. | **CONTRATO / GO:** Presupuesto determinista de iteraciones de corrección por slice medido en dispatches, activando escalado al agotarse. |
| **H6** (alta conf.) | Límites de reintentos ausentes fuera del flujo formal de recuperación. El identificador de objetivo en recovery y qa round es texto libre: un cambio sintáctico reinicia contadores. `dispatch/recovery.go:93 budgets.Recoveries[d.Objective]`, `history/projection.go StrengtheningUsed(objective string)`. Colisión con PR-HAR-22. | **DISPATCH / GO:** Vincular identidad a entidades persistidas (unidad de aceptación o ID de historia). Denegar a partir del tercer reintento por entidad. |
| **P6** (alta conf.) | En modo autónomo, cualquier modificación técnica (incluido refactor) reabre la historia en el control point, pasando todo elemento dependiente a refinamiento y realineando invariantes. `takt-sdd-workflow:326-329 route a refactor`, `A change alters the story first`, PR-DRQ-35, `takt-invariant-planning:27`. | **CONTRATO / CATÁLOGO:** Reabrir la historia únicamente ante variaciones del comportamiento observable; canalizar ajustes técnicos como revisiones tácticas del plan. |
| **P11** (media conf.) | Planificación serial y monolítica antes del primer slice ejecutable: spec, qa y tpm operan a escala global para todo el objetivo sin umbrales de granularidad definidos. `OPERATIONS:39 takt-workflow-selection`, `takt-invariant-planning:42-48` encadena hasta 5 rondas sin definición en glosario. | **CATÁLOGO:** Planificar de manera autónoma hasta la profundidad del primer vertical slice ejecutable y expandir de forma incremental por historia. |
| **H7** (media conf.) | Ausencia de tope de nodos por plan o revisión en el motor Go. Las unidades planificadas eluden la cota de cuatro unidades aplicable al modo no planificado. `dispatch/plan.go validPlan` únicamente valida duplicados, prerrequisitos y aciclicidad, `dispatch.go:92-95`. | **CONTRATO / GO:** Imponer presupuesto máximo de unidades por slice y versión de plan respaldado por la política de admisión en Go. |
| **P17** (media conf.) | El par ciego se activa cuando dos fases alcanzan el límite de concurrencia, situación habitual en ejecuciones de ancho cuatro. Su posterior re-planificación carece de cota. `takt-verification-consolidation:42-44`. | **CATÁLOGO:** Condicionar la activación del par ciego a evidencias concretas de divergencia y definir un criterio estricto de cierre. |
| **H12** (media conf.), **P21** (media conf.) | Si la descripción de una delegación no coincide exactamente con el plan, el mensaje resultante induce a declarar inputs ad-hoc, creando unidades imprevistas de manera silenciosa. `takt-vfs.ts:1083 unit input.description`, `cli/execution.go:43-45 prepareExecution` no valida contra el plan activo. La instrucción de entregar un slice antes de iniciar otro colisiona con el principio de no demorar trabajo independiente listo para ejecución. | **CLI / GO:** Enumerar explícitamente en el mensaje de rechazo las unidades del plan vigente pendientes de admisión. **CATÁLOGO:** Formalizar la regla en el estado READY incorporando criterio determinista de desempate. |
| **P18** (media conf.) | El ejemplo de referencia de desarrollo de videojuego omite la descomposición en historias U, contraviniendo la regla general de planificación. `planning-dag-examples.md:148-154` frente a `takt-invariant-planning:26`. | **CATÁLOGO:** Incorporar las historias U en el ejemplo y catalogar la expansión de diseño como límite superior orientativo. |

### 04 Saturación del orquestador

| ID / Conf. | HALLAZGO Y EVIDENCIA | CORRECCIÓN MÍNIMA |
| :--- | :--- | :--- |
| **H1** (alta conf.) | Llamadas como `dispatch`, `commit`, `activity`, `recovery` y `go_request` retornan la estructura completa de `Coordinator`, incluyendo salidas de ejecución acumuladas de todos los ciclos de GC. `takt-vfs.ts:1444`, `cli/dispatch.go:376-466`, `gc/coordinator.go:302 without Projections` no trunca el historial de salidas. | **GO CLI/DISPATCH:** Retornar exclusivamente deltas compactos de transición (versión, units, estado actual). Es la medida de mayor reducción de tokens con menor coste de implementación. |
| **P4** (media conf.) | Instrucciones que imponen recargar `verification-consolidation` antes de cada commit o consolidación y `workflow-selection` antes de cada fase, sin política de caché. `OPERATIONS:10` y `128-129`, `takt-verification-consolidation:14`. | **CATÁLOGO:** Cargar las skills una única vez por nodo de ejecución y actualizar únicamente tras eventos de compactación. |
| **P8** (media conf.) | Un mismo tipo de fallo se enruta a dos skills divergentes; se citan cotas de recuperación en OPERATIONS que no existen en el texto. `takt-sdd-workflow:104-105` remite a `takt-sdd-recovery` mientras `OPERATIONS:117` remite a `takt-failure-handling`. | **CATÁLOGO:** Establecer la tabla de OPERATIONS como fuente única de verdad para el enrutamiento de excepciones. |
| **H11** (media conf.) | `claim_assign` es denegado cuando otro autor ejecuta una inspección sobre la ruta; el orquestador carece de primitiva para pausar y esperar. `vfs/native.go:222-238 retry claim assign after that inspection completes`. | **GO VFS:** Aislar la vista de inspección como privada o reportar la referencia del dispatch bloqueante. |
| **P16** (media plaus.) | Se ordena enviar problemas no localizados al especialista responsable de aislar la causa, pero dicho rol no figura en la matriz de asignación. `OPERATIONS:29-34`. | **CATÁLOGO:** Designar explícitamente la responsabilidad de aislamiento (exigiendo archivo y línea a Verify o derivando a analyst). |
| **P5** (baja-media plaus.) | Verificar la coherencia multinivel exige la lectura completa de artefactos externos de especificación tras cada modificación del DAG. `OPERATIONS:101`, `takt-invariant-planning:85`, PR-ORQ-22. | **CONTRATO / CATÁLOGO:** Delegar la validación de coherencia a una instancia de consolidación que reporte únicamente inconsistencias encontradas. |
| **H14** (baja conf.) | Se inyectan avisos de memoria redundantes en cada turno que reproducen las descripciones de las herramientas ya declaradas en el sistema. `takt-memory.ts:159-175`. | **PLUGIN:** Inyectar dichos recordatorios únicamente ante transiciones efectivas de estado en la sesión. |
| **H16** (baja conf.) | El mensaje de rechazo de edición nativa sugiere a roles como `pm` o `analyst` el uso de herramientas reservadas exclusivamente al orquestador. `vfs/native.go:271-284`, `takt-vfs.ts:1204-1207`. | **VFS / GO:** Ajustar el texto de rechazo según el perfil del rol, instruyendo retornar la ruta al orquestador. |

### 05 Saturación y ceguera de especialistas

| ID / Conf. | HALLAZGO Y EVIDENCIA | CORRECCIÓN MÍNIMA |
| :--- | :--- | :--- |
| **H8** (alta conf.) | La presencia de cualquier claim activo o staged provoca el bloqueo de búsquedas globales y CodeGraph a roles ejecutores, sugiriendo emitir un blocker en lugar de acotar la búsqueda. `vfs/native.go:68-71, 183-188, 197`, `caller.go:78-88`. | **GO VFS:** Diferenciar consulta no acotada de conflicto real de concurrencia; mantener CodeGraph en modo solo lectura sobre el estado del repositorio. |
| **P13** (media plaus.) | La fase de Close en Verify acumula lectura de múltiples informes, re-ejecución de verificaciones y emisión del Test Completion Report en una sola instancia. `takt-verify/OPERATIONS.md:5`, `takt-qa-workflow:35-38` tensión con PR-ORQ-34. | **CATÁLOGO:** Distribuir la recolección de evidencia en instancias paralelas y delegar la elaboración del TCR al rol qa. |
| **P12** (baja-media plaus.) | El WBS y la matriz de trazabilidad operan como entradas monolíticas para todo el objetivo; cada agente lee el documento íntegro para extraer su fila de interés. `takt-memory-tps:13-17`, `takt-memory-qa:18`. | **CATÁLOGO:** Segmentar el WBS por vertical slice y modularizar las matrices de verificación por historia. |
| **H9** (baja parcial) | Instrucciones de lectura repetitivas en cada turno del subagente; la agregación de invariantes de múltiples autores puede generar discrepancias en la verificación de hash. `takt-vfs.ts:1959-1961`, `bindings.go:299`. | **PLUGIN / CATÁLOGO:** Inyectar la directiva una única vez por delegación y acotar inputs del verificador por clave de autor. |
| **P15** (baja plaus.) | La lectura de inputs por identificador computa formalmente como uso del subsistema de memoria, induciendo a cargar un contrato extenso sin opción explícita de omisión. `layout.go:59-63`, `takt-memory-contract:96, 105`. | **CATÁLOGO:** Desacoplar reglas de lectura de las de persistencia y permitir formalmente la ausencia de registros. |
| **H10** (baja conf.) | Inconsistencia de nomenclatura: las especificaciones operativas citan `mem get_observation`, mientras las herramientas registradas exponen `engram mem`. `takt-vfs.ts:319, 1960`, `engram/memory.go:39-44`. | **CATÁLOGO:** Unificar nombres bajo un único identificador verificado mediante pruebas unitarias de contrato. |
| **H13, H15** (baja conf.) | Roles de verificación, QA y planificación invocan shell sin confinamiento de sandbox; la protección contra escrituras recae únicamente en instrucciones en prosa. `takt-vfs.ts:712-714`, `takt-verification-conduct:31`. La estructura `session_entries` transmite todos los identificadores históricos de la sesión en cada traspaso, sin límite de escala. `dispatch/interlocutor.go:156-164`. | **GO PLUGIN / GO:** Aplicar sandbox estricto en modo scratch-only para tareas de verificación y control de calidad. Transmitir exclusivamente los identificadores registrados por el intermediario para el nodo raíz. |
| **P19** (baja plaus.) | En sesiones delegadas, la aceptación de `dispatch handoff` y la confirmación expresa del entregable constituyen dos pasos redundantes de una misma acción. `takt-interlocutor-handoff:20-21` frente a `47-49`. | **CATÁLOGO:** Definir normativamente que la aceptación del traspaso convalida automáticamente el entregable. |

---

## 06 Fundamentos de control en lazo cerrado

Del parche sintomático al control en lazo cerrado. Las correcciones aisladas por síntoma mitigan mecanismos puntuales pero tienden a proliferar excepciones en la prosa. El diseño normativo de Takt ya contempla un sistema de control con realimentación (`AGENTS.md`, `PRD OBSERVABILITY`, `SMC CONCEPT`), pero la implementación operaba con el lazo abierto: sensores enfocados en actividad y no en resultados ejecutables, límites delegados a la prosa, el operador humano como actuador de rescate y sobrecarga de contexto inyectada por el propio harness.

El bus `takt/obs` existe en la base de código, pero solo publica eventos de bajo nivel (escrituras de VFS, colisiones de claims y efectos de GC en `session/session.go:84-100`). Ningún resultado de verificación de aceptación, Verify o pruebas extremo a extremo se publica en el bus ni se persiste en el historial. Al carecer de sensores de resultado computacional, el orquestador LLM asume indebidamente el rol de controlador, consumiendo contexto en tareas de re-inspección y re-planificación que inducen saturación y falsos positivos.

**Los 36 hallazgos son 5 desviaciones de un mismo diseño:** La implementación dejaba el lazo abierto al carecer de eventos tipados de verificación, respaldar límites en prosa y carecer de fronteras de sandbox deterministas.

## 07 Diagnóstico unificado

| DESVIACIÓN | DISEÑO NORMATIVO | COMPORTAMIENTO OBSERVADO | SÍNTOMA RESULTANTE |
| :--- | :--- | :--- | :--- |
| **D1 Sensores sin resultados** | El progreso se cuantifica mediante resultados de verificación y deltas consolidados. | Los ejecutores no realizan checks computacionales. La primera ejecución ocurre en Verify y Finish asume siempre OutcomeCompleted. | Avance ficticio, integraciones rotas. |
| **D2 Carga autoinyectada** | Contexto estricto, recuperado bajo demanda ARCH_CONTEXT. | Cada dispatch retoma el Coordinator completo. Avisos por turno, recarga de skills por fase y lectura integral de invariantes. | Saturación y compactaciones. |
| **D3 Control en prosa** | Límites críticos respaldados de manera determinista por el harness. | Reintentos regulados por texto libre. Ausencia de tope de nodos y ciclos verify-fix sin cota. | Crecimiento improductivo del DAG. |
| **D4 Humano como actuador** | Corrección autónoma dentro de las políticas definidas por el usuario. | Consultas interactivas de shell, instalación y git; presupuestos agotados escalan forzosamente al operador. | Aprobaciones en modo autónomo. |
| **D5 Topología pesada** | Coordinación proporcional al alcance de la tarea. | Planificación serial global, cascadas ante cambios técnicos, Verify sobrecargado y lecturas bloqueadas por claims. | Saturación de especialistas y deriva. |

---

## 08 Evidencia empírica y principios de diseño

| PRINCIPIO | EVIDENCIA PRINCIPAL | MATICES Y CONTRAPARTIDAS |
| :--- | :--- | :--- |
| **E1 Verificar ejecutando, no revisando** | **ACAD** SWE-agent: edición asistida por linter logra 18.0 frente a 15.0 [6]. Agentless: tests de reproducción elevan éxito de 27.0% a 32.0% [8]. Sin feedback computacional externo, los modelos no se autocorrigen [11]. La combinación de verificación por ejecución y modelos alcanza 51% frente a 42-43% aislado [12].<br>**PRACT** Replit: la verificación autónoma mediante agente tester elevó la autonomía de 20 a más de 200 min [19]. Böckeler: prioridad a sensores computacionales (keep quality left) [23]. | Cursor retiró jueces LLM a gran escala al escalar modelos [18]. Los críticos LLM complementan la ejecución, no la sustituyen [16]. |
| **E2 Oráculo fuera del alcance de quien implementa** | **ACAD** ImpossibleBench: los modelos modifican tests en un 54% de casos no acotados; habilitar solicitud de intervención reduce la trampa a 9% [7]. METR: instrucciones en lenguaje natural tipo "no modifiques los tests" carecen de eficacia estadística [15].<br>**ACAD** Meta ACH: generación de pruebas guiada por mutación con 73% de aceptación en ingeniería [14]. | Ocultar completamente los tests mitiga trampas pero penaliza el rendimiento legítimo [7]. Anthropic recomienda tests inmutables de solo lectura [7], o recurre a restricciones operativas en especificación [32]. |
| **E3 Progreso medido con cota dura** | **ACAD** MAST: repetición de pasos (15.7%) e incapacidad de reconocer el cierre (12.4%) representan los fallos dominantes; verificar el objetivo añade +15.6% [1]. Magentic-One: ledgers con contador de estancamiento s2 y reinicio de contexto; sin ledger el éxito cae 31% [3]. BATS: presupuestos visibles logran 24.6% frente a 12.6% [4]. | En Magentic-One la evaluación de estancamiento depende en parte del LLM. Benchmarks predominantemente orientados a web. |
| **E4 Un dueño del estado y proyección mínima** | **PRACT** Manus: optimización de tasa de acierto en KV-cache fijando el objetivo al final del contexto y reteniendo errores [21]. Factory: la estructura previene la degradación, optimizar tokens por tarea y no por petición [20]. LangChain: reenviar salidas estructuradas en lugar de reformular incrementa éxito ~50% [31].<br>**ACAD** La longitud por sí misma degrada entre 13.9% y 85% a pesar de recuperación perfecta [13]. Enmascarar observaciones históricas iguala al resumen a mitad de coste [5]. | El rastreo integral de artefactos sigue abierto [20]. En determinados modelos como Qwen3-32B, enmascarar incrementó longitud de trayectoria [5]. |
| **E5 Un escritor por ámbito; lectura concurrente** | **PRACT** Cognition: escrituras estrictamente secuenciales (single threaded); revisores aislados identifican ~2 defectos adicionales por PR [16] [17]. Cursor: la contención de bloqueos degrada 20 agentes al rendimiento de 1-3 [18]. Aider: disociar arquitecto de editor alcanza 85.0% frente a 79.7% monolítico [26].<br>**ACAD** En SWE-bench Verified, configuraciones multiagente no estructuradas rinden por debajo del agente único [2]. | Anthropic documenta mejoras en investigación preliminar a costa de 15x consumo de tokens, advirtiendo dificultades de encaje arquitectónico en el código final. |
| **E6 Planificación proporcional y alcance congelado** | **PRACT** Böckeler: sobredimensionamiento en generación de especificaciones [22]. Thoughtworks cataloga SDD en fase de evaluación (Assess) [33]. Devin muestra menor rendimiento cuando se introducen directivas a mitad de ejecución [24].<br>**ACAD** Reducir el sobre-análisis especulativo y priorizar acción/observación eleva éxito ~30% con 43% menos cómputo [9]. | Cursor, Junie y Humanlayer respaldan la utilidad de planes preliminares livianos. |
| **E7 Autonomía por frontera técnica** | **LAB** El confinamiento en sandbox reduce en 84k las solicitudes de confirmación interactivas [27].<br>**PRACT** Kiro condicionó la ejecución desatendida a validación automatizada por tarea [29]. Simon Willison propone aislamiento en sandbox, credenciales de mínimo privilegio y topes de gasto [37]. | Se requiere complementar sandbox con allowlists estrictas de red y herramientas. |
| **E8 Prompt estático mínimo, cargado una vez** | **ACAD** ETH: directivas voluminosas tipo AGENTS.md no incrementan el éxito y elevan el coste por encima del 20% [10].<br>**PRACT** Manus: mantener prefijos de prompt estables y herramientas consistentes preserva la caché [21]. Cursor: alternar esquemas o modificar herramientas en tiempo de ejecución degrada el contexto [30]. | Directivas acotadas redactadas por desarrolladores ofrecen mejoras marginales en dominios específicos. |

---

## 09 Sistema objetivo en lazo cerrado

Un único lazo cerrado. El orquestador decide las siguientes acciones evaluando una proyección compacta y nunca el estado crudo. Un único escritor por ámbito ejecuta verificaciones computacionales en su propio bucle interno. Los oráculos ejecutan pruebas y emiten resultados verificables al bus. El plano de control cuantifica el progreso real y aplica correcciones automáticas. La interacción humana queda reservada exclusivamente al cruce de fronteras de seguridad o definiciones de valor.

### PLANO 1 Estado y contexto: un dueño, proyecciones pequeñas
*(Resuelve D2. Evidencia: E4 y E8)*
*   **1.1** El ledger de Go (dispatch, history, GC) constituye el único dueño del estado de ejecución (STR-15). Los modelos reciben proyecciones estructuradas, nunca el estado crudo.
*   **1.2** Cada llamada dispatch devuelve (id, transición, delta, siguientes acciones válidas). El estado completo se obtiene únicamente mediante lectura paginada explícita.
*   **1.3** Una ficha de objetivo compacta se proyecta de forma invariable al final del contexto en cada turno, conteniendo el objetivo activo, el slice en curso, el checklist de características y el presupuesto restante, suprimiendo recordatorios dispersos.
*   **1.4** Los artefactos de especialistas se referencian por identificador de evidencia sin paráfrasis en el orquestador. Los invariantes se recuperan por identificador y sección acotada.
*   **1.5** Cada skill se inyecta de forma determinista una sola vez por nodo de trabajo; el prefijo estático del prompt se mantiene invariante para favorecer la caché KV.
*   **1.6** La compactación opera como transición formal en frontera de vertical slices: enmascaramiento prioritario de observaciones antiguas, traspaso estructurado de artefactos y recurso al resumen sintético como último término.

### PLANO 2 Sensores: resultados, no actividad
*(Resuelve D1. Evidencia: E1 y E3. Alineado con PR-OBS-PRG-1)*
*   **2.1** Todo resultado de verificación (build, lint, typecheck, acceptance, Verify, E2E) y estados de excepción (bloqueado, imposible, no verificable) constituyen eventos tipados en el bus y el historial. Finish no asume éxito por defecto.
*   **2.2** Los ejecutores (dev, fix, test) ejecutan checks computacionales locales dentro de su bucle interno de trabajo, ejecutándose además de forma automatizada sobre cada modificación staged.
*   **2.3** Ningún nodo de trabajo se marca como concluido en la interfaz ni en la proyección sin evidencia computacional contrastada; los fallos se persisten de forma estructurada asociados a la entidad correspondiente.

### PLANO 3 Oráculos: separados de quien implementa
*(Resuelve el falso avance D1 y D5. Evidencia: E1 y E2)*
*   **3.1** En cada slice, el rol test define inicialmente las pruebas de aceptación ejecutables y constata su fallo inicial. El VFS las protege en modo solo lectura para el ejecutor mediante author keys.
*   **3.2** Una característica se convalida únicamente tras superar la aceptación, verificar la regresión y constatar la ejecución funcional de extremo a extremo por parte de un subagente tester aislado.
*   **3.3** La condición no verificable (ausencia de dependencias, GUI, carencia de entorno) apertura automáticamente una tarea técnica de aprovisionamiento de entorno en WBS, sin computar jamás como aceptada.
*   **3.4** Los críticos LLM operan con carácter estrictamente consultivo sobre el diff sin sesgo de contexto acumulado; un hallazgo solo detiene la integración si acompaña una prueba de reproducción ejecutable.
*   **3.5** Strengthen valida los casos de prueba mediante criterios computacionales objetivos: compilación limpia, superación reiterada e incremento demostrable de cobertura o muerte de mutantes.

### PLANO 4 Control: límites en código, medidos en trabajo registrado
*(Resuelve D3. Evidencia: E3. Fundamentado en PR-OBS-PRG-2)*
*   **4.1** La identidad de cada unidad se deriva formalmente de la entidad (nodo o historia), no de texto libre; intentos y presupuestos se computan contra dicho identificador unívoco.
*   **4.2** El ledger registra contadores de estancamiento computando eventos de resultado o cambios en el checklist; alcanzado el umbral, se re-planifica reiniciando el contexto del ejecutor con topes prefijados.
*   **4.3** El número máximo de nodos por plan y slice se parametriza en `admission_policy.yaml` y se valida deterministamente en el motor Go.
*   **4.4** El presupuesto remanente se expone en la ficha de objetivo; al agotarse, una política determinista opta entre re-enrutar o emitir informe de cierre, consultando al usuario únicamente si su política explícita lo prevé.
*   **4.5** Modelos avanzados como SMC/Kev se integran una vez consolidados los sensores de eventos del Plano 2, operando transitoriamente con contadores deterministas de ciclos.

### PLANO 5 Flujo y topología: un escritor por ámbito, ceremonia proporcional
*(Resuelve D5. Evidencia: E5 y E6)*
*   **5.1** La topología se ajusta a la dimensión del requerimiento: ajustes acotados asignados a un único ejecutor; características integradas al bucle de slice; productos nuevos inician con esqueleto ejecutable expandiendo historias progresivamente.
*   **5.2** El ciclo de vertical slice adopta una secuencia fija: especificación ligera, pruebas de aceptación, ejecución con checks computacionales locales, validación E2E, revisión consultiva y commit.
*   **5.3** Un único escritor por ámbito de archivos con paralelismo circunscrito a vertical slices con dominios disjuntos. Roles como PM, architect y críticos actúan como asesores de solo lectura bajo demanda.
*   **5.4** El alcance queda congelado al iniciar el vertical slice; hallazgos técnicos sobrevenidos se registran como elementos posteriores en el backlog sin alterar historias en curso.
*   **5.5** Los bloqueos de escritura no interfieren con operaciones de inspección; las consultas globales se delegan a subagentes livianos de solo lectura con alcance acotado.

### PLANO 6 Autonomía: frontera técnica en lugar de preguntas
*(Resuelve D4. Evidencia: E7)*
*   **6.1** El confinamiento en sandbox (aislamiento de sistema de archivos y red restringida con allowlist) delimita la frontera de seguridad, habilitando shell de inspección, instalación local y operaciones git desatendidas.
*   **6.2** Cada característica validada computacionalmente genera un commit automático en la rama de trabajo que sirve de punto seguro de reversión (rollback).
*   **6.3** La interacción humana se reserva exclusivamente a traspasos de frontera de seguridad, acciones irreversibles en infraestructura o definiciones de valor reservadas al criterio del usuario.
*   **6.4** Toda denegación emitida por el harness se contabiliza como defecto en la telemetría e incorpora en su respuesta la alternativa válida aplicable (STR-14).

### 10 Cobertura y absorción de hallazgos

Todos los hallazgos confirmados o plausibles de la auditoría quedan resueltos por los invariantes de los seis planos en el diseño objetivo (P20 y P22 fueron refutados durante la verificación y se excluyen). La absorción describe el contrato objetivo, no el estado implementado: el catálogo y el harness actuales conservan los defectos citados como evidencia hasta la etapa del §12 que los migra. Ningún hallazgo requiere un parche ad-hoc en la prosa.

| PLANO | HALLAZGOS ABSORBIDOS | MECANISMO ESTRUCTURAL DE SUSTITUCIÓN |
| :--- | :--- | :--- |
| **1 Estado y contexto** | H1, H9, H10, H14, H15, P4, P5, P15 | Contrato de proyección mínima y ficha recitada invariante. Recuperación por ID, carga única de skills y unificación estricta de nombres de herramientas. |
| **2 Sensores** | H2, P1, P2, P3, P13, P14 | Eventos de resultado tipados en bus/historial, Finish sin éxito por defecto y comprobaciones computacionales dentro del bucle interno del ejecutor. |
| **3 Oráculos** | P8, P17 | Definición de hecho respaldada por ejecución; enrutamiento único de fallos; par ciego condicionado a evidencia de divergencia; reclasificación de unverified como tarea de entorno técnica. |
| **4 Control** | H6, H7, P7, P9 | Identidad ligada a entidades unívocas, presupuesto de iteraciones de corrección por slice, topes de admisión en Go y políticas deterministas de cierre de presupuesto sin escalado obligatorio. |
| **5 Flujo** | H8, H11, H12, P6, P10, P11, P12, P16, P18, P19, P21 | Topología según magnitud, ciclo de slice estandarizado, un escritor por ámbito con asesores de solo lectura, alcance congelado y lecturas no bloqueadas. |
| **6 Autonomía** | H3, H4, H5, H13, H16 | Confinamiento en sandbox con frontera clara, commit por característica verificada y denegaciones informativas que indican la ruta válida. |

---

## 11 Decisiones de producto y gobernanza

La evidencia técnica identifica alternativas sobre componentes del diseño actual cuya definición corresponde a gobernanza y decisiones de producto:

*   **Profundidad de la especificación SDD preliminar:** La práctica actual genera PRD, arquitectura, especificaciones y WBS de forma global y serial antes del primer vertical slice. Investigaciones sobre sobre-análisis y reportes de producción sugieren planificar hasta la profundidad del primer slice ejecutable y expandir de forma incremental historia a historia, manteniendo al rol architect como revisor consultivo de coherencia global.
*   **Concurrencia de escritores:** La arquitectura previa admitía hasta cuatro especialistas concurrentes en fase de implementación. La evidencia favorece un único escritor por ámbito de código, preservando el valor de los especialistas en roles de desacoplamiento de pruebas, diseño arquitectónico previo o revisión de diffs sin contexto contaminado.
*   **Rol de los críticos LLM frente a compuertas de verificación:** El debate entre modelos como mecanismo de compuerta tiende a converger en patrones de votación con dispersión de criterio. Se plantea que la compuerta de aceptación repose en ejecución computacional, reservando a los críticos LLM un rol consultivo no bloqueante salvo entrega de prueba de reproducción ejecutable.
*   **Secuencia de integración de SMC y Kev:** El modelado de control por modos deslizantes requiere como requisito indispensable la existencia previa de eventos de resultado tipados en el bus de telemetría. Se plantea consolidar inicialmente los sensores computacionales y contadores deterministas antes de desplegar superficies complejas de control.

---

## 12 Plan de migración secuencial

El orden de implementación responde estrictamente a dependencias funcionales: el control determinista requiere sensores de resultado previos, y la optimización de flujo requiere una línea base de referencia. Cada etapa establece una condición de salida cuantitativa basada en trabajo registrado. Los umbrales son relativos a la línea base E0, se miden sobre sus dos escenarios con ambas familias de modelos y la etapa se acepta solo si se cumplen todas sus condiciones:

*   **E0 - Línea base medible:** Definición de dos escenarios objetivos de referencia (cambio acotado y MVP 2D) evaluados con dos familias de modelos. Métricas, cada una agregada por escenario y familia de modelos sobre la sesión completa: tokens por tarea; tokens del prompt estático del orquestador; ocupación de contexto en el orquestador por dispatch; consultas interactivas por sesión, distinguiendo las que corresponden a cruces de frontera; tasa de características validadas con evidencia computacional; tasa de falsos completados, calculada como los nodos cerrados como completados cuya verificación independiente posterior falla o no existe divididos por el total de nodos cerrados como completados; fallos diferidos en E2E; estancamientos; nodos de DAG por característica entregada; y acciones registradas hasta el primer slice ejecutable, contadas como entradas del historial de ejecución previas a la primera aceptación de slice; una ejecución sin ninguna aceptación de slice queda censurada para esta métrica, se reporta aparte y no entra en la comparación, con la misma regla en E0 y E4. Los valores de E0 resultantes son la línea base de las etapas siguientes. Como el bus aún no publica eventos de resultado tipados, E0 mide con instrumentación sobre el historial de ejecución y las transcripciones de sesión.
    *   *Condición de salida:* Métricas reproducibles de forma automatizada desde el historial de ejecución y las transcripciones.
*   **E1 - Planos 1 y 2: Proyección mínima y eventos tipados de resultado:** Implementación del contrato de proyección en el orquestador y registro de eventos de verificación en el bus y el historial de Go, mitigando la causa primaria de compactación prematura.
    *   *Condición de salida:* La carga por dispatch se independiza del historial de GC, todo nodo terminal registra un resultado tipado, ningún nodo se cierra como completado sin evidencia computacional asociada y las métricas de E0 se reproducen de forma automatizada desde el bus de eventos.
*   **E2 - Planos 3 y 6 en paralelo: Oráculos desacoplados y confinamiento en sandbox:** Aislamiento de tests de aceptación en modo solo lectura, integración de subagente tester E2E y delimitación de sandbox para operaciones de shell, dependencias y git.
    *   *Condición de salida:* Tasa de falsos completados a lo sumo el 50% de la de E0 y cero consultas al operador fuera de cruces de frontera.
*   **E3 - Plano 4: Control determinista:** Activación de contadores de estancamiento, topes de admisión en Go y políticas deterministas ante presupuestos agotados.
    *   *Condición de salida:* Eliminación de sesiones que excedan cotas nominales y resolución autónoma de agotamiento de presupuesto.
*   **E4 - Plano 5: Flujo y topología proporcional:** Estandarización de ciclos de vertical slice, asignación de un único escritor por ámbito y congelamiento formal de alcance por slice.
    *   *Condición de salida:* Nodos por característica entregada a lo sumo el 70% de E0 y menos acciones registradas hasta el primer slice ejecutable que en E0.
*   **E5 - Poda de catálogo y control avanzado (SMC):** Eliminación sistemática de reglas en prosa cuya observancia haya asumido el harness Go (STR-1) y despliegue de controladores SMC sobre telemetría consolidada.
    *   *Condición de salida:* Tokens del prompt estático del orquestador por debajo de E0 y tasa de características validadas con evidencia computacional no inferior a E0.

---

## 13 Fuentes y referencias verificadas

1. **ACAD** Cemri et al., "Why Do Multi-Agent LLM Systems Fail?" (MAST) arxiv.org/abs/2503.13657
2. **ACAD** Kim et al., "Towards a Science of Scaling Agent Systems", arxiv.org/abs/2512.08296 (SWE-bench evaluado en subconjuntos de 20 instancias)
3. **ACAD** Fourney et al., "Magentic-One" (Microsoft Research) arxiv.org/abs/2411.04468
4. **ACAD** Liu et al., "Budget-Aware Tool-Use Enables Effective Agent Scaling" (BATS) arxiv.org/abs/2511.17006
5. **ACAD** Lindenbauer et al. (JetBrains), "The Complexity Trap", arxiv.org/abs/2508.21433. El resumen alarga la trayectoria ~13-15% según el modelo; Qwen3-32B constituye la excepción.
6. **ACAD** Yang et al., "SWE-agent" arxiv.org/abs/2405.15793. 90.5% corresponde al éxito de edición base, reduciéndose a 57.2% tras edición fallida.
7. **ACAD** Zhong, Raghunathan, Carlini, "ImpossibleBench", arxiv.org/abs/2510.20270
8. **ACAD** Xia et al., "Agentless", arxiv.org/abs/2407.01489. 27.0% alcanzado con suite de regresión, la votación aislada obtiene 25.67%.
9. **ACAD** Cuadron et al., "The Danger of Overthinking" arxiv.org/abs/2502.08235
10. **ACAD** Gloaguen et al. (ETH), "Evaluating AGENTS.md", arxiv.org/abs/2602.11988. No incrementa el éxito estadístico y eleva el coste >20%.
11. **ACAD** Huang et al., "LLMs Cannot Self-Correct Reasoning Yet" (Google DeepMind), arxiv.org/abs/2310.01798
12. **ACAD** "R2E-Gym", arxiv.org/abs/2504.07164
13. **ACAD** Du et al., "Context Length Alone Hurts LLM Performance Despite Perfect Retrieval", arxiv.org/abs/2510.05381
14. **ACAD** Meta, ACH: mutation-guided test generation, arxiv.org/abs/2501.12862 (73% aceptación en WhatsApp y Messenger)
15. **ACAD** METR, "Recent Frontier Models Are Reward Hacking" metr.org
16. **PRACT** Cognition, "Multi-Agents: What's Actually Working" cognition.com
17. **PRACT** Cognition, "Don't Build Multi-Agents" cognition.com
18. **PRACT** Cursor, "Towards self-driving codebases", cursor.com
19. **PRACT** Replit, "Enabling Agent 3 to Self-Test at Scale"
20. **PRACT** Factory, "Evaluating context compression", factory.ai
21. **PRACT** Manus, "Context Engineering for AI Agents", manus.im
22. **PRACT** Böckeler, "Understanding Spec-Driven Development" martinfowler.com
23. **PRACT** Böckeler, "Harness engineering for coding agent users", martinfowler.com
24. **PRACT** Cognition, "Devin's 2025 Performance Review" cognition.com
25. **LAB** Google Jules, Planning Critic (~9.5% fallos) jules.google
26. **PRACT** Aider, "Separating code reasoning and editing" aider.chat
27. **LAB** Anthropic, "Claude Code sandboxing", anthropic.com
28. **PRACT** Amp, "Handoff (No More Compaction)" ampcode.com
29. **PRACT** Kiro, "Run all tasks: the feature we refused to ship", kiro.dev. Despliegue supeditado a validación formal por tarea.
30. **PRACT** Cursor, "Continually improving our agent harness" cursor.com
31. **PRACT** LangChain, "Benchmarking Multi-Agent Architectures", langchain.com. Ganancia cercana al 50% evaluada en escenario específico.
32. **LAB** Anthropic, "Effective harnesses for long-running agents", anthropic.com
33. **PRACT** Thoughtworks Radar, "Spec-driven development" (Assess) thoughtworks.com
34. **PRACT** Chroma, "Context Rot" chroma.com
35. **PRACT** HumanLayer, "Advanced Context Engineering for Coding Agents", github.com/humanlayer
36. **LAB** Google, ADK context-aware multi-agent framework, developers.googleblog.com
37. **PRACT** Simon Willison, "Designing agentic loops" simonwillison.net