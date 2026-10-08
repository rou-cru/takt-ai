# Product Master Index and Authoritative Map

**Actual implementation progress: N/A — this index defines documentary authority and boundaries, not executable behavior (0 own requirements).**

## Implementation audit

The progress percentages included in each document are a static integration audit, as of **2026-09-23**; they are not effort estimates or release statements. For PRD/ARCH/PIRS/VDS documents their own requirements are counted (not cross-references); for documents without IDs, the articles, rules, or numbered blocks that do express obligations are counted. **Complete = 1 point** only if the behavior reaches the official supported flow through a production path; **partial = 0.5** if it is connected but does not satisfy the full acceptance; **absent/not integrated = 0**, including helpers, mocks, isolated tests, declarative content without enforcement where deterministic behavior is required, and manual-validation requirements without recorded evidence. The percentage is `(complete + partial × 0.5) / own units × 100`, rounded to the nearest integer. A purely structural or vocabulary document, with no behavioral obligations, states N/A instead of fabricating a denominator.

This document is the Single Source of Truth for the structure and responsibility boundaries of the Takt AI product corpus, and defines the resolution hierarchy for normative conflicts.

## Corpus contract

This corpus defines approved product requirements, technical invariants, shared vocabulary, design contracts, release scope, and acceptance criteria. Definitions and examples belong here when they disambiguate those contracts. Implementation details and operational reports belong outside this corpus. A reported limitation does not waive a requirement.

## Reading path

Read the owning product requirements, then [the TUI surface profile](app/tui/VDS_TUI.md), which maps the shared brand tokens to the terminal and defines its patterns and acceptance criteria.

A derived C4 view of the implemented structure is in [docs/design/architecture](../docs/design/architecture/README.md); it is not normative.

## 1. Two Systems, One Product

Takt comprises two distinct systems governed by the same Constitution and built to the same engineering doctrine:

- **Application (`app/`)**: the application that installs and configures Takt, including the installation lifecycle, observable CLI contract, and TUI. Its models, FSMs, event loop, and visual grammar govern application flows.
- **Meta-harness (`meta-harness/`)**: the capabilities installed into OpenCode v2. Agent context, memory, orchestration, workflows, sensors, maintenance cycles, and harness extensions govern crew execution there.

```text
Takt application
    │ installs / updates / configures
    ▼
OpenCode v2 projection of the Takt meta-harness
    │ integrated into
    ▼
OpenCode v2
    │ runs the crew with Takt's capabilities and controls
    ▼
Orchestrated crew execution
```

### Execution Boundaries

| Aspect | Application | Installed meta-harness |
| :--- | :--- | :--- |
| Responsibility | Plan, authorize, apply, and report installation/configuration operations. | Extend OpenCode v2 and regulate crew execution toward the user's goal. |
| Events and state | Navigation, selections, forms, and installation operation results. | Dispatch, agent lifecycle, tool activity, evidence of progress, and control interventions. |
| Timing | Application execution and its local flows. | Crew execution, work-measured detection windows, and bounded progress budgets. |
| Interaction | Installation/configuration CLI and TUI. | Crew dialogue and escalation through OpenCode v2. |

Shared patterns such as events, state machines, and declarative tables do not imply a shared runtime, clock, controller, or event bus. The TUI's FSMs do not define the execution model of the installed crew. References comparing declarative patterns across systems do not transfer execution contracts between them.

### Reactive Control Within the Meta-harness

```text
Crew (plant) ──signals──▶ Observability (sensors)
     ▲                            │
     │                            ▼
Harness extensions ◀────── Control logic
   (actuators)             interprets signals
```

This loop senses and regulates crew execution; it is not a TUI navigation loop or a dashboard. A user-facing display of internal state consumes information from this system; it does not define its purpose.

### Installation and Projection Boundary

`meta-harness/platforms/PRD_PLATFORMS.md` defines the Takt-defined capability projection into OpenCode v2, its native integration mechanisms, and deployed artifact integrity. `app/installation/PRD_INSTALLATION.md` defines how installation changes are planned, authorized, applied, preserved, recovered, and reported. One defines the installed capability; the other defines the application operation that delivers it.

## 2. Document Map and Authority

