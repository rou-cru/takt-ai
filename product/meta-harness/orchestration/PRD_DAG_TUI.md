# PRD: Live DAG Projection in the OpenCode v2 TUI

## 1. Outcome

**PR-DTUI-1.** Takt MUST provide a read-only OpenCode v2 TUI view that renders the execution DAG as a graph, not as a list. The view makes the orchestrator's declared route and the harness-observed execution state visible together without allowing the TUI to select, authorize, or mutate work.

**PR-DTUI-2.** The view MUST NOT offer session navigation, node actions or execution controls, graph editing or route selection, cost or token analytics, or read OpenCode v2's private database.

## 2. Authority and invariants

**PR-DTUI-3.** The view consumes the history-derived topology and temporal states of the execution history. Every fact it shows, including layers and counts, is produced by Takt's snapshot; the view only decides what to draw and how. It is not an execution authority. It MUST NOT infer execution, closure, or success from missing events, agent text, elapsed time, or OpenCode v2 UI state. Refresh age and transport latency are capture metadata only.

## 3. Entry points

**PR-DTUI-4.** The view is reachable as a full route through a command with an optional key binding, and as a compact graph in the session sidebar. It renders fan-out, fan-in, multiple roots, and parallel branches, and shows the capture states current, empty, stale, unavailable, and uncertain, and marks unmonitored execution.

## 4. Graph layout

**PR-DTUI-5.** The view MUST render nodes and directed prerequisite edges, including joins and branches. A list alone does not satisfy this contract.

**PR-DTUI-6.** Graphs with no prerequisite path between them are drawn as separate blocks, never interleaved by layer. The route's rendering flows left to right, one column per layer; a layer that would overflow the width starts a new band below that flows the opposite way, and so on. The route never overflows sideways and scrolls vertically when the bands do not fit the height. Bands are balanced in width. Every edge follows the flow: within a band it advances in the band's direction, and between bands it only descends. Each edge leaves and enters a box through whichever of its four sides faces where the edge comes from or goes to: an edge turning into the next band leaves the bottom of a box with nothing below it in its column and enters the top of a box with nothing above it in its column. Edges that share a prerequisite or a dependent travel as one trunk from a point near the shared box, and a shared trunk never suggests an edge that does not exist. No edge runs through a box or along its border: an edge touches a box only at the side it leaves or enters through, and elsewhere keeps a blank cell from every box. Parallel edge tracks keep a blank column between them, a crossing is drawn differently from a junction, and no track runs against the edge of the viewport. Withdrawn units are not drawn. Each node, work unit or activity, is a single-line box with two cells: its state glyph, then its name with one blank cell on each side; nothing else is written in it. A box is as wide as its own name; a unit's name is bounded by `PR-DAG-REP-8`, so it is never cut. Activities run in their separate track, wrapped into rows within the width; an activity name wider than the route is cut with an ellipsis. No agent name, wave or width annotation is drawn on the route; the legend stays there.

**PR-DTUI-7.** The sidebar has 37 usable columns, fixed by the host, and scrolls only vertically; its form is compact and no row ever wraps:

- A bold `DAG` title followed by the snapshot's counts of units by state, or by the capture health when it is not current. The header of the route reads the same counts. Done units appear only in this count.
- The frontier (every unit that is in flight or planned, and every unit that ended without completing) as a track graph read top-down: the state glyph is the node, each edge is a two-column track carried down to its dependent, and joins and branches are drawn as connector rows, and a unit that alone follows the unit on the row above, with no other track open, is marked `└─` and indented one level per link. Active and planned units show their name only, never their agent.
- A layer wider than the track budget is one grouped row of its members' glyphs and count; a frontier that still needs more tracks is listed flat in dependency order.
- The sidebar is a quick reference: it shows that a plan exists and what is happening, not necessarily every unit. Activities follow as one row each in their separate track; withdrawn units stay out of the counts.

## 5. Graph semantics

**PR-DTUI-8.** Edges point from a prerequisite to its dependent work unit. For every declared prerequisite A of unit B, the snapshot MUST contain an edge from A to B. A prerequisite edge describes the orchestrator's declared structure. It does not grant the harness authority to schedule or block B.

**PR-DTUI-9.** The layout SHOULD use deterministic layers, drawn as columns or as rows. A unit without prerequisites has layer 0; any other unit has a layer one greater than its deepest prerequisite. The snapshot carries each unit's layer; the view does not recompute it.

**PR-DTUI-10.** The layout MUST be stable when only node state changes, except a withdrawal, which removes the unit and its edges (`PR-DTUI-6`). A topology revision MAY cause a new layout. The renderer MUST NOT reorder the graph on every lifecycle or telemetry update.

**PR-DTUI-11.** A change in activities may resize the separate activity track; it MUST NOT alter work-unit layout or order nodes by lifecycle state.

## 6. Node states

**PR-DTUI-12.** The view shows the orchestrator's delegation: what has been delegated and finished, what is delegated and in flight, and what is planned to be delegated. It renders the temporal states recorded in the execution history and MUST NOT collapse distinct in-flight conditions into a single running state. Acceptance, verification and assurance are not part of the view or of its snapshot.

### 6.1 Work source

