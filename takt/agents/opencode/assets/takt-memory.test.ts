import { afterAll, beforeAll, describe, expect, mock, test } from "bun:test"
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs"
import { tmpdir } from "node:os"
import { join, resolve } from "node:path"

// The plugin is rendered the way the deployment renders it: its placeholders replaced by the crew
// roster and a socket of this test's own, so the hook runs against a fake /admin on a real Unix socket.
const WORKSPACE = "/workspace"
const KEY = "ws-key"
const ROSTER = ["takt", "dev", "qa", "tpm", "verify"]
const dir = mkdtempSync(join(tmpdir(), "takt-memory-test-"))
const SOCKET = join(dir, "memory.sock")
const rendered = resolve(import.meta.dir, `takt-memory.rendered-${process.pid}.ts`)
let memoryPlugin: { setup: (ctx: never) => Promise<unknown> }

beforeAll(async () => {
  const source = readFileSync(resolve(import.meta.dir, "takt-memory.ts"), "utf8")
    .replace(`"__TAKT_MEMORY_WIRE__"`, JSON.stringify({ tool_prefix: "takt_memory_", record: "record", bind: "bind", describe: "describe", admin_path: "/admin" }))
    .replace(`"__TAKT_CREW_ROSTER__"`, JSON.stringify(ROSTER))
    .replace(`"__TAKT_AI_BINARY__"`, JSON.stringify("takt-ai"))
  writeFileSync(rendered, source)
  memoryPlugin = (await import(rendered)).default
})
afterAll(() => { rmSync(rendered, { force: true }); rmSync(dir, { recursive: true, force: true }) })

type Described = { id: number; exists: boolean; title?: string; outgoing?: Array<{ entry: number; kind: string }> }
type AdminCall = { tool: string; args: Record<string, unknown> }
type HookEvent = { tool: string; sessionID: string; agent: string; messageID: string; id: string; input: unknown }

// fakeAdmin answers /admin as the service does: it requires the headers the Streamable HTTP transport requires.
function fakeAdmin(entries: Described[]) {
  const calls: AdminCall[] = []
  const server = Bun.serve({
    unix: SOCKET,
    async fetch(req) {
      const accept = req.headers.get("accept") ?? ""
      if (req.headers.get("content-type") !== "application/json" || !accept.includes("application/json") || !accept.includes("text/event-stream")) return new Response("bad headers", { status: 400 })
      const body = await req.json() as { id: string; params: { name: string; arguments: Record<string, unknown> } }
      calls.push({ tool: body.params.name, args: body.params.arguments })
      const structured = body.params.name === "describe"
        ? { entries: (body.params.arguments.ids as number[]).map(id => entries.find(e => e.id === id) ?? { id, exists: false }) }
        : {}
      return Response.json({ jsonrpc: "2.0", id: body.id, result: { content: [{ type: "text", text: "{}" }], structuredContent: structured } })
    },
  })
  return { calls, stop: () => server.stop(true) }
}

type Options = {
  entries?: Described[]
  sessions?: Record<string, { parentID?: string }>
  effects?: Array<"allow" | "deny">
  // Whether the service is already running when the plugin starts; otherwise `start` brings it up on the nth run.
  running?: boolean
  startsService?: boolean
}

async function startMemory(options: Options = {}) {
  const runtime = globalThis as unknown as { Bun: { spawn: (...args: never[]) => unknown } }
  const originalSpawn = runtime.Bun.spawn
  const spawns: string[][] = []
  const cwds: Array<string | undefined> = []
  let admin = options.running === false ? undefined : fakeAdmin(options.entries ?? [])
  const created: Array<{ action: string; resources: string[]; metadata: Record<string, unknown> }> = []
  const effects = [...(options.effects ?? [])]
  mock.module("@opencode/client/service", () => ({ Service: { discover: async () => ({ url: "http://service.invalid" }), headers: () => ({}) } }))
  mock.module("@opencode/client", () => ({
    OpenCode: { make: () => ({ permission: { create: async (request: { id: string; action: string; resources: string[]; metadata: Record<string, unknown> }) => {
      created.push(request)
      return { effect: effects.shift() ?? "allow", id: request.id }
    } } }) },
  }))
  runtime.Bun.spawn = ((argv: string[], spawnOptions: { cwd?: string }) => {
    spawns.push(argv)
    cwds.push(spawnOptions.cwd)
    if (options.startsService && spawns.length > 1) admin = fakeAdmin(options.entries ?? [])
    return { stdout: `${JSON.stringify({ workspace: KEY, socket: SOCKET })}\n`, stderr: "", exited: Promise.resolve(0) }
  }) as typeof originalSpawn
  let hook: (event: HookEvent) => Promise<void> = async () => {}
  const hookNames: string[] = []
  const cleanup = await memoryPlugin.setup({
    location: { directory: WORKSPACE },
    session: { async get({ sessionID }: { sessionID: string }) { return { id: sessionID, ...options.sessions?.[sessionID] } } },
    tool: { async hook(name: string, callback: typeof hook) { hookNames.push(name); hook = callback; return { dispose() {} } } },
    event: { subscribe() { return { [Symbol.asyncIterator]() { return { next: () => new Promise<never>(() => {}) } } } } },
  } as never)
  return {
    spawns, cwds, created, hookNames, hook: (event: HookEvent) => hook(event),
    calls: () => admin?.calls ?? [],
    run: (tool: string, input: unknown, agent = "dev", sessionID = "root") =>
      hook({ tool: `takt_memory_${tool}`, sessionID, agent, messageID: "message", id: "tool-call", input }),
    stop() { if (typeof cleanup === "function") cleanup(); admin?.stop(); runtime.Bun.spawn = originalSpawn },
  }
}

