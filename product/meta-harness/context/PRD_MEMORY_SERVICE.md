# PRD: Independent Memory Service

## 1. Functional Requirements

### 1.1 Service Boundary and Single Ownership

| ID | Requirement |
| --- | --- |
| PR-MSV-1 | The memory service is an independent service process. It owns memory admission, immutable source content, evolution relations, session anchors and membership, operation receipts, and bounded queries for its corpus. MCP is an adapter over that owner; no second business API exists outside the defined MCP semantics. |
| PR-MSV-2 | Only the memory service opens the memory database. Clients, forwarders, harness modules, and the installer MUST NOT open it directly, and unavailability MUST surface as an unavailable result, never as an empty or newly created corpus. |
| PR-MSV-3 | The memory service MUST NOT read VFS locks, crew catalog files, or execution-history files. Crew runtime state has no copy in the service, and the service treats authored content as data, never as authority. |
| PR-MSV-4 | Session membership and closure facts have one owner: the service's own transaction. No other store maintains a competing session index. |

### 1.2 Public Operation Set

| ID | Requirement |
| --- | --- |
| PR-MSV-5 | The service exposes a finite public operation set: memory_info, memory_current_workspace, memory_search, memory_get, memory_context, memory_record, memory_continue_session, memory_close_session, and memory_operation for knowledge and reads; three protected issuer operations (memory_issuer_register_identity, memory_issuer_bind_session, memory_issuer_register_command); and administration. No other knowledge-writing tool exists, and the public interface exposes no legacy name, alias, or compatibility endpoint. |
| PR-MSV-6 | memory_get MUST validate a discriminated request: exactly one of an entry selection with metadata, status, content, or history views, or a session selection with the session index view. A content view accepts a section reference or a byte range, never both, and byte-range fields are rejected on other views. Ranges outside the source or inside a UTF-8 code point are rejected, supplying both cursor forms is rejected, and cross-view combinations are rejected as invalid argument. |
| PR-MSV-7 | memory_issuer_register_identity and memory_issuer_register_command are protected tools on the authenticated listener, discoverable only by enrolled issuer principals; each handler independently checks issuer authentication, current registration, namespace, and permission ceiling, and hidden discovery alone is not authorization. memory_issuer_bind_session is additionally discoverable to enrolled fixed-author clients, limited to its fixed-author branch, which binds only their own identity and session; it never covers crew namespaces or asserted identities and grants nothing else. Issuer operations are never projected to crew agents. memory_issuer_register_command binds issuer, operation key, payload digest, actor, session, corpus, and the permitted operation, and returns an opaque admission reference that is principal-bound and payload-bound: it proves admission of that one immutable action, not current holder status or any further permission. |
| PR-MSV-8 | Administration is a separate protected MCP connection over the operator local socket. Only that connection exposes memory_admin, whose tagged actions are enroll_principal, enroll_issuer, rotate_credential, revoke, and confirm_intent. None of them is arbitrary query execution or a content-deletion command. The network listener cannot advertise or invoke administration, even with an issuer credential. |
| PR-MSV-9 | Session creation for a client is an authenticated binding operation of the client admission path: the fixed-author branch of memory_issuer_bind_session for enrolled fixed-author clients, or issuer binding for crew sessions. It is not a knowledge-writing tool, and the knowledge and read operations operate on the bound session. |
| PR-MSV-40 | The service is an independent process launched by a subcommand of the Takt application executable. It accepts authenticated clients on one local Streamable HTTP listener and serves administration only on the operator socket of PR-MSV-8. A domain memory session never depends on an MCP transport session: a new transport session continues the same bound domain session. |
| PR-MSV-41 | An optional stdio forwarder connects stdio-only hosts to the running service. It never opens the memory storage, never starts a second owner, and never creates a corpus or any file; with the owner stopped it reports unavailable. |

### 1.3 Operation Contracts

Each request and result is a closed schema; any field not listed is rejected as invalid argument. The mutation context of PR-MSV-13 and the read options of PR-MSV-12 apply wherever the table names them.

