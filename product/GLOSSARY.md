# Product Glossary

Shared vocabulary for the Takt product corpus. Terms defined here carry their defined meaning in every document. This glossary creates no obligations.

“Takt AI” is the formal name; “Takt” is the contextual short form. Technical identifiers retain their actual spelling.

## 1. Systems and Destinations

| Term | Definition |
| :--- | :--- |
| **meta harness** | Takt’s layer of coordinated specialists and supporting capabilities over OpenCode v2 and user-chosen inference; not a replacement harness or a promise of identical model results. |
| **model** | The inference assigned to an agent, distinct from its provider and from the base harness. |
| **base harness** | OpenCode v2, which Takt extends with its canonical agents and supporting capabilities. |
| **platform** | The OpenCode v2 integration destination of the Takt application. |
| **target** | The concrete destination of an operation within the OpenCode v2 installation. |
| **scope** | The OpenCode v2 target together with the set of managed assets applicable to it. |
| **workspace** | The project tree the crew works on: everything under the project root of the active session, independent of whether it is a Git repository. |

## 2. What Is Installed

| Term | Definition |
| :--- | :--- |
| **capability** | An installable unit with a user-recognizable purpose, such as a specialist or workflow. |
| **managed asset** | A concrete file or configuration entry that Takt installs and controls within a scope. A capability is realized as one or more managed assets. |
| **user-managed configuration** | OpenCode v2 configuration owned by the user, outside the assets installed and managed by Takt. |
| **Takt default stack** | The complete prepared capability selection offered by default setup. |
| **mandatory core** | The set of capabilities that custom setup cannot omit. A strict subset of the Takt default stack. |
| **hardware-conditional capability** | An optional capability that is installed only when the host meets its declared hardware requirement. |

## 3. Installation State

| Term | Definition |
| :--- | :--- |
| **Takt-managed choice** | An explicit configuration decision made through Takt, including model assignments and exclusions; part of the expected configuration, not an external conflict. |
| **Takt configuration** | The set of installation and behavior decisions managed by Takt, including model assignments and exclusions. |
| **external modification** | A change made outside Takt’s configuration mechanisms; its origin does not imply fault or incompatibility. |
| **canonical version** | The reference definition distributed by Takt for an identified version. |
| **restore / sync** | Recover canonical content in an explicit scope; not necessarily undoing the last operation or resetting the full installation. `sync` is conceptual vocabulary here, not a declaration of CLI syntax. |
| **drift** | Divergence between an installed managed asset and the definition expected for its installed version and the user's selected configuration. Drift is a factual observation: it carries no judgment about whether the change was intentional, valid, or wanted. |
| **version drift** | A difference between the installed Takt version and the running build, distinct from divergence in an individual asset. |
| **drift correction** | Restoring affected managed assets to the definition expected for their installed version and the user's selected configuration. Distinct from upgrade. |
| **upgrade** | Adopting a newer version of installed capabilities. Distinct from drift correction. |
| **operation completion** | Whether the approved changes were applied. |
| **functional readiness** | Whether the resulting installation can be expected to work as designed. |

## 4. Crew Execution

