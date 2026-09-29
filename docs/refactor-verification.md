# Refactor integral: implementación y verificación

Implementado el 19 de septiembre de 2026, sin commits, despliegues, migraciones ni
cambios de configuración pública. Los cambios funcionales son la limpieza de
temporales, la eliminación del rechazo léxico y el presupuesto efectivo de
disponibilidad de Engram. Las instrucciones siguen pidiendo hechos, no planes,
salvo orden explícita del usuario.

## Resultado de verificación

Todo pasó con **Go 1.25.14, linux/arm64**, imagen `golang:1.25`:

| Comando | Resultado |
|---|---|
| `go build ./...` | Exit 0 |
| `go vet ./...` | Exit 0 |
| `go test ./...` | PASS |
| `go test -race ./...` | PASS, sin carreras reportadas |
| `make test-host` | PASS, ejecutado dentro del contenedor |
| `development/testing/test-containerized.sh` | PASS, paquetes predeterminados, exit 0 |

El host no tenía Go disponible y Git fallaba por `xcrun`; no se instalaron ni
repararon herramientas del host. CodeGraph se intentó antes de buscar código,
pero falló al abrir la configuración OpenSSL del sistema, también con escalación.

Se copiaron las fuentes permitidas a `/src` del contenedor
`takt-refactor-verify`, sin montar el host. El runner existente, sin modificar,
se ejecutó desde una copia de esas fuentes en `/tmp/takt-refactor-suite`:
`COPYFILE_DISABLE=1 bash /tmp/takt-refactor-suite/development/testing/test-containerized.sh`.
Su contenedor también usa copia por tar y cero montajes del host. Los archivos
AppleDouble de macOS deben excluirse: se interpretan como paquetes del catálogo.
La cobertura generada quedó aislada; no se reemplazó `coverage.out` del repositorio.

## Unidades de revisión y reversión

Cada comando focalizado de la tabla terminó en PASS. Las rutas delimitan qué
cambios revertir, **no** autorizan restaurar archivos completos con cambios ajenos.
Cuando dos unidades comparten un archivo, revertir solamente sus funciones/hunks.

| Unidad | Límite de reversión | Prueba focalizada |
|---|---|---|
| Cleanup | `filemerge/writer.go`: `StageTempFile`, interfaz/factory privada; `staging_test.go` | `go test ./takt/internal/filemerge` |
| Persistencia compartida | `memory/ledger.go`, `engram/acquire.go`: escritores y sus nuevos tests de staging | `go test ./takt/memory ./takt/engram` |
| Política de memoria | Validadores de `memory/memory.go`, casos léxicos/atribución en tests de memoria y CLI; skill de contrato, PIRS y API docs | `go test ./takt/memory ./takt/cli` |
| Refactor de memoria | `Record`, `prepareRecord`, `persistRecord`, `recordLinks`; no revertir validadores | `go test ./takt/memory` |
| Doctor/backups | Agregación/presentación en `doctor.go`; familia `priorStateFor` en `setup/operations.go`; caracterización de stat | `go test ./takt/doctor ./takt/setup` |
| Drift | `executeCorrectDrift`, `reinjectDrift`; tests de reinyección y cancelación | `go test ./takt/tui/runtime` |
| Planes | Builders, `planAssembler`, `resolveEntryAssignment` y `plan_assembly_test.go` | `go test ./takt/setup` |
| Targets/constantes/generador | `model/targets*` y sus defaults de opciones de agente (antes repartidos en `model/codex_options.go` y `model/claude_model.go`), consumidores setup/catalog/lifecycle/engram; acciones uninstall y tests de orden TUI; `generate-logo` | `go test ./takt/model ./takt/engram ./takt/setup ./takt/tui/... ./development/generate-logo` |
| Tiempos | `memory/client.go`, `client_test.go` | `go test ./takt/memory` |

Las rutas abreviadas de paquetes pertenecen a `takt/`, salvo el generador en
`development/generate-logo`. Revertir persistencia no exige retirar el arreglo de cleanup;
revertir el refactor de `Record` no exige restaurar el filtro léxico.

### Escenarios y límites

- **Staging:** fallos Create/Write/Chmod/Sync/Close, temporal eliminado, destino
  anterior intacto; éxito cerrado y completo; ledger `0600`, directorio `0700`,
  ejecutable `0755`; fallo de rename sin temporales abandonados.
- **Memoria:** español «se verificó todo», citas con “will” y demás palabras antes
  bloqueadas, tanto registro como cierre; atribución, autoridad, relaciones,
  deduplicación y concurrencia siguen cubiertas por la suite.
- **Doctor/backups:** tests existentes de takeover, archivos administrados/editados
  y restauración del original; prueba adicional de ausencia y symlinks rotos/cíclicos
  clasificados como missing, incluidos archivos compartidos por varios targets.
- **Drift:** éxito, cancelación sin cambios y resultado parcial; Engram obligatorio,
  CodeGraph tolerado. El binario falso cuenta una sola adquisición de CodeGraph
  por reinyección efectiva y ninguna cuando no hubo cambios o falló Engram.
- **Planes/TUI:** orden, managed paths, precedencia de presets y overrides, y tests
  de validación directa de renderers; orden visual y acciones sin cambios.
- **Generador:** comparación completa de TSX contra la construcción anterior con
  `json.Marshal`, incluyendo comillas, barras, controles, Unicode y UTF-8 inválido.
- **Tiempos:** salud inmediata, bloqueo de cabeceras y cuerpo, deadline compartido,
  binario ausente/fallido, cancelación antes/durante sondeo, deadline del llamador
  menor y proceso desacoplado que sobrevive al vencimiento de la espera.

Los tests de filesystem, HTTP y procesos se ejecutaron dentro de contenedores;
los planes/CC no requieren un harness externo adicional (N/A: análisis puro).

## Compatibilidad de planes

Se comparó el JSON completo de los planes antes/después, incluidos bytes de cada
artefacto, orden y managed paths: OpenCode, con defaults y override
de `takt-dev`, **idénticos en los dos casos** mediante `cmp`.

Para comparar el ensamblador se mantuvieron iguales los inputs: la actualización
del texto del contrato de memoria es un cambio deliberado independiente. También
se compiló y ejecutó cada prueba en la misma ruta `/tmp/plans.test`: OpenCode
incrusta `os.Executable()` en los plugins de memoria y VFS.

## Complejidad ciclomática

Se usó el mismo contador AST antes/después, sin instalar herramientas: base 1,
+1 por `if`, `for`, `range`, caso no-default de switch/select y operador `&&`/`||`.
Se midieron las fuentes previas a estos refactors y las finales con el mismo
programa temporal basado en `go/parser` y `ast.Inspect`.

| Función | Antes | Después | Helpers nuevos (CC) |
|---|---:|---:|---|
| `Record` | 14 | 4 | `prepareRecord` 5, `persistRecord` 4, `recordLinks` 4 |
| `deploymentChecks` | 12 | 7 | `inspectDeployment` 3, `add` 2, `deploymentResult` 2 |
| `priorStateFor` | 12 | 2 | `managedPriorState` 6, `preexistingPriorState` 6, `backedUpState` 2 |
| `executeCorrectDrift` | 11 | 7 | `reinjectDrift` 3 |

Todas las funciones señaladas y sus helpers cumplen **CC ≤10**. Adicionalmente,
`ensureServe` queda en 7 y su bucle de espera `waitForHealthy` en 6. No se dividieron
las tablas declarativas de transiciones.
