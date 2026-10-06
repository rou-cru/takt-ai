# Interaction Requirements Specification (PIRS)

**Actual implementation progress: 68%** — 28 own requirements: 11 complete, 16 partial, 1 not integrated. The interlocutor stack is deterministically enforced end to end: `takt/dispatch/interlocutor.go`'s `Switch` denies chaining and rejects a role that does not resolve to `RoleDirectInterlocutor` (IR-1, IR-19), `Handoff` requires the caller to be the current holder (IR-16), and `Abort` accepts only `"user"`/`"harness"` origin — never the temporary holder nor the convening agent (IR-24, matching the asymmetry exactly). `takt/agents/opencode/renderer.go`'s `interlocutorRules` denies the switch/handoff/abort tools outright for verification and maintenance roles, so they structurally cannot enter the stack (IR-3, IR-17). `BuildHandoffEnvelope`/`BuildAbortEnvelope` assemble the four-field envelope; completed handoffs validate Engram IDs, and optional filesystem copy existence is reported without gating the handoff (IR-22, IR-28). `takt/setup/conflict_impact.go` and `risk_acceptance.go` make Takt-managed choices durable and re-ask only on genuinely new content or impact (IR-12), exactly as required. Confirmation itself (IR-20/21) stays prompt-level because OpenCode v2 exposes `question` only as a model-invoked tool with no programmatic hook (verified against the installed plugin/client packages, comment above `takt-vfs.ts`'s `dispatch_switch` tool); the evidentiary judgment inside `Result` (IR-23) and the lent-session conduct (IR-25..27) are carried by agent instruction (`takt-interlocutor-handoff` skill) over real mechanism, capped at partial by convention. User environment customization as a direct, low-effort operation (IR-7) has no dedicated lever in this document's own scope.

## 1. Scope

This document specifies the interaction between the user and the agent crew: who speaks to whom, how the chat interface is lent and returned, when Takt becomes visible, how a decision is escalated, and how interaction is managed when configuration conflicts arise. It is the crew interaction standard: voice stays calm and precise, confirmation is contextual, and recovery explains actual state and next steps without blame; approved choices are not re-litigated without new consequences.

## 2. Interaction Requirements (IR)

| ID | Requirement |
| --- | --- |
| IR-1 | Specialists dedicated to discovering, negotiating, or validating user intent, architecture, or design communicate directly with the user without mandatory intermediation or a permanent orchestrator proxy (reference examples: PM, Architect, Product Designer). |
| IR-2 | Technical execution specialists that receive structured and aligned work operate internally within the crew and report to the orchestrator, without opening unnecessary direct conversations with the user (reference example: code implementer). |
| IR-3 | Exclusively internal support agents are not directly invocable by the user and do not act as their direct interlocutors. Classification responds to the nature of the assigned task. |
| IR-4 | The orchestrator coordinates the crew without obstructing direct communication when the specialist's role requires user contact. |
| IR-5 | Ordinary operation is fluid and silent; the agentic infrastructure does not generate friction or unnecessary interruptions in routine flows with a clear goal. |
| IR-6 | Takt deliberately becomes visible when it contributes critical value: before a strategic decision, a justified preventive halt, or technical protection against major risks. |
| IR-7 | User environment customization is a direct, intuitive, and low-effort operation. |
| IR-8 | When confidence is high and aligned with the goal, the crew advances autonomously. When confidence is low or course ambiguity exists, clarification is requested from the user based on the goal-directed progress principle. The conditions that make escalation mandatory regardless of assessed confidence are in `IR-18`. |
| IR-9 | The active interface holder presents to the user any unforeseen situation or blocker that requires human judgment and compromises the main goal. |
| IR-10 | Every escalation to the user includes context, implications, and analyzed alternatives, avoiding offloading unprocessed problems. |
| IR-11 | The decision made by the user after an escalation bindingly governs work continuity. |
| IR-12 | Takt-managed choices, including exclusions, remain durable intention rather than conflicts to ask about again. When an update introduces relevant external-change uncertainty or a new consequence, the interface holder presents the affected scope and feasible preservation/restoration choices before proceeding, without silently replacing content. Known deterministic incompatibility is explained with a viable alternative, not offered as merely a risky valid configuration. |
| IR-13 | Escalation is a deliberate and justified interruption, never the default operating mode. It MUST NOT be used to evade autonomous resolution of minor technical problems that fall within the orchestrator's delegated scope. |
| IR-18 | Escalation is mandatory, whatever the agent's assessed confidence, when: the recorded contract or specification contradicts the observed code or environment; a bounded budget is exhausted or an escalation bound is reached (`PR-HAR-6`, `PR-HAR-19`, `PR-HAR-21`); a substantial deviation, course ambiguity, or unforeseen high-risk situation is detected (`PR-ORQ-5`); or the evidence is insufficient to establish continuity (`PR-ORQ-8`). Outside these conditions, whether confidence is high or low is the agent's own judgment, and it continues unless `IR-8` calls for clarification. |

## 3. Role Classification in Interaction

Each instance's interaction is determined by its assigned responsibility, not by its specialty (`PR-CRW-1`, `PR-CRW-3`). Simplification uses these ordinary rules for planning, implementation, and verification. Only its GC participation is internal maintenance: no user conversation, interlocutor-stack entry, or waiting for human decisions (`PR-MNT-8`, `PR-CRW-17`). Dreaming independently retains user participation through the interface holder (`MEM-DRM-6`).

| Class | Relationship with the User | Reference Case |
| --- | --- | --- |
| Direct Interlocutor | Direct communication with the user to align intent, architecture, and scope | PM / Architect / Product Designer |
| Internal Crew | Crew-oriented communication; reports to the orchestrator | Technical implementation / Code |
| Exclusively Internal | Not user-invocable; internal evaluation or validation support | Internal judges / Validators |

## 4. Interlocutor Stack

> **Note — IR-15, IR-16.** Leadership handovers and changes in agent collaboration dynamics respond to these predefined rules, never to improvised decisions at execution time.

```text
                      User
                       ▲
                       │ direct dialogue
    ┌──────────────────┴─────────────────────┐
    │ Temporary specialist          [TOP]    │
    ├────────────────────────────────────────┤
    │ Initial session agent         [BASE]   │
    │ Orchestrator or invoked Specialist     │
    └────────────────────────────────────────┘
      Lend: push       Finish or failure: pop
```

| ID | Requirement |
| --- | --- |
| IR-14 | The agent with which the user starts the session forms the base of the interlocutor stack: normally the Orchestrator, or a directly invoked Specialist. Only the top agent holds the chat interface and speaks directly to the user. |
| IR-15 | Lending the interface pushes a capable specialist onto the stack. The previous holder remains immediately underneath in standby rather than being replaced or acting as a conversational proxy. |
| IR-16 | When the temporary holder finishes or fails, its layer is removed and the immediately lower agent regains the interface and direction of the work. |
| IR-17 | Exactly one agent waits beneath the active interlocutor: the stack holds at most two layers, the active interlocutor and the one immediately below it. Internal-only agents never enter this stack; they report internally, and the interface holder decides whether user interaction is needed. |

### 4.1 Transitions of the Interlocutor Stack

```text
convening agent holds the interface
  ──switch confirmation──▶  temporary holder holds the interface

temporary holder holds the interface
  ──handoff confirmation──▶ convening agent holds it again, with the output envelope
  ──abortion, no confirmation──▶ convening agent holds it again, with a synthesized envelope

reject, cancel, or freeform response: the current holder keeps the interface
```

| ID | Requirement |
| --- | --- |
| IR-19 | Lending and returning the interface happen only through three named transitions. A **switch** pushes a capable specialist onto the stack (`IR-15`). A **handoff** is the negotiated return: the temporary holder proposes it and delivers a structured result to the agent immediately underneath (`IR-16`). An **abortion** ends the temporary holder's layer without a handoff and without negotiation, accepting no rejection and demanding no artifact. A switch MUST be proposed by the agent that currently holds the interface or requested by the user, and MUST NOT be chained from the temporary holder: when the user asks for a different specialist mid-session, the current holder prepares its exit and reports the reason as `Early Handoff`, the agent underneath regains the interface, and that agent presents a new switch. There is no direct specialist-to-specialist transition, and an exit path that removes the temporary layer without destroying the session is always available. |
| IR-20 | A switch and a handoff each take effect only after an explicit user confirmation, which is a single simple selection and never a multi-select. A switch confirmation presents at minimum the target specialist and the session objective, and states the formal reason when context demands it. A handoff confirmation presents what is being returned, and identifies any unfinished work or absent result, so the decision is not taken blindly. An accepted transition MUST NOT be followed by a second confirmation. Abortion is termination rather than a transition and is exempt from confirmation, because one of the situations it exists to rescue is the unavailability of deterministic confirmation itself; when that capability is missing, the temporary holder MUST report the problem to the user instead of managing the transition through an improvised substitute. |
| IR-21 | A confirmation resolves to exactly one of three outcomes. **Accept** performs the transition. **Reject** does not perform it and leaves the current holder, the session, and its state unchanged; a rejected handoff simply continues the specialist session. A **freeform response** does not perform the transition either, and returns the user's text to the agent that requested it for re-evaluation. Cancelling, dismissing, or making no selection MUST resolve as reject. Presented copy MAY adapt to context but MUST preserve exactly these three semantics. |
| IR-22 | The temporary holder returns its outcome as an output envelope of exactly four fields — `Result`, `AdditionalContext`, `ExtraArtifacts`, and `Memory` — never as a conversation log. The envelope is emitted when the handoff is accepted; until then the session MAY keep updating its artifacts, and after rejected handoffs the final envelope reflects the complete state of the session. All four fields are always present, and every field except `Result` MAY be explicitly empty. `AdditionalContext` is concise, typically a paragraph; it MUST explain what happened whenever `Result` is not `Standard`, MUST name which optional standard artifacts were delivered or that none were, and MUST NOT carry raw logs, whole command errors, or transcripts, which belong in `ExtraArtifacts`. `ExtraArtifacts` references MUST be usable and `AdditionalContext` MUST explain why they exist; the standard result is delivered by Engram ID, never substituted by a filesystem path. `Memory` lists every memory entry created during the session and nothing else, derived by the harness rather than declared by the specialist (`MEM-AUT-6`). The result of standard-artifact verification travels alongside the envelope and never inside it (`PR-HAR-24`). |
| IR-23 | `Result` attributes the cause of the session's outcome and never grades the temporary holder's performance. It MUST carry exactly one of five mutually exclusive values, in ascending severity: `Standard`, the standard artifact was produced according to process, which alone MAY leave `AdditionalContext` empty; `Early Handoff`, the user expedited the close, with any artifact returned in its actual partial state and none manufactured when no output coherently exists; `Aborted`, the session ended without a handoff, which the harness synthesizes because no specialist remains to declare it; `Tech Fault`, a technical failure interrupted or degraded the process, supplying evidence in `ExtraArtifacts` where possible; and `Outraged`, where the temporary holder absorbed a failure caused by what the convening agent provided or instructed — incorrect, incomplete, or misplaced information, or a premise the user debunked — even when it rescued the session. `Outraged` prevails over every other value and MUST carry the available evidence in `AdditionalContext`: the concrete reference that failed, or, when no reference can be cited, the content delivered and the reaction it provoked. Evidence MUST NOT be fabricated to satisfy the format. A declared reference that does not exist attributes cause under `Outraged`; a reference that exists but cannot be read is `Tech Fault`. `Aborted` attributes fault to no agent and MUST NOT feed reasoning about degradation. These identifiers are contract values, never approved user-facing copy. |
| IR-24 | Which agent MAY emit which transition is asymmetric. The temporary holder MAY propose a handoff and MAY close early on the user's instruction, but MUST NOT emit an abortion: abortion is termination rather than a delivery of completed work. The convening agent MUST NOT emit one either, because it holds no interactive control while the interface is lent (`IR-14`). An abortion originates only from the user, available at any moment of the temporary holder's session, or from the harness itself when the convening agent can no longer receive a result, in which case no envelope is delivered and recovery happens in a later session. |

### 4.2 Opening and Conduct of the Lent Session

| ID | Requirement |
| --- | --- |
| IR-25 | Having read what `requirements` provides and calibrated its maturity against the `client` context (`CTX-6`, `CTX-7`), the temporary holder MUST open the session with a brief interaction that demonstrates understanding of the objective, proposes exactly one starting point rooted in its specialty, and then yields the turn to the user. It MUST NOT open with an empty request for direction, nor with a barrage of problems, alternatives, or questions. The content of the opening is strict; its style is free. |
| IR-26 | When a requirement the convening agent declared as delivered is not there, the temporary holder MUST NOT invent it, assume its content, or proceed as if it had been read. It states the absence to the user and either requests the document or asks whether the user prefers to postpone; once the document arrives or continuing without it is accepted, it returns immediately to its specialty instead of dwelling on the gap. Attribution of the cause follows `IR-23`: a declared reference that does not exist and one that exists but cannot be read are different results. |
| IR-27 | The temporary holder MUST produce and update its artifacts progressively during the session rather than deferring them to its close, so an early or abrupt end still leaves the real state recorded. A user decision that falls outside its specialty MUST be neither resolved nor discarded: it preserves the decision — as a brief note or provisional artifact, recorded in memory under the shared contract (`MEM-TYP-2`, `MEM-TYP-5`, `MEM-TYP-8`) — and reports it and its location on return (`IR-22`). What the session keeps as durable knowledge and what leaves no trace are governed by `ARCH_MEMORY`; the transition itself is never memory's answer to give (`PR-HAR-27`). |
| IR-28 | A completed standard result is the document the specialty produces through its official template for the reason it was convened, recorded in Engram and returned by ID. Additional artifacts MUST NOT replace it. An early close or failure reports the actual state and any results produced; it MUST NOT fabricate an artifact when no output coherently exists or present partial work as finalized. Filesystem copies are optional and their absence never gates a handoff (`PR-HAR-24`). |
