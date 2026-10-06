# Architecture Standard: Agent Long-Term Memory (LTM)

**Actual implementation progress: 58%** — 36 own requirements: 15 complete, 12 partial, 9 not integrated. `takt/memory/memory.go` deterministically partitions the two tiers (`tierOf`, L144-149), attributes every entry to a harness-supplied author distinct from its authority (`renderEntry`, L550-569, tested by `TestRecordDecisionFromInterlocutor`), stores five relation kinds as four distinct edge verbs (`relationEdges`, L133-140), and derives applicability from those links without promoting nature (`applicability`, L172-186, tested by `TestSupersededEntryStopsGoverning`); the session graph (start/end anchors, per-entry links, `Continue`) is fully wired in `recordLinks`/`linkClosed`/`Continue` (L409-423, L721-731, L575-604). `takt/agents/opencode/assets/takt-memory.ts` (L132-135) supplies author and session from the runtime identity, never from the model, and gates personal-tier writes on a deterministic OpenCode permission confirmation (L181-184). No mutation path is ever exposed — `takt/memory/client.go` issues only GET/POST, and `takt/engram/memory.go`'s `mcpTools` (L39-44) never lists `mem_update`/`mem_capture_passive`/`mem_judge`. What remains partial or missing: tier-leakage prevention is a code-fence/directory heuristic (`validateTier`, L510-520) rather than semantic, several typology and retrieval rules (concision, lifecycle timing, forbidden future-tense content, cross-session currency, retrieval ordering) live only in the `takt-memory-contract` skill or the per-role skills, provider evolution relations are not exposed for query, and the consolidation/Dreaming cycle (§6, §8) has no specialist, skill, or code anywhere in the catalog (zero "Dream" hits in Go/TS).

## 1. Problem & Principles

Context windows are ephemeral and bound to active sessions. Without long-term memory, multi-agent crews re-explain the same preferences and constraints every session, overwrite each other's context with no attribution, and accumulate raw operational noise that degrades retrieval precision over weeks and months.

Takt treats long-term memory as **implicit, persistent context**, governed by the 4R principles (Relevance, Recency, Ranking, Retrieval) and structured around explicit agent authorship and lifecycle consolidation.

## 2. Scope & Memory Tiering

