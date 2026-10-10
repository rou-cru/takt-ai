import { describe, expect, mock, test } from "bun:test"
import { existsSync, readFileSync } from "node:fs"
import { homedir } from "node:os"
import { dirname } from "node:path"
import type { OpenCodeEvent } from "@opencode/client"
import { Schema } from "effect"
import vfsPlugin from "./takt-vfs"

// The placeholders the harness templates at deploy time stay literal here, so
// the identities below are the exact strings the plugin compares against.
const ORCHESTRATOR = "__TAKT_ORCHESTRATOR_ID__"
const RESULT_AGENT = "__TAKT_RESULT_AGENTS__"
const VFS_AGENT = "__TAKT_VFS_AGENTS__"
const DELEGATIONS_KEY = "takt/vfs/delegations"
const INPUTS_KEY = "takt/vfs/inputs"
const BROKEN_AGENT = "broken-agent"
const WORKSPACE = "/workspace"
const SHELL_NOT_ADMITTED = 122
const SETTLE_ATTEMPTS = 100
const SETTLE_STEP_MS = 2
const QUIET_MS = 25
const VFS_TOOL_NAMES = ["vfs_bind", "vfs_write", "vfs_read", "vfs_delete", "vfs_discard", "vfs_verify", "vfs_consolidate"]

type Reply = { out?: string; err?: string; code?: number }
type Call = { kind: string; command: string; request: Record<string, unknown> }
type HookEvent = Record<string, unknown>
type Hooks = Map<string, Array<(event: HookEvent) => Promise<void>>>
type Ctx = { sessionID: string; agent: string }
type SessionInfo = { parentID?: string; title?: string; metadata?: Record<string, unknown>; time?: { created: number } }
type Options = {
  nativeInput?: unknown
  respond?: (call: Call) => Reply | undefined
  claims?: unknown[]
  sessions?: Record<string, SessionInfo>
  storage?: Record<string, unknown>
  permissions?: Record<string, unknown[]>
  promptError?: string
  home?: boolean
  // wait resolves when the host would report the session's turn ended.
  wait?: (sessionID: string) => Promise<void>
  // missing names sessions the host no longer has.
  missing?: string[]
}

const sleep = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms))
const until = async (ready: () => boolean, attempts = SETTLE_ATTEMPTS): Promise<void> => {
  if (ready() || attempts === 0) return
  await sleep(SETTLE_STEP_MS)
  return until(ready, attempts - 1)
}

const orchestrator: Ctx = { sessionID: "root", agent: ORCHESTRATOR }
const dev: Ctx = { sessionID: "dispatch", agent: "dev" }
const unitSessions = { dispatch: { parentID: "root", title: "unit-a" } }

const gcPlan = { session_id: "root", cycle_id: "cycle-1", mandate_class: "complexity", delta: [], closure: ["a.go"], reachability: "codegraph" }
const gcCycle = (phase: string, sessions: Record<string, string>, extra: Record<string, unknown> = {}) =>
  ({ phase, plan: gcPlan, scope: ["a.go"], sessions, report: null, started: "2026-01-01T00:00:00Z", ...extra })
const coordinator = (cycle?: unknown, extra: Record<string, unknown> = {}) =>
  ({ version: 1, units: 0, mutations: 0, cursor: 0, deferrals: 0, next_mandate: 0, requested: false, ...(cycle ? { cycle } : {}), ...extra })
const claim = (extra: Record<string, unknown>) =>
  ({ key: "claim-1", root_session_id: "root", work_unit_id: "unit-a", agent_id: "dev", target_instance: "dev", ...extra })
const shellPlan = (extra: Record<string, unknown> = {}) => ({
  decision: "ask", reason: "review each run", cwd: WORKSPACE, scratch: "/private/tmp/scratch", confirm: "/private/tmp/confirm",
  capture: false, writable: [`${WORKSPACE}/src`], protected: [`${WORKSPACE}/.git`], private: ["/private/secret"], ...extra,
})
const shellReply = (extra: Record<string, unknown> = {}): Reply => ({ out: JSON.stringify({ ok: true, shell: shellPlan(extra) }) })
const json = (value: unknown): Reply => ({ out: JSON.stringify(value) })

function defaultReply({ kind, command }: Call, claims: unknown[]): Reply {
  if (kind === "codegraph") return { out: "" }
  if (kind === "obs") return { out: "{}" }
  if (kind === "dispatch" || kind === "gc") return json(coordinator())
  if (command === "claims") return json({ ok: true, claims })
  if (command === "bind") return json({ ok: true, key: "author-1", attempt_id: "attempt-1", invariants_version: "inv-1", revision: 0, delta_hash: "d0" })
  if (command === "shell-prepare") return shellReply()
  return json({ ok: true, revision: 1, delta_hash: "d1", content: "staged content" })
}

// startVfs wires the plugin to a scripted takt-ai, a scripted session tree and
// a controllable event stream; nothing touches the network or the real HOME.
async function startVfs(options: Options = {}) {
  const runtime = globalThis as unknown as { Bun: { spawn: (...args: never[]) => unknown } }
  const originalSpawn = runtime.Bun.spawn
  const originalWarn = console.warn
  const originalError = console.error
  const calls: Call[] = []
  const warnings: unknown[][] = []
  const errors: unknown[][] = []
  const log = {
    created: [] as unknown[], interrupts: [] as string[], waits: [] as string[],
    prompts: [] as Array<{ sessionID: string; text: string }>, synthetics: [] as Array<{ sessionID: string; text: string }>,
  }
  const nativeSubagent = { input: options.nativeInput ?? { type: "object", properties: { description: { type: "string" }, agent: { type: "string" } } } as unknown }
  const tools = new Map<string, { execute?: (...args: never[]) => unknown }>()
  const toolHooks: Hooks = new Map()
  const permissionHooks: Hooks = new Map()
  const shellHooks: Hooks = new Map()
  const sessionHooks: Hooks = new Map()
  const storage = new Map<string, unknown>(Object.entries(options.storage ?? {}))
  const events: OpenCodeEvent[] = []
  let wake: (() => void) | undefined
  let closed = false
  const register = (hooks: Hooks) => async (name: string, callback: unknown) => {
    hooks.set(name, [...(hooks.get(name) ?? []), callback as (event: HookEvent) => Promise<void>])
    return { dispose() {} }
  }
  async function* stream(): AsyncGenerator<OpenCodeEvent> {
    while (!closed) {
      while (events.length === 0 && !closed) await new Promise<void>((resolve) => { wake = resolve })
      const event = events.shift()
      if (event) yield event
    }
  }
  console.warn = (...args: unknown[]) => { warnings.push(args) }
  console.error = (...args: unknown[]) => { errors.push(args) }
  runtime.Bun.spawn = ((argv: string[]) => {
    const kind = argv[1]
    let request: Record<string, unknown> = kind === "dispatch" ? JSON.parse(argv[7]) : kind === "gc" ? JSON.parse(argv[8]) : {}
    let settled: Reply | undefined
    const settle = () => {
      if (!settled) {
        const call = { kind, command: kind === "dispatch" || kind === "gc" ? String(request.action) : argv[2], request }
        calls.push(call)
        settled = options.respond?.(call) ?? defaultReply(call, options.claims ?? [])
      }
      return settled
    }
    return {
      stdin: { write(value: string) { request = JSON.parse(value) }, end() {} },
      get stdout() { return settle().out ?? "" },
      get stderr() { return settle().err ?? "" },
      get exited() { return Promise.resolve(settle().code ?? 0) },
    }
  }) as typeof originalSpawn

  const restore = () => {
    runtime.Bun.spawn = originalSpawn
    console.warn = originalWarn
    console.error = originalError
  }
  let cleanup: unknown
  try {
    cleanup = await vfsPlugin.setup({
      location: { directory: options.home ? homedir() : WORKSPACE },
      storage: { async get(key: string) { return storage.get(key) }, async set(key: string, value: unknown) { storage.set(key, value) } },
      session: {
        async get({ sessionID }: { sessionID: string }) {
          if (options.missing?.includes(sessionID)) throw new Error(`session ${sessionID} not found`)
          return { id: sessionID, ...options.sessions?.[sessionID] }
        },
        hook: register(sessionHooks),
        async create(request: unknown) { log.created.push(request); return { id: "child" } },
        async prompt(request: { sessionID: string; text: string }) {
          if (options.promptError) throw new Error(options.promptError)
          log.prompts.push(request)
        },
        async interrupt({ sessionID }: { sessionID: string }) { log.interrupts.push(sessionID) },
        async synthetic(request: { sessionID: string; text: string }) { log.synthetics.push(request) },
        async wait({ sessionID }: { sessionID: string }) { log.waits.push(sessionID); await options.wait?.(sessionID) },
      },
      agent: { async get({ agentID }: { agentID: string }) {
        if (agentID === BROKEN_AGENT) throw new Error("agent lookup failed")
        return { data: { id: agentID, permissions: options.permissions?.[agentID] ?? [] } }
      } },
      permission: { hook: register(permissionHooks) },
      shell: { hook: register(shellHooks) },
      tool: {
        hook: register(toolHooks),
        async transform(callback: (editor: { get(name: string): typeof nativeSubagent | undefined; update(name: string, change: (tool: typeof nativeSubagent) => void): void; add(definition: { name: string; execute?: (...args: never[]) => unknown }): void }) => void) {
          await callback({ get(name) { return name === "subagent" ? nativeSubagent : undefined }, update(name, change) { if (name === "subagent") change(nativeSubagent) }, add(definition) { tools.set(definition.name, definition) } })
          return { dispose() {} }
        },
      },
      event: { subscribe({ signal }: { signal: AbortSignal }) {
        signal.addEventListener("abort", () => { closed = true; wake?.() }, { once: true })
        return { [Symbol.asyncIterator]: stream }
      } },
    } as never)
  } catch (error) {
    restore()
    throw error
  }
  return {
    calls, log, warnings, errors, storage, nativeSubagent,
    actions: (kind: string) => calls.filter((call) => call.kind === kind).map((call) => call.command),
    requests: (kind: string, command: string) => calls.filter((call) => call.kind === kind && call.command === command).map((call) => call.request),
    run(name: string, input: unknown, ctx: Ctx = dev) {
      const execute = tools.get(name)?.execute
      if (!execute) throw new Error(`missing tool ${name}`)
      return (execute as unknown as (value: unknown, context: Ctx) => Promise<{ content: string }>)(input, ctx)
    },
    // fire runs every hook registered under name in order, as the host does.
    fire: (hooks: Hooks, name: string, event: HookEvent) =>
      (hooks.get(name) ?? []).reduce((chain, callback) => chain.then(() => callback(event)), Promise.resolve()),
    tool: toolHooks, permission: permissionHooks, shell: shellHooks, session: sessionHooks,
    publish(event: OpenCodeEvent) { events.push(event); wake?.() },
    async stop() {
      // Telemetry is deferred to a timer; let it drain before the stub goes.
      await sleep(SETTLE_STEP_MS * 2)
      if (typeof cleanup === "function") cleanup()
      restore()
    },
  }
}

describe("setup guards", () => {
  test("extends the actual native Effect schema used by delegation tools", async () => {
    const input = Schema.Struct({
      description: Schema.String, agent: Schema.String, prompt: Schema.String,
      sessionID: Schema.optional(Schema.String),
    })
    const vfs = await startVfs({ nativeInput: input })
    try {
      expect(vfs.nativeSubagent.input).toMatchObject({
        type: "object", required: ["description", "agent", "prompt"],
        properties: { description: { type: "string" }, agent: { type: "string" },
          author_keys: { type: "array", minItems: 1, uniqueItems: true } },
      })
    } finally { await vfs.stop() }
  })

  test("opened in the home directory, CodeGraph stays off and its tools refuse", async () => {
    const vfs = await startVfs({ home: true })
    try {
      expect(String(vfs.warnings[0][0])).toContain(`open in the home directory (${homedir()})`)
      expect(vfs.actions("codegraph")).toEqual([])
      expect(() => vfs.run("gc_findings", {})).toThrow("CodeGraph is unavailable")
      await expect(vfs.run("gc_request", {}, orchestrator)).rejects.toThrow(`CodeGraph is unavailable because ${homedir()} is the home directory`)
    } finally { await vfs.stop() }
  })

  test("a failed CodeGraph index fails setup with the most specific reason available", async () => {
    const failing = (reply: Reply) => (call: Call) => call.kind === "codegraph" ? reply : undefined
    await expect(startVfs({ respond: failing({ code: 1, err: " index locked\n" }) })).rejects.toThrow("CodeGraph workspace preparation failed: index locked")
    await expect(startVfs({ respond: failing({ code: 1, out: "partial output" }) })).rejects.toThrow("CodeGraph workspace preparation failed: partial output")
    await expect(startVfs({ respond: failing({ code: 3 }) })).rejects.toThrow("CodeGraph workspace preparation failed: exit 3")
  })

  test("a parent cycle in the session tree is refused", async () => {
    const vfs = await startVfs({ sessions: { a: { parentID: "b" }, b: { parentID: "a" } } })
    try {
      await expect(vfs.run("claim_list", {}, { sessionID: "a", agent: ORCHESTRATOR })).rejects.toThrow("session a has a parent cycle")
    } finally { await vfs.stop() }
  })
})

