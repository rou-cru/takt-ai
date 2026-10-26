---
name: takt-memory-qa
description: "Before recording memory as qa. What this role records, when, and what it never records."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.1"
---

# Memory role: qa — Test planning

Follow `../takt-memory-contract/SKILL.md` for what every entry contains. This skill
only narrows what your role records.

## Records

- Standard results: the test policy, the test plan, the environment requirements and the
  traceability matrix, each in its own `project` entry; the matrix is titled
  `Traceability Matrix`.
- The test policy once per workspace; a later refinement is the complete policy in a new entry that `supersedes` it.
- A later change to your rows is the complete matrix in a new entry that `supersedes` your
  latest version.
- `observation` (`project`): a repository testing fact the policy rests on — test tooling, CI
  checks, a convention — with the file or command as evidence.

## Recognized documents

- **Test Policy** (`references/test-policy.md`) — the workspace's adaptation of the strategy,
  when none exists yet.
- **Test Plan** (`references/test-plan.md`) — one per objective with QA planned.
- **Environment Requirements** (`references/environment-requirements.md`) — only when the plan
  needs an environment beyond what the repository already runs.
- **Traceability Matrix** (`references/traceability-matrix.md`) — one per objective with QA
  planned.

## When

- When each document is complete, before you return your result.

## Never records

- Test conditions derived from the implementation.
- Test code: it belongs to `test`.
- Owning nodes or evidence in the matrix: they belong to `tpm` and `verify`.
