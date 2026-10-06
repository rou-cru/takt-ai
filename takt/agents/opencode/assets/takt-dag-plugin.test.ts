import { afterAll, beforeAll, describe, expect, mock, test } from "bun:test"
import * as solid from "solid-js"
import { testRender } from "@opentui/solid"
import dagPlugin from "./takt-dag"

const FRAME = { width: 100, height: 40 }
const SETTLE_ATTEMPTS = 100
const SETTLE_STEP_MS = 2

type Reply = { out?: string; err?: string; code?: number; hang?: boolean }
type Rendered = { data?: { sessionID?: string } }
type Slot = { append?: string; prepend?: string; render: (input: { sessionID: string }) => unknown }

const sleep = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms))
const until = async (ready: () => boolean, attempts = SETTLE_ATTEMPTS): Promise<void> => {
  if (ready() || attempts === 0) return
  await sleep(SETTLE_STEP_MS)
  return until(ready, attempts - 1)
}

// Under Bun's node condition solid-js resolves to its server build, where
// effects, mounts and cleanups never run. The views' lifecycle is what these
// tests exercise, so effects and mounts run once, immediately, and cleanups are
// kept for the test to run when it unmounts. Signals are plain in that build, so
// a view shows the state as of its render: the tests remount to read the result
// of a poll, which is the documented way a view picks up the last snapshot.
const cleanups: Array<() => void> = []
const unmount = () => cleanups.splice(0).forEach((cleanup) => cleanup())
beforeAll(() => {
  mock.module("solid-js", () => ({
    ...solid,
    createEffect: (run: () => void) => { run() },
    onMount: (run: () => void) => { run() },
    onCleanup: (run: () => void) => { cleanups.push(run) },
  }))
})
afterAll(() => {
  mock.module("solid-js", () => solid)
})
const snapshot = (extra: Record<string, unknown> = {}) => ({
  schema_version: 1, projection_revision: 4, history_position: 4, session_id: "root", capture: "current", plan_version: "plan-7",
  nodes: [{ id: "unit-a", state: "settled", outcome: "completed", launched: true }], edges: [], activities: [], ...extra,
})
const reply = (value: unknown): Reply => ({ out: JSON.stringify(value) })

// startDag mounts the real plugin against a scripted takt-ai; each poll is one
// spawn, so the recorded argv shows exactly what the view asked for.
function startDag(respond: () => Reply) {
  const runtime = globalThis as unknown as { Bun: { spawn: (...args: never[]) => unknown } }
  const originalSpawn = runtime.Bun.spawn
  const spawned: string[][] = []
  const killed: number[] = []
  let route: { render: (input: Rendered) => unknown } | undefined
  let sidebar: Slot | undefined
  let app: Slot | undefined
  const layers: Array<() => { commands?: Array<{ id?: string; bind?: string; slash?: { name: string }; run: () => void }> }> = []
  const navigated: unknown[] = []
  runtime.Bun.spawn = ((argv: string[]) => {
    spawned.push(argv)
    const answer = respond()
    return { stdout: answer.out ?? "", stderr: answer.err ?? "", exited: answer.hang ? new Promise<number>(() => {}) : Promise.resolve(answer.code ?? 0), kill() { killed.push(spawned.length) } }
  }) as typeof originalSpawn
  const cleanup = dagPlugin.setup({
    location: { directory: "/workspace" },
    data: { location: { default: () => ({ directory: "/workspace" }) }, session: { root: (id: string) => id } },
    theme: { text: { base: "white", muted: "gray", feedback: { error: { base: "red" } } } },
    ui: {
      router: { register(value: typeof route) { route = value; return () => {} }, current: () => ({ type: "session", sessionID: "root" }), navigate(destination: unknown) { navigated.push(destination) } },
      slot(value: Slot) {
        if (value.prepend === "sidebar.content") sidebar = value
        if (value.append === "app") app = value
        return () => {}
      },
    },
    keymap: { layer: (input: (typeof layers)[number]) => { layers.push(input) } },
  } as never)
  return {
    spawned, killed, layers, navigated,
    route: (sessionID: string | undefined) => route!.render({ data: { sessionID } }),
    sidebar: (sessionID: string) => sidebar!.render({ sessionID }),
    app: () => app!.render({ sessionID: "root" }),
    stop() {
      if (typeof cleanup === "function") cleanup()
      runtime.Bun.spawn = originalSpawn
    },
  }
}

