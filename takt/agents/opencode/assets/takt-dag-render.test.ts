import { describe, expect, test } from "bun:test"
import realSession from "../testdata/dag-real-session.json"
import { testRender } from "@opentui/solid"
import {
  activityError,
  activityGlyph,
  boxLines,
  boxWidth,
  computeLayout as computeLayoutOf,
  countsLine,
  decodeSnapshot,
  type DagActivity,
  type DagNode,
  type DagSnapshot,
  type DrawnNode,
  edgePorts,
  edgeRows,
  fit,
  GraphView as GraphViewOf,
  glyphFor,
  hasContent,
  headerLine,
  ordinal,
  packActivities,
  sidebarDetail,
  sidebarRows as sidebarRowsOf,
  SidebarGraph as SidebarGraphOf,
  topologyError,
  topologyKey,
} from "./takt-dag"

type Counts = DagSnapshot["counts"]
/** A unit as a fixture declares it: Takt derives its layer from the edges. */
type Unit = Omit<DagNode, "layer">
const node = (overrides: Partial<Unit> = {}): Unit => ({ id: "unit-1", node_kind: "delegated", state: "planned", launched: false, ...overrides })
// The glyph of a drawn unit in the given state.
const glyph = (overrides: Partial<Omit<DrawnNode, "layer">>) =>
  glyphFor({ id: "unit-1", node_kind: "delegated", state: "planned", launched: false, layer: 0, ...overrides })
const activity = (overrides: Partial<DagActivity> = {}): DagActivity => ({ activity_id: "a", node_kind: "orchestrator", state: "in_flight", ...overrides })
const counts = (overrides: Partial<Counts> = {}): Counts =>
  ({ planned: 0, in_flight: 0, completed: 0, failed: 0, backtracked: 0, interrupted: 0, withdrawn: 0, ...overrides })

// The fixtures stand in for Takt's snapshot: layers follow the edges and counts
// follow the nodes, as Takt derives them; the view reads both and computes neither.
interface Fixture extends Omit<DagSnapshot, "nodes" | "counts"> {
  readonly nodes: readonly Unit[]
  readonly counts?: Counts
}
function layered(fixture: Fixture): DagSnapshot {
  const layers = new Map<string, number>()
  const layerOf = (id: string): number => {
    const known = layers.get(id)
    if (known !== undefined) return known
    const layer = Math.max(-1, ...fixture.edges.filter((e) => e.to === id).map((e) => layerOf(e.from))) + 1
    layers.set(id, layer)
    return layer
  }
  const tally: { -readonly [K in keyof Counts]: Counts[K] } = counts()
  for (const n of fixture.nodes) tally[n.state === "settled" ? (n.outcome ?? "completed") : n.state]++
  return { ...fixture, nodes: fixture.nodes.map((n) => ({ ...n, layer: layerOf(n.id) })), counts: fixture.counts ?? tally }
}
// A fixture taken as it is, every unit at layer 0: for topologies layering cannot walk.
const unlayered = (fixture: Fixture): DagSnapshot => ({ ...fixture, nodes: fixture.nodes.map((n) => ({ ...n, layer: 0 })), counts: counts() })
// The recorded session, through the same decoder the plugin reads Takt with.
const real = ((): DagSnapshot => {
  const response = decodeSnapshot(realSession)
  if (!("nodes" in response)) throw new Error("the recorded session is unavailable")
  return response
})()
type Layout = ReturnType<typeof computeLayoutOf>
const computeLayout = (fixture: Fixture, previous?: Layout, viewport?: { width: number; height: number }): Layout => computeLayoutOf(layered(fixture), previous, viewport)
const sidebarRows = (fixture: Fixture) => sidebarRowsOf(layered(fixture))
const GraphView = (props: { snapshot: Fixture; mode: "route" | "sidebar" }) => GraphViewOf({ ...props, snapshot: layered(props.snapshot) })
const SidebarGraph = (props: { snapshot: Fixture | undefined }) => SidebarGraphOf({ snapshot: props.snapshot === undefined ? undefined : layered(props.snapshot) })

describe("ordinal", () => {
  test("orders strings lexicographically", () => {
    expect(ordinal("a", "b")).toBe(-1)
    expect(ordinal("b", "a")).toBe(1)
    expect(ordinal("a", "a")).toBe(0)
  })
})

describe("activityError", () => {
  test("accepts distinct, non-empty activity identities", () => {
    expect(activityError([activity({ activity_id: "a" }), activity({ activity_id: "b", state: "settled" })])).toBeUndefined()
  })
  test("rejects a duplicate activity identity", () => {
    expect(activityError([activity({ activity_id: "a" }), activity({ activity_id: "a", state: "settled" })])).toBe("duplicate or empty activity identity")
  })
  test("rejects an empty activity identity", () => {
    expect(activityError([activity({ activity_id: "" })])).toBe("duplicate or empty activity identity")
  })
})

