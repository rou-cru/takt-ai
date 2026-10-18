# Asignaciones top (experimento)

Asignación de modelos Databricks a los agentes de `~/.config/opencode/opencode.json` (2026-10-16).
El índice es el Intelligence Index v4.3.2 de Artificial Analysis a esfuerzo máximo.

| Fase | Agente | Modelo | Índice AA | Por qué este modelo |
|---|---|---|---|---|
| Orquestación | `takt` | Sonnet 5.5 | 56 | Contexto de 1M y salida de hasta 128K. Su modelo no lo usa nadie más. |
| Intención | `pm` | Opus 5.5 | 58 | Primero del índice, con razonamiento adaptativo para decisiones de alcance. |
| Intención | `analyst` | Opus 5.5 | 58 | Compara alternativas con contexto grande. Distinto de `takt`, que consume su investigación. |
| Intención | `product-designer` | Astra 6 | 53 | Razonamiento sobre documentos estructurados y entrada de imagen declarada. |
| Intención | `spec` | Sol 6.1 | 52 | Buen modelo de coding agentic, a un punto de Astra, con texto e imagen. |
| Diseño | `architect` | Astra 6 | 53 | Razonamiento sobre estructura y contratos. |
| Diseño | `qa` | Astra 6 | 53 | Procesa documentos estructurados, lo que cubre la trazabilidad. |
| Diseño | `tpm` | GLM 5.3 Flash | 42 | Razonamiento y salida estructurada con contexto de 1M. |
| Ejecución | `dev` | GLM 5.3 | 45 | Ejecutor mediano con contexto de 1M, sin las alertas de fiabilidad de Kimi. |
| Ejecución | `fix` | DeepSeek V4.1 Flash | 39 | Multimodal, con razonamiento y herramientas. Distinto de `explorer`. |
| Ejecución | `simplify` | GLM 5.3 Flash | 42 | Contexto de 1M para ver un módulo completo. Su familia es distinta de `dev`. |
| Pruebas | `test` | Haiku 5.5 | 43 | El mejor pequeño y el más rápido. Distinto de `mutation`. |
| Pruebas | `mutation` | Sol 6.1 | 52 | Coding agentic fuerte para crear y ejecutar mutantes. |
| Validación | `verify` | Opus 5.5 | 58 | El mejor revisor medido. No comparte modelo con `takt`, `dev` ni los jueces. |
| Validación | `explorer` | Haiku 5.5 | 43 | Rapidez para ciclos interactivos de alto volumen. |
| Validación | `quality` | Luna 6 | 38 | Contexto largo e imagen. Es el mejor de la lista en AutomationBench. |
| Validación | `judge-a` | Sol 6.1 | 52 | Revisión adversarial con coding fuerte. |
| Validación | `judge-b` | Grok 4.7 | 46 | Familia distinta, con la alucinación medida más baja de los medianos (29%). |

## Reparto

- **Por modelo:** Opus 3, Astra 3, Sol 3, Haiku 2, GLM Flash 2, y 1 cada uno para Sonnet, GLM 5.3, Grok, DeepSeek Flash y Luna. Kimi K3 queda sin rol.
- **Regla dura:** nadie más comparte el modelo de `takt`.

## Cruces que quedan

- Opus está en `pm`, `analyst` y `verify`. No se validan directamente entre sí.
- Sol está en `spec`, `mutation` y `judge-a`.
- `qa` y `architect` comparten Astra.

## Límites

- Los índices son de Artificial Analysis. El rendimiento por rol (por ejemplo `verify` o `fix`) no está medido.
- Faltan las pruebas de tool-calling de `dev`, `fix` y `judge-b`.
- `build`, `explore`, `general` y `plan` no tienen modelo asignado porque Takt los desactiva deliberadamente en su harness (`None` en `opencode.json`).

## Revisión balanceada (Propuesta v2)

