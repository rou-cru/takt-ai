// Takt DAG — managed by takt-ai; edits are overwritten on sync.
// Read-only live projection of the execution DAG (PRD_DAG_TUI.md). The view
// renders what `takt-ai dag status` reports and never selects, admits or
// mutates work.
/** @jsxImportSource @opentui/solid */
import { resolve } from "node:path"
import { Plugin } from "@opencode/plugin/tui"
import type { RGBA } from "@opentui/core"
import { useTerminalDimensions } from "@opentui/solid"
import { For, Show, createEffect, createMemo, createSignal, onCleanup, onMount, type Accessor } from "solid-js"

const TAKT_AI = "__TAKT_AI_BINARY__"

// POLL_INTERVAL_MS is a transport cadence only: it never feeds node state,
// progress budgets, detection or enforcement (PRD_DAG_TUI.md §9).
const POLL_INTERVAL_MS = 2000

// ---------------------------------------------------------------------------
// Wire DTO (PRD_DAG_TUI.md §8), mirroring takt/history/snapshot.go.
// ---------------------------------------------------------------------------

type DagNodeState = "planned" | "in_flight" | "settled" | "withdrawn"
type DagFlight = "pending_launch" | "observed_running" | "suspended" | "cancellation_pending" | "uncertain" | "unmonitored"
type DagOutcome = "completed" | "failed" | "backtracked" | "interrupted"
type DagActivityKind = "orchestrator"
type Capture = "current" | "empty" | "uncertain" | "unavailable"
const DAG_STATES: readonly DagNodeState[] = ["planned", "in_flight", "settled", "withdrawn"]
const DAG_FLIGHTS: readonly DagFlight[] = ["pending_launch", "observed_running", "suspended", "cancellation_pending", "uncertain", "unmonitored"]
const DAG_OUTCOMES: readonly DagOutcome[] = ["completed", "failed", "backtracked", "interrupted"]
const DAG_CAPTURES: readonly Capture[] = ["current", "empty", "uncertain", "unavailable"]
const DAG_ACTIVITY_KINDS: readonly DagActivityKind[] = ["orchestrator"]
// An activity is running, or running with its capture lost; never failed or hung (PR-OBS-BUS-5).
type DagActivityFlight = Extract<DagFlight, "observed_running" | "unmonitored">
const DAG_ACTIVITY_FLIGHTS: readonly DagActivityFlight[] = ["observed_running", "unmonitored"]

/** Units in each state the view tells apart, tallied by Takt (PR-DTUI-18). */
interface DagCounts {
  readonly planned: number
  readonly in_flight: number
  readonly completed: number
  readonly failed: number
  readonly backtracked: number
  readonly interrupted: number
  readonly withdrawn: number
}
const DAG_COUNT_KEYS: readonly (keyof DagCounts)[] = ["planned", "in_flight", "completed", "failed", "backtracked", "interrupted", "withdrawn"]

export interface DagNode {
  readonly id: string
  readonly node_kind: "delegated"
  readonly session_id?: string
  readonly attempt_id?: string
  readonly state: DagNodeState
  /** Present only while state === "in_flight". */
  readonly flight?: DagFlight
  /** Present only while state === "settled". */
  readonly outcome?: DagOutcome
  readonly launched: boolean
  readonly contract?: string
  readonly prerequisites?: readonly string[]
  /** Depth in the DAG, assigned by Takt: 0 without prerequisites (PR-DTUI-9). */
  readonly layer: number
  /** Short label of the specialist admitted for the current attempt; absent until admitted. */
  readonly agent?: string
}

export interface DagActivity {
  readonly activity_id: string
  readonly node_kind: DagActivityKind
  readonly state: "in_flight" | "settled"
  readonly flight?: DagActivityFlight
  readonly outcome?: DagOutcome
}

interface DagEdge {
  readonly from: string
  readonly to: string
}

interface DagUnavailable {
  readonly capture: "unavailable"
}
type DagResponse = DagSnapshot | DagUnavailable

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value)
const isString = (value: unknown): value is string => typeof value === "string"
const oneOf = <T extends string>(value: unknown, values: readonly T[]): value is T => {
  const candidates: readonly string[] = values
  return typeof value === "string" && candidates.includes(value)
}
// isOptional accepts an absent field or one that passes check.
const isOptional = <T,>(value: unknown, check: (value: unknown) => value is T): value is T | undefined =>
  value === undefined || check(value)
const isOneOf = <T extends string>(values: readonly T[]) => (value: unknown): value is T => oneOf(value, values)
const isNonNegativeInteger = (value: unknown): value is number =>
  typeof value === "number" && Number.isInteger(value) && value >= 0
const isStringArray = (value: unknown): value is string[] => Array.isArray(value) && value.every(isString)

const isCounts = (value: unknown): value is DagCounts =>
  isRecord(value) && DAG_COUNT_KEYS.every((key) => isNonNegativeInteger(value[key]))

export function decodeSnapshot(value: unknown): DagResponse {
  const invalid = (): never => { throw new Error("invalid DAG snapshot JSON") }
  if (isRecord(value) && value.capture === "unavailable") {
    if (Object.keys(value).length !== 1) return invalid()
    return { capture: "unavailable" }
  }
  if (!isRecord(value) || value.schema_version !== 1 ||
      !isNonNegativeInteger(value.projection_revision) || !isNonNegativeInteger(value.history_position) ||
      !isString(value.session_id) || !oneOf(value.capture, DAG_CAPTURES) ||
      (value.plan_version !== undefined && !isString(value.plan_version)) ||
      !isCounts(value.counts) || !Array.isArray(value.nodes) || !Array.isArray(value.edges) ||
      !Array.isArray(value.activities)) return invalid()
  const nodes: DagNode[] = value.nodes.map((raw): DagNode => {
    if (!isRecord(raw) || !isString(raw.id) || raw.id.length === 0 || !oneOf(raw.state, DAG_STATES) ||
        raw.node_kind !== "delegated" ||
        !isOptional(raw.session_id, isString) ||
        !isOptional(raw.attempt_id, isString) ||
        !isOptional(raw.flight, isOneOf(DAG_FLIGHTS)) ||
        !isOptional(raw.outcome, isOneOf(DAG_OUTCOMES)) ||
        typeof raw.launched !== "boolean" ||
        !isNonNegativeInteger(raw.layer) ||
        !isOptional(raw.contract, isString) ||
        !isOptional(raw.prerequisites, isStringArray) ||
        !isOptional(raw.agent, isString) ||
        (raw.state !== "in_flight" && raw.flight !== undefined) ||
        (raw.state !== "settled" && raw.outcome !== undefined)) return invalid()
    return { id: raw.id, node_kind: raw.node_kind,
      ...(raw.session_id === undefined ? {} : { session_id: raw.session_id }),
      ...(raw.attempt_id === undefined ? {} : { attempt_id: raw.attempt_id }), state: raw.state,
      ...(raw.flight === undefined ? {} : { flight: raw.flight }),
      ...(raw.outcome === undefined ? {} : { outcome: raw.outcome }), launched: raw.launched, layer: raw.layer,
      ...(raw.contract === undefined ? {} : { contract: raw.contract }),
      ...(raw.prerequisites === undefined ? {} : { prerequisites: raw.prerequisites }),
      ...(raw.agent === undefined ? {} : { agent: raw.agent }) }
  })
  const edges: DagEdge[] = value.edges.map((raw): DagEdge => {
    if (!isRecord(raw) || !isString(raw.from) || !isString(raw.to)) return invalid()
    return { from: raw.from, to: raw.to }
  })
  const activities: DagActivity[] = value.activities.map((raw: unknown): DagActivity => {
    if (!isRecord(raw) || !isString(raw.activity_id) ||
        !oneOf(raw.node_kind, DAG_ACTIVITY_KINDS) ||
        (raw.state !== "in_flight" && raw.state !== "settled") ||
        (raw.flight !== undefined && !oneOf(raw.flight, DAG_ACTIVITY_FLIGHTS)) ||
        (raw.outcome !== undefined && !oneOf(raw.outcome, DAG_OUTCOMES)) ||
        (raw.state !== "in_flight" && raw.flight !== undefined) ||
        (raw.state !== "settled" && raw.outcome !== undefined)) return invalid()
    return { activity_id: raw.activity_id, node_kind: raw.node_kind, state: raw.state,
      ...(raw.flight === undefined ? {} : { flight: raw.flight }),
      ...(raw.outcome === undefined ? {} : { outcome: raw.outcome }) }
  })
  return { schema_version: 1, projection_revision: value.projection_revision,
    history_position: value.history_position, session_id: value.session_id,
    capture: value.capture, ...(value.plan_version === undefined ? {} : { plan_version: value.plan_version }),
    counts: value.counts, nodes, edges, activities }
}

export interface DagSnapshot {
  readonly schema_version: number
  readonly projection_revision: number
  readonly history_position: number
  readonly session_id: string
  readonly capture: Capture
  readonly plan_version?: string
  readonly counts: DagCounts
  readonly nodes: readonly DagNode[]
  readonly edges: readonly DagEdge[]
  /** Activities are not work units or edge endpoints. */
  readonly activities: readonly DagActivity[]
}

// ---------------------------------------------------------------------------
// Topology check (PRD_DAG_TUI.md §11): an invalid snapshot is rejected whole,
// never rendered partially.
// ---------------------------------------------------------------------------

export function activityError(activities: readonly DagActivity[]): string | undefined {
  const activityIDs = new Set<string>()
  for (const activity of activities) {
    if (!activity.activity_id || activityIDs.has(activity.activity_id)) return "duplicate or empty activity identity"
    activityIDs.add(activity.activity_id)
  }
  return undefined
}

