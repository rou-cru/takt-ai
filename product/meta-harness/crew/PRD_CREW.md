# PRD: Crew Role Classes and Permission Profiles

## 1. Functional Requirements

### 1.1 Role Classes

| ID | Requirement |
| --- | --- |
| PR-CRW-1 | Every specialty in the canonical definition MUST declare the role classes it admits. Each dispatched instance MUST be created with exactly one of those classes and its permission profile. A specialty with no declared classes, or an instance without one admitted class, MUST NOT be dispatched. The instance MUST NOT change role class during its lifetime; planning and implementation require separate instances. |
| PR-CRW-2 | The role classes are: **orchestrator**, **direct interlocutor**, **planning author**, **execution**, **verification**, **quality assurance**, and **maintenance**. |
| PR-CRW-3 | The instance's assigned role class determines its non-VFS permission profile and its interaction class. VFS access is declared separately for each exact instance ID. Specialty determines focus, not an additional role class or permission profile. Files it may touch are fixed per dispatch by the contract's declared file set. |
| PR-CRW-4 | Adding a specialist to the manifest MUST NOT introduce a new permission profile. |

### 1.2 Permission Profiles

| ID | Requirement |
| --- | --- |
| PR-CRW-5 | The orchestrator owns direction, dispatch, integration, planning-lane coherence, and the user-facing completion report. It delegates available specialties' judgment. Native filesystem and shell actions remain inside the execution boundary. |
| PR-CRW-6 | **Direct interlocutor.** Speaks with the user to align intent, architecture, or scope. Direct interlocutors include PM, architect, and product designer. Configuration mutation while holding the interface. Persists each artifact it authors as a document in the repository, through native file tools, and as one complete, isolated memory entry with the exact same content; it holds no VFS binding. With no applicable invariant, it authors its artifact from its specialty judgment and the brief. During slice execution it acts only as a read-only advisor on demand, except in a lent session or for a user-originated change to observable requirements. Git mutation denied. |
| PR-CRW-7 | **Planning author.** Produces specifications, detailed technical designs, and contracts. A planning author with no applicable invariant authors its artifact from its specialty judgment and the brief: the absence of an upstream document is never a reason to decline the dispatch, and never license to improvise past the brief. Applicable invariants, received by entry ID, bind it; a new or modified invariant respects every one still in force or the conflict is reported, and modifying one starts from the ID of the version to modify and creates a new version. The orchestrator compiles the execution plan from these artifacts. Persists each artifact it authors as a document in the repository, through native file tools, and as one complete, isolated memory entry with the exact same content; it holds no VFS binding. Git mutation denied; Git read allowed. Reports to the orchestrator when dispatched internally within the crew. |
| PR-CRW-8 | Execution implements a frozen contract and delivers revision-bound evidence. Workspace mutations use VFS. Configuration and Git mutation are denied. Reports go to the orchestrator. |
| PR-CRW-9 | Verification judges an identified revision against applicable invariants. Gate mode runs the applicable checks on staged work, and a failing gate verdict carries its executable evidence; acceptance mode executes the materialized result. Both use isolated shell. Reports identify evaluated authors, checks, failures, and unverified criteria and are delivered by entry ID. |
| PR-CRW-19 | Quality-assurance instances perform mutation analysis, exploration, or non-functional analysis. They consume materialized results and use isolated shell. They deliver evidence reports by entry ID and issue no gate verdict. |
| PR-CRW-16 | Maintenance participates in harness-controlled workspace GC or memory dreaming. Workspace mutation uses VFS; memory changes go only through harness-validated dreaming operations. Git mutation and configuration mutation are denied; Git read is allowed. These permissions do not bind ordinary instances of the same specialty assigned other roles. |
| PR-CRW-10 | Every class operates inside the execution boundary defined by the harness. Role permission profiles and oracle isolation determine which views and actions are available. |

### 1.3 Interaction and Invocability

| ID | Requirement |
| --- | --- |
| PR-CRW-11 | Direct interlocutors are user-invocable and may hold the interface. Planning authors and execution specialists are user-invocable but operate internally once dispatched with aligned work. Verification and quality-assurance specialists are exclusively internal and never enter the interlocutor stack. |
| PR-CRW-17 | Maintenance instances are not user-invocable as interlocutors and never enter the interlocutor stack. GC instances do not speak with or wait for the user; requests to trigger GC pass through the interface holder. Dreaming retains user requests and participation, relayed through the interface holder. These restrictions apply to the assigned maintenance role, not to every instance of a specialty that admits it. |
| PR-CRW-12 | The independent verifier is a different instance from every implementation author it evaluates. An author may run self-checks but cannot supply its own independent acceptance. Author and verifier may share a specialty. |
| PR-CRW-20 | The PM instance authoring stories and the PM instance formalizing their business documents are distinct. The latter consumes the story artifact by ID. The harness refuses self-consumption and returns the call to delegate a fresh PM instance. |

### 1.4 Crew Shape and High-Stakes Verification

| ID | Requirement |
| --- | --- |
| PR-CRW-13 | The installed crew is always complete; in a session it may be complete, partial when a specialist is unavailable, or a single agent. Capability does not depend on its shape. Work owned by another role class is routed to it when that class is present; when it is not, the agent performs the work itself to its own standard and declares which specialty's judgment it absorbed and on what basis. Declining delivery because a specialist is absent is a failure, and so is absorbing a specialty that was present. The complete crew is not a precondition for delivering. |
| PR-CRW-14 | Reviewers assess an identified diff in isolated contexts without the implementer's accumulated conversation. Semantic criticism is advisory; a functional defect blocks integration only with an executable reproduction. An invariant conflict returns to the authoritative owner. A blind pair uses two independent verification instances under identical briefs; neither receives the other's identity or findings. Both deliver their own evidence reports unmerged; agreement alone is not computational acceptance. |
| PR-CRW-15 | A blind pair is exceptional and requires concrete evidence of damage of unknown extent or unresolved divergence requiring greater assurance. Concurrency width never activates it. Both reports close one review round; correction and any repeated review consume the existing slice budget. The harness selects the reviewers; the orchestrator cannot replace one to obtain a preferred answer. |

### 1.5 VFS Grants

| ID | Requirement |
| --- | --- |
| PR-CRW-18 | Every deployed agent instance MUST declare its operation-level VFS capabilities by exact instance ID. A missing declaration MUST fail catalog validation; there is no global default and no inheritance from a shared agent definition or role class. An explicit empty list denies all VFS operations. Instances sharing one agent definition, including instances assigned different roles, MUST each declare their own list. Orchestration claim list, assign, and release are separate capabilities and MUST be explicitly assigned to the orchestrator instance. |