describe("topologyError", () => {
  const base = { schema_version: 1, projection_revision: 1, history_position: 1, session_id: "s", capture: "current", activities: [] } as const

  test("accepts a valid acyclic snapshot", () => {
    const snapshot = unlayered({ ...base, nodes: [node({ id: "a" }), node({ id: "b" })], edges: [{ from: "a", to: "b" }] })
    expect(topologyError(snapshot)).toBeUndefined()
  })
  test("rejects a duplicate node identity", () => {
    const snapshot = unlayered({ ...base, nodes: [node({ id: "a" }), node({ id: "a" })], edges: [] })
    expect(topologyError(snapshot)).toBe("duplicate node identity")
  })
  test("rejects an edge naming an unknown node", () => {
    const snapshot = unlayered({ ...base, nodes: [node({ id: "a" })], edges: [{ from: "a", to: "ghost" }] })
    expect(topologyError(snapshot)).toBe("edge a -> ghost names an unknown node")
  })
  test("rejects a prerequisite cycle", () => {
    const snapshot = unlayered({ ...base, nodes: [node({ id: "a" }), node({ id: "b" })], edges: [{ from: "a", to: "b" }, { from: "b", to: "a" }] })
    expect(topologyError(snapshot)).toBe("prerequisite cycle")
  })
  test("rejects an invalid activity even with valid nodes", () => {
    const snapshot = unlayered({ ...base, nodes: [node({ id: "a" })], edges: [], activities: [activity({ activity_id: "" })] })
    expect(topologyError(snapshot)).toBe("duplicate or empty activity identity")
  })
})

describe("topologyKey", () => {
  const base = { schema_version: 1, projection_revision: 1, history_position: 1, session_id: "s", capture: "current", activities: [] } as const
  test("is order-independent and reflects node kind and edges", () => {
    const forward = layered({ ...base, nodes: [node({ id: "a" }), node({ id: "b" })], edges: [{ from: "a", to: "b" }] })
    const reordered = layered({ ...base, nodes: [node({ id: "b" }), node({ id: "a" })], edges: [{ from: "a", to: "b" }] })
    expect(topologyKey(forward)).toBe(topologyKey(reordered))
  })
  test("changes when the edge set changes", () => {
    const withEdge = layered({ ...base, nodes: [node({ id: "a" }), node({ id: "b" })], edges: [{ from: "a", to: "b" }] })
    const withoutEdge = layered({ ...base, nodes: [node({ id: "a" }), node({ id: "b" })], edges: [] })
    expect(topologyKey(withEdge)).not.toBe(topologyKey(withoutEdge))
  })
  test("changes when a unit moves to another layer, not when only its state does", () => {
    const deep = { ...base, nodes: [{ ...node({ id: "a" }), layer: 0 }, { ...node({ id: "b" }), layer: 2 }], edges: [{ from: "a", to: "b" }] }
    const shallow = { ...deep, nodes: [{ ...node({ id: "a" }), layer: 0 }, { ...node({ id: "b" }), layer: 1 }] }
    const running = { ...deep, nodes: [{ ...node({ id: "a" }), layer: 0 }, { ...node({ id: "b", state: "in_flight" }), layer: 2 }] }
    expect(topologyKey(deep)).not.toBe(topologyKey(shallow))
    expect(topologyKey(deep)).toBe(topologyKey(running))
  })
})