| Term | Definition |
| :--- | :--- |
| **specialist** | A crew agent with a canonical specialty; its active-session instances each carry one assigned role. Specialty and instance are distinct. |
| **specialty** | The canonical focus and expertise of a specialist, with a declared set of admitted roles; not itself a permission profile. |
| **instance** | A concrete agent created for an assignment with one admitted role and its corresponding permissions; distinct from the reusable specialty. |
| **role class** | An instance's assigned responsibility, fixing its permissions and interaction class. |
| **Takt operation** | A mutation the product performs by its own deterministic design: installation, update, projection, and harness enforcement. Attributed to Takt. |
| **memory nature** | The kind of assertion a memory entry records, distinct from whether it currently applies. |
| **entry ID** | The persistent identifier of a memory entry, by which artifacts, evidence, and results are passed and retrieved. |
| **Global Tier / Workspace Tier** | The two memory partitions: Global holds cross-project user preferences and environment quirks; Workspace holds the active repository's invariants, constraints, and domain knowledge. |
| **session anchor** | A harness-created record delimiting a session's memory graph at its start or end. |
| **session narrative** | The end-of-session account of its objective, final state, and linked memory chronology. |
| **maintenance cycle** | A bounded harness-controlled run outside active task execution, not an orchestrator work unit: GC over the workspace or dreaming over memory. |
| **collector** | The simplification instance participating in a GC cycle, realigning the workspace to declared norms with bounded, behavior-preserving deltas. Not the name of every ordinary simplification assignment. |
| **structural doctrine** | The engineering standard for the structure of work, distinct from functional requirements or authority to change it. |
| **mandate class** | A family of work in a GC collector's mandate. |
| **workspace analyzer** | The static analysis layer over the code the crew builds, from which the collector's findings originate. Distinct from the detectors of the control bus, which observe crew execution rather than the code it produces. |
| **masked dead code** | Code whose only callers are tests: the test establishes no use, it hides the code's death from analysis. |
| **dreaming** | The long-term memory consolidation lifecycle: Periodic Simple Mode, Deep Dream, and macro consolidation. Distinct from workspace GC. |
| **Periodic Simple Mode** | The lossless background dreaming cycle that runs without the user. |
| **Deep Dream** | The dreaming cycle run with the user present, applying only user-validated changes. |
| **macro consolidation** | The dreaming cycle over closed sessions that derives Global Tier knowledge from Workspace Tier entries. |
| **dreaming operation** | A typed change the dreaming specialist proposes and the harness validates before applying: group, link, relocate, propose removal, or attach a derived label. Distinct from VFS consolidation. |
| **candidate set** | The reproducible set of groups, pairs, relinks, and signals the harness prepares for the dreaming specialist to judge. |
| **action** | The work-counting unit of the control plane, distinct from elapsed time. |
| **problem rate** | The incidence of deterministic problem signals relative to recorded work, distinct from elapsed time or an agent assessment. |
| **abortion** | The interface transition that ends a temporary holder's layer without handoff, negotiation, or artifact. |
| **acceptance entity** | The persisted identity of a story or slice acceptance scope to which attempts, evidence, and budgets bind. |
| **Action Journal** | The immutable, append-only record of every operation performed against the VFS. |
| **claim** | The VFS record granting one instance write ownership of a file scope. |
| **consolidation** | The deterministic flush of staged VFS state onto the physical filesystem, distinct from committing. |
| **control bus** | The local, ordered event stream of the control plane, carrying normalized OpenCode v2 and Takt events and result events. |
| **control plane** | The Takt-owned layer that senses crew execution through the control bus, evaluates detectors, and applies control actions. |
| **contest** | The orchestrator's request to verify an identified harness terminal failure against its preexisting applicable invariants. |
| **convening agent** | The agent that holds the interface and proposes a switch to lend it to a specialist. |
| **DAG** | The directed acyclic graph of work units whose route the orchestrator decides, shown as a projection of the execution history. |
| **DAG projection** | The graph state derived deterministically from the execution history; never a mutable model of its own. Distinct from the platform projection of Takt into OpenCode v2. |
| **envelope** | The structured outcome a temporary holder returns to the convening agent on handoff. |
| **execution boundary** | The isolation perimeter around crew execution: authorized workspace, scratch paths, network destinations, and external effects. |
| **execution history** | The append-only record of observed execution, written and sequenced only by the harness. |
| **gate** | A point at which work proceeds only if a condition holds: passing check evidence or a verification verdict before consolidation or acceptance, or user approval before an action. |
| **goal card** | The fixed-structure orientation summary of objective, current slice, feature checklist, and remaining budgets. |
| **handoff** | The negotiated return of the interface: the temporary holder proposes it and delivers its envelope to the convening agent. |
| **harness** | The deterministic layer Takt provides around the crew: admission, enforcement, execution history, and the VFS. Distinct from the base harness. |
| **interface holder** | The agent at the top of the interlocutor stack, the only one that holds the chat interface and speaks to the user. |
| **interlocutor** | An agent that speaks directly with the user. |
| **interlocutor stack** | The ordered layers of interlocutors: the interface holder on top and, while the interface is lent, the convening agent beneath it. |
| **invariant** | An authoritative reference against which work is judged, rather than an agent's claim about its own work. |
| **Kev** | The optional local semantic sensor the control plane consults on a policy alert. |
| **user story** | A functional outcome stated from the user's perspective, with its purpose, scope, priority, and acceptance criteria. |
| **business document** | A formal invariant, such as a PRD or brief, that details the user stories within its scope. |
| **participation mode** | The user's chosen involvement in a session: participative, taking part in slice decisions, or autonomous, delegating them within the crew's authority. |
| **planning lane** | A planning assignment that produces an invariant ahead of implementation, such as stories, a business document, an architecture, a specification, or a work breakdown. |
| **vertical slice** | Everything one user story, or one split of it, needs across the layers of the system, ending in a functional result that can be demonstrated. Distinct from a layer, which is one horizontal part of the system across many stories. **Slice** is its short form. |
| **sub-objective** | A bounded deliverable within a vertical slice, assigned as one work unit; it may span layers. |
| **slice control point** | The evaluation of an accepted slice and selection of subsequent work. |
| **SMC** | Sliding-mode control: the advanced control the control plane applies to the orchestrator over established result sensors. |
| **stagnation** | Recorded work that yields no new acceptance evidence and no feature-checklist change. |
| **milestone** | A meaningful accepted functional result toward the user's goal. |
| **orchestrator** | The crew agent that dispatches work units, decides the DAG's route, and keeps every agent's delivery aligned to the goal. |
| **projection** | The delivery of Takt's agents and capabilities into OpenCode v2 through its native configuration, plugins, hooks, and permissions. Distinct from the DAG projection. |
| **result event** | A typed record of the outcome of a check, verification, or end-to-end run, carried on the control bus and in the execution history. |
| **switch** | The interface transition that lends the interface to a capable specialist by pushing it onto the interlocutor stack. |
| **temporary holder** | The specialist that holds the interface while it is lent, until handoff, early close, failure, or abortion. |
| **verdict** | The judgment a verification instance issues on a staged delta, bound to the evaluated revision and applicable invariants. |
| **VFS** | The virtual file system that stages agents' file mutations, enforces exclusive ownership, and consolidates onto disk. |
| **work unit** | The unit of work the orchestrator dispatches to an agent under a contract, and the unit the DAG and the execution history record. |

