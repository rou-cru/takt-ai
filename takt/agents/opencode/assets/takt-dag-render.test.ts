import { describe, expect, test } from "bun:test"
import { testRender } from "@opentui/solid"
import {
  activityBoxWidth,
  activityError,
  activityText,
  computeLayout,
  edgePorts,
  edgeRows,
  GraphView,
  glyphFor,
  hasContent,
  headerLine,
  nodeKindFor,
  nodeText,
  ordinal,
  sidebarDetail,
  sidebarRows,
  SidebarGraph,
  topologyError,
  topologyKey,
} from "./takt-dag"

const node = (overrides: Partial<{ id: string; state: string; flight: string; outcome: string; node_kind: string; launched: boolean; agent: string }> = {}) =>
  ({ id: "unit-1", state: "planned", launched: false, ...overrides }) as never

describe("ordinal", () => {
  test("orders strings lexicographically", () => {
    expect(ordinal("a", "b")).toBe(-1)
    expect(ordinal("b", "a")).toBe(1)
    expect(ordinal("a", "a")).toBe(0)
  })
})

describe("activityError", () => {
  test("accepts distinct, non-empty activity identities", () => {
    expect(activityError([{ activity_id: "a", node_kind: "orchestrator", state: "in_flight" } as never, { activity_id: "b", node_kind: "maintenance", state: "settled" } as never])).toBeUndefined()
  })
  test("rejects a duplicate activity identity", () => {
    expect(activityError([{ activity_id: "a", node_kind: "orchestrator", state: "in_flight" } as never, { activity_id: "a", node_kind: "maintenance", state: "settled" } as never])).toBe("duplicate or empty activity identity")
  })
  test("rejects an empty activity identity", () => {
    expect(activityError([{ activity_id: "", node_kind: "orchestrator", state: "in_flight" } as never])).toBe("duplicate or empty activity identity")
  })
})

describe("topologyError", () => {
  const base = { schema_version: 1, projection_revision: 1, history_position: 1, session_id: "s", capture: "current" } as const

  test("accepts a valid acyclic snapshot", () => {
    const snapshot = { ...base, nodes: [node({ id: "a" }), node({ id: "b" })], edges: [{ from: "a", to: "b" }] } as never
    expect(topologyError(snapshot)).toBeUndefined()
  })
  test("rejects a duplicate node identity", () => {
    const snapshot = { ...base, nodes: [node({ id: "a" }), node({ id: "a" })], edges: [] } as never
    expect(topologyError(snapshot)).toBe("duplicate node identity")
  })
  test("rejects an edge naming an unknown node", () => {
    const snapshot = { ...base, nodes: [node({ id: "a" })], edges: [{ from: "a", to: "ghost" }] } as never
    expect(topologyError(snapshot)).toBe("edge a -> ghost names an unknown node")
  })
  test("rejects a prerequisite cycle", () => {
    const snapshot = { ...base, nodes: [node({ id: "a" }), node({ id: "b" })], edges: [{ from: "a", to: "b" }, { from: "b", to: "a" }] } as never
    expect(topologyError(snapshot)).toBe("prerequisite cycle")
  })
  test("rejects an invalid activity even with valid nodes", () => {
    const snapshot = { ...base, nodes: [node({ id: "a" })], edges: [], activities: [{ activity_id: "", node_kind: "orchestrator", state: "in_flight" }] } as never
    expect(topologyError(snapshot)).toBe("duplicate or empty activity identity")
  })
})

describe("topologyKey", () => {
  test("is order-independent and reflects node kind and edges", () => {
    const forward = { nodes: [node({ id: "a" }), node({ id: "b" })], edges: [{ from: "a", to: "b" }] } as never
    const reordered = { nodes: [node({ id: "b" }), node({ id: "a" })], edges: [{ from: "a", to: "b" }] } as never
    expect(topologyKey(forward)).toBe(topologyKey(reordered))
  })
  test("changes when the edge set changes", () => {
    const withEdge = { nodes: [node({ id: "a" }), node({ id: "b" })], edges: [{ from: "a", to: "b" }] } as never
    const withoutEdge = { nodes: [node({ id: "a" }), node({ id: "b" })], edges: [] } as never
    expect(topologyKey(withEdge)).not.toBe(topologyKey(withoutEdge))
  })
})