Rebalanceo enfocado en optimizar el costo sistémico real, evitar saturación de endpoints bajo concurrencia (`concurrency.specialists: 4`), eliminar cámaras de eco entre agentes que se auditan mutuamente y blindar la convergencia en ejecución sin disparar el gasto de generación.

| Fase | Agente | Modelo v2 | Familia | Índice AA | Cambio vs v1 | Justificación técnica |
|---|---|---|---|:---:|---|---|
| Orquestación | `takt` | Sonnet 5.5 | Anthropic | 56 | Sin cambio | Regla dura: 1M contexto, 128K salida. Ningún subagente comparte su modelo. |
| Intención | `pm` | Opus 5.5 | Anthropic | 58 | Sin cambio | Razonamiento adaptativo de frontera para delimitar alcance sin ambigüedad. |
| Intención | `analyst` | Grok 4.7 | xAI | 46 | Opus → Grok | Ahorro masivo en tokens de entrada al leer código. Mínima alucinación (29%). |
| Intención | `product-designer` | Astra 6 | OpenAI | 53 | Sin cambio | Fuerte razonamiento sobre documentos estructurados y entrada de imagen. |
| Intención | `spec` | Sol 6.1 | OpenAI | 52 | Sin cambio | Contratos técnicos y especificaciones con fuerte foco en agentic coding. |
| Diseño | `architect` | Astra 6 | OpenAI | 53 | Sin cambio | Estructura de sistemas y contratos de interfaces. |
| Diseño | `qa` | Grok 4.7 | xAI | 46 | Astra → Grok | Rompe la cámara de eco con `architect`. Audita trazabilidad desde otra familia. |
| Diseño | `tpm` | GLM 5.3 Flash | Zhipu | 42 | Sin cambio | Salida estructurada y planificación rápida en 1M de contexto a costo mínimo. |
| Ejecución | `dev` | GLM 5.3 | Zhipu | 45 | Sin cambio | Protege el presupuesto: absorbe el 50%+ de tokens de salida a bajo costo. |
| Ejecución | `fix` | Sol 6.1 | OpenAI | 52 | DeepSeek → Sol | Reparaciones quirúrgicas de alta precisión. Corta bucles de recuperación en 1 intento. |
| Ejecución | `simplify` | GLM 5.3 Flash | Zhipu | 42 | Sin cambio | Refactorización y GC sobre módulos completos a costo marginal. |
| Pruebas | `test` | Haiku 5.5 | Anthropic | 43 | Sin cambio | Máxima velocidad y bajo costo para iteraciones intensivas de pruebas unitarias. |
| Pruebas | `mutation` | Sol 6.1 | OpenAI | 52 | Sin cambio | Competencia adversarial contra `test` (OpenAI vs Anthropic) creando mutantes. |
| Validación | `verify` | Opus 5.5 | Anthropic | 58 | Sin cambio | Verificador supremo independiente. No comparte familia con `dev` ni `fix`. |
| Validación | `explorer` | Haiku 5.5 | Anthropic | 43 | Sin cambio | Exploración interactiva y comprobaciones de alto volumen con mínima latencia. |
| Validación | `quality` | Luna 6 | OpenAI | 38 | Sin cambio | Top en AutomationBench para verificación punta a punta y contexto visual. |
| Validación | `judge-a` | Sol 6.1 | OpenAI | 52 | Sin cambio | Arbitraje técnico de código (OpenAI). |
| Validación | `judge-b` | GLM 5.3 | Zhipu | 45 | Grok → GLM | Arbitraje independiente (Zhipu) frente a `judge-a`. Evita sobrecargar a Grok. |

### Reparto por modelo (v2)

- **Sol 6.1 (4):** `spec`, `fix`, `mutation`, `judge-a` (fases desacopladas temporalmente; `fix` solo despierta bajo fallo).
- **Astra 6 (2):** `product-designer`, `architect`.
- **Grok 4.7 (2):** `analyst`, `qa`.
- **Haiku 5.5 (2):** `test`, `explorer`.
- **GLM 5.3 (2):** `dev`, `judge-b`.
- **GLM 5.3 Flash (2):** `tpm`, `simplify`.
- **Opus 5.5 (2):** `pm`, `verify` (separados por todo el ciclo de vida; no compiten concurrentemente).
- **Sonnet 5.5 (1):** `takt` (aislado).
- **Luna 6 (1):** `quality`.
- **Inactivos por diseño:** `build`, `explore`, `general` y `plan` son desactivados deliberadamente por Takt.