// visit mounts a view, lets its poll land, unmounts it, and remounts to read
// what the poll left behind.
async function visit(respond: () => Reply, view: (dag: ReturnType<typeof startDag>) => unknown) {
  const dag = startDag(respond)
  try {
    const first = await testRender(() => view(dag) as never, FRAME)
    await until(() => dag.spawned.length > 0)
    await sleep(SETTLE_STEP_MS * 5)
    first.renderer.destroy()
    unmount()
    const second = await testRender(() => view(dag) as never, FRAME)
    try {
      await second.renderOnce()
      return { frame: second.captureCharFrame(), spawned: dag.spawned }
    } finally {
      second.renderer.destroy()
      unmount()
    }
  } finally {
    dag.stop()
  }
}

describe("DAG route view", () => {
  test("polls the session's snapshot and shows its header, graph and legend", async () => {
    const { frame, spawned } = await visit(() => reply(snapshot()), (d) => d.route("root"))
    expect(frame).toContain("Takt DAG · 1/1 done")
    expect(frame).toContain("unit-a")
    expect(frame).not.toContain("projection")
    expect(spawned[0]).toEqual([
      "__TAKT_AI_BINARY__", "dag", "status", "--workspace", "/workspace",
      "--state", "/workspace/.takt-ai/vfs", "--session", "root", "--format", "json",
    ] as never)
  })

  test("a failing query with nothing to keep leaves the DAG unavailable and says why", async () => {
    const { frame } = await visit(() => ({ code: 2, err: " store locked\n" }), (d) => d.route("root"))
    expect(frame).toContain("Takt DAG · unavailable")
    expect(frame).toContain("store locked")
    expect(frame).toContain("no confirmed projection")
  })

  test("an exit code without stderr is reported by its code", async () => {
    const { frame } = await visit(() => ({ code: 9 }), (d) => d.route("root"))
    expect(frame).toContain("dag status exited 9")
  })

  test("output that is not JSON is reported as a failed query", async () => {
    const { frame } = await visit(() => ({ out: "not json" }), (d) => d.route("root"))
    expect(frame).toContain("Takt DAG · unavailable")
    expect(frame).toContain("no confirmed projection")
  })

  test("a snapshot that does not match the wire contract is reported as a failed query", async () => {
    const { frame } = await visit(() => reply({ capture: "current", nodes: [], edges: [] }), (d) => d.route("root"))
    expect(frame).toContain("invalid DAG snapshot JSON")
    expect(frame).toContain("Takt DAG · unavailable")
  })

  test("takt answering that it cannot project is unavailable, not a failure", async () => {
    const { frame } = await visit(() => reply({ capture: "unavailable" }), (d) => d.route("root"))
    expect(frame).toContain("takt cannot provide a projection")
    expect(frame).toContain("Takt DAG · unavailable")
  })

  test("a snapshot with a broken topology is rejected as invalid", async () => {
    const twins = snapshot({ nodes: [{ id: "a", state: "planned", launched: false }, { id: "a", state: "planned", launched: false }] })
    const { frame } = await visit(() => reply(twins), (d) => d.route("root"))
    expect(frame).toContain("projection rejected: duplicate node identity")
    expect(frame).toContain("Takt DAG · invalid")
    expect(frame).toContain("no confirmed projection")
  })

  test("a confirmed empty DAG says there is no planned work", async () => {
    const { frame } = await visit(() => reply(snapshot({ capture: "empty", nodes: [] })), (d) => d.route("root"))
    expect(frame).toContain("Takt DAG · empty")
    expect(frame).toContain("empty DAG · no planned work")
  })

  test("waits for a session before asking takt anything", async () => {
    const { frame, spawned } = await visit(() => reply(snapshot()), (d) => d.route(undefined))
    expect(frame).toContain("Takt DAG · waiting")
    expect(frame).toContain("waiting for confirmed projection")
    expect(spawned).toEqual([])
  })
})