```text
product/
├── INDEX.md
├── brand.md                             Shared identity and design contract
├── GLOSSARY.md                          Shared vocabulary for both systems
├── governance/                         Shared product principles
│   ├── CONSTITUTION.md                 [CONST] Authority, value, and crew
│   └── DOCTRINE.md                     [DOCTRINE] Structural norms for how work is built
├── app/                                Installation/configuration application
│   ├── PRD_SETUP.md                    [PRD] Setup modes, stack selection, and ready-to-work handoff
│   ├── PRD_RELEASE_VALIDATION.md       [PRD] Isolated integration environment and final manual release gate
│   ├── installation/
│   │   └── PRD_INSTALLATION.md          [PRD] Installation lifecycle and CLI contract
│   └── tui/
│       ├── PRD_TUI.md                   [PRD] Application model stack, event loop, and FSMs
│       └── VDS_TUI.md                   [VDS] Design system, focus, and wireframes
└── meta-harness/                        Capabilities installed into OpenCode v2
    ├── crew/
    │   └── PRD_CREW.md                  [PRD] Role classes and permission profiles
    ├── context/
    │   ├── ARCH_CONTEXT.md              [ARCH] 4R principles, context architecture, and convened-specialist input context
    │   └── ARCH_MEMORY.md               [ARCH] Cognitive memory and consolidation
    ├── orchestration/
    │   ├── PRD_ORCHESTRATION.md          [PRD] Crew direction, dispatch, supervision, and resumption after a lent interface
    │   ├── PRD_DAG.md                    [PRD] Execution history, DAG projection, and mutation discipline
    │   ├── PRD_DAG_TUI.md                [PRD] Read-only live projection of the execution DAG in the OpenCode v2 TUI
    │   ├── WORKFLOW_SELECTION.md         [PRD] Decision tree for the route adopted before each planning or implementation phase
    │   └── PIRS_ORCHESTRATION.md         [PIRS] Crew dialogue, interlocutor transitions, lent-session conduct, visibility, and escalation
    ├── harness/
    │   ├── PRD_HARNESS.md               [PRD] Guardrails, permissions, transition arbitration, and safety policies
    │   └── PRD_VFS.md                   [PRD] Virtual staging and action journal
    ├── maintenance/
    │   ├── PRD_GC.md                    [PRD] Harness-controlled workspace GC
    │   └── PRD_DREAMING.md              [PRD] Harness-controlled dreaming
    ├── observability/
    │   └── PRD_OBSERVABILITY.md         [PRD] Crew sensing and feedback control
    └── platforms/
        └── PRD_PLATFORMS.md             [PRD] Takt-defined projection into OpenCode v2
```

### Document Responsibility Matrix

Requirement identifiers (`PR-*`) are immutable references. They are never renumbered or reused: a removed requirement retires its identifier, and a new one takes the next free number. Numeric order therefore does not imply reading order or section order.

Every requirement stated in this corpus is a first-release obligation. Each document defines its complete normative scope: a requirement is either defined here or it does not exist. Scope boundaries are stated as positive obligations and prohibitions in the owning document.

This index is the authoritative place for system and document boundaries. The matrix below assigns document responsibilities within the systems defined above. Documents do not duplicate responsibility boundaries: what belongs where is resolved here. References to owning requirements provide traceability, not competing authority.