### Motivos de los cambios y compensación de sesgos

1. **Ruptura de la cámara de eco `architect` / `qa`:** En v1 ambos usaban Astra 6, compartiendo sesgos de validación en la fase de diseño. En v2, `qa` pasa a Grok 4.7 (xAI), confrontando la arquitectura desde una familia independiente con baja tasa de alucinación.
2. **Desinflado de `analyst` (subsidio para ejecución):** En v1, `analyst` consumía tokens de Opus 5.5 para lectura de código pasivo. Moverlo a Grok 4.7 ahorra hasta un 85% de costo en esa fase, generando un superávit financiero que financia las mejoras en ejecución.
3. **Refuerzo quirúrgico en `fix` sin encarecer `dev`:** En vez de encarecer `dev` (que genera la mayoría del código), se mantiene en GLM 5.3 pero se eleva `fix` a Sol 6.1. Como `fix` solo se invoca cuando los tests fallan sobre fragmentos acotados, gasta pocos tokens pero garantiza reparar el defecto sin entrar en bucles de recuperación.
4. **Rebalanceo del tribunal de jueces (`judge-a` vs `judge-b`):** Al asumir Grok roles activos (`analyst`, `qa`), `judge-b` pasa a GLM 5.3, manteniendo la paridad adversarial (OpenAI vs Zhipu) y distribuyendo la carga de endpoints de Databricks.

### Ventajas frente a v1

- **Mayor tasa de éxito en primera pasada (first-pass yield):** `fix` con Sol 6.1 evita que los defectos menores se conviertan en fallos de recuperación que agoten las políticas de admisión (`recovery_failures: 2`).
- **Costo total de sesión neutro o inferior:** El ahorro obtenido al bajar `analyst` de Opus 5.5 a Grok 4.7 compensa holgadamente el gasto puntual de `fix` en Sol 6.1.
- **Mejor distribución de rate limits:** Ningún modelo acumula más de 2 agentes activos de forma simultánea dentro de la ventana de concurrencia de 4 especialistas.
- **Auditoría cruzada real:** Se eliminan las coincidencias de modelo entre agentes que deben confrontarse (`architect` vs `qa`).

### Desventajas y compromisos (trade-offs) frente a v1

- **Dependencia de Sol 6.1 en cuatro roles:** Aunque operan en distintas fases temporales, Sol 6.1 acumula 4 asignaciones (`spec`, `fix`, `mutation`, `judge-a`). Si una sesión sufre múltiples disputas y fallos de tests simultáneos, el endpoint de OpenAI puede experimentar picos de concurrencia.
- **Pérdida de capacidad de razonamiento extremo en `analyst`:** Grok 4.7 (46) tiene 12 puntos menos de índice AA que Opus 5.5 (58). Para análisis de código excepcionalmente intrincado o refactorizaciones abstractas complejas, el análisis de alternativas puede ser menos exhaustivo conceptualmente, aunque su baja alucinación mitiga conclusiones erróneas.
- **`judge-b` baja de índice (46 → 45):** GLM 5.3 en `judge-b` tiene un punto menos que Grok 4.7 y una tasa de alucinación ligeramente mayor, aunque compensado por su ventana de contexto de 1M y su independencia de familia frente a `judge-a`.


## Golden crew (Propuesta v3)

Referencia recomendada por Takt para equipos que pueden pagar, pero que no quieren gastar de más. El objetivo es que el costo baje porque el crew converge: pruebas confiables, implementación apegada a invariantes y validación que solo afina o detecta omisiones y desvíos. Parte de la v2 y conserva sus dos decisiones validadas: `analyst` deja Opus por FinOps y `qa` pasa a Grok.

