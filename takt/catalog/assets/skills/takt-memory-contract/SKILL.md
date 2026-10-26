---
name: takt-memory-contract
description: "Before using memory. The single content guide for every Takt agent: what a memory is, what each nature, scope and relation means, when to record, what never to record, and how to read memory."
license: AGPL-3.0
metadata:
  author: takt
  version: "2.1"
---

# Takt Memory Contract

This is the only definition of memory rules for Takt agents. Where any other memory guidance
disagrees with this contract, this contract wins.

## 1. What a memory is

A memory records **something that happened or what is true now**: an idea explored, a
fact verified, a decision taken, a file created or changed, a problem found, a
disagreement raised. It is written in **past or present tense** and is anchored to that
event.

Also record the deliverable your specialty was assigned to produce. A specialist whose work is a plan, task breakdown, proposal, or design records
that deliverable. Do not add plans, predictions, or recommendations outside your assignment.
A direct user order to remember something is recorded as that order ("The user asked to
keep X"); set `user_order` only then.

A good entry has a short, searchable `title` that names what happened, and a `content`
of a few sentences: what happened or is true, why, and the files or artifacts involved.
Engram stores every final planning or research result and every formal invariant. Record
each complete artifact in one dedicated `project` entry, with no other result, process, or
commentary mixed in. Aim for under roughly 400 lines and wrap
`content` as prose paragraphs of no more than 500 words. Keep the whole entry under
**45,000 bytes**; that limit alone decides a split. Genuinely distinct artifacts each get
their own self-contained entry. If an artifact
exceeds the byte limit, split it into ordered parts, each `supplements` the previous one.

The Engram write must succeed before the result is delivered. A requested filesystem
copy is a courtesy: produce it once, with the native edit tool, using the content already
recorded in Engram. A failed courtesy copy does not invalidate the recorded entry. Present
its ID through the assigned handoff operation, not through a phrase in your final answer.

## 2. Natures

| `nature` | Meaning |
|---|---|
| `proposal` | An option raised or explored and not adopted, with its tradeoff |
| `decision` | A choice the user approved, with the approval as `evidence` |
| `observation` | A result verified against evidence. `evidence` is mandatory: the file, command, test output, or entry `#id` that shows it |
| `hypothesis` | An interpretation not verified, stated as such |

Recording, citing, or grouping an entry never promotes its nature. A `hypothesis`
confirmed later is a **new** `observation` that `supersedes` it. Whoever records a user
decision is its author; the authority is the user, and it goes in `evidence`.
Orchestrator acceptance of an autonomous result permits downstream use; it does not
turn that result into a user-approved `decision`.

Nature is fixed; currency follows the §4 links: an entry another `supersedes` is superseded,
one another `disputes` is in dispute. Neither is deleted.

## 3. Scope and tiers

Memory has two canonical tiers, and the `scope` you choose picks one:

- `project` — the **workspace tier**: architectural invariants, technical constraints,
  domain models, and verified facts of this repository.
- `personal` — the **global tier**: durable user preferences, communication habits, and
  environment quirks that hold across projects.

- A global-tier entry carries nothing of the project: no code, no repository file paths,
  no workspace directory, no proprietary business logic. Read the global tier freely from
  project work; never write workspace artifacts into it.
- When unsure, use `project`.

## 4. Relations

Entries are never overwritten or deleted. A change in understanding is a **new** entry
whose `relates_to` names the affected entry by its `#id` and one relation:

| `relation` | Use when |
|---|---|
| `corrects` | The earlier entry was wrong; yours is the complete new version |
| `supersedes` | The earlier entry was valid and no longer governs; yours is the complete new version |
| `supplements` | A distinct artifact that adds to the earlier entry without changing it: the next part of a split artifact, or another author's matrix column |
| `exception-to` | The earlier entry still governs except in a stated scope |
| `disputes` | You disagree and nobody with authority has settled it |

- A revision of an artifact (a correction, a refinement, a later report) is the whole
  current artifact, readable alone, so readers need only its `#id`. Never record a diff, a
  patch, the changed sections, or "the rest stands", and never `supplements` your own artifact.
- Only user approval or verified evidence can `correct` or `supersede`. A later entry or a
  different role never settles a disagreement: without that authority, use `disputes`.
- Before declaring a relation, confirm the entry and its `#id`. Never guess an `#id`.
- Relations stay inside one tier: `relates_to` names an entry of the same scope, never one
  across the tier boundary.
- Same fact, same nature, still current: record nothing.

Knowledge in another role's domain is still recorded by you when your work produced it.
When unsure who usually writes it, `references/domain-writers.md` names the natural writer
per domain.

## 5. When to record

- In an autonomous delegation, call `memory_record` once per entry **when the assigned work
  finishes**, before you return your result, not after every step. Keep entries concise.
  A brief never waives this; if it does, follow the contract.
- A specialist holding the chat with the user records its artifact progressively: each draft
  is a complete `hypothesis` entry that `supersedes` the previous draft, and the version the user
  accepts is a new entry that `supersedes` the last draft with that accept as `evidence`.
  Only the accepted entry reaches other agents; drafts stay in memory.
- For invariant results, the complete-result rule in §1 takes precedence over concision;
  specialists working directly with the user or through a switch obtain any required
  approval before declaring a final result.
- A user preference or guidance noticed in dialogue is recorded only when the user
  expressed it; it usually belongs in `personal`. When it is unclear whether they meant it as
  lasting guidance, confirm before persisting it.
- Record only what you would stand behind. A memory the user explicitly asked you to record is
  always captured, however brief or narrow.

## 6. Never record

- Ordinary conversation, intermediate reasoning, or task progress.
- Process notes, status updates, or a second copy of a deliverable already recorded.
- Tool output dumps or code excerpts.
- Anything forbidden by §1.

## 7. Reading memory

- An entry whose `#id` you already know is retrieved directly with `mem_get_observation`; no
  prior search is needed. For an unknown topic, search with `mem_search` and judge by title.
  Open only what your work needs now, never an entry you only pass on by ID. Locate with
  `mem_search`, never with `mem_context`, which returns whole sessions.
- When a known `#id` cannot be retrieved, report that `#id` and the actual error; never
  substitute another entry found by search for it.
- A delegation arrives with the invariants it consumes, or with the declaration that none
  exists yet. Those entries bind as read-only and prevail over any restatement in the brief;
  with none declared, author from the brief. A session lent to you names them in `requirements`.
- Verified repository state prevails over a contradicting memory. When they
  disagree, trust the repository and record an `observation` that `corrects` the entry.
- Entries that were corrected or superseded are history, never directives.
- Open `disputes` stay open: present them as open, settled neither by recency nor by
  another role's assertion.
- Never promote a `proposal` or `hypothesis` into a fact when you rely on it.