export function topologyError(snapshot: DagSnapshot): string | undefined {
  const ids = new Set(snapshot.nodes.map((n) => n.id))
  if (ids.size !== snapshot.nodes.length) return "duplicate node identity"
  const invalidActivity = activityError(snapshot.activities)
  if (invalidActivity) return invalidActivity
  for (const e of snapshot.edges) {
    if (!ids.has(e.from) || !ids.has(e.to)) return `edge ${e.from} -> ${e.to} names an unknown node`
  }
  const children = new Map<string, string[]>()
  for (const e of snapshot.edges) children.set(e.from, [...(children.get(e.from) ?? []), e.to])
  const done = new Set<string>()
  const path = new Set<string>()
  const cyclic = (id: string): boolean => {
    if (path.has(id)) return true
    if (done.has(id)) return false
    path.add(id)
    const found = (children.get(id) ?? []).some(cyclic)
    path.delete(id)
    done.add(id)
    return found
  }
  return [...ids].some(cyclic) ? "prerequisite cycle" : undefined
}

// ---------------------------------------------------------------------------
// Layout — deterministic layers (PRD_DAG_TUI.md §5), assigned by Takt:
// layer 0 without prerequisites, otherwise one more than the deepest prerequisite.
// Layers are columns, flowing left to right. When the columns overflow the
// viewport width, the flow snakes: the columns split into the fewest bands that
// fit, balanced in width, and each band below flows the opposite way. Every
// edge follows that flow (PR-DTUI-6): within a band it advances to the next
// column; turning into the next band it only descends, leaving through the
// bottom of a slot nothing lies below in its column and entering through the
// top of one nothing lies above, and otherwise running down the gutter beside
// the band ends. Edges that skip layers run along a straight track shared by
// the origins that skip to the same targets, so every edge is a chain of links
// between adjacent columns. An edge touches a box only at the side it leaves
// or enters through and elsewhere keeps a blank cell from every box. A box is
// as wide as its name and sits against the side its edges enter from; unit names are bounded by contract (PR-DAG-REP-8),
// so the graph never overflows the width, and when its bands do not fit the
// height they close up and the view scrolls vertically. The links of one gap or
// gutter share a lane only when they form a complete biclique, so a shared
// trunk never draws an edge that does not exist; lanes are ordered to keep
// crossings few, and parallel lanes keep a blank column between them. Positions
// and routes depend on topology and viewport only, so a state-only update never
// moves a node (the whole Layout is reused).
// ---------------------------------------------------------------------------

type Point = readonly [x: number, y: number]

interface LayoutNode {
  readonly id: string
  readonly depth: number
  readonly x: number
  readonly y: number
  /** Box width: its name plus chrome. */
  readonly width: number
}

interface Port {
  readonly x: number
  readonly y: number
  readonly glyph: string
}

interface Viewport {
  readonly width: number
  readonly height: number
}

interface Layout {
  readonly nodes: ReadonlyMap<string, LayoutNode>
  /** One orthogonal polyline per edge: from beside the source box to beside the target box. */
  readonly paths: readonly (readonly Point[])[]
  /** Arrowhead cells, one per edge. */
  readonly heads: readonly Port[]
  readonly width: number
  readonly height: number
  /** Widest component's band count; 1 when nothing wraps. */
  readonly bands: number
  /** Node-id/kind set + edge set this layout was computed from; never lifecycle state. */
  readonly topologyKey: string
  readonly viewport: Viewport
}

// A node is a bordered box of two cells on one line, `│<glyph>│ <name> │`.
// NODE_ROWS is that line plus its two border rows; NODE_CHROME is every column
// but the name's: three borders, the glyph and one blank on each side of the name.
const NODE_ROWS = 3
const NODE_CHROME = 6
// Rows of a pass-through slot, and its minimum width in cells.
const PASS_ROWS = 1
const PASS_WIDTH = 3
// Empty rows between stacked slots of one column, between bands, and between
// disconnected components.
const SLOT_GAP = 1
const BAND_GAP = 2
const COMPONENT_GAP = 2
// Rows between bands once the graph closes up to fit the height.
const COMPACT_BAND_GAP = 1
// Columns between two parallel bus lanes: the lane, then a blank column, so two
// rails never read as one.
const LANE_PITCH = 2
// Cells of a gap besides its bus lanes: a stub before the lanes, a margin and
// the arrowhead cell after them.
const GAP_CHROME = 3
// Down/up passes of the crossing-reduction ordering.
const ORDER_SWEEPS = 4
const UNBOUNDED: Viewport = { width: Infinity, height: Infinity }

export function ordinal(a: string, b: string): number {
  if (a < b) return -1
  if (a > b) return 1
  return 0
}

/** Units and the prerequisite edges between them. */
interface Topology {
  readonly nodes: readonly DagNode[]
  readonly edges: readonly DagEdge[]
}

export function topologyKey(topology: Topology): string {
  const nodeIds = topology.nodes.map((n) => `${n.id}:${n.node_kind}`).sort(ordinal)
  const edgeKeys = topology.edges.map((e) => `${e.from}->${e.to}`).sort(ordinal)
  return `${nodeIds.join(",")}|${edgeKeys.join(",")}`
}

/** A unit a view draws: any but a withdrawn one. */
export type DrawnNode = DagNode & { readonly state: Exclude<DagNodeState, "withdrawn"> }

/** The units a view draws and the edges between them. */
interface DrawnGraph extends Topology {
  readonly nodes: readonly DrawnNode[]
}

const isDrawn = (node: DagNode): node is DrawnNode => node.state !== "withdrawn"

// graphOf is nodes with the edges that join two of them.
function graphOf(nodes: readonly DrawnNode[], edges: readonly DagEdge[]): DrawnGraph {
  const ids = new Set(nodes.map((n) => n.id))
  return { nodes, edges: edges.filter((e) => ids.has(e.from) && ids.has(e.to)) }
}

// drawnGraph is the work a view draws: a withdrawn unit was never work, so it
// leaves the graph with its edges (PR-DTUI-6).
export function drawnGraph(snapshot: DagSnapshot): DrawnGraph {
  return graphOf(snapshot.nodes.filter(isDrawn), snapshot.edges)
}

/** Width of the box holding name. */
export const boxWidth = (name: string) => Bun.stringWidth(name) + NODE_CHROME

/** The rows of the box holding glyph and name. */
export function boxLines(glyph: string, name: string): readonly [string, string, string] {
  const rule = "─".repeat(Bun.stringWidth(name) + 2)
  return [`┌─┬${rule}┐`, `│${glyph}│ ${name} │`, `└─┴${rule}┘`]
}

/**
 * The graph split by connectivity (edges read undirected): disconnected
 * graphs are separate components and are drawn apart. Ordered by smallest id,
 * members by id, so the result is deterministic. Layers are Takt's: the view
 * reads them and never recomputes them (PR-DTUI-9).
 */
function connectedComponents(graph: DrawnGraph): DrawnGraph[] {
  const neighbours = new Map<string, string[]>()
  for (const e of graph.edges) {
    neighbours.set(e.from, [...(neighbours.get(e.from) ?? []), e.to])
    neighbours.set(e.to, [...(neighbours.get(e.to) ?? []), e.from])
  }
  const byId = (a: DrawnNode, b: DrawnNode) => ordinal(a.id, b.id)
  const seen = new Set<string>()
  const components: DrawnGraph[] = []
  for (const { id } of graph.nodes.toSorted(byId)) {
    if (seen.has(id)) continue
    const members = new Set([id])
    const pending = [id]
    seen.add(id)
    for (let next = pending.pop(); next !== undefined; next = pending.pop()) {
      for (const n of neighbours.get(next) ?? []) if (!seen.has(n)) { seen.add(n); members.add(n); pending.push(n) }
    }
    components.push(graphOf(graph.nodes.filter((n) => members.has(n.id)).toSorted(byId), graph.edges))
  }
  return components
}

interface SlotBase {
  /** Column within its component: its layer less the component's first. */
  readonly column: number
  /** Links arriving from the previous column and leaving to the next one. */
  readonly inbound: Link[]
  readonly outbound: Link[]
  /** Position in its column, while the columns are ordered. */
  order: number
  /** First row, once rows are placed. */
  top: number
}

/** A node's box. */
interface BoxSlot extends SlotBase {
  readonly kind: "box"
  readonly name: string
  readonly layer: number
  /** The boxes joined to this one by an edge, whatever the edge passes through. */
  readonly prerequisites: BoxSlot[]
  readonly dependents: BoxSlot[]
}

/** Where a layer-skipping track crosses a column. */
interface PassSlot extends SlotBase {
  readonly kind: "pass"
}

type Slot = BoxSlot | PassSlot

/** A link between slots of adjacent columns. */
interface Link {
  readonly from: Slot
  readonly to: Slot
}

/** Layer-skipping edges of the origins that skip to the same targets: one straight track through the columns between. */
interface Track {
  readonly origins: BoxSlot[]
  readonly targets: readonly BoxSlot[]
  readonly passes: PassSlot[]
}

const slotRows = (slot: Slot) => (slot.kind === "box" ? NODE_ROWS : PASS_ROWS)
const slotWidth = (slot: Slot) => (slot.kind === "box" ? boxWidth(slot.name) : PASS_WIDTH)
// centreRow is the row a slot's edges run along: the middle of its box, or its track.
const centreRow = (slot: Slot) => slot.top + (slotRows(slot) - 1) / 2

/** A link's run along a lane, from where it joins the lane to where it leaves it. */
interface Run {
  readonly link: Link
  readonly from: number
  readonly to: number
}

interface LaneGroup {
  readonly targets: readonly Slot[]
  readonly runs: Run[]
  lo: number
  hi: number
  lane: number
}

interface Lanes {
  readonly lanes: ReadonlyMap<Link, number>
  readonly count: number
}

