# Wireframes TUI — interfaz de trabajo

**Abrir [la galería visual](index.html).** El home principal es horizontal: logo a la izquierda, acciones a la derecha. Las vistas operativas usan controles y paneles, no una cabecera de logo repetida ni un informe de texto.

## Contrato de esta guía

| Campo | Definición |
|---|---|
| Superficie / versión | Patrones reutilizables TUI / 1.0 |
| Identidad base | [brand.md 3.0](../../../brand.md) |
| Entorno | Terminal de PC/laptop; composición implementable con Bubble Tea y Lip Gloss |
| Activo indicado por producto | [takt-ai.png](../../assets/brand/takt-ai.png), sin reemplazarlo por `takt-ai-logo-source.png` |
| Principios | B01–B08; identidad sin verbosidad ni repetición obligatoria del logo |
| Alcance | Guía de pantallas actuales y futuras, no inventario de funcionalidades implementadas |
| Idioma de los ejemplos | Textos reales del TUI (`takt/tui/tui.go`, `takt/tui/install/install.go`), en inglés como en el código; captions de esta guía en español |
| Estado | Home completo y flujo de instalación real, con sus variantes por decisión previa (Setup Default/Custom, Review Default/Custom, Result éxito/cancelado/error) |

## Elegir un patrón

| Necesidad | Wireframe | Patrón |
|---|---|---|
| Inicio del TUI (menú completo) | 01 estándar, 03 estrecho | L03 |
| Elegir setup: Default vs Custom | 04-setup-default, 15-setup-custom | L05 + selector único |
| Elegir componentes (setup Custom) | 05-components | L04 |
| Revisar antes de instalar: Default vs Custom | 06-review, 07-review-narrow | L05 |
| Instalando | 08-progress | L08 |
| Resultado: éxito | 09-success | L08 |
| Resultado: cancelado | 10-result-cancelled | L09 |
| Archivos existentes en conflicto | 11-conflicts | L07 |
| Resultado: error | 12-result-error | L09 |
| Harnesses: validación sin selección | 13-targets-invalid | Vacío recuperable |
| Revisión bloqueada (fallo al preparar el plan) | 14-review-blocked | Bloqueo con salida |

No añadir una pantalla si basta un estado del patrón existente. La cantidad de pantallas del catálogo no determina la cantidad de pasos del producto.

## Cómo implementarlo

1. Elegir el patrón según la tarea; consultar [interacción y estados](patterns.md).
2. Sustituir fixtures por objetos y capacidades reales, sin copiar afirmaciones que el sistema no pueda demostrar.
3. Componer según ancho **y alto** recibidos del terminal; ver [layouts](layouts.md).
4. Usar tokens de `brand.md`, no extraer colores del PNG para construir otra paleta.
5. Validar contenido largo, foco, resize y consecuencias antes de declarar conformidad.

### Qué representan los dibujos

- Paneles y rellenos: agrupación y estados traducibles a bordes/celdas de Lip Gloss; no tarjetas web con sombras.
- `>`: foco actual. `(x)` y `[x]`: elección persistente. El foco puede estar en un objeto distinto del elegido.
- Botones: acciones, no frases añadidas al contenido. Sin barra de teclas: foco, marcadores y botones hacen descubrible la acción.
- El PNG muestra el activo correcto y su composición. **No implica que el TUI renderice bitmaps**; la conversión a arte de terminal reconocible y el fallback ASCII siguen pendientes.
- Los marcos externos representan el viewport de terminal, no un borde de aplicación obligatorio.
- El selector de tema pertenece a esta documentación, no añade controles al producto.

## Archivos

| Archivo | Uso |
|---|---|
| [index.html](index.html) | Galería, editada directamente a mano — no hay paso de build |
| [screens.mjs](screens.mjs) | Fuente de las pantallas: posición en celdas, textos reales y paneles |
| [patterns.md](patterns.md) | Transiciones, teclado y estados |
| [layouts.md](layouts.md) | Reglas adaptativas, límites y evidencia pendiente |

`index.html` se edita directamente; `screens.mjs` es la referencia de qué debería decir cada pantalla, pero no genera el HTML automáticamente.

## Brechas conocidas

- `product/app/tui/VDS_TUI.md` conserva reglas de la identidad anterior sobre firma textual y columna izquierda. No son autoridad para reproducir esas restricciones; su migración no se realiza aquí.
- No se ha validado una variante terminal compacta del activo indicado. El fallback textual con estrellas del plugin OpenCode no se considera aprobación de una variante.
- El PNG conserva sus colores originales por instrucción de producto. La interfaz usa petróleo/mineral; no se recolorea el activo silenciosamente.
- Contenido dinámico (rutas de archivos, conteos, mensajes de error) usa ejemplos realistas, no literales del código — lo estático (labels, botones, encabezados) sí es literal, verificado contra `takt/tui/tui.go` y `takt/tui/install/install.go`.
- Esta entrega no modifica código del TUI.