describe("graph layout", () => {
  const base = { schema_version: 1, projection_revision: 1, history_position: 1, session_id: "s", capture: "current", activities: [] } as const

  test("lays out branching depth and layer-skipping edges with deterministic routes", () => {
    const graph = {
      ...base,
      nodes: [node({ id: "a" }), node({ id: "b" }), node({ id: "c" }), node({ id: "d" })],
      edges: [{ from: "a", to: "b" }, { from: "b", to: "c" }, { from: "a", to: "c" }, { from: "c", to: "d" }],
    }
    const layout = computeLayout(graph)

    expect([...layout.nodes.keys()]).toEqual(["a", "b", "c", "d"])
    expect(layout.nodes.get("a")?.depth).toBe(0)
    expect(layout.nodes.get("c")?.depth).toBe(2)
    expect(layout.paths).toHaveLength(4)
    expect(layout.width).toBeGreaterThan(0)
    expect(layout.height).toBeGreaterThan(0)
    expect(edgeRows(layout).join("\n")).toContain("─")
    expect(edgePorts(layout).some((port) => port.glyph === "▶")).toBe(true)
    expect(computeLayout(graph, layout)).toBe(layout)
  })

  const chain = (length: number) => ({
    ...base,
    nodes: Array.from({ length }, (_, i) => node({ id: `unit-${i}` })),
    edges: Array.from({ length: length - 1 }, (_, i) => ({ from: `unit-${i}`, to: `unit-${i + 1}` })),
  })

  test("a wide viewport keeps one band flowing right", () => {
    const layout = computeLayout(chain(6), undefined, { width: 400, height: 100 })
    expect(layout.bands).toBe(1)
    expect(edgePorts(layout).every((port) => port.glyph === "▶")).toBe(true)
  })

  test("a narrow viewport wraps into a mirrored band below, joined through a gutter", () => {
    const layout = computeLayout(chain(6), undefined, { width: 60, height: 100 })
    expect(layout.bands).toBeGreaterThan(1)
    expect(layout.width).toBeLessThan(computeLayout(chain(6)).width)
    const first = layout.nodes.get("unit-0")
    const last = layout.nodes.get("unit-5")
    expect(last && first && last.y > first.y).toBe(true)
    expect(edgePorts(layout).some((port) => port.glyph === "◀")).toBe(true)
    const rows = edgeRows(layout)
    expect(rows).toHaveLength(layout.height)
    expect(rows.every((row) => row.length === layout.width)).toBe(true)
  })

  test("never cuts a name when the bands do not fit the height: the view scrolls instead", async () => {
    const ids = ["overhaul-defect-evidence", "overhaul-integration", "overhaul-acceptance"]
    const named = { ...base, nodes: ids.map((id) => node({ id })),
      edges: [{ from: ids[0], to: ids[1] }, { from: ids[1], to: ids[2] }] }
    const viewport = { width: 80, height: 5 }
    const layout = computeLayout(named, undefined, viewport)
    expect(layout.width).toBeLessThanOrEqual(viewport.width)
    expect(layout.height).toBeGreaterThan(viewport.height)
    const setup = await testRender(() => GraphView({ snapshot: named, mode: "route" }), { width: 80, height: 30 })
    try {
      await setup.renderOnce()
      const frame = setup.captureCharFrame()
      for (const id of ids) expect(frame).toContain(id)
      expect(frame).not.toContain("…")
    } finally {
      setup.renderer.destroy()
    }
  })

  test("a change in activities never moves a unit on the route", async () => {
    // The route's chrome: side columns and rows outside the graph; one activity row's rows.
    const sideColumns = 2
    const chromeRows = 12
    const activityRowRows = 4
    const width = 120
    const fitted = (height: number) => computeLayoutOf(real, undefined, { width: width - sideColumns, height }).height
    const natural = fitted(Infinity)
    // The recorded session closes up its bands when short of rows, so the
    // fit is what an activity row could change.
    expect(fitted(natural - 1)).toBeLessThan(natural)
    // Tall enough for the natural layout plus one activity row, not two.
    const height = natural + chromeRows + activityRowRows
    const frameWith = async (ids: readonly string[]) => {
      const snapshot = { ...real, activities: ids.map((id) => activity({ activity_id: id })) }
      const setup = await testRender(() => GraphViewOf({ snapshot, mode: "route" }), { width, height })
      try {
        await setup.renderOnce()
        return setup.captureCharFrame().split("\n").slice(0, natural)
      } finally {
        setup.renderer.destroy()
      }
    }
    const one = await frameWith(["probe"])
    // Long enough to wrap the lane into a second row at this width.
    const many = await frameWith(Array.from({ length: 12 }, (_, i) => `probe activity ${i}`))
    expect(many).toEqual(one)
  })

  test("every box is as wide as its own name plus its chrome", () => {
    const named = { ...base, nodes: ["a", "a much longer name"].map((id) => node({ id })), edges: [{ from: "a", to: "a much longer name" }] }
    for (const placed of computeLayout(named).nodes.values()) expect(placed.width).toBe(placed.id.length + 6)
  })

  test("a withdrawn unit and its edges are not drawn", () => {
    const graph = { ...base, nodes: [node({ id: "kept" }), node({ id: "next" }), node({ id: "gone", state: "withdrawn" })],
      edges: [{ from: "kept", to: "next" }, { from: "kept", to: "gone" }] }
    const layout = computeLayout(graph)
    expect([...layout.nodes.keys()].sort()).toEqual(["kept", "next"])
    expect(layout.paths).toHaveLength(1)
  })

  test("every edge follows the snake: forward within a band, only down between bands", () => {
    for (const width of [400, 90, 60, 30]) {
      const layout = computeLayout(chain(6), undefined, { width, height: 100 })
      const rows = [...new Set([...layout.nodes.values()].map((n) => n.y))].sort((a, b) => a - b)
      for (let i = 0; i < 5; i++) {
        const [from, to] = [layout.nodes.get(`unit-${i}`), layout.nodes.get(`unit-${i + 1}`)]
        const head = layout.heads[i]
        if (!from || !to) throw new Error("a chain unit was not laid out")
        if (from.y === to.y) {
          // Even bands flow right, odd ones left.
          expect(head.glyph).toBe(rows.indexOf(from.y) % 2 === 0 ? "▶" : "◀")
          expect(Math.sign(to.x - from.x)).toBe(rows.indexOf(from.y) % 2 === 0 ? 1 : -1)
        } else {
          expect(to.y).toBeGreaterThan(from.y)
        }
      }
    }
  })

  test("a turn into the next band leaves a box's bottom and enters a box's top when nothing is in the way", () => {
    const layout = computeLayout(chain(4), undefined, { width: 40, height: 100 })
    expect(layout.bands).toBeGreaterThan(1)
    const turn = layout.heads.find((head) => head.glyph === "▼")
    expect(turn).toBeDefined()
    const boxes = [...layout.nodes.values()]
    // The arrowhead sits right above a box, and some path starts right below one.
    expect(boxes.some((n) => turn && turn.y === n.y - 1 && turn.x >= n.x && turn.x < n.x + n.width)).toBe(true)
    expect(layout.paths.some(([[x, y]]) => boxes.some((n) => y === n.y + 3 && x >= n.x && x < n.x + n.width))).toBe(true)
  })

  test("edges into one box share one arrowhead", () => {
    const fanIn = { ...base, nodes: ["a", "b", "c", "join"].map((id) => node({ id })),
      edges: ["a", "b", "c"].map((from) => ({ from, to: "join" })) }
    const heads = edgePorts(computeLayout(fanIn))
    expect(heads).toHaveLength(3)
    expect(new Set(heads.map((head) => `${head.x},${head.y}`)).size).toBe(1)
  })

  test("splits the columns into bands of even width", () => {
    const layout = computeLayout(chain(7), undefined, { width: 60, height: 100 })
    const perBand = new Map<number, number>()
    for (const placed of layout.nodes.values()) perBand.set(placed.y, (perBand.get(placed.y) ?? 0) + 1)
    const sizes = [...perBand.values()]
    expect(sizes.length).toBeGreaterThan(1)
    expect(Math.max(...sizes) - Math.min(...sizes)).toBeLessThanOrEqual(1)
  })

  test("keeps a blank column at both edges and between parallel rails, and tells a crossing from a junction", () => {
    for (const viewport of [{ width: 200, height: 1000 }, { width: 120, height: 50 }, { width: 100, height: 50 }]) {
      const rows = edgeRows(computeLayout(real, undefined, viewport))
      expect(rows.every((row) => row.startsWith(" ") && row.endsWith(" "))).toBe(true)
      expect(rows.some((row) => /[│╫]{2}/.test(row))).toBe(false)
    }
    expect(edgeRows(computeLayout(real, undefined, { width: 120, height: 50 })).join("\n")).toContain("╫")
  })

  test("never draws wider than the viewport, down to one column per band", () => {
    const graph = chain(6)
    const narrowest = computeLayout(graph, undefined, { width: 1, height: 100 })
    expect(narrowest.bands).toBe(6)
    for (const width of [narrowest.width, 30, 45, 60, 90, 400]) {
      for (const height of [3, 10, 100]) {
        const layout = computeLayout(graph, undefined, { width, height })
        expect(layout.width).toBeLessThanOrEqual(width)
        expect(edgeRows(layout).every((row) => row.length === layout.width)).toBe(true)
      }
    }
  })

  test("a windowed terminal a few rows short still shows the whole graph within its width", () => {
    const ids = ["overhaul-defect-evidence", "overhaul-experience-direction", "overhaul-integration-contracts", "overhaul-playable-acceptance",
      "overhaul-implementation-tasks", "evocative-scene-soundtrack", "expressive-urban-cartoon-art", "playable-city-route-and-shell",
      "responsive-fair-platforming", "overhaul-staged-play-verification", "overhaul-materialized-release"]
    const tasks = ids.slice(5, 9)
    const graph = { ...base, nodes: ids.map((id) => node({ id })), edges: [
      { from: ids[0], to: ids[2] }, { from: ids[1], to: ids[2] }, { from: ids[2], to: ids[3] }, { from: ids[3], to: ids[4] },
      ...tasks.flatMap((task) => [{ from: ids[4], to: task }, { from: task, to: ids[9] }]), { from: ids[9], to: ids[10] },
    ] }
    for (const height of [22, 26, 32]) {
      const viewport = { width: 130, height }
      const layout = computeLayout(graph, undefined, viewport)
      expect(layout.width).toBeLessThanOrEqual(viewport.width)
      expect(layout.nodes.size).toBe(ids.length)
    }
  })

  test("reuses the layout for a state-only update and relayouts when the viewport changes", () => {
    const graph = chain(6)
    const viewport = { width: 60, height: 100 }
    const layout = computeLayout(graph, undefined, viewport)
    expect(computeLayout(graph, layout, { ...viewport })).toBe(layout)
    expect(computeLayout(graph, layout, { width: 400, height: 100 })).not.toBe(layout)
  })

  test("a layer-skipping edge crosses the layers it skips and wraps with the rest", () => {
    const skip = { ...base, nodes: ["a", "b", "c", "d", "e"].map((id) => node({ id })),
      edges: [{ from: "a", to: "b" }, { from: "b", to: "c" }, { from: "c", to: "d" }, { from: "d", to: "e" }, { from: "a", to: "e" }] }
    const layout = computeLayout(skip, undefined, { width: 30, height: 100 })
    expect(layout.bands).toBeGreaterThan(1)
    expect(edgePorts(layout)).toHaveLength(5)
    expect(edgeRows(layout).every((row) => row.length === layout.width)).toBe(true)
  })

  // Every cell each edge occupies, with whether the edge runs straight through it.
  const edgeCells = (layout: Layout) => {
    const boxes = [...layout.nodes.values()]
    // The box a port cell touches: beside its middle row, or right above or below it.
    const beside = (x: number, y: number) => boxes.find((n) =>
      (n.y + 1 === y && (n.x + n.width === x || n.x - 1 === x)) || ((n.y - 1 === y || n.y + 3 === y) && n.x <= x && x < n.x + n.width))?.id
    const cells = new Map<string, { edge: string; through: boolean; across: boolean }[]>()
    const edges = layout.paths.map((path, i) => {
      const edge = `${beside(...path[0])}->${beside(layout.heads[i].x, layout.heads[i].y)}`
      path.slice(1).forEach(([bx, by], j) => {
        const [ax, ay] = path[j]
        const steps = Math.abs(bx - ax) + Math.abs(by - ay)
        for (let k = 0; k <= steps; k++) {
          const key = `${ax + Math.sign(bx - ax) * k},${ay + Math.sign(by - ay) * k}`
          cells.set(key, [...(cells.get(key) ?? []), { edge, through: k > 0 && k < steps, across: ay === by }])
        }
      })
      return edge
    })
    return { boxes, cells, edges: new Set(edges) }
  }

  test("routes the real session clear of boxes, touching one only at its ports, without joining unrelated edges", () => {
    for (const viewport of [{ width: 400, height: 1000 }, { width: 200, height: 1000 }, { width: 150, height: 60 }, { width: 120, height: 50 }]) {
      const layout = computeLayout(real, undefined, viewport)
      const { boxes, cells, edges } = edgeCells(layout)
      const ports = new Set(layout.paths.flatMap((path, i) => [`${path[0][0]},${path[0][1]}`, `${layout.heads[i].x},${layout.heads[i].y}`]))
      for (const [key, owners] of cells) {
        const [x, y] = key.split(",").map(Number)
        // A box with a blank cell around it: an edge enters that ring only at a port.
        if (!ports.has(key)) expect(boxes.some((n) => x >= n.x - 1 && x <= n.x + n.width && y >= n.y - 1 && y <= n.y + 3)).toBe(false)
        for (const a of owners) for (const b of owners) {
          const [[af, at], [bf, bt]] = [a.edge.split("->"), b.edge.split("->")]
          // Edges may share a cell when every source drawn there reaches every target drawn there.
          const related = af === bf || at === bt || (edges.has(`${af}->${bt}`) && edges.has(`${bf}->${at}`))
          if (!related) expect(a.through && b.through && a.across !== b.across).toBe(true)
        }
      }
    }
  })

  test("in one band every edge bends only where it leaves and where it arrives", () => {
    const layout = computeLayout(real)
    expect(layout.bands).toBe(1)
    const turns = (path: readonly (readonly [number, number])[]) =>
      path.slice(2).filter(([x], i) => (path[i][0] === path[i + 1][0]) !== (path[i + 1][0] === x)).length
    for (const path of layout.paths) expect(turns(path)).toBeLessThanOrEqual(4)
  })

  test("handles an empty graph and a graph without edges", () => {
    const empty = computeLayout({ ...base, nodes: [], edges: [] })
    expect(empty.nodes.size).toBe(0)
    expect(empty.paths).toEqual([])
    expect(edgeRows(empty).every((row) => row.trim() === "")).toBe(true)
    expect(edgePorts(empty)).toEqual([])

    const isolated = computeLayout({ ...base, nodes: [node({ id: "alone" })], edges: [] })
    expect(isolated.nodes.get("alone")?.depth).toBe(0)
    expect(isolated.paths).toEqual([])
    expect(edgeRows(isolated).every((row) => row.trim() === "")).toBe(true)
  })

})

