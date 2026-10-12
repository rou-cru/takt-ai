# PRD: Quality Assurance and Testing

**Actual implementation progress: 97%** — 19 own requirements: 18 complete, 1 partial, 0 not integrated. The strategy skill, the QA specialists, their documents, and the routing of planning, the acceptance tail, and testing outcomes are catalog content reachable through the crew's normal flow; blindness rests on VFS file ownership. Gap: sole authorship of each traceability matrix column (`PR-QA-9`) rests on conduct; nothing refuses an entry for another author's column.

## 1. Problem

Testing that is decided per task by whichever agent happens to implement reproduces the implementer's assumptions in its tests: the code and its oracle share one author and one blind spot. Takt requires a single test strategy, adapted once to each workspace, planned per objective, traced from requirement to evidence, and executed by agents whose independence from the implementation is part of the design.

This document owns that strategy and its artifacts: the organizational test strategy, the test policy, the test plan, environment requirements, the requirements traceability matrix, test authorship and oracle rules, mutation adequacy, exploration, non-functional analysis, and the test completion report. Role classes and permission profiles belong to [PRD_CREW](../crew/PRD_CREW.md); staging, file ownership, verdicts, and acceptance checks to [PRD_VFS](../harness/PRD_VFS.md) (`PR-VFS-COL-2`, `PR-VFS-CSL-3`, `PR-VFS-CSL-6`, `PR-VFS-CSL-7`); route selection to [WORKFLOW_SELECTION](../orchestration/WORKFLOW_SELECTION.md); entry lifecycle to [ARCH_MEMORY](../context/ARCH_MEMORY.md). Tests are never an object of GC work: removal of tests is only the consequence defined in `PR-MNT-25` (`PR-CRW-16`).

## 2. Functional Requirements

### 2.1 Organizational Test Strategy

| ID | Requirement |
| --- | --- |
| PR-QA-1 | Takt AI governs the organizational test strategy of every workspace the crew works in, in place of an organization's universal strategy. Its single definition is the skill `takt-test-strategy` (`STR-1`), retrieved on activation (`RET-1`); test policies, test plans, and agent definitions refer to it and never restate it. A direct user instruction prevails within its scope (Article 5). |
| PR-QA-2 | Testing is organized in the four agile testing quadrants, each with one owning specialty, per the table below. No quadrant is owned by the specialty that implemented the code it tests, except Q1. A quadrant is planned only when the objective's acceptance needs it, never added as an unsolicited protection (Article 2). |
| PR-QA-3 | Test levels are component, integration, system, and acceptance. Component tests are Q1; integration and contract tests at an interface frozen by contract, and system and acceptance tests against the specification, are Q2. The test plan chooses the levels per objective. |
| PR-QA-4 | The orchestrator plans QA for an objective whose specification states acceptance criteria spanning more than one work unit, or that carries a must-not-fail risk. The QA lead enters as a planning author once the specification exists. The test author enters once the contracts it tests are frozen and the matrix exists, concurrently with implementation. The mutation analyst enters once the Q2 suite passes on the materialized workspace and the matrix holds must-not-fail rows. The explorer enters on the materialized deliverable when the plan selects Q3. The quality analyst enters when the test plan selects Q4 naming an ISO/IEC 25010 characteristic, which its brief carries. While QA is planned, the objective's test plan and traceability matrix are required invariants: with the specification settled and the test plan missing, invariant planning runs with the QA lead as its only lane (`PR-ORQ-19`). An objective with no QA planned is held to the Definition of Done in force without a test plan. |
| PR-QA-5 | The strategy defines the baseline Definition of Done: every planned quadrant has run; the acceptance checks of `PR-VFS-CSL-6` pass on the materialized workspace; every matrix row carries evidence; no must-not-fail row has a surviving non-equivalent mutant; and every finding left open is declared as residual risk. The node that closes the objective's acceptance evaluates it (`PR-QA-19`). Absent a test policy, the baseline is the Definition of Done in force. |

**Quadrant ownership (`PR-QA-2`).**

