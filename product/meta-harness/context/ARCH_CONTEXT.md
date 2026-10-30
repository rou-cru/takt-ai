# Context Architecture: Principles and SSOT

## 1. Relevance

Each agent receives exclusively the rules, capabilities, and information pertinent to its active task.

| ID | Requirement |
| --- | --- |
| REL-1 | An agent's persistent context does not include rules, tools, or information outside its active task. |
| REL-2 | Every operational constraint reaches the agent as clear semantic guidance before it acts. Deterministic backup complements that guidance. |

## 2. Recency

Context explicitly distinguishes between ephemeral information, durable knowledge, and session-dependent intermediate state.

| ID | Requirement |
| --- | --- |
| REC-1 | Ephemeral operational noise is removed from active context once its function is fulfilled (e.g., circumstantial confirmations or already-resolved clarifications). |
| REC-2 | High-value technical knowledge, explicit user preferences, and empirical evidence about encountered constraints are durably retained. |
| REC-3 | Intermediate information is evaluated based on the agent's role and workflow evolution. |
| REC-4 | Takt does not impose universal, rigid retention policies when relevance legitimately depends on dynamic context. |
| REC-5 | Compaction is a recorded transition. Scheduled compaction waits for a completed-slice boundary with no unresolved incident requiring immediate context. Compaction the runtime forces MAY interrupt a slice; it is recorded the same way and follows the same procedure. Either proceeds in order: mask obsolete observations, hand off structured artifacts by entry ID, and summarize synthetically only what remains. It preserves the goal, applicable invariants, evidence references, current work position, and remaining budgets, and does not increase repeated calls or denials. |

## 3. Ranking

The instruction architecture resolves concurrent or conflicting instructions without ambiguity.

| ID | Requirement |
| --- | --- |
| RNK-1 | When faced with a real, apparent, or foreseeably misinterpreted conflict, the instruction architecture possesses a deterministic and coherent resolution rule. An agent is never left in a state of decisional ambiguity. |
| RNK-2 | Rigid section hierarchies or arbitrary weights are not prescribed. |

## 4. Retrieval

Specialized content is loaded dynamically when the task requires it.

| ID | Requirement |
| --- | --- |
| RET-1 | All capabilities are discoverable via lightweight descriptors in the base context; their full specification is retrieved only when their use is activated. |
| RET-2 | The context architecture allows modulating capability visibility based on the agent's role, task state, or environment policies. |
| RET-4 | The harness injects each activated skill deterministically once per work unit and canonical revision. Loaded content is reused until it changes or compaction removes it, and load failures are reported explicitly. The static prompt prefix stays invariant across turns. Agents use the exact deployed tool identifiers; capability descriptors do not substitute for callable tools. |

## 5. Single Source of Truth (SSOT)

A **projected artifact** is an OpenCode v2-native derivation of the canonical definition. Projection preserves the Takt capability contract while permitting OpenCode v2's native representation and interaction mechanisms; exclusions and evidence remain explicit.

| ID | Requirement |
| --- | --- |
| SSOT-1 | Agent identities, base instructions, permissions, model configurations, skills, workflows, and harness policies originate from a single canonical source. |
| SSOT-2 | Any divergence between models or agent instances is an intentional projection generated from the canonical source, without independent maintenance branches. |
| SSOT-4 | The canonical definition of each specialist includes identity, permissions, and skills; every projection preserves those elements. |

## 6. Minimal Static Context

| ID | Requirement |
| --- | --- |
| CTX-1 | Takt reduces the permanent static context load to the minimum necessary to preserve identity, judgment, capabilities, and result quality. |
| CTX-2 | Context optimization is measured by fluency and precision during use, not by token counts alone. |
| CTX-3 | Context reduction never justifies a compromise in the agent's reasoning quality, coherence, or operational safety. |
| CTX-4 | Static context is distributed according to what each role needs to hold its position in the crew, never according to whether a specialist happens to carry an identity. An agent that only speaks to other agents receives what depends on its output, as an interlocutor receives its voice. |

## 7. Input Context of a Temporary Holder

When the interface is lent to a specialist through a switch, the context it starts from is composed, not inherited.

| ID | Requirement |
| :--- | :--- |
| CTX-5 | A temporary holder receives strictly four things: its role, its skills, the context envelope of `CTX-6` and `CTX-7`, and a presentation prompt of at most one paragraph stating what will be worked on. The prompt is a professional introduction — who the specialist is, what the user needs, and why it is being convened — and is not a substitute for the envelope. The convening agent does not redefine role and skills at convocation. The goal card of `CTX-9` is placed by the harness, is not part of the convocation, and still ends each of the temporary holder's turns. |
| CTX-6 | `requirements` MUST prioritize paths to formal, finalized documents, and MAY combine them with identifiers of memory entries when that additional material carries real value. It MUST evaluate to explicit emptiness when no useful information exists; emptiness is a declared fact, never filled with plausible references. Before the switch the convening agent MAY perform brief preparation to gather high-value context, bounded by a configurable ceiling on tool calls. It MUST NOT delegate prior exploration nor persist in speculative searches, and on reaching the ceiling without useful documentation it MUST pass an empty `requirements` and proceed. |
| CTX-7 | `client` is human context that calibrates the session — the user's knowledge level, intent, preferences, concerns, and perceived value — and is always present. It MUST NOT carry technical requirements or artifacts, and it carries no normative authority: a user's wish or prohibition stated there guides the specialist's exploration but does not constitute a technical requirement. The convening agent MUST state explicitly that the user's level or intent is unknown rather than infer either of them. |
| CTX-8 | The temporary holder MUST NOT receive the convening agent's conversation history. What the specialist needs from the preceding conversation reaches it as `requirements` and `client`, or not at all. |

## 8. Operational Context

| ID | Requirement |
| --- | --- |
| CTX-9 | The harness places one bounded goal card at the end of each turn's input context: active objective, current slice, feature checklist, and remaining budgets obtained from their owners. Its field structure and position stay fixed. The card is orientation, not an authority that admits or blocks actions. Tool descriptions and memory reminders are not repeated each turn. |
| CTX-10 | Specialist inputs identify the required artifact and section, author, and invariant version for each evaluated scope. Multi-author review binds each author separately. Artifacts pass by entry ID without orchestrator paraphrase. Only relevant sections are retrieved, retaining all applicable conditions and access to the complete source. Work breakdowns are partitioned by slice; each unit receives its own contract and prerequisites, not the entire objective backlog. |
| CTX-11 | Context handoff preserves explicit state and evidence pointers rather than full transcripts or accumulated command outputs. Runtime transitions refresh only changed context. Reading an input does not require loading persistence procedures or creating a memory entry. |