describe("node presentation", () => {
  test("glyphFor covers every drawn state/flight/outcome combination", () => {
    expect(glyph({ state: "planned" })).toBe("◌")
    expect(glyph({ state: "settled", outcome: "completed" })).toBe("✓")
    expect(glyph({ state: "settled", outcome: "failed" })).toBe("✗")
    expect(glyph({ state: "settled", outcome: "backtracked" })).toBe("↩")
    expect(glyph({ state: "settled", outcome: "interrupted" })).toBe("⊘")
    expect(glyph({ state: "in_flight", flight: "unmonitored" })).toBe("◐")
    expect(glyph({ state: "in_flight", flight: "pending_launch" })).toBe("○")
    expect(glyph({ state: "in_flight", flight: "uncertain" })).toBe("!")
    expect(glyph({ state: "in_flight", flight: "suspended" })).toBe("⏸")
    expect(glyph({ state: "in_flight", flight: "cancellation_pending" })).toBe("↯")
    expect(glyph({ state: "in_flight" })).toBe("●")
  })

  test("a box holds the glyph in its own cell and the name one blank from each border", () => {
    expect(boxLines("✓", "unit 7")).toEqual(["┌─┬────────┐", "│✓│ unit 7 │", "└─┴────────┘"])
    for (const line of boxLines("◌", "overhaul defect evidence")) expect(Bun.stringWidth(line)).toBe(boxWidth("overhaul defect evidence"))
  })

  test("renders activity-only and layered graph views in both layouts", async () => {
    const base = { schema_version: 1, projection_revision: 1, history_position: 1, session_id: "s", capture: "current", activities: [] } as const
    const review = activity({ activity_id: "review-1", flight: "observed_running" })
    const snapshots: Fixture[] = [
      { ...base, nodes: [], edges: [], activities: [review] },
      { ...base, nodes: [node({ id: "root" }), node({ id: "child", state: "in_flight" })], edges: [{ from: "root", to: "child" }], activities: [review] },
    ]
    for (const mode of ["route", "sidebar"] as const) {
      for (const snapshot of snapshots) {
        const setup = await testRender(() => GraphView({ snapshot, mode }), { width: 100, height: 30 })
        try {
          await setup.renderOnce()
          const frame = setup.captureCharFrame()
          expect(frame).toContain("│●│ review-1 │")
          expect(frame).not.toContain("activity")
          expect(frame).not.toContain("running")
          if (snapshot.nodes.length > 0) expect(frame).toContain("│◌│ root │")
        } finally {
          setup.renderer.destroy()
        }
      }
    }
  })
})

