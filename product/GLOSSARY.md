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

## 3. Installation State

| Term | Definition |
| :--- | :--- |
| **Takt-managed choice** | An explicit configuration decision made through Takt, including model assignments and exclusions; part of the expected configuration, not an external conflict. |
| **Takt configuration** | The set of installation and behavior decisions managed by Takt, including model assignments and exclusions. |
| **external modification** | A change made outside Takt’s configuration mechanisms; its origin does not imply fault or incompatibility. |
| **canonical version** | The reference definition distributed by Takt for an identified version. |
| **restore / sync** | Recover canonical content in an explicit scope; not necessarily undoing the last operation or resetting the full installation. `sync` is conceptual vocabulary here, not a declaration of CLI syntax. |
| **drift** | Divergence between an installed managed asset and the definition expected for its installed version and the user's selected configuration. Drift is a factual observation: it carries no judgment about whether the change was intentional, valid, or wanted. |
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
| **mandate class** | One of the families of work in the collector's mandate — removal of dead code, complexity, duplication, documentation, analyzer integrity. A cycle declares exactly one as its bounded scope (`PR-MNT-4`), which is also the unit damage is attributed to (`PR-MNT-31`). |
| **workspace analyzer** | The static analysis layer over the code the crew builds, from which the collector's findings originate (`PR-MNT-24`). Distinct from the declarative detectors of the control bus (`PR-OBS-DET-1`..`4`), which observe crew execution rather than the code it produces. |
| **masked dead code** | Code whose only callers are tests: the test establishes no use, it hides the code's death from analysis (`PR-MNT-25`). Not to be confused with code the session introduced whose consumer has not arrived yet (`PR-MNT-26`). |
| **dreaming** | The long-term memory consolidation lifecycle: Periodic Simple Mode, user-present Deep Dream, and macro consolidation over closed sessions (`ARCH_MEMORY` §6, `PRD_DREAMING`). Distinct from workspace GC. |
| **action** | One tool execution recorded by the control plane within the scope being measured; the unit in which bounded budgets and enforcement windows are counted (`PR-OBS-PRG-2` in `PRD_OBSERVABILITY`). Not a unit of elapsed time and not an agent's own count of its steps. |
| **problem rate** | The rate of problems appearing during a session — checks that start failing, files re-edited after consolidation, discarded or redone work, recurring collisions — derived by the harness from deterministic signals, never from an agent's own account, over a denominator of recorded work rather than elapsed time (`PR-MNT-9` in `PRD_OBSERVABILITY`). Its stability, not its level, is what the system regulates. |
| **user-instructed change** | A mutation an agent performs because the user asked for it. Attributed to the user, whatever its target. A target that happens to be Takt's installed configuration does not make it a Takt operation (`PR-CFG-3`). |

## 5. Release Scope Vocabulary

| Term | Definition |
| :--- | :--- |
| **first release** | The release this corpus ships. Every requirement stated in the corpus must be satisfied within it. |
