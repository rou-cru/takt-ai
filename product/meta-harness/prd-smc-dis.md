Aquí tienes el replanteamiento del Harness SMC en **tiempo discreto por eventos de trabajo** (\(k \in \mathbb{N}\)), manteniendo la estructura formal pero adaptando la dinámica a un sistema muestreado y orientado a eventos (cada llamada a herramienta, decisión o handoff es un evento \(k\)).

***

# Harness SMC Discreto para Orquestador de Agentes

## Objetivo

Proveer un arnés determinista basado en **Sliding Mode Control (SMC) discreto** que gestione la salud del orquestador de agentes en eventos de trabajo \(k\), previniendo degradación por entropía del contexto, bucles de decisión y violaciones de invariantes. El arnés actúa como un controlador híbrido que combina reglas duras (SMC) con un evaluador semántico asíncrono para decidir acciones de corrección, compactación y handoff. [alphacorp][digitalapplied]

## Alcance

- **Incluye**:
  - Definición formal de la superficie de deslizamiento \(s[k]\) y métricas de salud en tiempo discreto.
  - Ley de alcance discreta (discrete reaching law) y ley de control del arnés.
  - Integración con el evaluador semántico para transiciones de fase controladas por eventos.
  - Mecanismos de mitigación de chattering numérico y escalado de acciones.

- **Excluye**:
  - Implementación del evaluador semántico (solo interfaz y triggers).
  - Lógica interna de los especialistas o herramientas del orquestador.

## Definiciones y Variables

### Estado del Sistema (Eventos \(k\))

El estado del orquestador se define en el evento \(k \in \mathbb{N}\):

\[
x[k] = 
\begin{bmatrix}
x_1[k] \\ x_2[k] \\ x_3[k] \\ x_4[k]
\end{bmatrix}
=
\begin{bmatrix}
\text{uso\_ventana}[k] \\
\text{ratio\_llamadas\_repetidas}[k] \\
\text{violaciones\_esquema}[k] \\
\text{frecuencia\_rechazos}[k]
\end{bmatrix}
\]

- \(x_1[k]\): Uso de ventana de contexto, normalizado en \([0,1]\). [alphacorp]
- \(x_2[k]\): Ratio de llamadas a herramientas repetidas en una ventana deslizante de eventos.
- \(x_3[k]\): Tasa de violaciones de esquemas JSON por evento.
- \(x_4[k]\): Frecuencia de rechazos o errores de ejecución. [presba]

### Incremento de Estado

\[
\Delta x[k] = x[k] - x[k-1]
\]

### Referencias y Pesos

- Vector de referencia: \(x_{\text{ref}} = [x_{1,\text{ref}}, x_{2,\text{ref}}, x_{3,\text{ref}}, x_{4,\text{ref}}]^\top\)
  - Valores típicos: \(x_{1,\text{ref}} = 0.75\), \(x_{2,\text{ref}} = 0.1\), \(x_{3,\text{ref}} = 0\), \(x_{4,\text{ref}} = 0.05\).
- Vector de pesos: \(C = [c_1, c_2, c_3, c_4]^\top\), donde \(c_i > 0\) refleja la severidad de cada métrica.

### Superficie de Deslizamiento Discreta

\[
s[k] = C^\top \bigl(x[k] - x_{\text{ref}}\bigr)
= \sum_{i=1}^4 c_i \bigl(x_i[k] - x_{i,\text{ref}}\bigr)
\]

- Condición de salud: \(s[k] \le 0\).
- Condición de riesgo: \(s[k] > 0\).

### Incremento de la Superficie

\[
\Delta s[k] = s[k] - s[k-1]
\]

## Ley de Alcance y Control Discreta

### Ley de Alcance Discreta

Se impone la dinámica de alcance en el dominio discreto:

\[
s[k+1] - s[k] = -\eta \, \operatorname{sign}\bigl(s[k]\bigr) - k_s \, s[k]
\]

