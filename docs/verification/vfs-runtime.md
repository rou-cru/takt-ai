# VFS durable: avance y bloqueos de integración

**Implementación parcial del plan de integración; no habilita el release.** El
núcleo durable, la prueba de aislamiento macOS y la primera prueba vertical
(IPC de mutación + plugin OpenCode + viabilidad de captura shell) están
implementados. Ninguna entrega 0–5 se declara cerrada: todavía no existe el
recorrido Plugin → Go → proyección shell → gate → consolidación → aceptación →
commit ejecutado dentro de OpenCode real.

## Qué se puede ejecutar

```sh
# Núcleo, recuperación abrupta, identidad y correlación
 go test ./takt/vfs ./takt/session -count=1
 go test -race ./takt/vfs ./takt/session -count=1
 go test ./takt/vfs -run '^$' -fuzz FuzzCanonicalPaths -fuzztime=3s

# Prueba vertical del IPC de mutación: bind, create staged, denegación
# fuera de scope, inspect del verificador, veredicto, consolidación
 go test ./cmd/takt-ai -run TestVFSMutationIPC -count=1

# Prueba nativa de aislamiento y viabilidad de captura shell (no un
# ejecutor shell de producto)
 npm ci --prefix takt/runtime/sandbox --ignore-scripts
 npm test --prefix takt/runtime/sandbox

# Consulta offline de un store EXISTENTE creado por vfs.Open
 go run ./cmd/takt-ai vfs journal --workspace /ruta/workspace \
   --state /ruta/privada --session sesion --unit unidad --after -1 --limit 100

# Restauración explícita de un flush incompleto; no descarta staging
 go run ./cmd/takt-ai vfs recover --workspace /ruta/workspace \
   --state /ruta/privada --restore
```

Consulta y recuperación toman el mismo lock que el coordinador. Una segunda
instancia falla, incluso si intenta usar otro directorio de estado. Estas
operaciones son offline: no son IPC con un coordinador activo. No inicializan
un store nuevo cuando se escribe incorrectamente la ruta.

## IPC de mutación (primera prueba vertical)

`takt-ai vfs bind|op|verify|consolidate --workspace <dir> --state <dir>`
reciben JSON estricto por stdin con `ipc_version` obligatorio (rechazo explícito
ante versión distinta: resincronizar con `takt-ai setup sync`) y responden un
único JSON. Identidad (sesión/unidad/intento/agente/especialista/invariantes)
viaja por invocación; el rol se resuelve del catálogo canónico, nunca de los
argumentos. El lock del workspace serializa invocaciones concurrentes del
plugin. El store es privado por workspace:
`~/.local/share/takt-ai/vfs/<slug>-<sha256-8(workspace)>`.

## Recorrido OpenCode real (F1, 2026-09-13)

OpenCode 1.18.30, macOS 26.6.2 ARM64, workspace desechable instalado con
`setup install --targets opencode` bajo un HOME aislado. Recorrido ejecutado
íntegramente con las tools del plugin `takt-vfs`:

| Paso | Llamada | Resultado |
| --- | --- | --- |
| 1 | `vfs_bind` takt-dev scope `[notes/demo.md]` | binding + author_key |
| 2 | `vfs_write` notes/demo.md | staged revisión 1, delta_hash; disco intacto |
| 3 | `vfs_bind` verify scope vacío y author_key, en su propia unidad | binding del verificador |
| 4 | `vfs_verify` (author_key, revisión, delta_hash, pass) | `Verdict: passed` |
| 5 | `vfs_consolidate` checkpoint | archivo en disco (6 bytes) |

Journal correlacionado verificado: `create` (execution) → `verdict passed`
(verification) → `flush` con checkpoint; consulta offline por CLI sobre el
mismo store. Disco permaneció intacto hasta la consolidación en todos los
intentos; los veredictos fallidos nunca materializaron archivos.

Hallazgos del recorrido (corregidos en el plugin): el binding debe usar la
identidad del **especialista** (el gate anti-autoverificación compara agent
ids de autor y verificador); autor y verificador de un intento comparten
`attempt_id`; `author_key` y `delta_hash` se devuelven en la salida de las
tools para que el verificador referencie al autor; el sync reduplica el
plugin desde el binario (rebuild requerido tras editar el asset embebido).

Este recorrido acredita el avance B19 (creación verificada y consolidada con
trazabilidad), no cierra la entrega: falta el recorrido con verificador como
sesión de especialista independiente bajo control del harness (F2/F3). El
orquestador expresa intención de verificar; no emite ni controla el juicio.