| Document | Type | Authoritative Scope | Out of Scope |
| :--- | :--- | :--- | :--- |
| `brand.md` | BRAND | Shared identity, visual tokens, verbal identity, and common design contracts. Surface profiles apply or extend these rules without redefining them. | Functional authority, implementation details, and waivers of owning product requirements. |
| `GLOSSARY.md` | GLOSSARY | Shared vocabulary of both systems: the canonical meaning of terms used across documents. | Normative obligations of any kind, requirement definitions, system behavior, document boundaries. |
| `governance/CONSTITUTION.md` | CONST | User authority, value definition, crew identity, professional judgment, and authority/value conflict resolution. | System behavior, algorithms, interaction flows, context and memory architecture, screens, copy, platform mechanisms, token budgets, implementation schemas. |
| `governance/DOCTRINE.md` | DOCTRINE | Structural norms governing how work is built, binding both the crew's work in the user's workspace and this repository: single definition, fail-fast boundaries, data contracts and their validation point, present-requirement-only capability, and reuse before creation. Its binding surface and its limits. | Naming, formatting, file layout, language idiom, and library or technology selection; what the systems must do, which every owning PRD keeps; authority and value conflicts (owned by `governance/CONSTITUTION.md`); licence to act on a deviation, which belongs to the requirements of the party correcting it. |
| `meta-harness/crew/PRD_CREW.md` | PRD | Specialties and single-role instances, role classes, their permission profiles, and their invocability and interaction class. | The concrete roster of specialists and their model assignments, which files a specialist may touch, orchestration algorithms, agent prompt content. |
| `meta-harness/context/ARCH_CONTEXT.md` | ARCH | 4R principles (Relevance, Recency, Ranking, Retrieval), canonical SSOT, minimal static context, retrieval complexity, and the input context a convened specialist receives: the requirements and client envelope, the bounded preparation preceding a switch, and isolation from the convening agent's conversation. | Authority governance, interaction flows, long-term memory lifecycle, the dialogue the convened specialist then conducts (owned by `orchestration/PIRS_ORCHESTRATION.md`), OpenCode v2 interface details, interface copy, low-level implementations. |
| `meta-harness/context/ARCH_MEMORY.md` | ARCH | Long-term cognitive memory: tiering, multi-agent authorship and attribution, entry nature and applicability, recorded evolution, ingestion protocols, preservation rules, and the consolidation lifecycle. | Base context composition, storage schemas, filesystem paths, binary tool integrations, database protocols, vector math algorithms. |
| `meta-harness/orchestration/PRD_ORCHESTRATION.md` | PRD | Crew direction, work breakdown, dispatch, supervision, handoffs, exclusive file ownership, the convening agent's resumption of direction after a lent interface — the two non-interchangeable catch-up sources, envelope validation and result interpretation, and conduct facing an anomalous return — and the orchestrator's relationship with workflow frameworks. | Harness guardrails and transition arbitration, the dialogue and escalation surface, the composition of the convened specialist's input context (owned by `context/ARCH_CONTEXT.md`), interface rendering. |
| `meta-harness/orchestration/PRD_DAG.md` | PRD | Representation of the execution DAG as a projection derived from an append-only execution history, the temporal partitioning of its work units, and the discipline governing plan commitment and strategy mutation. | Dispatch and supervision algorithms (owned by `PRD_ORCHESTRATION.md`); the telemetry envelope and internal control bus (owned by `PRD_OBSERVABILITY.md`); the Action Journal (owned by `PRD_VFS.md`); memory content and lifecycle (owned by `ARCH_MEMORY.md`). |
| `meta-harness/orchestration/PRD_DAG_TUI.md` | PRD | Read-only live projection of the execution DAG in the OpenCode v2 TUI: snapshot contract, graph rendering, refresh and stale handling. | Session navigation, node actions, graph editing, execution control. |
| `meta-harness/orchestration/WORKFLOW_SELECTION.md` | PRD | Decision tree realizing `PR-ORQ-29`..`32`: the route adopted before each planning or implementation phase. | Internal rules of each adopted route. |
| `meta-harness/orchestration/PIRS_ORCHESTRATION.md` | PIRS | Dialogue model, role classification in interaction, system visibility, the escalation protocol, the interlocutor-stack transitions (switch, handoff, abortion) with their confirmation outcomes, the returned output envelope with its result attribution, and the conduct of a lent session: its opening, the standard artifact it must produce, and its response to a missing requirement or an out-of-specialty decision. | Orchestration algorithms and the convening agent's conduct on resumption (owned by `PRD_ORCHESTRATION.md`), harness enforcement and the deterministic arbitration of those transitions (owned by `PRD_HARNESS.md`), composition of the specialist's input context (owned by `context/ARCH_CONTEXT.md`), literal text strings, graphical interfaces, visual components. |
| `meta-harness/harness/PRD_HARNESS.md` | PRD | Deterministic guardrails, per-specialist permissions, interception of unauthorized action, protection of user-managed configuration, and the arbitration of interlocutor transitions: event validity and interactive-control provenance, standard-artifact and envelope verification, idempotency, atomicity, and the durable record of results attributing cause to the convening agent. | User conversation, orchestration algorithms, file mutation mechanics, and the dialogue model and confirmation semantics of those transitions (owned by `PIRS_ORCHESTRATION.md`). |
| `meta-harness/harness/PRD_VFS.md` | PRD | Virtual file system, Git boundary, transactional staging, concurrency safety net, physical consolidation, and action journaling. | Memory data structures, binary serialization formats, operating system kernel drivers, graphical diff tools. |
| `meta-harness/maintenance/PRD_GC.md` | PRD | Harness-controlled workspace GC: scheduling, bounded collector mandate, evidence, independent verification, non-blocking reporting of out-of-scope findings, and the simplification specialty's focus and GC-collector participation. | Ordinary simplification work outside GC (`PRD_CREW.md`); dreaming (`PRD_DREAMING.md`); session stability (`PRD_OBSERVABILITY.md`); staging mechanics (`PRD_VFS.md`). Workspace analyzers are not control-bus detectors. |
| `meta-harness/maintenance/PRD_DREAMING.md` | PRD | Harness-controlled dreaming scheduling and modalities: isolated phased cycles, lossless periodic cycles, deference and empty-scope skip policy, cycle reports and diary, user-validated Deep Dream procedure, and macro consolidation. | Memory content and preservation rules (`ARCH_MEMORY.md`); workspace GC rules (`PRD_GC.md`); role permissions (`PRD_CREW.md`). Sharing maintenance does not transfer rules between processes. |
| `meta-harness/observability/PRD_OBSERVABILITY.md` | PRD | Session feedback control plane: telemetry envelope, internal control bus, progress definition, the definition of an action and its accounting for every bounded budget and enforcement window, problem-rate derivation and session stability, deterministic detectors, control action taxonomy, and telemetry data governance. | Memory tier content and consolidation, orchestration algorithms, permission profile definition, export backend selection, user-facing dashboards. |
| `meta-harness/platforms/PRD_PLATFORMS.md` | PRD | Takt-defined projection into OpenCode v2, native mechanism selection, and deployed artifact integrity. | Local OpenCode v2 policies, interaction models, installation lifecycle. |
| `app/installation/PRD_INSTALLATION.md` | PRD | Installation lifecycle (scope, plan, authorization, preservation, result, retry) and the observable CLI contract. | Internal TUI architecture, visual styles, commands, internal tools, storage formats, backup mechanisms, execution algorithms. |
| `app/PRD_SETUP.md` | PRD | Setup modes, user-facing stack composition, initial model assignment policy, and ready-to-work handoff into OpenCode v2. | Installation mutation and recovery mechanics, projection mechanisms, crew runtime behavior, TUI architecture, visual grammar, CLI syntax. |
| `app/PRD_RELEASE_VALIDATION.md` | PRD | Isolated first-use integration environment and final manual validation of the setup-to-harness user journey required for release. | Application flow behavior, crew runtime contracts, Docker build implementation, CI pipeline implementation, public image distribution policy. |
| `app/tui/PRD_TUI.md` | PRD | Navigation architecture: control model stack, per-flow FSMs and transition tables, event loop, dispatch, and side effects. | Visual styles, color palettes, typographic themes, keyboard grammar, graphics library details, business logic of specific flows. |
| `app/tui/VDS_TUI.md` | VDS | TUI surface profile: token mapping, hierarchy, focus, keyboard grammar, components, nested selection, acceptance criteria and required evidence. | Navigation architecture, state ownership, event dispatch, interface library APIs, rendering mechanisms, business logic of specific flows. Library component defaults do not override its §§2–3. |

