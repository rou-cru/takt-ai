---
name: takt-qa-workflow
description: "Selected when an objective's work is assessing, testing or hardening a deliverable, or when a route closes the acceptance of an objective with QA planned. Governs QA planning, blind test authoring, acceptance, the mutation, exploration and quality critique, and the test completion report as one state machine. Orchestrator-only guide."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Takt QA Workflow

> **Target Audience:** Orchestrator agent only. QA specialists consume their assignments,
> not this skill.

Governs the objective's testing as states. Each state names what you delegate, the entries
you declare with `dispatch_inputs` for it, the result that ends it, and the next state. Every
datum travels by Engram ID; a brief never restates an entry it declares.

## Entry

| You arrive from | Start at |
| --- | --- |
| The selection tree, for an objective that assesses, tests or hardens a deliverable | 1. Assess |
| `takt-sdd-workflow`, at the acceptance of an objective with a test plan | 4. Accept |
| A bounded unit whose change touches behavior a current test plan covers | 4. Accept, for the matrix rows of that unit |

An objective reaches this skill from the second and third entries only with QA planned, its
test plan and matrix already delivered by invariant planning; one without QA planned is held
to the Definition of Done in force, without a test plan.

## States

| State | Delegate, with `dispatch_inputs` | Ends when | Next |
| --- | --- | --- | --- |
| 1. Assess | Nothing; read the current entries | Specification settled (Engram, or a repository artifact of equivalent rigor) | Test plan and matrix settled → 3; otherwise → 2 |
| | | Specification missing | Return control to Takt: a required invariant is missing |
| 2. Plan | `qa`: the specification, the frozen contracts, the PRD, the analyst's facts, the workspace test policy when one exists | `qa` delivers the policy (when authored), plan, matrix and environment requirements (when authored) | 3 |
| 3. Author | `tpm`: the plan and the matrix, for the test-author tasks and each row's owning node. Then one `test` per task: the specification, the frozen contracts, the matrix; each task's test files reserved with `claim_assign` | Every test task's staged work passes its Verify gate and is consolidated | 4 |
| 4. Accept | `verify` on the materialized workspace: the plan, the matrix, the specification. The brief states that it closes the objective's acceptance when the plan selects no mutation, exploration or quality | `verify` delivers its report | Failure → the outcome table; the plan selects Q3 or Q4, or the matrix holds must-not-fail rows → 5; otherwise this report closed the acceptance → 8 |
| 5. Critique | In one concurrent round, only what the plan selects. `mutation`: the plan, the matrix. `explorer`: the plan. `quality`: the plan, the specification | Each delivers its report | A surviving mutant → 6; a failed finding → the outcome table; otherwise → 7 |
| 6. Strengthen | `test` with its `author_key` through `claim_assign`: the specification, the contracts, the matrix, the mutation report. Its Verify gate, then consolidation. Then `mutation`: the plan, the matrix, the mutation report, naming the affected rows in the brief | `mutation` reports no survivor on a must-not-fail row, or the recovery budget is spent | 7 |
| 7. Close | `verify` on the materialized workspace when 5 ran: the plan, the matrix, the latest report of each specialty that ran. The brief states that it closes the objective's acceptance | `verify` delivers the test completion report | 8 |
| 8. Report | Nothing | Your completion report consumes the test completion report by ID and states every residual risk it lists | The objective's acceptance is closed |

Declare state 6 as one recovery before its first delegation: `dispatch_declare_recovery`
with the binary result "no surviving mutant on a must-not-fail row". A spent budget leaves
the survivors as residual risk, and the closing node states them.

In this route, commit states 3 through 7 with `dispatch_commit` before the first delegation
of state 3; inside SDD they are nodes of its DAG.

## Outcomes

| Outcome | Goes to | Declared with `dispatch_inputs` |
| --- | --- | --- |
| A Q2 test fails on the workspace, in an objective that delivers the code | `fix`, with the failed finding verbatim in the brief | The failing `verify` report |
| A Q2 test fails, in an objective that only assesses the code | No delegation: it is a finding of the test completion report, repair is the user's decision, and the states continue | — |
| `dev`, `fix` or `test` returns a contradiction between a test and the specification or a contract | The contract's owner: `spec` for behavior, `architect` for an interface; freeze the nodes that consume the contract | The specification or contract entry; the test path goes in the brief |
| The owner rules the test wrong | `test` with its `author_key` through `claim_assign` | The ruling |
| The owner rules the code wrong | `fix` | The ruling |
| An exploration or quality finding fails, in an objective that delivers the code | `fix`; a finding the owner of a contract must decide goes to that owner | The report |
| An exploration or quality finding fails, in an objective that only assesses the code | No delegation: it is a finding of the test completion report, and the states continue | — |
| `qa` returns an acceptance criterion too vague to test | `spec`, then 2 again with its revision | The specification entry |

After a repair consolidates, return to 4.

## When this route does not fit

If the objective turns out to need new functionality rather than testing, or the evidence
shows this route does not fit, return control to Takt with the reason, evidence and user
constraints. This exit does not select a replacement.
