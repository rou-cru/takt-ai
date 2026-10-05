---
name: takt-workflow-selection
description: "Before a planning or implementation phase starts, choose the route that governs it from a decision tree; return here with new evidence when the adopted route shows it does not fit."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
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
   - No → go to 2. An instruction for one phase does not constrain the other.
2. **Are required invariants missing or in need of refinement?**
   - Yes → adopt `takt-invariant-planning` to generate or refine them through their owners.
     Do not regenerate settled invariants. Once they are delivered, return to 1 with the
     updated evidence: invariant planning and SDD are complementary stages.
   - No; existing invariants are sufficient → go to 3.
3. **Is development still needed?**
   - Yes → adopt `takt-sdd-workflow`.
   - No → deliver the settled result; no implementation route is needed.

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
- If triviality is demonstrated within OPERATIONS' direct-work bounds, adopt the phase's
  bounded route (`takt-bounded-planning` or `takt-bounded-workflow`) by decision, limited to
  that justified small action and respecting ownership.
- Otherwise, if OPERATIONS' lightweight delegation conditions are satisfied, delegate to the
  needed specialists within its round limit; never replace them with Takt. A single planning
  specialty is delegated to its specialist, not executed directly by default.
- Otherwise, load `takt-workflow-selection-exceptions`.

If uncertainty persists or proceeding requires changing the user's explicit instruction,
load `takt-workflow-selection-exceptions` and consult the user; never override that
instruction. Evaluate every new phase on its own current evidence, not on the previous
phase's choice.