| Operation | Request | Result |
| --- | --- | --- |
| memory_info | Optional authorized corpus; optional diagnostics or review selection. | API, source-schema, and derived-schema versions; limits; capabilities; readiness; bounded diagnostics and review results (PR-MSV-32 to PR-MSV-34). |
| memory_current_workspace | An explicit workspace reference or a client-local directory hint. | The registered workspace identity, how it was detected, and the authorized alternatives. Reading never registers or creates a workspace. |
| memory_search | Query; project or personal scope; optional nature; include-history flag; all-workspaces flag over authorized workspaces; read options. | Compact hits with conflict and exception flags, the count of omitted superseded entries (PR-MSV-19), and the page fields of PR-MSV-18. |
| memory_get | The discriminated selection of PR-MSV-6; read options. | Exact details per requested item, with per-item outcomes (PR-MSV-20). The include-history flag adds bounded evolution references, never inline successor bodies. |
| memory_context | An authorized selection; compact and older-history flags; read options. | Bounded orientation and history for that selection. It never returns an automatic dump of session content. |
| memory_record | Title, content, nature, scope, optional evidence, optional relation to an existing reference, user-order intent; mutation context. | The committed memory reference, its content digest, and the terminal receipt. |
| memory_continue_session | A previous closed-session reference, or an empty withdrawal; mutation context. | The revised continuity declaration and the terminal receipt. |
| memory_close_session | Session objective and final state for explicit closure, or the authorized fallback mode; mutation context. | The end-anchor reference, the entry count, and the terminal receipt. |
| memory_operation | An operation key and the action status or cancel. | Status returns operation status; cancel returns a mutation receipt (PR-MSV-14, PR-MSV-24). |

### 1.4 Closed Domain API Shapes

| ID | Requirement |
| --- | --- |
| PR-MSV-10 | A memory reference pairs an opaque memory identifier with an opaque entry identifier; both are service-issued, nonempty, and neither is a capability. An old reference never silently resolves to a successor. |
| PR-MSV-11 | Mixed invariant lists and human-readable citations use one canonical reserved token, `memory:v1:<M>:<E>`, where `<M>` and `<E>` are the unpadded base64url encodings of the UTF-8 memory identifier and entry identifier. The token is owned by the memory domain and decoded strictly; ordinary operation payloads carry the reference as a structured pair, not as a token. Padded, noncanonical, extra-segment, empty, invalid-UTF-8, numeric, and legacy-prefixed forms are rejected; a malformed memory-prefixed value fails instead of resolving as a filesystem path. |
| PR-MSV-12 | Every bounded read carries request options with defaults of 16 KiB and 20 items and hard maxima of 64 KiB and 50 items; an exact batch read accepts at most 32 references. A response-envelope budget below the advertised minimum, or one the service cannot meet, is rejected. These values are contract limits of the published interface. |
| PR-MSV-13 | Authentication and trusted attribution arrive through transport context, never through authored content. A mutation request carries an operation key, and optionally an expected session revision, a confirmation reference, and an admission reference. |
| PR-MSV-14 | A mutation receipt is terminal: applied requires a commit sequence and its operation-specific result; rejected requires a domain error and no committed source effects; canceled has no source effects. Operation status is a separate tagged union of pending, meaning registered without a terminal receipt and claiming no progress, or terminal receipt. An unknown operation key returns not found, never canceled, and never proves that a delayed submission cannot arrive. |
| PR-MSV-15 | Domain errors carry a stable code, message, retryability, and optional continuation from the set: invalid argument, forbidden, not found, conflict, stale precondition, cursor expired, busy, overloaded, unavailable. A cross-tenant denial reveals no object existence. |
| PR-MSV-16 | API version, source schema version, and derived schema version are tracked separately. An unknown API major or a newer source schema refuses incompatible operations; additive optional fields retain their existing meaning. |

### 1.5 Bounded Reads, Byte Accounting, and Freshness

| ID | Requirement |
| --- | --- |
| PR-MSV-17 | Encoded-size accounting is exact at the owned output boundary: the UTF-8 bytes of the compact serialization of the complete tool result, including content blocks, structured output, error flags, emitted metadata, and every duplicate copy, and excluding the protocol envelope, transport framing, compression, tokenizer overhead, and host-added text. The reported byte total is stable against its own digits, resource reads count equivalently, and no hidden full body accompanies a truncated view. |
| PR-MSV-18 | Continuations are opaque, integrity-protected cursors bound to principal, tenant, authorized corpus set, query, filter, order, API version, snapshot sequence, incarnation, and expiry. Every page reauthorizes current access; an expired, altered, or cross-principal cursor is rejected. Later changes appear in the observed sequence, not inside a paged window, and paging never holds a database transaction across requests. Every page reports its API version, snapshot sequence, observed sequence, incarnation, items, bytes used, a truncation flag, and the next continuation when one exists. |
| PR-MSV-19 | Search order is deterministic by score and then committed position. Canonical scope and status predicates are applied before result limits and body retrieval; a page reports the superseded entries it omitted with their count and never widens the result window to fill a page. Conflict and exception flags derive from canonical relations. A rebuilt index returns results identical to the pre-rebuild result. |
| PR-MSV-20 | Exact, batch, and section reads reconstruct the requested bytes within the reported total, in request order, with per-item outcomes. A large artifact is returned as reconstructible ranges with its digest, and a continuation identifies both the next request item and any partial content offset. |
| PR-MSV-21 | Optional resource templates invoke the same queries and schemas as the tools, and no resource enumerates the corpus. Tools remain sufficient on hosts without resource support. |

