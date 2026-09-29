# Catalog authoring conventions

What a skill must be (its segmentation, cross-reference direction, orphan prohibition, and
the rule that every prohibition states its alternative) is normative and lives in
`product/governance/DOCTRINE.md` (STR-11..14), binding through `STR-10`. This file only
covers what DOCTRINE itself excludes from its own scope (`STR-7`: naming, formatting, and
file layout are the project's own, expressed through its tooling) — the house style for
writing a skill file, once its type and boundaries are already settled.

## Format

Prefer a table or a list over a dense paragraph wherever the content is enumerable —
criteria, steps, a decision between named options, a mapping from a signal to an outcome.
Reserve prose paragraphs for a single rule that needs its own reasoning stated inline, one
bold-led sentence per rule.

## Suffix naming for exception skills

An exception skill's name extends its paired flow skill's name with one of two suffixes,
chosen by what the exception actually does:

| Suffix | Use when | Example |
| --- | --- | --- |
| `-recovery` | The exception retries or backtracks work already committed to a specific plan or DAG in progress. | `takt-sdd-recovery` |
| `-exceptions` | The exception handles a deviation in a live protocol or a decision that has not yet committed anything (nothing to backtrack). | `takt-interlocutor-exceptions`, `takt-workflow-selection-exceptions` |

## Frontmatter `description`

No `"Trigger:"` label prefix, no self-referential audience notes, no taxonomy jargon
("exception handling only"). Open directly with the real-world situation that lands
someone on this skill, under ~120 words. This is the convention for every skill written
from this point forward; it is not retrofitted onto older skills as part of unrelated work.
