---
name: takt-qa-workflow
description: "Selected by the tree when assessment, testing or hardening needs coordinated QA, or as the acceptance tail with QA planned. Governs QA planning, blind test authoring, acceptance, critique, strengthening and reporting as one state machine."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Takt QA Workflow

> **Target Audience:** Orchestrator only. Specialists consume assignments, not this flow.

Declare each assignment's inputs with `dispatch_inputs` by Engram ID, including applicable
supplements. Briefs name the purpose and scope, never restate consumed artifacts. Distinguish
an objective that delivers or hardens code from an audit that only assesses it: an audit
preserves findings and never infers permission to repair.

## Entry

| The selection tree adopted it | Start at |
| --- | --- |
| To assess, test or harden an existing deliverable with QA planned | 1. Assess |
| As the acceptance tail with QA planned | 4. Accept |

The tail requires the test plan and matrix from invariant planning.

## States

| State | Delegate, with inputs by ID | Ends when | Next |
| --- | --- | --- | --- |
| 1. Assess | Nothing; read current entries | Specification settled (Engram or a repository artifact of equivalent rigor) | Missing plan or matrix → 2; plan selects Q2 → 3; otherwise → 4 |
| | | Required specification missing | Return the missing invariant to Takt; do not infer it |
| 2. Plan | `qa`: specification, frozen contracts, PRD, analyst facts, existing test policy | Policy when authored, plan, matrix and applicable environment requirements delivered; a diagnostic blocker is also a real result | Q2 selected → 3; otherwise → 4; blocker → outcomes |
| 3. Author | `tpm`: plan and matrix, always assigning each matrix row its owning node; creating test-author tasks only when Q2 is selected. Then `test` per task: plan, specification, contracts and matrix, with disjoint test-file claims | Each delta passes its Verify gate and consolidates | 4; gate failure or blocker → outcomes |
| 4. Accept | Before Verify, confirm every matrix row has its owning DAG node. If any are missing, delegate `tpm` only that mapping with the existing WBS and matrix; do not repeat decomposition. Then existing `verify`, without author_keys, on the materialized workspace: specification, plan, matrix and applicable supplements | Acceptance report, including failed or unverified checks | Eligible critique → 5; none and completion report delivered → 8; otherwise → 7; failures/blockers → outcomes, alongside any eligible critique |
| 5. Critique | Independent assignments concurrently by default: `mutation` only with Q2 selected, must-not-fail rows and an executable green original suite, consuming plan, matrix and acceptance report; `explorer` for selected Q3, consuming plan and acceptance report; `quality` for selected Q4, consuming plan, specification and acceptance report | Reports or diagnostic blockers delivered | Eligible survivor → 6; findings/blockers → outcomes; otherwise → 7 |
| 6. Strengthen | `test`: plan, specification, contracts, matrix and mutation report, with its existing author_key when continuing staged work. Authoring → Verify gate → consolidation → `mutation` on only affected rows, consuming plan, matrix and prior mutation report | No must-not-fail survivor, or {{policy.recovery_max_attempts}} rounds admitted | Survivor with budget → next round; exhausted → 7 with residual risk; baseline/gate failure or blocker → outcomes |
| 7. Close | Existing `verify`, without author_keys, on the materialized workspace: acceptance report, specification, plan, matrix, applicable supplements and the latest mutation, exploration and quality reports that exist. State explicitly that it closes acceptance | Matrix evidence and test completion report delivered | 8 |
| 8. Report | Nothing | Consume the test completion report by ID; state unmet criteria, unchecked work and every residual risk | Audit/report closed, not necessarily product accepted |

Critique entry criteria apply per assignment: a failed suite bars mutation, not an independent
Q3/Q4 session whose own entry conditions hold. Serialize only a concrete conflict in services,
ports, data or measurements, naming it; no general barrier between independent critiques.
An audit records ineligible critiques as not run, preserves every finding and proceeds to
Close/Report without repair. If no critique runs, Accept supplies the test completion report
when tasked to close; otherwise Close records it from the acceptance evidence.

Close consumes valid evidence; it executes only checks missing or invalidated by subsequent
changes to their evaluated scope. A strengthened suite invalidates its previous suite result;
it does not automatically invalidate unrelated measurements. Failed or unverified criteria
stay failed or unverified. Closing an audit certifies neither the product nor a milestone commit.

Commit the states that run from Author (or Accept without Q2) through Close before delegation;
inside SDD they are nodes of its DAG. Strengthen is forward correction, not reversible recovery.
Before **each** round, use `dispatch_commit` with a new version and the standing `base_version`,
plus `qa_round: { objective, units }`: the stable acceptance objective and that round's test-author
units only, not gates or mutation. The first admitted author unit consumes one round; siblings,
retries and later plan revisions do not reset or double-count it. Membership cannot migrate.
At the bound, continue to Close/Report with residual risk or request the user's finite
`dispatch_exception` on `budget/qa-strengthening-rounds` for that objective.

## Outcomes

| Outcome | Destination | Inputs by ID |
| --- | --- | --- |
| Build, lint, Q1/Q2 or end-to-end failure; failed exploration or quality finding, in a delivery/hardening objective | One `fix` per localized code defect, `dev` when defects correlate; contract owner for a contradiction; data owner for missing prerequisites | The finding that localizes it and applicable contracts |
| The same failure in an assessment-only objective | Retain finding; run critiques whose own entry holds, skip the rest; Close/Report, never inferred repair | Reports retained for closure |
| Check not run, environment/service/data/configuration blocker | Owner of the missing fact or environment, identified from plan/analyst facts; no pass assumed. In an audit retain as unverified and close | Diagnostic report, plan and relevant facts |
| A test contradicts specification or frozen interface | `spec` for behavior, `architect` for interface; freeze only consuming nodes pending the ruling | Contradicting artifacts and report; test path named in the brief |
| Owner rules test wrong | `test`, continuing its author_key only if staged work exists; gate and consolidate | Ruling and affected artifacts |
| Owner rules code wrong | One `fix` per localized defect, `dev` when defects correlate; do not alter the oracle | Ruling and the finding that localizes it |
| Verify gate fails | One `fix` per localized code defect, `dev` when defects correlate (test defects to `test`); contradiction to contract owner; unmet inputs to their owner. Keep delta staged pending decision; no automatic consolidation | Gate report and applicable inputs |
| Mutation original baseline cannot run or fails, including during Strengthen | Localized code failure to its own `fix`, `dev` when failures correlate; environment gap to its owner, contradiction to contract owner; in an audit retain blocker and close. It is not a survivor round | Mutation blocker with original command/output, acceptance report and plan |
| Q4 measurable criterion absent from plan | `qa` to complete the plan; Quality chooses measurement methods, never invents acceptance | Quality blocker, plan and specification |
| Requirement too vague to derive a criterion (`qa` or `quality`) | `spec`, then Plan with the revision | Diagnostic blocker and specification |
| Must-not-fail non-equivalent mutant survives | `test` through Strengthen, never implementer; at budget → Close with risk | Mutation report and affected rows |

After authorized repair consolidates, return to Accept for affected scope; invalidate its older
evidence and re-run the applicable critiques. A documented blocker is a completed diagnostic
result: record and deliver it through the existing handoff, without an incomplete plan or a
fictitious artifact. Resume blocked work only when its missing input or ruling arrives.

## When this route does not fit

Return control to Takt with reason, evidence and user constraints when new functionality or a
different route is needed. This exit does not select the replacement.