describe("takt-ai answers", () => {
  test("a refusal, an unparseable answer or an incomplete answer surfaces as an error", async () => {
    let reply: Reply = {}
    const vfs = await startVfs({ sessions: unitSessions, respond: (call) => call.command === "op" ? reply : undefined })
    try {
      await vfs.run("vfs_bind", { scope: ["src/a.go"] })
      const write = () => vfs.run("vfs_write", { path: "src/a.go", content: "x" })
      reply = { out: "garbage", code: 0 }
      await expect(write()).rejects.toThrow("takt-ai vfs op exited 0: garbage")
      reply = { out: "", err: "stderr text", code: 4 }
      await expect(write()).rejects.toThrow("takt-ai vfs op exited 4: stderr text")
      expect((await write().catch((e: Error) => e) as Error).cause).toBeUndefined()
      reply = json({ ok: false, error: "scope violation" })
      await expect(write()).rejects.toThrow("scope violation")
      reply = { out: JSON.stringify({ ok: false }), err: "denied upstream", code: 1 }
      await expect(write()).rejects.toThrow("takt-ai vfs op exited 1: denied upstream")
      reply = json({ revision: 1 })
      await expect(write()).rejects.toThrow("takt-ai vfs op returned a response without ok")
      reply = json({ ok: true })
      await expect(write()).rejects.toThrow("takt-ai VFS mutation response omitted revision or delta_hash")
    } finally { await vfs.stop() }
  })

  test("operations an agent issues together each start from the revision the previous one left", async () => {
    let current = 0
    const vfs = await startVfs({
      sessions: unitSessions,
      respond: (call) => {
        if (call.command !== "op") return undefined
        if (call.request.expected_revision !== current) return json({ ok: false, error: "vfs: revision changed" })
        current += 1
        return json({ ok: true, revision: current, delta_hash: `d${current}` })
      },
    })
    try {
      await vfs.run("vfs_bind", { scope: ["src/a.go", "src/b.go"] })
      await Promise.all([
        vfs.run("vfs_write", { path: "src/a.go", content: "a" }),
        vfs.run("vfs_write", { path: "src/b.go", content: "b" }),
        vfs.run("vfs_read", { path: "src/a.go" }),
      ])
      expect(vfs.requests("vfs", "op").map((request) => request.expected_revision)).toEqual([0, 1, 2])
    } finally { await vfs.stop() }
  })

  test("a coordination command that exits non-zero reports its stderr", async () => {
    const vfs = await startVfs({ respond: (call) => call.kind === "dispatch" && call.command === "commit" ? { code: 1, err: "plan rejected" } : undefined })
    try {
      await expect(vfs.run("dispatch_commit", { version: "v1", plan: [] }, orchestrator)).rejects.toThrow("dispatch commit: plan rejected")
    } finally { await vfs.stop() }
  })
})

