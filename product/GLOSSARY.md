# Product Glossary

**Actual implementation progress: N/A (this glossary is explicitly non-normative; it defines no functional requirements of its own).**

Shared vocabulary for the Takt product corpus. Terms defined here carry their
defined meaning in every document; documents do not redefine them locally.

This glossary fixes vocabulary. It creates no obligations: normative
requirements live in the documents listed in `INDEX.md`.

“Takt AI” is the formal name; “Takt” is the contextual short form. Technical identifiers retain their actual spelling.

## 1. Systems and Destinations

| Term | Definition |
| :--- | :--- |
| **meta harness** | Takt’s layer of coordinated specialists and supporting capabilities over OpenCode v2 and user-chosen inference; not a replacement harness or a promise of identical model results. |
| **model** | The inference assigned to an agent, distinct from its provider and from the base harness. |
| **base harness** | OpenCode v2, which Takt extends with its canonical agents and supporting capabilities. |
| **platform** | The OpenCode v2 integration destination of the Takt application. |
| **target** | The concrete destination of an operation within the OpenCode v2 installation. The official target is the user's global installation. |
| **scope** | The OpenCode v2 target together with the set of managed assets applicable to it. |
| **workspace** | The project tree the crew works on: everything under the project root of the active session. The VFS stages and consolidates mutations within it; effects outside it are not staged. Independent of whether it is a Git repository. |

## 2. What Is Installed

| Term | Definition |
| :--- | :--- |
| **capability** | An installable unit with a purpose the user can recognize and select, such as a specialist or a workflow skill. Capabilities are what setup presents and the user chooses; Engram memory is part of the mandatory core, not a selectable capability. |
| **managed asset** | A concrete file or configuration entry that Takt installs and controls within a scope. A capability is realized as one or more managed assets. |
| **user-managed configuration** | Configuration of OpenCode v2 owned by the user that Takt never installed. It lies outside Takt's managed assets and is never Takt's to replace. |
| **Takt default stack** | The complete, strongly opinionated set of capabilities that the default setup mode installs: the mandatory core plus every curated optional capability. |
| **mandatory core** | The set of capabilities that custom setup cannot omit. A strict subset of the Takt default stack (`PR-SET-12`). |
| **hardware-conditional capability** | An optional capability that is installed only when the host meets its declared hardware requirement (`PR-SET-34`). |

## 3. Installation State

| Term | Definition |
| :--- | :--- |
| **Takt-managed choice** | An explicit configuration decision made through Takt, including model assignments and exclusions; part of the expected configuration, not an external conflict. |
| **Takt configuration** | The set of installation and behavior decisions managed by Takt, including model assignments and exclusions. |
| **external modification** | A change made outside Takt’s configuration mechanisms; its origin does not imply fault or incompatibility. |
| **canonical version** | The reference definition distributed by Takt for an identified version. |
| **restore / sync** | Recover canonical content in an explicit scope; not necessarily undoing the last operation or resetting the full installation. `sync` is conceptual vocabulary here, not a declaration of CLI syntax. |
| **drift** | Divergence between an installed managed asset and the definition expected for its installed version and the user's selected configuration. Drift is a factual observation: it carries no judgment about whether the change was intentional, valid, or wanted. |
| **version drift** | Drift of a whole installation: its installed version differs from the running build, so every managed asset's expected definition may have changed even where its content matches what was installed. Detected by the drift check alongside asset drift; never resolved by drift correction. When the running build is newer, upgrade resolves it; when the installed version is newer, or is not a release that can be ordered, this build can neither correct nor upgrade it. |
| **drift correction** | Restoring affected managed assets to the definition expected for their installed version. Distinct from upgrade. |
| **upgrade** | Adopting a newer version of installed capabilities. Distinct from drift correction (`PRD_INSTALLATION`). |
| **operation completion** | Whether the approved changes were applied. Reported separately from functional readiness. |
| **functional readiness** | Whether the resulting installation can be expected to work as designed. Acknowledged drift can leave it uncertain without making the operation a failure. |

## 4. Crew Execution