describe("graph layout", () => {
  const base = { schema_version: 1, projection_revision: 1, history_position: 1, session_id: "s", capture: "current" } as const

  test("lays out branching depth and layer-skipping edges with deterministic routes", () => {
    const graph = {
      ...base,
      nodes: [node({ id: "a" }), node({ id: "b" }), node({ id: "c" }), node({ id: "d" })],
      edges: [{ from: "a", to: "b" }, { from: "b", to: "c" }, { from: "a", to: "c" }, { from: "c", to: "d" }],
    } as never
    const layout = computeLayout(graph)

    expect([...layout.nodes.keys()]).toEqual(["a", "b", "c", "d"])
    expect(layout.nodes.get("a")?.depth).toBe(0)
    expect(layout.nodes.get("c")?.depth).toBe(2)
    expect(layout.paths.map((path) => path.length)).toEqual([3, 4, 8, 3])
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
  }) as never

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

  test("falls back to one band when the bands do not fit the height", () => {
    const layout = computeLayout(chain(6), undefined, { width: 60, height: 5 })
    expect(layout.bands).toBe(1)
    expect(layout.width).toBe(computeLayout(chain(6)).width)
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
      edges: [{ from: "a", to: "b" }, { from: "b", to: "c" }, { from: "c", to: "d" }, { from: "d", to: "e" }, { from: "a", to: "e" }] } as never
    const layout = computeLayout(skip, undefined, { width: 30, height: 100 })
    expect(layout.bands).toBeGreaterThan(1)
    expect(edgePorts(layout)).toHaveLength(5)
    expect(edgeRows(layout).every((row) => row.length === layout.width)).toBe(true)
  })

  test("handles an empty graph and a graph without edges", () => {
    const empty = computeLayout({ ...base, nodes: [], edges: [] } as never)
    expect(empty.nodes.size).toBe(0)
    expect(empty.paths).toEqual([])
    expect(edgeRows(empty).every((row) => row.trim() === "")).toBe(true)
    expect(edgePorts(empty)).toEqual([])

    const isolated = computeLayout({ ...base, nodes: [node({ id: "alone" })], edges: [] } as never)
    expect(isolated.nodes.get("alone")?.depth).toBe(0)
    expect(isolated.paths).toEqual([])
    expect(edgeRows(isolated).every((row) => row.trim() === "")).toBe(true)
  })

})

describe("node presentation", () => {
  test("nodeKindFor defaults to delegated when node_kind is absent", () => {
    expect(nodeKindFor(node())).toBe("delegated")
    expect(nodeKindFor(node({ node_kind: "delegated" }))).toBe("delegated")
  })
  test("glyphFor covers every state/flight/outcome combination", () => {
    expect(glyphFor(node({ state: "planned" }))).toBe("◌")
    expect(glyphFor(node({ state: "withdrawn" }))).toBe("×")
    expect(glyphFor(node({ state: "settled", outcome: "failed" }))).toBe("✗")
    expect(glyphFor(node({ state: "settled", outcome: "backtracked" }))).toBe("↩")
    expect(glyphFor(node({ state: "settled", outcome: "interrupted" }))).toBe("⊘")
    expect(glyphFor(node({ state: "settled" }))).toBe("✓")
    expect(glyphFor(node({ state: "in_flight", flight: "pending_launch" }))).toBe("○")
    expect(glyphFor(node({ state: "in_flight", flight: "uncertain" }))).toBe("!")
    expect(glyphFor(node({ state: "in_flight", flight: "suspended" }))).toBe("⏸")
    expect(glyphFor(node({ state: "in_flight", flight: "cancellation_pending" }))).toBe("↯")
    expect(glyphFor(node({ state: "in_flight" }))).toBe("●")
  })

  test("nodeText composes the kind glyph, state glyph, kind label and id", () => {
    expect(nodeText(node({ id: "unit-7", state: "settled" }))).toBe("✓ unit-7")
  })

  test("renders activity-only and layered graph views in both layouts", async () => {
    const base = { schema_version: 1, projection_revision: 1, history_position: 1, session_id: "s", capture: "current" } as const
    const activity = { activity_id: "review-1", node_kind: "orchestrator", state: "in_flight", flight: "observed_running" }
    const snapshots = [
      { ...base, nodes: [], edges: [], activities: [activity] },
      { ...base, nodes: [node({ id: "root" }), node({ id: "child", state: "in_flight" })], edges: [{ from: "root", to: "child" }], activities: [activity] },
    ]
    for (const mode of ["route", "sidebar"] as const) {
      for (const snapshot of snapshots) {
        const setup = await testRender(() => GraphView({ snapshot: snapshot as never, mode }) as never, { width: 100, height: 30 })
        try {
          await setup.renderOnce()
          const frame = setup.captureCharFrame()
          expect(frame).toContain("review-1")
          if (snapshot.nodes.length > 0) expect(frame).toContain("root")
        } finally {
          setup.renderer.destroy()
        }
      }
    }
  })
})