const record = (extra: Record<string, unknown> = {}) => ({ nature: "observation", scope: "workspace", title: "t", content: "c", evidence: "e", ...extra })
const globalRecord = record({ scope: "global", nature: "decision", title: "prefers tabs", content: "always tabs" })
const MATRIX = "Traceability Matrix: ship it"

describe("binding", () => {
  test("every memory call binds its caller first and no process runs per call", async () => {
    const memory = await startMemory({ sessions: { child: { parentID: "mid" }, mid: { parentID: "top" } } })
    try {
      await memory.run("search", { query: "x", scope: "workspace" }, "dev", "child")
      await memory.run("read", { id: 1 }, "dev", "child")
      await memory.run("record", record(), "takt", "top")
      expect(memory.hookNames).toEqual(["execute.before"])
      expect(memory.calls()).toEqual([
        { tool: "bind", args: { host_session: "child", agent: "dev", claimed_root: "top", workspace: KEY } },
        { tool: "bind", args: { host_session: "child", agent: "dev", claimed_root: "top", workspace: KEY } },
        { tool: "bind", args: { host_session: "top", agent: "takt", claimed_root: "top", workspace: KEY } },
      ])
      expect(memory.spawns).toEqual([["takt-ai", "memory", "start"]])
      expect(memory.cwds).toEqual([WORKSPACE])
    } finally { memory.stop() }
  })

  test("a tool that is not a memory tool is left alone", async () => {
    const memory = await startMemory()
    try {
      await memory.run("record", record(), "dev")
      const before = memory.calls().length
      await memory.hook({ tool: "shell", sessionID: "root", agent: "dev", messageID: "m", id: "i", input: {} })
      expect(memory.calls()).toHaveLength(before)
    } finally { memory.stop() }
  })

  test("an author outside the crew roster is refused and nothing is bound", async () => {
    const memory = await startMemory()
    try {
      await expect(memory.run("record", record(), "intruder")).rejects.toThrow('"intruder" is not a Takt crew member')
      expect(memory.calls()).toEqual([])
    } finally { memory.stop() }
  })
})

describe("recovery through start", () => {
  test("a silent socket makes the call start the service once and retry", async () => {
    const memory = await startMemory({ running: false, startsService: true })
    try {
      await memory.run("search", { query: "x", scope: "workspace" })
      expect(memory.spawns).toHaveLength(2)
      expect(memory.calls().map(c => c.tool)).toEqual(["bind"])
    } finally { memory.stop() }
  })

  test("a service that still does not answer refuses the call as unavailable after one start", async () => {
    const memory = await startMemory({ running: false })
    try {
      await expect(memory.run("search", { query: "x", scope: "workspace" })).rejects.toThrow("The Takt memory service is unavailable")
      expect(memory.spawns).toHaveLength(2)
    } finally { memory.stop() }
  })
})