describe("activity presentation", () => {
  test("an activity shows the state glyph every node uses", () => {
    expect(activityGlyph(activity({ activity_id: "a1" }))).toBe(glyph({ state: "in_flight" }))
    expect(activityGlyph(activity({ activity_id: "a2", state: "settled", outcome: "completed" }))).toBe(glyph({ state: "settled", outcome: "completed" }))
    expect(activityGlyph(activity({ activity_id: "a3", state: "settled", outcome: "failed" }))).toBe(glyph({ state: "settled", outcome: "failed" }))
  })
  test("lost capture shows the unmonitored glyph, in flight and not failed", () => {
    const lost = activity({ activity_id: "a4", flight: "unmonitored" })
    expect(activityGlyph(lost)).toBe(glyph({ state: "in_flight", flight: "unmonitored" }))
  })
  test("packActivities wraps the lane into rows of boxes as wide as their names", () => {
    const lane = ["a1", "a2", "a3"].map((id) => activity({ activity_id: id }))
    const wide = packActivities(lane, 400)
    expect(wide.every((slot) => slot.row === 0 && slot.width === boxWidth("a1"))).toBe(true)
    const narrow = packActivities(lane, 20)
    expect(narrow.some((slot) => slot.row > 0)).toBe(true)
    expect(narrow.every((slot) => slot.left + slot.width <= 20)).toBe(true)
  })
  test("packActivities cuts a box wider than the lane to its width", () => {
    const [slot] = packActivities([activity({ activity_id: "a direct activity with a long name" })], 20)
    expect(slot.width).toBe(20)
    expect(slot.name).toEndWith("…")
    expect(boxWidth(slot.name)).toBe(20)
  })
  test("fit never returns more cells than it is given", () => {
    for (const width of [0, 1, 2, 5]) expect(Bun.stringWidth(fit("a direct activity", width))).toBeLessThanOrEqual(width)
    expect(fit("a direct activity", 0)).toBe("")
  })
})