o equivalentemente:

\[
s[k+1] = (1 - k_s) \, s[k] - \eta \, \operatorname{sign}\bigl(s[k]\bigr)
\]

- \(\eta > 0\): ganancia de acción discontinua (robustez).
- \(k_s \ge 0\): ganancia exponencial (suaviza convergencia, \(0 \le k_s < 1\) para estabilidad). [mathworks][mdpi]

Condición de alcanzabilidad discreta:

\[
\bigl(s[k+1] - s[k]\bigr) \cdot \operatorname{sign}\bigl(s[k]\bigr) < 0 \quad \text{y} \quad \bigl(s[k+1] + s[k]\bigr) \cdot \operatorname{sign}\bigl(s[k]\bigr) \ge 0
\]

Esto garantiza convergencia cuasi-deslizante en tiempo discreto. [mdpi][ucl]

### Ley de Control del Arnés Discreto

La acción de control \(u[k]\) se define como:

\[
u[k] = u_{\text{eq}}[k] + u_{\text{sw}}[k]
\]

- \(u_{\text{eq}}[k]\): políticas nominales de gestión de contexto.
- \(u_{\text{sw}}[k]\): componente conmutada para corrección.

Vector de acciones:

\[
u[k] = 
\begin{bmatrix}
u_1[k] \\ u_2[k] \\ u_3[k] \\ u_4[k]
\end{bmatrix}
=
\begin{bmatrix}
\text{política\_compactación}[k] \\
\text{límite\_llamadas\_repetidas}[k] \\
\text{tolerancia\_esquema}[k] \\
\text{umbral\_rechazo}[k]
\end{bmatrix}
\]

Ley conmutada discreta:

\[
u_{\text{sw}}[k] = -K \, \operatorname{sign}\bigl(s[k]\bigr)
\]

- \(K\): vector de ganancias que determina intensidad de correcciones. [mathworks]

### Mitigación de Chattering Numérico

Se introduce una capa límite \(\phi > 0\) con función de saturación:

\[
\operatorname{sat}_\phi\bigl(s[k]\bigr) =
\begin{cases}
+1, & s[k] > \phi \\
\frac{s[k]}{\phi}, & |s[k]| \le \phi \\
-1, & s[k] < -\phi
\end{cases}
\]

Ley suavizada discreta:

\[
u_{\text{sw}}[k] = -K \, \operatorname{sat}_\phi\bigl(s[k]\bigr)
\]

Alternativamente, se puede usar una **ley de alcance de potencia múltiple** para reducir chattering:

\[
s[k+1] = (1 - q T_s) s[k] - k_1 |s[k]|^\alpha \operatorname{sign}(s[k]) - k_2 |s[k]|^\beta \operatorname{sign}(s[k])
\]

con \(0 < \alpha < 1 < \beta\), lo que acelera convergencia lejos de la superficie y la ralentiza cerca de ella. [scispace][mdpi]

## Integración con Evaluador Semántico

### Señal de Alerta por Eventos

Se define una señal de alerta para el evaluador semántico basada en eventos:

\[
a[k] = f\bigl(s[k], \Delta s[k], N_{\text{cruces}}[k], T_{>0}[k]\bigr)
\]

- \(N_{\text{cruces}}[k]\): número de cruces de \(s[k]\) por encima de umbral en ventana de eventos.
- \(T_{>0}[k]\): conteo acumulado de eventos con \(s[k] > 0\).

### Trigger de Transición de Fase

Cuando \(a[k] > \bar{a}\) (umbral configurado), el evaluador semántico activa:

1. Consolidación de estado (checkpoint).
2. Compactación de memoria.
3. Handoff a nueva instancia del orquestador. [digitalapplied]

## Requisitos Funcionales

