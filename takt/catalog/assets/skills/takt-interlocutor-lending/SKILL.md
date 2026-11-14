---
name: takt-interlocutor-lending
description: "An invariant depends on decisions only the user can close, or the user asks to talk to a specialist directly: lending the chat interface to that specialist and reading what comes back."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Takt Interlocutor Lending

Governs lending the chat interface to pm, architect or product-designer, and resuming after
it returns.

## Lend or delegate

| Situation | Route |
| --- | --- |
| The invariant to generate depends on user decisions not yet taken | Lend the interface |
| The user orders it | Lend the interface |
| Intent is already approved and the lane only consumes or refines it | Delegate autonomously |

## Lending

1. Call `dispatch_switch` naming the specialist and the objective. The user accepts them
   before the switch takes effect; a refusal means no lending: reevaluate. Its
   `requirements` list the current entry IDs of the invariants the specialist consumes and of
   the settled decisions that bind it. When no useful entry exists, pass `requirements` empty
   rather than inventing one. `client`
   carries what is known of the user. The specialist starts from `objective`, `requirements`
   and `client` alone.
2. Only the user aborts a lent session: if they ask, tell them to run `/takt-abort`.

## Reading the return

Read the returned envelope before anything else. `Result` attributes cause and never grades the
specialist.

| `Result` | Conduct |
| --- | --- |
| `Standard` | Consume the delivered entry IDs as the invariant |
| `EarlyHandoff` | Take the artifact as returned, in its actual partial state |
| `Aborted` | No content to interpret and no fault to any agent: consult the user, never reconstruct |
| `TechFault`, objective, authority and continuity established | Reconcile transition and artifact evidence; route bounded diagnosis or correction |
| `TechFault`, otherwise | Consult the user |
| `Outraged` | Your own brief or inputs caused the failure: read the incident, correct them, do not chase it into a broader pattern the evidence does not support |

- What happened during the session comes from the accepted transition history; the envelope
  carries the declared payload; governing knowledge comes from Takt memory and the referenced
  documents, queried by topic. Never fill a gap with what seems plausible: declare it and investigate within
  authority; consult the user if continuity remains indeterminable.
- Inspect `ExtraArtifacts` only as deeply as `AdditionalContext` says they matter.
- A malformed or incomplete envelope leaves the session unsettled: resolve the gap before
  resuming the DAG around it.
- After an anomalous return, reconcile evidence and assess impact before resuming.