describe("sidebar rows", () => {
  const base = { schema_version: 1, projection_revision: 1, history_position: 1, session_id: "s", capture: "current", activities: [] } as const
  const text = (fixture: Fixture) => sidebarRows(fixture).map((row) => row.text)
  const diamond = [{ from: "intent-pm", to: "experience-design" }, { from: "intent-pm", to: "structure-arch" },
    { from: "experience-design", to: "behavior-spec" }, { from: "structure-arch", to: "behavior-spec" }]

  test("draws a planned diamond as lanes: the glyph is the node, edges are never named", () => {
    const snapshot = { ...base, edges: diamond,
      nodes: ["intent-pm", "experience-design", "structure-arch", "behavior-spec"].map((id) => node({ id })) }
    expect(text(snapshot)).toEqual([
      "◌ intent-pm",
      "├─┐",
      "◌ │ experience-design",
      "│ ◌ structure-arch",
      "├─┘",
      "◌ behavior-spec",
    ])
  })

  test("draws the frontier only: completed units are the title's count, not rows", () => {
    const snapshot = { ...base, edges: diamond, nodes: [
      node({ id: "intent-pm", state: "settled", outcome: "completed", agent: "pm" }),
      node({ id: "experience-design", state: "settled", outcome: "completed", agent: "design" }),
      node({ id: "structure-arch", state: "in_flight", flight: "observed_running", agent: "arch" }),
      node({ id: "behavior-spec" }),
    ] }
    expect(sidebarRows(snapshot)).toEqual([
      { text: "● structure-arch" },
      { text: "└─◌ behavior-spec" },
    ])
  })

  test("shows an unmonitored unit with its own glyph and no error tone", () => {
    const snapshot = { ...base, edges: [], nodes: [node({ id: "u", state: "in_flight", flight: "unmonitored" })] }
    expect(sidebarRows(snapshot)).toEqual([{ text: "◐ u" }])
  })

  test("folds the oldest rows into a leading count when the graph exceeds the row budget", () => {
    const ids = Array.from({ length: 30 }, (_, i) => `u${String(i).padStart(2, "0")}`)
    const snapshot = { ...base, nodes: ids.map((id) => node({ id })), edges: [] }
    const rows = sidebarRows(snapshot)
    expect(rows).toHaveLength(16)
    expect(rows[0]).toEqual({ text: "… 15 more", tone: "muted" })
    expect(rows.at(-1)?.text).toBe("◌ u29")
  })

  test("groups a layer wider than the lane budget into one row", () => {
    const wide = ["b", "c", "d", "e"]
    const snapshot = { ...base, nodes: ["a", ...wide, "f"].map((id) => node({ id })),
      edges: wide.flatMap((id) => [{ from: "a", to: id }, { from: id, to: "f" }]) }
    expect(text(snapshot)).toEqual(["◌ a", "└─≡ ◌◌◌◌ 4 parallel", "  └─◌ f"])
  })

  test("a unit that ended without completing stays in the frontier, a failure in the error tone", () => {
    const snapshot = { ...base, nodes: [node({ id: "a", state: "settled", outcome: "completed" }), node({ id: "b", state: "settled", outcome: "backtracked" }),
      node({ id: "c", state: "settled", outcome: "failed" })], edges: [] }
    expect(sidebarRows(snapshot)).toEqual([{ text: "↩ b" }, { text: "✗ c", tone: "error" }])
  })

  test("falls back to a flat list in dependency order when lanes exceed the budget", () => {
    const snapshot = { ...base, nodes: ["r", "a", "b", "c", "a2", "b2", "c2", "z"].map((id) => node({ id })), edges: [
      ...["a", "b", "c"].map((id) => ({ from: "r", to: id })),
      ...["a", "b", "c"].map((id) => ({ from: id, to: `${id}2` })),
      { from: "r", to: "z" }, { from: "c2", to: "z" },
    ] }
    expect(text(snapshot)).toEqual(["◌ r", "◌ a", "◌ b", "◌ c", "◌ a2", "◌ b2", "◌ c2", "◌ z"])
  })

  test("marks a chain with └─ and indents each further link", () => {
    const snapshot = { ...base, nodes: ["a", "b", "c"].map((id) => node({ id })),
      edges: [{ from: "a", to: "b" }, { from: "b", to: "c" }] }
    expect(text(snapshot)).toEqual(["◌ a", "└─◌ b", "  └─◌ c"])
  })

  test("a long chain caps its indent and every row fits the sidebar", () => {
    const ids = Array.from({ length: 20 }, (_, i) => `unit-${i}-with-a-long-identity`)
    const snapshot = { ...base, nodes: ids.map((id) => node({ id })),
      edges: ids.slice(1).map((to, i) => ({ from: ids[i], to })) }
    const rows = text(snapshot)
    for (const row of rows) expect(Bun.stringWidth(row)).toBeLessThanOrEqual(37)
    expect(rows.at(-1)?.indexOf("└─")).toBe(rows.at(-2)?.indexOf("└─"))
    expect(rows.at(-1)?.indexOf("└─")).toBeGreaterThan(0)
  })

  test("a chain unit that forks stays in its lane, under its fork row", () => {
    const snapshot = { ...base, nodes: ["a", "b", "c", "d"].map((id) => node({ id })),
      edges: [{ from: "a", to: "b" }, { from: "b", to: "c" }, { from: "b", to: "d" }] }
    expect(text(snapshot)).toEqual(["◌ a", "◌ b", "├─┐", "◌ │ c", "  ◌ d"])
  })

  test("a unit beside an open lane, or not right below its parent, is not a chain link", () => {
    const snapshot = { ...base, nodes: ["r", "a", "b", "a2"].map((id) => node({ id })),
      edges: [{ from: "r", to: "a" }, { from: "r", to: "b" }, { from: "a", to: "a2" }] }
    expect(text(snapshot)).toEqual(["◌ r", "├─┐", "◌ │ a", "│ ◌ b", "◌ a2"])
  })

  test("never wraps: a long identity is cut to the sidebar's 37 columns", () => {
    const id = "an-identity-far-too-long-for-the-sidebar"
    const [row] = text({ ...base, nodes: [node({ id })], edges: [] })
    expect(row).toBe(`◌ ${id.slice(0, 34)}…`)
    expect(Bun.stringWidth(row)).toBe(37)
  })

  test("stacks disconnected graphs, hides withdrawn work, marks failures, lists running activities only", () => {
    const snapshot = { ...base, nodes: [
      node({ id: "pm", state: "settled", outcome: "failed" }), node({ id: "spec" }),
      node({ id: "solo", state: "in_flight" }), node({ id: "gone", state: "withdrawn" }),
    ], edges: [{ from: "pm", to: "spec" }], activities: [
      activity({ activity_id: "a1" }),
      activity({ activity_id: "a2", state: "settled", outcome: "completed" }),
    ] }
    expect(sidebarRows(snapshot)).toEqual([
      { text: "✗ pm", tone: "error" },
      { text: "└─◌ spec" },
      { text: "● solo" },
      { text: "● a1" },
    ])
  })

  test("an unmonitored activity keeps its row with the unmonitored glyph and no failure tone", () => {
    const snapshot = { ...base, nodes: [], edges: [], activities: [
      activity({ activity_id: "a1", flight: "unmonitored" }),
    ] }
    expect(sidebarRows(snapshot)).toEqual([{ text: "◐ a1" }])
  })
})