| ID | Requirement |
| --- | --- |
| MEM-SCP-1 | Memory MUST be strictly partitioned into two canonical tiers: **Global Tier** (cross-project, scoped to the user's organization — for a personal user, the organization is the user) and **Workspace Tier** (isolated to the active repository/project). |
| MEM-SCP-2 | Global Tier knowledge MUST encompass durable user preferences, developer communication habits, universal environment quirks, and cross-repository technical playbooks derived through Macro Consolidation (`MEM-DRM-7`). It MUST NOT ingest raw project-specific business logic, proprietary code snippets, or verbatim Workspace entries; only derived playbook knowledge crosses into this tier. |
| MEM-SCP-3 | Workspace Tier knowledge MUST encompass repository-level architectural invariants, technical constraints, and domain models. |
| MEM-SCP-4 | Crossing tiers MUST be explicit and one-directional at write time: project-level agents MAY read Global Tier context, but MUST NOT write workspace-specific artifacts into the Global Tier. Workspace content enters the Global Tier only as playbooks derived by Macro Consolidation (`MEM-DRM-7`); no other cycle or agent promotes Workspace entries across tiers. |

## 3. Multi-Agent Authorship & Attribution

| ID | Requirement |
| --- | --- |
| MEM-AUT-1 | Any active agent role within the crew is authorized to read and write memory entries within its assigned operational scope. |
| MEM-AUT-2 | The specialist that authored an entry is an inseparable part of that entry, together with its session and chronological position. The entry MUST distinguish its writer from the source or authority supporting its content: recording a user's decision does not make the writer its decision-maker. |
| MEM-AUT-3 | Agents MUST NOT emit anonymous or unattributed memory entries. |
| MEM-AUT-4 | Entries from different specialists MUST remain individually distinguishable, including inside consolidated groups. Grouping MUST preserve each entry's complete content, authorship, provenance, chronology, nature, and evolution links; it MUST NOT blend different stances into a collective assertion. |
| MEM-AUT-5 | Retrieval MUST preserve chronological and authorial order, so that reading entries in sequence reconstructs how understanding evolved and where each stance originated. |
| MEM-AUT-6 | The harness MUST deterministically record which entries each session and author created, without depending on any agent's declaration, so the receiving agent can relate them without searching. |
| MEM-AUT-7 | Authorship, session, and entry structure MUST be assigned by the harness from the runtime identity of the writing agent. Agents contribute only the intent and content of an entry; they MUST NOT assert their own authorship or session. |

## 4. Memory Typology & Ingestion Protocols

| ID | Requirement |
| --- | --- |
| MEM-TYP-1 | **Protocol Memories**: Routine memory entries MUST be concise, structured, and emitted during formal lifecycle events (e.g., session termination, milestone completion, or verified handoffs). |
| MEM-TYP-2 | **User-Prompted Memories**: When an agent with direct user interaction identifies high-value preference or guidance, it MUST ask the user before persisting. Persistence occurs ONLY upon deterministic positive confirmation via harness tooling. |
| MEM-TYP-3 | User-prompted memories are not subject to standard conciseness caps and MAY retain arbitrary length and detail as dictated by the user's instruction. |
| MEM-TYP-4 | No working framework grants or withholds the right to persist memory. Any specialist persists what its work produced, attributed to itself; separation between authors comes from authorship, not from a reserved memory type. |
| MEM-TYP-5 | Independently of its ingestion protocol, each entry MUST identify its nature and applicable scope. Nature distinguishes a **proposal** (not adopted), **formalized decision** (approved choice), **verified observation** (evidence-backed result), or **hypothesis** (interpretation awaiting verification), rather than assigning a numerical hardness score. Supporting approval or evidence MUST remain attributable. |
| MEM-TYP-6 | Nature and current applicability MUST remain distinct: a formalized decision may be superseded, while a proposal may remain pending. Recording or consolidating an entry MUST NOT promote a proposal or hypothesis into an approved decision or verified observation. |
| MEM-TYP-7 | Entries MUST record something that happened or what is true at the time of writing, anchored to that event: an idea explored, a fact verified, a decision taken, an artifact changed. They MUST NOT record next steps, plans, pending work, or predictions, unless the user directly orders it; such an entry is attributed to the user's order. |
| MEM-TYP-8 | Guidance on what each specialist records, when, and what it never records MUST be defined per specialist, while the rules that shape every entry MUST have a single shared definition that per-specialist guidance references and never restates. |

## 5. Operational Lifecycle & Mutation Rules

| ID | Requirement |
| --- | --- |
| MEM-OPS-1 | **Append-Only Operation**: Day-to-day work MUST preserve existing entries intact. Active agents MUST NOT delete or overwrite them, including entries they authored in the current session. |
| MEM-OPS-2 | Corrections and evolution MUST be recorded as new entries explicitly linked to the affected entries. The relation MUST distinguish an evolution of the linked entry (correcting, superseding, or supplementing it), a scoped exception, and unresolved disagreement. Neither a later timestamp nor a different specialist's assertion alone resolves a disagreement. |
| MEM-OPS-3 | Session closure does not authorize content loss. Outside user-validated Deep Dream, entries MAY be grouped without loss but MUST retain their full content. Age alone neither invalidates knowledge nor authorizes deletion. |
| MEM-OPS-4 | At session close, a single session narrative MUST be recorded. The agent holding the user-facing interface contributes only the session objective and its final state; the harness generates the chronology of the entries created during the session, in past tense, and their links by identity and relation. The narrative MUST NOT restate, merge, or promote the linked entries, and MUST NOT carry next steps or future work. No other agent contributes a session narrative. |
| MEM-OPS-5 | The harness MUST structure each session's memory as a linked graph: every entry links to a session start anchor; the session narrative of `MEM-OPS-4` is the end anchor and links to the start anchor and to every entry of the session. Continuity with a previous session exists only when the agent holding the user-facing interface declares it, and links the current start anchor to the previous session's end anchor. The declaration MUST name a closed session, MAY name one from another workspace, and becomes permanent only with the current session's first new entry; until then it can be replaced or withdrawn. Without that declaration, each session remains an isolated graph. |

## 6. Consolidation Engine (Dream Lifecycle)

To prevent semantic entropy and memory degradation over long operating horizons, Takt defines a dual-mode consolidation lifecycle (Periodic Simple Mode / Deep Dream), with macro consolidation over closed sessions. Scheduling belongs to `PRD_DREAMING.md`.

## 7. Retrieval & Context Integration

| ID | Requirement |
| --- | --- |
| MEM-RET-2 | Memory recall MUST operate Just-in-Time via lightweight indexing and targeted retrieval on demand. |
| MEM-RET-3 | In the event of a factual conflict between an older uncompacted memory entry and verified repository source code, the active codebase state prevails. |
| MEM-RET-4 | Retrieval MUST distinguish currently applicable knowledge, superseded or corrected history, and unresolved positions using scope, supporting authority or evidence, and explicit evolution links. Historical entries MUST NOT be presented as current directives, and unresolved disagreement MUST NOT be silently settled by recency. |
| MEM-RET-5 | Current applicability MUST be determined from recorded evolution even when consolidation or Deep Dream has not run. An entry may cease to govern without being deleted; relevant history remains retrievable with its original attribution. |
| MEM-RET-6 | Retrieval across the append-only memory graph MUST scale sub-linearly by evaluating boolean status filters (`active` vs `superseded`) before any targeted lookup, never scanning the full graph. |

## 8. Consolidation Requirements

| ID | Requirement |
| --- | --- |
| MEM-DRM-1 | Takt MUST support consolidation outside active task execution: user-guided review in Deep Dream, and organization in Periodic Simple Mode that is lossless except for the bounded mechanical exception of `PR-MNT-23`. Both cycles are conducted by the maintenance class and scheduled by the harness (`PR-DRM-1`, `PR-DRM-5`). |
| MEM-DRM-2 | **User-Directed Deep Mode (Deep Dream)**: A cycle explicitly requested and accompanied by the user is the only mode permitted to delete entries or reduce their retained content on judgment; the bounded mechanical removals of `PR-MNT-23` are the sole exception and never require Deep Dream. The user MUST validate what may be removed; requesting the cycle alone does not authorize every deletion. |
| MEM-DRM-3 | **Periodic Simple Mode**: When the configured interval has elapsed since the previous cycle and memory has grown, a background cycle MAY group related entries into fewer consolidated units. It MUST preserve every constituent entry's complete content and identity, including duplicates and operational debris already stored; grouping MUST NOT become lossy summarization or deletion. Objective mechanical removal, defined and bounded by `PR-MNT-23`, is the sole exception to this preservation rule and MUST NOT be extended to any case requiring judgment. |
| MEM-DRM-4 | Consolidated groups MUST allow targeted retrieval of their relevant constituent entries without requiring the whole group's history to be loaded. Reducing the number of units MUST NOT obscure provenance or evolution. |
| MEM-DRM-5 | A superseded preference MUST cease to govern current behavior while remaining available as historical content. Its removal is permitted only in Deep Dream after user validation; supersession does not itself authorize deletion. |
| MEM-DRM-6 | Consolidation never runs as part of active task execution (`PR-DRM-2`). Both cycles are conducted by a maintenance specialist (`PR-CRW-16`), which organizes entries across authors and removes them only through user validation in Deep Dream or the bounded mechanical cases of `PR-MNT-23` in Periodic Simple Mode. The agent holding the interface relays the user's request and participation, and does not conduct the cycle. |
| MEM-DRM-7 | **Macro Consolidation (Playbooks)**: On a macro interval across $N$ closed sessions or milestones, consolidation derives procedural playbooks from closed session narratives (`MEM-OPS-4`) and bus outcomes, which include the recorded execution DAG and the mutation classification of `PR-DAG-MUT-4`, so derivation can compare observed execution against the orchestrator's declared rationale rather than relying on that account alone. Playbook rules MUST cite constituent session anchors and MUST NOT alter or delete underlying episodic or declarative entries. This derivation is the only path by which Workspace-tier knowledge reaches the Global Tier (`MEM-SCP-2`, `MEM-SCP-4`). |

Provider mutation, passive capture, and conflict-judgment tools (`mem_update`, `mem_capture_passive`, `mem_judge`) are exposed only within Deep Dream cycles.
