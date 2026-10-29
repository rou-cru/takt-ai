# Interactions across layers

How the crew's prose, contracts and runtime behave as one distributed system, from install to a delivered result. These views integrate the per-topic views in [agents/](../agents/README.md) and [runtime/](../runtime/README.md); they do not repeat them.

## Agents: decisions and artifacts

| View | Diagram | Type |
| :--- | :--- | :--- |
| Who takt delegates to, the trigger, and what comes back | [delegation-map.html](diagrams/delegation-map.html) | Architecture |
| Planning artifacts: author, input, mechanism | [artifact-chain-planning.html](diagrams/artifact-chain-planning.html) | Workflow |
| Planning handoff by Engram ID, with coherence | [planning-handoff.html](diagrams/planning-handoff.html) | Sequence |
| From DAG to delivered code: staged work, verdicts, QA reports | [artifact-chain-delivery.html](diagrams/artifact-chain-delivery.html) | Workflow |
| QA workflow states | [qa-workflow.html](diagrams/qa-workflow.html) | Lifecycle |

## System: layers, contracts and runtime

| View | Diagram | Type |
| :--- | :--- | :--- |
| The three layers and their stores | [layers-overview.html](diagrams/layers-overview.html) | Architecture |
| How the catalog becomes the installed crew | [catalog-projection.html](diagrams/catalog-projection.html) | Dataflow |
| A request to the crew, end to end | [request-sequence.html](diagrams/request-sequence.html) | Sequence |
| Admission and guiding refusals | [admission-refusals.html](diagrams/admission-refusals.html) | Workflow |
| Control loop, wired and not wired | [control-loop.html](diagrams/control-loop.html) | Dataflow |

## Artifacts by agent

| Agent | Produces | Consumes |
| :--- | :--- | :--- |
| pm | User stories; business documents (a different pm unit writes them) | User words; story IDs |
| analyst | Investigation, environment facts | A concrete question |
| product-designer | Experience rules, tokens, style guides | Story and document IDs; contracts |
| architect | ADR, C4, interface contracts | Story, document and fact IDs; experience rules |
| spec | BDD spec or FRD | Stories, documents, design, contracts |
| qa | Test policy, test plan, matrix (requirement, condition, must-not-fail) | Spec, contracts, existing policy |
| tpm | WBS with owning node per matrix row | Spec, design, contracts, plan, matrix |
| dev, fix, simplify | Staged delta under an author key | Invariant IDs, brief; fix gets one finding |
| test | Staged Q2 tests | Spec, contracts, its matrix rows; never the code |
| verify | Verdict per author key; acceptance and completion reports; matrix evidence | Staged files; all invariants |
| judge-a, judge-b | Two unmerged blind finding reports | Identical brief |
| mutation, explorer, quality | One observation report each | Plan, matrix, acceptance report |
| takt | Briefs, committed DAG, consolidation, commits, session record | Everything above, by ID |

## Layers

| Layer | What it is | Owner of its facts |
| :--- | :--- | :--- |
| Behavior | Agent and skill prose, composed BASELINE, PERSONA, SOUL, OPERATIONS | Catalog |
| Contract | Agent-to-agent messages and agent-to-harness acts: admission, claims, delivery, verdicts, memory | `takt-ai` stores (history, VFS) and Engram |
| Runtime | OpenCode sessions and plugins, MCP servers, one-shot `takt-ai` calls | OpenCode and the plugins that call `takt-ai` |

## Related views

| Topic | Where |
| :--- | :--- |
| Install steps | [runtime/diagrams/install-sequence.html](../runtime/diagrams/install-sequence.html) |
| Work unit, VFS revision, GC, memory | [runtime/](../runtime/README.md) |
| Orchestrator conduct, route selection, repair, judges | [agents/](../agents/README.md) |

## Known gaps

- The control plane has sensors, stores and actuators for refusal, gate, escalate and rollback. Detector families and throttle are taxonomy only; contain acts solely through gc mandate demotion.
- Dreaming has no runtime.
- The concurrency ceiling is 4 (`takt/dispatch/admission_policy.yaml`).

## Regenerating

Same procedure as the parent [README](../README.md): run archify `finalize` on each file in `sources/` with `--repo-root` at the repository root and `--quality showcase`. Sources pin the revision they were checked against.
