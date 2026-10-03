# Harness SMC para Orquestador de Agentes

## Objetivo

Proveer un arnés determinista basado en Sliding Mode Control (SMC) que gestione la salud del orquestador de agentes, previniendo degradación por entropía del contexto, bucles de decisión y violaciones de invariantes.  El arnés actúa como un controlador híbrido que combina reglas duras (SMC) con un evaluador semántico asíncrono para decidir acciones de corrección, compactación y handoff. [alphacorp](https://alphacorp.ai/blog/what-is-context-rot-why-bigger-ai-context-windows-make-models-worse)

## Alcance

- **Incluye**:
  - Definición formal de la superficie de deslizamiento \(s(t)\) y métricas de salud.
  - Ley de alcance (reaching law) y ley de control del arnés.
  - Integración con el evaluador semántico para transiciones de fase controladas.
  - Mecanismos de mitigación de chattering y escalado de acciones.

- **Excluye**:
  - Implementación del evaluador semántico (solo interfaz y triggers).
  - Lógica interna de los especialistas o herramientas del orquestador.

## Definiciones y Variables

### Estado del Sistema

El estado del orquestador se define como:

\[
x(t) = 
\begin{bmatrix}
x_1(t) \\ x_2(t) \\ x_3(t) \\ x_4(t)
\end{bmatrix}
=
\begin{bmatrix}
\text{uso\_ventana}(t) \\
\text{ratio\_llamadas\_repetidas}(t) \\
\text{violaciones\_esquema}(t) \\
\text{frecuencia\_rechazos}(t)
\end{bmatrix}
\]

- \(x_1(t)\): Uso de ventana de contexto, normalizado en \([0,1]\). [alphacorp](https://alphacorp.ai/blog/what-is-context-rot-why-bigger-ai-context-windows-make-models-worse)
- \(x_2(t)\): Ratio de llamadas a herramientas repetidas en una ventana deslizante. 
- \(x_3(t)\): Tasa de violaciones de esquemas JSON por llamada. 
- \(x_4(t)\): Frecuencia de rechazos o errores de ejecución. [presba](https://presba.com/research/context-degradation.html)

### Referencias y Pesos

- Vector de referencia: \(x_{\text{ref}} = [x_{1,\text{ref}}, x_{2,\text{ref}}, x_{3,\text{ref}}, x_{4,\text{ref}}]^\top\)
  - Valores típicos: \(x_{1,\text{ref}} = 0.75\), \(x_{2,\text{ref}} = 0.1\), \(x_{3,\text{ref}} = 0\), \(x_{4,\text{ref}} = 0.05\).
- Vector de pesos: \(C = [c_1, c_2, c_3, c_4]^\top\), donde \(c_i > 0\) refleja la severidad de cada métrica.

### Superficie de Deslizamiento

\[
s(t) = C^\top \bigl(x(t) - x_{\text{ref}}\bigr)
= \sum_{i=1}^4 c_i \bigl(x_i(t) - x_{i,\text{ref}}\bigr)
\]

- Condición de salud: \(s(t) \le 0\).
- Condición de riesgo: \(s(t) > 0\).

## Ley de Alcance y Control

### Ley de Alcance

Se impone la dinámica:

\[
\dot{s}(t) = -\eta \, \operatorname{sign}\bigl(s(t)\bigr) - k \, s(t)
\]

- \(\eta > 0\): ganancia de acción discontinua (robustez).
- \(k \ge 0\): ganancia exponencial (suaviza convergencia). [mathworks](https://www.mathworks.com/help/slcontrol/ug/design-sliding-mode-control-reaching-law.html)

Esto garantiza \(s(t)\,\dot{s}(t) < 0\) para \(s(t) \neq 0\), asegurando alcanzabilidad. [mathworks](https://www.mathworks.com/help/slcontrol/ug/design-sliding-mode-control-reaching-law.html)

### Ley de Control del Arnés

La acción de control \(u(t)\) se define como:

\[
u(t) = u_{\text{eq}}(t) + u_{\text{sw}}(t)
\]

- \(u_{\text{eq}}(t)\): políticas nominales de gestión de contexto.
- \(u_{\text{sw}}(t)\): componente conmutada para corrección.

Vector de acciones:

\[
u(t) = 
\begin{bmatrix}
u_1(t) \\ u_2(t) \\ u_3(t) \\ u_4(t)
\end{bmatrix}
=
\begin{bmatrix}
\text{política\_compactación}(t) \\
\text{límite\_llamadas\_repetidas}(t) \\
\text{tolerancia\_esquema}(t) \\
\text{umbral\_rechazo}(t)
\end{bmatrix}
\]

Ley conmutada:

\[
u_{\text{sw}}(t) = -K \, \operatorname{sign}\bigl(s(t)\bigr)
\]

- \(K\): vector de ganancias que determina intensidad de correcciones. [mathworks](https://www.mathworks.com/help/slcontrol/ug/design-smc-controller-for-robotic-manipulator.html)

### Mitigación de Chattering

Se introduce una capa límite \(\phi > 0\):

\[
\operatorname{sign}_\phi\bigl(s(t)\bigr) =
\begin{cases}
+1, & s(t) > \phi \\
\frac{s(t)}{\phi}, & |s(t)| \le \phi \\
-1, & s(t) < -\phi
\end{cases}
\]

Ley suavizada:

\[
u_{\text{sw}}(t) = -K \, \operatorname{sign}_\phi\bigl(s(t)\bigr)
\]

Esto reduce oscilaciones de alta frecuencia en las acciones del arnés. [hal](https://hal.science/hal-03131458/document)

## Integración con Evaluador Semántico

### Señal de Alerta

Se define una señal de alerta para el evaluador semántico:

\[
a(t) = f\bigl(s(t), \dot{s}(t), N_{\text{cruces}}(t), T_{>0}(t)\bigr)
\]

- \(N_{\text{cruces}}(t)\): número de cruces de \(s(t)\) por encima de umbral en ventana temporal.
- \(T_{>0}(t)\): tiempo acumulado con \(s(t) > 0\).

### Trigger de Transición de Fase

Cuando \(a(t) > \bar{a}\) (umbral configurado), el evaluador semántico activa:

1. Consolidación de estado (checkpoint).
2. Compactación de memoria.
3. Handoff a nueva instancia del orquestador. [digitalapplied](https://www.digitalapplied.com/blog/context-engineering-agent-reliability-playbook-2026)

## Requisitos Funcionales

| ID  | Requisito | Prioridad |
|-----|-----------|-----------|
| RF1 | Calcular \(s(t)\) en tiempo real para cada llamada a herramienta. | Crítica |
| RF2 | Aplicar ley de control \(u(t)\) cuando \(s(t) > 0\). | Crítica |
| RF3 | Mitigar chattering con capa límite \(\phi\). | Alta |
| RF4 | Generar señal de alerta \(a(t)\) para el evaluador semántico. | Alta |
| RF5 | Soportar handoff controlado al superar umbral \(\bar{a}\). | Alta |

## Requisitos No Funcionales

| ID  | Requisito | Prioridad |
|-----|-----------|-----------|
| RNF1 | Latencia de cálculo de \(s(t)\) < 10 ms por llamada. | Crítica |
| RNF2 | Disponibilidad del arnés ≥ 99.9%. | Crítica |
| RNF3 | Configuración de parámetros (\(C\), \(x_{\text{ref}}\), \(\eta\), \(k\), \(\phi\)) vía archivo YAML. | Media |
| RNF4 | Logs estructurados (JSON) de \(x(t)\), \(s(t)\), \(u(t)\) para auditoría. | Alta |

## Pseudocódigo del Arnés

```python
def arnes_step(x_t, x_ref, C, eta, k, K, phi):
    # Calcular superficie de deslizamiento
    s_t = sum(c_i * (x_i - x_ref_i) for c_i, x_i, x_ref_i in zip(C, x_t, x_ref))
    
    # Ley de alcance (para monitoreo)
    ds_dt = -eta * sign(s_t) - k * s_t
    
    # Mitigación de chattering
    if s_t > phi:
        sign_phi = 1
    elif s_t < -phi:
        sign_phi = -1
    else:
        sign_phi = s_t / phi
    
    # Acción de control conmutada
    u_sw = [-K_i * sign_phi for K_i in K]
    
    # Acción de control equivalente (políticas nominales)
    u_eq = politicas_nominales()
    
    # Acción total
    u_t = [u_eq_i + u_sw_i for u_eq_i, u_sw_i in zip(u_eq, u_sw)]
    
    # Señal de alerta para evaluador semántico
    a_t = calcular_alerta(s_t, ds_dt, N_cruces, T_mayor_0)
    
    return u_t, s_t, a_t
```

## Riesgos y Mitigaciones

| Riesgo | Mitigación |
|--------|------------|
| Chattering del arnés | Capa límite \(\phi\), escalar a evaluador semántico tras 2-3 bloqueos consecutivos.  [hal](https://hal.science/hal-03131458/document) |
| Meta-degradación del evaluador | Entradas ultra-estructuradas (JSONs, métricas), sin leer prompt completo.  |
| Handoff loss | Guardar estado explícito de especialistas, no solo del orquestador.  [digitalapplied](https://www.digitalapplied.com/blog/context-engineering-agent-reliability-playbook-2026) |
| Sobrecoste computacional | Arnés a frecuencia alta (1:1), evaluador asíncrono por eventos de umbral.  |