// assignLanes gives every link of one gap or gutter the lane it runs along.
// Sources with the same target set share a lane: a fork reads as one trunk and
// a complete biclique as one bar, and since every source on a lane reaches
// every target on it, a shared lane never draws an edge that does not exist.
// Lanes are ordered so as few runs as possible cross a lane they do not belong
// to; groups share a lane only when a free row separates them. In a gap sources
// sit before the lanes and targets after them; in a gutter both sit before them.
function assignLanes(runs: readonly Run[], gutter: boolean): Lanes {
  const groups: LaneGroup[] = []
  for (const run of runs) {
    const targets = runs.filter((r) => r.link.from === run.link.from).map((r) => r.link.to)
    const same = groups.find((g) => g.targets.length === targets.length && targets.every((t) => g.targets.includes(t)))
    const group = same ?? { targets, runs: [], lo: Infinity, hi: -Infinity, lane: 0 }
    if (!same) groups.push(group)
    group.runs.push(run)
    group.lo = Math.min(group.lo, run.from, run.to)
    group.hi = Math.max(group.hi, run.from, run.to)
  }
  const inside = (g: LaneGroup, row: number) => g.lo <= row && row <= g.hi
  const sourceRows = (g: LaneGroup) => new Map(g.runs.map((r) => [r.link.from, r.from])).values()
  const targetRows = (g: LaneGroup) => new Map(g.runs.map((r) => [r.link.to, r.to]))
  // Runs of `far` that cross the lane of `near` when `near` lies closer to the sources.
  const crossings = (near: LaneGroup, far: LaneGroup) => {
    const [ends, lane] = gutter ? [far, near] : [near, far]
    let count = [...sourceRows(far)].filter((row) => inside(near, row)).length
    for (const [target, row] of targetRows(ends)) if (inside(lane, row) && !lane.targets.includes(target)) count++
    return count
  }
  groups.sort((x, y) => crossings(x, y) - crossings(y, x) || x.lo - y.lo || x.hi - y.hi)
  groups.forEach((group, i) => {
    const touching = groups.slice(0, i).filter((g) => g.lo <= group.hi + 1 && group.lo <= g.hi + 1)
    group.lane = Math.max(-1, ...touching.map((g) => g.lane)) + 1
  })
  return {
    lanes: new Map(groups.flatMap((g) => g.runs.map((r) => [r.link, g.lane] as const))),
    count: groups.length === 0 ? 0 : Math.max(...groups.map((g) => g.lane)) + 1,
  }
}

/** The lane lanes gave link: every link routed along lanes has one. */
function laneIn(lanes: Lanes, link: Link): number {
  const lane = lanes.lanes.get(link)
  if (lane === undefined) throw new Error("invalid DAG layout: a link has no lane")
  return lane
}

interface Piece {
  readonly nodes: LayoutNode[]
  readonly paths: Point[][]
  readonly heads: Port[]
  readonly width: number
  readonly height: number
  readonly bands: number
}

/** How tightly a layout is drawn: rows between bands. */
interface Sizing {
  readonly bandGap: number
}

/** One connected component as columns of slots. */
interface SlotGraph {
  readonly columns: Slot[][]
  readonly boxes: readonly BoxSlot[]
  readonly links: readonly Link[]
  /** Every edge as the links it runs through, box to box. */
  readonly traces: readonly (readonly Link[])[]
  readonly tracks: readonly Track[]
}

// slotGraph turns one component's nodes and edges into slots. Origins whose
// layer-skipping edges reach the same targets share one track: one
// pass-through per skipped column, so they run as one trunk instead of one
// slot per edge and column. A link joins two slots once, however many edges
// run through it.
function slotGraph({ nodes, edges }: DrawnGraph): SlotGraph {
  const first = Math.min(...nodes.map((n) => n.layer))
  const boxes = nodes.map((n): BoxSlot => ({
    kind: "box", name: n.id, layer: n.layer, column: n.layer - first,
    inbound: [], outbound: [], prerequisites: [], dependents: [], order: 0, top: 0,
  })).sort((a, b) => ordinal(a.name, b.name))
  const byName = new Map(boxes.map((b) => [b.name, b]))
  const box = (name: string): BoxSlot => {
    const found = byName.get(name)
    if (!found) throw new Error(`invalid DAG layout: edge names unknown node ${name}`)
    return found
  }
  for (const e of edges) {
    const [from, to] = [box(e.from), box(e.to)]
    from.dependents.push(to)
    to.prerequisites.push(from)
  }

  const tracks = new Map<string, Track>()
  for (const origin of boxes.toSorted((a, b) => a.column - b.column || ordinal(a.name, b.name))) {
    const targets = origin.dependents.filter((d) => d.column > origin.column + 1).sort((a, b) => ordinal(a.name, b.name))
    if (targets.length === 0) continue
    const key = targets.map((t) => t.name).join("\n")
    const track = tracks.get(key) ?? { origins: [], targets, passes: [] }
    track.origins.push(origin)
    tracks.set(key, track)
  }
  // The first origin is the leftmost: the track crosses every column between it and its farthest target.
  for (const track of tracks.values()) {
    const last = Math.max(...track.targets.map((t) => t.column))
    for (let column = track.origins[0].column + 1; column < last; column++) {
      track.passes.push({ kind: "pass", column, inbound: [], outbound: [], order: 0, top: 0 })
    }
  }

  const links: Link[] = []
  const link = (from: Slot, to: Slot): Link => {
    const known = from.outbound.find((l) => l.to === to)
    if (known) return known
    const made = { from, to }
    from.outbound.push(made)
    to.inbound.push(made)
    links.push(made)
    return made
  }
  const chain = (slots: readonly Slot[]) => slots.slice(1).map((to, i) => link(slots[i], to))
  const traces = [
    ...boxes.flatMap((from) => from.dependents.filter((to) => to.column === from.column + 1).map((to) => chain([from, to]))),
    ...[...tracks.values()].flatMap((track) => track.origins.flatMap((origin) => track.targets.map((target) =>
      chain([origin, ...track.passes.filter((p) => p.column > origin.column && p.column < target.column), target])))),
  ]

  const columns: Slot[][] = Array.from({ length: Math.max(...boxes.map((b) => b.column)) + 1 }, () => [])
  for (const slot of [...boxes, ...[...tracks.values()].flatMap((t) => t.passes)]) columns[slot.column].push(slot)
  return { columns, boxes, links, traces, tracks: [...tracks.values()] }
}

// nearestFree is the row closest to home that free accepts, trying the row below before the one above.
function nearestFree(home: number, free: (row: number) => boolean): number {
  if (free(home)) return home
  for (let distance = 1; ; distance++) {
    if (free(home + distance)) return home + distance
    if (free(home - distance)) return home - distance
  }
}

// orderColumns orders each column by the mean position of its neighbours,
// sweeping both ways, so edges cross as little as the layering allows.
function orderColumns(columns: Slot[][]): void {
  const reindex = (column: readonly Slot[]) => column.forEach((slot, i) => { slot.order = i })
  columns.forEach(reindex)
  const reorder = (column: Slot[], neighbours: (slot: Slot) => readonly Slot[]) => {
    const mean = (slot: Slot) => {
      const around = neighbours(slot)
      return around.length === 0 ? slot.order : around.reduce((sum, n) => sum + n.order, 0) / around.length
    }
    const keyed = column.map((slot) => ({ slot, key: mean(slot) }))
    keyed.sort((x, y) => x.key - y.key || x.slot.order - y.slot.order)
    keyed.forEach(({ slot }, i) => { column[i] = slot })
    reindex(column)
  }
  for (let sweep = 0; sweep < ORDER_SWEEPS; sweep++) {
    for (const column of columns.slice(1)) reorder(column, (slot) => slot.inbound.map((l) => l.from))
    for (const column of columns.slice(0, -1).toReversed()) reorder(column, (slot) => slot.outbound.map((l) => l.to))
  }
}

// placeRows sets each box on the mean row of the boxes its edges join, pushed
// down just enough to clear the box above it.
function placeRows(columns: readonly Slot[][]): void {
  const place = (column: readonly Slot[], around: (box: BoxSlot) => readonly BoxSlot[]) => {
    let floor = 0
    for (const slot of column) {
      if (slot.kind !== "box") continue
      const near = around(slot)
      const want = near.length === 0 ? slot.top : Math.round(near.reduce((sum, n) => sum + centreRow(n), 0) / near.length - (NODE_ROWS - 1) / 2)
      slot.top = Math.max(want, floor)
      floor = slot.top + NODE_ROWS + SLOT_GAP
    }
  }
  for (const column of columns) place(column, (box) => box.prerequisites)
  for (const column of columns.slice(0, -1).toReversed()) place(column, (box) => box.dependents)
  for (const column of columns) place(column, (box) => box.prerequisites)
}

// placeTracks runs each track along one row through every column it skips, so
// its edges bend only where they leave and where they arrive. The row keeps a
// blank row from every box it passes, so a track never runs along a border; it
// clears every other track from its first origin's column to its farthest
// target's, and the edge rows of the other boxes at both ends. The first
// origin's row is tried first, then the nearest rows. Shorter tracks go first.
function placeTracks(columns: readonly Slot[][], tracks: readonly Track[]): void {
  const span = (track: Track) => ({ first: track.origins[0].column, last: Math.max(...track.targets.map((t) => t.column)) })
  const length = (track: Track) => span(track).last - span(track).first
  const key = (track: Track) => track.targets.map((t) => t.name).join("\n")
  const taken = columns.map(() => new Set<number>())
  for (const track of tracks.toSorted((x, y) => length(x) - length(y) || ordinal(key(x), key(y)))) {
    const { first, last } = span(track)
    const free = (row: number) => {
      for (let c = first; c <= last; c++) {
        if (taken[c].has(row)) return false
        const boxes = columns[c].filter((s): s is BoxSlot => s.kind === "box")
        if (c === first || c === last) {
          const owner = boxes.find((b) => centreRow(b) === row)
          if (owner && !track.origins.includes(owner) && !track.targets.includes(owner)) return false
        } else if (boxes.some((b) => b.top - SLOT_GAP <= row && row < b.top + NODE_ROWS + SLOT_GAP)) return false
      }
      return true
    }
    const row = nearestFree(centreRow(track.origins[0]), free)
    for (let c = first; c <= last; c++) taken[c].add(row)
    for (const pass of track.passes) pass.top = row
  }
}