## 3. Hierarchy and Conflict Resolution Rules

System boundaries and normative authority are separate maps: document type does not merge the two execution systems. Apply the following rules within the relevant system and domain when a discrepancy arises between documents:

```text
       [ CONSTITUTION.md ] (User Authority and Collaboration Philosophy)
               │
               ├──► [ DOCTRINE.md ] (Structural Norms for How Work Is Built)
               │
               ├──► [ ARCH_*.md ] (Context, SSOT, and Cognitive Memory)
               │
               ├──► [ PIRS_*.md ] (Interaction and User Escalation)
               │
               ├──► [ PRD_*.md ] (Deterministic Functional Requirements)
               │
               └──► [ brand.md ] (Shared Identity and Design)
                            └──► [ VDS_*.md ] (Surface Profiles)
```

1. **Authority or value conflict:** `governance/CONSTITUTION.md` prevails.
2. **Conflict over the shape of an implementation:** `governance/DOCTRINE.md` prevails, except against a requirement mandating the behavior itself, where the owning document prevails and the doctrine governs only the shape in which it is met (`STR-8`).
3. **Context representation, retention, or memory conflict:** The corresponding `ARCH` prevails — `meta-harness/context/ARCH_CONTEXT.md` for base context representation and SSOT, `meta-harness/context/ARCH_MEMORY.md` for long-term memory tiering, authorship, and consolidation.
4. **Interaction or user visibility conflict:** The corresponding `PIRS` prevails for crew interaction and visibility; application installation output remains governed by its installation PRD.
5. **Functionality or observable behavior conflict:** The `PRD` of the relevant domain prevails.
6. **Identity and common design conflict:** `brand.md` governs shared identity and design contracts; a surface profile applies or extends them.
7. **Surface presentation, layout, or visual ergonomics conflict:** The relevant `VDS` governs local presentation within the shared design contract. Neither design document changes functional obligations or grants exceptions to the owning PRD.
