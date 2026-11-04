# PRD: Takt Projection into OpenCode v2

## 1. Functional Requirements

### 1.1 OpenCode v2 Functional Projection

| ID | Requirement |
| --- | --- |
| PR-PLT-1 | Takt MUST deliver its canonical specialist definitions, tools, skills, and observable behavior through OpenCode v2. |
| PR-PLT-3 | Equivalence is evaluated by observable capability contracts and evidence of delivered quality, not identical files, controls, or generated model outputs. Takt's declared behavior and exclusions remain visible. |
| PR-PLT-4 | OpenCode v2 is the sole runtime target of the Takt product. |
| PR-PLT-5 | The canonical Takt definition MUST be projected into OpenCode v2 without replacing its agent roster with OpenCode v2's generic agents. |

### 1.2 Official Integration Scope

| ID | Requirement |
| --- | --- |
| PR-PLT-8 | Proactive harness capabilities delivered through runtime extension code MUST be included in the OpenCode v2 integration. |
| PR-PLT-9 | Memory writing MUST be available in the official integration and go through OpenCode v2 runtime tooling that records authorship, session, and links. |
| PR-PLT-10 | Memory operations follow operation-dependent routing: crew to the memory service over MCP, crew to the harness to the memory service, and harness to the memory service are all valid. The operation, the acting identity, the available knowledge, and the necessary controls determine the route. Deployment topology MUST NOT determine routing, and no mandatory intermediary or global proxy exists between the crew and the memory service. |
| PR-PLT-11 | The official integration is one supported route, not a required intermediary: independent manual and custom clients, and hosts that deliver trusted per-call identity, are supported clients of the memory service with direct access of their own. No host or client type is required to implement every route, and the official integration may wrap the service for its own clients without becoming a mandatory intermediary for any other client. |
| PR-PLT-12 | In the official integration, the acting identity for a memory operation comes from host-delivered execution context, never from model-authored fields; authored identity or session assertions outside that trusted context are rejected, and no unbound native memory write tool is exposed in parallel under another name. |
| PR-PLT-13 | The projected memory permission set is the static upper bound of the harness's crew memory capability derivation (PR-HAR-35); runtime context subtracts unavailable actions only, and registered issuer ceilings and service grants bound that set without broadening it. |

### 1.3 Technical Mechanism Selection

| ID | Requirement |
| --- | --- |
| PR-MEC-1 | Takt MUST use OpenCode v2 native mechanisms as the default for hosting and extending Takt-defined agents. |
| PR-MEC-2 | Takt-owned extension code MAY fill a product capability that OpenCode v2 does not provide, but MUST NOT replace an adequate native mechanism or introduce a parallel agent implementation. |
| PR-MEC-3 | For the confirmed first host adapter (OpenCode), the memory integration uses that host's server-plugin tool transformation mechanism for per-call context and MUST NOT assume an identity-injection hook the host does not provide. On other or future hosts, a supported native mechanism that injects trusted per-call identity is permitted and is selected under the platform's native-mechanism priority; mechanism selection follows per-host evidence rather than a universal transformation rule. |

### 1.4 Deployed Artifact Integrity

| ID | Requirement |
| --- | --- |
| PR-ART-1 | Every artifact, configuration, directive, or skill deployed by Takt MUST be effectively functional in OpenCode v2 or generate a visible error report. |
| PR-ART-2 | Takt MUST deterministically verify artifact validity before or during deployment, preventing OpenCode v2 from discarding or ignoring them silently. |
| PR-ART-3 | Each module delivered into OpenCode v2 MUST be fully operational end to end, admitting no temporary degradations or orphaned artifacts. |
| PR-ART-4 | Coexistence of multiple non-canonical variants of the same capability to force artificial compatibility is not admitted. |
| PR-ART-5 | The deployed projection preserves role-specific denial continuations, bounded transition responses, result-event capture, and configured isolation. Validate these contracts through observable execution, including repeated dispatch, multi-author evidence, protected tests, and denied boundary crossings; artifact registration alone is insufficient. |