| Term | Definition |
| :--- | :--- |
| **specialist** | A crew agent with a canonical specialty; its active-session instances each carry one assigned role. Specialty and instance are distinct (`PRD_CREW`). |
| **specialty** | The canonical focus and expertise of a specialist, with a declared set of admitted roles; not itself a permission profile. |
| **instance** | A concrete agent created for an assignment with one admitted role and its corresponding permissions; distinct from the reusable specialty. |
| **simplification** | The specialty focused on cleanup, removal of duplication, and reduction of structural complexity, used in ordinary crew roles or as the collector in GC (`PRD_GC`). |
| **role class** | An instance's assigned responsibility, fixing its permissions and interaction class. A specialty declares its admitted roles; each instance is created with one of them (`PRD_CREW`). |
| **Takt operation** | A mutation the product performs by its own deterministic design: installation, update, projection, and harness enforcement. Attributed to Takt. |
| **memory nature** | What a memory entry is, independent of whether it still governs: **proposal** (an option raised and not adopted), **formalized decision** (a choice approved by its authority), **verified observation** (a result backed by evidence), or **hypothesis** (an interpretation awaiting verification) (`MEM-TYP-5`). |
| **Global Tier / Workspace Tier** | The two memory partitions: Global holds cross-project user preferences and environment quirks; Workspace holds the active repository's invariants, constraints, and domain knowledge (`MEM-SCP-1`). |
| **session anchor** | A harness-created memory record that bounds a session's memory graph: the start anchor every session entry links to, and the end anchor (the session narrative). A declared continuation links a session's start anchor to the previous session's end anchor (`MEM-OPS-5`). |
| **session narrative** | The single end-of-session record and end session anchor. The agent holding the user-facing interface supplies the session objective and final state; the harness generates the chronology of the session's memory entries and their links, without restating them (`MEM-OPS-4`). |
| **maintenance cycle** | A bounded harness-controlled run outside active task execution, not an orchestrator work unit. GC over the workspace and dreaming over memory have separate contracts (`PRD_GC`, `PRD_DREAMING`), not inherited rules. |
| **collector** | The simplification instance participating in a GC cycle, realigning the workspace to declared norms with bounded, behavior-preserving deltas and independent verification (`PRD_GC`). Not the name of every ordinary simplification assignment. |
| **structural doctrine** | The engineering standard of `governance/DOCTRINE.md`: the structural norms governing how work is built, which the collector realigns to (`PR-MNT-13`). Deviating from it grounds a finding, never by itself a licence to correct (`STR-9`). |
| **mandate class** | One of the families of work in the collector's mandate, enumerated in `PR-MNT-4`. A cycle declares exactly one as its bounded scope (`PR-MNT-4`), which is also the unit damage is attributed to (`PR-MNT-31`). |
| **workspace analyzer** | The static analysis layer over the code the crew builds, from which the collector's findings originate (`PR-MNT-24`). Distinct from the declarative detectors of the control bus (`PR-OBS-DET-1`..`4`), which observe crew execution rather than the code it produces. |
| **masked dead code** | Code whose only callers are tests: the test establishes no use, it hides the code's death from analysis (`PR-MNT-25`). Not to be confused with code the session introduced whose consumer has not arrived yet (`PR-MNT-26`). |
| **dreaming** | The long-term memory consolidation lifecycle: Periodic Simple Mode, user-present Deep Dream, and macro consolidation over closed sessions (`ARCH_MEMORY` §6, `PRD_DREAMING`). Distinct from workspace GC. |
| **dreaming operation** | A typed change the dreaming specialist proposes and the harness validates before applying: group, link, relocate, propose removal, or attach a derived label (`PR-DRM-16`). Distinct from VFS consolidation. |
| **candidate set** | The reproducible set of groups, pairs, relinks, and signals the harness prepares for the dreaming specialist to judge (`PR-DRM-17`). |
| **action** | One tool execution recorded by the control plane within the scope being measured; the unit in which bounded budgets and enforcement windows are counted (`PR-OBS-PRG-2` in `PRD_OBSERVABILITY`). Not a unit of elapsed time and not an agent's own count of its steps. |
| **problem rate** | The rate of problems appearing during a session — checks that start failing, files re-edited after consolidation, discarded or redone work, recurring collisions — derived by the harness from deterministic signals, never from an agent's own account, over a denominator of recorded work rather than elapsed time (`PR-MNT-9` in `PRD_OBSERVABILITY`). Its stability, not its level, is what the system regulates. |
| **abortion** | The interface transition that ends a temporary holder's layer without handoff, negotiation, or artifact; it originates only from the user or the harness (`IR-19`, `IR-24`). |
| **Action Journal** | The immutable, append-only record of every operation performed against the VFS (`PR-VFS-JRN-1`). |
| **claim** | The VFS record that grants one agent instance exclusive ownership of a file scope, pending or active, until released or revoked (`PR-VFS-COL-4`). |
| **consolidation** | The deterministic flush of staged VFS state onto the physical filesystem, distinct from committing (`PR-VFS-CSL-1`, `PR-VFS-GIT-3`). |
| **convening agent** | The agent that holds the interface and proposes a switch to lend it to a specialist (`IR-19`, `IR-24`). |
| **DAG** | The directed acyclic graph of work units the orchestrator owns, shown as a projection of the execution history. |
| **DAG projection** | The graph state derived deterministically from the execution history; it is never a mutable model of its own. Distinct from the platform projection of Takt into OpenCode v2. |
| **envelope** | The four-field structure (Result, AdditionalContext, ExtraArtifacts, Memory) in which a temporary holder returns its outcome on handoff (`IR-22`). |
| **execution history** | The append-only record of observed execution, written and sequenced only by the harness (`PR-DAG-REP-2`). |
| **gate** | A point at which work proceeds only if a condition holds: a verification verdict before consolidation, or user approval before an action. |
| **handoff** | The negotiated return of the interface: the temporary holder proposes it and delivers its envelope to the agent underneath, after user confirmation (`IR-19`, `IR-20`). |
| **harness** | The deterministic layer Takt provides around the crew: admission, enforcement, execution history, and the VFS. It is not OpenCode, which is the base harness. |
| **interface holder** | The agent at the top of the interlocutor stack, the only one that holds the chat interface and speaks to the user (`IR-14`). |
| **interlocutor** | An agent that speaks directly with the user. The interlocutor stack has at most two layers: the active interlocutor and the one beneath it (`IR-14`, `IR-17`). |
| **invariant** | A reference against which a staged delta is judged: in precedence order, the user's goal and directives, the work unit's contract, and the framework's planning artifacts; never an agent's own claim (`PR-VFS-CSL-3`). |
| **milestone** | A point at which accepted work is committed by the orchestrator after acceptance checks pass on the materialized workspace (`PR-VFS-GIT-3`). |
| **orchestrator** | The crew agent that dispatches work units, owns the DAG, and keeps every agent's delivery aligned to the goal (`PR-CRW-5`). |
| **projection** | The delivery of Takt's agents and capabilities into OpenCode v2 through its native configuration, plugins, hooks, and permissions (`PR-PLT-2`). Distinct from the DAG projection. |
| **switch** | The interface transition that lends the interface to a capable specialist by pushing it onto the interlocutor stack, after user confirmation (`IR-19`, `IR-20`). |
| **temporary holder** | The specialist that holds the interface while it is lent, until handoff, early close, failure, or abortion (`IR-16`, `IR-19`). |
| **verdict** | The judgment a verification instance, distinct from the author, issues on a staged delta; it must match the staged revision for consolidation to proceed (`PR-VFS-CSL-3`). |
| **VFS** | The virtual file system that stages agents' file mutations, enforces exclusive ownership, and consolidates onto disk, leaving the physical filesystem untouched until consolidation (`PR-VFS-STG-1`, `PR-VFS-STG-2`). |
| **work unit** | The unit of work the orchestrator dispatches to an agent under a contract, and the unit the DAG and the execution history record (`PR-CRW-5`, `PR-DAG-REP-3`). |