describe("hasContent / headerLine / sidebarDetail", () => {
  const base = { schema_version: 1, projection_revision: 3, history_position: 3, session_id: "s", capture: "current", edges: [], activities: [] } as const

  test("hasContent is true with nodes, activities, or neither", () => {
    expect(hasContent(layered({ ...base, nodes: [node()] }))).toBe(true)
    expect(hasContent(layered({ ...base, nodes: [], activities: [activity()] }))).toBe(true)
    expect(hasContent(layered({ ...base, nodes: [] }))).toBe(false)
    expect(hasContent(layered({ ...base, nodes: [node({ state: "withdrawn" })] }))).toBe(false)
  })

  test("headerLine reports the failure health without a snapshot", () => {
    expect(headerLine(undefined, "unavailable")).toBe("Takt DAG · unavailable")
  })
  const counts = (overrides: object = {}) => ({ planned: 0, in_flight: 0, completed: 0, failed: 0, backtracked: 0, interrupted: 0, withdrawn: 0, ...overrides })

  test("headerLine shows where the delegation stands, not internal revisions, for a current capture", () => {
    expect(headerLine(layered({ ...base, nodes: [], counts: counts(), plan_version: "plan-9" }), "confirmed")).toBe("Takt DAG")
    expect(headerLine(layered({ ...base, nodes: [], counts: counts({ completed: 7, in_flight: 1, planned: 2 }) }), "confirmed")).toBe("Takt DAG · 7 done · 1 in flight · 2 planned")
  })
  test("headerLine reports the view's health over the snapshot's capture when stale", () => {
    const snapshot = { ...base, nodes: [], counts: counts() }
    expect(headerLine(snapshot, "stale")).toBe("Takt DAG · stale")
  })

  test("countsLine leaves out a state with no unit and never counts a withdrawn one", () => {
    expect(countsLine(counts({ completed: 3, failed: 1, backtracked: 2, interrupted: 1, withdrawn: 4 }))).toBe("3 done · 1 failed · 2 backtracked · 1 interrupted")
  })

  test("the route and the sidebar read the same counts", () => {
    const snapshot = { ...base, nodes: [], counts: counts({ completed: 7, in_flight: 1, planned: 2, failed: 1 }) }
    expect(sidebarDetail(snapshot, "confirmed")).toBe("✓7 ●1 ◌2 ✗1")
    expect(headerLine(snapshot, "confirmed")).toBe("Takt DAG · 7 done · 1 in flight · 2 planned · 1 failed")
  })

  test("sidebarDetail shows the health that needs attention, otherwise the counts", () => {
    expect(sidebarDetail(undefined, "waiting")).toBe("waiting")
    expect(sidebarDetail(layered({ ...base, nodes: [], counts: counts() }), "confirmed")).toBe("")
    expect(sidebarDetail(layered({ ...base, nodes: [], counts: counts() }), "invalid")).toBe("invalid")
  })
})

