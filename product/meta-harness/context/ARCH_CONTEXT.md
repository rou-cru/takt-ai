# Context Architecture: Principles and SSOT

**Actual implementation progress: 65%** — 24 own requirements: 10 complete, 11 partial, 3 not integrated. `takt/catalog/packages.go`'s `AgentDefinition.ContextPaths` composes each agent's static context deterministically from one canonical source — only baseline plus persona/soul when the role holds the interface, plus operations — and `takt/agents/opencode/layout.go`'s `MemoryClause` names skill paths without injecting their content, matching CTX-1/RET-1's minimal-load claim. `takt/agents/opencode/renderer.go`'s `agentPermissionRules` deterministically denies VFS, git-mutation, edit, and interlocutor tools by role, and `takt/agents/opencode/assets/takt-vfs.ts`'s `dispatch_switch` tool genuinely isolates a convened specialist in a brand-new native OpenCode session seeded only with `objective`/`requirements`/`client`, never the orchestrator's own history — CTX-8 is fully deterministic, not prompt judgment. `takt/memory/memory.go`'s `validateAuthority` deterministically rejects an unevidenced observation/decision and an unconfirmed personal-scope entry (REC-2/4). What remains prompt-only judgment: CTX-5/6/7's paragraph cap, explicit-emptiness declaration, and tool-call ceiling (only stated in `takt/catalog/assets/agents/takt/OPERATIONS.md:2-11`); RNK-1's conflict rules (stated per-scenario in role/skill files, e.g. `takt/catalog/assets/agents/takt-verify/OPERATIONS.md:1`, never enforced); and RET-4's load-failure disclosure (`takt/catalog/assets/shared/BASELINE.md:29`). No mechanism prunes ephemeral operational noise (REC-1), no adoption-justification process exists for retrieval mechanisms like RAG (RET-5), and no check validates that a context reduction preserved fluency or precision (CTX-2). `takt/skills/skills.go`'s `LoadSkills`/`BuildSkillPlan` deploy every catalog skill globally regardless of an agent's declared `skills:` list, so SSOT-4's per-agent skill membership is validated at catalog load but never scoped at runtime.

This document is Takt's context architecture standard. A design, projection, or harness configuration is correct if it satisfies these requirements and incorrect if it violates them.

## 1. Scope

Takt structures its context architecture on four fundamental principles and a single source of truth (SSOT). Differences between models or agents are intentional variations produced from the canonical definition, never independent sources of truth.

## 2. Relevance

Each agent receives exclusively the rules, capabilities, and information pertinent to its active task.

| ID | Requirement |
| --- | --- |
| REL-1 | An agent's persistent context does not include rules, tools, or information outside its active task. |
| REL-2 | Every operational constraint is expressed as clear semantic guidance. The agent must not discover its limits by blindly colliding against the harness. Deterministic backup complements the semantic instruction. |

## 3. Recency

Context explicitly distinguishes between ephemeral information, durable knowledge, and session-dependent intermediate state.

| ID | Requirement |
| --- | --- |
| REC-1 | Ephemeral operational noise is removed from active context once its function is fulfilled (e.g., circumstantial confirmations or already-resolved clarifications). |
| REC-2 | High-value technical knowledge, explicit user preferences, and empirical evidence about encountered constraints are durably retained to avoid rework. |
| REC-3 | Intermediate information is evaluated based on the agent's role and workflow evolution, relying on user validation when persistence is necessary. |
| REC-4 | Takt does not impose universal, rigid retention policies when relevance legitimately depends on dynamic context. |
| REC-5 | Context compaction is an inherent capability that Takt leverages and progressively optimizes, prioritizing OpenCode v2's efficient native mechanisms and incorporating specialized logic only when product complexity demands it. |

## 4. Ranking

The internal instruction architecture produces consistent, Constitution-aligned behavior, eliminating operational ambiguities when faced with concurrent or conflicting instructions.

| ID | Requirement |
| --- | --- |
| RNK-1 | When faced with a real, apparent, or foreseeably misinterpreted conflict, the instruction architecture possesses a deterministic and coherent resolution rule. An agent must never be left in a state of decisional ambiguity. |
| RNK-2 | Rigid section hierarchies or arbitrary weights are not prescribed; hierarchy effectiveness is validated by the absence of ambiguity in practical execution. |