## 5. Quality and Testing

| Term | Definition |
| :--- | :--- |
| **blind test authorship** | Writing acceptance and contract tests (Q2) from the specification, frozen contracts, and traceability matrix, never from the implementation; the implementer's in-flight files stay outside the test author's reach through file ownership (`PR-QA-12`). |
| **Definition of Done (DoD)** | The conditions an objective must meet to count as complete. The strategy sets the baseline, the test policy carries the workspace default, and a test plan may only tighten it (`PR-QA-5`, `PR-QA-6`). |
| **environment requirements** | The services, data, configuration, and platforms a test plan needs beyond what the repository already runs; authored only when that environment is non-trivial (`PR-QA-8`). |
| **mutant** | A deliberate small fault introduced into a copy of the code to check whether the tests detect it. A **surviving mutant** is one no test detects; an **equivalent mutant** behaves identically to the original and is discarded (`PR-QA-15`). |
| **QA planned** | An objective whose acceptance the selection tree judged to need the QA workflow rather than individual quality specialists; its test plan and traceability matrix are then required invariants (`PR-QA-4`). |
| **QA workflow** | The orchestrator's state machine for an objective's testing, `takt-qa-workflow`: a route for assessing an existing deliverable and the acceptance tail of a route with QA planned (`PR-QA-20`). |
| **quality assurance (role class)** | The class of the QA analysts — mutation, exploration, non-functional analysis — that critique the materialized product without mutating it and issue no gate verdict (`PR-CRW-19`). |
| **organizational test strategy** | The test strategy Takt AI governs for every workspace, in place of an organization's universal one; defined once in `takt-test-strategy` (`PR-QA-1`). |
| **requirements traceability matrix (RTM)** | The per-objective table linking each requirement to its test condition, must-not-fail marking, owning DAG node, and evidence, each column with one author, extended only by supplementing entries (`PR-QA-9`, `PR-QA-10`). |
| **test completion report** | The record of testing performed, deviations, completion evaluation, residual risk, reusable assets, and lessons, made by the Verify node that closes the objective's acceptance; distinct from the orchestrator's user-facing completion report (`PR-QA-19`). |
| **test oracle** | What decides whether a test's outcome is correct. A Q2 test is an immutable oracle for the implementer (`PR-QA-13`). |
| **test plan** | The per-objective statement of chosen quadrants, test levels, entry and exit criteria, must-not-fail risks, and effective Definition of Done (`PR-QA-7`). |
| **test policy** | The single per-workspace adaptation of the organizational test strategy to the project, held in the Workspace Tier and carrying its default Definition of Done (`PR-QA-6`). |
| **testing quadrant** | One of four classes of testing by orientation (technology- or business-facing) and purpose (supporting the team or critiquing the product), Q1 to Q4, each with one owning specialty (`PR-QA-2`). |

## 6. Release Scope Vocabulary

| Term | Definition |
| :--- | :--- |
| **first release** | The release this corpus ships. Every requirement stated in the corpus must be satisfied within it. |
