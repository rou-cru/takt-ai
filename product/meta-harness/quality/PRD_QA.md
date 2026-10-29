# PRD: Quality Assurance and Testing

## 1. Functional Requirements

### 1.1 Organizational Test Strategy

| ID | Requirement |
| --- | --- |
| PR-QA-2 | Testing is organized in four testing quadrants, each with one owning specialty, per the table below. No quadrant is owned by the specialty that implemented the code it tests, except Q1. Q2 acceptance tests are part of every SDD slice; any other quadrant is planned only when the objective's acceptance needs it, never added as an unsolicited protection. |
| PR-QA-3 | Test levels are component, integration, system, and acceptance. Component tests are Q1; integration and contract tests at an interface frozen by contract, and system and acceptance tests against the specification, are Q2. The test plan chooses the levels per objective. |
| PR-QA-4 | QA is planned as the exception. Signals, in any phase: a must-not-fail priority, an acceptance criterion crossing more than one frozen contract, abundant tests that add no value, complexity needing hard guarantees, a drastic earlier failure, a legacy refactor, or the user's request; the QA lead or the architect may be consulted. Quality specialists remain delegable on their own without it. With QA planned, the QA lead enters as a planning author once the specification exists. The test author enters once the contracts it tests are frozen and, with QA planned, the matrix exists, before implementation. The mutation analyst enters only when Q2 is selected, once the original Q2 suite is executable and passes on the materialized workspace and the matrix holds must-not-fail rows. The explorer enters on the materialized deliverable when the plan selects Q3. The quality analyst enters when the test plan selects Q4 naming a non-functional quality characteristic, which the analyst receives in the test plan. While QA is planned, the objective's test plan and traceability matrix are required invariants: with the specification settled and the test plan missing, invariant planning runs with the QA lead as its only planning lane. An objective with no QA planned is held to the Definition of Done in force without a test plan. |
| PR-QA-5 | For executable product changes, the baseline Definition of Done requires passing acceptance and regression checks and functional end-to-end execution by the verifier as isolated independent tester. Non-executable deliverables require a recorded deterministic check of their declared acceptance criteria instead of software execution. Every applicable traceability row requires evidence and all planned quadrants MUST complete. No must-not-fail row may retain a non-equivalent surviving mutant. Failed, blocked, impossible, and unverified criteria remain unmet. Report open findings as residual risk without treating that report as satisfaction of an unmet criterion. The baseline applies without a test plan; a policy or plan may only tighten it. |
| PR-QA-21 | The QA lead is a planning author. The test author is an ordinary execution instance. The mutation analyst, the explorer, and the quality analyst are quality-assurance instances: each holds its own specialty, and they share the class's conduct on the materialized workspace. |

| Quadrant | Orientation | Purpose | Owner |
| --- | --- | --- | --- |
| Q1 | Technology-facing, supporting the team | Unit tests of the delivered unit | dev |
| Q2 | Business-facing, supporting the team | Acceptance and contract tests | Test author writes blind; verifier runs and judges |
| Q3 | Business-facing, critiquing the product | Exploratory testing | Explorer |
| Q4 | Technology-facing, critiquing the product | Non-functional analysis | Quality analyst |

### 1.2 Test Policy

| ID | Requirement |
| --- | --- |
| PR-QA-6 | Each workspace has exactly one test policy, adapting the strategy to that project. The QA lead authors it once, the first time QA is planned there, from repository facts — agent instruction files, CI configuration, test tooling, and conventions — and persists it as a planning-author artifact whose memory entry lives in the Workspace Tier. The policy carries the workspace's default Definition of Done, derived from the baseline of `PR-QA-5`; a test plan may only tighten it. |

### 1.3 Test Plan and Environment

| ID | Requirement |
| --- | --- |
| PR-QA-7 | The QA lead authors a logical test plan per objective from the specification, frozen interfaces, and test policy. It is partitioned by story and slice; each unit receives only relevant conditions. It states quadrants, levels, entry and exit criteria, must-not-fail risks, and effective Definition of Done. The next slice's plan is delivered before its test author starts. |
| PR-QA-8 | The QA lead declares non-trivial environment requirements from analyst facts: services, data, configuration, and platforms. The orchestrator assigns provisioning to an execution unit before the checks that need it. An unverified check opens an environment-provisioning unit in the slice's work breakdown, with an owner and a completion check. |

### 1.4 Requirements Traceability