describe("delegation lifecycle", () => {
  const launch = (input: Record<string, unknown>) => ({ tool: "subagent", sessionID: "root", id: "call-1", input })
  const declareNone = (vfs: Awaited<ReturnType<typeof startVfs>>, unit: string) => vfs.run("dispatch_inputs", { work_unit_id: unit, none: true }, orchestrator)
  // acceptInputs lets the harness validate every declared input and result.
  const acceptInputs = (call: Call) => call.kind === "dispatch" && (call.command === "validate_inputs" || call.command === "validate_results") ? { out: "{}" } : undefined

  test("a delegation is refused until its consumed invariants are declared", async () => {
    const vfs = await startVfs()
    try {
      await expect(vfs.fire(vfs.tool, "execute.before", launch({ description: "unit-a", agent: "dev" }))).rejects.toThrow("Declare the invariants work unit unit-a consumes")
      expect(vfs.actions("dispatch")).toEqual([])
    } finally { await vfs.stop() }
  })

  test("a declaration names existing invariants or their absence, exactly one", async () => {
    const vfs = await startVfs({ respond: acceptInputs })
    try {
      await expect(vfs.run("dispatch_inputs", { work_unit_id: "unit-a" }, orchestrator)).rejects.toThrow("exactly one of them")
      await expect(vfs.run("dispatch_inputs", { work_unit_id: "unit-a", result_ids: [7], none: true }, orchestrator)).rejects.toThrow("exactly one of them")
      await expect(vfs.run("dispatch_inputs", { work_unit_id: "unit-a", none: true }, { sessionID: "child-r", agent: RESULT_AGENT })).rejects.toThrow("dispatch_inputs belongs to the orchestrator")
      await vfs.run("dispatch_inputs", { work_unit_id: "unit-a", result_ids: [7, 8] }, orchestrator)
      expect(vfs.requests("dispatch", "validate_inputs")).toEqual([{ action: "validate_inputs", session: "root", result_ids: [7, 8] }])
    } finally { await vfs.stop() }
  })

  test("a declaration naming a missing entry is refused", async () => {
    const vfs = await startVfs({ respond: (call) => call.kind === "dispatch" && call.command === "validate_inputs" ? { code: 1, err: "entry #9 not found" } : undefined })
    try {
      await expect(vfs.run("dispatch_inputs", { work_unit_id: "unit-a", result_ids: [9] }, orchestrator)).rejects.toThrow("entry #9 not found")
      await expect(vfs.fire(vfs.tool, "execute.before", launch({ description: "unit-a", agent: "dev" }))).rejects.toThrow("Declare the invariants work unit unit-a consumes")
    } finally { await vfs.stop() }
  })

  test("a delegation must name its unit and its specialist", async () => {
    const vfs = await startVfs()
    try {
      await expect(vfs.fire(vfs.tool, "execute.before", launch({ agent: "dev" }))).rejects.toThrow("Name the delegation after the work unit")
      await expect(vfs.fire(vfs.tool, "execute.before", launch({ description: "unit-a" }))).rejects.toThrow("requested specialist identity is missing")
      expect(vfs.actions("dispatch")).toEqual([])
    } finally { await vfs.stop() }
  })

  test("a staging specialist is refused without its pending claim", async () => {
    const vfs = await startVfs()
    try {
      await declareNone(vfs, "unit-v")
      await expect(vfs.fire(vfs.tool, "execute.before", launch({ description: "unit-v", agent: VFS_AGENT }))).rejects.toThrow("no matching pending clean claim for session root, work unit unit-v")
      expect(vfs.actions("dispatch")).toEqual([])
    } finally { await vfs.stop() }
  })

  test("an admitted delegation is remembered durably and launched", async () => {
    const vfs = await startVfs({ claims: [claim({ work_unit_id: "unit-v", agent_id: VFS_AGENT, target_instance: VFS_AGENT, pending: true })] })
    try {
      await declareNone(vfs, "unit-v")
      await vfs.fire(vfs.tool, "execute.before", launch({ description: "unit-v", agent: VFS_AGENT }))
      expect(vfs.actions("dispatch")).toEqual(["admit", "launch"])
      expect(vfs.requests("dispatch", "admit")[0]).toMatchObject({ event: "unit-v", dispatch: "root:call-1", session: "root", agent: VFS_AGENT })
      expect(vfs.storage.get(DELEGATIONS_KEY)).toEqual({ "root:call-1": { unit: "unit-v", root: "root", agent: VFS_AGENT, caller: "root", admittedAt: expect.any(Number) } })
    } finally { await vfs.stop() }
  })

  test("a denied admission is thrown and nothing is remembered", async () => {
    const vfs = await startVfs({ respond: (call) => call.kind === "dispatch" && call.command === "admit" ? { code: 1, err: "budget exceeded" } : undefined })
    try {
      await declareNone(vfs, "unit-a")
      await expect(vfs.fire(vfs.tool, "execute.before", launch({ description: "unit-a", agent: "dev" }))).rejects.toThrow("dispatch admit: budget exceeded")
      expect(vfs.actions("dispatch")).toEqual(["admit"])
      expect(vfs.storage.has(DELEGATIONS_KEY)).toBe(false)
    } finally { await vfs.stop() }
  })

  const producerSessions = { "child-r": { parentID: "root", title: "unit-r" } }
  const admitProducer = async (vfs: Awaited<ReturnType<typeof startVfs>>, consumed?: number[]) => {
    await vfs.run("dispatch_inputs", consumed ? { work_unit_id: "unit-r", result_ids: consumed } : { work_unit_id: "unit-r", none: true }, orchestrator)
    await vfs.fire(vfs.tool, "execute.before", { tool: "subagent", sessionID: "root", id: "call-r", input: { description: "unit-r", agent: RESULT_AGENT } })
    const context = { tools: Object.fromEntries(VFS_TOOL_NAMES.map((name) => [name, {}])), sessionID: "child-r", agent: RESULT_AGENT, system: [] }
    await vfs.fire(vfs.session, "context", context)
    expect(context.tools).toEqual({})
    return context.system as Array<{ text: string }>
  }
  // finished is the host's report that the producer's delegation completed.
  const finished = () => ({ tool: "subagent", sessionID: "root", id: "call-r", status: "completed", result: { content: "Breakdown recorded." } })

  test("the specialist receives the invariants declared for its unit, or their declared absence", async () => {
    const declared = await startVfs({ sessions: producerSessions, respond: acceptInputs })
    try {
      const system = await admitProducer(declared, [7, 8])
      expect(system.map((entry) => entry.text)).toContain("Work unit unit-r consumes these invariants: Engram #7, Engram #8. Read each with mem_get_observation; they bind as read-only and prevail over any restatement in the brief.")
    } finally { await declared.stop() }
    const none = await startVfs({ sessions: producerSessions })
    try {
      const system = await admitProducer(none)
      expect(system.map((entry) => entry.text)).toContain("Work unit unit-r consumes no recorded invariant yet; author it from the brief.")
    } finally { await none.stop() }
  })

  test("a declaration survives a plugin restart", async () => {
    const first = await startVfs({ respond: acceptInputs })
    try {
      await first.run("dispatch_inputs", { work_unit_id: "unit-r", result_ids: [7] }, orchestrator)
    } finally { await first.stop() }
    const stored = first.storage.get(INPUTS_KEY)
    expect(stored).toEqual({ "root\0unit-r": [7] })
    const restarted = await startVfs({ sessions: producerSessions, storage: { [INPUTS_KEY]: stored } })
    try {
      await restarted.fire(restarted.tool, "execute.before", { tool: "subagent", sessionID: "root", id: "call-r", input: { description: "unit-r", agent: RESULT_AGENT } })
      const context = { tools: {}, sessionID: "child-r", agent: RESULT_AGENT, system: [] as Array<{ text: string }> }
      await restarted.fire(restarted.session, "context", context)
      expect(context.system.map((entry) => entry.text)).toContain("Work unit unit-r consumes these invariants: Engram #7. Read each with mem_get_observation; they bind as read-only and prevail over any restatement in the brief.")
    } finally { await restarted.stop() }
  })

  test.each(["Breakdown recorded.", "Completed report: all findings are listed in this chat.", "Created report.md; the result is in that file."])("a producer with only unstructured output (%s) is nudged once, then fails and settles", async (content) => {
    const vfs = await startVfs({ sessions: producerSessions })
    try {
      await admitProducer(vfs)
      const event = finished()
      event.result.content = content
      await expect(vfs.fire(vfs.tool, "execute.after", event)).rejects.toThrow("the specialist ended without delivering its result; resume its session to ask for it")
      expect(vfs.log.prompts).toHaveLength(1)
      expect(vfs.log.prompts[0]).toMatchObject({ sessionID: "child-r" })
      expect(vfs.log.prompts[0].text).toContain("Call deliver_result")
      expect(vfs.log.waits).toEqual(["child-r"])
      expect(vfs.requests("dispatch", "finish")).toEqual([{ action: "finish", event: "unit-r", dispatch: "root:call-r", session: "root" }])
      expect(vfs.storage.get(DELEGATIONS_KEY)).toEqual({})
    } finally { await vfs.stop() }
  })

  test("a producer that delivered ends cleanly without a nudge", async () => {
    const vfs = await startVfs({ sessions: producerSessions, respond: acceptInputs })
    try {
      await admitProducer(vfs)
      const delivered = await vfs.run("deliver_result", { result_ids: [4, 5] }, { sessionID: "child-r", agent: RESULT_AGENT })
      expect(delivered.content).toBe("Delivered 2 result id(s)")
      expect(vfs.requests("dispatch", "validate_results")[0]).toMatchObject({ session: "root", agent: RESULT_AGENT, result_ids: [4, 5] })
      await vfs.run("deliver_result", { result_ids: [6] }, { sessionID: "child-r", agent: RESULT_AGENT })
      const event = finished()
      await vfs.fire(vfs.tool, "execute.after", event)
      expect(event.result.content).toBe("Breakdown recorded.\n\nDelivered results: Engram #4, Engram #5, Engram #6")
      expect(vfs.log.prompts).toEqual([])
      expect(vfs.actions("dispatch").at(-1)).toBe("finish")
    } finally { await vfs.stop() }
  })

  test("a background producer stays in flight after its launch returns and settles when its child goes idle", async () => {
    const vfs = await startVfs({ sessions: producerSessions })
    try {
      await vfs.run("dispatch_inputs", { work_unit_id: "unit-r", none: true }, orchestrator)
      await vfs.fire(vfs.tool, "execute.before", { tool: "subagent", sessionID: "root", id: "call-r", input: { description: "unit-r", agent: RESULT_AGENT, background: true } })
      await vfs.fire(vfs.session, "context", { tools: {}, sessionID: "child-r", agent: RESULT_AGENT, system: [] })
      const event = finished()
      await vfs.fire(vfs.tool, "execute.after", event)
      expect(event.result.content).toBe("Breakdown recorded.")
      expect(vfs.log.prompts).toEqual([])
      expect(vfs.requests("dispatch", "finish")).toEqual([])
      vfs.publish({ id: "evt-child", created: 1, type: "session.idle", data: { sessionID: "child-r" } } as OpenCodeEvent)
      await until(() => vfs.requests("dispatch", "finish").length > 0)
      expect(vfs.requests("dispatch", "finish")).toEqual([{ action: "finish", event: "unit-r", dispatch: "root:call-r", session: "root" }])
      expect(vfs.log.prompts).toEqual([])
      expect(vfs.storage.get(DELEGATIONS_KEY)).toEqual({})
    } finally { await vfs.stop() }
  })

  // A retry of a unit runs in a new session; the session of the earlier
  // attempt, still around under the same title, never stands in for it.
  test("an earlier attempt's session neither credits a retry nor becomes its child", async () => {
    const sessions = { ...producerSessions, "child-r2": { parentID: "root", title: "unit-r" } }
    const vfs = await startVfs({ sessions, respond: acceptInputs })
    const old = { sessionID: "child-r", agent: RESULT_AGENT }
    try {
      await admitProducer(vfs)
      await vfs.run("deliver_result", { result_ids: [4] }, old)
      await vfs.fire(vfs.tool, "execute.after", finished())
      await vfs.fire(vfs.tool, "execute.before", { tool: "subagent", sessionID: "root", id: "call-r2", input: { description: "unit-r", agent: RESULT_AGENT } })
      await vfs.fire(vfs.session, "context", { tools: {}, ...old, system: [] })
      await expect(vfs.run("deliver_result", { result_ids: [9] }, old)).rejects.toThrow("nothing was credited")
      await vfs.fire(vfs.session, "context", { tools: {}, sessionID: "child-r2", agent: RESULT_AGENT, system: [] })
      await expect(vfs.fire(vfs.tool, "execute.after", { ...finished(), id: "call-r2" })).rejects.toThrow("ended without delivering")
      expect(vfs.log.prompts.map((prompt) => prompt.sessionID)).toEqual(["child-r2"])
      expect(vfs.storage.get(DELEGATIONS_KEY)).toEqual({})
    } finally { await vfs.stop() }
  })

  test("a resumed session is the child of the delegation that names it", async () => {
    const vfs = await startVfs({ sessions: producerSessions, respond: acceptInputs })
    const child = { sessionID: "child-r", agent: RESULT_AGENT }
    try {
      await admitProducer(vfs)
      await vfs.fire(vfs.tool, "execute.after", { ...finished(), status: "error" })
      await vfs.fire(vfs.tool, "execute.before", { tool: "subagent", sessionID: "root", id: "call-r2", input: { description: "unit-r", agent: RESULT_AGENT, sessionID: "child-r" } })
      await vfs.run("deliver_result", { result_ids: [5] }, child)
      const event = { ...finished(), id: "call-r2" }
      await vfs.fire(vfs.tool, "execute.after", event)
      expect(event.result.content).toBe("Breakdown recorded.\n\nDelivered results: Engram #5")
    } finally { await vfs.stop() }
  })

  test("a child is learned when the host creates it, so one that dies before its first turn still ends its delegation", async () => {
    const vfs = await startVfs({ sessions: producerSessions })
    try {
      await vfs.run("dispatch_inputs", { work_unit_id: "unit-r", none: true }, orchestrator)
      await vfs.fire(vfs.tool, "execute.before", { tool: "subagent", sessionID: "root", id: "call-r", input: { description: "unit-r", agent: RESULT_AGENT, background: true } })
      await vfs.fire(vfs.tool, "execute.after", finished())
      vfs.publish({ id: "evt-new", created: 1, type: "session.created", data: { sessionID: "child-r", agent: RESULT_AGENT, parentID: "root", title: "unit-r" } } as unknown as OpenCodeEvent)
      await until(() => (vfs.storage.get(DELEGATIONS_KEY) as Record<string, { child?: string }>)["root:call-r"]?.child === "child-r")
      vfs.publish({ id: "evt-gone", created: 2, type: "session.deleted", data: { sessionID: "child-r" } } as OpenCodeEvent)
      await until(() => vfs.requests("dispatch", "finish").length > 0)
      expect(vfs.requests("dispatch", "finish")).toEqual([{ action: "finish", event: "unit-r", dispatch: "root:call-r", session: "root" }])
      expect(vfs.storage.get(DELEGATIONS_KEY)).toEqual({})
    } finally { await vfs.stop() }
  })

  // After a restart nothing in memory remembers which sessions already ran a
  // delegation; a session created before the delegation was admitted is an
  // earlier one's all the same.
  test("a session created before the delegation was admitted is never its child", async () => {
    const sessions = {
      "child-old": { parentID: "root", title: "unit-r", time: { created: Date.now() - 60_000 } },
      "child-new": { parentID: "root", title: "unit-r", time: { created: Date.now() + 1 } },
    }
    const vfs = await startVfs({ sessions, respond: acceptInputs })
    try {
      await vfs.run("dispatch_inputs", { work_unit_id: "unit-r", none: true }, orchestrator)
      await vfs.fire(vfs.tool, "execute.before", { tool: "subagent", sessionID: "root", id: "call-r", input: { description: "unit-r", agent: RESULT_AGENT } })
      await vfs.fire(vfs.session, "context", { tools: {}, sessionID: "child-old", agent: RESULT_AGENT, system: [] })
      await expect(vfs.run("deliver_result", { result_ids: [9] }, { sessionID: "child-old", agent: RESULT_AGENT })).rejects.toThrow("nothing was credited")
      await vfs.run("deliver_result", { result_ids: [4] }, { sessionID: "child-new", agent: RESULT_AGENT })
      const event = finished()
      await vfs.fire(vfs.tool, "execute.after", event)
      expect(event.result.content).toBe("Breakdown recorded.\n\nDelivered results: Engram #4")
    } finally { await vfs.stop() }
  })

  test("a background child still running after a restart keeps its slot until its turn ends", async () => {
    let release = () => {}
    const turn = new Promise<void>((resolve) => { release = resolve })
    const vfs = await startVfs({
      sessions: { ...producerSessions, "child-gone-r": { parentID: "root", title: "unit-g" } },
      missing: ["child-gone-r"],
      wait: async (sessionID) => { if (sessionID === "child-r") await turn },
      storage: { [DELEGATIONS_KEY]: {
        "old:1": { unit: "unit-r", root: "root", agent: RESULT_AGENT, background: true, child: "child-r" },
        "old:2": { unit: "unit-g", root: "root", agent: RESULT_AGENT, background: true, child: "child-gone-r" },
      } },
    })
    try {
      await vfs.run("dispatch_inputs", { work_unit_id: "unit-n", none: true }, orchestrator)
      await vfs.fire(vfs.tool, "execute.before", { tool: "subagent", sessionID: "root", id: "call-n", input: { description: "unit-n", agent: RESULT_AGENT } })
      // The child the host no longer has is reconciled; the running one is not.
      expect(vfs.requests("dispatch", "uncertain")).toEqual([{ action: "uncertain", event: "unit-g", dispatch: "old:2", session: "root" }])
      expect(Object.keys(vfs.storage.get(DELEGATIONS_KEY) as object).sort()).toEqual(["old:1", "root:call-n"])
      expect(vfs.requests("dispatch", "finish")).toEqual([])
      release()
      await until(() => vfs.requests("dispatch", "finish").length > 0)
      expect(vfs.requests("dispatch", "finish")).toEqual([{ action: "finish", event: "unit-r", dispatch: "old:1", session: "root" }])
      expect(Object.keys(vfs.storage.get(DELEGATIONS_KEY) as object)).toEqual(["root:call-n"])
    } finally { await vfs.stop() }
  })

  test("a background watch that fails is taken up again by the next reconciliation", async () => {
    const missing: string[] = []
    const vfs = await startVfs({
      sessions: producerSessions, missing,
      wait: async () => { throw new Error("host unreachable") },
      storage: { [DELEGATIONS_KEY]: { "old:1": { unit: "unit-r", root: "root", agent: RESULT_AGENT, background: true, child: "child-r" } } },
    })
    const delegate = async (unit: string) => {
      await vfs.run("dispatch_inputs", { work_unit_id: unit, none: true }, orchestrator)
      await vfs.fire(vfs.tool, "execute.before", { tool: "subagent", sessionID: "root", id: `call-${unit}`, input: { description: unit, agent: RESULT_AGENT } })
    }
    try {
      await delegate("unit-a")
      await until(() => vfs.errors.some(([label]) => label === "Takt background watch"))
      expect(vfs.requests("dispatch", "uncertain")).toEqual([])
      // By the next pass the host no longer has the child: it is reconciled.
      missing.push("child-r")
      await delegate("unit-b")
      expect(vfs.requests("dispatch", "uncertain")).toEqual([{ action: "uncertain", event: "unit-r", dispatch: "old:1", session: "root" }])
      expect(Object.keys(vfs.storage.get(DELEGATIONS_KEY) as object).sort()).toEqual(["root:call-unit-a", "root:call-unit-b"])
    } finally { await vfs.stop() }
  })

  test("a resumed session runs the unit its new delegation names", async () => {
    const vfs = await startVfs({ sessions: producerSessions, respond: acceptInputs })
    try {
      await admitProducer(vfs)
      await vfs.fire(vfs.tool, "execute.after", { ...finished(), status: "error" })
      await vfs.run("dispatch_inputs", { work_unit_id: "unit-r-fix", none: true }, orchestrator)
      await vfs.fire(vfs.tool, "execute.before", { tool: "subagent", sessionID: "root", id: "call-fix", input: { description: "unit-r-fix", agent: RESULT_AGENT, sessionID: "child-r" } })
      const context = { tools: {}, sessionID: "child-r", agent: RESULT_AGENT, system: [] as Array<{ text: string }> }
      await vfs.fire(vfs.session, "context", context)
      expect(context.system.map((part) => part.text)).toContain("Work unit unit-r-fix consumes no recorded invariant yet; author it from the brief.")
    } finally { await vfs.stop() }
  })

  test("a top-level session's deliver_result has no delegation to credit and says so", async () => {
    const vfs = await startVfs({ respond: acceptInputs })
    try {
      const reply = await vfs.run("deliver_result", { result_ids: [3] }, { sessionID: "lent", agent: RESULT_AGENT })
      expect(reply.content).toBe("Validated 1 result id(s); no delegation to credit them to")
    } finally { await vfs.stop() }
  })

  test("model usage is attributed to who ran in that session", async () => {
    const vfs = await startVfs({ sessions: producerSessions, respond: acceptInputs })
    // usage is a host usage event for sessionID; usageOf finds what was recorded for it.
    const usage = (id: string, sessionID: string) => ({ id, created: 1, type: "session.usage.updated",
      data: { sessionID, cost: 0.01, tokens: { input: 1, output: 1, reasoning: 0, cache: { read: 0, write: 0 } } } }) as unknown as OpenCodeEvent
    const usageOf = (sessionID: string) => vfs.requests("obs", "ingest").find((r) => r.event_class === "model_usage" && r.session_id === sessionID)
    try {
      await admitProducer(vfs)
      vfs.publish(usage("u-child", "child-r"))
      vfs.publish(usage("u-root", "root"))
      await until(() => usageOf("child-r") !== undefined && usageOf("root") !== undefined)
      expect(usageOf("child-r")).toMatchObject({ agent: RESULT_AGENT, work_unit_id: "unit-r" })
      expect(usageOf("root")).toMatchObject({ agent: "harness", work_unit_id: "" })
    } finally { await vfs.stop() }
  })

  test("a delegation stays recorded until its end is", async () => {
    let recorded: unknown
    const holder: { vfs?: Awaited<ReturnType<typeof startVfs>> } = {}
    const vfs = await startVfs({ sessions: producerSessions, respond: (call) => {
      if (call.kind === "dispatch" && call.command === "finish") recorded = holder.vfs?.storage.get(DELEGATIONS_KEY)
      return acceptInputs(call)
    } })
    holder.vfs = vfs
    try {
      await admitProducer(vfs)
      await vfs.run("deliver_result", { result_ids: [4] }, { sessionID: "child-r", agent: RESULT_AGENT })
      await vfs.fire(vfs.tool, "execute.after", finished())
      expect(Object.keys(recorded as object)).toEqual(["root:call-r"])
      expect(vfs.storage.get(DELEGATIONS_KEY)).toEqual({})
    } finally { await vfs.stop() }
  })

  test("a deleted background child ends its delegation", async () => {
    const vfs = await startVfs({ sessions: producerSessions })
    try {
      await vfs.run("dispatch_inputs", { work_unit_id: "unit-r", none: true }, orchestrator)
      await vfs.fire(vfs.tool, "execute.before", { tool: "subagent", sessionID: "root", id: "call-r", input: { description: "unit-r", agent: RESULT_AGENT, background: true } })
      await vfs.fire(vfs.session, "context", { tools: {}, sessionID: "child-r", agent: RESULT_AGENT, system: [] })
      await vfs.fire(vfs.tool, "execute.after", finished())
      vfs.publish({ id: "evt-gone", created: 1, type: "session.deleted", data: { sessionID: "child-r" } } as OpenCodeEvent)
      await until(() => vfs.requests("dispatch", "finish").length > 0)
      expect(vfs.requests("dispatch", "finish")).toEqual([{ action: "finish", event: "unit-r", dispatch: "root:call-r", session: "root" }])
      expect(vfs.storage.get(DELEGATIONS_KEY)).toEqual({})
    } finally { await vfs.stop() }
  })

  test("a settled inherited delegation is not reconciled again over a later attempt", async () => {
    const vfs = await startVfs({
      sessions: producerSessions,
      storage: { [DELEGATIONS_KEY]: { "old:1": { unit: "unit-r", root: "root", agent: RESULT_AGENT, background: true, child: "child-r" } } },
    })
    try {
      vfs.publish({ id: "evt-idle", created: 1, type: "session.idle", data: { sessionID: "child-r" } } as OpenCodeEvent)
      await until(() => vfs.requests("dispatch", "finish").length > 0)
      await vfs.run("dispatch_inputs", { work_unit_id: "unit-r", none: true }, orchestrator)
      await vfs.fire(vfs.tool, "execute.before", { tool: "subagent", sessionID: "root", id: "call-r", input: { description: "unit-r", agent: RESULT_AGENT } })
      expect(vfs.requests("dispatch", "uncertain")).toEqual([])
      expect(vfs.requests("dispatch", "reconcile")).toEqual([])
      expect(Object.keys(vfs.storage.get(DELEGATIONS_KEY) as object)).toEqual(["root:call-r"])
    } finally { await vfs.stop() }
  })

  test("a launch the core refuses is not remembered", async () => {
    const vfs = await startVfs({ respond: (call) => call.kind === "dispatch" && call.command === "launch" ? { code: 1, err: "unit already settled" } : undefined })
    try {
      await vfs.run("dispatch_inputs", { work_unit_id: "unit-r", none: true }, orchestrator)
      await expect(vfs.fire(vfs.tool, "execute.before", { tool: "subagent", sessionID: "root", id: "call-r", input: { description: "unit-r", agent: RESULT_AGENT } })).rejects.toThrow("unit already settled")
      expect(vfs.storage.get(DELEGATIONS_KEY)).toEqual({})
    } finally { await vfs.stop() }
  })

  test("only the root orchestrator going idle asks whether maintenance is due", async () => {
    const vfs = await startVfs({ sessions: unitSessions })
    try {
      await vfs.fire(vfs.tool, "execute.after", { tool: "vfs_write", sessionID: "dispatch", id: "t1", agent: "dev" })
      vfs.publish({ id: "evt-leaf", created: 1, type: "session.idle", data: { sessionID: "dispatch" } } as OpenCodeEvent)
      vfs.publish({ id: "evt-root", created: 2, type: "session.idle", data: { sessionID: "root" } } as OpenCodeEvent)
      await until(() => vfs.requests("gc", "tick").length > 0)
      expect(vfs.requests("gc", "tick")).toEqual([{ action: "tick", session: "root" }])
    } finally { await vfs.stop() }
  })
})

