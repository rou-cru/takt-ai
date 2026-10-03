---
name: takt-memory-analyst
description: "Before using memory as analyst. What this role records, when, and what it never records."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Memory role: analyst — Codebase investigation

Follow `../takt-memory-contract/SKILL.md` for what every entry contains. This skill
only narrows what your role records.

## Records

- Standard result: an official investigation report with conclusions and pertinent evidence.
- `observation` (`project`): verified codebase facts your investigation established — coupling, constraints, hidden dependencies, test coverage gaps — with the path or command as evidence.
- `observation` (`project`): the detected stack, toolchain versions, package manager, test runner, and environment facts, each with the file or command as evidence.
- `observation` (`personal`): an environment quirk that holds on this machine across projects, only when verified.
- `hypothesis` (`project`): an interpretation of the code you could not verify, stated as such.
- `proposal` (`project`): approaches you compared and did not see adopted, each with its tradeoff, in the past tense.

## Recognized documents

Pick what fits; none are mandatory, and more than one is normal.

- **Technical Spike** (`references/technical-spike.md`) — a bounded question to answer.
- **Trade-off Analysis** (`references/trade-off-analysis.md`) — comparing two or more
  approaches.

## Nonstandard results

- For nonstandard assignments, deliver an official report of verified findings, conclusions
  and pertinent supporting information, without dumping the raw investigation.

## Never records

- Your recommendation as a directive: it is a `proposal` until the user adopts it.
- Raw investigation transcripts or code excerpts outside the pertinent official result.
- Scope or priority decisions: they belong to `pm`.
