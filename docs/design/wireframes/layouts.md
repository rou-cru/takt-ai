# Composición adaptativa y verificación

**Diseñar para ancho útil, no para una columna por defecto.** La pantalla completa de un laptop es el contexto principal; el mismo usuario puede reducir el terminal a media pantalla. El layout debe responder a ambas situaciones sin perder el carácter de herramienta.

## Reglas de composición propuestas

Los umbrales siguientes son decisiones de esta guía, no capacidades ya implementadas. Se ajustarán con evidencia de render y del logo en celdas.

| Viewport real | Home | Operación |
|---|---|---|
| Desde 100 columnas, alto suficiente | Logo lateral y acciones; conjunto centrado | Lista–detalle o paneles de trabajo; mínimo 32 columnas de lista y 48 de detalle |
| 80–99 columnas | Home aún lateral si caben logo y controles | Lista o formulario con ancho acotado; detalle anidado, no dos paneles ilegibles |
| 60–79 columnas | Logo arriba y acciones agrupadas debajo | Panel principal, campos apilados y acciones legibles |
| Ancho amplio, 20–23 filas | Logo lateral menor y grupo compacto | Reducir separación vertical, paginar contenido; mantener acciones y consecuencias |
| Menos de 60 columnas o 20 filas | Aviso legible y alternativa documentada | No aplicar cambios por un resize ni inventar compatibilidad |

No inferir el viewport a partir del monitor físico. No cambiar el tamaño de la fuente del usuario. Si traducciones o nombres reales no caben, el cálculo de ajuste prevalece sobre el breakpoint nominal.

## Presupuestos de espacio

- **Home 120×32:** logo de referencia 34×16 celdas a la izquierda; acciones de 43 columnas a la derecha. Se centra el conjunto, no cada línea de texto. No rellenar el espacio restante con mensajes, estado irrelevante o estadísticas.
- **Home 80×24:** logo 26×12 y acciones de 37 columnas; conserva relación horizontal. No confundir el umbral de este home con el de lista–detalle.
- **Home 60×20:** logo 16×8 arriba, grupo de acciones de 32 columnas debajo. Esta escala necesita prueba de reconocimiento de su variante terminal; si falla, recomponer, no reemplazar por texto.
- **Operación 120×32:** contexto compacto en filas superiores; región de trabajo en paneles; acciones y ayuda al pie. El logo no consume filas.
- **120×40:** puede mantener un grupo acotado y equilibrado o mostrar más filas útiles; no estirar artificialmente cada separación ni llenar la altura con copy.
- **Resize:** conservar objeto, selección, borrador y foco. El contenido se redistribuye, no se reinicia el flujo.

Las posiciones exactas en `screens.mjs` son capturas de composiciones concretas. La implementación debe calcular posiciones y tamaños; no copiar coordenadas como layout universal.

## Traducción a Bubble Tea y Lip Gloss

| Elemento visual | Responsabilidad de implementación |
|---|---|
| Shell | Dimensiones desde resize; reservar acciones/ayuda; distribuir el área de contenido |
| Home lateral | Componer logo y controles horizontalmente; centrar el conjunto según espacio restante |
| Paneles | Bordes nativos, padding en celdas, ancho útil calculado descontando bordes y gaps |
| Lista | Foco propio, elección persistente separada y viewport para contenido largo |
| Formulario | Labels junto al control cuando caben; error asociado; Tab entre campos/regiones |
| Acciones | Controles diferenciados, foco local y orden estable; dividir en filas si no caben |
| Resultado | Grupo centrado y acotado; estado, consecuencia temporal y siguiente acción |
| Detalle/diff | Vista anidada o panel si cabe; retorno al objeto y scroll previos |

No se añaden dependencias ni componentes en esta entrega. La tabla describe responsabilidades, no prescribe nombres de APIs de librería.

## Contenido largo y estado

1. Medir ancho visible de terminal, no bytes ni longitud UTF-16; contemplar caracteres de ancho doble y combinaciones.
2. No truncar riesgos ni etiquetas de compromiso. Los nombres largos tienen detalle recuperable; paginar si hace falta.
3. Mantener acciones y ayuda fuera del viewport de contenido, sin superponerlos.
4. Al abrir detalle estrecho, conservar selección, filtro y posición de retorno.
5. Los paneles son agrupaciones funcionales. No crear uno por cada dato, ni anidar contenedores innecesarios.

## Tokens, temas y logo

`build.mjs` lee los colores de `brand.md`: no mantiene otra paleta. `bg.canvas`, `bg.surface`, `border.control`, `brand.ink`, `focus.ring` y roles semánticos de estado son las referencias estables.

El fondo del canvas se pinta una sola vez para todo el viewport (`--bg-canvas` en `.terminal`); ningún elemento reaplica un color por defecto encima de otro que ya tiene su propio rol. Cada texto visible lleva su rol de color propio (heading/content/secondary/muted/focus/selección/warning/danger/success), nunca depende de heredar el color del contenedor — así aparece la variedad de color por rol que `build.mjs` ya expresa en CSS (`.muted`, `.warning`, `.danger`, `.success`, `.focus`, `.brand` son colores distintos sobre el mismo `--bg-canvas`).

El PNG conserva su apariencia original. La galería no recolorea ni recorta el activo. Su representación en HTML es una referencia visual, no prueba de compatibilidad de bitmaps con terminal. Se requiere una variante terminal aprobada y su fallback; no sustituir el logo por estrellas y texto.

El filtro monocromo de la galería solo aproxima ausencia de color. No prueba un terminal ANSI, tema heredado o lector de pantalla. Las reglas de salida lineal accesible y CLI redirigida siguen siendo independientes del home gráfico.

## Evidencia y puerta de entrega

| Comprobación | Estado de esta entrega |
|---|---|
| Fixtures dentro de dimensiones declaradas | Automatizada por `build.mjs` |
| Texto sin colisión con otros textos, bordes o logo | Automatizada para el repertorio de glifos de fixtures |
| Exactamente un foco y logo en cada home | Automatizada |
| Galería reproducible / tokens derivados | `node docs/design/wireframes/build.mjs --check` |
| Render real de Bubble Tea/Lip Gloss | Pendiente; esta guía no modifica TUI |
| Reconocimiento de logo rasterizado/compacto | Pendiente, requiere activo terminal y revisión |
| Navegación real, resize, texto largo y lector | Pendiente de implementación |

Al implementar, cubrir V01–V17 de `brand.md` según aplicabilidad. Repetir home, selección, revisión, parcial y conflicto en estándar (120×32) y estrecho (60×20). Probar además claro, oscuro, monocromo, ASCII, nombres largos y retorno tras resize. No declarar aprobación del diseño por pasar únicamente un validador geométrico.