describe("maintenance cycle", () => {
  test("a restart aborts a stale cycle and leaves stored delegations to the delegation path", async () => {
    const vfs = await startVfs({
      storage: { [DELEGATIONS_KEY]: { "old:1": { unit: "u-old", root: "root" } } },
      respond: (call) => call.kind === "gc" && call.command === "status" ? json(coordinator(gcCycle("verify", { verifier: "gcv" }))) : undefined,
    })
    try {
      await vfs.run("gc_request", {}, orchestrator)
      await until(() => vfs.log.prompts.length > 0)
      expect(vfs.requests("dispatch", "uncertain")).toEqual([])
      expect(vfs.storage.get(DELEGATIONS_KEY)).toEqual({ "old:1": { unit: "u-old", root: "root" } })
      expect(vfs.log.interrupts).toContain("gcv")
      expect(vfs.actions("gc")).toContain("recover")
      expect(vfs.log.prompts[0].sessionID).toBe("gcv")
      expect(vfs.log.prompts[0].text).toContain("Maintenance cycle cycle-1; mandate complexity.")
      expect(vfs.log.prompts[0].text).toContain("gc_delta")
    } finally { await vfs.stop() }
  })

  test("concurrent startup hooks reconcile a stored delegation once", async () => {
    const vfs = await startVfs({
      storage: { [DELEGATIONS_KEY]: { "old:1": { unit: "u-old", root: "root" } } },
    })
    try {
      await Promise.all([
        vfs.fire(vfs.tool, "execute.before", { tool: "subagent", sessionID: "root", id: "call-1", input: { description: "new-1", agent: RESULT_AGENT } }),
        vfs.fire(vfs.tool, "execute.before", { tool: "subagent", sessionID: "root", id: "call-2", input: { description: "new-2", agent: RESULT_AGENT } }),
      ].map(async (hook) => { try { await hook } catch { /* the new units have no declared inputs */ } }))
      expect(vfs.requests("dispatch", "uncertain")).toEqual([{ action: "uncertain", event: "u-old", dispatch: "old:1", session: "root" }])
      expect(vfs.requests("dispatch", "reconcile")).toEqual([{ action: "reconcile", event: "u-old", dispatch: "old:1", session: "root", pass: false }])
      expect(vfs.storage.get(DELEGATIONS_KEY)).toEqual({})
    } finally { await vfs.stop() }
  })

  test("a stored delegation that fails to reconcile is kept, never blocks a delegation, and is retried", async () => {
    let failing = true
    const vfs = await startVfs({
      storage: { [DELEGATIONS_KEY]: { "old:1": { unit: "u-old", root: "root" } } },
      respond: (call) => failing && call.kind === "dispatch" && call.command === "reconcile" ? { code: 1, err: "history is locked" } : undefined,
    })
    const launch = (id: string) => expect(vfs.fire(vfs.tool, "execute.before", { tool: "subagent", sessionID: "root", id, input: { description: `new-${id}`, agent: RESULT_AGENT } }))
      // The delegation gets past the cleanup and is refused only for its own missing inputs.
      .rejects.toThrow("dispatch_inputs")
    try {
      await launch("call-1")
      expect(vfs.storage.get(DELEGATIONS_KEY)).toEqual({ "old:1": { unit: "u-old", root: "root" } })
      failing = false
      await launch("call-2")
      expect(vfs.storage.get(DELEGATIONS_KEY)).toEqual({})
      expect(vfs.requests("dispatch", "reconcile")).toHaveLength(2)
    } finally { await vfs.stop() }
  })

  test("a delegation this process admitted is never reconciled as dead by a later hook", async () => {
    const vfs = await startVfs()
    try {
      for (const unit of ["unit-1", "unit-2"]) {
        await vfs.run("dispatch_inputs", { work_unit_id: unit, none: true }, orchestrator)
        await vfs.fire(vfs.tool, "execute.before", { tool: "subagent", sessionID: "root", id: `call-${unit}`, input: { description: unit, agent: RESULT_AGENT } })
      }
      expect(vfs.requests("dispatch", "admit")).toHaveLength(2)
      expect(vfs.requests("dispatch", "uncertain")).toEqual([])
      expect(vfs.requests("dispatch", "reconcile")).toEqual([])
      expect(Object.keys(vfs.storage.get(DELEGATIONS_KEY) as object)).toHaveLength(2)
    } finally { await vfs.stop() }
  })

  test("the attached verifier session reaches only its own gc tools, once per phase", async () => {
    const verifier: Ctx = { sessionID: "gcv", agent: "verify" }
    const vfs = await startVfs({
      respond: (call) => {
        if (call.kind !== "gc") return undefined
        if (call.command === "status") return json(coordinator(gcCycle("verify", { verifier: "gcv" })))
        if (call.command === "delta") return json({ revision: 1, delta_hash: "h1", files: { "a.go": "package a" } })
        return undefined
      },
    })
    try {
      await vfs.run("gc_request", {}, orchestrator)
      await until(() => vfs.log.prompts.length > 0)
      expect(JSON.parse((await vfs.run("gc_delta", {}, verifier)).content)).toEqual({ revision: 1, delta_hash: "h1", files: { "a.go": "package a" } })
      await expect(vfs.run("gc_delta", {}, { sessionID: "gcv", agent: "simplify" })).rejects.toThrow("GC delta requires the attached verifier session")
      await expect(vfs.run("gc_findings", {}, verifier)).rejects.toThrow("GC findings requires the attached collector session")

      const outside = { tool: "write", sessionID: "gcv", id: "t1", agent: "verify" }
      await expect(vfs.fire(vfs.tool, "execute.before", outside)).rejects.toThrow("This maintenance cycle does not use that tool")
      await vfs.fire(vfs.tool, "execute.before", { ...outside, tool: "read" })
      await vfs.fire(vfs.tool, "execute.before", { ...outside, tool: "gc_verdict" })
      // The verifier keeps what an ordinary verification uses: web and Engram reads.
      for (const tool of ["webfetch", "websearch", "engram_mem_search", "engram_mem_get_observation"]) await vfs.fire(vfs.tool, "execute.before", { ...outside, tool })

      vfs.publish({ id: "evt-idle", created: 1, type: "session.idle", data: { sessionID: "gcv" } } as OpenCodeEvent)
      await sleep(QUIET_MS)
      expect(vfs.log.prompts).toHaveLength(1)
      expect(vfs.requests("gc", "tick")).toEqual([])
    } finally { await vfs.stop() }
  })

  test("an authorized collector stages under the cycle's own identity", async () => {
    const collector: Ctx = { sessionID: "gcc", agent: "simplify" }
    let authorization: unknown = gcCycle("investigate", { collector: "gcc" }, { author_key: "author-gc" })
    const vfs = await startVfs({
      respond: (call) => {
        if (call.kind !== "gc") return undefined
        if (call.command === "status") return json(coordinator(gcCycle("investigate", { collector: "gcc" })))
        if (call.command === "authorize") return json(authorization)
        return undefined
      },
    })
    try {
      await vfs.run("gc_request", {}, orchestrator)
      await until(() => vfs.log.prompts.length > 0)
      expect(vfs.log.prompts[0].text).toContain("gc_findings")

      authorization = gcCycle("investigate", { collector: "gcc" })
      await expect(vfs.run("gc_authorize", {}, collector)).rejects.toThrow("GC authorization response omitted its binding plan")

      authorization = gcCycle("investigate", { collector: "gcc" }, { author_key: "author-gc" })
      await vfs.run("gc_authorize", {}, collector)
      expect(vfs.storage.get("takt/vfs/bindings")).toMatchObject([{ key: "author-gc", agent: "simplify", session: "root", dispatch: "gcc", unit: "cycle-1", cycle: "cycle-1" }])

      await vfs.run("vfs_write", { path: "a.go", content: "package a" }, collector)
      expect(vfs.requests("vfs", "op")[0]).toMatchObject({ session_id: "root", work_unit_id: "cycle-1", cycle_id: "cycle-1", author_key: "author-gc", action: "create" })
    } finally { await vfs.stop() }
  })

  test("a pump that fails aborts the cycle and stops its lane", async () => {
    const vfs = await startVfs({
      promptError: "prompt refused",
      respond: (call) => call.kind === "gc" && call.command === "status" ? json(coordinator(gcCycle("verify", { verifier: "gcv" }))) : undefined,
    })
    try {
      await vfs.run("gc_request", {}, orchestrator)
      await until(() => vfs.actions("gc").includes("abort"))
      expect(vfs.requests("gc", "abort")[0]).toMatchObject({ action: "abort", evidence: "prompt refused" })
      expect(vfs.log.interrupts.filter((id) => id === "gcv").length).toBeGreaterThanOrEqual(2)
      expect(vfs.errors.some(([label]) => label === "Takt GC")).toBe(true)
    } finally { await vfs.stop() }
  })

  test("a lane's binding names its cycle and is forgotten once that cycle is over", async () => {
    const lane = { key: "author-gc", agent: "simplify", session: "root", dispatch: "gcc", unit: "cycle-0", revision: 1, deltaHash: "d1", cycle: "cycle-0" }
    const ordinary = { key: "author-1", agent: "dev", session: "root", dispatch: "dispatch", unit: "unit-a", revision: 1, deltaHash: "d1" }
    const vfs = await startVfs({ storage: { "takt/vfs/bindings": [lane, ordinary] } })
    try {
      await vfs.run("gc_request", {}, orchestrator)
      await until(() => (vfs.storage.get("takt/vfs/bindings") as unknown[]).length === 1)
      expect(vfs.storage.get("takt/vfs/bindings")).toEqual([ordinary])
    } finally { await vfs.stop() }
  })

  test("the orchestrator is told when maintenance starts and when it concludes, and only then", async () => {
    let state: Reply = json(coordinator(gcCycle("baseline", {})))
    const vfs = await startVfs({ respond: (call) => call.kind === "gc" && call.command === "status" ? state : undefined })
    try {
      const notified = async () => {
        const event = { agent: ORCHESTRATOR, system: [] as Array<{ type: string; text: string }> }
        await vfs.fire(vfs.session, "context", event)
        return event.system.map(({ text }) => text)
      }
      expect((await notified())[0]).toContain("A cleanup cycle is running")
      state = json(coordinator())
      expect((await notified())[0]).toContain("The cleanup cycle concluded")
      expect(await notified()).toEqual([])
      state = { code: 1, err: "status unavailable" }
      expect(await notified()).toEqual([])
    } finally { await vfs.stop() }
  })
})

