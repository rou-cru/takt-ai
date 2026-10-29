# Engineering Doctrine of Takt AI

## 1. Structural Norms

| ID | Norm |
| --- | --- |
| STR-1 | **Single definition.** Every fact, rule, and structure has exactly one definitional home; every other site refers to it rather than restating it. Design establishes that home before implementation. Divergence between sites is realigned to the home, never reconciled by adding a third site. |
| STR-2 | **Fail fast by design.** An invalid state is rejected where it first becomes representable, not tolerated and compensated downstream. When a consumer must defend itself against its producer, the correction moves the boundary instead of adding the defense. |
| STR-3 | **Data contracts.** Data crosses a contract ready for its declared use: it arrives usable and leaves as agreed. A consumer does not re-validate or re-normalize what the contract already guarantees, and a producer does not emit a shape its contract does not promise. |
| STR-4 | **Validation at acquisition.** Data obtained or produced once logic is already under way — from outside the system, from the user, from inference — is validated and normalized at the point it enters. |
| STR-5 | **Present requirement only.** Capability is built for a requirement that exists, never for one anticipated. Abstraction is justified by multiplicity that exists, not by multiplicity expected. A single implementation behind an interface, a factory for one product, and configuration for a value that never varies are excluded. |
| STR-6 | **Reuse before creation.** What the workspace already provides is reused or extended before an equivalent is written. Duplication introduced because existing code was not located is a defect. |
| STR-11 | **Skill segmentation.** A skill declares exactly one responsibility from a closed taxonomy: a *routing* skill selects which other skill applies, and nothing else, adopted only when that choice cannot be resolved without one; a *flow* skill states the normal procedure for its own scope; an *exception* skill pairs with exactly one flow skill, stays shorter than it, and triggers only on an actual deviation from that flow. |
| STR-12 | **Cross-reference direction.** A skill names another only toward a more specific, whole skill, never a subsection, and only downward: a general or always-loaded document may name a specific skill, and a flow skill may name its own paired exception skill or the more capable route it defers to. No two skills name each other back, and no skill states how another skill or role carries out its part — it may say the matter belongs to that other skill, never how that skill handles it. |
| STR-13 | **No orphan skill.** Every skill has at least one real consumer, declared on some agent's skill list or declared common to all agents. |
| STR-14 | **Prohibition states its alternative.** An instruction that forbids an action names the action that replaces it, or how to choose among the valid ones. |
| STR-15 | **Single authority for state.** Every runtime fact a decision can rest on, above all one that can refuse, block, or admit work, has exactly one owner store, and the decision reads that owner when it acts. A copy, cache, mirror, or local projection of the fact never takes part in a decision, whatever its freshness guarantee; a component that needs the fact asks its owner, and a derived fact is recomputed from its owner rather than stored beside it. Components cooperating on the same work (processes, plugins, agents, stores) form one distributed system with one authority per fact, not peers that synchronize. A second writer of a fact, or a decision resting on a copy, is a defect and never an accepted trade-off; the correction removes the copy, never adds reconciliation. This norm admits no exception. |
| STR-16 | **Concurrency controlled by design.** Wherever two actors can reach the same fact concurrently, the design names its serialization point (lock, queue, or transaction), and every read a decision rests on happens inside the same serialized unit as the action it authorizes. Check-then-act across separate invocations, separate locks, or an asynchronous gap is a defect. Contention waits a bounded time and then refuses with the call that retries it; it never proceeds unserialized. |

## 2. Limits

| ID | Limit |
| --- | --- |
| STR-7 | The doctrine governs structure, not taste. Naming, formatting, file layout, language idiom, and library or technology selection are the project's own, expressed through its tooling and declared configuration. No agent extends this document by inference. |

## 3. Binding Surface

| ID | Requirement |
| --- | --- |
| STR-10 | The doctrine binds through the canonical source: the definitions of the agents Takt installs, the skills it ships, and the deterministic behavior of the harness. It is never written into the user's repository, and Takt neither authors nor amends documents there to carry it. |

## 4. Resolution of doubts

When two structural norms pull apart, the reading that leaves less structure standing prevails.
