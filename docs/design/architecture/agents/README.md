# Agent behavior as a deterministic system

A model of the crew as processes that exchange messages, assuming the happy path: every agent does what its prose says. It is derived from the catalog prose (`takt/catalog/assets/agents/*/agent.yaml`, `OPERATIONS.md` and skills), not from runtime traces. Harness acts are marked as security-colored nodes and messages in the diagrams.

| View | Diagram | Type |
| :--- | :--- | :--- |
| Every agent, grouped by role class, with the harness | [crew-topology.html](diagrams/crew/crew-topology.html) | Architecture |
| How takt picks a route | [workflow-selection.html](diagrams/crew/workflow-selection.html) | Workflow |
| Intent through a lent takt-pm | [planning-intent.html](diagrams/flows/planning-intent.html) | Sequence |
| Analysis, architecture and experience in one round | [planning-design.html](diagrams/flows/planning-design.html) | Sequence |
| Specification and task breakdown | [planning-decompose.html](diagrams/flows/planning-decompose.html) | Sequence |
| Implementation, verification, consolidation | [delivery-sequence.html](diagrams/flows/delivery-sequence.html) | Sequence |
| Repair after a failing verdict (takt-fix) | [repair-sequence.html](diagrams/flows/repair-sequence.html) | Sequence |
| Blind review by judge-a and judge-b | [judge-sequence.html](diagrams/flows/judge-sequence.html) | Sequence |

Role classes appear as containers; each agent is its own node or participant. takt-simplify's cleanup and GC role is shown in the GC cycle view in [runtime](../runtime/README.md) and in the topology, not in a separate sequence.

## Orchestrator decision trees and state machines

Everything takt decides is conduct from `OPERATIONS.md` and its skills. These views show that conduct as decision trees (workflow type) and state machines (lifecycle type).

| View | Diagram | Source prose |
| :--- | :--- | :--- |
| Session lifecycle | [session.html](diagrams/orchestrator/session.html) | OPERATIONS, session-resume, memory-orchestrator |
| Resuming earlier work | [session-resume.html](diagrams/orchestrator/session-resume.html) | takt-session-resume |
| Route misfit and return to the tree | [selection-return.html](diagrams/orchestrator/selection-return.html) | takt-workflow-selection, exceptions |
| Invariant-planning states | [invariant-planning.html](diagrams/orchestrator/invariant-planning.html) | takt-invariant-planning |
| SDD workflow lifecycle | [sdd-lifecycle.html](diagrams/orchestrator/sdd-lifecycle.html) | takt-sdd-workflow |
| Failure and recovery | [sdd-recovery.html](diagrams/orchestrator/sdd-recovery.html) | takt-sdd-recovery, OPERATIONS |
| Escalation conduct | [escalation.html](diagrams/orchestrator/escalation.html) | OPERATIONS |
| Preparing a delegation | [delegation.html](diagrams/orchestrator/delegation.html) | OPERATIONS |
| Handling a staged delivery | [delivery.html](diagrams/orchestrator/delivery.html) | OPERATIONS, takt-vfs-coordination |
| Acting directly | [direct-work.html](diagrams/orchestrator/direct-work.html) | OPERATIONS |
| Lending an interlocutor, reading the return | [lending.html](diagrams/orchestrator/lending.html) | takt-interlocutor-lending |

Not drawn as diagrams:

| Decision | Rule |
| :--- | :--- |
| Which specialist for which need | Unverified context: analyst. Intent and scope: pm. Behavior and acceptance: spec. Structure and technology: architect. Experience or identity: product-designer. Task decomposition: tpm. Implementation: dev. Defect repair: fix. Cleanup: simplify. Independent verdict: verify, never omitted from a planned verification. Each is omitted when settled artifacts already cover the need. |
| When to request the blind judge pair | Only when the user asks, work advanced past deviations of unknown origin, the real state is unknown beyond a quick validation, a DAG ends with two or more phases that each reached the ceiling, or a large refactor of unknown impact has invariants to contrast. |
| Whether a phase is verified | A committed plan is required when a round reaches the concurrency ceiling of implementers or the change is critical; below that floor it is planned when the restored confidence outweighs the cost, and skipped for documentation or minor adjustments. |
| Contesting a recorded failure | `dispatch_contest`, at most the per-session contest budget; it requests independent verification and takt never chooses the verifier. |
| GC cycles | Requested only when the user asks, after `gc_prepare` validated analyzers and acceptance commands; ordinary cleanup goes to simplify. |

## Processes

Role classes come from `agent.yaml`.