// A bare <Show> under a <box> (no <text>-typed fallback) throws "Orphan text
// error" the moment its condition is false: Solid's off-state placeholder
// needs a <text> parent. These render the exact falsy branches that used to
// crash — no activities, no problem, no snapshot yet.
describe("crash regressions: falsy branches under a plain box", () => {
  const base = { schema_version: 1, projection_revision: 1, history_position: 1, session_id: "s", capture: "current", activities: [] } as const

  test("GraphView renders without throwing when there are no activities", async () => {
    const build = (activities: readonly DagActivity[]): Fixture =>
      ({ ...base, nodes: [node({ id: "a" }), node({ id: "b" })], edges: [{ from: "a", to: "b" }], activities })
    for (const activities of [[]]) {
      const setup = await testRender(() => GraphView({ snapshot: build(activities), mode: "route" }), { width: 60, height: 20 })
      try {
        await setup.renderOnce()
        const frame = setup.captureCharFrame()
        expect(frame).toContain("a")
        expect(frame).not.toContain("Activities")
      } finally {
        setup.renderer.destroy()
      }
    }
  })

  test("SidebarGraph renders nothing without throwing when there is no snapshot yet", async () => {
    const setup = await testRender(() => SidebarGraph({ snapshot: undefined }), { width: 60, height: 20 })
    try {
      await setup.renderOnce()
      expect(setup.captureCharFrame().trim()).toBe("")
    } finally {
      setup.renderer.destroy()
    }
  })

  test("SidebarGraph renders nothing without throwing for an empty DAG", async () => {
    const snapshot = { ...base, nodes: [], edges: [] }
    const setup = await testRender(() => SidebarGraph({ snapshot }), { width: 60, height: 20 })
    try {
      await setup.renderOnce()
      expect(setup.captureCharFrame().trim()).toBe("")
    } finally {
      setup.renderer.destroy()
    }
  })

  test("SidebarGraph renders the graph once there is content", async () => {
    const snapshot = { ...base, nodes: [node({ id: "solo" })], edges: [] }
    const setup = await testRender(() => SidebarGraph({ snapshot }), { width: 60, height: 20 })
    try {
      await setup.renderOnce()
      expect(setup.captureCharFrame()).toContain("solo")
    } finally {
      setup.renderer.destroy()
    }
  })
})