## 5. Specialties and Routes

| Term | Definition |
| :--- | :--- |
| **PM** | The specialty that defines product intent, value, scope, priorities, user stories, and business documents. |
| **product designer** | The specialty that defines coherent, accessible, feasible user and developer experiences. |
| **architect** | The specialty that defines buildable architecture, boundaries, data flow, and interface contracts, and reviews cross-artifact coherence. |
| **analyst** | The specialty that reduces uncertainty through codebase research, evidence, and compared alternatives, including isolating unresolved causes. |
| **spec** | The specialty that translates intent into observable behavior and acceptance scenarios. |
| **TPM** | The specialty that decomposes requirements into scoped, traceable work with justified dependencies. |
| **QA lead** | The specialty that plans risk-based testing: test policy, test plan, environment requirements, traceability, and the test completion report. |
| **test author** | The specialty that writes acceptance and contract tests blind, from specifications and frozen contracts. |
| **dev** | The specialty that implements requested changes and demonstrates them against available contracts. |
| **fix** | The specialty that repairs one localized defect, or one identical mechanical pattern, with a regression check. |
| **simplification** | The specialty focused on cleanup, removal of duplication, and reduction of structural complexity, used in ordinary crew roles or as the collector in GC. |
| **verifier** | The specialty that independently judges invariants, staged gates, and executable end-to-end acceptance, as the isolated tester of each slice. |
| **judge** | A verifier instance convened for independent adversarial review when ordinary verification is insufficient. |
| **mutation analyst** | The quality-assurance specialty that judges whether acceptance tests detect targeted faults. |
| **explorer** | The quality-assurance specialty that explores the delivered product as its user would, in chartered sessions. |
| **quality analyst** | The quality-assurance specialty that analyzes named non-functional quality characteristics with evidence. |
| **dreaming specialist** | The specialty that judges the candidate set and proposes dreaming operations. |
| **SDD** | Specification-driven development: delivery that plans to the next executable slice and implements it against its invariants. |
| **route** | The workflow the orchestrator adopts before a planning or implementation phase: bounded planning, bounded workflow, invariant planning, SDD workflow, or QA workflow. |
| **SDD workflow** | The route that delivers an objective slice by slice under SDD. |
| **invariant planning** | The route that produces the invariants the next executable slice needs. |
| **bounded planning / bounded workflow** | The direct-work routes: planning without specialists, or implementation without delegation. |

## 6. Quality and Testing

| Term | Definition |
| :--- | :--- |
| **blind test authorship** | Writing acceptance and contract tests (Q2) from the specification, frozen contracts, and traceability matrix, never from the implementation. |
| **Definition of Done (DoD)** | The acceptance conditions for an objective to count as complete. |
| **environment requirements** | The services, data, configuration, and platforms a test plan needs beyond what the repository already runs. |
| **mutant** | A deliberate small fault introduced into a copy of the code to check whether the tests detect it. A **surviving mutant** is one no test detects; an **equivalent mutant** behaves identically to the original and is discarded. |
| **QA planned** | An objective whose acceptance uses the QA workflow rather than individual quality specialists. |
| **QA workflow** | The orchestrator's state machine for an objective's testing: a route for assessing an existing deliverable and the acceptance tail of a route with QA planned. |
| **quality assurance (role class)** | The class of the mutation analyst, explorer, and quality analyst, which critique the materialized product without mutating it. |
| **organizational test strategy** | Takt AI's common testing standard across workspaces, distinct from a workspace's test policy or an objective's test plan. |
| **requirements traceability matrix (RTM)** | The structured relation between requirements, test conditions, owning work units, and evidence. |
| **test completion report** | The record of testing performed, deviations, completion evaluation, residual risk, reusable assets, and lessons; distinct from the orchestrator's user-facing completion report. |
| **test oracle** | The reference used to decide whether a test's outcome is correct. |
| **test plan** | The per-objective statement of chosen quadrants, test levels, entry and exit criteria, must-not-fail risks, and effective Definition of Done. |
| **test policy** | A workspace's adaptation of the organizational test strategy, including its default Definition of Done. |
| **testing quadrant** | One of four test classes, Q1 to Q4, by technology or business orientation and by support or critique purpose. |