describe("activity presentation", () => {
  test("activityText names running vs settled activities by lane", () => {
    expect(activityText({ activity_id: "a1", node_kind: "orchestrator", state: "in_flight" } as never)).toBe("◆ direct activity a1 · running")
    expect(activityText({ activity_id: "a2", node_kind: "maintenance", state: "settled", outcome: "completed" } as never)).toBe("◇ GC activity a2 · completed")
    expect(activityText({ activity_id: "a3", node_kind: "maintenance", state: "settled" } as never)).toBe("◇ GC activity a3 · settled")
  })
  test("activityBoxWidth is the text width plus two border columns", () => {
    const activity = { activity_id: "a1", node_kind: "orchestrator", state: "in_flight" } as never
    expect(activityBoxWidth(activity)).toBe(activityText(activity).length + 2)
  })
})

describe("sidebar rows", () => {
  const base = { schema_version: 1, projection_revision: 1, history_position: 1, session_id: "s", capture: "current" } as const
  const text = (snapshot: never) => sidebarRows(snapshot).map((row) => row.text)
  const diamond = [{ from: "intent-pm", to: "experience-design" }, { from: "intent-pm", to: "structure-arch" },
    { from: "experience-design", to: "behavior-spec" }, { from: "structure-arch", to: "behavior-spec" }]

  test("draws a planned diamond as lanes: the glyph is the node, edges are never named", () => {
    const snapshot = { ...base, edges: diamond,
      nodes: ["intent-pm", "experience-design", "structure-arch", "behavior-spec"].map((id) => node({ id })) } as never
    expect(text(snapshot)).toEqual([
      "◌ intent-pm",
      "├─┐",
      "◌ │ experience-design",
      "│ ◌ structure-arch",
      "├─┘",
      "◌ behavior-spec",
    ])
  })

  test("keeps completed units in the graph, muted; only the glyph tells the state", () => {
    const snapshot = { ...base, edges: diamond, nodes: [
      node({ id: "intent-pm", state: "settled", outcome: "completed", agent: "pm" }),
      node({ id: "experience-design", state: "settled", outcome: "completed", agent: "design" }),
      node({ id: "structure-arch", state: "in_flight", flight: "observed_running", agent: "arch" }),
      node({ id: "behavior-spec" }),
    ] } as never
    expect(sidebarRows(snapshot)).toEqual([
      { text: "✓ intent-pm", tone: "muted" },
      { text: "├─┐" },
      { text: "✓ │ experience-design", tone: "muted" },
      { text: "│ ● structure-arch" },
      { text: "├─┘" },
      { text: "◌ behavior-spec" },
    ])
  })

  test("folds the oldest rows into a leading count when the graph exceeds the row budget", () => {
    const ids = Array.from({ length: 30 }, (_, i) => `u${String(i).padStart(2, "0")}`)
    const snapshot = { ...base, nodes: ids.map((id) => node({ id })), edges: [] } as never
    const rows = sidebarRows(snapshot)
    expect(rows).toHaveLength(16)
    expect(rows[0]).toEqual({ text: "… 15 more", tone: "muted" })
    expect(rows.at(-1)?.text).toBe("◌ u29")
  })

  test("groups a layer wider than the lane budget into one row", () => {
    const wide = ["b", "c", "d", "e"]
    const snapshot = { ...base, nodes: ["a", ...wide, "f"].map((id) => node({ id })),
      edges: wide.flatMap((id) => [{ from: "a", to: id }, { from: id, to: "f" }]) } as never
    expect(text(snapshot)).toEqual(["◌ a", "└─≡ ◌◌◌◌ 4 parallel", "  └─◌ f"])
  })

  test("mutes a group only once every unit in it is completed", () => {
    const wide = ["b", "c", "d", "e"]
    const done = { state: "settled", outcome: "completed", agent: "x" }
    const group = (nodes: object[]) => sidebarRows({ ...base, nodes: [node({ id: "a", ...done }), ...nodes],
      edges: wide.map((id) => ({ from: "a", to: id })) } as never)[1]
    expect(group(wide.map((id) => node({ id, ...done })))).toEqual({ text: "└─≡ ✓✓✓✓ 4 parallel", tone: "muted" })
    expect(group(wide.map((id, i) => node({ id, ...(i ? done : {}) })))).toEqual({ text: "└─≡ ◌✓✓✓ 4 parallel" })
  })

  test("falls back to a flat list in dependency order when lanes exceed the budget", () => {
    const snapshot = { ...base, nodes: ["r", "a", "b", "c", "a2", "b2", "c2", "z"].map((id) => node({ id })), edges: [
      ...["a", "b", "c"].map((id) => ({ from: "r", to: id })),
      ...["a", "b", "c"].map((id) => ({ from: id, to: `${id}2` })),
      { from: "r", to: "z" }, { from: "c2", to: "z" },
    ] } as never
    expect(text(snapshot)).toEqual(["◌ r", "◌ a", "◌ b", "◌ c", "◌ a2", "◌ b2", "◌ c2", "◌ z"])
  })

  test("marks a chain with └─ and indents each further link", () => {
    const snapshot = { ...base, nodes: ["a", "b", "c"].map((id) => node({ id })),
      edges: [{ from: "a", to: "b" }, { from: "b", to: "c" }] } as never
    expect(text(snapshot)).toEqual(["◌ a", "└─◌ b", "  └─◌ c"])
  })

  test("a long chain caps its indent and every row fits the sidebar", () => {
    const ids = Array.from({ length: 20 }, (_, i) => `unit-${i}-with-a-long-identity`)
    const snapshot = { ...base, nodes: ids.map((id) => node({ id })),
      edges: ids.slice(1).map((to, i) => ({ from: ids[i], to })) } as never
    const rows = text(snapshot)
    for (const row of rows) expect(Bun.stringWidth(row)).toBeLessThanOrEqual(37)
    expect(rows.at(-1)?.indexOf("└─")).toBe(rows.at(-2)?.indexOf("└─"))
    expect(rows.at(-1)?.indexOf("└─")).toBeGreaterThan(0)
  })

  test("a chain unit that forks stays in its lane, under its fork row", () => {
    const snapshot = { ...base, nodes: ["a", "b", "c", "d"].map((id) => node({ id })),
      edges: [{ from: "a", to: "b" }, { from: "b", to: "c" }, { from: "b", to: "d" }] } as never
    expect(text(snapshot)).toEqual(["◌ a", "◌ b", "├─┐", "◌ │ c", "  ◌ d"])
  })

  test("a unit beside an open lane, or not right below its parent, is not a chain link", () => {
    const snapshot = { ...base, nodes: ["r", "a", "b", "a2"].map((id) => node({ id })),
      edges: [{ from: "r", to: "a" }, { from: "r", to: "b" }, { from: "a", to: "a2" }] } as never
    expect(text(snapshot)).toEqual(["◌ r", "├─┐", "◌ │ a", "│ ◌ b", "◌ a2"])
  })

  test("never wraps: a long identity is cut to the sidebar's 37 columns", () => {
    const id = "an-identity-far-too-long-for-the-sidebar"
    const [row] = text({ ...base, nodes: [node({ id })], edges: [] } as never)
    expect(row).toBe(`◌ ${id.slice(0, 34)}…`)
    expect(Bun.stringWidth(row)).toBe(37)
  })

  test("stacks disconnected graphs, hides withdrawn work, marks failures, lists running activities only", () => {
    const snapshot = { ...base, nodes: [
      node({ id: "pm", state: "settled", outcome: "failed" }), node({ id: "spec" }),
      node({ id: "solo", state: "in_flight" }), node({ id: "gone", state: "withdrawn" }),
    ], edges: [{ from: "pm", to: "spec" }], activities: [
      { activity_id: "a1", node_kind: "orchestrator", state: "in_flight" },
      { activity_id: "a2", node_kind: "maintenance", state: "settled", outcome: "completed" },
    ] } as never
    expect(sidebarRows(snapshot)).toEqual([
      { text: "✗ pm", tone: "error" },
      { text: "└─◌ spec" },
      { text: "● solo" },
      { text: "◆ direct activity" },
    ])
  })
})