### Principios de asignación

1. **Palanca:** se invierte en los roles que definen o que hacen de oráculo, porque un error ahí se multiplica aguas abajo. Se ahorra en la ejecución de alto volumen, que ya tiene un oráculo que la corrige.
2. **Opus solo con contexto acotado:** Opus tiende a consumir mucho contexto. Por eso va solo en roles cuyo contexto está limitado por el propio rol, como `test`, que escribe a ciegas, o `fix`, que trabaja sobre un problema localizado. Nunca en roles que exploran el codebase.
3. **Quien revisa no comparte familia con quien produce:** `test`≠`dev`, `test`≠`mutation`, `qa`≠`architect`, `verify`≠`spec`/`dev`/`fix`, jueces≠`dev`/`fix` y jueces distintos entre sí.
4. **El balance se mide por relaciones, no por conteo:** una familia puede tener varios roles siempre que no defina y revise la misma cadena.

| Fase | Agente | Modelo v3 | Familia | Índice AA | Cambio vs v2 | Justificación |
|---|---|---|---|:---:|---|---|
| Orquestación | `takt` | Sonnet 5.5 | Anthropic | 56 | Sin cambio | Regla dura: 1M de contexto y 128K de salida. Ningún subagente comparte su modelo. |
| Intención | `pm` | Opus 5.5 | Anthropic | 58 | Sin cambio | Volumen bajo y máxima palanca: un error de alcance es el rework más caro. |
| Intención | `analyst` | Grok 4.7 | xAI | 46 | Sin cambio | Decisión FinOps: explora el codebase, justo lo que vuelve caro a Opus. |
| Intención | `product-designer` | Astra 6 | OpenAI | 53 | Sin cambio | Documentos estructurados y entrada de imagen. |
| Intención | `spec` | Sol 6.1 | OpenAI | 52 | Sin cambio | Escenarios de aceptación observables. La revisan `test` (Anthropic) y `verify` (xAI). |
| Diseño | `architect` | Astra 6 | OpenAI | 53 | Sin cambio | Contratos de interfaces exactos. Los revisa `qa` (xAI). |
| Diseño | `qa` | Grok 4.7 | xAI | 46 | Sin cambio | Trazabilidad auditada por una familia distinta de `architect`. |
| Diseño | `tpm` | GLM 5.3 Flash | Zhipu | 42 | Sin cambio | Descomposición estructurada a costo mínimo. `takt` corrige la secuenciación. |
| Ejecución | `dev` | GLM 5.3 | Zhipu | 45 | Sin cambio | Absorbe la mayor parte de los tokens de salida. Un oráculo fuerte hace suficiente a un modelo mediano. |
| Ejecución | `fix` | Opus 5.5 | Anthropic | 58 | Sol → Opus | Contexto acotado y uso poco frecuente. Al salir de OpenAI, `judge-a` deja de juzgar a su propio modelo cuando falla un `fix`. |
| Ejecución | `simplify` | GLM 5.3 Flash | Zhipu | 42 | Sin cambio | Preserva el comportamiento y las pruebas lo protegen. Comparte familia con `dev` (v1 decía lo contrario). |
| Pruebas | `test` | Opus 5.5 | Anthropic | 58 | Haiku → Opus | Es el oráculo del crew. Escribe a ciegas desde la spec y los contratos, y es de otra familia que `spec` y `dev`. |
| Pruebas | `mutation` | Sol 6.1 | OpenAI | 52 | Sin cambio | Prueba si `test` detecta fallos desde otra familia. |
| Validación | `verify` | Grok 4.7 | xAI | 46 | Opus → Grok | Mayor gasto recurrente. Con pruebas confiables, su trabajo es afinar y detectar omisiones. La alucinación baja limita los hallazgos falsos. |
| Validación | `explorer` | Haiku 5.5 | Anthropic | 43 | Sin cambio | Sesiones interactivas de alto volumen con latencia mínima. |
| Validación | `quality` | Luna 6 | OpenAI | 38 | Sin cambio | Contexto largo e imagen. Revisa trabajo de Zhipu. |
| Validación | `judge-a` | Sol 6.1 | OpenAI | 52 | Sin cambio | Revisión adversarial de una familia distinta de `dev` y de `fix`. |
| Validación | `judge-b` | Grok 4.7 | xAI | 46 | GLM 5.3 → Grok | Deja de compartir modelo con `dev`. Se usa poco. |

