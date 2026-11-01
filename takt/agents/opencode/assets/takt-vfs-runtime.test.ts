import { beforeEach, describe, expect, mock, test } from "bun:test"
import { existsSync, readFileSync } from "node:fs"
import { homedir } from "node:os"
import { dirname } from "node:path"
import type { OpenCodeEvent } from "@opencode/client"
import { Schema } from "effect"
import vfsPlugin from "./takt-vfs"
import { historyFixture, type FixtureExecution } from "../testdata/history-fixture.mjs"

const contracts = JSON.parse(readFileSync(new URL("../testdata/contracts.json", import.meta.url), "utf8"))

// The placeholders the harness templates at deploy time stay literal here, so
// the identities below are the exact strings the plugin compares against.
const ORCHESTRATOR = "__TAKT_ORCHESTRATOR_ID__"
const RESULT_AGENT = "__TAKT_RESULT_AGENTS__"
const VFS_AGENT = "__TAKT_VFS_AGENTS__"
// VFS_AUTHORS is untemplated here, so this placeholder self-matches via .includes().
const VFS_AUTHOR = "__TAKT_VFS_AUTHORS__"
const DELEGATIONS_KEY = "takt/vfs/delegations"
const BINDINGS_KEY = "takt/vfs/bindings"
const IPC_VERSION = 5
// CALLER_FIELDS are the harness-delivered caller facts every VFS request carries.
const CALLER_FIELDS = ["session_id", "caller_session", "caller_agent"]
// REMOVED_FIELDS are the client-supplied identity and state no VFS request carries.
const REMOVED_FIELDS = ["expected_revision", "delta_hash", "verifier_key", "view_key", "agent_id", "specialist", "cycle_id"]
// CLAIM_COMMANDS name an assignment's target with agent_id and specialist.
const CLAIM_COMMANDS = ["assign"]
const BROKEN_AGENT = "broken-agent"
// OVER_HOST_OUTPUT_CAP is a staged file length past any tool-output cap the host applies.
const OVER_HOST_OUTPUT_CAP = 100_000
const WORKSPACE = "/workspace"
const SHELL_NOT_ADMITTED = 122
const SETTLE_ATTEMPTS = 100
const SETTLE_STEP_MS = 2
const QUIET_MS = 25
const VFS_TOOL_NAMES = ["vfs_bind", "vfs_write", "vfs_read", "vfs_delete", "vfs_discard", "vfs_verify", "vfs_consolidate"]

type Reply = { out?: string; err?: string; code?: number }
type Call = { kind: string; command: string; request: Record<string, unknown>; argv: string[]; options: Record<string, unknown> }
type HookEvent = Record<string, unknown>
type Hooks = Map<string, Array<(event: HookEvent) => Promise<void>>>
type Ctx = { sessionID: string; agent: string; id?: string }
type SessionInfo = { parentID?: string; title?: string; metadata?: Record<string, unknown>; time?: { created: number } }
type Options = {
  nativeCalls?: Record<string,"completed"|"error"|"running">
  history?: { inputs?: Record<string,number[]>; executions?: Record<string,Partial<FixtureExecution>>; holders?: Record<string,string> }
  nativeInput?: unknown
  respond?: (call: Call) => Reply | undefined
  claims?: unknown[]
  sessions?: Record<string, SessionInfo>
  storage?: Record<string, unknown>
  permissions?: Record<string, unknown[]>
  promptError?: string
  home?: boolean
  nativeRead?: (input:unknown)=>Promise<unknown>
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

const saved = (records:Record<string,Partial<FixtureExecution>>) => Object.fromEntries(Object.entries(records).map(([dispatch,d])=>[dispatch,{dispatch,attempt:"1",agent:RESULT_AGENT,caller:dispatch.split(":")[0],inputs:[],admitted_at:0,...d}]))
const activeDispatches = (vfs:Awaited<ReturnType<typeof startVfs>>) => Object.values(vfs.history.executions).filter((d)=>!d.settled&&!d.cycle).map((d)=>d.dispatch).sort()
const orchestrator: Ctx = { sessionID: "root", agent: ORCHESTRATOR }
const dev: Ctx = { sessionID: "dispatch", agent: "dev" }
const author: Ctx = { sessionID: "dispatch", agent: VFS_AUTHOR }
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
  if (kind === "gc" && command === "prompt") return json({allowed:true})
  if (kind === "dispatch" || kind === "gc") return json(coordinator())
  if (command === "claims") return json({ ok: true, claims })
  if (command === "bind") return json({ ok: true, key: "author-1", attempt_id: "attempt-1", invariants_version: "inv-1", revision: 0, delta_hash: "d0" })
  if (command === "shell-prepare" || command === "shell-continue") return shellReply()
  return json({ ok: true, revision: 1, delta_hash: "d1", content: "staged content" })
}

