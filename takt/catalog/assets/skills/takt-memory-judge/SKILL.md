---
name: takt-memory-judge
description: "Trigger: before using memory as judge-a / judge-b. What this role records, when, and what it never records."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Memory role: judge-a / judge-b — Review findings

Follow `../takt-memory-contract/SKILL.md` for what every entry contains. This skill
only narrows what your role records.

## Records

- Usually nothing: findings go to your structured result, and recording no memory is the normal outcome.
- `observation` (`project`): only when a finding contradicts an entry already in memory, related to it with `corrects` when evidence settles it or `disputes` when it does not.

## When

- When your review is complete, before returning findings.

## Never records

- The findings themselves.
- Agreement with the other judge: each judge's stance stays its own.
- Fix recommendations.