type Across = "left" | "right"
/** The side a link leaves its slot through, and the side it enters the next one through. */
type Exit = Across | "bottom"
type Entry = Across | "top"
/** The arrowhead beside the side an edge enters a box through. */
const HEAD: Record<Entry, string> = { left: "▶", right: "◀", top: "▼" }

// layoutComponent lays one connected component out as columns, wrapped into
// bands that, gutters included, fit width whenever one column per band does.
function layoutComponent(component: DrawnGraph, width: number, sizing: Sizing): Piece {
  const { columns, boxes, links, traces, tracks } = slotGraph(component)
  orderColumns(columns)
  placeRows(columns)
  placeTracks(columns, tracks)

  const colWidth = columns.map((column) => Math.max(PASS_WIDTH, ...column.map(slotWidth)))
  // Lanes of every gap between adjacent columns, as if the flow never wrapped.
  const gapRuns = (c: number) => links.filter((l) => l.from.column === c).map((link) => ({ link, from: centreRow(link.from), to: centreRow(link.to) }))
  const gaps = columns.slice(1).map((_, c) => assignLanes(gapRuns(c), false))
  const gapWidth = gaps.map((gap) => Math.max(1, gap.count) * LANE_PITCH + GAP_CHROME)

  // Wrap: a column that would overflow the budget starts the next band.
  const route = (budget: number): Piece => {
    const bands = wrapBands(colWidth, gapWidth, budget)
    const bandOf: number[] = []
    bands.forEach((cs, band) => cs.forEach((c) => { bandOf[c] = band }))
    const band = (slot: Slot) => bandOf[slot.column]
    // Even bands flow right, odd ones left.
    const flow = (slot: Slot) => (band(slot) % 2 === 0 ? 1 : -1)
    const across = (direction: number): Across => (direction > 0 ? "right" : "left")
    const turns = (link: Link) => band(link.from) !== band(link.to)

    // The sides each link uses, chosen by where it goes: toward the next column
    // within a band. Turning into the next band, it enters the top of a box
    // nothing lies above in its column, otherwise the side toward the band end,
    // where the flow turns; it leaves the bottom of a box nothing lies below
    // only when it drops straight into such a top, otherwise the side toward
    // the band end. A pass-through has no box: its track only runs across.
    const lowest = (slot: Slot) => slot.kind === "box" && columns[slot.column].every((o) => o === slot || o.top < slot.top)
    const topmost = (slot: Slot) => slot.kind === "box" && columns[slot.column].every((o) => o === slot || o.top > slot.top)
    const entryOf = (link: Link): Entry => (turns(link) && topmost(link.to) ? "top" : across(-flow(link.to)))
    const exitOf = (link: Link): Exit => (entryOf(link) === "top" && lowest(link.from) ? "bottom" : across(flow(link.from)))
    const turning = links.filter(turns)

    // Below a band, a corridor row carries every link entering the top of the
    // next band as one trunk into its arrowhead, with a blank row above it and
    // the arrowhead row below it.
    const corridorUsed = bands.map((_, b) => turning.some((l) => band(l.from) === b && entryOf(l) === "top"))
    const shift: number[] = []
    const corridor: number[] = []
    let bottom = 0
    bands.forEach((cs, b) => {
      const slots = cs.flatMap((c) => columns[c])
      const lo = Math.min(...slots.map((s) => s.top))
      const hi = Math.max(...slots.map((s) => s.top + slotRows(s)))
      shift[b] = bottom - lo
      corridor[b] = bottom + hi - lo + 1
      bottom += hi - lo + (corridorUsed[b] ? Math.max(sizing.bandGap, 3) : sizing.bandGap)
    })
    const height = bottom - sizing.bandGap
    const rowOf = (slot: Slot) => centreRow(slot) + shift[band(slot)]
    // The row a turning link reaches its entry along: the corridor for a top, beside the slot for a side.
    const intoRow = (link: Link) => (entryOf(link) === "top" ? corridor[band(link.from)] : rowOf(link.to))

    const gutter = (side: number) =>
      assignLanes(turning.filter((l) => flow(l.from) === side && exitOf(l) !== "bottom").map((link) => ({ link, from: rowOf(link.from), to: intoRow(link) })), true)
    const [rightGutter, leftGutter] = [gutter(1), gutter(-1)]

    // A gutter keeps a blank column between its outermost lane and the edge.
    const leftMargin = leftGutter.count > 0 ? 3 + (leftGutter.count - 1) * LANE_PITCH + 1 : 0
    const bandWidth = bands.map((cs) => cs.reduce((sum, c, i) => sum + colWidth[c] + (i < cs.length - 1 ? gapWidth[c] : 0), 0))
    const widest = Math.max(...bandWidth)
    const right = leftMargin + widest
    // Every band is spread to the widest one's width, so the gutters sit beside the band ends.
    const spread = [...gapWidth]
    bands.forEach((cs, band) => {
      const gaps = cs.length - 1
      const extra = widest - bandWidth[band]
      for (let i = 0; i < gaps; i++) spread[cs[i]] += Math.floor(extra / gaps) + (i < extra % gaps ? 1 : 0)
    })
    const colX = columnX(bands, colWidth, spread, leftMargin, right)

    // A box sits against the side its edges enter from; a pass-through spans its column.
    const widthOf = (slot: Slot) => (slot.kind === "box" ? slotWidth(slot) : colWidth[slot.column])
    const leftOf = (slot: Slot) => (flow(slot) > 0 ? colX[slot.column] : colX[slot.column] + colWidth[slot.column] - widthOf(slot))
    // The cell beside a side of a slot where a link attaches: beside a box, or on a pass-through's own track.
    const portCell = (slot: Slot, side: Exit | Entry): Point => {
      const [x, w, y, top] = [leftOf(slot), widthOf(slot), rowOf(slot), slot.top + shift[band(slot)]]
      switch (side) {
        case "left":
          return [x - 1, y]
        case "right":
          return [x + w, y]
        case "top":
          return [x + Math.floor(w / 2), slot.kind === "box" ? top - 1 : y]
        case "bottom":
          return [x + Math.floor(w / 2), slot.kind === "box" ? top + NODE_ROWS : y]
      }
    }
    // The lane a link runs along: in the gap after its column, or in the gutter beside the band end.
    const laneX = (link: Link) => {
      const c = link.from.column
      if (!turns(link)) {
        const edge = flow(link.from) > 0 ? colX[c] + colWidth[c] : colX[c] - 1
        return edge + flow(link.from) * (2 + laneIn(gaps[c], link) * LANE_PITCH)
      }
      return flow(link.from) > 0 ? right + 2 + laneIn(rightGutter, link) * LANE_PITCH : leftMargin - 3 - laneIn(leftGutter, link) * LANE_PITCH
    }
    // The cells of a link from its exit to its entry: from a bottom straight
    // along the corridor into a top, otherwise along its lane.
    const leg = (link: Link): Point[] => {
      const [a, b] = [portCell(link.from, exitOf(link)), portCell(link.to, entryOf(link))]
      const into: Point = [b[0], turns(link) ? intoRow(link) : b[1]]
      if (exitOf(link) === "bottom") return [a, [a[0], into[1]], into, b]
      const lane = laneX(link)
      return [a, [lane, a[1]], [lane, into[1]], into, b]
    }
    const path = (trace: readonly Link[]) =>
      trace.flatMap(leg).filter((p, i, all) => i === 0 || p[0] !== all[i - 1][0] || p[1] !== all[i - 1][1])
    const head = (trace: readonly Link[]): Port => {
      const last = trace[trace.length - 1]
      const [x, y] = portCell(last.to, entryOf(last))
      return { x, y, glyph: HEAD[entryOf(last)] }
    }

    return {
      nodes: boxes.map((box) => ({ id: box.name, depth: box.layer, x: leftOf(box), y: box.top + shift[band(box)], width: widthOf(box) })),
      paths: traces.map(path),
      heads: traces.map(head),
      width: right + (rightGutter.count > 0 ? 4 + (rightGutter.count - 1) * LANE_PITCH : 0),
      height,
      bands: bands.length,
    }
  }
  // Narrow the budget by what the gutters spill over the width, until the band
  // fits or every band already holds a single column.
  let budget = width
  for (;;) {
    const piece = route(budget)
    const over = piece.width - width
    if (over <= 0 || piece.bands === columns.length) return piece
    budget -= over
  }
}