| Quadrant | Orientation | Purpose | Owner |
| --- | --- | --- | --- |
| Q1 | Technology-facing, supporting the team | Unit tests of the delivered unit | Development (`takt-dev`) |
| Q2 | Business-facing, supporting the team | Acceptance and contract tests | Blind test author (`takt-test`) writes; Verify (`takt-verify`) runs and judges |
| Q3 | Business-facing, critiquing the product | Exploratory testing | Explorer (`takt-explorer`) |
| Q4 | Technology-facing, critiquing the product | Non-functional analysis | Quality analyst (`takt-quality`) |

The QA lead (`takt-qa`) is a planning author. The test author is an ordinary execution instance (`PR-CRW-8`). The mutation analyst (`takt-mutation`), the explorer, and the quality analyst are verification-class instances: they share Verify's base conduct, and only their objective differs (`PR-CRW-9`). None introduces a role class or permission profile (`PR-CRW-2`, `PR-CRW-4`).

### 2.2 Test Policy

| ID | Requirement |
| --- | --- |
| PR-QA-6 | Each workspace has exactly one test policy, adapting the strategy to that project. The QA lead authors it once, the first time QA is planned there, from repository facts — agent instructions such as `AGENTS.md`, CI configuration, test tooling, and conventions — and persists it as a planning-author artifact (`PR-CRW-7`) whose memory entry lives in the Workspace Tier (`MEM-SCP-3`). Later refinements are supplementing entries (`MEM-OPS-2`), never rewrites. The policy carries the workspace's default Definition of Done, derived from the baseline of `PR-QA-5`; a test plan may only tighten it. |

### 2.3 Test Plan and Environment

| ID | Requirement |
| --- | --- |
| PR-QA-7 | Each objective with QA planned has one test plan, authored by the QA lead from the specification, the frozen contracts, and the test policy. It states the chosen quadrants and test levels, entry and exit criteria, the must-not-fail risks, and the effective Definition of Done. When invariant planning precedes SDD, the plan is delivered with the specification (`PR-ORQ-19`). |
| PR-QA-8 | Environment requirements are authored by the QA lead only when the environment the plan needs is non-trivial: services, data, configuration, or platforms beyond what the repository already runs. They build on the environment facts the analyst established and never restate them. |

### 2.4 Requirements Traceability

| ID | Requirement |
| --- | --- |
| PR-QA-9 | Each objective with QA planned has one requirements traceability matrix: requirement → test condition → owning DAG node → evidence. The QA lead creates it with the requirement, the test condition, and the must-not-fail marking; TPM adds the owning node; Verify adds the evidence, identifying the evaluated scope as `PR-VFS-CSL-7` requires. Each column has exactly that one author. |
| PR-QA-10 | Every addition to the matrix is a supplementing memory entry linked to the matrix (`MEM-OPS-2`), never a rewrite of it. A requirement with no test condition, a condition with no owning node, and a row with no evidence when acceptance closes are each visible in the matrix as such. |

### 2.5 Test Authorship and Oracle

| ID | Requirement |
| --- | --- |
| PR-QA-11 | Q1 unit tests are black-box: they exercise only the delivered unit's public interface, never its internals. The regression test the fix specialty writes for a defect is black-box under the same rule. |
| PR-QA-12 | Q2 tests are authored blind: the test author writes them from the specification, the frozen contracts, and the matrix, never from the implementation. It reads whatever else its work needs, including its own earlier tests and existing tests within its role. Blindness rests on file ownership: the implementer's in-flight files belong to the implementer's claim, so the VFS denies the test author access to them (`PR-ORQ-7`, `PR-VFS-COL-2`); beyond that, its conduct forbids seeking the implementation. Its writable set is disjoint from every implementer's (`PR-ORQ-18`), and its tests reach the workspace through the same staged verdict as any delta (`PR-VFS-CSL-3`). |
| PR-QA-13 | A Q2 test is an immutable oracle for the implementer. An implementer who finds the specification and a test in contradiction returns a blocker naming both, and the orchestrator routes it to the owner of the contract at issue; the implementer never edits a test to make it pass. When that owner rules the test wrong, the test author corrects it; otherwise the test stands. |
| PR-QA-14 | A failing Q2 test is repaired in the code by the fix specialty, never in the test. |

### 2.6 Mutation Adequacy

