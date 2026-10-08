# PRD: Takt Projection into OpenCode v2

**Actual implementation progress: 77%** — 13 own requirements: 7 complete, 6 partial, 0 not integrated. Gaps: artifact validity is only checked after deployment and covers a subset of deployed artifacts, and proactive maintenance (dreaming) is not delivered through the OpenCode integration.

## 1. Problem

OpenCode v2 is Takt's only official harness. Takt defines its own canonical agents, capabilities, and behavior; OpenCode v2 supplies the runtime and native integration mechanisms that host them. The product must project that canonical definition without replacing Takt's roster with OpenCode's generic agents or maintaining parallel implementations.

## 2. Functional Requirements

### 2.1 OpenCode v2 Functional Projection

| ID | Requirement |
| --- | --- |
| PR-PLT-1 | Takt MUST deliver its canonical specialist definitions, tools, skills, and observable behavior through OpenCode v2. Takt-defined agents remain the source of truth for the crew. |
| PR-PLT-2 | Projection MUST use OpenCode v2 native agent configuration, plugins, hooks, and permissions to host Takt-defined agents when they provide the required mechanism, without compromising Takt's product identity or agent definitions. |
| PR-PLT-3 | Equivalence is evaluated by observable capability contracts and evidence of delivered quality, not identical files, controls, or generated model outputs. OpenCode v2's native representation remains an implementation detail; Takt's declared behavior and exclusions remain visible. |
| PR-PLT-4 | OpenCode v2 is the only official harness and the sole runtime target of the Takt product. |
| PR-PLT-5 | The canonical Takt definition MUST be projected into OpenCode v2 without replacing its agent roster with OpenCode's generic agents or creating a second source of truth. |

### 2.2 Official Integration Scope

| ID | Requirement |
| --- | --- |
| PR-PLT-8 | Proactive harness capabilities delivered through runtime extension code MUST be included in the OpenCode v2 integration. |
| PR-PLT-9 | Memory writing MUST go through OpenCode v2 runtime tooling that records authorship, session, and links (`MEM-AUT-7`, `MEM-OPS-5`). The official integration MUST NOT ship with read-only memory. |

### 2.3 Technical Mechanism Selection

| ID | Requirement |
| --- | --- |
| PR-MEC-1 | Takt MUST use OpenCode v2 native mechanisms as the default for hosting and extending Takt-defined agents. |
| PR-MEC-2 | Takt-owned extension code MAY fill a product capability that OpenCode v2 does not provide, but MUST NOT replace an adequate native mechanism or introduce a parallel agent implementation. |

### 2.4 Deployed Artifact Integrity

| ID | Requirement |
| --- | --- |
| PR-ART-1 | Every artifact, configuration, directive, or skill deployed by Takt MUST be effectively functional in OpenCode v2 or generate a visible error report. |
| PR-ART-2 | Takt MUST deterministically verify artifact validity before or during deployment, preventing OpenCode v2 from discarding or ignoring them silently. |
| PR-ART-3 | Each increment or module delivered into OpenCode v2 MUST be fully operational end to end, admitting no temporary degradations or orphaned artifacts. |
| PR-ART-4 | Coexistence of multiple non-canonical variants of the same capability to force artificial compatibility is not admitted. |