| Class | Agents | VFS capabilities | Receives | Produces |
| :--- | :--- | :--- | :--- | :--- |
| orchestrator | takt | claim_list, claim_assign, claim_release, discard, consolidate | Objective from the user | Briefs, plan, DAG, final report |
| planning_author | analyst, spec, tpm | none | Brief and consumed invariants | One Engram record per artifact, returned as IDs |
| direct_interlocutor | pm, architect, product-designer | none | Brief, or a lent session with requirements | Decisions with user approval, returned as IDs |
| execution | dev, fix, simplify | bind, write, read, delete | Brief, writable set, acceptance | Staged work and an author key |
| verification | verify, judge-a, judge-b | read, verify (gates arrive bound; no bind tool) | Brief, author keys | A verdict per author key, plus a report |

## Messages

- Delegation: one-paragraph brief (objective, authorized scope, expected output, acceptance). Invariants never travel in the brief; they are declared by Engram ID.
- Lend: Takt asks the user, then switches the interface to a pm, architect or product-designer with requirements, client and objective. The specialist hands back one of Standard, EarlyHandoff, TechFault or Outraged (or the lend is aborted).
- Delivery: planning and interlocutor results are Engram IDs; execution results are staged work plus an author key; verification results are verdicts bound to author keys plus a report recorded in Engram and delivered by ID with `deliver_result`. A later report is a new entry that supplements the earlier one.
- Two-part delivery: a specialist's delivery ends in VFS; takt releases each one where the plan sends it (phase verification, consolidation or a spot fix) before delegating more.
- Ownership: only Takt owns claims, the DAG, delegation, consolidation and Git.

## Harness touchpoints

| Agent step | Harness act | Enforcement shown in code | Evidence |
| :--- | :--- | :--- | :--- |
| Takt commits a plan, declares consumed IDs | Records units and prerequisites in the execution history | Entries are validated on append | `takt/history/history.go` |
| Takt delegates | Admission: concurrency ceiling, unit already in flight, withdrawn unit, unplanned-unit bound, recovery limits | Denial is recorded and changes no state | `takt/dispatch/dispatch.go` |
| Takt assigns a claim | Reserves the exact file set; a same-root collision is a typed error; without an author key, overlapping claims from other roots are discarded with their staged work; with it, the existing work continues | Deterministic | `takt/vfs/operation.go` |
| Specialist binds | Scope check and base capture for every scoped path | Deterministic | `takt/vfs/operation.go` |
| Execution stages | Revision increments and any verdict is cleared | Deterministic | `takt/vfs/vfs.go` |
| Verifier gates | Prepared bound from the delegation's author keys and read by path; needs the verify grant; self-verification refused; verdict bound to revision and delta hash; a verdict on changed staged work is returned for a re-read | Deterministic | `takt/vfs/operation.go`, `takt/agents/opencode/assets/takt-vfs.ts` |
| Any VFS operation is refused | The refusal blocks only that operation; a collision is recorded for observation and never latches | Deterministic | `takt/vfs/vfs.go` |
| Takt consolidates | Orchestrator-only; flushes through a manifest and verifies bases; partial flush blocks all calls until recovery | Deterministic | `takt/vfs/vfs.go`, `takt/vfs/recovery.go` |
| Takt lends an interface | Records a switch; denies a role that is ineligible or a second holder | Deterministic | `takt/dispatch/interlocutor.go` |
| Agent writes memory | Validates, anchors to the session, links to related entries, deduplicates by ID | Deterministic | `takt/memory/memory.go` |
| Anyone runs Git | Git mutation is guarded by role | Permission rule from the renderer | `takt/agents/opencode/renderer.go` |
| Orchestrator goes idle | GC coordinator decides whether a cycle is due | Deterministic | `takt/cli/gc_coordinate.go` |
| Any tool or dispatch event | Event envelope validated and appended to the event store | Deterministic | `takt/cli/obs.go` |

## Conduct the prose asks for, with no harness act in the model

- Choosing a route with the workflow-selection tree, and never re-adopting a route under identical evidence.
- Cutting a DAG for concurrency and the width self-check before commit.
- Comparing outputs across product, behavior, architecture and experience, and returning incompatibilities to their owners.
- Resuming a session from its anchors and confirming continuity with the user.
- The escalation triggers and the recovery conduct (freeze, restore only the abandoned scope, report).

## Fidelity notes

- Role grants and skill lists are read from `agent.yaml`; the happy-path order is from `OPERATIONS.md` and the SDD and invariant-planning skills.
- Policy values such as the concurrency ceiling are placeholders in the prose; the numbers live in `takt/dispatch/admission_policy.yaml`.
- The judge is one agent definition run as two blind instances, judge-a and judge-b; the topology shows one node and the judge sequence shows both.
- takt-analyst appears only when there is a concrete question; the other planning lanes follow the invariant-planning rules.