describe("global tier", () => {
  test("workspace content in a global record is refused, naming workspace scope", async () => {
    const memory = await startMemory()
    try {
      for (const field of [{ content: "see\n```go\nx\n```" }, { title: `in ${WORKSPACE}/src` }, { evidence: `${WORKSPACE}/a.go` }]) {
        await expect(memory.run("record", { ...globalRecord, ...field })).rejects.toThrow("scope workspace")
      }
      expect(memory.created).toEqual([])
      expect(memory.calls()).toEqual([])
    } finally { memory.stop() }
  })

  test("a global record asks the user, then binds the approved arguments as its grant", async () => {
    const memory = await startMemory()
    try {
      await memory.run("record", globalRecord)
      expect(memory.created).toMatchObject([{ action: "memory_global", resources: ["prefers tabs"], metadata: { title: "prefers tabs", content: "always tabs" } }])
      expect(memory.calls()).toEqual([{ tool: "bind", args: expect.objectContaining({ grants: [globalRecord] }) }])
    } finally { memory.stop() }
  })

  test("a user-ordered record asks its own question; a global one asks twice", async () => {
    const memory = await startMemory()
    try {
      await memory.run("record", record({ user_order: true }))
      await memory.run("record", { ...globalRecord, user_order: true })
      expect(memory.created.map(c => c.action)).toEqual(["memory_user_order", "memory_global", "memory_user_order"])
      expect(memory.created[0].metadata).toMatchObject({ question: "Did you order recording this?" })
    } finally { memory.stop() }
  })

  test("a denial throws and nothing is bound", async () => {
    const memory = await startMemory({ effects: ["deny"] })
    try {
      await expect(memory.run("record", globalRecord)).rejects.toThrow("The user declined: memory_global")
      expect(memory.calls()).toEqual([])
    } finally { memory.stop() }
  })

  test("an ordinary workspace record asks nothing and carries no grant", async () => {
    const memory = await startMemory()
    try {
      await memory.run("record", record())
      expect(memory.created).toEqual([])
      expect(memory.calls()[0].args).not.toHaveProperty("grants")
    } finally { memory.stop() }
  })
})

describe("traceability matrix", () => {
  const entries: Described[] = [
    { id: 1, exists: true, title: MATRIX },
    { id: 2, exists: true, title: `${MATRIX}: Owning node`, outgoing: [{ entry: 1, kind: "supplements" }] },
    // An external revision of the column keeps the chain; it is the head the next version supersedes.
    { id: 3, exists: true, title: "revised by hand", outgoing: [{ entry: 2, kind: "supersedes" }] },
    { id: 4, exists: true, title: `${MATRIX}: Evidence`, outgoing: [{ entry: 1, kind: "supplements" }] },
    { id: 9, exists: true, title: "an unrelated fact" },
  ]
  const column = (title: string, relates_to?: unknown) => record({ title, relates_to })
  const refused = async (agent: string, input: unknown, message: string) => {
    const memory = await startMemory({ entries })
    try {
      await expect(memory.run("record", input, agent)).rejects.toThrow(message)
      expect(memory.calls().filter(c => c.tool === "bind")).toEqual([])
    } finally { memory.stop() }
  }
  const accepted = async (agent: string, input: unknown) => {
    const memory = await startMemory({ entries })
    try {
      await memory.run("record", input, agent)
      expect(memory.calls().at(-1)?.tool).toBe("bind")
    } finally { memory.stop() }
  }

  test("only qa creates the root, titled with its objective", async () => {
    await accepted("qa", column(MATRIX))
    await refused("tpm", column(MATRIX), "Only qa creates the traceability matrix root")
    await refused("qa", column("Traceability Matrix"), `\`${"Traceability Matrix: "}<objective>\``)
    await refused("dev", column(MATRIX), "Only qa, tpm and verify write the traceability matrix")
  })

  test("a column's first version supplements the root under its exact title", async () => {
    await accepted("tpm", column(`${MATRIX}: Owning node`, { id: 1, relation: "supplements" }))
    await accepted("verify", column(`${MATRIX}: Evidence`, { id: 1, relation: "supplements" }))
    await refused("tpm", column("Owning node", { id: 1, relation: "supplements" }), `Title this entry exactly \`${MATRIX}: Owning node\``)
    await refused("verify", column(`${MATRIX}: Owning node`, { id: 1, relation: "supplements" }), `\`${MATRIX}: Evidence\``)
    await refused("tpm", column(`${MATRIX}: Owning node`), "supplements")
    await refused("tpm", column(`${MATRIX}: Owning node`, { id: 1, relation: "exception-to" }), "supplements for its first version or supersedes")
  })

  test("a later version supersedes the head of its own chain, whoever wrote that head", async () => {
    await accepted("tpm", column(`${MATRIX}: Owning node`, { id: 3, relation: "supersedes" }))
    await refused("tpm", column(`${MATRIX}: Owning node`, { id: 4, relation: "supersedes" }), "which is not yours")
    await refused("tpm", column(`${MATRIX}: Owning node`, { id: 9, relation: "supersedes" }), "not a matrix column chain")
  })

  test("a dispute and an unrelated record stay open to any author", async () => {
    await accepted("dev", record({ title: "the matrix is wrong", relates_to: { id: 2, relation: "disputes" } }))
    await accepted("dev", record({ relates_to: { id: 9, relation: "supplements" } }))
  })
})