### 1.6 Operation Lifecycle and Receipts

| ID | Requirement |
| --- | --- |
| PR-MSV-22 | Submission and cancellation are serialized per operation key. Submission validates current service access, admission binding, and the expected session revision, then commits source effects and the terminal receipt in one transaction. Cancellation creates a terminal tombstone even when submission has not arrived: if submission won, cancellation returns applied; if cancellation won, no delayed registration or submission can revive the key. Reuse of a key with a different payload digest fails. |
| PR-MSV-23 | Receipts, tombstones, and operation keys are retained for the full lifetime of their service and corpus identity, with no time-based collection. Retained fields are minimal: key, issuer or principal binding, digest, terminal outcome, and result references. Keys are never reused or reopened after rejection or cancellation, and credential rotation, consolidation cycles, and maintenance cannot delete this control evidence. Only explicitly authorized retirement of the corpus or service identity ends its lifetime, after pending crew reservations are reconciled; retired identities remain non-reusable. |
| PR-MSV-24 | Operation status is available to the caller for its own keys; cancellation is available only where the actor or its issuer already holds cancellation authority. |

### 1.7 Identity, Tenant Isolation, and Clients

| ID | Requirement |
| --- | --- |
| PR-MSV-25 | The first local trust root is the operating-system account owning a private service directory with mode 0700 and an operator socket with mode 0600. Only that protected channel performs enrollment. There is no unauthenticated network enrollment and no agent-facing administration. Same-UID processes share this trust boundary. |
| PR-MSV-26 | In the local profile, credentials are independent opaque random bearer values, stored only as verifiers, presented in request headers, and checked on every request; rotation and revocation are service-owned. Loopback is not authentication: local HTTP is an operating-system-bound profile and never an externally bound plaintext fallback. Remote hosting requires transport security and maintained integration with an existing OAuth/OIDC authority with resource-audience validation; the local credential form does not define that profile's tokens. |
| PR-MSV-27 | A human client has a registered human author; an independent automation client has a fixed integration author and service-bound generic sessions. Neither inherits crew privileges. The service assigns canonical identities and records issuer provenance; client metadata, transport session identifiers, and author arguments never become trusted identity. |
| PR-MSV-28 | Tenant is a hard partition. Global scope means organization-global within that tenant, or the single-user tenant of a personal user, never service-global. Workspace names and remotes do not establish identity or membership; clients register location mappings explicitly and the service does not probe supplied paths. Cross-workspace access and continuity require authorization to both endpoints, and detectable project excerpts are rejected from global writes. |
| PR-MSV-29 | No credential enters stored memory, URLs, logs, model inputs, or callable tool arguments. Manual clients receive separate credentials through protected input or credential stores. |
| PR-MSV-30 | Crew sessions are registered under an enrolled crew issuer and cannot be closed or continued through generic-client permissions. Under the authority split the memory architecture defines (MEM-OPS-6), the service evaluates only data-dependent source and session preconditions, supplies only the records the harness requests, and contributes no session narrative. |
| PR-MSV-31 | Deterministic positive confirmation governs user-prompted persistence (MEM-TYP-2). Required confirmation binds principal, action, scope, and exact payload digest; a model's user-order flag or evidence string cannot manufacture approval, and enrollment authority does not grant content-deletion rights. |

### 1.8 Diagnostics

| ID | Requirement |
| --- | --- |
| PR-MSV-32 | The service reports versions, limits, capabilities, readiness, and bounded diagnostics and review results through its info operation, for consumption by the application's diagnostics command. |
| PR-MSV-33 | Diagnostics distinguish ready, unreachable, malformed response, and newer-schema states, and return at most three bounded issues. Each state maps to one distinct reported outcome; none is reported as healthy by default. |
| PR-MSV-34 | The review-due check reports exactly the review state its diagnostics provide and derives no due scheduling, ordering, interval, or clearing rule of its own. A recognized project with zero due records passes, and a project the diagnostics do not recognize with no records passes as healthy. A missing scheduling rule, an unparseable response, or an unrecognized response shape is reported as its own distinct warning state and is never presented as a review pass or a due count. |