describe("DAG route entry", () => {
  test("<leader>d and /takt-dag open the route for the current session", () => {
    const dag = startDag(() => ({ hang: true }))
    try {
      dag.app()
      const open = dag.layers.flatMap((layer) => layer().commands ?? []).find((command) => command.id === "takt.dag.open")
      expect(open?.bind).toBe("<leader>d")
      expect(open?.slash?.name).toBe("takt-dag")
      open!.run()
      expect(dag.navigated).toEqual([{ type: "plugin", name: "takt.dag", data: { sessionID: "root" } }])
    } finally { dag.stop() }
  })
})

describe("DAG route exit", () => {
  test("Esc on the full-screen route returns to the session it was opened from", async () => {
    const dag = startDag(() => ({ hang: true }))
    try {
      const view = await testRender(() => dag.route("root") as never, FRAME)
      const close = dag.layers.flatMap((layer) => layer().commands ?? []).find((command) => command.id === "takt.dag.close")
      expect(close?.bind).toBe("escape")
      const legend = dag.layers.flatMap((layer) => layer().commands ?? []).find((command) => command.id === "takt.dag.legend")
      expect(legend?.bind).toBe("?")
      close!.run()
      expect(dag.navigated).toEqual([{ type: "session", sessionID: "root" }])
      view.renderer.destroy()
      unmount()
    } finally { dag.stop() }
  })
})

describe("DAG sidebar view", () => {
  test("shows the one-line header and the graph of the confirmed projection", async () => {
    const { frame, spawned } = await visit(() => reply(snapshot()), (d) => d.sidebar("root"))
    expect(frame).toContain("DAG 1/1")
    expect(frame).toContain("✓ unit-a")
    expect(spawned[0]).toContain("root")
  })

  test("shows only the health and the problem while nothing is confirmed", async () => {
    const { frame } = await visit(() => ({ code: 1, err: "no store" }), (d) => d.sidebar("root"))
    expect(frame).toContain("DAG unavailable")
    expect(frame).toContain("no store")
    expect(frame).not.toContain("unit-a")
  })
})

describe("DAG polling lifetime", () => {
  test("views share one poll, and the last one out kills the query still in flight", async () => {
    const dag = startDag(() => ({ hang: true }))
    try {
      const route = await testRender(() => dag.route("root") as never, FRAME)
      const sidebar = await testRender(() => dag.sidebar("root") as never, FRAME)
      await until(() => dag.spawned.length > 0)
      expect(dag.spawned).toHaveLength(1)
      route.renderer.destroy()
      sidebar.renderer.destroy()
      expect(dag.killed).toEqual([])
      unmount()
      expect(dag.killed).toEqual([1])
    } finally { dag.stop() }
  })

  test("following another session drops the query still in flight and asks for the new one", async () => {
    const dag = startDag(() => ({ hang: true }))
    try {
      const first = await testRender(() => dag.route("root") as never, FRAME)
      await until(() => dag.spawned.length > 0)
      const second = await testRender(() => dag.sidebar("other") as never, FRAME)
      await until(() => dag.spawned.length > 1)
      expect(dag.killed).toEqual([1])
      expect(dag.spawned[1]).toContain("other")
      expect(dag.spawned[1]).not.toContain("root")
      first.renderer.destroy()
      second.renderer.destroy()
      unmount()
    } finally { dag.stop() }
  })
})
