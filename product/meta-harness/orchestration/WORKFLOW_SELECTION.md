# Workflow selection

At each planning or implementation boundary, select the route using the following tree. Re-evaluate only when the goal, required inputs, or route-fit evidence changes.

## Decision tree

| Step | Question | Answer → outcome |
| --- | --- | --- |
| 1 | Did the user directly and explicitly request direct work for this phase? | Planning without specialists → bounded planning. Implementation without delegation → bounded workflow. No → step 2. |
| 2 | Are invariants needed for the next executable slice missing or invalid? | Yes → invariant planning; once the slice invariants are delivered, back to step 1 with the updated evidence. A general or ambiguous request requires final user stories among the required invariants; a trivial or narrowly technical one does not. Invariant planning stands on its own; when SDD follows it, its specifications, and its test plan when QA is planned, feed TPM, SDD's first node. With QA planned, its applicable test-plan section and traceability rows are required inputs. No → step 3. |
| 3 | Is development still needed? | Independent bounded units → delegate within the cumulative unplanned limit; bounded diagnosis, implementation and verification use a compact plan. Integrated implementation units → the SDD workflow with slice-scoped specifications and acceptance criteria (memory or equivalent repository artifact); without them → step 2. With QA planned, either development branch uses the QA workflow as its acceptance tail. Assessment, testing or hardening → QA workflow if planned, otherwise the relevant quality specialist. Nothing remains → return the result. |

Phase-only direct-work orders do not extend to other phases. Judge QA planning against the whole objective, consulting the QA lead when unclear; a focused review alone does not activate it. Keep committed checks; findings authorize neither repair nor certification.

Applicable invariants are consumed, not regenerated; resolve their recorded version and evolution before use. Bounded is never chosen by elimination, convenience, cost, or absence of specialists. SDD and bounded never govern the same implementation simultaneously.

## Return from an adopted route

| Situation | Outcome |
| --- | --- |
| The adopted route reports, with evidence, that it does not fit | Back to step 1 with the reason, evidence, and constraints; the same route is never re-adopted under identical evidence. |
| The tree leads only to routes that already reported a misfit, and the adjustment is trivial: one file, one concern, and no contract or interface change or other specialty's judgment | The orchestrator adopts the phase's bounded route by decision, limited to that small action. |
| Same, not trivial, and the work is bounded units within the unplanned-delegation limit | Delegation to the needed specialists; a single planning specialty still goes to its specialist. |
| Uncertainty persists or nothing fits | The orchestrator resolves the gap within its delegated authority, or escalates it when it is a value decision reserved to the user. |
| Proceeding requires changing an explicit user instruction | The orchestrator consults the user with evidence and alternatives. |
