# Implementation Progress

Static recount: **2026-10-18**, `ec9616a0`. Confirmed corrections only; other classifications are retained. This report is not part of the normative corpus ([INDEX](INDEX.md)) and does not waive any requirement.

## Scoring

Count each document's own requirements, or obligatory articles/blocks where IDs are absent. Score production-integrated completion as 1, partial integration as 0.5, and absent integration or manual validation without recorded evidence as 0. Divide by all counted units and round half up. Missing classifications are unverified; documents without behavioral obligations are N/A. These figures are not release certification.

## Status

| Document | Progress | Complete | Partial | Not integrated | Unverified manual | Own units |
|---|---:|---:|---:|---:|---:|---:|
| [PRD_ORCHESTRATION](meta-harness/orchestration/PRD_ORCHESTRATION.md) | 65% | 11 | 25 | 0 | 0 | 36 |
| [PRD_DAG](meta-harness/orchestration/PRD_DAG.md) | 71% | 13 | 11 | 2 | 0 | 26 |
| [PIRS_ORCHESTRATION](meta-harness/orchestration/PIRS_ORCHESTRATION.md) | 70% | 11 | 17 | 0 | 0 | 28 |
| [PRD_HARNESS](meta-harness/harness/PRD_HARNESS.md) | 80% | 18 | 9 | 1 | 0 | 28 |
| [PRD_VFS](meta-harness/harness/PRD_VFS.md) | 90% | 25 | 6 | 0 | 0 | 31 |
| [ARCH_CONTEXT](meta-harness/context/ARCH_CONTEXT.md) | 70% | 11 | 10 | 2 | 0 | 23 |
| [ARCH_MEMORY](meta-harness/context/ARCH_MEMORY.md) | 64% | 17 | 12 | 7 | 0 | 36 |
| [PRD_CREW](meta-harness/crew/PRD_CREW.md) | 75% | 10 | 10 | 0 | 0 | 20 |
| [PRD_GC](meta-harness/maintenance/PRD_GC.md) | 83% | 13 | 7 | 0 | 0 | 20 |
| [PRD_DREAMING](meta-harness/maintenance/PRD_DREAMING.md) | 2% | 0 | 1 | 22 | 0 | 23 |
| [PRD_QA](meta-harness/quality/PRD_QA.md) | Unverified | — | — | — | — | 20 |
| [PRD_OBSERVABILITY](meta-harness/observability/PRD_OBSERVABILITY.md) | 46% | 4 | 24 | 7 | 0 | 35 |
| [PRD_SETUP](app/PRD_SETUP.md) | 91% | 27 | 4 | 1 | 0 | 32 |
| [PRD_INSTALLATION](app/installation/PRD_INSTALLATION.md) | 92% | 39 | 7 | 0 | 0 | 46 |
| [PRD_TUI](app/tui/PRD_TUI.md) | 95% | 20 | 0 | 1 | 0 | 21 |
| [VDS_TUI](app/tui/VDS_TUI.md) | 95% | 25 | 3 | 0 | 0 | 28 |
| [PRD_PLATFORMS](meta-harness/platforms/PRD_PLATFORMS.md) | 77% | 7 | 6 | 0 | 0 | 13 |
| [PRD_RELEASE_VALIDATION](app/PRD_RELEASE_VALIDATION.md) | 43% | 3 | 6 | 1 | 4 | 14 |
| [CONSTITUTION](governance/CONSTITUTION.md) | 50% | 0 | 13 | 0 | 0 | 13 articles |
| [DOCTRINE](governance/DOCTRINE.md) | 56% | 2 | 14 | 0 | 0 | 16 |
| [brand](brand.md) | 77% | 7 | 6 | 0 | 0 | 13 blocks |

Unverified: QA's requirement-level score, installation's full reclassification, runtime/visual/platform acceptance, manual evidence and staged VFS work. No tests or independent review underpin this recount.

## Gaps