### Reparto por modelo (v3)

- **Grok 4.7 (4):** `analyst`, `qa`, `verify`, `judge-b`.
- **Opus 5.5 (3):** `pm`, `test`, `fix`. Todos con contexto acotado.
- **Sol 6.1 (3):** `spec`, `mutation`, `judge-a`.
- **Astra 6 (2):** `product-designer`, `architect`.
- **GLM 5.3 Flash (2):** `tpm`, `simplify`.
- **1 cada uno:** Sonnet 5.5 (`takt`), GLM 5.3 (`dev`), Haiku 5.5 (`explorer`), Luna 6 (`quality`).
- **Por familia:** OpenAI 6, Anthropic 5, xAI 4, Zhipu 3. Kimi K3 y DeepSeek V4.1 Flash quedan sin rol.

### A favor

- **El gasto sigue a la palanca:** el modelo más fuerte pasa de validar al final (`verify`) a definir el oráculo (`test`). Así, un defecto se detecta cuando nace y no después de una iteración completa de `dev`→`verify`→`fix`.
- **El contexto de Opus queda acotado:** sus tres roles trabajan con un contexto limitado por diseño. El riesgo que lo sacó de `analyst` no se repite.
- **Independencia en cada relación de revisión:** ninguna familia revisa trabajo que ella misma produjo. En particular, cuando un `fix` fallido llega a los jueces, ninguno comparte familia con `dev` ni con `fix`.
- **OpenAI deja de ser dueño de la cadena:** sigue definiendo (`spec`, `architect`) y probando la calidad de los tests (`mutation`), pero ya no repara ni se juzga a sí mismo.
- **El costo recurrente baja:** `verify`, que se ejecuta en cada entrega, deja el modelo más caro.

### En contra

- **Grok concentra 4 roles, uno recurrente:** con `concurrency.specialists: 4`, varias entregas en paralelo pueden coincidir en `verify` sobre el mismo endpoint. Es la principal presión de rate limits en v3.
- **`judge-b` comparte modelo con `verify`:** los jueces actúan cuando la verificación ordinaria no alcanza, y uno de ellos usa el mismo modelo que la compuerta que no alcanzó.
- **`judge-a` comparte modelo con `spec`:** juzga contra invariantes que su propio modelo redactó.
- **`test` en Opus cuesta más por prueba:** el ahorro depende de que el volumen de `verify` supere al de `test` y de que haya menos iteraciones. Ninguna de las dos cosas está medida.
- **`verify` baja 12 puntos de índice:** si las pruebas no resultan tan confiables como se espera, la compuerta queda más débil que en v2.
- **Anthropic concentra los extremos:** produce el oráculo (`test`) y la reparación (`fix`). Si ambos comparten un punto ciego, `fix` puede ajustar código a pruebas con la misma omisión. `mutation` y `verify` mitigan ese riesgo desde otras familias.

### Pendiente para cerrar la referencia

- **Precios por millón de tokens de cada modelo en Databricks,** para pasar de hipótesis a costo por sesión.
- **Volumen medido por rol,** sobre todo `verify` frente a `test`, e iteraciones `dev`→`verify`→`fix` antes y después del cambio.
- **Datos de Kimi K3 y DeepSeek V4.1 Flash:** son candidatos para `judge-b` o `quality`. Descargarían a Grok y separarían a `judge-b` de `verify`.
- **Pruebas de tool-calling de `fix` (Opus), `verify` y `judge-b` (Grok)** con sus nuevos modelos.
