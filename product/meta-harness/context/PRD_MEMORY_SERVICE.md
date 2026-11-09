# PRD: Independent Memory Service

## 1. Functional Requirements

### 1.1 Service Boundary

| ID | Requirement |
| --- | --- |
| PR-MSV-1 | The memory service is an independent service process and the sole owner of the memory store. It provides the memory functions `ARCH_MEMORY.md` defines exclusively through MCP; no other interface to the store exists. |
| PR-MSV-2 | Only the memory service opens the memory store. Unavailability MUST surface as an unavailable result, never as an empty or newly created store. |

### 1.2 Clients

| ID | Requirement |
| --- | --- |
| PR-MSV-40 | The crew, through the official integration, and external clients use the same MCP interface over the same memory. An external client needs neither the crew nor OpenCode. |
| PR-MSV-41 | Clients depend only on the MCP interface, never on the service's files or process. |
| PR-MSV-42 | The service assigns the attribution and session of every entry an external client records; the client plays, for its own sessions, the role `ARCH_MEMORY.md` assigns to the base of the interlocutor stack. An external client MUST NOT record into, continue, or close a crew session. |
| PR-MSV-43 | The service accepts connections only from the local user's processes and exposes no listener reachable from other hosts. |

### 1.3 Availability, Durability, and Diagnostics

| ID | Requirement |
| --- | --- |
| PR-MSV-35 | Starting the service is explicit and idempotent: repeated startup reuses the running owner, and readiness is confirmed before any success is reported. |
| PR-MSV-36 | Committed memory survives process or host failure. Concurrent writes never lose or corrupt one another; a write that cannot proceed fails as retryable without partial effect. Corruption is reported and never triggers a reset or replacement store, and a store written by a newer version is refused rather than altered. |
| PR-MSV-37 | The service produces a consistent backup of its committed memory; uninstallation retention (`PR-INS-36`) uses it. |
| PR-MSV-32 | The service reports its version, readiness, semantic coverage, and bounded diagnostics through MCP for setup and the diagnostics command. |
| PR-MSV-33 | Diagnostics distinguish ready, not ready, unreachable, malformed response, and newer-store states; each maps to one distinct reported outcome, and none is reported as healthy by default. |

## 2. Failure Behavior

| Situation | Expected Behavior |
| --- | --- |
| An external client attempts to record into, continue, or close a crew session | Refuse with no effect, naming the operations available on the client's own sessions. |
| The local semantic model is unavailable when an entry is written | Record the entry, which remains explicitly uncovered and retrievable by its terms; diagnostics report the coverage gap. |
