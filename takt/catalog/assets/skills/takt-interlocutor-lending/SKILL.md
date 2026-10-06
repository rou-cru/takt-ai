---
name: takt-interlocutor-lending
description: "An invariant depends on decisions only the user can close, or the user asks to talk to a specialist directly: lending the chat interface to that specialist and reading what comes back."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Takt Interlocutor Lending

> **Target Audience:** Orchestrator agent only.

Governs lending the chat interface to pm, architect or product-designer, and resuming after
it returns.

## Lend or delegate

| Situation | Route |
| --- | --- |
| The invariant to generate depends on user decisions not yet taken | Lend the interface |
| The user orders it | Lend the interface |
| Intent is already approved and the lane only consumes or refines it | Delegate autonomously |

## Lending

1. Invoke the native `question` tool with a single selection naming the specialist and
   the objective. A reject, a cancel, or a freeform reply means no lending: reevaluate.
2. Only after an explicit accept, call `dispatch_switch`. Its `requirements` carry the
   Engram IDs of the invariants the specialist consumes (IDs only, never their content) and the
   settled decisions that bind it; `client` carries what is known of the user. The specialist
   starts from `objective`, `requirements` and `client` alone.
3. If the user asks to cut a lent session short, call `dispatch_abort_switch`.

## Reading the return

Read the returned envelope before anything else. `Result` attributes cause and never grades the
specialist.

| `Result` | Conduct |
| --- | --- |
| `Standard` | Consume the delivered Engram IDs as the invariant |
| `EarlyHandoff` | Take the artifact as returned, in its actual partial state |
| `Aborted` | No content to interpret and no fault to any agent: consult the user, never reconstruct |
| `TechFault` | Take it to the user for a decision, never advance on your own because it looks trivial |
| `Outraged` | Your own brief or inputs caused the failure: read the incident, correct them, do not chase it into a broader pattern the evidence does not support |

- What happened during the session comes from the envelope; what remains in effect as
  knowledge comes from memory, queried by topic. Never fill a gap with what seems plausible:
  declare it and consult the user.
- Inspect `ExtraArtifacts` only as deeply as `AdditionalContext` says they matter.
- A malformed or incomplete envelope leaves the session unsettled: resolve the gap before
  resuming the DAG around it.
- Before advancing after any anomalous return, assess its scale, explain its impact and
  propose how to handle it, and confirm the procedure with the user.