| ID  | Requisito | Prioridad |
|-----|-----------|-----------|
| RF1 | Calcular \(s[k]\) en cada evento \(k\) (llamada a herramienta). | Crítica |
| RF2 | Aplicar ley de control \(u[k]\) cuando \(s[k] > 0\). | Crítica |
| RF3 | Mitigar chattering numérico con capa límite \(\phi\) o ley de potencia. | Alta |
| RF4 | Generar señal de alerta \(a[k]\) para el evaluador semántico. | Alta |
| RF5 | Soportar handoff controlado al superar umbral \(\bar{a}\). | Alta |

## Requisitos No Funcionales

| ID  | Requisito | Prioridad |
|-----|-----------|-----------|
| RNF1 | Latencia de cálculo de \(s[k]\) < 5 ms por evento. | Crítica |
| RNF2 | Disponibilidad del arnés ≥ 99.9%. | Crítica |
| RNF3 | Configuración de parámetros (\(C\), \(x_{\text{ref}}\), \(\eta\), \(k_s\), \(\phi\)) vía archivo YAML. | Media |
| RNF4 | Logs estructurados (JSON) de \(x[k]\), \(s[k]\), \(u[k]\) para auditoría. | Alta |

## Pseudocódigo del Arnés Discreto

```python
def arnes_discrete_step(x_k, x_k_prev, x_ref, C, eta, k_s, K, phi):
    # Calcular superficie de deslizamiento
    s_k = sum(c_i * (x_i - x_ref_i) for c_i, x_i, x_ref_i in zip(C, x_k, x_ref))
    s_k_prev = sum(c_i * (x_i_prev - x_ref_i) for c_i, x_i_prev, x_ref_i in zip(C, x_k_prev, x_ref))
    
    # Incremento de superficie
    delta_s_k = s_k - s_k_prev
    
    # Ley de alcance (para monitoreo)
    # s_k_plus_1 = (1 - k_s) * s_k - eta * sign(s_k)
    
    # Mitigación de chattering (saturation)
    if s_k > phi:
        sat_phi = 1
    elif s_k < -phi:
        sat_phi = -1
    else:
        sat_phi = s_k / phi
    
    # Acción de control conmutada
    u_sw = [-K_i * sat_phi for K_i in K]
    
    # Acción de control equivalente (políticas nominales)
    u_eq = politicas_nominales()
    
    # Acción total
    u_k = [u_eq_i + u_sw_i for u_eq_i, u_sw_i in zip(u_eq, u_sw)]
    
    # Señal de alerta para evaluador semántico
    a_k = calcular_alerta_eventos(s_k, delta_s_k, N_cruces, T_mayor_0)
    
    return u_k, s_k, a_k
```

## Riesgos y Mitigaciones

| Riesgo | Mitigación |
|--------|------------|
| Chattering numérico | Capa límite \(\phi\), ley de potencia múltiple, o implementación implícita del SMC. [mdpi][inria] |
| Meta-degradación del evaluador | Entradas ultra-estructuradas (JSONs, métricas), sin leer prompt completo. |
| Handoff loss | Guardar estado explícito de especialistas, no solo del orquestador. [digitalapplied] |
| Sobrecoste computacional | Arnés a frecuencia 1:1 por evento, evaluador asíncrono por umbrales de \(a[k]\). |


### Notas de Diseño Discreto

1. **Estabilidad**: Para garantizar estabilidad en tiempo discreto, se requiere \(0 \le k_s < 1\) y \(\eta > 0\) suficientemente grande para dominar perturbaciones. [mdpi][ucl]
2. **Quasi-Sliding Mode**: En tiempo discreto, el sistema no permanece exactamente en \(s[k] = 0\), sino en una banda \(|s[k]| \le \epsilon\), donde \(\epsilon\) depende de \(\eta\), \(k_s\) y el periodo de muestreo (evento). [mdpi][scispace]
3. **Event-Triggered**: Este diseño es inherentemente *event-triggered*, ya que cada \(k\) corresponde a un evento de trabajo (llamada, decisión, error), no a un tiempo continuo muestreado. [sagepub][sciencedirect]