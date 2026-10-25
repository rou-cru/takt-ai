---
name: takt-invariant-planning
description: "Choosing and ordering the planning specialists that generate invariants before implementation."
license: AGPL-3.0
metadata:
  author: takt
  version: "2.0"
---

# Takt Invariant Planning

Orders the planning lanes (analyst, pm, product-designer, architect, spec, qa) that generate the
invariants before implementation. It governs only these lanes, never implementation nodes.
Work through the states in order; each names the condition that ends it, and a state is left
only once that condition holds.

## 1. Assess

For each invariant the objective needs (user stories, intent, facts, experience, structure, behavior, and,
when QA is planned, the workspace test policy, the test plan and the traceability matrix),
classify it from the evidence at hand: **settled** (existing, approved, validated — no lane),
**to refine** (a lane consumes the existing entry and revises it), or **to generate** (a lane
authors it). Ends when every needed invariant is classified; "probably settled" is not a class.
The user's statement of intent is what pm formalizes, never a settled pm invariant; pm's
requirements are settled only when an artifact of equivalent rigor holds them. A general or
ambiguous request needs user stories; a story is settled only when final, with no question about
its expected result open. When a story is revised, every invariant derived from it is **to refine**.

## 2. Select

Each invariant to generate or refine has its owner: pm for user stories and, in a separate
delegation, for requirements formalization, analyst
for objective facts that are unknown and recoverable, product-designer for experience rules,
architect for structure and technology, spec for observable behavior when the objective needs
it formalized, qa for the test policy, test plan, traceability matrix and environment
requirements when QA is planned. Ends when the lane set is fixed. Apart from qa as the only lane over a settled
specification, a single unit is bounded specialist work and
leaves this skill: return control to Takt with that reason for reevaluation. This does not authorize direct execution or replacing the specialist's
judgment with Takt's. Whenever the evidence shows this route does not fit, likewise return
control with the reason, evidence, and constraints preserved.

**Expanded lane.** When one owner's invariant spans focus areas or document types that one
brief cannot cover at the required detail (pm's focus areas name them), expand that lane into
a sub-DAG of instances of the same owner, one per focus area or document type, each a planned
unit under every rule of this skill. A consolidating instance of that owner, consuming the
partial results to remove contradictions, align details and fill gaps, is recommended; omit it
when consumers can take the partial results directly (one architect consuming two designer
results). After consolidation, you decide which partial invariants stop serving.

## 3. Plan

Connect lanes by consumption: an edge exists only where one lane's decisions depend on
another's result. A lane started before its input exists guesses it.

- pm roots the plan whenever it is a lane: stories first, then the business documents a
  different pm delegation formalizes from them; every other lane decides against those. No lane
  consuming a story starts while a question about its expected result is open: close it with the
  user or the stories' pm first. Spec follows pm directly only when no experience or structure lane
  stands between them.
- analyst runs on a concrete question derived from an invariant already in hand — a fact that
  intent, experience, or structure depends on — never first by default with nothing to
  investigate.
- designer and architect are independent unless one's decisions constrain the other; both
  feed spec as strict invariants.
- qa follows spec, and architect when it is a lane, consuming the specification, the frozen
  contracts and pm's PRD, whose must-not-fail priorities it plans against; the plan then ends
  `S → Q`.
- Lanes whose inputs are all available form one concurrent round within the
  {{policy.concurrent_specialists}}-specialist ceiling.

The order, concurrency, and edges derive from what is settled and what must be generated or
refined, never from a fixed sequence. With more than one lane known in advance, commit this
planning DAG with `dispatch_commit` before the first dispatch, each lane a unit with its
prerequisites. `references/planning-dag-examples.md` shows how common scenarios resolve; read
it when building the DAG. Ends when the plan is committed.

## 4. Dispatch

A lane starts once every result it consumes has been delivered. Declare its consumed Engram IDs
with `dispatch_inputs`, or declare that none exists yet; the brief never restates them. Issue
every lane of a round in the same response. Ends when every lane has delivered.

## 5. Coherence

When a round returns, compare its documents with each other. On a contradiction between documents of different rank, the higher one stands: the user's
directive, then user stories, then business documents, then what derives from them. Between equal
ranks, decide which stands (the one backed by the user's directive or verified facts). Re-dispatch
only the other lane with its original document, its first result, the standing document and the contradiction.
Between qa and spec, spec stands for behavior. When neither is backed, the conflict stays disputed: how many lanes consume a document is not
evidence, so ask the user or gather verified facts before deciding. You decide; the lane rewrites. Two failed alignments
of one conflict: escalate. Ends when no contradiction remains open; planning is then complete. Return the delivered invariants to Takt to evaluate development
against the updated evidence, not to regenerate them.

## Specialist unavailable

If a lane's specialist cannot be dispatched, or its dispatch fails past the recovery bounds in
OPERATIONS, name that lane blocked, apply BASELINE's fallback and proceed: lanes that do
not consume its result keep running per the plan, and lanes that do wait on the named gap
rather than guessing. Escalate per OPERATIONS if the objective cannot be judged without it.

## Mode per lane

A directly selected specialist or one reached by switch works with the user; an ordinary
delegation from the orchestrator is autonomous. A lane with the user needs the user's approval
before its consumers start, and one interlocutor holds the interface at a time. If the user
starts with a given lane, it is the root: its consumers go after it and what it settled is
omitted.