## 5. Retrieval

Specialized content is loaded dynamically when the task requires it, avoiding massive universal injections into the base context.

| ID | Requirement |
| --- | --- |
| RET-1 | All capabilities are discoverable via lightweight descriptors in the base context; their full specification is retrieved only when their use is activated. |
| RET-2 | The context architecture allows modulating capability visibility based on the agent's role, task state, or environment policies. |
| RET-4 | Skills are loaded on demand from their canonical path and retain their provenance; a load failure is explicitly reported and not ignored. |
| RET-5 | Adopting complex retrieval mechanisms (such as RAG or vector indexing) requires explicit quantitative and qualitative justification of return versus maintenance cost. Technical inertia is not a justification. |

## 6. Single Source of Truth (SSOT)

A **projected artifact** is an OpenCode v2-native derivation of the canonical definition. Projection preserves the Takt capability contract while permitting OpenCode v2's native representation and interaction mechanisms. Consistency does not promise identical outputs or erase useful model differences; exclusions and evidence remain explicit.

| ID | Requirement |
| --- | --- |
| SSOT-1 | Agent identities, base instructions, permissions, model configurations, skills, workflows, and harness policies originate from a single canonical source. |
| SSOT-2 | Any divergence between models or agent instances is an intentional projection generated from the canonical source, without independent maintenance branches. |
| SSOT-4 | The canonical definition of each subagent includes identity, permissions, and skills; every projection preserves those elements. |

## 7. Minimal Static Context

| ID | Requirement |
| --- | --- |
| CTX-1 | Takt reduces the permanent static context load to the minimum necessary to preserve identity, judgment, capabilities, and result quality. |
| CTX-2 | Context optimization must manifest as tangible improvements in fluency and precision during use, not merely in isolated token-count metrics. Suggested token limits are indicative reference values, not rigid constraints. |
| CTX-3 | Context reduction never justifies a compromise in the agent's reasoning quality, coherence, or operational safety. |
| CTX-4 | Static context is distributed according to what each role needs in order to hold its position in the crew, never according to whether a specialist happens to carry an identity. An agent that only speaks to other agents needs to know what depends on its output as much as an interlocutor needs its voice; economising that away is a reduction `CTX-3` forbids. |

## 8. Input Context of a Convened Specialist

When the interface is lent to a specialist through a switch (`IR-19`), the context it starts from is composed, not inherited.

```text
requirements: [<empty>, <paths to finalized documents and/or memory entry identifiers>]
client:       <human context of the user, always present>
```

| ID | Requirement |
| :--- | :--- |
| CTX-5 | A convened specialist receives strictly four things: its role, its skills, the context envelope of `CTX-6` and `CTX-7`, and a presentation prompt of at most one paragraph stating what will be worked on. The prompt is a professional introduction — who the specialist is, what the user needs, and why it is being convened — and is not a substitute for the envelope. Role and skills originate from the specialist's canonical definition and its projection (`SSOT-1`, `SSOT-4`); the convening agent does not redefine them at convocation. |
| CTX-6 | `requirements` MUST prioritize paths to formal, finalized documents, and MAY combine them with identifiers of memory entries when that additional material carries real value. It MUST evaluate to explicit emptiness when no useful information exists; emptiness is a declared fact, never filled with plausible references. Before the switch the convening agent MAY perform brief preparation to gather high-value context, bounded by a configurable ceiling on tool calls. It MUST NOT delegate prior exploration nor persist in speculative searches, and on reaching the ceiling without useful documentation it MUST pass an empty `requirements` and proceed. |
| CTX-7 | `client` is human context that calibrates the session — the user's knowledge level, intent, preferences, concerns, and perceived value — and is always present. It MUST NOT carry technical requirements or artifacts, and it carries no normative authority: a user's wish or prohibition stated there guides the specialist's exploration but does not constitute a technical requirement. The convening agent MUST state explicitly that the user's level or intent is unknown rather than infer either of them. |
| CTX-8 | The convened specialist MUST NOT receive the convening agent's conversation history. That isolation is the point: noise, discarded proposals, and earlier hesitations would bias the specialist's own analysis, and it is the same exclusion `REL-1` applies to any information outside the active task. What the specialist needs from the preceding conversation reaches it as `requirements` and `client`, or not at all. |