| ID | Requirement |
| --- | --- |
| PR-QA-9 | Each objective with QA planned has one logical requirements traceability matrix, partitioned by story and slice: requirement → test condition → owning DAG node → evidence. The QA lead creates it with the requirement, the test condition, and the must-not-fail marking; TPM adds the owning node for every row independently of quadrant selection (including plans without Q2); the verifier adds the evidence with its evaluated scope. Each column has exactly that one author: only the QA lead creates the matrix, an entry evolving it is accepted only from a column author and is attributed to that author's column, and an author with no column records a dispute instead. |
| PR-QA-10 | Every addition to the matrix is a supplementing memory entry linked to the matrix, never a rewrite of it. A requirement with no test condition, a condition with no owning node, and a row with no evidence when acceptance closes are each visible in the matrix as such. |

### 1.5 Test Authorship and Oracle

| ID | Requirement |
| --- | --- |
| PR-QA-11 | Q1 unit tests are black-box: they exercise only the delivered unit's public interface, never its internals. The regression test the fix specialty writes for a defect is black-box under the same rule. |
| PR-QA-12 | The test author derives Q2 tests from the specification, frozen interfaces, and applicable traceability rows, never from implementation. Before implementation it records an executable failing baseline for the intended missing behavior, distinguishing it from environment failure. The harness isolates implementation content from the test author and protects the acceptance suite from implementer writes through author-bound read-only views. Historical and remote copies are not permitted workarounds. |
| PR-QA-13 | A Q2 test is an immutable oracle for the implementer. An implementer who finds the specification and a test in contradiction returns a blocker naming both, and the orchestrator routes it to the owner of the contract at issue; the implementer never edits a test to make it pass. When that owner rules the test wrong, the test author corrects it; otherwise the test stands. |
| PR-QA-14 | A genuine Q2 failure is corrected in implementation. A contract conflict follows PR-QA-13; the implementer cannot change the oracle. |

### 1.6 Mutation Adequacy

| ID | Requirement |
| --- | --- |
| PR-QA-15 | The mutation analyst judges whether the Q2 suite detects faults where they matter: it generates a small number of mutants targeted at the code behind must-not-fail matrix rows, on a copy outside the project, never in the workspace. It discards equivalent mutants before reporting. |
| PR-QA-16 | Mutation survivors go to the test author, not the implementer. Strengthened tests MUST compile, pass repeatedly on the original implementation, and increase relevant coverage or kill a previously surviving non-equivalent mutant. The mutation analyst reruns affected rows. Authoring-unit membership for each strengthening round is declared atomically with its plan version; started membership is immutable. A failed original baseline routes to correction, not strengthening. |

### 1.7 Exploration and Non-functional Analysis

| ID | Requirement |
| --- | --- |
| PR-QA-17 | Q3 exploration runs as chartered sessions: each charter names the area, the risk, and the heuristics used. The explorer uses the deliverable as its user would, on the materialized workspace, and records its findings in memory, delivering the entry ID. |
| PR-QA-18 | Q4 analyzes the non-functional quality characteristics selected by the test plan. The quality analyst measures each criterion and reports verified, failed, or unverified with evidence. Missing test criteria return to the QA lead; ambiguous requirements return to spec; localized code defects go to fix. A failed criterion blocks acceptance only with executable measurement evidence; otherwise the report is advisory. The QA lead records the completion evaluation. |

### 1.8 Test Completion Report

| ID | Requirement |
| --- | --- |
| PR-QA-19 | The QA lead authors the test completion report from independent acceptance evidence and the selected mutation, exploration, and non-functional reports, retrieved by ID and affected scope. Independent evidence collection may run in parallel when environments permit. The verifier supplies matrix evidence. The report states testing performed, plan deviations, completion against exit criteria, residual risk, reusable assets, and lessons. It is delivered by entry ID to the orchestrator for the user-facing result. Closing an audit does not accept the product. |

### 1.9 QA Workflow

| ID | Requirement |
| --- | --- |
| PR-QA-20 | The QA workflow is assess, plan, author, accept, critique, strengthen, close, report. Each state names its specialist, bounded input IDs, exit result, and destination. Slice acceptance applies PR-QA-5. Failed results route to diagnosis or correction; unverified results create environment work under PR-QA-8. Slice close returns to the slice control point, including critical stories. Report closes only the objective. An assessment-only objective reports findings without repairing them. Exhausted work cannot be certified with unmet criteria. Critiques with unmet entry conditions are skipped explicitly. |