- [PRD_ORCHESTRATION](meta-harness/orchestration/PRD_ORCHESTRATION.md): 36 own requirements: 11 complete, 25 partial, 0 not integrated. PR-ORQ-35..37 (slice control point, story concurrency, final stories before consuming lanes) rest on instructions. Gaps: orchestrator judgment (goal supervision, decomposition, escalation, milestone calibration, route selection and resumption after a lent session) rests on prompt with no harness gating; recovery does not block consolidating the scope's deltas before its result is demonstrated; and a fresh claim replaces another root session's overlapping claim and discards its staged work instead of requiring explicit revocation.
- [PRD_DAG](meta-harness/orchestration/PRD_DAG.md): 26 own requirements: 13 complete, 11 partial, 2 not integrated. Gaps: no verification verdict, acceptance result, consolidation or session closure enters the execution history, so cross-author precedence, contest verdicts and contract satisfaction are not readable from the projection; plan revision records no rationale or user authority and an invalid first baseline is rejected without a record; ordinary termination never yields a failed outcome, only forced backtracking exists, and causal associations and execution-history continuity are missing.
- [PIRS_ORCHESTRATION](meta-harness/orchestration/PIRS_ORCHESTRATION.md): 28 own requirements: 11 complete, 17 partial, 0 not integrated. Gaps: confirmation of switches and handoffs, the choice of Result and its evidence, escalation judgment, and the opening and conduct of a lent session rest on agent instruction because OpenCode v2 exposes no programmatic hook; the handoff envelope is not validated against the four-field contract and the convening agent can emit an abortion; customizing the user environment at low effort is limited to the setup menu's configuration and model assignment.
- [PRD_HARNESS](meta-harness/harness/PRD_HARNESS.md): 28 own requirements: 18 complete, 9 partial, 1 not integrated. Gaps: configuration custody is granted by static role rather than following the interlocutor-stack holder. Interlocutor transitions have no idempotency key, an agent-invoked abort is stamped as user-originated, envelope validation covers shape only, a failed activation can leave a partial transition, and an Outraged result has no isolated durable record.
- [PRD_VFS](meta-harness/harness/PRD_VFS.md): 31 requirements: 25 complete, 6 partial. Partial: STG-6 (recovery consolidation gate), STG-7/COL-4 (automatic cross-root discard), STG-8 (idle release confirmation), CSL-7 (verdict scope), JRN-3 (orchestrator/verifier inspection grants).
- [ARCH_CONTEXT](meta-harness/context/ARCH_CONTEXT.md): 23 own requirements: 11 complete, 10 partial, 2 not integrated. Gaps: the tool-call ceiling, one-paragraph cap, explicit emptiness and always-present client context for a convened specialist, conflict resolution between instructions, skill load-failure reporting and context-reduction quality rest on prompt judgment; ephemeral noise is not pruned, and no check validates that a context reduction preserved fluency or precision.
- [ARCH_MEMORY](meta-harness/context/ARCH_MEMORY.md): 36 own requirements: 17 complete, 12 partial, 7 not integrated. Gaps: tier-leakage prevention is heuristic; entry concision, timing and no-future-content rules, the authority needed to correct or supersede, repository precedence and chronological retrieval order rest on skills; current applicability is derived only within the closing session's narrative and is not queryable across sessions, and retrieval has no status filter; the consolidation and Dreaming cycle (grouping, Deep Dream, playbooks) and the local semantic representation of entries have no implementation.
- [PRD_CREW](meta-harness/crew/PRD_CREW.md): 20 requirements: 10 complete, 10 partial. PR-CRW-20 is partial: the harness refuses resuming the authoring pm session; the rest of the pm separation is conduct. Gaps: role conduct and blind review depend on instructions; dreaming is absent. PR-CRW-19 is partial: QA permissions exist, but shell non-mutation and reporting remain conduct rules.
- [PRD_GC](meta-harness/maintenance/PRD_GC.md): 20 own requirements: 13 complete, 7 partial, 0 not integrated. Gaps: managed cycles declare their scope from the session delta alone without following code-graph reachability; magic-value, defensive-check, docstring-quality, minimal-fix and single-architectural-finding judgments, and the later removal of tests left failing by a dead-code removal, rest on collector judgment with no tool gate; the demotion of a mandate class lives only in process memory and does not survive a restart.
- [PRD_DREAMING](meta-harness/maintenance/PRD_DREAMING.md): 23 own requirements: 0 complete, 1 partial, 22 not integrated. No production path schedules, isolates or dispatches Simple, Deep or Macro Dream, applies deference or the fast path, writes the cycle report and diary, or enforces its invariants; no local inference capability, candidate generation or operation validation exists; only the maintenance role class's refusal of orchestrator dispatch exists.
- [PRD_OBSERVABILITY](meta-harness/observability/PRD_OBSERVABILITY.md): 35 own requirements: 4 complete, 24 partial, 7 not integrated. Gaps: the envelope, bus and store are recorded end to end and GC uses OBSERVE, CONTAIN and ESCALATE, but THROTTLE, ROLLBACK and GATE are never invoked; the only detector is the GC mandate-reversion check, with a fixed threshold and no baseline. The detector policy tables, problem-rate recording, external export and the stream-liveness class do not exist, and escalation carries no analyzed alternatives.
- [PRD_SETUP](app/PRD_SETUP.md): 32 requirements: 27 complete, 4 partial, 1 absent. Partial: PR-SET-12 (dreaming assets), 13 (optional skills/workflows), 17 (reconciliation explanation), 31 (supporting-asset declarations). Absent: PR-SET-34 (hardware gating).
- [PRD_INSTALLATION](app/installation/PRD_INSTALLATION.md): retained estimate: 46 requirements, 39 complete, 7 partial; full reclassification is unverified. Gaps: upgrade executes as install; restore lacks authorization, edit protection and text output; recovery and uninstall reporting are incomplete. Interactive setup does reload recorded choices.
- [PRD_TUI](app/tui/PRD_TUI.md): 21 own requirements: 20 complete, 0 partial, 1 not integrated. Gap: PR-TUI-25, no flow adjusts admission limits.
- [VDS_TUI](app/tui/VDS_TUI.md): 28 own requirements: 25 complete, 3 partial, 0 not integrated. Partial: PR-UX-6 (no ASCII variant of the status glyphs, field bar, panel borders or progress bar), PR-UX-21 (reconciliation reaches the review but not the toggle), PR-UX-29 (page scrolling missing in uninstall and models). The notice below 60×20 asks to resize but names no alternative.
- [PRD_PLATFORMS](meta-harness/platforms/PRD_PLATFORMS.md): 13 own requirements: 7 complete, 6 partial, 0 not integrated. Gaps: artifact validity is only checked after deployment and covers a subset of deployed artifacts, and proactive maintenance (dreaming) is not delivered through the OpenCode integration.
- [PRD_RELEASE_VALIDATION](app/PRD_RELEASE_VALIDATION.md): 14 requirements: 3 complete, 6 partial, 1 absent, 4 manual requirements without recorded evidence (PR-REL-5/7/11/12, counted at zero). Gaps: first-use image, manual gate, native-platform evidence and separate compatibility results.
- [CONSTITUTION](governance/CONSTITUTION.md): 13 authority articles assessed: 0 complete, 13 partial, 0 without verifiable integration. Gaps: all thirteen articles rest on explicit agent instruction, with deterministic support only for Git and secret-file permissions, confirmations for destructive actions and installed-file ownership; value, loyalty, disagreement, goal direction, the one-time exception rule and the orchestrator not performing an available specialty's judgment are enforced by prompt alone, as are the agent's conduct toward the user's environment and the precedence of formal intent.
- [DOCTRINE](governance/DOCTRINE.md): 16 clauses: 2 complete, 14 partial. STR-4/15/16 have production integration through memory validation and serialized VFS ownership checks, not product-wide enforcement. Other gaps concern skill structure and crew conduct.
- [brand](brand.md): assessment of 13 numbered blocks that apply to the surfaces the product has (the brand does not use requirement IDs): 7 complete blocks, 6 partial, 0 not integrated. Gaps: sustained-capital group labels, the repository README departing from the stated descriptor and making an unevidenced claim, no documented alternative route for terminals below the minimum size, and the palette check not running automatically with the surface contract incomplete.
