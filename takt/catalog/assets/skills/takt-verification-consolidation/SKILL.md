---
name: takt-verification-consolidation
description: "You are committing a plan, consolidating staged work, closing a milestone, resolving an incomplete consolidation, requesting the blind review pair, or contesting a terminal failure. Explains when and how work is verified, consolidated and committed."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Takt Verification and Consolidation

## Verification

Verification is a planned decision over a phase (the nodes a verification node lists as prerequisites),
judged as a whole. A finished unit creates no uncertainty, and each gate costs a dispatch and
delays its authors' consolidation. Plan one only when a module is complete, a change's deviation would
cascade into later work (such as a frozen contract or interface it feeds), or the real state is unknown; everything else
consolidates on delivery. One gate covers several authors, so gates stay clearly fewer than
implementation units. A planned gate, and any verification or repair beyond the
unplanned-delegation limit, needs a committed plan before the first delegation. For a staged phase, declare all `author_keys` in one Verify delegation; a passing verdict supports that author's consolidation, and a failing
verdict is corrected, never discarded: route each of its findings by your OPERATIONS' owner table; after the correction,
delegate Verify again with that author key, and consolidate only when a
verdict covers the staged revision. Build,
lint and full suites run in Verify units; checks on the materialized workspace after consolidation remain the executable acceptance, not proof of an unconsolidated delta.

## Consolidation and commit

Consolidating is not a Git commit: accept each vertical slice with one global Verify node after its writers finish; commit a milestone only after such a node passes with no problems found, and
load takt-git-commit before writing the message. An incomplete consolidation identifies
its manifest, author, files and state. Invoke the original normal operation as the
orchestrator; it requests approval to restore that exact snapshot, or, if
files diverged, to keep current files and close the incomplete effect as interrupted
with evidence retained. Repair approval never authorizes consolidation; ownership,
current verdicts and physical bases are checked again. If the physical base changed,
inspect the named files and use claim_assign with author_key to continue the staged
work from the current base, or explicitly abandon it with vfs_discard.

## Blind review pair and contests

Request the blind review pair
only when the user asks, work advanced past deviations of unknown origin, the real state is
unknown beyond what a quick validation can map, an objective's recovery was exhausted, a DAG concludes in which two or more phases each reached the ceiling, or a large refactor of unknown code-level impact has invariants to contrast; say pair and target
in the brief. The pair judges the invariants of the work the brief names, staged or materialized, never your own questions or goals, and returns unmerged
findings from identical briefs. Read them as a map, not a repair list: what both report is a point of critical attention, what only one reports shows where agents get confused, and what neither reports leaves the priority until the objective is back under control; replan from that map. Dispute a verdict with evidence before
the user rather than re-dispatching. To contest a unit's recorded terminal failure, call `dispatch_contest`
(at most {{policy.contests}} per session): it records the contest and spends one; you delegate the Verify of
that unit's author key yourself.
