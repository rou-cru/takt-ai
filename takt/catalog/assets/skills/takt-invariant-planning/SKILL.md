---
name: takt-invariant-planning
description: "Choosing and ordering the planning specialists that generate invariants before implementation."
license: AGPL-3.0
metadata:
  author: takt
  version: "2.0"
---

# Takt Invariant Planning

Orders the planning lanes (analyst, pm, product-designer, architect, spec) that generate the
invariants before implementation. It governs only these lanes, never implementation nodes.
Work through the states in order; each names the condition that ends it, and a state is left
only once that condition holds.

## 1. Assess

For each invariant the objective needs (intent, facts, experience, structure, behavior),
classify it from the evidence at hand: **settled** (existing, approved, validated — no lane),
**to refine** (a lane consumes the existing entry and revises it), or **to generate** (a lane
authors it). Ends when every needed invariant is classified; "probably settled" is not a class.

## 2. Select

One lane per invariant to generate or refine: pm for intent, analyst for objective facts that
are unknown and recoverable, product-designer for experience rules, architect for structure
and technology, spec for observable behavior. Ends when the lane set is fixed. A single lane
is bounded work and leaves this skill.

## 3. Plan

Connect lanes by consumption: an edge exists only where one lane's decisions depend on
another's result. A lane started before its input exists guesses it.

- pm roots the plan while intent is unsettled: every other lane decides against it.
- analyst runs on a concrete question derived from an invariant already in hand — a fact that
  intent, experience, or structure depends on — never first by default with nothing to
  investigate.
- designer and architect are independent unless one's decisions constrain the other; both
  feed spec as strict invariants.
- Lanes whose inputs are all available form one concurrent round within the
  four-specialist ceiling.

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

When a round returns, compare its documents with each other. On a contradiction, decide which
stands (the one backed by the user's directive or verified facts) and re-dispatch only the other
lane with its original document, its first result, the standing document and the contradiction.
When neither is backed, the conflict stays disputed: how many lanes consume a document is not
evidence, so ask the user or gather verified facts before deciding. You decide; the lane rewrites. Two failed alignments
of one conflict: escalate. Ends when no contradiction remains open; planning is then complete.

## Specialist unavailable

If a lane's specialist cannot be dispatched, or its dispatch fails past the recovery bounds in
OPERATIONS, name that lane blocked per the crew's declared fallback and proceed: lanes that do
not consume its result keep running per the plan, and lanes that do wait on the named gap
rather than guessing. Escalate per OPERATIONS if the objective cannot be judged without it.

## Mode per lane

A directly selected specialist or one reached by switch works with the user; an ordinary
delegation from the orchestrator is autonomous. A lane with the user needs the user's approval
before its consumers start, and one interlocutor holds the interface at a time. If the user
starts with a given lane, it is the root: its consumers go after it and what it settled is
omitted.