describe("interlocutor switch", () => {
  const switchReply = (call: Call) => call.kind === "dispatch" && call.command === "switch" ? { out: "{}" } : undefined
  const envelope = { result: "Standard", additional_context: "done", extra_artifacts: [], memory: [3] }
  const request = { target_agent: "dev", objective: "Ship it", requirements: "typed" }

  test("only the root orchestrator switches or aborts", async () => {
    const vfs = await startVfs({ sessions: { leaf: { parentID: "root" } } })
    try {
      const leaf: Ctx = { sessionID: "leaf", agent: ORCHESTRATOR }
      await expect(vfs.run("dispatch_switch", request, leaf)).rejects.toThrow("dispatch_switch belongs to the root orchestrator")
      await expect(vfs.run("dispatch_abort_switch", { reason: "r" }, leaf)).rejects.toThrow("dispatch_abort_switch belongs to the root orchestrator")
      await expect(vfs.run("dispatch_abort_switch", { reason: "r" }, orchestrator)).rejects.toThrow("no active interlocutor switch to abort")
    } finally { await vfs.stop() }
  })

  test("a switch the core refuses stops the child it created", async () => {
    const vfs = await startVfs({ respond: (call) => call.kind === "dispatch" && call.command === "switch" ? { code: 1, err: "no capacity" } : undefined })
    try {
      await expect(vfs.run("dispatch_switch", request, orchestrator)).rejects.toThrow("dispatch switch: no capacity")
      expect(vfs.log.created).toEqual([{ title: "Takt switch: dev", agent: "dev", metadata: { takt_switch: "root" } }])
      expect(vfs.log.interrupts).toEqual(["child"])
      expect(vfs.log.prompts).toEqual([])
    } finally { await vfs.stop() }
  })

  test("a user abort ends the lent session and clears the switch", async () => {
    const vfs = await startVfs({ respond: switchReply })
    try {
      await vfs.run("dispatch_switch", request, orchestrator)
      expect(vfs.log.prompts[0]).toMatchObject({ sessionID: "child" })
      expect(vfs.log.prompts[0].text).toBe("You are taking over as the active interlocutor for this workspace.\nObjective: Ship it\nRequirements: typed")
      await vfs.run("dispatch_abort_switch", { reason: "changed course" }, orchestrator)
      expect(vfs.requests("dispatch", "abort_switch")).toEqual([{ action: "abort_switch", session: "root", child: "child", evidence: "changed course", origin: "user" }])
      expect(vfs.log.interrupts).toEqual(["child"])
      await expect(vfs.run("dispatch_abort_switch", { reason: "again" }, orchestrator)).rejects.toThrow("no active interlocutor switch to abort")
    } finally { await vfs.stop() }
  })

  test("a handoff returns the envelope to the root and ends the holder's turn", async () => {
    const vfs = await startVfs({
      sessions: { child: { metadata: { takt_switch: "root" } } },
      respond: (call) => call.kind === "dispatch" && call.command === "handoff" ? json(envelope) : switchReply(call),
    })
    try {
      await vfs.run("dispatch_switch", request, orchestrator)
      const holder: Ctx = { sessionID: "child", agent: "dev" }
      expect((await vfs.run("dispatch_handoff", { result: "Standard", additional_context: "done", extra_artifacts: [], result_ids: [3] }, holder)).content).toBe("")
      expect(vfs.requests("dispatch", "handoff")[0]).toMatchObject({ session: "root", child: "child", agent: "dev", result_ids: [3] })
      expect(vfs.log.synthetics).toHaveLength(1)
      expect(vfs.log.synthetics[0].sessionID).toBe("root")
      expect(vfs.log.synthetics[0].text).toContain("The interface is back from dev.")
      expect(JSON.parse(vfs.log.synthetics[0].text.split("Envelope:\n")[1])).toEqual(envelope)
      await until(() => vfs.log.interrupts.includes("child"))
      expect(vfs.log.interrupts).toEqual(["child"])
      await expect(vfs.run("dispatch_abort_switch", { reason: "late" }, orchestrator)).rejects.toThrow("no active interlocutor switch to abort")
    } finally { await vfs.stop() }
  })

  test("a lent session that dies is reported as aborted to the root", async () => {
    const vfs = await startVfs({ respond: (call) => call.kind === "dispatch" && call.command === "abort_switch" ? json(envelope) : switchReply(call) })
    try {
      await vfs.run("dispatch_switch", request, orchestrator)
      vfs.publish({ id: "evt-other", created: 1, type: "session.deleted", data: { sessionID: "unrelated" } } as OpenCodeEvent)
      vfs.publish({ id: "evt-dead", created: 2, type: "session.deleted", data: { sessionID: "child" } } as OpenCodeEvent)
      await until(() => vfs.log.synthetics.length > 0)
      expect(vfs.requests("dispatch", "abort_switch")).toEqual([{ action: "abort_switch", session: "root", child: "child", evidence: "session ended", origin: "harness" }])
      expect(vfs.log.synthetics[0].text).toContain("a lent session that ended without a handoff")
    } finally { await vfs.stop() }
  })

  test("a failed abort report is logged, not thrown", async () => {
    const vfs = await startVfs({ respond: (call) => call.kind === "dispatch" && call.command === "abort_switch" ? { code: 1, err: "core down" } : switchReply(call) })
    try {
      await vfs.run("dispatch_switch", request, orchestrator)
      vfs.publish({ id: "evt-dead", created: 1, type: "session.deleted", data: { sessionID: "child" } } as OpenCodeEvent)
      await until(() => vfs.errors.length > 0)
      expect(vfs.errors[0][0]).toBe("Takt interlocutor abort_switch")
      expect(vfs.log.synthetics).toEqual([])
    } finally { await vfs.stop() }
  })
})

describe("path ownership", () => {
  const edit = (resources: string[]): HookEvent => ({ action: "edit", effect: "allow", resources, sessionID: "dispatch", agent: "dev" })

  test("an unreadable ownership store denies the edit", async () => {
    const vfs = await startVfs({ sessions: unitSessions, respond: (call) => call.command === "claims" ? { code: 1, err: "store offline" } : undefined })
    try {
      const event = edit([`${WORKSPACE}/src/a.ts`])
      await vfs.fire(vfs.permission, "evaluate", event)
      expect(event.effect).toBe("deny")
      expect(event.message).toBe("Takt refused this edit: path ownership could not be read")
    } finally { await vfs.stop() }
  })

  test("a held path is denied however it is spelled, and settled claims hold nothing", async () => {
    const vfs = await startVfs({
      sessions: unitSessions,
      claims: [claim({ key: "idle", scope: ["src/free.ts"] }), claim({ key: "live", work_unit_id: "unit-b", active: true, scope: ["src/held.ts"] })],
    })
    try {
      const relative = edit(["./src/held.ts"])
      await vfs.fire(vfs.permission, "evaluate", relative)
      expect(relative.effect).toBe("deny")
      expect(relative.message).toBe("Takt refused this edit: src/held.ts is held by work unit unit-b (agent dev); wait for its consolidation or discard it")

      const free = edit([`${WORKSPACE}/src/free.ts`])
      await vfs.fire(vfs.permission, "evaluate", free)
      expect(free.effect).toBe("allow")

      const blank = edit([""])
      await vfs.fire(vfs.permission, "evaluate", blank)
      expect(blank.effect).toBe("allow")
      expect(vfs.actions("vfs").filter((command) => command === "claims")).toHaveLength(2)
    } finally { await vfs.stop() }
  })
})