// wrapBands groups the columns into the fewest bands no wider than budget,
// then narrows the limit to the smallest one that still needs no more bands and
// evens the bands out: they come out the same width, not one full and one nearly
// empty.
function wrapBands(colWidth: readonly number[], gapWidth: readonly number[], budget: number): number[][] {
  const fill = (limit: number): number[][] => {
    const bands: number[][] = []
    for (let start = 0; start < colWidth.length; ) {
      let end = start
      for (let used = colWidth[start]; end + 1 < colWidth.length && used + gapWidth[end] + colWidth[end + 1] <= limit; end++) used += gapWidth[end] + colWidth[end + 1]
      bands.push(Array.from({ length: end - start + 1 }, (_, i) => start + i))
      start = end + 1
    }
    return bands
  }
  const width = (cs: readonly number[]) => cs.reduce((sum, c, i) => sum + colWidth[c] + (i < cs.length - 1 ? gapWidth[c] : 0), 0)
  const all = Array.from({ length: colWidth.length }, (_, i) => i)
  if (width(all) <= budget) return [all]
  const count = fill(budget).length
  let [lo, hi] = [Math.min(Math.max(...colWidth), budget), budget]
  while (lo < hi) {
    const mid = Math.floor((lo + hi) / 2)
    if (fill(mid).length <= count) hi = mid
    else lo = mid + 1
  }
  const bands = fill(lo)
  // The greedy fill leaves the last band short: move a trailing column on while that narrows the wider of the two.
  for (let moved = true; moved; ) {
    moved = false
    for (let j = 0; j + 1 < bands.length; j++) {
      if (bands[j].length < 2) continue
      const [kept, shifted] = [bands[j].slice(0, -1), [...bands[j].slice(-1), ...bands[j + 1]]]
      if (Math.max(width(kept), width(shifted)) < Math.max(width(bands[j]), width(bands[j + 1]))) {
        bands[j] = kept
        bands[j + 1] = shifted
        moved = true
      }
    }
  }
  return bands
}

// columnX places every column: even bands run rightwards from leftMargin, odd
// ones leftwards from right.
function columnX(bands: readonly number[][], colWidth: readonly number[], gapWidth: readonly number[], leftMargin: number, right: number): number[] {
  const colX: number[] = []
  bands.forEach((cs, band) => {
    if (band % 2 === 0) {
      let x = leftMargin
      cs.forEach((c, i) => {
        colX[c] = x
        if (i < cs.length - 1) x += colWidth[c] + gapWidth[c]
      })
    } else {
      let edge = right
      cs.forEach((c, i) => {
        colX[c] = edge - colWidth[c]
        if (i < cs.length - 1) edge = colX[c] - gapWidth[c]
      })
    }
  })
  return colX
}

/** A layout before it is keyed to the topology and viewport it was drawn for. */
type Drawn = Omit<Layout, "topologyKey" | "viewport">

// buildLayout stacks the graph's connected components top to bottom, each
// laid out in bands no wider than width and drawn as tightly as sizing says.
function buildLayout(graph: DrawnGraph, width: number, sizing: Sizing): Drawn {
  const nodes = new Map<string, LayoutNode>()
  const paths: Point[][] = []
  const heads: Port[] = []
  let y = 0
  let maxWidth = 0
  let bands = 1
  for (const component of connectedComponents(graph)) {
    const piece = layoutComponent(component, width, sizing)
    for (const node of piece.nodes) nodes.set(node.id, { ...node, y: node.y + y })
    for (const path of piece.paths) paths.push(path.map(([px, py]): Point => [px, py + y]))
    for (const head of piece.heads) heads.push({ ...head, y: head.y + y })
    y += piece.height + COMPONENT_GAP
    maxWidth = Math.max(maxWidth, piece.width)
    bands = Math.max(bands, piece.bands)
  }
  return { nodes, paths, heads, width: maxWidth, height: Math.max(0, y - COMPONENT_GAP), bands }
}

// computeLayout wraps into bands that fit the viewport width. While the bands
// do not fit its height it closes them up; past that the view scrolls
// vertically. Names are never cut.
export function computeLayout(snapshot: DagSnapshot, previous?: Layout, viewport: Viewport = UNBOUNDED): Layout {
  const drawn = drawnGraph(snapshot)
  const key = topologyKey(drawn)
  if (previous?.topologyKey === key && previous.viewport.width === viewport.width && previous.viewport.height === viewport.height) return previous
  return { ...fitLayout(drawn, viewport), topologyKey: key, viewport }
}

/** Where the layout placed a drawn unit: it places every one. */
function placeOf(layout: Layout, id: string): LayoutNode {
  const at = layout.nodes.get(id)
  if (!at) throw new Error(`invalid DAG layout: unit ${id} has no place`)
  return at
}

function fitLayout(graph: DrawnGraph, viewport: Viewport): Drawn {
  const natural = buildLayout(graph, viewport.width, { bandGap: BAND_GAP })
  return natural.height <= viewport.height ? natural : buildLayout(graph, viewport.width, { bandGap: COMPACT_BAND_GAP })
}

const UP = 1
const DOWN = 2
const LEFT = 4
const RIGHT = 8

const LINE_GLYPHS: Record<number, string> = {
  0: " ",
  [UP]: "│", [DOWN]: "│", [UP | DOWN]: "│",
  [LEFT]: "─", [RIGHT]: "─", [LEFT | RIGHT]: "─",
  [DOWN | RIGHT]: "┌", [DOWN | LEFT]: "┐", [UP | RIGHT]: "└", [UP | LEFT]: "┘",
  [UP | DOWN | RIGHT]: "├", [UP | DOWN | LEFT]: "┤",
  [LEFT | RIGHT | DOWN]: "┬", [LEFT | RIGHT | UP]: "┴",
  [UP | DOWN | LEFT | RIGHT]: "┼",
}

// Glyph of a cell where one edge runs straight across another's vertical: the
// vertical goes over, so it never reads as a junction (┼ is reserved for edges
// that really meet).
const CROSSING_GLYPH = "╫"

/** Rasterizes every edge path into text rows; cells record their links. */
export function edgeRows(layout: Layout): string[] {
  const at = (x: number, y: number) => y * layout.width + x
  const mask = new Array<number>(layout.height * layout.width).fill(0)
  // How each edge passes a cell, so a crossing tells apart from a junction.
  const passes = new Map<number, number[]>()
  for (const path of layout.paths) {
    if (path.length === 0) throw new Error("invalid DAG layout: empty edge path")
    const own = new Map<number, number>()
    const link = ([ax, ay]: Point, [bx, by]: Point) => {
      for (let y = Math.min(ay, by); y < Math.max(ay, by); y++) {
        own.set(at(ax, y), (own.get(at(ax, y)) ?? 0) | DOWN)
        own.set(at(ax, y + 1), (own.get(at(ax, y + 1)) ?? 0) | UP)
      }
      for (let x = Math.min(ax, bx); x < Math.max(ax, bx); x++) {
        own.set(at(x, ay), (own.get(at(x, ay)) ?? 0) | RIGHT)
        own.set(at(x + 1, ay), (own.get(at(x + 1, ay)) ?? 0) | LEFT)
      }
    }
    for (let i = 1; i < path.length; i++) link(path[i - 1], path[i])
    for (const [cell, m] of own) {
      mask[cell] |= m
      passes.set(cell, [...(passes.get(cell) ?? []), m])
    }
  }
  const glyphAt = (cell: number) => {
    const through = passes.get(cell) ?? []
    const straight = through.every((m) => m === (UP | DOWN) || m === (LEFT | RIGHT))
    if (mask[cell] === (UP | DOWN | LEFT | RIGHT) && straight) return CROSSING_GLYPH
    return LINE_GLYPHS[mask[cell]]
  }
  return Array.from({ length: layout.height }, (_, y) => Array.from({ length: layout.width }, (_, x) => glyphAt(at(x, y))).join(""))
}

/** Where edges meet their boxes: the arrowhead beside the target, in the flow's direction. */
export function edgePorts(layout: Layout): Port[] {
  return [...layout.heads]
}

// ---------------------------------------------------------------------------
// Node glyphs (PRD_DAG_TUI.md §4 legend, §6 state table). The §6
// table also names `suspended`, `pending termination` and `interrupted`,
// which the §4 legend has no glyph for; each gets its own so no in-flight or
// settled distinction collapses into another.
// ---------------------------------------------------------------------------

// One glyph per state the view tells apart; the counts line and the legend
// read the same table.
const GLYPH = {
  done: "✓", failed: "✗", backtracked: "↩", interrupted: "⊘", planned: "◌",
  admitted: "○", uncertain: "!", suspended: "⏸", stopping: "↯", unmonitored: "◐", running: "●",
} as const

function settledGlyph(outcome: DagOutcome | undefined): string {
  switch (outcome) {
    case "failed":
      return GLYPH.failed
    case "backtracked":
      return GLYPH.backtracked
    case "interrupted":
      return GLYPH.interrupted
    default:
      return GLYPH.done
  }
}

export function glyphFor(node: DrawnNode): string {
  switch (node.state) {
    case "planned":
      return GLYPH.planned
    case "settled":
      return settledGlyph(node.outcome)
    case "in_flight":
      switch (node.flight) {
        case "pending_launch":
          return GLYPH.admitted
        case "uncertain":
          return GLYPH.uncertain
        case "suspended":
          return GLYPH.suspended
        case "cancellation_pending":
          return GLYPH.stopping
        case "unmonitored":
          return GLYPH.unmonitored
        default:
          return GLYPH.running
      }
  }
}

// An activity is a node: the state glyph every node uses, its identity as its
// name. Lost capture reads as the unmonitored unit does, in flight and not failed.
export function activityGlyph(activity: DagActivity): string {
  if (activity.state === "settled") return settledGlyph(activity.outcome)
  return activity.flight === "unmonitored" ? GLYPH.unmonitored : GLYPH.running
}

/** Where an activity box sits in the lane below the graph. */
interface ActivitySlot {
  readonly left: number
  readonly row: number
  readonly width: number
  /** The activity's name, cut to fit the box. */
  readonly name: string
}

/**
 * Places the activity boxes left to right, one cell apart, starting a new row
 * where the next box would pass width. A box wider than width is cut to it.
 */
