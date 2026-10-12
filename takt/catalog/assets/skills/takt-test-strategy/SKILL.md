---
name: takt-test-strategy
description: "When testing for an objective is being planned, a test policy or test plan is authored, or a test plan's acceptance tail runs: the quadrants and levels testing is organized in, who owns each, when each QA specialty enters, the baseline Definition of Done, and the rules that keep acceptance tests independent of the code they judge."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Takt Test Strategy

The organizational test strategy of every workspace the crew works in. Test policies and test
plans refer to it and never restate it. A direct user instruction prevails within its scope.

## Quadrants

| Quadrant | Orientation | Purpose | Owner |
| --- | --- | --- | --- |
| Q1 | Technology-facing, supporting the team | Unit tests of the delivered unit | `dev` |
| Q2 | Business-facing, supporting the team | Acceptance and contract tests | `test` writes; `verify` runs and judges |
| Q3 | Business-facing, critiquing the product | Exploratory testing | `explorer` |
| Q4 | Technology-facing, critiquing the product | Non-functional analysis | `quality` |

No quadrant but Q1 is owned by the specialty that implemented the code it tests. Plan a
quadrant only when the objective's acceptance needs it, never as an unsolicited protection.

## Levels

| Level | Quadrant | Tested against |
| --- | --- | --- |
| Component | Q1 | The delivered unit's public interface |
| Integration and contract | Q2 | An interface frozen by contract |
| System and acceptance | Q2 | The specification |

The test plan chooses the levels per objective.

## When each specialty enters

| Specialty | Enters when |
| --- | --- |
| QA planned at all | The specification states acceptance criteria spanning more than one work unit, or the objective carries a must-not-fail risk |
| `qa` | The specification exists; it plans as a planning author |
| `test` | The contracts it tests are frozen and the traceability matrix exists, concurrently with implementation |
| `mutation` | The Q2 suite passes on the materialized workspace and the matrix holds must-not-fail rows |
| `explorer` | The deliverable is materialized and the plan selects Q3 |
| `quality` | The test plan selects Q4 naming an ISO/IEC 25010 characteristic, which its brief carries; it gives the non-functional acceptance verdict instead of `verify` |

An objective with no QA planned is held to the Definition of Done in force, without a test plan.
While QA is planned, the test plan and traceability matrix are required invariants of the
objective, as the specification is.

## Baseline Definition of Done

1. Every planned quadrant has run.
2. The acceptance checks pass on the materialized workspace.
3. Every traceability matrix row carries evidence.
4. No must-not-fail row has a surviving non-equivalent mutant.
5. Every finding left open is declared as residual risk.

The test policy carries the workspace's default, derived from this baseline; a test plan may
tighten it, never loosen it. With no test policy, this baseline is the Definition of Done in force.

## Traceability

| Column | Sole author |
| --- | --- |
| Requirement, test condition, must-not-fail marking | `qa` |
| Owning DAG node | `tpm` |
| Evidence, with the evaluated scope | `verify` |

Every addition is an entry that supplements the matrix, never a rewrite. A requirement with no
test condition, a condition with no owning node, and a row with no evidence when acceptance closes
each stay visible as such.

## Oracles

| Rule | Instead |
| --- | --- |
| Q1 unit tests and fix regression tests never reach a unit's internals | Exercise only its public interface |
| Q2 tests are never written from the implementation | Write them from the specification, the frozen contracts and the matrix |
| An implementer never edits a Q2 test to make it pass | Return the contradiction as a blocker; the orchestrator routes it to the owner of the contract at issue, and `test` corrects the test only when that owner rules it wrong |
| A failing Q2 test is never repaired in the test | `fix` repairs the code |
| A surviving mutant never goes to the implementer | `test` strengthens the suite; one unresolved on a must-not-fail row when acceptance closes is residual risk |

## Acceptance tail

1. `verify` runs acceptance on the materialized workspace: build, lint, suites including Q2,
   and end to end.
2. The `mutation`, `explorer` and `quality` nodes the test plan selects run next.
3. When any of them ran, a closing `verify` node consumes their reports by ID, adds each
   matrix row's evidence and records the test completion report. When none ran, the acceptance
   `verify` node closes and records it.

Surviving mutants loop before the closing node: `test` strengthens the suite, a `verify` gate
judges the staged tests, they are consolidated, and `mutation` re-runs only the affected rows,
within the ordinary recovery budgets. A failed `explorer` or `quality` finding goes through
ordinary recovery: `fix` repairs the code, and a contract defect goes to its owner. When
`quality` gives the non-functional acceptance verdict, it replaces only that verdict; the
closing `verify` still records the evidence and the report, consuming `quality`'s report.

The orchestrator's completion report consumes the test completion report by ID without
omitting its residual risk.