describe("claims and consolidation", () => {
  test("releasing a claim asks only when an agent is working under it, and never drops staged work", async () => {
    const vfs = await startVfs({
      claims: [
        claim({ key: "k-pending", pending: true, scope: ["a.go"] }),
        claim({ key: "k-active", active: true, scope: ["b.go", "c.go"] }),
        claim({ key: "k-staged", staged: true, scope: ["d.go"] }),
        claim({ key: "k-idle" }),
        claim({ key: "k-old", root_session_id: "old-root" }),
      ],
    })
    try {
      await Promise.all([
        expect(vfs.run("claim_release", { claim_key: "k-active" }, orchestrator)).rejects.toThrow("(instance dev, active) owns [b.go, c.go]"),
        expect(vfs.run("claim_release", { claim_key: "k-staged" }, orchestrator)).rejects.toThrow("holds staged work for [d.go]"),
      ])
      expect(vfs.actions("vfs")).not.toContain("release")
      for (const key of ["k-pending", "k-idle", "k-old"]) await vfs.run("claim_release", { claim_key: key }, orchestrator)
      await vfs.run("claim_release", { claim_key: "k-active", confirmed: true }, orchestrator)
      expect(vfs.requests("vfs", "release")).toEqual([
        expect.objectContaining({ session_id: "root", key: "k-pending" }),
        expect.objectContaining({ session_id: "root", key: "k-idle" }),
        expect.objectContaining({ session_id: "root", key: "k-old" }),
        expect.objectContaining({ session_id: "root", key: "k-active" }),
      ])
    } finally { await vfs.stop() }
  })

  test("a released claim's binding speaks for nobody afterwards", async () => {
    const vfs = await startVfs({ sessions: unitSessions, claims: [claim({ key: "author-1", scope: ["src/a.go"] })] })
    try {
      await vfs.run("vfs_bind", { scope: ["src/a.go"] })
      await vfs.run("claim_release", { claim_key: "author-1" }, orchestrator)
      await expect(vfs.run("vfs_write", { path: "src/a.go", content: "x" })).rejects.toThrow("no VFS binding in this delegation")
    } finally { await vfs.stop() }
  })

  test("binding again to an earlier key makes it the one a call without author_key uses", async () => {
    const keys = ["k1", "k2", "k1"]
    const vfs = await startVfs({ sessions: unitSessions, respond: (call) => call.command === "bind"
      ? json({ ok: true, key: keys.shift(), attempt_id: "1", invariants_version: "inv", revision: 0, delta_hash: "d0" }) : undefined })
    try {
      for (const scope of [["a.go"], ["b.go"], ["a.go"]]) await vfs.run("vfs_bind", { scope })
      await vfs.run("vfs_write", { path: "a.go", content: "x" })
      expect(vfs.requests("vfs", "op").at(-1)).toMatchObject({ author_key: "k1" })
    } finally { await vfs.stop() }
  })

  test("claim_assign with an author key asks to keep the staged work", async () => {
    const vfs = await startVfs()
    try {
      await vfs.run("dispatch_inputs", { work_unit_id: "unit-a", none: true }, orchestrator)
      await vfs.run("claim_assign", { work_unit_id: "unit-a", agent: "dev", scope: ["a.go", "b.go"], author_key: "author-key" }, orchestrator)
      expect(vfs.requests("vfs", "assign")).toEqual([expect.objectContaining({ author_key: "author-key", scope: ["a.go", "b.go"] })])
    } finally { await vfs.stop() }
  })

  test("a claim is assigned with the invariants declared for its unit, and not before they are", async () => {
    const vfs = await startVfs({ respond: (call) => call.kind === "dispatch" && call.command === "validate_inputs" ? { out: "{}" } : undefined })
    try {
      const assign = () => vfs.run("claim_assign", { work_unit_id: "unit-a", agent: "dev", scope: ["a.go"] }, orchestrator)
      await expect(assign()).rejects.toThrow("Declare the invariants work unit unit-a consumes")
      expect(vfs.requests("vfs", "assign")).toEqual([])
      await vfs.run("dispatch_inputs", { work_unit_id: "unit-a", result_ids: [7, 8] }, orchestrator)
      await assign()
      expect(vfs.requests("vfs", "assign")).toEqual([expect.objectContaining({ invariants: ["engram:7", "engram:8"] })])
    } finally { await vfs.stop() }
  })

  const verifierGrants = [
    { action: "*", resource: "*", effect: "deny" },
    ...["vfs_bind", "vfs_read", "vfs_verify"].map((name) => ({ action: name, resource: name, effect: "allow" })),
  ]
  const gateContext = () => ({ tools: Object.fromEntries(["vfs_bind", "vfs_read", "vfs_verify"].map((name) => [name, {}])), sessionID: "verify-child", agent: "verify", system: [] as Array<{ type: string; text: string }> })

  test("a verifier gate arrives bound to its author, with the author's files listed and no bind tool", async () => {
    const author = { key: "author-key", agent: "dev", session: "root", dispatch: "dev-child", unit: "writer", revision: 1, deltaHash: "hash" }
    const reviewer: Ctx = { sessionID: "verify-child", agent: "verify" }
    const vfs = await startVfs({
      sessions: { "verify-child": { parentID: "root", title: "gate" } },
      permissions: { verify: verifierGrants },
      storage: { "takt/vfs/bindings": [author] },
      claims: [
        { key: "author-key", root_session_id: "root", work_unit_id: "writer", agent_id: "dev", target_instance: "dev", active: true, scope: ["src/a.go", "src/b.go"] },
        { key: "gate-key", root_session_id: "root", work_unit_id: "gate", agent_id: "verify", target_instance: "verify", pending: true, scope: [], author_key: "author-key" },
      ],
      respond: (call) => call.command === "bind" ? json({ ok: true, key: "gate-key", attempt_id: "a1", invariants_version: "v1" }) : undefined,
    })
    try {
      const event = gateContext()
      await vfs.fire(vfs.session, "context", event)
      expect(vfs.requests("vfs", "bind")).toEqual([expect.objectContaining({ scope: [], author_key: "author-key", specialist: "verify" })])
      expect(Object.keys(event.tools)).toEqual(["vfs_read", "vfs_verify"])
      expect(event.system[0].text).toContain("author_key author-key: src/a.go, src/b.go")
      await vfs.run("vfs_read", { path: "src/a.go" }, reviewer)
      expect(vfs.requests("vfs", "op")).toEqual([expect.objectContaining({ view_key: "author-key" })])
    } finally { await vfs.stop() }
  })

  test("one verifier unit judges several authors, each through its own gate", async () => {
    const authors = ["author-a", "author-b"].map((key) => ({ key, agent: "dev", session: "root", dispatch: `${key}-child`, unit: key, revision: 1, deltaHash: `hash-${key}` }))
    const reviewer: Ctx = { sessionID: "verify-child", agent: "verify" }
    const gate = (author: string) => ({ key: `gate-${author}`, root_session_id: "root", work_unit_id: "gate", agent_id: "verify", target_instance: "verify", pending: true, scope: [], author_key: author })
    const vfs = await startVfs({
      sessions: { "verify-child": { parentID: "root", title: "gate" } },
      permissions: { verify: verifierGrants },
      storage: { "takt/vfs/bindings": authors },
      claims: [gate("author-a"), gate("author-b")],
      respond: (call) => call.command === "bind" ? json({ ok: true, key: `gate-${call.request.author_key}`, attempt_id: "a1", invariants_version: "v1" }) : undefined,
    })
    try {
      await vfs.fire(vfs.session, "context", gateContext())
      expect(vfs.requests("vfs", "bind")).toEqual(authors.map(({ key }) => expect.objectContaining({ scope: [], author_key: key })))
      await vfs.run("vfs_read", { path: "src/a.go", author_key: "author-a" }, reviewer)
      await vfs.run("vfs_read", { path: "src/b.go", author_key: "author-b" }, reviewer)
      expect(vfs.requests("vfs", "op")).toEqual([expect.objectContaining({ view_key: "author-a" }), expect.objectContaining({ view_key: "author-b" })])
      await expect(vfs.run("vfs_read", { path: "src/c.go", author_key: "author-z" }, reviewer)).rejects.toThrow("is not one of your gates")
      for (const { key } of authors) await vfs.run("vfs_verify", { author_key: key, pass: true, finding: "holds" }, reviewer)
      expect(vfs.requests("vfs", "verify")).toEqual(authors.map(({ key }) => expect.objectContaining({ author_key: key, verifier_key: `gate-${key}`, pass: true })))
      const again = gateContext()
      await vfs.fire(vfs.session, "context", again)
      expect(vfs.requests("vfs", "bind")).toHaveLength(2)
    } finally { await vfs.stop() }
  })

  test("two verifiers judging the same author concurrently each read and attach a verdict", async () => {
    const author = { key: "author-a", agent: "dev", session: "root", dispatch: "author-a-child", unit: "author-a", revision: 1, deltaHash: "hash-a" }
    const gate = (unit: string) => ({ key: `gate-${unit}`, root_session_id: "root", work_unit_id: unit, agent_id: "verify", target_instance: "verify", pending: true, scope: [], author_key: "author-a" })
    const judges = ["judge-1", "judge-2"].map((unit) => ({ unit, ctx: { sessionID: `${unit}-child`, agent: "verify" } as Ctx }))
    const vfs = await startVfs({
      sessions: Object.fromEntries(judges.map(({ unit, ctx }) => [ctx.sessionID, { parentID: "root", title: unit }])),
      permissions: { verify: verifierGrants },
      storage: { "takt/vfs/bindings": [author] },
      claims: judges.map(({ unit }) => gate(unit)),
      respond: (call) => call.command === "bind" ? json({ ok: true, key: `gate-${call.request.work_unit_id}`, attempt_id: "a1", invariants_version: "v1" }) : undefined,
    })
    try {
      for (const { ctx } of judges) await vfs.fire(vfs.session, "context", { ...gateContext(), sessionID: ctx.sessionID })
      await Promise.all(judges.map(({ ctx }) => vfs.run("vfs_read", { path: "src/a.go", author_key: "author-a" }, ctx)))
      await Promise.all(judges.map(({ ctx }) => vfs.run("vfs_verify", { author_key: "author-a", pass: true, finding: "holds" }, ctx)))
      expect(vfs.requests("vfs", "verify").map((request) => request.verifier_key)).toEqual(["gate-judge-1", "gate-judge-2"])
    } finally { await vfs.stop() }
  })

  test("a gate reads by path without naming its author: the path's owner decides", async () => {
    const authors = ["author-a", "author-b"].map((key) => ({ key, agent: "dev", session: "root", dispatch: `${key}-child`, unit: key, revision: 1, deltaHash: `hash-${key}` }))
    const reviewer: Ctx = { sessionID: "verify-child", agent: "verify" }
    const gate = (author: string) => ({ key: `gate-${author}`, root_session_id: "root", work_unit_id: "gate", agent_id: "verify", target_instance: "verify", pending: true, scope: [], author_key: author })
    const owned = (author: string, path: string) => ({ key: author, root_session_id: "root", work_unit_id: author, agent_id: "dev", target_instance: "dev", active: true, scope: [path] })
    const vfs = await startVfs({
      sessions: { "verify-child": { parentID: "root", title: "gate" } },
      permissions: { verify: verifierGrants },
      storage: { "takt/vfs/bindings": authors },
      claims: [owned("author-a", "src/a.go"), owned("author-b", "src/b.go"), gate("author-a"), gate("author-b")],
      respond: (call) => call.command === "bind" ? json({ ok: true, key: `gate-${call.request.author_key}`, attempt_id: "a1", invariants_version: "v1" }) : undefined,
    })
    try {
      await vfs.fire(vfs.session, "context", gateContext())
      await vfs.run("vfs_read", { path: "src/b.go" }, reviewer)
      await vfs.run("vfs_read", { path: "src/a.go" }, reviewer)
      // A path no author owns is workspace content, readable through any gate.
      await vfs.run("vfs_read", { path: "README.md" }, reviewer)
      expect(vfs.requests("vfs", "op")).toEqual([
        expect.objectContaining({ view_key: "author-b" }), expect.objectContaining({ view_key: "author-a" }), expect.objectContaining({ view_key: "author-a" }),
      ])
    } finally { await vfs.stop() }
  })

  test("a verdict cannot silently move to a version changed after the verifier read it", async () => {
    const reviewer = { sessionID: "verify-child", agent: "verify" }
    const author = { key: "author-key", agent: "dev", session: "root", dispatch: "dev-child", unit: "writer", revision: 1, deltaHash: "hash-1" }
    const vfs = await startVfs({
      sessions: { "verify-child": { parentID: "root", title: "gate" } },
      permissions: { verify: verifierGrants },
      storage: { "takt/vfs/bindings": [author] },
      claims: [{ key: "gate-key", root_session_id: "root", work_unit_id: "gate", agent_id: "verify",
        target_instance: "verify", pending: true, scope: [], author_key: author.key }],
      respond: call => {
        if (call.command === "bind") return json({ ok: true, key: "gate-key" })
        if (call.command === "op" && call.request.action === "create") return json({ ok: true, revision: 2, delta_hash: "hash-2" })
        if (call.command === "verify") return json({ ok: false, error: "invalid verdict: staged version changed" })
        return undefined
      },
    })
    try {
      await vfs.fire(vfs.session, "context", { ...reviewer, system: [] })
      await vfs.run("vfs_read", { path: "a.go", author_key: author.key }, reviewer)
      await vfs.run("vfs_write", { path: "a.go", content: "changed" }, { sessionID: "dev-child", agent: "dev" })
      await expect(vfs.run("vfs_verify", { author_key: author.key, pass: true, finding: "holds" }, reviewer)).rejects.toThrow(/staged version changed.*read it again with vfs_read/)
      expect(vfs.requests("vfs", "verify")[0]).toMatchObject({ expected_revision: 1, delta_hash: "hash-1" })
    } finally { await vfs.stop() }
  })

  describe("a verdict covers one consistent state of the author's work", () => {
    const reviewer = { sessionID: "verify-child", agent: "verify" }
    const author = { key: "author-key", agent: "dev", session: "root", dispatch: "dev-child", unit: "writer", revision: 1, deltaHash: "hash-1" }
    const startGate = () => {
      let revision = 1
      return startVfs({
        sessions: { "verify-child": { parentID: "root", title: "gate" } },
        permissions: { verify: verifierGrants },
        storage: { "takt/vfs/bindings": [{ ...author }] },
        claims: [{ key: "gate-key", root_session_id: "root", work_unit_id: "gate", agent_id: "verify",
          target_instance: "verify", pending: true, scope: [], author_key: author.key }],
        respond: call => {
          if (call.command === "bind") return json({ ok: true, key: "gate-key" })
          if (call.command === "op" && call.request.action === "create") { revision += 1; return json({ ok: true, revision, delta_hash: `hash-${revision}` }) }
          if (call.command === "op" && call.request.action === "read") return json({ ok: true, content: `content of ${call.request.path}` })
          return undefined
        },
      })
    }
    const authorWrites = (vfs: Awaited<ReturnType<typeof startVfs>>) => vfs.run("vfs_write", { path: "a.go", content: "changed" }, { sessionID: "dev-child", agent: "dev" })

    test("reads under an unchanged state carry no note and the verdict covers that state", async () => {
      const vfs = await startGate()
      try {
        await vfs.fire(vfs.session, "context", { ...reviewer, system: [] })
        const first = await vfs.run("vfs_read", { path: "a.go" }, reviewer)
        const second = await vfs.run("vfs_read", { path: "b.go" }, reviewer)
        expect(first.content).toBe("content of a.go")
        expect(second.content).toBe("content of b.go")
        await vfs.run("vfs_verify", { author_key: author.key, pass: true, finding: "holds" }, reviewer)
        expect(vfs.requests("vfs", "verify")[0]).toMatchObject({ expected_revision: 1, delta_hash: "hash-1" })
      } finally { await vfs.stop() }
    })

    test("a read after the author changed names the files read before it, and the verdict covers the new state", async () => {
      const vfs = await startGate()
      try {
        await vfs.fire(vfs.session, "context", { ...reviewer, system: [] })
        await vfs.run("vfs_read", { path: "a.go" }, reviewer)
        await authorWrites(vfs)
        const moved = await vfs.run("vfs_read", { path: "b.go" }, reviewer)
        expect(moved.content).toContain("a.go")
        expect(moved.content).toContain("content of b.go")
        await vfs.run("vfs_verify", { author_key: author.key, pass: true, finding: "holds" }, reviewer)
        expect(vfs.requests("vfs", "verify")[0]).toMatchObject({ expected_revision: 2, delta_hash: "hash-2" })
      } finally { await vfs.stop() }
    })

    test("each change restarts the list, so the note names only files read since the last one", async () => {
      const vfs = await startGate()
      try {
        await vfs.fire(vfs.session, "context", { ...reviewer, system: [] })
        await vfs.run("vfs_read", { path: "a.go" }, reviewer)
        await authorWrites(vfs)
        await vfs.run("vfs_read", { path: "b.go" }, reviewer)
        await vfs.run("vfs_read", { path: "c.go" }, reviewer)
        await authorWrites(vfs)
        const note = (await vfs.run("vfs_read", { path: "d.go" }, reviewer)).content
        expect(note).toContain("b.go, c.go")
        expect(note).not.toContain("a.go")
      } finally { await vfs.stop() }
    })
  })

  for (const count of [1, 2]) test(`one delegation prepares ${count} authors and its retry in CodeMode`, async () => {
    const authors = Array.from({ length: count }, (_, index) => ({
      key: `author-${index}`, agent: "dev", session: "root", dispatch: `dev-${index}`,
      unit: `writer-${index}`, revision: 1, deltaHash: `hash-${index}`,
    }))
    const claims: Record<string, unknown>[] = authors.map(author => ({
      key: author.key, root_session_id: "root", work_unit_id: author.unit,
      agent_id: "dev", target_instance: "dev", active: true, scope: [`${author.key}.go`],
    }))
    let generation = 0
    const reviewer = { sessionID: "verify-child", agent: "verify" }
    const vfs = await startVfs({
      sessions: { "verify-child": { parentID: "root", title: "gate" } },
      permissions: { verify: verifierGrants }, claims,
      storage: { "takt/vfs/bindings": authors },
      respond: call => {
        if (call.kind !== "vfs") return undefined
        if (call.command === "assign-verifier") {
          const key = `gate-${generation}-${call.request.author_key}`
          claims.push({ key, root_session_id: "root", work_unit_id: "gate", agent_id: "verify",
            target_instance: "verify", pending: true, scope: [], author_key: call.request.author_key })
          return json({ ok: true, key })
        }
        if (call.command === "bind") {
          const gate = claims.find(gate => gate.pending && gate.author_key === call.request.author_key)
          if (!gate) throw new Error("no pending gate")
          gate.pending = false
          gate.active = true
          return json({ ok: true, key: gate.key, attempt_id: String(generation), invariants_version: "inv" })
        }
        return undefined
      },
    })
    try {
      expect(() => vfs.run("claim_assign_verifier", {}, orchestrator)).toThrow("missing tool")
      expect(vfs.nativeSubagent.input).toHaveProperty("properties.author_keys")
      await vfs.run("dispatch_inputs", { work_unit_id: "gate", none: true }, orchestrator)
      for (generation = 1; generation <= 2; generation++) {
        const id = `review-${generation}`
        await vfs.fire(vfs.tool, "execute.before", { tool: "subagent", sessionID: "root", id,
          input: { agent: "verify", description: "gate", author_keys: authors.map(author => author.key) } })
        const event = { ...reviewer, system: [] as Array<{ text: string }> } // real CodeMode shape
        await vfs.fire(vfs.session, "context", event)
        expect(event.system.map(part => part.text).join("\n")).toContain("every gate is already bound")
        for (const author of authors) {
          await vfs.run("vfs_read", { path: `${author.key}.go`, author_key: author.key }, reviewer)
          await vfs.run("vfs_verify", { author_key: author.key, pass: true, finding: "reviewed" }, reviewer)
          expect(vfs.requests("vfs", "verify").at(-1)).toMatchObject({
            author_key: author.key, verifier_key: `gate-${generation}-${author.key}`,
            expected_revision: 1, delta_hash: author.deltaHash,
          })
        }
        await vfs.fire(vfs.tool, "execute.after", { tool: "subagent", sessionID: "root", id, status: "completed", result: {} })
      }
      expect(vfs.requests("vfs", "assign-verifier")).toHaveLength(count * 2)
      expect(vfs.requests("vfs", "bind")).toHaveLength(count * 2)
      const ids = vfs.requests("vfs", "op").map(request => request.call_id)
      expect(ids.every(id => typeof id === "string" && id.length > 0)).toBe(true)
      expect(new Set(ids).size).toBe(ids.length)
      const governed = vfs.calls.filter(call => (call.kind === "dispatch" && call.command === "admit") || call.command === "assign-verifier")
      expect(governed[0].command).toBe("admit")
    } finally { await vfs.stop() }
  })

  test("a judge delegated again under another unit no longer judges through its earlier gate", async () => {
    const author = { key: "author-0", agent: "dev", session: "root", dispatch: "dev-0", unit: "writer-0", revision: 1, deltaHash: "hash-0" }
    const claims: Record<string, unknown>[] = [{ key: author.key, root_session_id: "root", work_unit_id: author.unit,
      agent_id: "dev", target_instance: "dev", active: true, scope: ["author-0.go"] }]
    const first = { sessionID: "verify-1", agent: "verify" }
    const second = { sessionID: "verify-2", agent: "verify" }
    const vfs = await startVfs({
      sessions: { "verify-1": { parentID: "root", title: "gate-1" }, "verify-2": { parentID: "root", title: "gate-2" } },
      permissions: { verify: verifierGrants }, claims,
      storage: { "takt/vfs/bindings": [author] },
      respond: call => {
        if (call.kind !== "vfs") return undefined
        if (call.command === "assign-verifier") {
          const key = `gate-${String(call.request.work_unit_id)}`
          claims.push({ key, root_session_id: "root", work_unit_id: call.request.work_unit_id, agent_id: "verify",
            target_instance: "verify", pending: true, scope: [], author_key: call.request.author_key })
          return json({ ok: true, key })
        }
        if (call.command === "bind") {
          const gate = claims.find(gate => gate.pending && gate.work_unit_id === call.request.work_unit_id)
          if (!gate) throw new Error("no pending gate")
          gate.pending = false
          gate.active = true
          return json({ ok: true, key: gate.key, attempt_id: "1", invariants_version: "inv" })
        }
        return undefined
      },
    })
    try {
      for (const [unit, judge] of [["gate-1", first], ["gate-2", second]] as const) {
        await vfs.run("dispatch_inputs", { work_unit_id: unit, none: true }, orchestrator)
        await vfs.fire(vfs.tool, "execute.before", { tool: "subagent", sessionID: "root", id: unit,
          input: { agent: "verify", description: unit, author_keys: [author.key] } })
        await vfs.fire(vfs.session, "context", { ...judge, system: [] })
      }
      await expect(vfs.run("vfs_verify", { author_key: author.key, pass: false, finding: "stale" }, first)).rejects.toThrow("not assigned to this verifier delegation")
      await vfs.run("vfs_verify", { author_key: author.key, pass: true, finding: "reviewed" }, second)
      expect(vfs.requests("vfs", "verify")).toEqual([expect.objectContaining({ verifier_key: "gate-gate-2", author_key: author.key })])
    } finally { await vfs.stop() }
  })

  test.each([[[]], [["a", "a"]], [["a", " "]], ["a"]])("author_keys %j is refused before anything is admitted", async (authorKeys) => {
    const vfs = await startVfs({ permissions: { verify: verifierGrants } })
    try {
      await vfs.run("dispatch_inputs", { work_unit_id: "gate", none: true }, orchestrator)
      await expect(vfs.fire(vfs.tool, "execute.before", { tool: "subagent", sessionID: "root", id: "bad",
        input: { agent: "verify", description: "gate", author_keys: authorKeys } })).rejects.toThrow("unique, nonempty staged author keys")
      expect(vfs.actions("dispatch")).toEqual([])
    } finally { await vfs.stop() }
  })

  describe("a failed verifier assignment", () => {
    const assignment = async (failing: { release?: boolean, finish?: boolean }) => {
      const vfs = await startVfs({
        permissions: { verify: verifierGrants },
        respond: call => {
          if (call.kind === "vfs" && call.command === "assign-verifier") {
            return call.request.author_key === "b" ? { code: 1, err: "author b is gone" } : json({ ok: true, key: `gate-${call.request.author_key}` })
          }
          if (call.kind === "vfs" && call.command === "release" && failing.release) return { code: 1, err: "release failed" }
          if (call.kind === "dispatch" && call.command === "finish" && failing.finish) return { code: 1, err: "finish failed" }
          return undefined
        },
      })
      return vfs
    }
    const delegate = (vfs: Awaited<ReturnType<typeof startVfs>>) => vfs.run("dispatch_inputs", { work_unit_id: "gate", none: true }, orchestrator)
      .then(() => vfs.fire(vfs.tool, "execute.before", { tool: "subagent", sessionID: "root", id: "call",
        input: { agent: "verify", description: "gate", author_keys: ["a", "b", "c"] } }))

    test("releases the gates already prepared and finishes the dispatch", async () => {
      const vfs = await assignment({})
      try {
        await expect(delegate(vfs)).rejects.toThrow("author b is gone")
        expect(vfs.requests("vfs", "release")).toEqual([expect.objectContaining({ session_id: "root", key: "gate-a" })])
        expect(vfs.actions("dispatch")).toEqual(["admit", "finish"])
        expect(vfs.storage.has(DELEGATIONS_KEY)).toBe(false)
        expect(vfs.errors).toEqual([])
      } finally { await vfs.stop() }
    })

    test("keeps its own error when the cleanup fails too, and still attempts every step", async () => {
      const vfs = await assignment({ release: true, finish: true })
      try {
        await expect(delegate(vfs)).rejects.toThrow("author b is gone")
        expect(vfs.requests("vfs", "release")).toHaveLength(1)
        expect(vfs.actions("dispatch")).toEqual(["admit", "finish"])
        expect(vfs.errors.map(([label]) => label)).toEqual(["Takt gate release", "Takt dispatch finish"])
      } finally { await vfs.stop() }
    })
  })

  test("a specialist taking over staged work replaces the old binding and continues from its revision", async () => {
    const old = { key: "author-1", agent: "dev", session: "root", dispatch: "dev-child", unit: "unit-dev", revision: 3, deltaHash: "old" }
    const fixer: Ctx = { sessionID: "fix-child", agent: "fix" }
    const vfs = await startVfs({
      sessions: { "fix-child": { parentID: "root", title: "unit-fix" } },
      storage: { "takt/vfs/bindings": [old] },
      respond: (call) => call.command === "bind" ? json({ ok: true, key: "author-1", attempt_id: "2", invariants_version: "v1", revision: 3, delta_hash: "h3" }) : call.command === "op" ? json({ ok: true, revision: 4, delta_hash: "h4" }) : undefined,
    })
    try {
      await vfs.run("vfs_bind", { scope: ["src/a.go"] }, fixer)
      await vfs.run("vfs_write", { path: "src/a.go", content: "x" }, fixer)
      expect(vfs.requests("vfs", "op")).toEqual([expect.objectContaining({ agent_id: "fix", work_unit_id: "unit-fix", author_key: "author-1", expected_revision: 3 })])
    } finally { await vfs.stop() }
  })

  test("a consolidation blocked by a moved base tells the orchestrator to freeze and escalate", async () => {
    let failure = "physical base changed on src/a.go"
    const vfs = await startVfs({
      sessions: unitSessions,
      respond: (call) => call.command === "consolidate" ? json({ ok: false, error: failure }) : undefined,
    })
    try {
      await vfs.run("vfs_bind", { scope: ["src/a.go"] })
      await expect(vfs.run("vfs_consolidate", { author_key: "author-1", checkpoint: "cp" }, orchestrator))
        .rejects.toThrow("physical base changed on src/a.go; freeze this path and escalate with the report")
      failure = "disk full"
      await expect(vfs.run("vfs_consolidate", { author_key: "author-1", checkpoint: "cp" }, orchestrator)).rejects.toThrow(/^disk full$/)
      expect(vfs.storage.get("takt/vfs/bindings")).toMatchObject([{ key: "author-1" }])
    } finally { await vfs.stop() }
  })

  // Verdicts inform; the core only asks that the user accept consolidating work
  // a current verdict failed, and the plugin asks through the OpenCode service.
  describe("consolidating over a failing verdict", () => {
    const failing = "vfs: a current verification verdict failed; consolidating needs the user's explicit acceptance: gate-a: misses the edge case"
    const caller = { ...orchestrator, messageID: "message", id: "tool-call" }
    const withService = (effects: Array<"allow" | "deny" | "ask">) => {
      const created: Array<Record<string, unknown>> = []
      mock.module("@opencode/client/service", () => ({ Service: { discover: async () => ({ url: "http://service.invalid" }), headers: () => ({}) } }))
      mock.module("@opencode/client", () => ({
        OpenCode: { make: () => ({ permission: { create: async (request: Record<string, unknown>) => {
          created.push(request)
          return { effect: effects.shift() ?? "allow", id: `request-${created.length}` }
        } } }) },
      }))
      return created
    }
    const start = (refuse: boolean) => startVfs({
      sessions: unitSessions,
      respond: (call) => call.command === "consolidate"
        ? (refuse && call.request.accept_failing !== true ? json({ ok: false, error: failing }) : json({ ok: true }))
        : undefined,
    })

    test("the user's acceptance consolidates with it; the model never supplies it", async () => {
      const created = withService(["ask"])
      const vfs = await start(true)
      try {
        await vfs.run("vfs_bind", { scope: ["src/a.go"] })
        const done = vfs.run("vfs_consolidate", { author_key: "author-1", checkpoint: "cp", accept_failing: true }, caller)
        await until(() => created.length === 1)
        await sleep(1)
        expect(created[0]).toMatchObject({ sessionID: "root", action: "vfs_consolidate_failing", metadata: { findings: failing } })
        vfs.publish({ id: "evt-reply", created: 1, type: "permission.replied", data: { requestID: "request-1", reply: "once" } } as OpenCodeEvent)
        expect((await done).content).toBe("Consolidated to workspace")
        expect(vfs.requests("vfs", "consolidate").map((request) => request.accept_failing)).toEqual([undefined, true])
      } finally { await vfs.stop() }
    })

    test("a refusal keeps the work staged", async () => {
      const created = withService(["deny"])
      const vfs = await start(true)
      try {
        await vfs.run("vfs_bind", { scope: ["src/a.go"] })
        await expect(vfs.run("vfs_consolidate", { author_key: "author-1" }, caller)).rejects.toThrow("Permission denied: vfs_consolidate_failing")
        expect(created).toHaveLength(1)
        expect(vfs.requests("vfs", "consolidate")).toHaveLength(1)
        expect(vfs.storage.get("takt/vfs/bindings")).toMatchObject([{ key: "author-1" }])
      } finally { await vfs.stop() }
    })

    test("without a failing verdict nobody is asked", async () => {
      const created = withService([])
      const vfs = await start(false)
      try {
        await vfs.run("vfs_bind", { scope: ["src/a.go"] })
        expect((await vfs.run("vfs_consolidate", { author_key: "author-1" }, caller)).content).toBe("Consolidated to workspace")
        expect(created).toEqual([])
      } finally { await vfs.stop() }
    })
  })
})