export function packActivities(activities: readonly DagActivity[], width: number): ActivitySlot[] {
  const slots: ActivitySlot[] = []
  let left = 0
  let row = 0
  for (const activity of activities) {
    const cells = Math.min(boxWidth(activity.activity_id), width)
    if (left > 0 && left + cells > width) {
      row++
      left = 0
    }
    slots.push({ left, row, width: cells, name: fit(activity.activity_id, cells - NODE_CHROME) })
    left += cells + 1
  }
  return slots
}

// The glyph legend, shown on demand in the route view (`?`): the states a drawn node can be in.
const LEGEND_LINES = [
  `${GLYPH.done} done   ${GLYPH.running} in flight   ${GLYPH.planned} planned   ${GLYPH.admitted} admitted   ${GLYPH.uncertain} uncertain`,
  `${GLYPH.unmonitored} unmonitored   ${GLYPH.failed} failed   ${GLYPH.backtracked} backtracked`,
  `${GLYPH.suspended} suspended   ${GLYPH.stopping} stopping   ${GLYPH.interrupted} interrupted`,
]

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

// Columns and rows the route spends outside the graph: the scrollbar and
// margin, then the header, problem line, spacer, scrollbar row, legend and prompt.
const ROUTE_SIDE_COLUMNS = 2
const ROUTE_CHROME_ROWS = 12
// Rows of one activity lane row: a spacer, then the bordered box.
const ACTIVITY_ROW_ROWS = 4

/** A node box with its top-left corner at left, top. */
function NodeBox(props: { readonly left: number; readonly top: number; readonly glyph: string; readonly name: string }) {
  return (
    <For each={boxLines(props.glyph, props.name)}>
      {(line, row) => (
        <text position="absolute" left={props.left} top={props.top + row()}>
          {line}
        </text>
      )}
    </For>
  )
}

/** Display health: the snapshot's own capture, or the view's failure state. */
type Health = "waiting" | "confirmed" | "stale" | "unavailable" | "invalid"

/**
 * The graph: layers left to right, every node one typed, state-bearing box.
 * On the route it fills the space above the prompt, fitted to its width, and
 * scrolls vertically with the keyboard focused on it. In the sidebar it is as tall as the graph, since
 * the host's sidebar scrolls vertically, and scrolls sideways within the
 * column; the extra row is the horizontal scrollbar.
 */
export function GraphView(props: { readonly snapshot: DagSnapshot; readonly mode: "route" | "sidebar" }) {
  // Solid runs a component body once per mount, so this closure carries the
  // last layout across reactive re-runs and a state-only update reuses it.
  let previous: Layout | undefined
  const activities = createMemo(() => props.snapshot.activities)
  const terminal = props.mode === "route" ? useTerminalDimensions() : undefined
  // The route fits the graph and the activity lane to the terminal width; the
  // sidebar's are never wrapped.
  const routeWidth = () => {
    const size = terminal?.()
    return size ? size.width - ROUTE_SIDE_COLUMNS : Infinity
  }
  const activitySlots = createMemo(() => packActivities(activities(), routeWidth()))
  const activityHeight = createMemo(() => activitySlots().reduce((rows, slot) => Math.max(rows, slot.row + 1), 0) * ACTIVITY_ROW_ROWS)
  const viewport = (): Viewport => {
    const size = terminal?.()
    // The activity lane never enters the fit: a change in activities must not
    // move a unit (PR-DTUI-11); the lane scrolls below the graph instead.
    return size ? { width: routeWidth(), height: size.height - ROUTE_CHROME_ROWS } : UNBOUNDED
  }
  const layout = createMemo(() => (previous = computeLayout(props.snapshot, previous, viewport())))
  // Each drawn node with the place the layout gave it.
  const placed = createMemo(() => drawnGraph(props.snapshot).nodes.map((node) => ({ node, at: placeOf(layout(), node.id) })))
  const hasNodes = createMemo(() => layout().nodes.size > 0)
  const rows = createMemo(() => (hasNodes() ? edgeRows(layout()) : []))
  const ports = createMemo(() => edgePorts(layout()))
  const workHeight = createMemo(() => (hasNodes() ? layout().height : 0))
  const activityLaneWidth = createMemo(() => activitySlots().reduce((width, slot) => Math.max(width, slot.left + slot.width), 0))
  const graphWidth = createMemo(() =>
    Math.max(hasNodes() ? layout().width : 0, activityLaneWidth()),
  )
  const graphHeight = createMemo(() => workHeight() + activityHeight())
  const scrollport = () =>
    props.mode === "route"
      ? { scrollY: true, focused: true, flexGrow: 1, minWidth: 0, minHeight: 0 }
      : { scrollY: false, height: graphHeight() + 1, flexShrink: 0 }

  return (
    <scrollbox scrollX width="100%" {...scrollport()} horizontalScrollbarOptions={{ showArrows: true }}>
      {/* flexShrink 0: the graph keeps its size and overflows the viewport
          instead of being squeezed into it. */}
      <box width={graphWidth()} height={graphHeight()} flexShrink={0}>
        <For each={rows()}>
          {(row, y) => (
            <text position="absolute" left={0} top={y()}>
              {row}
            </text>
          )}
        </For>
        <For each={placed()}>
          {({ node, at }) => <NodeBox left={at.x} top={at.y} glyph={glyphFor(node)} name={node.id} />}
        </For>
        <For each={ports()}>
          {(port) => (
            <text position="absolute" left={port.x} top={port.y} zIndex={1}>
              {port.glyph}
            </text>
          )}
        </For>
        <For each={activities()}>
          {(activity, index) => {
            const slot = () => activitySlots()[index()]
            return (
              <NodeBox
                left={slot().left}
                top={workHeight() + 1 + slot().row * ACTIVITY_ROW_ROWS}
                glyph={activityGlyph(activity)}
                name={slot().name}
              />
            )
          }}
        </For>
      </box>
    </scrollbox>
  )
}

/** Work units or activities: an activity-only snapshot still has a lane to draw. */
export function hasContent(snapshot: DagSnapshot): boolean {
  return drawnGraph(snapshot).nodes.length > 0 || snapshot.activities.length > 0
}

// countsLine says where the delegation stands: every count is Takt's, and a
// state with no unit is left out. The route words it; the sidebar spends
// fewer columns on the same counts (sidebarDetail).
export function countsLine(counts: DagCounts): string {
  const parts: [number, string][] = [
    [counts.completed, "done"], [counts.in_flight, "in flight"], [counts.planned, "planned"],
    [counts.failed, "failed"], [counts.backtracked, "backtracked"], [counts.interrupted, "interrupted"],
  ]
  return parts.filter(([n]) => n > 0).map(([n, word]) => `${n} ${word}`).join(" · ")
}

// headerLine names the view, then whatever needs attention: a capture that is
// not current, otherwise where the delegation stands. Projection and plan
// revisions are internal and stay out.
export function headerLine(snapshot: DagSnapshot | undefined, health: Health): string {
  const capture = snapshot && health === "confirmed" ? snapshot.capture : health
  if (capture !== "current") return `Takt DAG · ${capture}`
  const line = snapshot ? countsLine(snapshot.counts) : ""
  return line === "" ? "Takt DAG" : `Takt DAG · ${line}`
}

// The OpenCode v2 sidebar is 42 columns wide (SESSION_SIDEBAR_WIDTH) less its
// padding, leaving 37 usable columns; the host fixes it and never passes it
// to the slot. Every sidebar row fits it, so nothing ever wraps.
const SIDEBAR_COLUMNS = 37
// Graph lanes the sidebar gutter may use, two columns each. A layer with more
// units than this is drawn as one grouped row instead.
const MAX_SIDEBAR_LANES = 3
// Rows the sidebar graph may use. It is pinned outside the host's scrollbox,
// so an unbounded graph would squeeze the scrolling sections to nothing.
const MAX_SIDEBAR_ROWS = 16
// Gutter cell of a grouped layer row; its members' own glyphs follow it.
const GROUP_GLYPH = "≡"
// Gutter marker of a unit that alone follows the unit above it, and the
// indent per further link of the same chain.
const CHAIN_GLYPH = "└─"
const CHAIN_INDENT = "  "
// Deepest indent a chain reaches, in CHAIN_INDENT steps; further links keep
// it, so a long chain still fits SIDEBAR_COLUMNS.
const MAX_CHAIN_INDENT = 4

export type SidebarTone = "muted" | "error"
export interface SidebarRow {
  readonly text: string
  readonly tone?: SidebarTone
}

/** Cuts text to width terminal cells, marking the cut with an ellipsis. */
export function fit(text: string, width: number): string {
  if (Bun.stringWidth(text) <= width) return text
  let kept = ""
  for (const char of text) {
    if (Bun.stringWidth(kept + char) > width - 1) break
    kept += char
  }
  return `${kept}…`
}

/**
 * What follows the sidebar's bold title: a capture that is not current,
 * otherwise Takt's counts as glyph and number, the same counts the route words.
 */
export function sidebarDetail(snapshot: DagSnapshot | undefined, health: Health): string {
  const capture = snapshot && health === "confirmed" ? snapshot.capture : health
  if (capture !== "current") return capture
  if (!snapshot) return ""
  const { counts } = snapshot
  const parts: [string, number][] = [
    [GLYPH.done, counts.completed], [GLYPH.running, counts.in_flight], [GLYPH.planned, counts.planned],
    [GLYPH.failed, counts.failed], [GLYPH.backtracked, counts.backtracked], [GLYPH.interrupted, counts.interrupted],
  ]
  return parts.filter(([, n]) => n > 0).map(([glyph, n]) => `${glyph}${n}`).join(" ")
}

/** One row of the frontier graph: a unit, or a layer too wide for its lanes. */
interface FrontierItem {
  readonly members: readonly DrawnNode[]
  readonly glyph: string
  readonly label: string
  readonly tone?: SidebarTone
  /** The items its members' edges lead to, in row order. */
  readonly children: FrontierItem[]
}

