---
name: takt-workflow-selection
description: "Before a planning or implementation phase starts, choose the route that governs it from a decision tree; return here with new evidence when the adopted route shows it does not fit."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.1"
---

# Takt Workflow Selection

The tree below selects the route for a phase; adopt the route it reaches and load only that
route's skill. Never load another workflow's skill to help decide. How the adopted route runs,
and whether it keeps fitting, belongs to its own skill. Keep the objective, phase, user
constraints, available invariants, and any route that already reported a misfit, with its
reason and evidence, in view.

## Decision tree

1. **Did the user directly and explicitly request direct work for this phase?**
   - Yes, planning without specialists → adopt `takt-bounded-planning`.
   - Yes, implementation without delegation → adopt `takt-bounded-workflow`.
   - No → go to 2. Respect the instruction's stated scope: a task-wide no-delegation order
     covers its phases, including verification; a phase-only order does not extend to others.
2. **Are required invariants missing or in need of refinement?**
   - Yes → adopt `takt-invariant-planning` to generate or refine them through their owners.
     Do not regenerate settled invariants. Once they are delivered, return to 1 with the
     updated evidence. Invariant planning stands on its own; when SDD follows it, its
     specifications, and its test plan when QA is planned, feed TPM, SDD's first node.
     A general or ambiguous request requires final user stories among those invariants; a
     trivial or narrowly technical request does not.
   - No; existing invariants are sufficient → go to 3. An invariant is settled when a current
     Engram entry holds it, judged by title and relations, not body, and its consumer reads the body; it is missing when no such
     entry exists, and to refine when the objective changes what the entry holds. A specification is
     also settled when a repository artifact of equivalent rigor holds it. QA is planned, as the
     exception, only when acceptance needs the QA workflow rather than individual quality
     specialists. Signals, in any phase: a must-not-fail priority, a criterion crossing several frozen
     contracts, abundant tests that add no value, complexity needing hard guarantees, a drastic earlier failure, a
     legacy refactor, or the user's request. If unclear, ask `qa`, or `architect` for complexity.
     Its test plan and traceability matrix are then required invariants too. Judge the whole
     objective: a focused review alone does not activate QA; do not relabel integrated acceptance
     to avoid it or manufacture QA artifacts when the focused brief suffices.
3. **Is development still needed?**
   - Yes, as bounded units — independent ones, or one implementation unit with its diagnosis and
     verification → delegate to the needed specialists under OPERATIONS' unplanned-delegation limit
     and commitment rules; no route is adopted.
   - Yes, as several implementation units producing an integrated result → adopt `takt-sdd-workflow` once formal,
     broken-down specifications exist; without them they are required invariants: go to 2.
   - With QA planned, either development branch above ends in `takt-qa-workflow` as its
     acceptance tail.
   - No, the objective assesses, tests or hardens an existing deliverable → adopt `takt-qa-workflow`
     only when QA is planned under step 2; otherwise delegate the focused assessment to the
     relevant quality specialist with its scope and criteria. Do not omit committed checks;
     findings do not authorize repairs or certify the product.
   - No, nothing else → deliver the settled result; no implementation route is needed.

Bounded is never chosen by elimination, convenience, dispatch cost, or absence of
specialists. SDD and bounded never govern the same implementation simultaneously: end the
affected route's governance before adopting another, preserving unrelated progress and
ownership.

## Return from an adopted route

When the adopted route shows, with evidence gathered while running it, that it does not fit
this work, it returns control with the reason, evidence, and constraints. Restart at 1 with
that evidence. Never re-adopt the same route under identical evidence, including an
explicitly requested bounded route.

When the tree leads back to routes that already reported a misfit under the same evidence:
- If OPERATIONS' three direct-work tests hold, adopt the phase's bounded route
  (`takt-bounded-planning` or `takt-bounded-workflow`) by decision, limited to that small action
  and respecting ownership.
- Otherwise, if the work is bounded units within OPERATIONS' unplanned-delegation limit,
  delegate to the needed specialists; never replace them with Takt. A single planning
  specialty is delegated to its specialist, not executed directly by default.
- Otherwise, load `takt-workflow-selection-exceptions`.

If uncertainty persists or proceeding requires changing the user's explicit instruction,
load `takt-workflow-selection-exceptions`; never override that instruction. Evaluate each
new phase on its current evidence, not the previous phase's choice.