describe("shell admission", () => {
  const command = "echo it's"
  const shellEvent = (extra: HookEvent = {}) => ({ tool: "shell", input: { command }, sessionID: "dispatch", id: "s1", agent: "dev", ...extra })
  const wrapperCommand = String.raw`exec /bin/sh -c 'supervised(wrapped:echo it'\''s|/private/tmp/confirm)'`

  test("an unbound specialist is admitted as an inspector and runs through the sandbox wrapper", async () => {
    mock.module("./takt-sandbox.mjs", () => ({
      wrap: (line: string) => Promise.resolve(`wrapped:${line}`),
      supervise: (wrapped: string, confirm: string) => `supervised(${wrapped}|${confirm})`,
    }))
    const vfs = await startVfs({ sessions: unitSessions, respond: (call) => call.command === "shell-prepare" ? shellReply({ cwd: "/elsewhere" }) : undefined })
    try {
      await vfs.fire(vfs.tool, "execute.before", shellEvent())
      expect(vfs.requests("vfs", "shell-prepare")[0]).toMatchObject({ session_id: "root", work_unit_id: "dispatch", agent_id: "dev", specialist: "dev", command })
      const created: HookEvent = { command }
      await vfs.fire(vfs.shell, "create.before", created)
      expect(created.command).toBe(command)
      const wrapper = readFileSync(String(created.shell), "utf8")
      expect(wrapper).toContain(`cd '/elsewhere' || exit ${SHELL_NOT_ADMITTED}`)
      expect(wrapper).toContain("export TMPDIR='/private/tmp/scratch'")
      expect(wrapper).toContain("export HOME='/private/tmp/scratch'")
      expect(wrapper).toContain(wrapperCommand)

      const replay: HookEvent = { command }
      await vfs.fire(vfs.shell, "create.before", replay)
      expect(String(replay.command)).toContain("this shell command was not admitted")
      expect(replay.shell).toBeUndefined()

      const approval: HookEvent = { action: "shell", effect: "allow", sessionID: "dispatch", agent: "dev" }
      await vfs.fire(vfs.permission, "evaluate", approval)
      expect([approval.effect, approval.message]).toEqual(["ask", "review each run"])
      const repeat: HookEvent = { action: "shell", effect: "allow", sessionID: "dispatch", agent: "dev" }
      await vfs.fire(vfs.permission, "evaluate", repeat)
      expect(repeat.effect).toBe("deny")

      await vfs.fire(vfs.tool, "execute.after", { tool: "shell", sessionID: "dispatch", id: "s1" })
      expect(existsSync(dirname(String(created.shell)))).toBe(false)
      expect(vfs.actions("vfs")).not.toContain("shell-import")
    } finally { await vfs.stop() }
  })

  test("a bound specialist's captured command is imported as a staged delta", async () => {
    mock.module("./takt-sandbox.mjs", () => ({
      wrap: (line: string) => Promise.resolve(`wrapped:${line}`),
      supervise: (wrapped: string, confirm: string) => `supervised(${wrapped}|${confirm})`,
    }))
    const vfs = await startVfs({
      sessions: unitSessions,
      respond: (call) => call.command === "shell-prepare" ? shellReply({ capture: true }) : call.command === "shell-import" ? json({ ok: true, revision: 5, delta_hash: "d5" }) : undefined,
    })
    try {
      await vfs.run("vfs_bind", { scope: ["src/a.go"] })
      await vfs.fire(vfs.tool, "execute.before", shellEvent())
      expect(vfs.requests("vfs", "shell-prepare")[0]).toMatchObject({ author_key: "author-1", expected_revision: 0, work_unit_id: "unit-a" })
      const created: HookEvent = { command }
      await vfs.fire(vfs.shell, "create.before", created)
      expect(readFileSync(String(created.shell), "utf8")).not.toContain("export HOME")
      await vfs.fire(vfs.tool, "execute.after", { tool: "shell", sessionID: "dispatch", id: "s1" })
      expect(vfs.requests("vfs", "shell-import")[0]).toMatchObject({ author_key: "author-1", expected_revision: 0, work_unit_id: "unit-a" })
      await vfs.run("vfs_write", { path: "src/a.go", content: "x" })
      expect(vfs.requests("vfs", "op")[0]).toMatchObject({ expected_revision: 5 })
    } finally { await vfs.stop() }
  })

  test("a plan that denies, or that omits what it must carry, refuses the command", async () => {
    let reply: Reply = { out: JSON.stringify({ ok: true, shell: { decision: "deny", reason: "network egress" } }) }
    const vfs = await startVfs({ sessions: unitSessions, respond: (call) => call.command === "shell-prepare" ? reply : undefined })
    try {
      await expect(vfs.fire(vfs.tool, "execute.before", shellEvent())).rejects.toThrow("Takt refused this shell command: network egress")
      reply = json({ ok: true })
      await expect(vfs.fire(vfs.tool, "execute.before", shellEvent())).rejects.toThrow("response omitted shell plan")
      reply = json({ ok: true, shell: { decision: "allow", reason: "fine" } })
      await expect(vfs.fire(vfs.tool, "execute.before", shellEvent())).rejects.toThrow("omitted an allowed shell plan field")
    } finally { await vfs.stop() }
  })

  test("an identical command is never shared between callers", async () => {
    const vfs = await startVfs({ sessions: { ...unitSessions, other: { parentID: "root", title: "unit-b" } } })
    try {
      await vfs.fire(vfs.tool, "execute.before", shellEvent())
      await expect(vfs.fire(vfs.tool, "execute.before", shellEvent({ sessionID: "other" }))).rejects.toThrow("an identical command from another session is pending")
      await expect(vfs.fire(vfs.tool, "execute.before", shellEvent({ agent: ORCHESTRATOR }))).rejects.toThrow("an identical specialist command is pending")

      await vfs.fire(vfs.tool, "execute.before", shellEvent({ input: { command: "ls" }, agent: ORCHESTRATOR }))
      await expect(vfs.fire(vfs.tool, "execute.before", shellEvent({ input: { command: "ls" } }))).rejects.toThrow("an identical command from another session is pending")
    } finally { await vfs.stop() }
  })

  test("the orchestrator's direct commands pass once per admission and then are refused", async () => {
    const vfs = await startVfs()
    try {
      const direct = shellEvent({ agent: ORCHESTRATOR, input: { command: "pwd" } })
      await vfs.fire(vfs.tool, "execute.before", direct)
      await vfs.fire(vfs.tool, "execute.before", direct)
      const passes = [{ command: "pwd" }, { command: "pwd" }, { command: "pwd" }]
      await passes.reduce((chain, event) => chain.then(() => vfs.fire(vfs.shell, "create.before", event)), Promise.resolve())
      expect(passes.map(({ command: text }) => text.includes("was not admitted"))).toEqual([false, false, true])
      expect(vfs.actions("vfs")).not.toContain("shell-prepare")
    } finally { await vfs.stop() }
  })
})