// A failure anywhere in an item shows.
const toneOf = (glyphs: readonly string[]) => (glyphs.includes(GLYPH.failed) ? { tone: "error" as const } : {})

// layerItems is one item per unit of a layer, or one grouped item when the
// layer is wider than the lane budget.
function layerItems(layer: readonly DrawnNode[]): FrontierItem[] {
  const glyphs = layer.map(glyphFor)
  if (layer.length <= MAX_SIDEBAR_LANES) {
    return layer.map((node, i) => ({ members: [node], glyph: glyphs[i], label: node.id, ...toneOf([glyphs[i]]), children: [] }))
  }
  return [{ members: layer, glyph: GROUP_GLYPH, label: `${glyphs.join("")} ${layer.length} parallel`, ...toneOf(glyphs), children: [] }]
}

// sidebarGraph is the frontier: every unit still in flight or planned, and
// every unit that ended without completing, with the edges between them.
// Completed units are the title's count and withdrawn ones are not work, so
// neither is drawn. Within each connected component a layer wider than the
// lane budget becomes one item; its units share a layer, so no edge joins them
// and the contracted graph stays acyclic.
function sidebarGraph(snapshot: DagSnapshot): FrontierItem[] {
  const drawn = drawnGraph(snapshot)
  const frontier = graphOf(drawn.nodes.filter((n) => !(n.state === "settled" && n.outcome === "completed")), drawn.edges)
  const order = connectedComponents(frontier).flatMap(({ nodes }) =>
    [...new Set(nodes.map((n) => n.layer))].sort((a, b) => a - b).flatMap((layer) => layerItems(nodes.filter((n) => n.layer === layer))))
  const joins = (from: FrontierItem, to: FrontierItem) =>
    frontier.edges.some((e) => from.members.some((m) => m.id === e.from) && to.members.some((m) => m.id === e.to))
  for (const item of order) item.children.push(...order.filter((other) => joins(item, other)))
  return order
}

// connectorRow draws, between two unit rows, lanes joining the unit's lane
// `at`: merging into it from above, or forking from it below. Lanes passing
// by stay vertical; a joining line crossing one draws ┼.
function connectorRow(lanes: readonly (FrontierItem | undefined)[], at: number, others: readonly number[], kind: "merge" | "fork"): SidebarRow {
  const count = Math.max(lanes.length, at + 1, ...others.map((o) => o + 1))
  const mask = new Array<number>(count * 2 - 1).fill(0)
  lanes.forEach((target, lane) => {
    if (target !== undefined && lane !== at && !others.includes(lane)) mask[lane * 2] |= UP | DOWN
  })
  mask[at * 2] |= UP | DOWN
  for (const other of others) {
    mask[other * 2] |= kind === "merge" ? UP : DOWN
    const [low, high] = [Math.min(at, other) * 2, Math.max(at, other) * 2]
    for (let cell = low; cell <= high; cell++) {
      if (cell > low) mask[cell] |= LEFT
      if (cell < high) mask[cell] |= RIGHT
    }
  }
  return { text: mask.map((m) => LINE_GLYPHS[m]).join("").trimEnd() }
}

// isChainLink reports a unit that alone follows the unit on the row above,
// with no other lane open, and does not fork itself.
function isChainLink(
  item: FrontierItem,
  previous: FrontierItem | undefined,
  incoming: readonly number[],
  at: number,
  lanes: readonly (FrontierItem | undefined)[],
): boolean {
  return (
    incoming.length === 1 &&
    previous !== undefined &&
    previous.children.length === 1 &&
    previous.children[0] === item &&
    item.children.length <= 1 &&
    lanes.every((target, lane) => lane === at || target === undefined)
  )
}

// unitRow draws a unit's row: its glyph in lane `at` among the open lanes, or,
// for the chain-th link of a chain, the indented chain marker; then its label.
function unitRow(item: FrontierItem, at: number, lanes: readonly (FrontierItem | undefined)[], chain: number): SidebarRow {
  const laneGlyph = (target: FrontierItem | undefined, lane: number) => {
    if (lane === at) return item.glyph
    return target === undefined ? " " : "│"
  }
  const cells = lanes.map((target, lane) => laneGlyph(target, lane))
  const gutter = chain > 0 ? `${CHAIN_INDENT.repeat(Math.min(chain - 1, MAX_CHAIN_INDENT))}${CHAIN_GLYPH}${item.glyph}` : cells.join(" ").trimEnd()
  return { text: `${gutter} ${fit(item.label, SIDEBAR_COLUMNS - Bun.stringWidth(gutter) - 1)}`, ...(item.tone ? { tone: item.tone } : {}) }
}

// laneRows lays the graph out like `git log --graph`, top-down: the state
// glyph is the node, sitting in its lane, and every edge is a lane carried
// down to the unit it leads to. A unit that alone follows the unit on the row
// above, with no other lane open, has no connector row to show the edge, so it
// is marked `└─` and indented one level per link, up to MAX_CHAIN_INDENT;
// a unit that forks stays in its lane so its fork row lines up under it.
// Undefined when the walk needs more lanes than the budget.
function laneRows(order: readonly FrontierItem[]): SidebarRow[] | undefined {
  const lanes: (FrontierItem | undefined)[] = []
  const rows: SidebarRow[] = []
  const free = () => (lanes.includes(undefined) ? lanes.indexOf(undefined) : lanes.length)
  let previous: FrontierItem | undefined
  let chain = 0
  for (const item of order) {
    const incoming = lanes.flatMap((target, lane) => (target === item ? [lane] : []))
    const at = incoming[0] ?? free()
    if (incoming.length > 1) {
      rows.push(connectorRow(lanes, at, incoming.slice(1), "merge"))
      for (const lane of incoming.slice(1)) lanes[lane] = undefined
    }
    chain = isChainLink(item, previous, incoming, at, lanes) ? chain + 1 : 0
    previous = item
    lanes[at] = item
    rows.push(unitRow(item, at, lanes, chain))
    const [first, ...rest] = item.children
    lanes[at] = first
    const forks = rest.map((child) => {
      const lane = free()
      lanes[lane] = child
      return lane
    })
    if (forks.length > 0) rows.push(connectorRow(lanes, at, forks, "fork"))
    while (lanes.length > 0 && lanes.at(-1) === undefined) lanes.pop()
    if (lanes.length > MAX_SIDEBAR_LANES) return undefined
  }
  return rows
}

/**
 * The DAG for the sidebar's narrow column: the frontier as lanes (a flat
 * list in dependency order when it needs more lanes than the budget), then
 * running activities. Every row fits SIDEBAR_COLUMNS and the total never
 * exceeds MAX_SIDEBAR_ROWS: the oldest rows fold into a leading "… N more".
 */
export function sidebarRows(snapshot: DagSnapshot): SidebarRow[] {
  const order = sidebarGraph(snapshot)
  const graph =
    laneRows(order) ??
    order.map((item) => ({ text: fit(`${item.glyph} ${item.label}`, SIDEBAR_COLUMNS), ...(item.tone ? { tone: item.tone } : {}) }))
  const activities = snapshot.activities
    .filter((a) => a.state === "in_flight")
    .map((a) => ({ text: fit(`${activityGlyph(a)} ${a.activity_id}`, SIDEBAR_COLUMNS) }))
  const rows = [...graph, ...activities]
  if (rows.length <= MAX_SIDEBAR_ROWS) return rows
  const hidden = rows.length - (MAX_SIDEBAR_ROWS - 1)
  return [{ text: `… ${hidden} more`, tone: "muted" }, ...rows.slice(hidden)]
}

/**
 * The sidebar's rows, or nothing: unlike the route view it never explains
 * why (no confirmed projection, empty DAG), the title line already does.
 * Every branch below must resolve to a <text>, never to `false`/`undefined`
 * directly under <box> — Show's own off-state placeholder needs one too.
 */
export function SidebarGraph(props: {
  readonly snapshot: DagSnapshot | undefined
  readonly tones?: Partial<Record<SidebarTone, RGBA>>
}) {
  return (
    <Show when={props.snapshot} fallback={<text></text>}>
      {(shown: Accessor<DagSnapshot>) => (
        <Show when={hasContent(shown())} fallback={<text></text>}>
          <box flexDirection="column">
            <For each={sidebarRows(shown())}>
              {(row) => (
                <text wrapMode="none" fg={row.tone ? props.tones?.[row.tone] : undefined}>
                  {row.text}
                </text>
              )}
            </For>
          </box>
        </Show>
      )}
    </Show>
  )
}

// ---------------------------------------------------------------------------
// Snapshot query
// ---------------------------------------------------------------------------

// AbortReturn is the part of the core's abort reply this plugin relays.
type AbortReturn = { child: string; root: string; notice: string }
function isAbortReturn(value: unknown): value is AbortReturn {
  return isRecord(value) && isString(value.child) && isString(value.root) && isString(value.notice)
}

function stateDir(workspace: string): string {
  return resolve(workspace, ".takt-ai", "vfs")
}