### 1.9 Service Lifecycle and Durability

| ID | Requirement |
| --- | --- |
| PR-MSV-35 | Local lifecycle explicitly selects or starts the service owner and checks readiness before any success claim. Repeating startup or setup converges to the same state without duplicating listeners, credentials, or configuration. |
| PR-MSV-36 | The service holds one write owner over its storage with durable synchronization, bounded queues, and per-tenant fairness. Waiting ends in busy or overloaded with same-key continuation. No inference or external call runs inside a write transaction. Corruption is reported and never triggers a source reset, and unknown newer schemas refuse writes. |
| PR-MSV-37 | Backups capture committed state together with its write-ahead log. A restore yields a new incarnation identity and invalidates earlier cursors. |

### 1.10 Preservation and First-Delivery Scope

| ID | Requirement |
| --- | --- |
| PR-MSV-38 | The first delivery replaces the exposed memory functionality on fresh data. It performs no historical import, no migration, no fork, no aliases, no dual write, and no second backend; legacy memory data remains intact and untouched, and incompatibility with it is reported, never resolved by silent reinterpretation, conversion, or deletion. Schema evolution of the service's own store, consistent backups, and retirement or deletion authorized under PR-MSV-23 remain governed by those rules; this requirement grants no deletion. |
| PR-MSV-42 | The first delivery covers every memory function Takt exposes: attributed append with author distinct from authority and append-only evolution; session graph, continuity, and explicit and fallback close; Workspace and Global isolation with project resolution; search options with governing status, omitted counts, and flags; targeted reads and context; result-reference validation at handoff; skill and protocol projection; truthful setup, update, readiness, doctor, and uninstall; review-due diagnostics; status-aware lexical retrieval; local identity with manual clients; and fresh installation on every required support target. |
| PR-MSV-43 | Full replacement of the exposed functionality is claimed only when every function of PR-MSV-42 is verified on fresh data by executed evidence; documentation alone verifies nothing. A function the user accepts as omitted is reported as a named omission of a revised scope, never as full replacement. |
| PR-MSV-39 | The initial operation set provides efficient lexical retrieval. Semantic representation remains governed by MEM-RET-7 and lies outside the initial operation set; the initial operation set makes no MEM-RET-7 conformance claim. |

## 2. Failure Behavior

| Situation | Expected Behavior |
| --- | --- |
| The service is unreachable during a client read or write | Report unavailable. No empty or replacement corpus is created, and no result is presented as a successful empty read. |
| A response cannot be parsed or violates the published schema | Report a distinct malformed-response state; it is never treated as healthy or empty. |
| The source schema is newer than the running service | Refuse incompatible operations and report the newer-schema state. |
| Admission binding or payload digest does not match the registered command | Reject with no source effect; the operation key does not become reusable. |
| A concurrent entry makes the expected session revision stale at close | Return stale precondition instead of silently omitting the entry. |
| Write contention exceeds the wait deadline | Return busy or overloaded, with same-key continuation available. |
| Authorization is missing or revoked for the requested corpus | Return forbidden without revealing whether the object exists. |
| Cancellation arrives before submission, or a delayed submission arrives after cancellation | The key stays canceled; the delayed submission has no source effect. |
| Cancellation arrives after a committed submission | Return the applied receipt. |
| The service crashes after committing, or a reply is lost and the caller retries the same key | Return the original terminal receipt; the operation is applied once. |
| Storage is full | Reject the write with no partial source effect and report the condition. |
| Source or derived storage is corrupt | Report corruption; never reset the source. Derived indexes may be rebuilt from source. |

## Ownership boundary

This PRD owns the observable behavior of the independent memory service and its clients. The memory-authority split between execution owner and memory service belongs to ARCH_MEMORY.md (MEM-OPS-6); crew runtime identity, lifecycle admission, reservations, and completion belong to the harness PRD; memory meaning, authorship, evolution, semantic representations, and consolidation belong to ARCH_MEMORY.md; operation-dependent routing and host projection belong to the platforms PRD; setup and installation lifecycle belong to the application PRDs. Storage schema, module layout, and library selection are implementation choices and are not defined by this corpus.