// startVfs wires the plugin to a scripted takt-ai, a scripted session tree and
// a controllable event stream; nothing touches the network or the real HOME.
async function startVfs(options: Options = {}) {
  const runtime = globalThis as unknown as { Bun: { spawn: (...args: never[]) => unknown } }
  const initial = structuredClone(options.history ?? {})
  if (options.sessions?.dispatch?.title === "unit-a") {
    initial.executions ??= {}
    initial.executions["existing-dev"] = { dispatch:"existing-dev",root:"root",unit:"unit-a",agent:"dev",attempt:"1",caller:"root",child:"dispatch",inputs:[],admitted_at:0 }
  }
  const history=historyFixture(initial,[VFS_AGENT])
  const nativeCalls = new Map(Object.entries(options.nativeCalls ?? {}))
  const claims=(options.claims??[]) as Array<{pending?:boolean;active?:boolean;scope?:string[];work_unit_id?:string;agent_id?:string;[key:string]:unknown}>
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
  const nativeRead = { input: { type: "object", properties: { filePath: { type: "string" } } }, execute: async (value: unknown) => options.nativeRead ? options.nativeRead(value) : ({ content: "native read", original: value }) }
  const tools = new Map<string, { execute?: (...args: never[]) => unknown }>()
  tools.set("read", nativeRead)
  const toolHooks: Hooks = new Map()
  const permissionHooks: Hooks = new Map()
  const shellHooks: Hooks = new Map()
  const sessionHooks: Hooks = new Map()
  const storage = new Map<string, unknown>(Object.entries(options.storage ?? {}))
  // reads and writes name every storage key the plugin touched, in order.
  const reads: string[] = []
  const writes: string[] = []
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
  runtime.Bun.spawn = ((argv: string[], spawnOptions: Record<string, unknown> = {}) => {
    const kind = argv[1]
    let request: Record<string, unknown> = kind === "dispatch" ? JSON.parse(argv[7]) : kind === "gc" ? JSON.parse(argv[8]) : {}
    let settled: Reply | undefined
    const settle = () => {
      if (!settled) {
        const call = { kind, command: kind === "dispatch" || kind === "gc" ? String(request.action) : argv[2], request, argv, options: spawnOptions }
        calls.push(call)
        let custom=options.respond?.(call)
        if (!custom && kind==="vfs" && call.command==="assign" && !history.inputs.has(`${request.session_id}\0${request.work_unit_id}`))custom={code:1,err:"inputs absent; call dispatch_inputs before claim_assign"}
        if (!custom && kind==="vfs" && call.command==="edit-guard") {
          const held=claims.find(c=>(c.pending||c.active) && (request.resources as string[]).some(path=>(c.scope??[]).includes(path.replace(`${WORKSPACE}/`,"").replace(/^\.\//,""))))
          if(held)custom={code:1,err:`edit refused: ${(held.scope as string[])[0]} is held by work unit ${held.work_unit_id} (agent ${held.agent_id}); wait for its consolidation or use vfs_discard`}
        }
        let current: {cycle?:{sessions:Record<string,string>;plan:{session_id:string;cycle_id:string}}}|null = null
        try { if (kind === "gc" && custom?.code !== 1) current = JSON.parse(custom?.out || "null") } catch { /* A scripted failure has no cycle payload. */ }
        if (kind === "gc" && current?.cycle?.sessions) {
          for (const [role,child] of Object.entries(current.cycle.sessions)) {
            history.executions[`gc:${role}`] ??= {dispatch:`gc:${role}`,root:current.cycle.plan.session_id,unit:current.cycle.plan.cycle_id+(role==="verifier"?"/verify":""),attempt:"1",agent:role==="verifier"?"verify":"simplify",child:String(child),role,cycle:current.cycle.plan.cycle_id,inputs:[],caller:current.cycle.plan.session_id,admitted_at:0}
          }
        }
        if (kind==="gc" && call.command==="prompt") {
          const d=history.activeChild(request.child);if (d?.prompted===request.phase) settled=json({allowed:false});else {if(d)d.prompted=String(request.phase);settled=json({allowed:true})}
        }
        // Successful scripts still apply owner-side effects; failures do not.
        const owned = custom?.code === 1 ? undefined : history.answer(kind,call.command,request,claims)
        settled ??= owned ?? custom ?? defaultReply(call,claims)
      }
      return settled!
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
      storage: { async get(key: string) { reads.push(key); return storage.get(key) }, async set(key: string, value: unknown) { writes.push(key); storage.set(key, value) }, async remove(key: string) { storage.delete(key) } },
      session: {
        context: async ({sessionID}:{sessionID:string}) => { if (options.missing?.includes(sessionID)) throw {_tag:"SessionNotFoundError",message:`session ${sessionID} not found`}; return [{type:"assistant",content:[...nativeCalls].filter(([dispatch])=>dispatch.startsWith(`${sessionID}:`)).map(([dispatch,status])=>({type:"tool",name:"subagent",id:dispatch.slice(sessionID.length+1),state:{status}}))}] },
        async get({ sessionID }: { sessionID: string }) {
          if (options.missing?.includes(sessionID)) throw {_tag:"SessionNotFoundError",message:`session ${sessionID} not found`}
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
        async wait({ sessionID }: { sessionID: string }) { log.waits.push(sessionID); if(options.missing?.includes(sessionID)) throw {_tag:"SessionNotFoundError"}; await options.wait?.(sessionID) },
      },
      agent: { async get({ agentID }: { agentID: string }) {
        if (agentID === BROKEN_AGENT) throw new Error("agent lookup failed")
        return { data: { id: agentID, permissions: options.permissions?.[agentID] ?? [] } }
      } },
      permission: { hook: register(permissionHooks) },
      shell: { hook: register(shellHooks) },
      tool: {
        hook: register(toolHooks),
        async transform(callback: (editor: { list(): Array<{id:string}>; get(name: string): typeof nativeSubagent | undefined; update(name: string, change: (tool: typeof nativeSubagent) => void): void; add(definition: { name: string; execute?: (...args: never[]) => unknown }): void }) => void) {
          await callback({ list() { return [...tools.keys()].map(id=>({id})) }, get(name) { return name === "subagent" ? nativeSubagent : name === "read" ? nativeRead : undefined }, update(name, change) { if (name === "subagent") change(nativeSubagent); if (name === "read") change(nativeRead) }, add(definition) { tools.set(definition.name, definition) } })
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
    calls, log, warnings, errors, storage, reads, writes, nativeSubagent, history,
    callStatus: (dispatch:string,status:"completed"|"error"|"running")=>nativeCalls.set(dispatch,status),
    actions: (kind: string) => calls.filter((call) => call.kind === kind).map((call) => call.command),
    // schema is the input schema a registered tool advertises to the host.
    schema: (name: string) => (tools.get(name) as { input?: { properties?: Record<string, Record<string, unknown>> } } | undefined)?.input,
    requests: (kind: string, command: string) => calls.filter((call) => call.kind === kind && call.command === command).map((call) => call.request),
    run(name: string, input: unknown, ctx: Ctx = dev) {
      const execute = tools.get(name)?.execute
      if (!execute) throw new Error(`missing tool ${name}`)
      return (execute as unknown as (value: unknown, context: Ctx) => Promise<{ content: string; metadata?: Record<string, unknown> }>)(input, ctx)
    },
    // fire runs every hook registered under name in order, as the host does.
    fire: (hooks: Hooks, name: string, event: HookEvent) => {
      if(name==="execute.after" && event.tool==="subagent") nativeCalls.set(`${event.sessionID}:${event.id}`,event.status==="error"?"error":"completed")
      return (hooks.get(name) ?? []).reduce((chain, callback) => chain.then(() => callback(event)), Promise.resolve())
    },
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
      await vfs.run("gc_request", {}, orchestrator) // Core owns GC prerequisites; ordinary coordination remains available.
    } finally { await vfs.stop() }
  })

  test("failed CodeGraph preparation retains the protection hooks and diagnostics", async () => {
    for (const reply of [{ code:1, err:"index locked" }, { code:1, out:"partial output" }, { code:3 }]) {
      const vfs = await startVfs({ respond:call => call.kind === "codegraph" ? reply : undefined })
      try {
        expect(vfs.errors.some(error => String(error[0]).includes("protection hooks remain active"))).toBe(true)
        expect(vfs.tool.get("execute.before")!.length).toBeGreaterThan(0)
        expect(vfs.permission.get("evaluate")!.length).toBeGreaterThan(0)
        await expect(vfs.run("vfs_discard", {author_key:"a"})).rejects.toThrow("belongs to the orchestrator")
      } finally { await vfs.stop() }
    }
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

  test("a coordination command that exits non-zero reports its stderr", async () => {
    const vfs = await startVfs({ respond: (call) => call.kind === "dispatch" && call.command === "commit" ? { code: 1, err: "plan rejected" } : undefined })
    try {
      await expect(vfs.run("dispatch_commit", { version: "v1", plan: [] }, orchestrator)).rejects.toThrow("dispatch commit: plan rejected")
    } finally { await vfs.stop() }
  })
})

describe("stateless VFS caller", () => {
  test("a binding copy an earlier plugin stored is ignored and never read or written", async () => {
    const vfs = await startVfs({ sessions: unitSessions, storage: { [BINDINGS_KEY]: [{ key: "stale", session: "old-root", dispatch: "dispatch", agent: "dev" }] } })
    try {
      expect(vfs.storage.has(BINDINGS_KEY)).toBe(true)
      await vfs.run("vfs_bind", { scope: ["src/a.go"] })
      await vfs.run("vfs_write", { path: "src/a.go", content: "x" })
      expect(vfs.reads).not.toContain(BINDINGS_KEY)
      expect(vfs.writes).not.toContain(BINDINGS_KEY)
    } finally { await vfs.stop() }
  })

  // The continuation of staged work from an earlier root session, through the
  // tools: the fake core answers per the contract and the plugin keeps nothing.
  test("staged work carried over from an earlier root is bound, judged by several gates, consolidated and discarded", async () => {
    const sessions = {
      "dev-child": { parentID: "root", title: "unit-b" },
      "verify-child": { parentID: "root", title: "gate" },
    }
    const devCtx: Ctx = { sessionID: "dev-child", agent: "dev" }
    const reviewer: Ctx = { sessionID: "verify-child", agent: "verify" }
    const verifierGrants = [{ action: "*", resource: "*", effect: "deny" }, ...["vfs_read", "vfs_verify"].map((name) => ({ action: name, resource: name, effect: "allow" }))]
    const claims: Record<string, unknown>[] = [
      { key: "old-key", root_session_id: "root", work_unit_id: "unit-b", agent_id: "dev", target_instance: "dev", pending: true, staged: true, scope: ["src/a.go"] },
      { key: "other-key", root_session_id: "root", work_unit_id: "unit-c", agent_id: "dev", target_instance: "dev", active: true, staged: true, scope: ["src/b.go"] },
    ]
    const vfs = await startVfs({
      sessions, claims, history:{executions:saved({"write-b":{root:"root",unit:"unit-b",agent:"dev",child:"dev-child"}})},permissions: { verify: verifierGrants },
      respond: (call) => {
        if (call.kind !== "vfs") return undefined
        switch (call.command) {
          case "claims": return json({ ok: true, claims })
          case "assign": return json({ ok: true, key: "old-key" })
          case "bind": return json({ ok: true, key: "old-key", attempt_id: "1", invariants_version: "inv-1", revision: 3, delta_hash: "h3" })
          case "adopt-gates": return json({ ok: true, gates: [
            { key: "gate-old-key", author_key: "old-key", scope: ["src/a.go"] },
            { key: "gate-other-key", author_key: "other-key", scope: ["src/b.go"] },
          ] })
          case "op": return json({ ok: true, revision: 4, delta_hash: "h4", ...(call.request.action === "read" ? { content: "staged" } : {}) })
          case "consolidate": return json({ ok: true, seq: 7 })
          default: return json({ ok: true })
        }
      },
    })
    try {
      await vfs.run("dispatch_inputs", { work_unit_id: "unit-b", result_ids: [] }, orchestrator)
      await vfs.run("claim_assign", { work_unit_id: "unit-b", agent: "dev", scope: ["src/a.go"], author_key: "old-key" }, orchestrator)
      expect((await vfs.run("vfs_bind", { scope: ["src/a.go"] }, devCtx)).content).toContain("author_key old-key")
      await vfs.run("vfs_write", { path: "src/a.go", content: "continued" }, devCtx)

      await vfs.run("dispatch_inputs", { work_unit_id: "gate", result_ids: [] }, orchestrator)
      await vfs.fire(vfs.tool, "execute.before", { tool: "subagent", sessionID: "root", id: "review", agent: ORCHESTRATOR,
        input: { agent: "verify", description: "gate", author_keys: ["old-key", "other-key"] } })
      const gate = { tools: { vfs_read: {}, vfs_verify: {} }, ...reviewer, system: [] as Array<{ type: string; text: string }> }
      await vfs.fire(vfs.session, "context", gate)
      expect(gate.system.map(({ text }) => text).join("\n")).toContain("author_key other-key: src/b.go")
      for (const author of ["old-key", "other-key"]) {
        await vfs.run("vfs_read", { path: author === "old-key" ? "src/a.go" : "src/b.go", author_key: author }, reviewer)
        await vfs.run("vfs_verify", { author_key: author, pass: true, finding: "holds" }, reviewer)
      }
      expect((await vfs.run("vfs_consolidate", { author_key: "old-key" }, orchestrator)).content).toBe("Consolidated to workspace")
      expect((await vfs.run("vfs_discard", { author_key: "other-key" }, orchestrator)).content).toBe("Discarded staged delta")

      const caller = (ctx: Ctx, unit?: string) => ({ ipc_version: IPC_VERSION, session_id: "root", caller_session: ctx.sessionID, caller_agent: ctx.agent, ...(unit ? { work_unit_id: unit } : {}) })
      expect(vfs.requests("vfs", "assign")).toEqual([{ ...caller(orchestrator, "root"), work_unit_id: "unit-b", agent_id: "dev", specialist: "dev", scope: ["src/a.go"], author_key: "old-key" }])
      expect(vfs.requests("vfs", "bind")).toEqual([{ ...caller(devCtx, "unit-b"), scope: ["src/a.go"] }])
      expect(vfs.requests("vfs", "adopt-gates")).toEqual([caller(reviewer, "gate")])
      expect(vfs.requests("vfs", "op")).toEqual([
        { ...caller(devCtx), call_id: expect.any(String), action: "create", path: "src/a.go", content: "continued" },
        { ...caller(reviewer), call_id: expect.any(String), action: "read", path: "src/a.go", author_key: "old-key" },
        { ...caller(reviewer), call_id: expect.any(String), action: "read", path: "src/b.go", author_key: "other-key" },
      ])
      expect(vfs.requests("vfs", "verify")).toEqual(["old-key", "other-key"].map(author =>
        ({ ...caller(reviewer), call_id: expect.any(String), author_key: author, pass: true, finding: "holds" })))
      expect(vfs.requests("vfs", "consolidate")).toEqual([{ ...caller(orchestrator), author_key: "old-key" }])
      expect(vfs.requests("vfs", "discard")).toEqual([{ ...caller(orchestrator), author_key: "other-key" }])

      // Every VFS request carries the caller block and none of the removed fields.
      const sent = vfs.calls.filter(call => call.kind === "vfs")
      for (const { command, request } of sent) {
        for (const field of CALLER_FIELDS) expect(request).toHaveProperty(field)
        for (const field of REMOVED_FIELDS) if (!CLAIM_COMMANDS.includes(command) || !["agent_id", "specialist"].includes(field)) expect(request).not.toHaveProperty(field)
      }
      expect(vfs.writes).not.toContain(BINDINGS_KEY)
    } finally { await vfs.stop() }
  })
})

describe("delegation lifecycle", () => {
  const launch = (input: Record<string, unknown>) => ({ tool: "subagent", sessionID: "root", id: "call-1", input })
  const declareNone = (vfs: Awaited<ReturnType<typeof startVfs>>, unit: string) => vfs.run("dispatch_inputs", { work_unit_id: unit, result_ids: [] }, orchestrator)
  // acceptInputs lets the harness validate every declared input and result.
  const acceptInputs = (call: Call) => call.kind === "dispatch" && (call.command === "inputs" || call.command === "validate_results") ? { out: "{}" } : undefined

  test("a delegation is refused until its consumed invariants are declared", async () => {
    const vfs = await startVfs()
    try {
      await expect(vfs.fire(vfs.tool, "execute.before", launch({ description: "unit-a", agent: "dev" }))).rejects.toThrow("dispatch_inputs")
      expect(activeDispatches(vfs)).toEqual([])
    } finally { await vfs.stop() }
  })

  test("a declaration names the existing invariants a unit consumes, an empty list their absence", async () => {
    const vfs = await startVfs({ respond: acceptInputs })
    try {
      await expect(vfs.run("dispatch_inputs", { work_unit_id: "unit-a", result_ids: [] }, { sessionID: "child-r", agent: RESULT_AGENT })).rejects.toThrow("dispatch_inputs belongs to the orchestrator")
      await vfs.run("dispatch_inputs", { work_unit_id: "unit-a", result_ids: [] }, orchestrator)
      await vfs.run("dispatch_inputs", { work_unit_id: "unit-a", result_ids: [7, 8] }, orchestrator)
      expect(vfs.requests("dispatch", "inputs")).toEqual([{ action: "inputs", session: "root",event:"unit-a",result_ids: [] }, { action: "inputs", session: "root",event:"unit-a",result_ids: [7, 8] }])
    } finally { await vfs.stop() }
  })

  test("a declaration naming a missing entry is refused", async () => {
    const vfs = await startVfs({ respond: (call) => call.kind === "dispatch" && call.command === "inputs" ? { code: 1, err: "entry #9 not found" } : undefined })
    try {
      await expect(vfs.run("dispatch_inputs", { work_unit_id: "unit-a", result_ids: [9] }, orchestrator)).rejects.toThrow("entry #9 not found")
      await expect(vfs.fire(vfs.tool, "execute.before", launch({ description: "unit-a", agent: "dev" }))).rejects.toThrow("dispatch_inputs")
    } finally { await vfs.stop() }
  })

  test("a delegation must name its unit and its specialist", async () => {
    const vfs = await startVfs()
    try {
      await expect(vfs.fire(vfs.tool, "execute.before", launch({ agent: "dev" }))).rejects.toThrow("Name the delegation after the work unit")
      await expect(vfs.fire(vfs.tool, "execute.before", launch({ description: "unit-a" }))).rejects.toThrow("requested specialist identity is missing")
      expect(activeDispatches(vfs)).toEqual([])
    } finally { await vfs.stop() }
  })

  test("a staging specialist is refused without its pending claim", async () => {
    const vfs = await startVfs()
    try {
      await declareNone(vfs, "unit-v")
      await expect(vfs.fire(vfs.tool, "execute.before", launch({ description: "unit-v", agent: VFS_AGENT }))).rejects.toThrow("no matching pending clean claim for session root, work unit unit-v")
      expect(activeDispatches(vfs)).toEqual([])
    } finally { await vfs.stop() }
  })

  test("an admitted delegation is remembered durably and launched", async () => {
    const vfs = await startVfs({ claims: [claim({ work_unit_id: "unit-v", agent_id: VFS_AGENT, target_instance: VFS_AGENT, pending: true })] })
    try {
      await declareNone(vfs, "unit-v")
      await vfs.fire(vfs.tool, "execute.before", launch({ description: "unit-v", agent: VFS_AGENT }))
      expect(vfs.requests("dispatch","prepare")).toHaveLength(1)
      expect(vfs.requests("dispatch","launch")).toEqual([])
      expect(vfs.requests("dispatch", "prepare")[0]).toMatchObject({ event: "unit-v", dispatch: "root:call-1", session: "root", agent: VFS_AGENT })
      expect(vfs.history.executions["root:call-1"]).toMatchObject({unit:"unit-v",root:"root",agent:VFS_AGENT,caller:"root",admitted_at:expect.any(Number)})
      // The execution is recorded under the instance that holds a lease.
      const [lease] = vfs.requests("dispatch", "lease")
      expect(lease.owner).toEqual(expect.any(String))
      expect(vfs.history.executions["root:call-1"].owner).toBe(String(lease.owner))
      expect(vfs.writes).toEqual([])
    } finally { await vfs.stop() }
  })

  test("a denied admission is thrown and nothing is remembered", async () => {
    const vfs = await startVfs({ respond: (call) => call.kind === "dispatch" && call.command === "prepare" ? { code: 1, err: "budget exceeded" } : undefined })
    try {
      await declareNone(vfs, "unit-a")
      await expect(vfs.fire(vfs.tool, "execute.before", launch({ description: "unit-a", agent: "dev" }))).rejects.toThrow("dispatch prepare: budget exceeded")
      expect(vfs.requests("dispatch","prepare")).toHaveLength(1)
      expect(vfs.storage.has(DELEGATIONS_KEY)).toBe(false)
    } finally { await vfs.stop() }
  })

  const producerSessions = { "child-r": { parentID: "root", title: "unit-r" } }
  const admitProducer = async (vfs: Awaited<ReturnType<typeof startVfs>>, consumed?: number[]) => {
    await vfs.run("dispatch_inputs", consumed ? { work_unit_id: "unit-r", result_ids: consumed } : { work_unit_id: "unit-r", result_ids: [] }, orchestrator)
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
    const stored = Object.fromEntries(first.history.inputs)
    expect(stored).toEqual({ "root\0unit-r": [7] })
    expect(first.writes).toEqual([])
    const restarted = await startVfs({ sessions: producerSessions, history: { inputs: stored } })
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
      expect(vfs.requests("dispatch", "finish")).toEqual([{ action: "finish", event: "unit-r", dispatch: "root:call-r", attempt:"1", session: "root" }])
      expect(Object.values(vfs.history.executions).filter((d)=>!d.settled && !d.cycle)).toEqual([])
    } finally { await vfs.stop() }
  })

  test("a producer that delivered ends cleanly without a nudge", async () => {
    const vfs = await startVfs({ sessions: producerSessions, respond: acceptInputs })
    try {
      await admitProducer(vfs)
      const delivered = await vfs.run("deliver_result", { result_ids: [4, 5] }, { sessionID: "child-r", agent: RESULT_AGENT })
      expect(delivered.content).toBe("Delivered 2 result id(s)")
      expect(vfs.requests("dispatch", "deliver")[0]).toMatchObject({ child:"child-r",dispatch:"root:call-r",attempt:"1",agent: RESULT_AGENT, result_ids: [4, 5] })
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
      await vfs.run("dispatch_inputs", { work_unit_id: "unit-r", result_ids: [] }, orchestrator)
      await vfs.fire(vfs.tool, "execute.before", { tool: "subagent", sessionID: "root", id: "call-r", input: { description: "unit-r", agent: RESULT_AGENT, background: true } })
      await vfs.fire(vfs.session, "context", { tools: {}, sessionID: "child-r", agent: RESULT_AGENT, system: [] })
      const event = finished()
      await vfs.fire(vfs.tool, "execute.after", event)
      expect(event.result.content).toBe("Breakdown recorded.")
      expect(vfs.log.prompts).toEqual([])
      expect(vfs.requests("dispatch", "finish")).toEqual([])
      vfs.publish({ id: "evt-child", created: 1, type: "session.idle", data: { sessionID: "child-r" } } as OpenCodeEvent)
      await until(() => vfs.requests("dispatch", "finish").length > 0)
      expect(vfs.requests("dispatch", "finish")).toEqual([{ action: "finish", event: "unit-r", dispatch: "root:call-r", attempt:"1", session: "root" }])
      expect(vfs.log.prompts).toEqual([])
      expect(Object.values(vfs.history.executions).filter((d)=>!d.settled && !d.cycle)).toEqual([])
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
      expect(Object.values(vfs.history.executions).filter((d)=>!d.settled && !d.cycle)).toEqual([])
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
      await vfs.run("dispatch_inputs", { work_unit_id: "unit-r", result_ids: [] }, orchestrator)
      await vfs.fire(vfs.tool, "execute.before", { tool: "subagent", sessionID: "root", id: "call-r", input: { description: "unit-r", agent: RESULT_AGENT, background: true } })
      await vfs.fire(vfs.tool, "execute.after", finished())
      vfs.publish({ id: "evt-new", created: 1, type: "session.created", data: { sessionID: "child-r", agent: RESULT_AGENT, parentID: "root", title: "unit-r" } } as unknown as OpenCodeEvent)
      await until(() => vfs.history.executions["root:call-r"]?.child === "child-r")
      vfs.callStatus("root:call-r","error")
      vfs.publish({ id: "evt-gone", created: 2, type: "session.deleted", data: { sessionID: "child-r" } } as OpenCodeEvent)
      await until(() => vfs.requests("dispatch", "finish").length > 0)
      expect(vfs.requests("dispatch", "finish")).toEqual([{ action: "finish", event: "unit-r", dispatch: "root:call-r", attempt:"1", session: "root" }])
      expect(Object.values(vfs.history.executions).filter((d)=>!d.settled && !d.cycle)).toEqual([])
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
      await vfs.run("dispatch_inputs", { work_unit_id: "unit-r", result_ids: [] }, orchestrator)
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
      nativeCalls:{"old:1":"completed","old:2":"error"},
      wait: async (sessionID) => { if (sessionID === "child-r") await turn },
      history: { executions: saved({
        "old:1": { unit: "unit-r", root: "root", agent: RESULT_AGENT, background: true, child: "child-r" },
        "old:2": { unit: "unit-g", root: "root", agent: RESULT_AGENT, background: true, child: "child-gone-r" },
      }) },
    })
    try {
      await vfs.run("dispatch_inputs", { work_unit_id: "unit-n", result_ids: [] }, orchestrator)
      await vfs.fire(vfs.tool, "execute.before", { tool: "subagent", sessionID: "root", id: "call-n", input: { description: "unit-n", agent: RESULT_AGENT } })
      // The child the host no longer has is reconciled; the running one is not.
      expect(vfs.requests("dispatch", "uncertain")).toEqual([])
      await until(()=>!!vfs.history.executions["old:2"].settled)
      expect(activeDispatches(vfs)).toEqual(["old:1","root:call-n"])
      expect(vfs.requests("dispatch","finish")).toEqual([{action:"finish",event:"unit-g",dispatch:"old:2",attempt:"1",session:"root"}])
      release()
      await until(() => !!vfs.history.executions["old:1"].settled)
      expect(vfs.requests("dispatch", "finish")).toContainEqual({ action: "finish", event: "unit-r", dispatch: "old:1",attempt:"1",session: "root" })
      expect(activeDispatches(vfs)).toEqual(["root:call-n"])
    } finally { await vfs.stop() }
  })

  test("a background watch that fails is taken up again by the next reconciliation", async () => {
    const missing: string[] = []
    const vfs = await startVfs({
      sessions: producerSessions, missing,
      wait: async () => { throw new Error("host unreachable") },
      history: { executions: saved({ "old:1": { unit: "unit-r", root: "root", agent: RESULT_AGENT, background: true, child: "child-r" } }) },
    })
    const delegate = async (unit: string) => {
      await vfs.run("dispatch_inputs", { work_unit_id: unit, result_ids: [] }, orchestrator)
      await vfs.fire(vfs.tool, "execute.before", { tool: "subagent", sessionID: "root", id: `call-${unit}`, input: { description: unit, agent: RESULT_AGENT } })
    }
    try {
      await delegate("unit-a")
      await until(() => vfs.errors.some(([label]) => label === "Takt native delegation reconciliation"))
      expect(vfs.requests("dispatch", "uncertain")).toEqual([])
      // By the next pass the host no longer has the child: a background launch
      // whose call left the caller's context can never go idle, so it settles.
      missing.push("child-r")
      await delegate("unit-b")
      await until(()=>!!vfs.history.executions["old:1"].settled)
      expect(vfs.requests("dispatch","finish")).toEqual([{action:"finish",event:"unit-r",dispatch:"old:1",attempt:"1",session:"root"}])
      expect(activeDispatches(vfs)).toEqual(["root:call-unit-a","root:call-unit-b"])
    } finally { await vfs.stop() }
  })

  test("a resumed session runs the unit its new delegation names", async () => {
    const vfs = await startVfs({ sessions: producerSessions, respond: acceptInputs })
    try {
      await admitProducer(vfs)
      await vfs.fire(vfs.tool, "execute.after", { ...finished(), status: "error" })
      await vfs.run("dispatch_inputs", { work_unit_id: "unit-r-fix", result_ids: [] }, orchestrator)
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
      if (call.kind === "dispatch" && call.command === "finish") recorded = structuredClone(holder.vfs?.history.executions)
      return acceptInputs(call)
    } })
    holder.vfs = vfs
    try {
      await admitProducer(vfs)
      await vfs.run("deliver_result", { result_ids: [4] }, { sessionID: "child-r", agent: RESULT_AGENT })
      await vfs.fire(vfs.tool, "execute.after", finished())
      expect(Object.keys(recorded as object)).toEqual(["root:call-r"])
      expect(Object.values(vfs.history.executions).filter((d)=>!d.settled && !d.cycle)).toEqual([])
    } finally { await vfs.stop() }
  })

  test("a deleted background child settles only with its exact terminal call", async () => {
    const missing:string[]=[]
    const vfs = await startVfs({ sessions: producerSessions,missing })
    try {
      await vfs.run("dispatch_inputs", { work_unit_id: "unit-r", result_ids: [] }, orchestrator)
      await vfs.fire(vfs.tool, "execute.before", { tool: "subagent", sessionID: "root", id: "call-r", input: { description: "unit-r", agent: RESULT_AGENT, background: true } })
      await vfs.fire(vfs.session, "context", { tools: {}, sessionID: "child-r", agent: RESULT_AGENT, system: [] })
      await vfs.fire(vfs.tool, "execute.after", finished())
      missing.push("child-r");vfs.callStatus("root:call-r","error")
      vfs.publish({ id: "evt-gone", created: 1, type: "session.deleted", data: { sessionID: "child-r" } } as OpenCodeEvent)
      await until(() => vfs.requests("dispatch", "finish").length > 0)
      expect(vfs.requests("dispatch", "finish")).toEqual([{ action: "finish", event: "unit-r", dispatch: "root:call-r", attempt:"1", session: "root" }])
      expect(Object.values(vfs.history.executions).filter((d)=>!d.settled && !d.cycle)).toEqual([])
    } finally { await vfs.stop() }
  })

  test("a settled inherited delegation is not reconciled again over a later attempt", async () => {
    const vfs = await startVfs({
      nativeCalls:{"old:1":"completed"},
      sessions: producerSessions,
      history: { executions: saved({ "old:1": { unit: "unit-r", root: "root", agent: RESULT_AGENT, background: true, child: "child-r" } }) },
    })
    try {
      vfs.publish({ id: "evt-idle", created: 1, type: "session.idle", data: { sessionID: "child-r" } } as OpenCodeEvent)
      await until(() => vfs.requests("dispatch", "finish").length > 0)
      await vfs.run("dispatch_inputs", { work_unit_id: "unit-r", result_ids: [] }, orchestrator)
      await vfs.fire(vfs.tool, "execute.before", { tool: "subagent", sessionID: "root", id: "call-r", input: { description: "unit-r", agent: RESULT_AGENT } })
      expect(vfs.requests("dispatch", "uncertain")).toEqual([])
      expect(vfs.requests("dispatch", "reconcile")).toEqual([])
      expect(activeDispatches(vfs)).toEqual(["root:call-r"])
    } finally { await vfs.stop() }
  })

  test("a launch the core refuses is not remembered", async () => {
    const vfs = await startVfs({ respond: (call) => call.kind === "dispatch" && call.command === "prepare" ? { code: 1, err: "unit already settled" } : undefined })
    try {
      await vfs.run("dispatch_inputs", { work_unit_id: "unit-r", result_ids: [] }, orchestrator)
      await expect(vfs.fire(vfs.tool, "execute.before", { tool: "subagent", sessionID: "root", id: "call-r", input: { description: "unit-r", agent: RESULT_AGENT } })).rejects.toThrow("unit already settled")
      expect(Object.values(vfs.history.executions).filter((d)=>!d.settled && !d.cycle)).toEqual([])
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
  test("a failed prompt from an earlier cycle cannot interrupt a replacement cycle", async () => {
    let statusCalls=0
    const vfs=await startVfs({promptError:"late prompt failure",respond:call=>{
      if(call.kind==="gc" && call.command==="status") {
        const old=statusCalls++===0
        return json(coordinator(gcCycle("baseline",{verifier:old?"old":"new"},{plan:{...gcPlan,cycle_id:old?"previous":"replacement"}})))
      }
      return undefined
    }})
    try {
      await vfs.run("gc_request",{},orchestrator)
      await until(()=>vfs.errors.some(([label])=>label==="Takt GC"))
      expect(vfs.log.interrupts).toEqual([])
      expect(vfs.actions("gc")).not.toContain("abort")
      expect(vfs.actions("gc")).not.toContain("unprompt")
      expect(vfs.requests("gc","prompt")[0]).toMatchObject({event:"previous"})
    }finally{await vfs.stop()}
  })

  test("a restart retains a live cycle and delegates native reconciliation to the host", async () => {
    const vfs = await startVfs({
      history: { executions: saved({ "old:1": { unit: "u-old", root: "root" } }) },
      wait: async () => new Promise<void>(() => {}),
      respond: (call) => call.kind === "gc" && call.command === "status" ? json(coordinator(gcCycle("verify", { verifier: "gcv" }))) : undefined,
    })
    try {
      await vfs.run("gc_request", {}, orchestrator)
      await until(() => vfs.log.prompts.length > 0)
      expect(vfs.requests("dispatch", "uncertain")).toEqual([])
      expect(activeDispatches(vfs)).toEqual(["old:1"])
      expect(vfs.log.interrupts).toEqual([])
      expect(vfs.actions("gc")).not.toContain("recover")
      expect(vfs.actions("gc")).not.toContain("abort")
      expect(vfs.log.prompts[0].sessionID).toBe("gcv")
      expect(vfs.log.prompts[0].text).toContain("Maintenance cycle cycle-1; mandate complexity.")
      expect(vfs.log.prompts[0].text).toContain("gc_delta")
    } finally { await vfs.stop() }
  })

  test("concurrent startup hooks cannot declare an unobserved execution terminated", async () => {
    const vfs=await startVfs({history:{executions:saved({"old:1":{root:"root",unit:"u-old"}})}})
    try {
      await Promise.all(["call-1","call-2"].map(id=>expect(vfs.fire(vfs.tool,"execute.before",{tool:"subagent",sessionID:"root",id,input:{description:id,agent:RESULT_AGENT}})).rejects.toThrow("dispatch_inputs")))
      expect(activeDispatches(vfs)).toEqual(["old:1"])
      expect(vfs.requests("dispatch","reconcile")).toEqual([])
      expect(vfs.requests("dispatch","finish")).toEqual([])
    } finally {await vfs.stop()}
  })

  test("a native reconciliation failure keeps its dispatch without blocking unrelated work, and is retried", async () => {
    let failing=true
    const vfs=await startVfs({nativeCalls:{"old:1":"completed"},history:{executions:saved({"old:1":{root:"root",unit:"u-old",child:"old-child",background:true}})},wait:async()=>{if(failing)throw new Error("host unavailable")}})
    const launch=async(id:string)=>{await vfs.run("dispatch_inputs",{work_unit_id:id,result_ids:[]},orchestrator);await vfs.fire(vfs.tool,"execute.before",{tool:"subagent",sessionID:"root",id,input:{description:id,agent:RESULT_AGENT}})}
    try {
      await launch("new-1")
      await until(()=>vfs.errors.length>0)
      expect(activeDispatches(vfs)).toEqual(["old:1","root:new-1"])
      failing=false
      await launch("new-2")
      await until(()=>!!vfs.history.executions["old:1"].settled)
      expect(vfs.log.waits.filter(id=>id==="old-child")).toHaveLength(2)
      expect(activeDispatches(vfs)).toEqual(["root:new-1","root:new-2"])
    }finally{await vfs.stop()}
  })

  test("a delegation this process admitted is never reconciled as dead by a later hook", async () => {
    const vfs = await startVfs()
    try {
      for (const unit of ["unit-1", "unit-2"]) {
        await vfs.run("dispatch_inputs", { work_unit_id: unit, result_ids: [] }, orchestrator)
        await vfs.fire(vfs.tool, "execute.before", { tool: "subagent", sessionID: "root", id: `call-${unit}`, input: { description: unit, agent: RESULT_AGENT } })
      }
      expect(vfs.requests("dispatch", "prepare")).toHaveLength(2)
      expect(vfs.requests("dispatch", "uncertain")).toEqual([])
      expect(vfs.requests("dispatch", "reconcile")).toEqual([])
      expect(activeDispatches(vfs)).toHaveLength(2)
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
      // The verifier keeps what an ordinary verification uses: web and memory reads.
      for (const tool of ["webfetch", "websearch", "memory_search", "engram_mem_get_observation"]) await vfs.fire(vfs.tool, "execute.before", { ...outside, tool })

      vfs.publish({ id: "evt-idle", created: 1, type: "session.idle", data: { sessionID: "gcv" } } as OpenCodeEvent)
      await sleep(QUIET_MS)
      expect(vfs.log.prompts).toHaveLength(1)
      expect(vfs.requests("gc", "tick")).toEqual([])
    } finally { await vfs.stop() }
  })

  test("an authorized collector stages as its lane, under the cycle's root session", async () => {
    const collector: Ctx = { sessionID: "gcc", agent: "simplify" }
    const vfs = await startVfs({
      // The lane names the cycle's root in the metadata it was created with.
      sessions: { gcc: { metadata: { takt_gc: "cycle-1", takt_root: "root" } } },
      respond: (call) => {
        if (call.kind !== "gc") return undefined
        if (call.command === "status") return json(coordinator(gcCycle("investigate", { collector: "gcc" })))
        if (call.command === "authorize") return json(gcCycle("investigate", { collector: "gcc" }, { author_key: "author-gc" }))
        return undefined
      },
    })
    try {
      await vfs.run("gc_request", {}, orchestrator)
      await until(() => vfs.log.prompts.length > 0)
      expect(vfs.log.prompts[0].text).toContain("gc_findings")

      await vfs.run("gc_authorize", {}, collector)
      await vfs.run("vfs_write", { path: "a.go", content: "package a" }, collector)
      const [op] = vfs.requests("vfs", "op")
      expect(op).toMatchObject({ session_id: "root", caller_session: "gcc", caller_agent: "simplify", action: "create" })
      expect(op).not.toHaveProperty("author_key")
      expect(vfs.writes).not.toContain(BINDINGS_KEY)
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
      expect(vfs.log.interrupts.filter((id) => id === "gcv")).toEqual(["gcv"])
      expect(vfs.errors.some(([label]) => label === "Takt GC")).toBe(true)
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
  // asked records each confirmation the harness raises; answer decides it.
  let asked: Array<Record<string, unknown>> = []
  let answer: "allow" | "deny" = "allow"
  beforeEach(() => {
    asked = []
    answer = "allow"
    mock.module("@opencode/client/service", () => ({ Service: { discover: async () => ({ url: "http://service.invalid" }), headers: () => ({}) } }))
    mock.module("@opencode/client", () => ({ OpenCode: { make: () => ({ permission: { create: async (r: Record<string, unknown>) => {
      asked.push(r)
      return { id: r.id, effect: answer }
    } } }) } }))
  })
  const envelope = { result: "Standard", additional_context: "done", extra_artifacts: [], memory: [3] }
  const request = { target_agent: "dev", objective: "Ship it", requirements: "typed" }
  const lent = { child: { metadata: { takt_switch: "root" } } }
  const NO_HOLDER = "harness: no interlocutor switch is active for this session; nothing to abort"
  const NOT_HOLDER = "harness: session other is not the current interlocutor holder; abort the holder session instead"
  // core stands in for the execution history: it alone knows who holds the role.
  const core = () => {
    let holder: string | undefined
    return (call: Call): Reply | undefined => {
      if (call.kind !== "dispatch") return undefined
      const body = call.request as { child?: string }
      if (call.command === "switch") { holder = body.child; return { out: "{}" } }
      if (call.command === "handoff") { holder = undefined; return json(envelope) }
      if (call.command === "session_deleted") {
        if (!holder || body.child !== holder) return json({ aborted: false })
        holder = undefined
        return json({ ...envelope, result: "Aborted", aborted: true, root: "root", child: body.child })
      }
      if (call.command !== "abort_switch") return undefined
      const target = body.child || holder
      if (!holder) return { code: 1, err: NO_HOLDER }
      if (target !== holder) return { code: 1, err: NOT_HOLDER }
      holder = undefined
      return json({ ...envelope, result: "Aborted", child: target })
    }
  }

  test("only the root orchestrator switches or aborts", async () => {
    const vfs = await startVfs({ sessions: { leaf: { parentID: "root" } } })
    try {
      const leaf: Ctx = { sessionID: "leaf", agent: ORCHESTRATOR }
      await expect(vfs.run("dispatch_switch", request, leaf)).rejects.toThrow("dispatch_switch belongs to the root orchestrator")
      await expect(vfs.run("dispatch_abort_switch", { reason: "r" }, leaf)).rejects.toThrow("dispatch_abort_switch belongs to the root orchestrator")
    } finally { await vfs.stop() }
  })

  test("an abort with no switch is refused with the core's text", async () => {
    const vfs = await startVfs({ respond: core() })
    try {
      await expect(vfs.run("dispatch_abort_switch", { reason: "r" }, orchestrator)).rejects.toThrow(NO_HOLDER)
      expect(vfs.log.interrupts).toEqual([])
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

  test("a failed child prompt aborts that child so the holder is cleared", async () => {
    const vfs = await startVfs({ promptError: "prompt refused", respond: core() })
    try {
      await expect(vfs.run("dispatch_switch", request, orchestrator)).rejects.toThrow("prompt refused")
      expect(vfs.requests("dispatch", "abort_switch")).toEqual([{ action: "abort_switch", session: "root", child: "child", evidence: "the switched session could not be prompted", origin: "harness" }])
      expect(vfs.log.interrupts).toEqual(["child"])
      await expect(vfs.run("dispatch_abort_switch", { reason: "again" }, orchestrator)).rejects.toThrow(NO_HOLDER)
    } finally { await vfs.stop() }
  })

  test("a user abort names no child, ends the lent session the core reports and clears the switch", async () => {
    const vfs = await startVfs({ respond: core() })
    try {
      await vfs.run("dispatch_switch", request, orchestrator)
      expect(vfs.log.prompts[0]).toMatchObject({ sessionID: "child" })
      expect(vfs.log.prompts[0].text).toBe("You are taking over as the active interlocutor for this workspace.\nObjective: Ship it\nRequirements: typed")
      const result = await vfs.run("dispatch_abort_switch", { reason: "changed course" }, orchestrator)
      expect(JSON.parse(result.content)).toMatchObject({ result: "Aborted", child: "child" })
      expect(vfs.requests("dispatch", "abort_switch")).toEqual([{ action: "abort_switch", session: "root", child: "", evidence: "changed course", origin: "user" }])
      expect(vfs.log.interrupts).toEqual(["child"])
      await expect(vfs.run("dispatch_abort_switch", { reason: "again" }, orchestrator)).rejects.toThrow(NO_HOLDER)
    } finally { await vfs.stop() }
  })

  test("a switch, abort or handoff the user refuses changes nothing", async () => {
    const vfs = await startVfs({ sessions: lent, respond: core() })
    const caller = { ...orchestrator, messageID: "message", id: "tool-call" }
    try {
      answer = "deny"
      await expect(vfs.run("dispatch_switch", request, caller)).rejects.toThrow("Permission denied: interlocutor_switch")
      expect(asked[0]).toMatchObject({ sessionID: "root", action: "interlocutor_switch", resources: ["dev"], metadata: { objective: "Ship it" } })
      expect(vfs.log.created).toEqual([])
      expect(vfs.requests("dispatch", "switch")).toEqual([])
      answer = "allow"
      await vfs.run("dispatch_switch", request, caller)
      answer = "deny"
      await expect(vfs.run("dispatch_abort_switch", { reason: "model decided" }, caller)).rejects.toThrow("Permission denied: interlocutor_abort")
      expect(vfs.requests("dispatch", "abort_switch")).toEqual([])
      const holder = { sessionID: "child", agent: "dev", messageID: "m", id: "t" }
      await expect(vfs.run("dispatch_handoff", { result: "Standard", additional_context: "done", extra_artifacts: [], result_ids: [3] }, holder)).rejects.toThrow("Permission denied: interlocutor_handoff")
      expect(vfs.requests("dispatch", "handoff")).toEqual([])
      expect(vfs.log.interrupts).toEqual([])
    } finally { await vfs.stop() }
  })

  test("a plugin restarted mid-switch can still abort it", async () => {
    const respond = core()
    const first = await startVfs({ respond })
    try { await first.run("dispatch_switch", request, orchestrator) } finally { await first.stop() }
    const restarted = await startVfs({ respond })
    try {
      await restarted.run("dispatch_abort_switch", { reason: "after restart" }, orchestrator)
      expect(restarted.requests("dispatch", "abort_switch")).toEqual([{ action: "abort_switch", session: "root", child: "", evidence: "after restart", origin: "user" }])
      expect(restarted.log.interrupts).toEqual(["child"])
    } finally { await restarted.stop() }
  })

  test("a handoff returns the envelope to the root and ends the holder's turn", async () => {
    const vfs = await startVfs({ sessions: lent, respond: core() })
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
      await expect(vfs.run("dispatch_abort_switch", { reason: "late" }, orchestrator)).rejects.toThrow(NO_HOLDER)
    } finally { await vfs.stop() }
  })

  test("a lent session that dies is reported as aborted to the root", async () => {
    const vfs = await startVfs({ sessions: lent, respond: core() })
    try {
      await vfs.run("dispatch_switch", request, orchestrator)
      vfs.publish({ id: "evt-dead", created: 2, type: "session.deleted", data: { sessionID: "child" } } as OpenCodeEvent)
      await until(() => vfs.log.synthetics.length > 0)
      expect(vfs.requests("dispatch", "session_deleted")).toEqual([{ action: "session_deleted", child: "child" }])
      expect(vfs.log.synthetics[0].text).toContain("a lent session that ended without a handoff")
    } finally { await vfs.stop() }
  })

  test("a deleted session that holds nothing is reported to the core and logs nothing", async () => {
    const vfs = await startVfs({ sessions: { other: { parentID: "root" } }, respond: core() })
    try {
      await vfs.run("dispatch_switch", request, orchestrator)
      vfs.publish({ id: "evt-other", created: 1, type: "session.deleted", data: { sessionID: "other" } } as OpenCodeEvent)
      await until(() => vfs.requests("dispatch", "session_deleted").length > 0)
      expect(vfs.requests("dispatch", "session_deleted")).toEqual([{ action: "session_deleted", child: "other" }])
      await new Promise((resolve) => setTimeout(resolve, 20))
      expect(vfs.errors).toEqual([])
      expect(vfs.log.synthetics).toEqual([])
      await vfs.run("dispatch_abort_switch", { reason: "still active" }, orchestrator)
    } finally { await vfs.stop() }
  })

  test("a restarted plugin that cannot resolve the deleted session's root still reports it", async () => {
    const vfs = await startVfs({ sessions: {}, respond: core() })
    try {
      vfs.publish({ id: "evt-gone", created: 1, type: "session.deleted", data: { sessionID: "unknown" } } as OpenCodeEvent)
      await until(() => vfs.requests("dispatch", "session_deleted").length > 0)
      expect(vfs.requests("dispatch", "session_deleted")).toEqual([{ action: "session_deleted", child: "unknown" }])
      expect(vfs.errors).toEqual([])
    } finally { await vfs.stop() }
  })

  test("a failed abort report is logged, not thrown", async () => {
    const vfs = await startVfs({ sessions: lent, respond: (call) => call.kind === "dispatch" && call.command === "session_deleted" ? { code: 1, err: "core down" } : call.kind === "dispatch" && call.command === "switch" ? { out: "{}" } : undefined })
    try {
      await vfs.run("dispatch_switch", request, orchestrator)
      vfs.publish({ id: "evt-dead", created: 1, type: "session.deleted", data: { sessionID: "child" } } as OpenCodeEvent)
      await until(() => vfs.errors.length > 0)
      expect(vfs.errors[0][0]).toBe("Takt session_deleted")
      expect(vfs.log.synthetics).toEqual([])
    } finally { await vfs.stop() }
  })
})

describe("path ownership", () => {
  const edit = (resources: string[]): HookEvent => ({ action: "edit", effect: "allow", resources, sessionID: "dispatch", agent: "dev" })

  test("an unreadable ownership store denies the edit", async () => {
    const vfs = await startVfs({ sessions: unitSessions, respond: (call) => call.command === "edit-guard" ? { code: 1, err: "store offline" } : undefined })
    try {
      const event = edit([`${WORKSPACE}/src/a.ts`])
      await vfs.fire(vfs.permission, "evaluate", event)
      expect(event.effect).toBe("deny")
      expect(event.message).toContain("store offline")
      expect(event.message).toContain("retry the edit")
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
      expect(relative.message).toContain("src/held.ts is held by work unit unit-b (agent dev)")
      expect(relative.message).toContain("vfs_discard")

      const free = edit([`${WORKSPACE}/src/free.ts`])
      await vfs.fire(vfs.permission, "evaluate", free)
      expect(free.effect).toBe("allow")

      const blank = edit([""])
      await vfs.fire(vfs.permission, "evaluate", blank)
      expect(blank.effect).toBe("allow")
      expect(vfs.actions("vfs").filter((command) => command === "edit-guard")).toHaveLength(2)
    } finally { await vfs.stop() }
  })
})

describe("claims and consolidation", () => {
  test("releasing a claim leaves every decision to the core", async () => {
    let ended = false
    const vfs = await startVfs({
      respond: (call) => call.command === "release" && !ended ? json({ ok: false, error: "unit still running" }) : undefined,
    })
    try {
      await expect(vfs.run("claim_release", { claim_key: "k-active" }, orchestrator)).rejects.toThrow()
      ended = true
      await vfs.run("claim_release", { claim_key: "k-active" }, orchestrator)
      await expect(vfs.run("claim_release", { claim_key: "k-active" }, dev)).rejects.toThrow("belongs to the orchestrator")
      expect(vfs.actions("vfs")).toEqual(["release", "release"])
      expect(vfs.requests("vfs", "release")).toEqual([1, 2].map(() => ({
        ipc_version: IPC_VERSION, session_id: "root", caller_session: "root", caller_agent: ORCHESTRATOR, key: "k-active",
      })))
    } finally { await vfs.stop() }
  })

  test("a limit exception is recorded only once the user accepts it", async () => {
    const created: Array<Record<string, unknown>> = []
    mock.module("@opencode/client/service", () => ({ Service: { discover: async () => ({ url: "http://service.invalid" }), headers: () => ({}) } }))
    mock.module("@opencode/client", () => ({
      OpenCode: { make: () => ({ permission: { create: async (request: Record<string, unknown>) => {
        created.push(request)
        return { effect: created.length === 1 ? "allow" : "deny", id: request.id }
      } } }) },
    }))
    const vfs = await startVfs({ sessions: unitSessions })
    const caller = { ...orchestrator, messageID: "message", id: "tool-call" }
    const exception = { bound: "budget/unplanned-units", allowance: 1, evidence: "four units are pending" }
    try {
      await vfs.run("dispatch_exception", exception, caller)
      expect(created[0]).toMatchObject({ sessionID: "root", action: "dispatch_exception", metadata: { bound: "budget/unplanned-units", allowance: "1", evidence: "four units are pending" } })
      expect(vfs.requests("dispatch", "exception")).toEqual([expect.objectContaining({ action: "exception", bound: "budget/unplanned-units", allowance: 1 })])
      await expect(vfs.run("dispatch_exception", exception, caller)).rejects.toThrow("Permission denied: dispatch_exception")
      expect(vfs.requests("dispatch", "exception")).toHaveLength(1)
      await expect(vfs.run("dispatch_exception", exception, { sessionID: "dispatch", agent: ORCHESTRATOR })).rejects.toThrow("dispatch_exception belongs to the root orchestrator")
    } finally { await vfs.stop() }
  })

  test("claim_assign with an author key asks to keep the staged work", async () => {
    const vfs = await startVfs()
    try {
      await vfs.run("dispatch_inputs", { work_unit_id: "unit-a", result_ids: [] }, orchestrator)
      await vfs.run("claim_assign", { work_unit_id: "unit-a", agent: "dev", scope: ["a.go", "b.go"], author_key: "author-key" }, orchestrator)
      expect(vfs.requests("vfs", "assign")).toEqual([expect.objectContaining({ author_key: "author-key", scope: ["a.go", "b.go"] })])
    } finally { await vfs.stop() }
  })

  test("a claim is assigned with the invariants declared for its unit, and not before they are", async () => {
    const vfs = await startVfs({ respond: (call) => call.kind === "dispatch" && call.command === "inputs" ? { out: "{}" } : undefined })
    try {
      const assign = () => vfs.run("claim_assign", { work_unit_id: "unit-a", agent: "dev", scope: ["a.go"] }, orchestrator)
      await expect(assign()).rejects.toThrow("dispatch_inputs")
      expect(vfs.requests("vfs", "assign")).toHaveLength(1)
      await vfs.run("dispatch_inputs", { work_unit_id: "unit-a", result_ids: [7, 8] }, orchestrator)
      await assign()
      expect(vfs.requests("vfs", "assign")[1]).not.toHaveProperty("invariants")
      expect(vfs.history.inputs.get("root\0unit-a")).toEqual([7,8])
    } finally { await vfs.stop() }
  })

  const verifierGrants = [
    { action: "*", resource: "*", effect: "deny" },
    ...["vfs_bind", "vfs_read", "vfs_verify"].map((name) => ({ action: name, resource: name, effect: "allow" })),
  ]
  const gateContext = () => ({ tools: Object.fromEntries(["vfs_bind", "vfs_read", "vfs_verify"].map((name) => [name, {}])), sessionID: "verify-child", agent: "verify", system: [] as Array<{ type: string; text: string }> })

  test("a verifier gate arrives bound through one adoption, with each author's files and any adoption error listed", async () => {
    const vfs = await startVfs({
      sessions: { "verify-child": { parentID: "root", title: "gate" } },
      permissions: { verify: verifierGrants },
      history:{executions:saved({"gate-run":{root:"root",unit:"gate",agent:"verify",child:"verify-child"}})},
      claims: [{ key: "gate-key", root_session_id: "root", work_unit_id: "gate", agent_id: "verify", target_instance: "verify", pending: true, scope: [], author_key: "author-key" }],
      respond: (call) => call.command === "adopt-gates" ? json({ ok: true, gates: [
        { key: "gate-key", author_key: "author-key", scope: ["src/a.go", "src/b.go"] },
        { key: "gate-other", author_key: "author-other", scope: ["src/c.go"], error: "author-other was superseded" },
      ] }) : undefined,
    })
    try {
      const event = gateContext()
      await vfs.fire(vfs.session, "context", event)
      expect(vfs.requests("vfs", "adopt-gates")).toEqual([{ ipc_version: IPC_VERSION, session_id: "root", caller_session: "verify-child", caller_agent: "verify", work_unit_id: "gate" }])
      expect(vfs.actions("vfs")).not.toContain("bind")
      expect(Object.keys(event.tools)).toEqual(["vfs_read", "vfs_verify"])
      expect(event.system.map(part=>part.text).join("\n")).toContain("author_key author-key: src/a.go, src/b.go")
      expect(event.system.map(part=>part.text).join("\n")).toContain("author_key author-other: src/c.go")
      expect(event.system.map(part=>part.text).join("\n")).toContain("author-other was superseded")
    } finally { await vfs.stop() }
  })

  test("a gate's read names the files the core reports as read under an earlier author state", async () => {
    const reviewer: Ctx = { sessionID: "verify-child", agent: "verify" }
    let outdated: string[] | undefined
    const vfs = await startVfs({
      sessions: { "verify-child": { parentID: "root", title: "gate" } },
      respond: (call) => call.command === "op" ? json({ ok: true, revision: 2, delta_hash: "h2", content: `content of ${call.request.path}`, ...(outdated ? { outdated_paths: outdated } : {}) }) : undefined,
    })
    try {
      expect((await vfs.run("vfs_read", { path: "a.go" }, reviewer)).content).toBe("content of a.go")
      outdated = ["b.go", "c.go"]
      const moved = (await vfs.run("vfs_read", { path: "d.go", author_key: "author-key" }, reviewer)).content
      expect(moved).toContain("b.go, c.go")
      expect(moved).toEndWith("content of d.go")
      expect(vfs.requests("vfs", "op")).toEqual([
        expect.objectContaining({ action: "read", path: "a.go" }),
        expect.objectContaining({ action: "read", path: "d.go", author_key: "author-key" }),
      ])
      expect(vfs.requests("vfs", "op")[0]).not.toHaveProperty("author_key")
    } finally { await vfs.stop() }
  })

  test("a staged read is handed over whole, outside the host's tool-output cap", async () => {
    const reviewer: Ctx = { sessionID: "verify-child", agent: "verify" }
    const big = "x".repeat(OVER_HOST_OUTPUT_CAP)
    const vfs = await startVfs({
      sessions: { "verify-child": { parentID: "root", title: "gate" } },
      respond: (call) => call.command === "op" ? json({ ok: true, revision: 1, delta_hash: "h1", content: big }) : undefined,
    })
    try {
      const read = await vfs.run("vfs_read", { path: "big.js" }, reviewer)
      expect(read.content).toBe(big)
      expect(read.metadata).toEqual({ truncated: false })
    } finally { await vfs.stop() }
  })

  describe("vfs_inspect", () => {
    const analyst: Ctx = { sessionID: "analyst-child", agent: "analyst" }
    const sessions = { "analyst-child": { parentID: "root", title: "inspect" } }

    test("forwards its filters and page to the offline CLI without stdin", async () => {
      const vfs = await startVfs({ sessions, respond: (call) => call.kind === "vfs" && call.command === "history" ? { out: "page" } : undefined })
      try {
        const page = await vfs.run("vfs_inspect", { source: "history", session: "root", unit: "unit-a", after: 3, limit: 10 }, analyst)
        expect(page.content).toBe("page")
        const [call] = vfs.calls.filter((c) => c.kind === "vfs" && c.command === "history")
        expect(call.argv.slice(3)).toEqual(["--workspace", WORKSPACE, "--state", expect.any(String), "--session", "root", "--unit", "unit-a", "--after", "3", "--limit", "10"])
        expect(call.options).toMatchObject({ stdin: "ignore" })
      } finally { await vfs.stop() }
    })

    test("refuses a source that is neither journal nor history", async () => {
      const vfs = await startVfs({ sessions })
      try {
        await expect(vfs.run("vfs_inspect", { source: "events" }, analyst)).rejects.toThrow("source must be journal or history")
      } finally { await vfs.stop() }
    })

    test("relays the CLI refusal with its exit code", async () => {
      const vfs = await startVfs({ sessions, respond: (call) => call.kind === "vfs" && call.command === "journal" ? { code: 2, err: "vfs: --limit between 1 and 500" } : undefined })
      try {
        await expect(vfs.run("vfs_inspect", { source: "journal", limit: 0 }, analyst)).rejects.toThrow("takt-ai vfs journal exited 2: vfs: --limit between 1 and 500")
      } finally { await vfs.stop() }
    })

    test("advertises the page bound the core serves", async () => {
      const vfs = await startVfs({ sessions })
      try {
        expect(vfs.schema("vfs_inspect")?.properties?.limit).toMatchObject({ minimum: 1, maximum: contracts.inspect_page_limit })
      } finally { await vfs.stop() }
    })
  })

  test("a verdict the core refuses reaches the verifier unchanged", async () => {
    const reviewer: Ctx = { sessionID: "verify-child", agent: "verify" }
    const refusal = "read the author's files again with vfs_read and attach the verdict again"
    const vfs = await startVfs({
      sessions: { "verify-child": { parentID: "root", title: "gate" } },
      respond: (call) => call.command === "verify" ? json({ ok: false, error: refusal }) : undefined,
    })
    try {
      await expect(vfs.run("vfs_verify", { author_key: "author-key", pass: true, finding: "holds" }, reviewer)).rejects.toThrow(new RegExp(`^${refusal}$`))
      expect(vfs.requests("vfs", "verify")).toEqual([{ ipc_version: IPC_VERSION, session_id: "root", caller_session: "verify-child", caller_agent: "verify",
        call_id: expect.any(String), author_key: "author-key", pass: true, finding: "holds" }])
    } finally { await vfs.stop() }
  })

  for (const count of [1, 2]) test(`one delegation prepares ${count} authors and its retry in CodeMode`, async () => {
    const authors = Array.from({ length: count }, (_, index) => `author-${index}`)
    const claims: Record<string, unknown>[] = authors.map(author => ({
      key: author, root_session_id: "root", work_unit_id: `writer-${author}`,
      agent_id: "dev", target_instance: "dev", active: true, scope: [`${author}.go`],
    }))
    let generation = 0
    const reviewer = { sessionID: "verify-child", agent: "verify" }
    const vfs = await startVfs({
      sessions: { "verify-child": { parentID: "root", title: "gate" } },
      permissions: { verify: verifierGrants }, claims,
      respond: call => {
        if (call.kind !== "vfs") return undefined
        if (call.command === "adopt-gates") {
          return json({ ok: true, gates: authors.map(author => ({ key: `gate-${generation}-${author}`, author_key: author, scope: [`${author}.go`] })) })
        }
        return undefined
      },
    })
    try {
      expect(() => vfs.run("claim_assign_verifier", {}, orchestrator)).toThrow("missing tool")
      expect(vfs.nativeSubagent.input).toHaveProperty("properties.author_keys")
      await vfs.run("dispatch_inputs", { work_unit_id: "gate", result_ids: [] }, orchestrator)
      for (generation = 1; generation <= 2; generation++) {
        const id = `review-${generation}`
        await vfs.fire(vfs.tool, "execute.before", { tool: "subagent", sessionID: "root", id,
          input: { agent: "verify", description: "gate", author_keys: authors,...(generation===2?{sessionID:"verify-child"}:{}) } })
        const event = { ...reviewer, system: [] as Array<{ text: string }> } // real CodeMode shape
        await vfs.fire(vfs.session, "context", event)
        for (const author of authors) expect(event.system.map(part => part.text).join("\n")).toContain(`author_key ${author}: ${author}.go`)
        for (const author of authors) {
          await vfs.run("vfs_read", { path: `${author}.go`, author_key: author }, reviewer)
          await vfs.run("vfs_verify", { author_key: author, pass: true, finding: "reviewed" }, reviewer)
        }
        await vfs.fire(vfs.tool, "execute.after", { tool: "subagent", sessionID: "root", id, status: "completed", result: {} })
      }
      expect(vfs.requests("vfs","assign-verifier")).toEqual([])
      expect(vfs.requests("dispatch","prepare")).toEqual([1,2].map(generation=>expect.objectContaining({dispatch:`root:review-${generation}`,event:"gate",author_keys:authors})))
      expect(vfs.requests("vfs", "adopt-gates")).toHaveLength(2)
      expect(vfs.requests("vfs", "verify")).toHaveLength(count * 2)
      const ids = [...vfs.requests("vfs", "op"), ...vfs.requests("vfs", "verify")].map(request => request.call_id)
      expect(ids.every(id => typeof id === "string" && id.length > 0)).toBe(true)
      expect(new Set(ids).size).toBe(ids.length)
      const governed = vfs.calls.filter(call => (call.kind === "dispatch" && call.command === "prepare") || call.command === "assign-verifier")
      expect(governed[0].command).toBe("prepare")
    } finally { await vfs.stop() }
  })

  test.each([[[]], [["a", "a"]], [["a", " "]], ["a"]])("author_keys %j is refused before anything is admitted", async (authorKeys) => {
    const vfs = await startVfs({ permissions: { verify: verifierGrants } })
    try {
      await vfs.run("dispatch_inputs", { work_unit_id: "gate", result_ids: [] }, orchestrator)
      await expect(vfs.fire(vfs.tool, "execute.before", { tool: "subagent", sessionID: "root", id: "bad",
        input: { agent: "verify", description: "gate", author_keys: authorKeys } })).rejects.toThrow("unique, nonempty staged author keys")
      expect(activeDispatches(vfs)).toEqual([])
    } finally { await vfs.stop() }
  })

  describe("a failed verifier assignment", () => {
    const assignment = async (failing: { release?: boolean, finish?: boolean }) => {
      const vfs = await startVfs({
        permissions: { verify: verifierGrants },
        respond:call=>call.kind==="dispatch"&&call.command==="prepare"?{code:1,err:`author b is gone; prepared gates released; attempt closed;${failing.release?" release failed;":""}${failing.finish?" finish failed;":""} retry the same subagent call`}:undefined,
      })
      return vfs
    }
    const delegate = (vfs: Awaited<ReturnType<typeof startVfs>>) => vfs.run("dispatch_inputs", { work_unit_id: "gate", result_ids: [] }, orchestrator)
      .then(() => vfs.fire(vfs.tool, "execute.before", { tool: "subagent", sessionID: "root", id: "call",
        input: { agent: "verify", description: "gate", author_keys: ["a", "b", "c"] } }))

    test("releases the gates already prepared and finishes the dispatch", async () => {
      const vfs = await assignment({})
      try {
        await expect(delegate(vfs)).rejects.toThrow("author b is gone")
        expect(vfs.requests("vfs", "release")).toEqual([])
        expect(vfs.requests("dispatch","prepare")).toHaveLength(1)
        expect(activeDispatches(vfs)).toEqual([])
        expect(vfs.storage.has(DELEGATIONS_KEY)).toBe(false)
        expect(vfs.errors).toEqual([])
      } finally { await vfs.stop() }
    })

    test("keeps its own error when the cleanup fails too, and still attempts every step", async () => {
      const vfs = await assignment({ release: true, finish: true })
      try {
        await expect(delegate(vfs)).rejects.toThrow("author b is gone")
        expect(vfs.requests("vfs", "release")).toEqual([])
        expect(vfs.requests("dispatch","prepare")).toHaveLength(1)
        expect(activeDispatches(vfs)).toEqual([])
        expect(vfs.requests("dispatch","finish")).toEqual([])
      } finally { await vfs.stop() }
    })
  })

  test("a moved base explains the ordinary continuation without granting an override", async () => {
    let failure = "physical base changed on src/a.go"
    const vfs = await startVfs({
      sessions: unitSessions,
      respond: (call) => call.command === "consolidate" ? json({ ok: false, error: failure }) : undefined,
    })
    try {
      await vfs.run("vfs_bind", { scope: ["src/a.go"] })
      await expect(vfs.run("vfs_consolidate", { author_key: "author-1", checkpoint: "cp" }, orchestrator))
        .rejects.toThrow("physical base changed on src/a.go; inspect the named author and files; use claim_assign with author_key")
      failure = "disk full"
      await expect(vfs.run("vfs_consolidate", { author_key: "author-1", checkpoint: "cp" }, orchestrator)).rejects.toThrow(/^disk full$/)
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
          return { effect: effects.shift() ?? "allow", id: request.id }
        } } }) },
      }))
      return created
    }
    const start = (refuse: boolean) => startVfs({
      sessions: unitSessions,
      respond: (call) => call.command === "consolidate"
        ? (refuse && call.request.accept_failing !== true ? json({ ok: false, error: failing, revision: 4, delta_hash: "h4" }) : json({ ok: true, seq: 1 }))
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
        vfs.publish({ id: "evt-reply", created: 1, type: "permission.replied", data: { requestID: String(created[0].id), reply: "once" } } as OpenCodeEvent)
        expect((await done).content).toBe("Consolidated to workspace")
        // The acceptance echoes the staged state the refusal named.
        expect(vfs.requests("vfs", "consolidate")).toEqual([
          expect.not.objectContaining({ accept_failing: expect.anything() }),
          expect.objectContaining({ author_key: "author-1", accept_failing: true, accepted_revision: 4, accepted_hash: "h4" }),
        ])
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
  const toolSource = (id: string) => ({ type: "tool", messageID: "m1", id })
  const importOk = (call: { command: string }) => call.command === "shell-prepare" ? shellReply({ capture: true }) : call.command === "shell-complete" ? json({ ok: true, revision: 5, delta_hash: "d5" }) : undefined
  // admit runs execute.before and returns the command the host will execute: the
  // call's text carrying its token.
  const admit = async (vfs: Awaited<ReturnType<typeof startVfs>>, event: HookEvent) => {
    await vfs.fire(vfs.tool, "execute.before", event)
    return String((event.input as { command: string }).command)
  }
  const sandboxed = () => mock.module("./takt-sandbox.mjs", () => ({
    wrap: (line: string, options: { callID: string }) => Promise.resolve(`wrapped:${line}|${options.callID}`),
    supervise: (wrapped: string, confirm: string) => `supervised(${wrapped}|${confirm})`,
  }))

  test("an unbound specialist is admitted as an inspector and runs through the sandbox wrapper", async () => {
    mock.module("./takt-sandbox.mjs", () => ({
      wrap: (line: string) => Promise.resolve(`wrapped:${line}`),
      supervise: (wrapped: string, confirm: string) => `supervised(${wrapped}|${confirm})`,
    }))
    const vfs = await startVfs({ sessions: unitSessions, respond: (call) => (call.command === "shell-prepare" || call.command==="shell-continue") ? shellReply({ cwd: "/elsewhere" }) : undefined })
    try {
      const marked = await admit(vfs, shellEvent())
      expect(marked.startsWith(`${command}\n#takt-call:`)).toBe(true)
      expect(vfs.requests("vfs", "shell-prepare")).toEqual([{ ipc_version: IPC_VERSION, session_id: "root", caller_session: "dispatch", caller_agent: "dev", caller_dispatch: "existing-dev", caller_attempt: "1", call_id: expect.any(String), command, tool_call_id: "s1" }])
      const created: HookEvent = { command: marked }
      await vfs.fire(vfs.shell, "create.before", created)
      expect(created.command).toBe(command)
      const wrapper = readFileSync(String(created.shell), "utf8")
      expect(wrapper).toContain(`cd '/elsewhere' || exit ${SHELL_NOT_ADMITTED}`)
      expect(wrapper).toContain("export TMPDIR='/private/tmp/scratch'")
      expect(wrapper).toContain("export HOME='/private/tmp/scratch'")
      expect(wrapper).toContain(wrapperCommand)

      const replay: HookEvent = { command: marked }
      await vfs.fire(vfs.shell, "create.before", replay)
      expect(String(replay.command)).toContain("this shell command was not admitted")
      expect(replay.shell).toBeUndefined()

      const approval: HookEvent = { action: "shell", effect: "allow", sessionID: "dispatch", agent: "dev", source: toolSource("s1") }
      await vfs.fire(vfs.permission, "evaluate", approval)
      expect([approval.effect, approval.message]).toEqual(["ask", "review each run"])
      const repeat: HookEvent = { action: "shell", effect: "allow", sessionID: "dispatch", agent: "dev", source: toolSource("s1") }
      await vfs.fire(vfs.permission, "evaluate", repeat)
      expect(repeat.effect).toBe("deny")

      await vfs.fire(vfs.tool, "execute.after", { tool: "shell", sessionID: "dispatch", id: "s1" })
      expect(existsSync(dirname(String(created.shell)))).toBe(false)
      expect(vfs.requests("vfs","shell-complete")).toHaveLength(1)
    } finally { await vfs.stop() }
  })

  test("a bound specialist's captured command is imported as a staged delta", async () => {
    sandboxed()
    const vfs = await startVfs({ sessions: unitSessions, respond: importOk })
    try {
      await vfs.run("vfs_bind", { scope: ["src/a.go"] })
      const marked = await admit(vfs, shellEvent())
      const [prepare] = vfs.requests("vfs", "shell-prepare")
      const created: HookEvent = { command: marked }
      await vfs.fire(vfs.shell, "create.before", created)
      expect(readFileSync(String(created.shell), "utf8")).not.toContain("export HOME")
      await vfs.fire(vfs.tool, "execute.after", { tool: "shell", sessionID: "dispatch", id: "s1" })
      expect(vfs.requests("vfs", "shell-complete")).toEqual([{ ipc_version: IPC_VERSION, session_id: "root", caller_session: "dispatch", caller_agent: "dev", call_id: prepare.call_id }])
    } finally { await vfs.stop() }
  })

  test("an import the core refuses because the staged work moved reaches the caller unchanged", async () => {
    sandboxed()
    const moved = "the staged work changed while the command ran; run the command again"
    const vfs = await startVfs({
      sessions: unitSessions,
      respond: (call) => call.command === "shell-prepare" ? shellReply({ capture: true }) : call.command === "shell-complete" ? json({ ok: false, error: moved }) : undefined,
    })
    try {
      await vfs.fire(vfs.shell, "create.before", { command: await admit(vfs, shellEvent()) })
      await expect(vfs.fire(vfs.tool, "execute.after", { tool: "shell", sessionID: "dispatch", id: "s1", agent: "dev" })).rejects.toThrow(new RegExp(`^${moved}$`))
      expect(Object.keys(vfs.requests("vfs", "shell-complete")[0]).sort()).toEqual(["call_id", "ipc_version", ...CALLER_FIELDS].sort())
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

  test("identical commands from different calls both run, each in its own sandbox with its own import", async () => {
    sandboxed()
    const vfs = await startVfs({ sessions: { ...unitSessions, other: { parentID: "root", title: "unit-b" } }, respond: importOk })
    try {
      const first = await admit(vfs, shellEvent())
      const second = await admit(vfs, shellEvent({ sessionID: "other", id: "s2" }))
      const third = await admit(vfs, shellEvent({ id: "s3" }))
      expect(new Set([first, second, third]).size).toBe(3)
      const [p1, p2, p3] = vfs.requests("vfs", "shell-prepare")
      const [a, b, c]: HookEvent[] = [{ command: first }, { command: second }, { command: third }]
      await Promise.all([a, b, c].map((created) => vfs.fire(vfs.shell, "create.before", created)))
      for (const [created, prepare] of [[a, p1], [b, p2], [c, p3]] as const) {
        expect(created.command).toBe(command)
        expect(readFileSync(String(created.shell), "utf8")).toContain(`|${prepare.call_id}|`)
      }
      expect(new Set([a.shell, b.shell, c.shell]).size).toBe(3)
      await vfs.fire(vfs.tool, "execute.after", { tool: "shell", sessionID: "other", id: "s2" })
      expect(vfs.requests("vfs", "shell-complete")).toEqual([expect.objectContaining({ caller_session: "other", call_id: p2.call_id })])
      await vfs.fire(vfs.tool, "execute.after", { tool: "shell", sessionID: "dispatch", id: "s1" })
      await vfs.fire(vfs.tool, "execute.after", { tool: "shell", sessionID: "dispatch", id: "s3" })
      expect(vfs.requests("vfs", "shell-complete").map((request) => String((request as { call_id?: unknown }).call_id))).toEqual([p2, p1, p3].map((prepare) => String(prepare.call_id)))
    } finally { await vfs.stop() }
  })

  test("two interleaved create.before events for one call: only one runs", async () => {
    mock.module("./takt-sandbox.mjs", () => ({
      wrap: async (line: string, options: { callID: string }) => { await sleep(SETTLE_STEP_MS); return `wrapped:${line}|${options.callID}` },
      supervise: (wrapped: string, confirm: string) => `supervised(${wrapped}|${confirm})`,
    }))
    const vfs = await startVfs({ sessions: unitSessions, respond: importOk })
    try {
      const marked = await admit(vfs, shellEvent())
      const [prepare] = vfs.requests("vfs", "shell-prepare")
      const one: HookEvent = { command: marked }
      const two: HookEvent = { command: marked }
      await Promise.all([vfs.fire(vfs.shell, "create.before", one), vfs.fire(vfs.shell, "create.before", two)])
      const ran = [one, two].filter((event) => event.shell !== undefined)
      expect(ran).toHaveLength(1)
      expect(String([one, two].find((event) => event.shell === undefined)?.command)).toContain("this shell command was not admitted")
      expect(readFileSync(String(ran[0].shell), "utf8")).toContain(`|${prepare.call_id}|`)
      await vfs.fire(vfs.tool, "execute.after", { tool: "shell", sessionID: "dispatch", id: "s1" })
      expect(vfs.requests("vfs", "shell-complete")).toEqual([expect.objectContaining({ call_id: prepare.call_id })])
    } finally { await vfs.stop() }
  })

  test("interleaved create.before events of two calls with the same text keep their own plans", async () => {
    mock.module("./takt-sandbox.mjs", () => ({
      wrap: async (line: string, options: { callID: string }) => { await sleep(SETTLE_STEP_MS); return `wrapped:${line}|${options.callID}` },
      supervise: (wrapped: string, confirm: string) => `supervised(${wrapped}|${confirm})`,
    }))
    const vfs = await startVfs({ sessions: unitSessions, respond: importOk })
    try {
      const first = await admit(vfs, shellEvent())
      const second = await admit(vfs, shellEvent({ id: "s2" }))
      const [p1, p2] = vfs.requests("vfs", "shell-prepare")
      const a: HookEvent = { command: first }
      const b: HookEvent = { command: second }
      await Promise.all([vfs.fire(vfs.shell, "create.before", b), vfs.fire(vfs.shell, "create.before", a)])
      expect(readFileSync(String(a.shell), "utf8")).toContain(`|${p1.call_id}|`)
      expect(readFileSync(String(b.shell), "utf8")).toContain(`|${p2.call_id}|`)
    } finally { await vfs.stop() }
  })

  test("a failed preparation releases the reservation", async () => {
    let reply: Reply = json({ ok: true })
    const vfs = await startVfs({ sessions: unitSessions, respond: (call) => call.command === "shell-prepare" ? reply : undefined })
    try {
      const failed = shellEvent()
      await expect(vfs.fire(vfs.tool, "execute.before", failed)).rejects.toThrow("response omitted shell plan")
      const late: HookEvent = { command: String((failed.input as { command: string }).command) }
      await vfs.fire(vfs.shell, "create.before", late)
      expect(String(late.command)).toContain("this shell command was not admitted")
      reply = shellReply()
      await vfs.fire(vfs.tool, "execute.before", shellEvent({ id: "s2" }))
    } finally { await vfs.stop() }
  })

  test("a user shell can never consume an orchestrator's admission", async () => {
    sandboxed()
    const vfs = await startVfs({ sessions: unitSessions })
    try {
      const marked = await admit(vfs, shellEvent({ agent: ORCHESTRATOR, sessionID: "root", id: "d1", input: { command: "pwd" } }))
      const user: HookEvent = { command: "pwd" }
      await vfs.fire(vfs.shell, "create.before", user)
      expect(String(user.command)).toContain("this shell command was not admitted")
      const guess: HookEvent = { command: `pwd\n#takt-call:${"0".repeat(32)}` }
      await vfs.fire(vfs.shell, "create.before", guess)
      expect(String(guess.command)).toContain("this shell command was not admitted")
      // The orchestrator's own call still has its admission.
      const own: HookEvent = { command: marked }
      await vfs.fire(vfs.shell, "create.before", own)
      expect(own.command).toBe("pwd")
      expect(own.shell).toBeUndefined()
    } finally { await vfs.stop() }
  })

  test("a direct admission and a specialist admission of the same text never mix", async () => {
    sandboxed()
    const vfs = await startVfs({ sessions: unitSessions })
    try {
      const direct = admit(vfs, shellEvent({ agent: ORCHESTRATOR, sessionID: "root", id: "d1" }))
      const specialist = admit(vfs, shellEvent())
      const [directMarked, specialistMarked] = [await direct, await specialist]
      const own: HookEvent = { command: specialistMarked }
      await vfs.fire(vfs.shell, "create.before", own)
      expect(own.shell).toBeDefined()
      const native: HookEvent = { command: directMarked }
      await vfs.fire(vfs.shell, "create.before", native)
      expect(native.shell).toBeUndefined()
      expect(native.command).toBe(command)
    } finally { await vfs.stop() }
  })

  test("a call that ends in error releases its reservation and its wrapper directory", async () => {
    sandboxed()
    const vfs = await startVfs({ sessions: unitSessions, respond: importOk })
    try {
      const marked = await admit(vfs, shellEvent())
      const created: HookEvent = { command: marked }
      await vfs.fire(vfs.shell, "create.before", created)
      expect(existsSync(dirname(String(created.shell)))).toBe(true)
      await vfs.fire(vfs.tool, "execute.after", { tool: "shell", sessionID: "dispatch", id: "s1", status: "error" })
      expect(existsSync(dirname(String(created.shell)))).toBe(false)
      const replay: HookEvent = { command: marked }
      await vfs.fire(vfs.shell, "create.before", replay)
      expect(String(replay.command)).toContain("this shell command was not admitted")
    } finally { await vfs.stop() }
  })

  test("a call whose execute.after never fires is released when its session goes idle or is deleted", async () => {
    sandboxed()
    const vfs = await startVfs({ sessions: unitSessions, respond: importOk })
    try {
      const idleCreated: HookEvent = { command: await admit(vfs, shellEvent()) }
      await vfs.fire(vfs.shell, "create.before", idleCreated)
      const [prepare] = vfs.requests("vfs", "shell-prepare")
      vfs.publish({ id: "evt-idle", created: 1, type: "session.idle", data: { sessionID: "dispatch" } } as OpenCodeEvent)
      await until(() => !existsSync(dirname(String(idleCreated.shell))) && vfs.requests("vfs", "shell-complete").length === 1)
      expect(vfs.requests("vfs", "shell-complete")).toEqual([expect.objectContaining({ call_id: prepare.call_id })])

      const goneCreated: HookEvent = { command: await admit(vfs, shellEvent({ id: "s2" })) }
      await vfs.fire(vfs.shell, "create.before", goneCreated)
      vfs.callStatus("root:call-r","error")
      vfs.publish({ id: "evt-gone", created: 2, type: "session.deleted", data: { sessionID: "dispatch" } } as OpenCodeEvent)
      await until(() => !existsSync(dirname(String(goneCreated.shell))))
      expect(vfs.requests("vfs", "shell-complete")).toHaveLength(1)
    } finally { await vfs.stop() }
  })

  test("evaluate correlates by tool call, not by session", async () => {
    sandboxed()
    const vfs = await startVfs({
      sessions: unitSessions,
      respond: (call) => call.command === "shell-prepare" ? shellReply({ reason: "review each run" }) : undefined,
    })
    try {
      const first: HookEvent = { command: await admit(vfs, shellEvent()) }
      const second: HookEvent = { command: await admit(vfs, shellEvent({ id: "s2" })) }
      await vfs.fire(vfs.shell, "create.before", first)
      // s2 has not started: evaluating it is refused, s1's admission stays intact.
      const early: HookEvent = { action: "shell", effect: "allow", sessionID: "dispatch", agent: "dev", source: toolSource("s2") }
      await vfs.fire(vfs.permission, "evaluate", early)
      expect(early.effect).toBe("deny")
      await vfs.fire(vfs.shell, "create.before", second)
      const unrelated: HookEvent = { action: "shell", effect: "allow", sessionID: "dispatch", agent: "dev", source: toolSource("s9") }
      await vfs.fire(vfs.permission, "evaluate", unrelated)
      expect(unrelated.effect).toBe("deny")
      const sourceless: HookEvent = { action: "shell", effect: "allow", sessionID: "dispatch", agent: "dev" }
      await vfs.fire(vfs.permission, "evaluate", sourceless)
      expect(sourceless.effect).toBe("deny")
      for (const id of ["s2", "s1"]) {
        const own: HookEvent = { action: "shell", effect: "allow", sessionID: "dispatch", agent: "dev", source: toolSource(id) }
        await vfs.fire(vfs.permission, "evaluate", own)
        expect([own.effect, own.message]).toEqual(["ask", "review each run"])
      }
    } finally { await vfs.stop() }
  })

  test("a direct reservation whose call ends without create.before cannot be consumed later", async () => {
    const vfs = await startVfs()
    try {
      const marked = await admit(vfs, shellEvent({ agent: ORCHESTRATOR, sessionID: "root", id: "d1", input: { command: "pwd" } }))
      await vfs.fire(vfs.tool, "execute.after", { tool: "shell", sessionID: "root", id: "d1" })
      const user: HookEvent = { command: marked }
      await vfs.fire(vfs.shell, "create.before", user)
      expect(String(user.command)).toContain("this shell command was not admitted")
    } finally { await vfs.stop() }
  })

  test("the orchestrator's direct commands pass once per admission", async () => {
    const vfs = await startVfs()
    try {
      const first = await admit(vfs, shellEvent({ agent: ORCHESTRATOR, input: { command: "pwd" } }))
      const second = await admit(vfs, shellEvent({ agent: ORCHESTRATOR, id: "s2", input: { command: "pwd" } }))
      const passes: HookEvent[] = [{ command: first }, { command: second }, { command: first }]
      await passes.reduce((chain, event) => chain.then(() => vfs.fire(vfs.shell, "create.before", event)), Promise.resolve())
      expect(passes.map(({ command: text }) => String(text).includes("was not admitted"))).toEqual([false, false, true])
      expect(passes[0].command).toBe("pwd")
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
      expect(toolNames(event)).toEqual(["vfs_write", "read"])
      expect(JSON.stringify(event.system)).not.toContain("vfs_delete")
      expect(JSON.stringify(event.system)).not.toContain("vfs_read")
      expect(event.system).toContainEqual({ type: "text", text: expect.stringContaining("Work unit unit-a owns exactly these workspace-relative paths: src/a.go, src/b.go.") })
    } finally { await vfs.stop() }
  })

  test("an author arrives bound to the scope the harness assigned; a refused bind leaves vfs_bind with its reason", async () => {
    const scope = ["src/a.go", "src/b.go"]
    for (const refused of [false, true]) {
      const vfs = await startVfs({ sessions: unitSessions, permissions: { dev: grants }, claims: [claim({ pending: true, scope })],
        respond: (call) => refused && call.command === "bind" ? json({ ok: false, error: "no admission yet" }) : undefined })
      try {
        const event = contextFor()
        await vfs.fire(vfs.session, "context", event)
        expect(vfs.requests("vfs", "bind")).toEqual([expect.objectContaining({ work_unit_id: "unit-a", scope })])
        expect(toolNames(event)).toEqual(refused ? ["vfs_bind", "vfs_write", "read"] : ["vfs_write", "read"])
        const text = event.system.map(part => part.text).join("\n")
        expect(text.includes("no admission yet")).toBe(refused)
        expect(text).not.toContain("author_key")
      } finally { await vfs.stop() }
    }
  })

  test("a claim naming an author turns the grant into a judging assignment", async () => {
    const vfs = await startVfs({ sessions: unitSessions, permissions: { dev: grants }, claims: [claim({ pending: true, author_key: "author-9" })] })
    try {
      const event = contextFor()
      await vfs.fire(vfs.session, "context", event)
      expect(event.system.map(part=>part.text).join("\n")).toContain("You judge the staged work of these authors for work unit unit-a; every gate is already bound.")
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

describe("permission responses are outside telemetry's critical path",()=>{
  test("a usage lookup failure cannot kill the approval consumer",async()=>{
    let created:Record<string,unknown>|undefined
    mock.module("@opencode/client/service",()=>({Service:{discover:async()=>({url:"http://fixture"}),headers:()=>({})}}))
    mock.module("@opencode/client",()=>({OpenCode:{make:()=>({permission:{create:async(r:Record<string,unknown>)=>{created=r;return {id:r.id,effect:"ask"}}}})}}))
    const failing="a current verification verdict failed"
    const vfs=await startVfs({respond:call=>call.kind==="dispatch"&&call.command==="resolve"&&call.request.child==="usage-only"
      ? {code:1,err:"telemetry execution lookup failed"}
      : call.command==="consolidate" && !call.request.accept_failing ? json({ok:false,error:failing,revision:1,delta_hash:"h1"}):undefined})
    try {
      vfs.publish({id:"bad-usage",created:1,type:"session.usage.updated",data:{sessionID:"usage-only",cost:1,tokens:{input:1,output:1,reasoning:0,cache:{read:0,write:0}}}} as OpenCodeEvent)
      const result=vfs.run("vfs_consolidate",{author_key:"author"},{...orchestrator,id:"approve",messageID:"m"} as Ctx)
      await until(()=>created!==undefined)
      vfs.publish({id:"reply",created:1,type:"permission.replied",data:{requestID:String(created!.id),reply:"once"}} as OpenCodeEvent)
      expect((await result).content).toBe("Consolidated to workspace")
      expect(vfs.errors.some(e=>e[0]==="Takt usage context unavailable")).toBe(true)
    } finally {await vfs.stop()}
  })
  test("an orchestrator question has no delegation correlation to resolve",async()=>{
    const vfs=await startVfs({respond:call=>call.kind==="dispatch"&&call.command==="resolve"?{code:1,err:"queries unavailable"}:undefined})
    try {
      const before=vfs.requests("dispatch","resolve").length
      await vfs.fire(vfs.tool,"execute.before",{tool:"question",sessionID:"root",agent:ORCHESTRATOR,id:"question",input:{}})
      await vfs.fire(vfs.tool,"execute.after",{tool:"question",sessionID:"root",agent:ORCHESTRATOR,id:"question",status:"completed"})
      expect(vfs.requests("dispatch","resolve").length).toBe(before)
    }finally{await vfs.stop()}
  })
  test("post-result context failures diagnose without rejecting an already successful VFS result",async()=>{
    const vfs=await startVfs({respond:call=>call.kind==="dispatch"&&call.command==="resolve"?{code:1,err:"queries unavailable"}:undefined})
    try {
      await vfs.fire(vfs.tool,"execute.after",{tool:"vfs_write",sessionID:"root",agent:ORCHESTRATOR,id:"write",status:"completed"})
      await until(()=>vfs.errors.some(e=>e[0]==="Takt post-result maintenance context unavailable"))
      expect(vfs.errors.some(e=>e[0]==="Takt post-result maintenance context unavailable")).toBe(true)
    }finally{await vfs.stop()}
  })
})

test("a vanished child and an expired owner never prove termination of a running native call",async()=>{
  const vfs=await startVfs({missing:["gone"],nativeCalls:{"old:call":"running"},history:{executions:saved({"old:call":{unit:"orphan",root:"root",child:"gone",owner:"expired"}})}})
  const launch=async(unit:string)=>{await vfs.run("dispatch_inputs",{work_unit_id:unit,result_ids:[]},orchestrator);await vfs.fire(vfs.tool,"execute.before",{tool:"subagent",sessionID:"root",agent:ORCHESTRATOR,id:unit,input:{description:unit,agent:RESULT_AGENT}})}
  try {
    await launch("first")
    await until(()=>vfs.errors.some(e=>e[0]==="Takt native delegation reconciliation"))
    expect(vfs.history.executions["old:call"].settled).toBeFalsy()
    vfs.callStatus("old:call","error")
    await launch("second")
    expect(vfs.history.executions["old:call"].settled).toBe(true)
  }finally{await vfs.stop()}
})

test("a completed background launch with a verified deleted child can compensate its exact attempt",async()=>{
  const vfs=await startVfs({missing:["gone"],nativeCalls:{"old:call":"completed"},history:{executions:saved({"old:call":{unit:"orphan",root:"root",child:"gone",background:true,owner:"expired"}})}})
  try {
    await vfs.run("dispatch_inputs",{work_unit_id:"next",result_ids:[]},orchestrator)
    await vfs.fire(vfs.tool,"execute.before",{tool:"subagent",sessionID:"root",agent:ORCHESTRATOR,id:"next",input:{description:"next",agent:RESULT_AGENT}})
    expect(vfs.history.executions["old:call"].settled).toBe(true)
    expect(vfs.requests("dispatch","finish")).toContainEqual(expect.objectContaining({dispatch:"old:call",attempt:"1"}))
  }finally{await vfs.stop()}
})

test("a background delegation whose launch call left the caller's context still settles when its child is idle",async()=>{
  const vfs=await startVfs({history:{executions:saved({"old:call":{unit:"orphan",root:"root",child:"idle",background:true,owner:"expired"}})}})
  try {
    await vfs.run("dispatch_inputs",{work_unit_id:"next",result_ids:[]},orchestrator)
    await vfs.fire(vfs.tool,"execute.before",{tool:"subagent",sessionID:"root",agent:ORCHESTRATOR,id:"next",input:{description:"next",agent:RESULT_AGENT}})
    await until(()=>!!vfs.history.executions["old:call"].settled)
    expect(vfs.requests("dispatch","finish")).toContainEqual(expect.objectContaining({dispatch:"old:call",attempt:"1"}))
  }finally{await vfs.stop()}
})

test("a failed background child wait is not proof of termination",async()=>{
  const vfs=await startVfs({nativeCalls:{"old:call":"completed"},wait:async()=>{throw Error("host unavailable")},history:{executions:saved({"old:call":{unit:"orphan",root:"root",child:"live",background:true,owner:"expired"}})}})
  try {
    await vfs.run("dispatch_inputs",{work_unit_id:"next",result_ids:[]},orchestrator)
    await vfs.fire(vfs.tool,"execute.before",{tool:"subagent",sessionID:"root",agent:ORCHESTRATOR,id:"next",input:{description:"next",agent:RESULT_AGENT}})
    await until(()=>vfs.errors.some(e=>e[0]==="Takt native delegation reconciliation"))
    expect(vfs.history.executions["old:call"].settled).toBeFalsy()
    expect(vfs.requests("dispatch","finish")).not.toContainEqual(expect.objectContaining({dispatch:"old:call"}))
  }finally{await vfs.stop()}
})

describe("native inspection lifecycle", () => {
  test("native read reserves before execution and returns the original result", async () => {
    let enter!:()=>void
    let finish!:()=>void
    const entered = new Promise<void>(resolve=>{enter=resolve})
    const gate = new Promise<void>(resolve=>{finish=resolve})
    const vfs = await startVfs({ sessions: unitSessions, nativeRead: async input => { enter(); await gate; return {content:"native read",original:input} }, respond: call => {
      if (call.command === "native-inspection-start" || call.command === "native-inspection-end") return json({ok:true})
      return undefined
    } })
    try {
      const input = {filePath:"own.go",offset:2,limit:1}
      const running = vfs.run("read",input,{...author,id:"read-1"})
      await entered
      expect(vfs.requests("vfs","native-inspection-start")).toHaveLength(1)
      expect(vfs.requests("vfs","native-inspection-end")).toHaveLength(0)
      finish()
      expect(await running).toMatchObject({content:"native read",original:input})
      expect(vfs.requests("vfs","native-inspection-end")).toHaveLength(1)
      expect(vfs.requests("vfs","native-inspection-start")[0]).toMatchObject({tool:"read",path:"own.go"})
    } finally { finish?.(); await vfs.stop() }
  })

  test("native errors still end the reservation and release failures remain visible", async () => {
    const vfs = await startVfs({ sessions:unitSessions, nativeRead:async()=>{throw new Error("native read failed")}, respond:call=>{
      if (call.command === "native-inspection-start") return json({ok:true})
      if (call.command === "native-inspection-end") return {code:1,err:"release failed"}
      return undefined
    } })
    try {
      await expect(vfs.run("read",{filePath:"own.go"},{...author,id:"read-2"})).rejects.toThrow("native inspection and reservation release failed")
      expect(vfs.requests("vfs","native-inspection-end")).toHaveLength(1)
    } finally { await vfs.stop() }
  })

  test("an author's native read without a host call id reserves nothing and is refused", async () => {
    const vfs = await startVfs({ sessions:unitSessions })
    try {
      await expect(vfs.run("read",{filePath:"own.go"},author)).rejects.toThrow("no host call id")
      expect(vfs.requests("vfs","native-inspection-start")).toHaveLength(0)
    } finally { await vfs.stop() }
  })

  test("reconciliation releases a reservation whose caller session is gone, never a live one", async () => {
    const inspections = [
      {call_id:"gone-call",session_id:"root",caller_session:"gone",caller_agent:VFS_AUTHOR},
      {call_id:"live-call",session_id:"root",caller_session:"live",caller_agent:VFS_AUTHOR},
    ]
    const vfs = await startVfs({ missing:["gone"], respond: call => call.command === "native-inspections" ? json({ok:true,inspections}) : undefined })
    try {
      await vfs.run("dispatch_inputs",{work_unit_id:"next",result_ids:[]},orchestrator)
      await vfs.fire(vfs.tool,"execute.before",{tool:"subagent",sessionID:"root",agent:ORCHESTRATOR,id:"next",input:{description:"next",agent:RESULT_AGENT}})
      await until(()=>vfs.requests("vfs","native-inspection-end").length > 0)
      expect(vfs.requests("vfs","native-inspection-end")).toEqual([expect.objectContaining({call_id:"gone-call",caller_session:"gone"})])
    } finally { await vfs.stop() }
  })

  test("a non-author's native read reserves nothing", async () => {
    const vfs = await startVfs({ sessions:unitSessions, nativeRead:async input=>({content:"native read",original:input}) })
    try {
      expect(await vfs.run("read",{filePath:"own.go"},dev)).toMatchObject({content:"native read"})
      expect(vfs.requests("vfs","native-inspection-start")).toHaveLength(0)
      expect(vfs.requests("vfs","native-inspection-end")).toHaveLength(0)
    } finally { await vfs.stop() }
  })
})
