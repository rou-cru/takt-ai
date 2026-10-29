# PRD: Takt Projection into OpenCode v2

## 1. Functional Requirements

### 1.1 OpenCode v2 Functional Projection

| ID | Requirement |
| --- | --- |
| PR-PLT-1 | Takt MUST deliver its canonical specialist definitions, tools, skills, and observable behavior through OpenCode v2. Takt-defined agents remain the source of truth for the crew. |
| PR-PLT-3 | Equivalence is evaluated by observable capability contracts and evidence of delivered quality, not identical files, controls, or generated model outputs. OpenCode v2's native representation remains an implementation detail; Takt's declared behavior and exclusions remain visible. |
| PR-PLT-4 | OpenCode v2 is the sole runtime target of the Takt product. |
| PR-PLT-5 | The canonical Takt definition MUST be projected into OpenCode v2 without replacing its agent roster with OpenCode v2's generic agents or creating a second source of truth. |

### 1.2 Official Integration Scope

| ID | Requirement |
| --- | --- |
| PR-PLT-8 | Proactive harness capabilities delivered through runtime extension code MUST be included in the OpenCode v2 integration. |
| PR-PLT-9 | Memory writing MUST be available in the official integration and go through OpenCode v2 runtime tooling that records authorship, session, and links. |

### 1.3 Technical Mechanism Selection

| ID | Requirement |
| --- | --- |
| PR-MEC-1 | Takt MUST use OpenCode v2 native mechanisms as the default for hosting and extending Takt-defined agents. |
| PR-MEC-2 | Takt-owned extension code MAY fill a product capability that OpenCode v2 does not provide, but MUST NOT replace an adequate native mechanism or introduce a parallel agent implementation. |

### 1.4 Deployed Artifact Integrity

| ID | Requirement |
| --- | --- |
| PR-ART-1 | Every artifact, configuration, directive, or skill deployed by Takt MUST be effectively functional in OpenCode v2 or generate a visible error report. |
| PR-ART-2 | Takt MUST deterministically verify artifact validity before or during deployment, preventing OpenCode v2 from discarding or ignoring them silently. |
| PR-ART-3 | Each increment or module delivered into OpenCode v2 MUST be fully operational end to end, admitting no temporary degradations or orphaned artifacts. |
| PR-ART-4 | Coexistence of multiple non-canonical variants of the same capability to force artificial compatibility is not admitted. |
| PR-ART-5 | The deployed projection preserves role-specific denial continuations, bounded transition responses, result-event capture, and configured isolation. Validate these contracts through observable execution, including repeated dispatch, multi-author evidence, protected tests, and denied boundary crossings; artifact registration alone is insufficient. |
