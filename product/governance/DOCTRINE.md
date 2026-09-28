# Engineering Doctrine of Takt AI

**Actual implementation progress: 57%** — 14 structural clauses (STR-1..14): 3 complete, 10 partial, 1 without verifiable integration. Package separation, boundary validation, and reuse are visible in `takt/` and its contracts; enforcement exists by prompt (soft enforcement) for the requirement that every code change or crew work satisfies and preserves these rules, and by harness (deterministic enforcement) for SSOT/projection and ownership in `takt/vfs`. Single definition (STR-1), and doctrine's own scope limits — never extended by inference, never a license to act unauthorized (STR-7, STR-9) — now carry prompt-only judgment; a product requirement outranking doctrine in a conflict (STR-8) does too. Skill segmentation, cross-reference direction, and the prohibition-states-its-alternative rule (STR-11, STR-12, STR-14) are catalog prompt content only, no different from STR-1's own status; no-orphan-skill (STR-13) is the exception, checked deterministically by `TestNoOrphanSkills` in `takt/catalog/native_content_test.go`. The figure measures incorporation into the product, not the document's normative quality.

This document is the project's engineering standard. It decides conflicts about the shape of implementation.

## 1. Scope

The Constitution governs authority and value. Product requirements govern what the systems must do. This document governs how what they do is built.

It binds the crew's work in the user's workspace and the Takt AI codebase itself. The norms the crew is held to are the norms this project holds itself to; a standard the authors exempt themselves from is a recommendation, not a standard.

Article 12 is its anchor: simplicity is maintained unless a clear reason and benefit compensate for the cost of greater complexity. What follows makes that operable rather than aspirational. An evolving program's structure degrades unless resources are deliberately spent preserving it, and agents accelerate that degradation faster than review absorbs it.

The doctrine is structural. It does not decide taste.

## 2. Structural Norms

| ID | Norm |
| --- | --- |
| STR-1 | **Single definition.** Every fact, rule, and structure has exactly one definitional home; every other site refers to it rather than restating it. Design establishes that home before implementation. Divergence between sites is realigned to the home, never reconciled by adding a third site. |
| STR-2 | **Fail fast by design.** An invalid state is rejected where it first becomes representable, not tolerated and compensated downstream. A design that requires each consumer to defend itself against its producer has placed the boundary in the wrong place; moving the boundary is the correction, adding the defence is not. |
| STR-3 | **Data contracts.** Data crosses a contract ready for its declared use: it arrives usable and leaves as agreed. A consumer does not re-validate or re-normalize what the contract already guarantees, and a producer does not emit a shape its contract does not promise. |
| STR-4 | **Validation at acquisition.** Data obtained or produced once logic is already under way — from outside the system, from the user, from inference — is validated and normalized at the point it enters, so that `STR-3` holds from there onward. Deferring that work does not avoid it; it distributes it across every later consumer. |
| STR-5 | **Present requirement only.** Capability is built for a requirement that exists, never for one anticipated. Abstraction is justified by multiplicity that exists, not by multiplicity expected. A single implementation behind an interface, a factory for one product, and configuration for a value that never varies are each the absence of a requirement rather than the presence of foresight. |
| STR-6 | **Reuse before creation.** What the workspace already provides is reused or extended before an equivalent is written. Duplication introduced because existing code was not located is a defect, not a preference: locating it is part of the work, not a courtesy. |
| STR-11 | **Skill segmentation.** A skill declares exactly one responsibility from a closed taxonomy: a *routing* skill selects which other skill applies, and nothing else, adopted only when that choice cannot be resolved without one; a *flow* skill states the normal procedure for its own scope; an *exception* skill pairs with exactly one flow skill, stays shorter than it, and triggers only on an actual deviation from that flow. A skill that serves two of these at once has merged two definitional homes `STR-1` requires apart. |
| STR-12 | **Cross-reference direction.** A skill names another only toward a more specific, whole skill, never a subsection, and only downward: a general or always-loaded document may name a specific skill, and a flow skill may name its own paired exception skill or the more capable route it defers to. No two skills name each other back, and no skill states how another skill or role carries out its part — it may say the matter belongs to that other skill, never how that skill handles it. |
| STR-13 | **No orphan skill.** Every skill has at least one real consumer, declared on some agent's skill list or declared common to all agents. A skill installed with no declared reader fails `STR-5`'s present-requirement test outright, not partially. |
| STR-14 | **Prohibition states its alternative.** An instruction that forbids an action names the action that replaces it, or how to choose among the valid ones. A bare "never do X" is not a stricter rule than one with an alternative; it is an incomplete one, left for the reader to complete under pressure. |

## 3. Limits

| ID | Limit |
| --- | --- |
| STR-7 | The doctrine governs structure, not taste. Naming, formatting, file layout, language idiom, and library or technology selection are the project's own, expressed through its tooling and declared configuration (Article 12). No agent extends this document by inference. |
| STR-8 | The doctrine never waives a product requirement. Where a requirement mandates behavior this doctrine would otherwise call redundant, the requirement prevails and the doctrine governs only the shape in which it is met. |
| STR-9 | The doctrine is not a licence to act. A deviation is grounds for a finding; whether it may be corrected without the user is decided by the correcting party's own requirements, never by the weight of the norm deviated from. |

## 4. Binding Surface

| ID | Requirement |
| --- | --- |
| STR-10 | The doctrine binds through the canonical source (`SSOT-1`): the definitions of the agents Takt installs, the skills it ships, and the deterministic behavior of the harness. It is never written into the user's repository, and Takt neither authors nor amends documents there to carry it (Article 6). The norm travels in the crew, not in the user's files. |

## 5. Resolution of doubts

When the doctrine and a product requirement disagree, the requirement prevails.

When two structural norms pull apart — most often `STR-5` against `STR-6`, where reuse would require an abstraction no present requirement justifies — the reading that leaves less structure standing prevails.

When this document is insufficient to decide, the question is not resolved here: it is escalated to the document that is authoritative according to the master map in [`product/INDEX.md`](../INDEX.md).