describe("delegated VFS context", () => {
  const grants = [
    { action: "*", resource: "*", effect: "deny" },
    { action: "vfs_bind", resource: "*", effect: "allow" },
    { action: "vfs_write", resource: "vfs_write", effect: "allow" },
  ]
  const contextFor = (extra: HookEvent = {}) => ({
    tools: Object.fromEntries([...VFS_TOOL_NAMES, "read"].map((name) => [name, {}])),
    sessionID: "dispatch", agent: "dev", system: [] as Array<{ type: string; text: string }>, ...extra,
  })
  const toolNames = (event: { tools: Record<string, unknown> }) => Object.keys(event.tools)

  test("an exact claim grants only the tools the agent explicitly allows and names its scope", async () => {
    const vfs = await startVfs({ sessions: unitSessions, permissions: { dev: grants }, claims: [claim({ active: true, scope: ["src/a.go", "src/b.go"] })] })
    try {
      const event = contextFor()
      await vfs.fire(vfs.session, "context", event)
      expect(toolNames(event)).toEqual(["vfs_bind", "vfs_write", "read"])
      expect(JSON.stringify(event.system)).not.toContain("vfs_delete")
      expect(JSON.stringify(event.system)).not.toContain("vfs_read")
      expect(event.system).toEqual([{ type: "text", text: expect.stringContaining("Work unit unit-a owns exactly these workspace-relative paths: src/a.go, src/b.go.") }])
    } finally { await vfs.stop() }
  })

  test("a claim naming an author turns the grant into a judging assignment", async () => {
    const vfs = await startVfs({ sessions: unitSessions, permissions: { dev: grants }, claims: [claim({ pending: true, author_key: "author-9" })] })
    try {
      const event = contextFor()
      await vfs.fire(vfs.session, "context", event)
      expect(event.system[0].text).toContain("You judge the staged work of these authors for work unit unit-a; every gate is already bound.")
    } finally { await vfs.stop() }
  })

  // remainingTools is what a delegated session keeps under the given setup.
  const remainingTools = async (options: Options, extra: HookEvent = {}) => {
    const vfs = await startVfs({ sessions: unitSessions, ...options })
    try {
      const event = contextFor(extra)
      await vfs.fire(vfs.session, "context", event)
      return toolNames(event)
    } finally { await vfs.stop() }
  }

  test("without a claim, an explicit allowance or a working lookup, every VFS tool is removed", async () => {
    expect(await remainingTools({ permissions: { dev: grants } })).toEqual(["read"])
    expect(await remainingTools({ claims: [claim({ active: true })] })).toEqual(["read"])
    const brokenClaim = claim({ agent_id: BROKEN_AGENT, target_instance: BROKEN_AGENT, active: true })
    expect(await remainingTools({ claims: [brokenClaim] }, { agent: BROKEN_AGENT })).toEqual(["read"])
  })

  test("root sessions remain untouched and CodeMode contexts still resolve claims", async () => {
    const vfs = await startVfs({ sessions: unitSessions, permissions: { dev: grants } })
    try {
      const root = contextFor({ sessionID: "root" })
      await vfs.fire(vfs.session, "context", root)
      expect(toolNames(root)).toHaveLength(VFS_TOOL_NAMES.length + 1)
      await vfs.fire(vfs.session, "context", { sessionID: "dispatch", agent: "dev", system: [] })
      expect(vfs.actions("vfs")).toEqual(["claims"])
    } finally { await vfs.stop() }
  })
})
