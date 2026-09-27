---
name: takt-memory-contract
description: "Trigger: before using memory. The single content guide for every Takt agent: what a memory is, what each nature, scope and relation means, when to record, what never to record, and how to read memory."
license: AGPL-3.0
metadata:
  author: takt
  version: "2.0"
---

# Takt Memory Contract

This is the only definition of memory rules for Takt agents. Your role skill
(`.opencode/skills/takt-memory-<role>/SKILL.md`) says **what** your role records; this contract
says what a good memory contains. Where any other memory guidance disagrees with this
contract, this contract wins.

Requirement IDs refer to `ARCH_MEMORY` (MEM-*).

## 1. What a memory is

A memory records **something that happened or what is true now**: an idea explored, a
fact verified, a decision taken, a file created or changed, a problem found, a
disagreement raised. It is written in **past or present tense** and is anchored to that
event (MEM-TYP-7).

Record what happened, what is true now, or the deliverable your specialty was assigned to
produce. A specialist whose work is a plan, task breakdown, proposal, or design records
that deliverable. Do not add plans, predictions, or recommendations outside your assignment.
A direct user order to remember something is recorded as that order ("The user asked to
keep X"); set `user_order` only then.

An idea that was explored but not adopted is a `proposal` in the past tense ("X was
proposed as ..."), not a plan.

A good entry has a short, searchable `title` that names what happened, and a `content`
of a few sentences: what happened or is true, why, and the files or artifacts involved.
Engram stores every final planning or research result and every formal invariant. Record
each complete artifact in one dedicated `project` entry, with no other result, process, or
commentary mixed in. Consumers retrieve that entry directly by its ID; they do not search
for it or reconstruct it. A requested filesystem copy must match the entry and never replaces
it. If the write fails, the result is not delivered. Present its ID through the assigned
handoff operation, not through a phrase in your final answer.
Its nature follows §2.

## 2. Natures (MEM-TYP-5/6)

| `nature` | Meaning |
|---|---|
| `proposal` | An option raised or explored and not adopted, with its tradeoff |
| `decision` | A choice the user approved, with the approval as `evidence`. Any role may record it |
| `observation` | A result verified against evidence. `evidence` is mandatory: the file, command, test output, or entry `#id` that shows it |
| `hypothesis` | An interpretation not verified, stated as such |

Recording, citing, or grouping an entry never promotes its nature. A `hypothesis`
confirmed later is a **new** `observation` that `supersedes` it. Whoever records a user
decision is its author; the authority is the user, and it goes in `evidence`.
Orchestrator acceptance of an autonomous result permits downstream use; it does not
turn that result into a user-approved `decision`.

What an entry **is** and what it **governs today** are separate (MEM-TYP-6). Nature is
fixed; currency follows the evolution links already recorded (§4): an entry another
`supersedes` is superseded, one another `disputes` is in dispute. So an entry stops
governing without being deleted, and recording a correction or a supersession promotes
nothing — a `proposal` stays a `proposal` (MEM-RET-5).

## 3. Scope and tiers (MEM-SCP-1..4)

Memory has two canonical tiers, and the `scope` you choose picks one:

- `project` — the **workspace tier**: architectural invariants, technical constraints,
  domain models, and verified facts of this repository.
- `personal` — the **global tier**: durable user preferences, communication habits, and
  environment quirks that hold across projects.

- A global-tier entry carries nothing of the project: no code, no repository file paths,
  no workspace directory, no proprietary business logic. Read the global tier freely from
  project work; never write workspace artifacts into it.
- When unsure, use `project`. Never copy project content into `personal`.

## 4. Relations (MEM-OPS-1..3)

Entries are never overwritten or deleted. A change in understanding is a **new** entry
whose `relates_to` names the affected entry by its `#id` and one relation:

| `relation` | Use when |
|---|---|
| `corrects` | The earlier entry was wrong |
| `supersedes` | The earlier entry was valid and no longer governs |
| `supplements` | Adds to the earlier entry without changing it |
| `exception-to` | The earlier entry still governs except in a stated scope |
| `disputes` | You disagree and nobody with authority has settled it |

- Only user approval or verified evidence can `correct` or `supersede`. A later entry or a
  different role never settles a disagreement: without that authority, use `disputes`.
- Before declaring a relation, search memory (`mem_search`, then `mem_get_observation`) to
  find the entry and its `#id`. Never guess an `#id`.
- Relations stay inside one tier: `relates_to` names an entry of the same scope, never one
  across the tier boundary.
- Same fact, same nature, still current: record nothing.

Knowledge in another role's domain is still recorded by you when your work produced it
(MEM-TYP-4). The table only helps avoid two roles recording the same thing:

| Domain | Natural writer |
|---|---|
| Workspace stack, toolchain, environment facts | analyst |
| Codebase facts, dependencies, impact | analyst |
| Product scope and priorities | pm |
| Behavioral requirements and acceptance rules | spec |
| Architecture, boundaries, interface contracts | architect |
| Interaction and UX | product designer |
| Adopted sequencing and dependency choices | tpm |
| Non-obvious implementation facts | dev |
| Defect root causes | fix |
| Verified acceptance results | verify |
| Review findings that contradict memory | judges |
| Session objective and state | orchestrator |

## 5. When to record (MEM-TYP-1..3)

- Call `memory_record` once per entry **when the assigned work finishes**, before you
  return your result, not after every step. Keep entries concise.
- For invariant results, the complete-result rule in §1 takes precedence over concision:
  autonomous delegates record before returning each result; specialists working directly
  with the user or through a switch obtain any required approval before declaring a final
  result. An orchestrator's autonomous acceptance is not user approval evidence.
- A user preference or guidance noticed in dialogue is recorded only when the user
  expressed it; it usually belongs in `personal`. When it is unclear whether they meant it as
  lasting guidance, confirm before persisting it.
- Record only what you would stand behind. A memory the user explicitly asked you to record is
  captured even if brief or narrow in scope; the concision guidance above never becomes a
  reason to skip a direct request.

## 6. Never record

- Ordinary conversation, intermediate reasoning, or task progress.
- Process notes, status updates, or a second copy of a deliverable already recorded.
- Tool output dumps or code excerpts.
- Anything forbidden by §1.

## 7. Reading memory

- Read with `mem_search`, `mem_get_observation`, and `mem_context`.
- Verified repository state prevails over a contradicting memory (MEM-RET-3). When they
  disagree, trust the repository and record an `observation` that `corrects` the entry.
- Entries that were corrected or superseded are history, never directives.
- Open `disputes` stay open: present them as open, settled neither by recency nor by
  another role's assertion (MEM-RET-4/5).
- Never promote a `proposal` or `hypothesis` into a fact when you rely on it.