**PR-DTUI-13.** Every work-unit node has the delegated kind; direct orchestrator and non-delegated GC/maintenance execution are activity records, never work-unit nodes.

| Record | Kind | Meaning |
|---|---|---|
| Work-unit node | delegated | Ordinary work delegated through dispatch, including simplification outside GC. |
| Activity | orchestrator | Direct work performed by the orchestrator itself. |
| Activity | maintenance | Harness-run GC/maintenance that was not delegated as ordinary work. |

**PR-DTUI-14.** This classification is independent of temporal state and does not affect authority, scheduling, declared prerequisites or layout depth. Session, plan/revision, and GC-cycle records never become work units. An activity is shown only under its activity identity (for GC, the cycle identity) as its name, with the state glyph every node uses, in the separate activity track and with no prerequisite edges.

## 7. Observability required by the view

**PR-DTUI-15.** The view consumes a complete snapshot of the topology and lifecycle recorded in the execution history, not isolated OpenCode v2 events. Additional role, action-count, token, model, provider, cost, and VFS metadata remain content-free.

## 8. Snapshot contract

**PR-DTUI-16.** Takt MUST expose a read-only snapshot query that takes the workspace, the private state, the root session, and the output format, and changes nothing.

**PR-DTUI-17.** The root-session parameter scopes the projection to one root session; a session that has recorded nothing yet is an empty DAG, never an earlier session's graph, unless it declared continuity with an earlier crew session (`PR-DAG-USE-1`): then the projection is the earlier history followed by its own. The continuity declaration is the memory service's; the execution history keeps no copy, and Takt asks the service for it. An earlier session whose history is not in this workspace's execution history adds nothing. The TUI passes the root of the session it is showing and drops the displayed graph when that session changes. A response scoped to another session is discarded, and the displayed revision resets with the session.

**PR-DTUI-18.** The response MUST contain:

| Field | Content |
| :--- | :--- |
| Schema version | Contract version, 1. |
| Projection revision | Monotonic revision of the projection. |
| History position | Position in the execution history the projection reflects. |
| Session | The root session the projection is scoped to. |
| Capture | Capture state of the snapshot, current in the nominal case. |
| Nodes | One record per work unit: identity, kind, temporal state, in-flight condition (`unmonitored` when capture of that execution was lost), session, attempt, launch status, prerequisites, layer, and agent once the unit is admitted. |
| Edges | One record per declared dependency, with source and target unit identities. Work without a committed plan has none. |
| Counts | Units planned, in flight, completed, failed, backtracked, interrupted and withdrawn. Withdrawn units are counted apart from the rest. |
| Activities | Always present, empty when no activities exist. Each record carries activity identity, kind, state, and the applicable in-flight condition, including `unmonitored`, or outcome. |

**PR-DTUI-19.** The projection revision MUST be monotonic for a projection scope and advances with every history entry the projection reflects, so every render-visible projection field changes only with the revision. The view's own capture health, stale or unavailable, is local and applies regardless of revision. The TUI MUST replace its snapshot only when the received revision is newer than the displayed revision.

**PR-DTUI-20.** The history position is an ordering and provenance value. It MUST NOT be interpreted as elapsed time or a timeout.

**PR-DTUI-21.** The agent field contains the short label of the specialist admitted for the unit's current attempt, absent until the unit is first admitted. Plans do not declare it.

## 9. Live update behavior

**PR-DTUI-22.** The view MAY use bounded polling to obtain snapshots. The polling interval is a transport concern only; it MUST NOT participate in DAG state, progress budgets, detection, or enforcement.

**PR-DTUI-23.** The TUI MUST:

- avoid overlapping snapshot requests;
- ignore older revisions;
- render a complete snapshot atomically;
- stop polling when the last view showing the graph, the route or the sidebar, is disposed; views that are open together share one poll;
- avoid redrawing when the projection revision is unchanged.

## 10. OpenCode v2 surface

**PR-DTUI-24.** The view uses only the supported OpenCode v2 TUI plugin surface: plugin discovery, route registration and navigation, command and key binding, the sidebar slot, lifecycle cleanup that stops polling and cancels an active request, and optional non-blocking notifications. It MUST NOT depend on undocumented internal TUI components when the supported surface is sufficient.

**PR-DTUI-25.** OpenCode v2 events MAY trigger a refresh, but they are not authoritative DAG state. The view MUST NOT open sessions.

## 11. Failure behavior

**PR-DTUI-26.** Telemetry or TUI failure MUST NOT interrupt the governed execution path.

**PR-DTUI-27.** Each failure condition has its required display.

| Condition | Required display |
|---|---|
| No snapshot exists | `waiting for confirmed projection` |
| Query succeeds | Render the returned graph and revision |
| Query fails after a valid snapshot | Keep graph; mark it `stale` |
| Takt cannot provide a projection | Mark `unavailable`; do not infer state |
| Projection contains uncertain execution | Render the affected node as `uncertain` |
| Projection contains unmonitored execution | Render the affected node or activity as `unmonitored`, never as hung or failed, and do not attribute it to an agent |
| Empty valid plan | Render an explicit empty DAG state |
| Invalid topology | Reject the snapshot and display a projection error; do not render a partial graph |
| OpenCode v2 event stream is unavailable | Continue from Takt snapshot; do not attribute failure to an agent |