describe("hasContent / headerLine / sidebarDetail", () => {
  const base = { schema_version: 1, projection_revision: 3, history_position: 3, session_id: "s", capture: "current", edges: [] } as const

  test("hasContent is true with nodes, activities, or neither", () => {
    expect(hasContent({ ...base, nodes: [node()] } as never)).toBe(true)
    expect(hasContent({ ...base, nodes: [], activities: [{ activity_id: "a" }] } as never)).toBe(true)
    expect(hasContent({ ...base, nodes: [] } as never)).toBe(false)
  })

  test("headerLine reports the failure health without a snapshot", () => {
    expect(headerLine(undefined, "unavailable")).toBe("Takt DAG · unavailable")
  })
  test("headerLine shows progress, not internal revisions, for a current capture", () => {
    expect(headerLine({ ...base, nodes: [], plan_version: "plan-9" } as never, "confirmed")).toBe("Takt DAG")
    const nodes = [node({ id: "a", state: "settled" }), node({ id: "b", state: "in_flight" }), node({ id: "c", state: "planned" })]
    expect(headerLine({ ...base, nodes } as never, "confirmed")).toBe("Takt DAG · 1/3 done · 1 running")
  })
  test("headerLine reports the view's health over the snapshot's capture when stale", () => {
    const snapshot = { ...base, nodes: [] } as never
    expect(headerLine(snapshot, "stale")).toBe("Takt DAG · stale")
  })

  test("sidebarDetail shows the health that needs attention, otherwise completed over live units", () => {
    expect(sidebarDetail(undefined, "waiting")).toBe("waiting")
    expect(sidebarDetail({ ...base, nodes: [] } as never, "confirmed")).toBe("")
    expect(sidebarDetail({ ...base, nodes: [] } as never, "invalid")).toBe("invalid")
    const nodes = [node({ id: "a", state: "settled" }), node({ id: "b", state: "in_flight" }), node({ id: "c", state: "withdrawn" })]
    expect(sidebarDetail({ ...base, nodes } as never, "confirmed")).toBe("1/2")
  })
})