export default Plugin.define({
  id: "takt.dag",
  setup(context) {
    const workspace = (context.location ?? context.data.location.default()).directory
    const [snapshot, setSnapshot] = createSignal<DagSnapshot | undefined>()
    const [health, setHealth] = createSignal<Health>("waiting")
    const [problem, setProblem] = createSignal<string | undefined>()
    // The private state directory is per workspace, so its history spans every
    // session that ever ran there; the graph shown is only this root session's.
    const [session, setSession] = createSignal<string | undefined>()

    // A failed query keeps the last confirmed graph and marks it stale;
    // without one there is nothing to keep, so Takt is unavailable (§11).
    const fail = (reason: string) => {
      setProblem(reason)
      setHealth(snapshot() ? "stale" : "unavailable")
    }

    const accept = (next: DagResponse) => {
      // Takt answering that it cannot project is not a failed query: the last
      // graph stays on screen, but the capture is unavailable, not stale (§11).
      if (next.capture === "unavailable") {
        setProblem("takt cannot provide a projection")
        setHealth("unavailable")
        return
      }
      const invalid = topologyError(next)
      if (invalid) {
        setProblem(`projection rejected: ${invalid}`)
        setHealth("invalid")
        return
      }
      const shown = snapshot()
      // Only a strictly newer revision replaces the graph; an unchanged one
      // leaves every signal untouched, so nothing redraws.
      if (!shown || next.projection_revision > shown.projection_revision) setSnapshot(next)
      if (health() !== "confirmed") setHealth("confirmed")
      if (problem() !== undefined) setProblem(undefined)
    }

    let running: ReturnType<typeof Bun.spawn> | undefined
    const poll = async () => {
      const root = session()
      if (running || root === undefined) return // no overlapping snapshot requests
      const proc = Bun.spawn(
        [TAKT_AI, "dag", "status", "--workspace", workspace, "--state", stateDir(workspace), "--session", root, "--format", "json"],
        { cwd: workspace, stdin: "ignore", stdout: "pipe", stderr: "pipe" },
      )
      running = proc
      try {
        const [out, err, code] = await Promise.all([
          new Response(proc.stdout).text(),
          new Response(proc.stderr).text(),
          proc.exited,
        ])
        if (running !== proc) return // disposed while in flight
        if (code !== 0) return fail(err.trim() || `dag status exited ${code}`)
        const parsed: unknown = JSON.parse(out)
        accept(decodeSnapshot(parsed))
      } catch (error) {
        if (running === proc) fail(error instanceof Error ? error.message : "Unknown DAG status error")
      } finally {
        if (running === proc) running = undefined
      }
    }

    // Polling lives exactly as long as a view (the route or the sidebar) is
    // mounted (§9); views share one poll. The last confirmed snapshot stays in
    // setup's closure, so remounting a view shows it until a newer revision
    // arrives.
    let timer: ReturnType<typeof setInterval> | undefined
    let viewers = 0
    const stopPolling = () => {
      clearInterval(timer)
      timer = undefined
      running?.kill()
      running = undefined
    }
    // A different session shares nothing with the graph on screen: drop it
    // and any query still in flight for it, then ask for the new one.
    const follow = (sessionID: string) => {
      const root = context.data.session.root(sessionID)
      if (root === session()) return
      running?.kill()
      running = undefined
      setSession(root)
      setSnapshot(undefined)
      setProblem(undefined)
      setHealth("waiting")
      if (viewers > 0) void poll()
    }
    const acquire = () => {
      if (viewers++ > 0) return
      void poll()
      timer = setInterval(() => void poll(), POLL_INTERVAL_MS)
    }
    const release = () => {
      viewers = Math.max(0, viewers - 1)
      if (viewers === 0) stopPolling()
    }

    const DagView = (props: { readonly sessionID?: string }) => {
      createEffect(() => props.sessionID !== undefined && follow(props.sessionID))
      onMount(acquire)
      onCleanup(release)
      const [legend, setLegend] = createSignal(false)
      // The route replaces the session view, so Esc must lead back to it.
      context.keymap.layer(() => ({
        mode: "global",
        commands: [
          {
            id: "takt.dag.legend",
            title: "Show or hide the DAG legend",
            bind: "?",
            run: () => setLegend((shown) => !shown),
          },
          {
            id: "takt.dag.close",
            title: "Back to session",
            bind: "escape",
            run: () =>
              context.ui.router.navigate(
                props.sessionID !== undefined ? { type: "session", sessionID: props.sessionID } : { type: "home" },
              ),
          },
        ],
      }))
      return (
        // The route box fills the host's route area; only the graph shrinks,
        // so the header and legend always stay visible above the prompt.
        <box flexDirection="column" flexGrow={1} minWidth={0} minHeight={0}>
          <text flexShrink={0} fg={context.theme.text.base}>{headerLine(snapshot(), health())}</text>
          <text flexShrink={0}>{problem() ?? ""}</text>
          <text flexShrink={0}> </text>
          <Show
            when={snapshot()}
            fallback={<text flexShrink={0}>{health() === "waiting" ? "waiting for confirmed projection" : "no confirmed projection"}</text>}
          >
            {(shown: Accessor<DagSnapshot>) => (
              <Show when={hasContent(shown())} fallback={<text flexShrink={0}>empty DAG · no planned work</text>}>
                <GraphView snapshot={shown()} mode="route" />
              </Show>
            )}
          </Show>
          <Show when={legend()} fallback={<text flexShrink={0}></text>}>
            <text flexShrink={0}> </text>
            <For each={LEGEND_LINES}>{(line) => <text flexShrink={0}>{line}</text>}</For>
          </Show>
        </box>
      )
    }

    const SidebarView = (props: { readonly sessionID: string }) => {
      createEffect(() => follow(props.sessionID))
      onMount(acquire)
      onCleanup(release)
      return (
        <box flexDirection="column">
          <text wrapMode="none" fg={context.theme.text.base}>
            <b>DAG</b> <span style={{ fg: context.theme.text.muted }}>{sidebarDetail(snapshot(), health())}</span>
          </text>
          <text>{problem() ?? ""}</text>
          <SidebarGraph
            snapshot={snapshot()}
            tones={{ muted: context.theme.text.muted, error: context.theme.text.feedback.error.base }}
          />
        </box>
      )
    }

    // abortLent is the user's own abort of a lent interlocutor session
    // (IR-24): no agent holds one. Run from the lent session or from the
    // session that lent it, it confirms, has the core record the abort with
    // the user as origin, stops the lent session and hands the core's notice
    // to the convening agent.
    const abortLent = async () => {
      const route = context.ui.router.current()
      if (route.type !== "session") {
        context.ui.toast.show({ variant: "warning", message: "Open the lent session, or the session that lent it, to abort it." })
        return
      }
      const confirmed = await context.ui.dialog.confirm({
        title: "Abort the lent session",
        message: "End the lent specialist's session now and return the interface without a handoff?",
        label: { confirm: "Abort", cancel: "Keep it" },
      })
      if (confirmed !== true) return
      const request = { action: "abort_switch", child: route.sessionID, evidence: "the user ran /takt-abort", origin: "user" }
      const proc = Bun.spawn(
        [TAKT_AI, "dispatch", "--workspace", workspace, "--state", stateDir(workspace), "--request", JSON.stringify(request)],
        { cwd: workspace, stdin: "ignore", stdout: "pipe", stderr: "pipe" },
      )
      const [out, err, code] = await Promise.all([new Response(proc.stdout).text(), new Response(proc.stderr).text(), proc.exited])
      let reply: unknown
      try { reply = JSON.parse(out) } catch { reply = undefined }
      if (code !== 0 || !isAbortReturn(reply)) {
        const refusal = typeof reply === "object" && reply !== null && typeof (reply as { error?: unknown }).error === "string" ? (reply as { error: string }).error : ""
        context.ui.toast.show({ variant: "error", message: refusal || (err || out).trim() || `abort exited ${code}` })
        return
      }
      // The core already cleared the holder, so the convening agent gets the
      // notice even when the stop fails, told that the lent session may still
      // run; each failure still reaches the toast.
      const stop = await context.client.session.interrupt({ sessionID: reply.child }).then(() => undefined, (error: unknown) => ({ error }))
      const text = stop ? `${reply.notice}\nStopping the lent session failed; it may still be running.` : reply.notice
      try {
        await context.client.session.synthetic({ sessionID: reply.root, text })
      } catch (error) {
        if (stop) {
          const reason = (failure: unknown) => failure instanceof Error ? failure.message : String(failure)
          throw new AggregateError([stop.error, error], `stopping the lent session failed (${reason(stop.error)}) and notifying the convening agent failed (${reason(error)})`, { cause: error })
        }
        throw error
      }
      if (stop) throw stop.error
      context.ui.toast.show({ variant: "success", message: "Lent session aborted; the interface is back with the convening agent." })
    }

    const unregisterRoute = context.ui.router.register({ name: "takt.dag", render: (input) => <DagView sessionID={input.data?.sessionID} /> })

    // A keymap layer belongs to the component that creates it, so the command
    // lives in an app-slot component for the plugin's whole lifetime.
    const releaseCommands = context.ui.slot({
      append: "app",
      render: () => {
        context.keymap.layer(() => ({
          mode: "global",
          commands: [
            {
              id: "takt.dag.open",
              title: "Open Takt DAG",
              // ctrl+shift+d is OpenCode's default input.delete.line.
              bind: "<leader>d",
              palette: true,
              slash: { name: "takt-dag" },
              run: () => {
                const route = context.ui.router.current()
                context.ui.router.navigate({
                  type: "plugin",
                  name: "takt.dag",
                  data: route.type === "session" ? { sessionID: route.sessionID } : undefined,
                })
              },
            },
            {
              id: "takt.interlocutor.abort",
              title: "Abort the lent Takt session",
              palette: true,
              slash: { name: "takt-abort" },
              run: () => void abortLent().catch((error: unknown) =>
                context.ui.toast.show({ variant: "error", message: error instanceof Error ? error.message : String(error) })),
            },
          ],
        }))
        return null
      },
    })

    const releaseSidebar = context.ui.slot({ append: "sidebar.footer", render: (input) => <SidebarView sessionID={input.sessionID} /> })

    return () => {
      viewers = 0
      stopPolling()
      releaseSidebar()
      releaseCommands()
      unregisterRoute()
    }
  },
})