**Relectura 2026-09-14:** la evidencia F1 es histórica, no un nuevo ensayo ni
prueba de independencia. El plugin actual deja al modelo elegir `specialist`
en `vfs_bind`, expone `author_key` y selecciona para `vfs_verify` otro binding
sin comprobar que pertenezca al actor que llama. Por tanto, dos bindings no
prueban dos especialistas independientes. Además usa `invariants_hash: "native"`,
no referencias reales del objetivo/contrato, y permite shell nativo sin captura.
Cerrar estos límites es trabajo de release en B16/B18–B22, no solo validación.
La [auditoría actual](../../release-backlog-audit.md) distingue estos huecos del
núcleo durable ya implementado.

## Límites de las APIs

| Superficie | Contrato implementado |
| --- | --- |
| `vfs.Open` | SQLite 1.46.2, escritor único, almacenamiento privado fuera del workspace, lock del inode del workspace y recuperación pendiente detectada al abrir. |
| `Bind` | Núcleo que requiere un llamador de control confiable. Obtiene rol del catálogo canónico, congela bases del scope y crea binding por sesión/unidad/intento/agente; el plugin toma la identidad del agente que OpenCode entrega, nunca del modelo. |
| `Apply` | Consume llamada por sesión, exige revisión esperada, escritura en ownership explícito, journal sin contenido y commit durable antes de confirmar. |
| `Inspect` / `Verify` | Verificador canónico ligado por author_key y con las mismas invariantes inspecciona la vista del autor; gate liga hash del delta y revisión. Rechazo conserva delta y hallazgo privado; editar invalida gate. |
| `ConsolidateCheckpoint` | API de control confiable con checkpoint explícito; verifica bases declaradas, gate y conflictos; persiste intención antes de reemplazar y verifica estado final. |
| `Recover` | Compara la porción tocada antes de restaurar; detiene ante cambios externos incompatibles; conserva manifiesto si una restauración no se confirma. |
| `session.NewDurable` | Bus por referencia a journal durable; correlación VFS por operación. Git/plataforma reciben unidad explícita, sin setter global. |

`New` y los métodos primitivos existentes siguen disponibles para composición y
pruebas en memoria; **no son una frontera de autorización runtime**. El futuro
adaptador debe usar los bindings privados y no aceptar identidad autorizante,
rol o invariantes de argumentos del modelo. `AttachVerdict` y `Consolidate`
sin checkpoint no habilitan consolidación durable.

La base congelada abarca los archivos declarados; no constituye todavía un
snapshot de lectura de todo el workspace ni una protección contra procesos
externos. La consolidación soporta archivos regulares y sus bits Unix de
permisos. No promete preservar ACL, xattrs, enlaces ni archivos especiales.
`os.Root` ancla el acceso; el reemplazo recorre directorios con `O_NOFOLLOW`,
crea temporales exclusivos y usa descriptores para el rename. La atomicidad
es respecto de operaciones Takt, **no** respecto de observadores externos.

SQLite separa estado privado (contenidos, bases, gates) del journal exportable;
este último tiene secuencia estable y triggers que rechazan update/delete.
No es un ledger resistente a un administrador que manipule la base local.
Un fallo de persistencia bloquea el proceso hasta reabrir; nunca publica la
mutación como durable. El estado SQLite usa schema 1; no hay migraciones entre
versiones de schema todavía.

## Evidencia de esta implementación

| Prueba | Resultado |
| --- | --- |
| `go test ./... -count=1` | Pasó durante la implementación; repetir tras modificaciones posteriores. |
| VFS/session con `-count=1` y `-race` | Pasaron: reapertura, delta vacío/borrado, scopes, alias, gates, replay, dos unidades con mismo autor y cuatro escrituras concurrentes. |
| Flush | Inyección antes/después de cada reemplazo y verificación; reapertura y recuperación; salida abrupta real del subprocess; fallo de restauración y cambios externos incompatibles. |
| Persistencia fallida | Trigger SQLite que aborta la transacción simulando fallo de almacenamiento. No equivale a llenar un disco real. |
| Sandbox macOS | Pasó nativamente en macOS 26.6.2 ARM64; OpenCode instalado reporta 1.18.30, pero no participó en este recorrido. |
| Linux | La compilación cruzada no acredita Ubuntu ni aislamiento nativo. Falta ejecutar en Ubuntu Server 26.04 LTS ARM64. |

El sandbox fija `@anthropic-ai/sandbox-runtime` **0.0.76** y su lockfile.
La prueba deniega sus directorios compartidos de escritura por defecto
(los directorios compartidos que el sandbox hereda, `/tmp/claude` y los logs del
usuario; ninguno corresponde a un agente soportado). Se omite configuración de
red: no se introduce una allowlist de dominios. Los derechos de escritura son archivos
literales y scratch privado, nunca el workspace real. Renames que necesitan
crear un hermano fuera de scope se deniegan; no amplían permisos.