// A bare <Show> under a <box> (no <text>-typed fallback) throws "Orphan text
// error" the moment its condition is false: Solid's off-state placeholder
// needs a <text> parent. These render the exact falsy branches that used to
// crash — no activities, no problem, no snapshot yet.
describe("crash regressions: falsy branches under a plain box", () => {
  const base = { schema_version: 1, projection_revision: 1, history_position: 1, session_id: "s", capture: "current" } as const

  test("GraphView renders without throwing when there are no activities", async () => {
    const build = (activities: unknown) =>
      ({ ...base, nodes: [node({ id: "a" }), node({ id: "b" })], edges: [{ from: "a", to: "b" }], activities }) as never
    for (const activities of [undefined, []]) {
      const setup = await testRender(() => GraphView({ snapshot: build(activities), mode: "route" }) as never, { width: 60, height: 20 })
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
    const setup = await testRender(() => SidebarGraph({ snapshot: undefined }) as never, { width: 60, height: 20 })
    try {
      await setup.renderOnce()
      expect(setup.captureCharFrame().trim()).toBe("")
    } finally {
      setup.renderer.destroy()
    }
  })

  test("SidebarGraph renders nothing without throwing for an empty DAG", async () => {
    const snapshot = { ...base, nodes: [], edges: [] } as never
    const setup = await testRender(() => SidebarGraph({ snapshot }) as never, { width: 60, height: 20 })
    try {
      await setup.renderOnce()
      expect(setup.captureCharFrame().trim()).toBe("")
    } finally {
      setup.renderer.destroy()
    }
  })

  test("SidebarGraph renders the graph once there is content", async () => {
    const snapshot = { ...base, nodes: [node({ id: "solo" })], edges: [] } as never
    const setup = await testRender(() => SidebarGraph({ snapshot }) as never, { width: 60, height: 20 })
    try {
      await setup.renderOnce()
      expect(setup.captureCharFrame()).toContain("solo")
    } finally {
      setup.renderer.destroy()
    }
  })
})
