---
name: takt-test-strategy
description: "When a test policy or test plan is authored, or tests are written against one: the quadrants and levels testing is organized in, who owns each, the baseline Definition of Done, the authors of the traceability matrix, and the rules that keep acceptance tests independent of the code they judge."
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
| An implementer never edits a Q2 test to make it pass | Return the contradiction as a blocker; the owner of the contract at issue rules, and only that ruling makes `test` correct the test |
| A failing Q2 test is never repaired in the test | `fix` repairs the code |
| A surviving mutant never goes to the implementer | `test` strengthens the suite; one unresolved on a must-not-fail row when acceptance closes is residual risk |