Viabilidad de captura shell (segunda prueba del probe): un comando
transforma archivos solo dentro de una proyección privada y su resultado se
importa como delta VFS. Restricciones observadas en seatbelt macOS: solo
escritura sobre archivos literales preexistentes declarados; crear archivos,
`sed -i` y renombras dentro de la proyección se deniegan; una ruta protegida
no puede ser ancestro de la proyección. Contrato viable confirmado:
transformar a scratch (escribible) y sobrescribir el destino declarado por
redirección en un solo comando. Un comando que no quepa en ese contrato se
deniega explícitamente; no hay fallback a disco.

## Pendientes bloqueantes, no diferidos

- Plugin OpenCode compatible, IPC versionado, instalación/activación/doctor y
  cierre de rutas nativas, tareas, MCP/custom tools, comandos y formatters.
  OpenCode V2 no ejecuta language servers: sustituir LSP por lint/typecheck.
  Avance F1: plugin desplegado por `setup install/sync` con binario inyectado,
  `ipc_version` obligatorio, check `opencode:vfs-plugin` en doctor y recorrido
  OpenCode real ejecutado. Falta: cierre de rutas nativas restantes, tareas,
  custom tools, comandos y formatters (LSP sustituido por lint/typecheck en V2); versionado del transporte más
  allá del campo de versión.
- Scheduler y dispatch de sesiones nativas, máximo canónico de cuatro,
  dependencias respaldadas por evidencia y extensiones de scope del orquestador.
- Creación/edición/patch/glob/grep integrados sobre la proyección del agente.
- Shell: snapshot/proyección sin hardlinks, importación como transacción,
  conservación de resultados fallidos, cancelación y fin confirmado de todos
  los descendientes. `adapter.mjs` solo genera el wrapper de la prueba.
  Viabilidad F0 confirmada en macOS con contrato de escritura restringido
  (scratch + redirección sobre archivo declarado); falta el ejecutor de
  producto y Ubuntu.
- Consentimiento exacto y consumible en OpenCode, resistencia a `always` y reglas
  amplias, serialización por sesión y protección contra solicitudes concurrentes.
- Git con argumentos de inspección seguros, aceptación sobre workspace protegido,
  corrección tras fallo y commit que preserve el índice previo del usuario.
- Matriz completa de fallos de I/O/disco lleno, ataques de reemplazo concurrente
  de rutas, Ubuntu nativo, recorrido OpenCode real y recorrido manual final.

Las extensiones runtime fuera de OpenCode y el bloqueo VFS de lecturas cruzadas
permanecen diferidos según producto. Los puntos anteriores **no** se reclasifican
como diferidos ni se acreditan mediante las pruebas del núcleo.

## Unidades de revisión y rollback

1. Sandbox: `takt/runtime/sandbox/` y exclusión `node_modules/`; retirar juntos
   revierte únicamente la prueba de viabilidad, sin cambiar instalación.
2. Núcleo durable: nuevos archivos de `takt/vfs`, cambios de `vfs.go`, sus pruebas
   y dependencia SQLite; revisar recuperación antes que transporte. No abrir
   stores nuevos con un binario anterior ni borrar stores con recuperación pendiente.
3. Correlación: `takt/session` y sus pruebas; todos los llamadores se migran juntos.
4. Evidencia offline: `cmd/takt-ai/vfs*`, despacho en `main.go` y esta guía.
   Retirarlo no elimina estado ni archivos del workspace.
5. Prueba vertical F0/F1: `takt-vfs.ts`, su artefacto y prueba en
   `takt/agents/opencode`, subcomandos de mutación en `cmd/takt-ai/vfs.go`
   (con `ipc_version`), check en `takt/doctor`, wiring en
   `takt/setup/components.go` y `TestVFSMutationIPC`; retirarlos revierte
   instalación, recorrido y evidencia del plugin VFS.

No se hicieron commits ni se modificó configuración personal de OpenCode.

## Referencias oficiales consultadas

- [Plugins OpenCode](https://opencode.ai/v2/docs/build/plugins) y
  [custom tools](https://opencode.ai/v2/docs/build/plugins): capacidades del adaptador,
  no garantías de VFS ni de cierre de todas las rutas alternativas.
- [Go: acceso resistente a traversal](https://go.dev/blog/osroot): `os.Root`
  restringe acceso a un árbol; no ofrece una transacción multiarchivo.
- [Sandbox Runtime](https://github.com/anthropics/sandbox-runtime): dependencia
  preview que requiere pin y pruebas de aislamiento propias.