| ID | Requirement |
| --- | --- |
| PR-QA-15 | The mutation analyst judges whether the Q2 suite detects faults where they matter: it generates a small number of mutants targeted at the code behind must-not-fail matrix rows, on a copy outside the project, never in the workspace. It discards equivalent mutants before reporting. |
| PR-QA-16 | Surviving mutants are delivered to the test author, which strengthens the suite; they are never delivered to the implementer. The strengthened tests pass a staged verdict and are consolidated, then the mutation analyst re-runs only the affected rows, within the ordinary recovery budgets. A surviving mutant on a must-not-fail row still unresolved when the objective's acceptance closes is residual risk (`PR-QA-19`). |

### 2.7 Exploration and Non-functional Analysis

| ID | Requirement |
| --- | --- |
| PR-QA-17 | Q3 exploration runs as chartered sessions: each charter names the area, the risk, and the heuristics used, such as SFDPOT and FEW HICCUPPS. The explorer uses the deliverable as its user would, on the materialized workspace, and records its findings in memory, delivering the entry ID. |
| PR-QA-18 | Q4 analysis is performed per ISO/IEC 25010 characteristic named in the brief, with evidence for each. When the objective's acceptance is non-functional, the quality analyst issues that acceptance verdict in place of Verify; it replaces only the verdict, and the closing Verify node of `PR-QA-19` still records matrix evidence and the test completion report, consuming the quality analyst's report. A failed exploration or non-functional finding goes through ordinary recovery: the fix specialty repairs the code, and a contract defect goes to its owner. |

### 2.8 Test Completion Report

| ID | Requirement |
| --- | --- |
| PR-QA-19 | The objective's acceptance tail is: acceptance Verify (build, lint, suites including Q2, and end to end), then the mutation, exploration, and non-functional nodes the test plan selects. When any of them ran, a closing Verify node consumes their reports by ID, adds the matrix evidence, and records the test completion report; when none ran, the acceptance Verify node closes and records it. The closing node records a test completion report in the shape of ISO/IEC/IEEE 29119-3: summary of testing performed, deviations from the plan, completion evaluation against the exit criteria and the effective Definition of Done, residual risk, reusable test assets, and lessons. It is a memory entry delivered by ID. The user-facing completion report remains the orchestrator's (`PR-CRW-5`), which consumes this one by ID and never omits its residual risk. |

## 3. Failure Behavior

| Situation | Expected Behavior |
| --- | --- |
| Specification and Q2 test contradict | The implementer returns a blocker; the orchestrator routes it to the contract owner; neither test nor code changes until it rules (`PR-QA-13`). |
| A Q2 test fails on the materialized workspace | Forward correction in the code by the fix specialty (`PR-VFS-CSL-6`, `PR-QA-14`). |
| A test plan loosens the policy's Definition of Done | The policy's Definition of Done stands (`PR-QA-6`). |
| A mutant survives on a must-not-fail row | Delivered to the test author; the strengthened tests pass a verdict and are consolidated, and mutation re-runs the affected rows; unresolved when acceptance closes, it is declared residual risk (`PR-QA-16`). |
| An exploration or non-functional finding fails | Ordinary recovery: the fix specialty repairs the code; a contract defect goes to its owner (`PR-QA-18`). |
| A matrix row has no evidence when acceptance closes | The completion evaluation records it unmet and the report declares it as residual risk (`PR-QA-10`, `PR-QA-19`). |

## 4. Pattern References (informative)

The quadrants of `PR-QA-2` follow Marick's agile testing quadrants as developed by Crispin and Gregory. The strategy, policy, plan, environment requirements, and completion report follow the document set of ISO/IEC/IEEE 29119-3; test levels follow ISO/IEC/IEEE 29119-1. Targeted mutation of `PR-QA-15` follows Meta's ACH (Automated Compliance Hardening): few, risk-targeted mutants with equivalent-mutant filtering, rather than exhaustive mutation. Charters and heuristics of `PR-QA-17` follow session-based test management, Bach's SFDPOT, and Bolton's FEW HICCUPPS oracles. Q4 characteristics follow ISO/IEC 25010. These are pattern references, not requirements to adopt their tools.
