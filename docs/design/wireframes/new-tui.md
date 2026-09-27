# New TUI — qué se construye con qué

Base: Bubble Tea, Bubbles, Lip Gloss y Huh en **v2**, más Harmonica (animación). Un único tema oscuro. El logo ya está resuelto en código. Los paneles no llevan título en el borde.

`index.html` refleja este documento: cada pantalla está dibujada como la renderizarían estas librerías.

## 1. Composición y elementos (Lip Gloss + Bubbles)

| Elemento | Pantallas | Librería |
|---|---|---|
| Home: logo a la izquierda, menú a la derecha, "Takt AI" abajo | Home | Lip Gloss `JoinHorizontal` + `Place` |
| Home estrecho: logo arriba, menú debajo | Home 60×20 | Lip Gloss `JoinVertical` + `Place` |
| Cabecera: "Takt AI" a la izquierda, paso a la derecha | Install | Lip Gloss `JoinHorizontal` + `PlaceHorizontal` |
| Paneles con borde y fondo, sin título | Install | Lip Gloss `Border` + `Background`, compositor v2 para que el fondo no tenga huecos |
| Paneles centrados o a lo ancho | Install | Lip Gloss `Place` / `Width` |
| Menú del Home con `>` | Home | Lip Gloss |
| Colores por rol (foco, marcado, secundario, negrita) | Todas | Lip Gloss `Foreground`, `Bold` |
| Resaltado de warning / danger / success con fondo | Result, Existing files, Harnesses, Review bloqueado | Lip Gloss `Foreground` + `Background` |
| Barra de progreso `████░░░░  40%` | Installing | Bubbles `progress`, `WithFillCharacters('█','░')` |
| Spinner del paso en curso `⠋` | Installing | Bubbles `spinner.MiniDot` |
| Lista de pasos `✓` / `⠋` / pendiente | Installing | Lip Gloss `list` con enumerador propio |
| Terminales con menos colores o sin color | Todas | Lip Gloss v2, rebaja automática de color |

## 2. Mejoras que aportan las librerías

| Mejora | Pantallas | Librería |
|---|---|---|
| Barra de progreso animada, avanza suave | Installing | Bubbles `progress` `SetPercent` + Harmonica |
| Spinner animado en el paso activo | Installing | Bubbles `spinner` |
| Scroll cuando el contenido no cabe (`pgup`/`pgdown`) | Existing files, Review | Bubbles `viewport` |
| Indicador de página junto al scroll | Existing files, Review | Bubbles `paginator` |
| Modo accesible: salida lineal para lectores de pantalla y salida redirigida | Formularios | Huh `WithAccessible(true)` |
| Rutas clicables en terminales que lo soportan | Existing files, Result error | Lip Gloss v2 `Hyperlink` |

## 3. Ajustes de diseño para usar componentes directamente

| Ajuste | Pantallas | Componente |
|---|---|---|
| Nombre y descripción en dos líneas (`Key` multilínea, descripción pre-estilada en `text.muted`); la elección es la opción bajo `>` (sin `(x)`) | Setup | Huh `Select` |
| Descripción general arriba; cada opción con nombre y descripción en dos líneas (`Key` multilínea) | Components | Huh `MultiSelect` |
| Checklist sin botón Continue: Enter envía | Components, Harnesses | Huh `MultiSelect` |
| Error de validación debajo del campo con formato Huh (` * mensaje`) | Harnesses | Huh `MultiSelect` + `Validate` |
| Ruta como título, Source / Affects / aviso como descripción, opciones debajo; la elección es la opción bajo `>`; sin botón Continue; varios conflictos = un `Group` por archivo | Existing files | Huh `Select` + `Group` |
| Campo enfocado marcado con barra `┃` a la izquierda, dentro del panel | Setup, Components, Harnesses, Existing files | Huh `Theme` (`Focused.Base`) |
| Resumen etiqueta–valor en dos columnas | Review | Lip Gloss `table` sin bordes |
| Botones con fondo: solo el enfocado lleva relleno de color, resto sin relleno; sin `>` ni corchetes | Review, Result, Review bloqueado | Lip Gloss, estilos `FocusedButton` / `BlurredButton` del tema Huh |

## Tema Huh con tokens de brand

| Estilo Huh | Token |
|---|---|
| `Focused.Base` (barra `┃`) | `focus.ring` |
| `Title` | `text.primary`, negrita |
| `Description` | `text.muted` |
| `SelectSelector`, `MultiSelectSelector` (`> `) | `focus.ring` |
| `SelectedOption` | `focus.ring` (Select) · `success.fg` (MultiSelect) |
| `SelectedPrefix` / `UnselectedPrefix` | `[x] ` / `[ ] `, `text.muted` |
| `ErrorMessage` | `danger.fg` sobre `danger.bg` |
| `FocusedButton` | `bg.canvas` sobre `focus.ring` |
| `BlurredButton` | `text.secondary`, sin relleno |
