---
name: takt-invariant-planning
description: "Choosing and ordering the planning specialists that generate invariants before implementation."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.2"
---

# Takt Invariant Planning

Orders the planning lanes (analyst, pm, product-designer, architect, spec) that generate the
invariants before implementation. It governs only these lanes, never implementation nodes.
It applies when two or more lanes must produce invariants; whatever is already settled
(existing, approved, validated) removes the lane or the edge.

**Order by consumption.** A lane starts once every invariant result it consumes has been recorded;
lanes whose inputs are all available run together within the four-specialist ceiling. No lane has a
fixed place: a chain, a fan-out, a diamond or independent lanes are right when they match what is
consumed. A lane started before its input exists guesses it. Each brief carries only the Engram
entry IDs of the results that lane consumes; a filesystem path or summary is not a substitute.

**Select by need, connect by consumption.** analyst when environment, tools, prior work or other
objective facts are unknown and recoverable; pm when intent is unsettled;
designer and architect when experience rules and structure are missing; spec when observable
behavior is missing. Usual edges: analyst feeds every lane whose decisions depend on its findings;
designer and architect are independent unless one's decisions constrain the other; both feed spec
as strict invariants. Each edge is absent when its input is already settled.

**Coherence.** When a level returns, compare its documents with each other. On a contradiction,
decide which stands (the one backed by the user's directive or verified facts; on a tie, the one
more lanes consume) and re-dispatch only the other lane with its original document, its first
result, the standing document and the contradiction. You decide; the lane rewrites. Two failed
alignments of one conflict: escalate.

**Specialist unavailable.** If a lane's specialist cannot be dispatched, or its dispatch
fails past the recovery bounds in OPERATIONS, name that lane blocked per the crew's
declared fallback and proceed: lanes that do not consume its result keep running per the
order above, and lanes that do wait on the named gap rather than guessing. Escalate per
OPERATIONS if the objective cannot be judged without it.

**Mode per lane.** A directly selected specialist or one reached by switch works with the user;
an ordinary delegation from the orchestrator is autonomous. A lane with the user needs the user's approval before its
consumers start, and one interlocutor holds the interface at a time. With the order left open,
refine gradually: context, facts, intent, experience, structure, behavior. If the user starts with a
given lane, it is the root: its consumers go after it and what it settled is omitted.
