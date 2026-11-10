---
name: takt-memory-contract
description: "Before using Takt memory. The Takt memory rules every Takt agent applies beyond the `takt_memory_*` tools' own descriptions: what to record, how artifacts and revisions are recorded, when to record, what never to record, and how to read Takt memory."
license: AGPL-3.0
metadata:
  author: takt
  version: "2.2"
---

# Takt Memory Contract

This is the only definition of Takt memory rules for Takt agents beyond the `takt_memory_*` tool
descriptions. Where any other Takt memory guidance disagrees with this contract, this contract wins.

## 1. What a Takt memory is

Besides what `takt_memory_record` describes, record the deliverable your specialty was
assigned to produce. A specialist whose work is a plan, task breakdown, proposal, or design records
that deliverable. Do not add plans, predictions, or recommendations outside your assignment.
A direct user order to remember something is recorded as that order ("The user asked to
keep X").

Record every final planning or research result and every formal invariant, each artifact
whole in one dedicated `workspace` entry, with no other result, process, or commentary mixed
in. Genuinely distinct artifacts each get their own self-contained entry.

The Takt memory write must succeed before the result is delivered. A repository copy exists only
when the user asks for one, and carries the content already recorded. Present the entry's ID
through the assigned handoff operation, not through a phrase in your final answer.

## 2. Natures

Orchestrator acceptance of an autonomous result permits downstream use; it does not turn
that result into a user-approved `decision`.

## 4. Relations

- A revision records the complete artifact and `corrects` or `supersedes` the entry it revises.
- Same fact, same nature, still current: record nothing, except to deliver it as your result.

Knowledge in another role's domain is still recorded by you when your work produced it.
When unsure who usually writes it, `references/domain-writers.md` names the natural writer
per domain.

## 5. When to record

- In an autonomous delegation, call `takt_memory_record` once per entry **when the assigned
  work finishes**, before you return your result, not after every step.
  A brief never waives this; if it does, follow the contract.
- A specialist holding the chat with the user records a new artifact progressively: each draft
  is a `hypothesis` entry that `supersedes` the previous draft, and the version the user
  accepts is a `decision` that records the complete artifact and `supersedes` the last draft.
  Only the accepted entry reaches other agents; drafts stay in Takt memory.
- Drafts exist only before an artifact's first approval. A change to an approved artifact is a
  revision (§4), recorded as a `decision` once the user accepts it; nothing unaccepted
  supersedes an approved version.
- Specialists working directly with the user or through a switch obtain any required
  approval before declaring a final result.
- A user preference or guidance noticed in dialogue is recorded only when the user
  expressed it; it usually belongs in `global`. When it is unclear whether they meant it as
  lasting guidance, confirm before persisting it.
- Record only what you would stand behind. A Takt memory the user explicitly asked you to record is
  always captured, however brief or narrow.

## 6. Never record

- Ordinary conversation, intermediate reasoning, or task progress.
- Process notes, status updates, or a second copy of a deliverable already recorded.
- Tool output dumps or code excerpts.
- Anything forbidden by §1.

## 7. Reading Takt memory

- For an unknown topic, search with `takt_memory_search` and judge by title.
- Open with `takt_memory_read` only what your work needs now, and of a long entry only the
  sections it needs; never open an entry you only pass on by ID.
- Entries a session lent to you names in `requirements` bind as read-only.
- Pass an invariant only by the ID of its current version, never by summary or file path. A
  document the user supplies is recorded before it is passed, and from then on only its ID
  travels.
- Verified repository state prevails over a contradicting Takt memory: trust the repository and
  record an `observation` that `corrects` the entry, or that `disputes` it when the entry is a
  decision.
- Present open `disputes` as open.
