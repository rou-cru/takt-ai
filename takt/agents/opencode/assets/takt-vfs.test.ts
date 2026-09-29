import { describe, expect, test } from "bun:test"
import { readFileSync } from "node:fs"
import { resolve } from "node:path"
import type { OpenCodeEvent } from "@opencode/client"
import vfsPlugin from "./takt-vfs"
import {
  asString,
  gcCoordinateResponse,
  isBoolean,
  isCoordinator,
  isCoordinatorResponse,
  isGCCycle,
  isGCCycleResponse,
  isGCPlan,
  isHandoffEnvelope,
  isOptional,
  isResponseObject,
  isStagedView,
  isString,
  isStringArray,
  isToolInput,
  orchestratorOnly,
  parseClaim,
  parseCoordinationResponse,
  parseVFSShell,
  responseObject,
  toolInput,
  vfsResponse,
} from "./takt-vfs"

const contracts = JSON.parse(readFileSync(resolve(import.meta.dir, "../testdata/contracts.json"), "utf8"))

describe("plugin setup", () => {
  test("registers the VFS and dispatch tools and hooks", async () => {
    const runtime = globalThis as unknown as { Bun: { spawn: (...args: never[]) => unknown } }
    const originalSpawn = runtime.Bun.spawn
    const tools = new Map<string, { execute?: (...args: never[]) => unknown }>()
    const hooks = new Map<string, unknown[]>()
    const toolHooks = new Map<string, unknown[]>()
    const permissionHooks = new Map<string, unknown[]>()
    const shellHooks = new Map<string, unknown[]>()
    const storage = new Map<string, unknown>()
    const requests: Array<{ command: string; request: Record<string, unknown> }> = []
    const eventQueue: OpenCodeEvent[] = []
    let wakeEvent: (() => void) | undefined
    let eventsClosed = false
    let disposePlugin: (() => void) | undefined
    async function* subscribedEvents(): AsyncGenerator<OpenCodeEvent> {
      while (!eventsClosed) {
        while (eventQueue.length === 0 && !eventsClosed) {
          await new Promise<void>((resolve) => { wakeEvent = resolve })
        }
        if (eventsClosed) return
        const event = eventQueue.shift()
        if (event) yield event
      }
    }
    const publishEvent = (event: OpenCodeEvent) => {
      eventQueue.push(event)
      wakeEvent?.()
    }
    runtime.Bun.spawn = ((argv: string[]) => {
      let request: Record<string, unknown> = {}
      const command = argv[2]
      if (argv[1] === "dispatch" || argv[1] === "gc") request = JSON.parse(argv[argv[1] === "gc" ? 8 : 7])
      return {
        stdin: { write(value: string) { request = JSON.parse(value) }, end() {} },
        get stdout() {
          requests.push({ command, request })
          if (command === "codegraph") return ""
          if (command === "ingest") return "{}"
          if (command === "claims") return JSON.stringify({ ok: true, claims: [{ key: "claim-1", root_session_id: "root", work_unit_id: "unit-a", agent_id: "dev", target_instance: "dev", active: true, scope: ["src/held.ts"] }] })
          if (argv[1] === "dispatch" && request.action === "switch") return JSON.stringify({ ok: true })
          if (argv[1] === "dispatch" || argv[1] === "gc") return JSON.stringify({ version: 1, units: 0, mutations: 0, cursor: 0, deferrals: 0, next_mandate: 0, requested: false, draining: false })
          if (command === "shell-prepare") return JSON.stringify({ ok: true, shell: { decision: "ask", reason: "review each run", cwd: "/workspace", scratch: "/private/tmp", confirm: "/private/tmp/confirm", capture: false, writable: ["/workspace/src"], protected: ["/workspace/.git"], private: ["/private/secret"] } })
          if (command === "bind") return JSON.stringify({ ok: true, key: request.agent_id === "verify" ? "verifier-1" : "author-1", attempt_id: "attempt-1", invariants_version: "invariants-1", revision: 0, delta_hash: "delta-0" })
          return JSON.stringify({ ok: true, key: "claim-1", revision: 2, delta_hash: "delta-2", content: "staged content" })
        },
        stderr: "",
        exited: Promise.resolve(0),
      }
    }) as typeof originalSpawn

    try {
      const cleanup = await vfsPlugin.setup({
        location: { directory: "/workspace" },
        storage: { async get(key: string) { return storage.get(key) }, async set(key: string, value: unknown) { storage.set(key, value) } },
        session: {
          async get({ sessionID }: { sessionID: string }) {
            return sessionID === "dispatch" ? { id: sessionID, parentID: "root", title: "unit-a" }
              : sessionID === "verifier" ? { id: sessionID, parentID: "root", title: "unit-a-verify" }
                : { id: sessionID }
          },
          async hook(name: string, callback: unknown) { hooks.set(name, [...(hooks.get(name) ?? []), callback]); return { dispose() {} } },
          async create() { return { id: "child" } }, async prompt() {}, async interrupt() {}, async synthetic() {},
        },
        agent: { async get({ agentID }: { agentID: string }) { return { data: { id: agentID, permissions: [] } } } },
        permission: { async hook(name: string, callback: unknown) { permissionHooks.set(name, [...(permissionHooks.get(name) ?? []), callback]); return { dispose() {} } } },
        shell: { async hook(name: string, callback: unknown) { shellHooks.set(name, [...(shellHooks.get(name) ?? []), callback]); return { dispose() {} } } },
        tool: {
          async hook(name: string, callback: unknown) { toolHooks.set(name, [...(toolHooks.get(name) ?? []), callback]); return { dispose() {} } },
          async transform(callback: (editor: { add(definition: { name: string; execute?: (...args: never[]) => unknown }): void }) => void) {
            callback({ add(definition) { tools.set(definition.name, definition) } })
            return { dispose() {} }
          },
        },
        event: { subscribe({ signal }: { signal: AbortSignal }) {
          signal.addEventListener("abort", () => {
            eventsClosed = true
            wakeEvent?.()
          }, { once: true })
          return { [Symbol.asyncIterator]: subscribedEvents }
        } },
      } as never)
      if (typeof cleanup === "function") disposePlugin = cleanup

      expect(tools.has("vfs_bind")).toBe(true)
      expect(tools.has("vfs_write")).toBe(true)
      expect(tools.has("dispatch_commit")).toBe(true)
      expect(tools.has("dispatch_switch")).toBe(true)
      expect(hooks.has("context")).toBe(true)

      const execute = (name: string, input: unknown, context = { sessionID: "dispatch", agent: "dev" }) => {
        const tool = tools.get(name)?.execute
        if (!tool) throw new Error(`missing tool ${name}`)
        return (tool as unknown as (value: unknown, context: { sessionID: string; agent: string }) => Promise<{ content: string }>)(input, context)
      }
      const invokeHook = async (collection: Map<string, unknown[]>, name: string, index: number, event: Record<string, unknown>) => {
        const callback = collection.get(name)?.[index]
        if (typeof callback !== "function") throw new Error(`missing ${name} hook ${index}`)
        await (callback as (value: Record<string, unknown>) => Promise<void>)(event)
      }
      await expect(execute("gc_baseline", {})).rejects.toThrow("GC baseline requires the attached verifier session")
      await expect(execute("gc_prepare", {})).rejects.toThrow("GC preparation belongs to the root ordinary workflow")
      await expect(execute("dispatch_commit", { version: "v1", plan: [] })).rejects.toThrow("dispatch_commit belongs to the root orchestrator")
      await expect(execute("vfs_discard", { author_key: "claim-1", call_id: "discard-1" })).rejects.toThrow("vfs_discard belongs to the orchestrator")
      await expect(execute("vfs_verify", { author_key: "missing", pass: false, finding: "not staged" })).rejects.toThrow("author_key does not name an active binding")
      await expect(execute("vfs_consolidate", { author_key: "claim-1", checkpoint: "checkpoint" })).rejects.toThrow("vfs_consolidate belongs to the orchestrator")
      const orchestrator = { sessionID: "root", agent: "__TAKT_ORCHESTRATOR_ID__" }
      expect((await execute("claim_list", {}, orchestrator)).content).toContain("claim-1")
      await expect(execute("claim_release", { claim_key: "missing" }, orchestrator)).rejects.toThrow("unknown claim_key missing")
      await execute("claim_release", { claim_key: "claim-1", confirmed: true }, orchestrator)
      await execute("claim_assign", { work_unit_id: "unit-b", agent: "test", scope: ["src/new.ts"] }, orchestrator)
      await execute("claim_assign_verifier", { work_unit_id: "unit-b-verify", agent: "verify", author_key: "claim-1" }, orchestrator)
      await execute("dispatch_commit", { version: "plan-1", plan: [{ unit: "unit-b", contract: "unit tests" }] }, orchestrator)
      await execute("dispatch_activity_start", { activity_id: "activity-1", node_kind: "orchestrator" }, orchestrator)
      await execute("dispatch_close_recovery", { objective: "restore", evidence: "tests pass", demonstrated: true }, orchestrator)
      await execute("dispatch_switch", { target_agent: "dev", objective: "Implement feature", requirements: "typed", client: "review" }, orchestrator)
      await execute("dispatch_abort_switch", { reason: "handoff not needed" }, orchestrator)
      await invokeHook(toolHooks, "execute.before", 1, {
        tool: "subagent", sessionID: "dispatch", id: "delegation-1", input: { agent: "dev", description: "unit-a" },
      })
      await invokeHook(toolHooks, "execute.after", 2, {
        tool: "subagent", sessionID: "dispatch", id: "delegation-1", status: "completed",
      })
      await execute("gc_request", {}, orchestrator)
      await new Promise((resolve) => setTimeout(resolve, 10))
      expect(requests.some(({ command }) => command === "coordinate")).toBe(true)
      await execute("vfs_bind", { scope: ["src/a.go"] })
      await execute("vfs_write", { path: "src/a.go", content: "hello", call_id: "write-1" })
      expect((await execute("vfs_read", { path: "src/a.go", call_id: "read-1" })).content).toBe("staged content")
      await execute("vfs_delete", { path: "src/a.go", call_id: "delete-1" })
      expect(requests.map(({ command }) => command)).toContain("op")
      expect(requests.some(({ request }) => request.action === "create")).toBe(true)
      expect(requests.some(({ request }) => request.action === "read")).toBe(true)
      expect(requests.some(({ request }) => request.action === "delete")).toBe(true)

      const unadmittedShell = { command: "echo outside dispatch" }
      await invokeHook(shellHooks, "create.before", 0, unadmittedShell)
      expect(unadmittedShell.command).toContain("shell command was not admitted")
      const directShell = { command: "echo direct" }
      await invokeHook(toolHooks, "execute.before", 0, { tool: "shell", input: { command: directShell.command }, sessionID: "dispatch", agent: "__TAKT_ORCHESTRATOR_ID__" })
      await invokeHook(shellHooks, "create.before", 0, directShell)
      expect(directShell.command).toBe("echo direct")
      // RESULT_AGENTS is untemplated here, so this placeholder self-matches via .includes().
      const resultAgentShell = { command: "echo result-agent" }
      await invokeHook(toolHooks, "execute.before", 0, { tool: "shell", input: { command: resultAgentShell.command }, sessionID: "dispatch", agent: "__TAKT_RESULT_AGENTS__" })
      await invokeHook(shellHooks, "create.before", 0, resultAgentShell)
      expect(resultAgentShell.command).toBe("echo result-agent")
      const rejectedPermission: { action: string; effect: string; sessionID: string; agent: string; message?: string } = {
        action: "shell", effect: "allow", sessionID: "dispatch", agent: "dev",
      }
      await invokeHook(permissionHooks, "evaluate", 0, rejectedPermission)
      expect(rejectedPermission.effect).toBe("deny")
      expect(rejectedPermission.message).toContain("it was not admitted")
      const admittedShell: { command: string; shell?: string } = { command: "echo governed" }
      await invokeHook(toolHooks, "execute.before", 0, { tool: "shell", input: { command: admittedShell.command }, sessionID: "dispatch", agent: "dev" })
      expect(requests.map(({ command }) => command)).toContain("shell-prepare")
      await invokeHook(shellHooks, "create.before", 0, admittedShell)
      if (admittedShell.shell) {
        const shellPermission = { action: "shell", effect: "allow", sessionID: "dispatch", agent: "dev", message: "" }
        await invokeHook(permissionHooks, "evaluate", 0, shellPermission)
        expect(shellPermission.effect).toBe("ask")
        expect(shellPermission.message).toBe("review each run")
      } else {
        expect(admittedShell.command).toContain("sandbox unavailable, shell denied")
      }
      await invokeHook(toolHooks, "execute.after", 2, { tool: "shell", sessionID: "dispatch", id: "shell-1" })
      await invokeHook(toolHooks, "execute.before", 0, { sessionID: "dispatch", id: "tool-1" })
      await invokeHook(toolHooks, "execute.after", 0, { sessionID: "dispatch", id: "tool-1", tool: "read", agent: "dev", status: "completed" })
      await invokeHook(toolHooks, "execute.before", 1, { sessionID: "dispatch", id: "other-1", tool: "read", agent: "dev", input: {} })

      const permission: { action: string; effect: string; resources: string[]; sessionID: string; agent: string; message?: string } = {
        action: "edit", effect: "allow", resources: ["/workspace/src/held.ts"], sessionID: "dispatch", agent: "dev",
      }
      await invokeHook(permissionHooks, "evaluate", 1, permission)
      expect(requests.map(({ command }) => command)).toContain("claims")
      expect(permission.effect).toBe("deny")
      expect(permission.message).toContain("src/held.ts is held by work unit unit-a")
      await invokeHook(permissionHooks, "evaluate", 1, { ...permission, effect: "deny" })
      await invokeHook(permissionHooks, "evaluate", 1, { ...permission, action: "read", effect: "allow" })

      const contextEvent = { agent: "not-the-orchestrator", system: [] as Array<{ type: string; text: string }> }
      await invokeHook(hooks, "context", 0, contextEvent)
      expect(contextEvent.system).toEqual([])

      const usageEvent: OpenCodeEvent = {
        id: "evt-usage-1",
        created: 1,
        type: "session.usage.updated",
        data: {
          sessionID: "dispatch",
          cost: 0.25,
          tokens: { input: 12, output: 8, reasoning: 2, cache: { read: 3, write: 1 } },
        },
      }
      publishEvent(usageEvent)
      publishEvent(usageEvent)
      await new Promise((resolve) => setTimeout(resolve, 10))

      const usageRequests = requests.filter(({ command, request }) => command === "ingest" && request.event_class === "model_usage")
      expect(usageRequests).toHaveLength(1)
      expect(usageRequests[0].request).toMatchObject({
        event_class: "model_usage",
        session_id: "dispatch",
        agent: "dev",
        work_unit_id: "unit-a",
        attributes: {
          usage_event_id: "evt-usage-1",
          input_tokens: 12,
          output_tokens: 8,
          reasoning_tokens: 2,
          cache_read_tokens: 3,
          cache_write_tokens: 1,
          cost_usd: 0.25,
        },
      })
      expect(usageRequests[0].request).not.toHaveProperty("attributes.message_id")

      await execute("vfs_discard", { author_key: "author-1", call_id: "discard-1" }, orchestrator)
      await execute("vfs_bind", { scope: ["src/a.go"] })
      await execute("vfs_bind", { author_key: "author-1", scope: ["src/a.go"] }, { sessionID: "verifier", agent: "verify" })
      expect((await execute("vfs_read", { path: "src/a.go", call_id: "verify-read-1" }, { sessionID: "verifier", agent: "verify" })).content).toBe("staged content")
      expect((await execute("vfs_verify", { author_key: "author-1", pass: false, finding: "missing assertion" }, { sessionID: "verifier", agent: "verify" })).content).toContain("Verdict: rejected")
      await execute("vfs_consolidate", { author_key: "author-1", checkpoint: "unit-a-verified" }, orchestrator)
    } finally {
      disposePlugin?.()
      runtime.Bun.spawn = originalSpawn
    }
  })
})

describe("type guards", () => {
  test("isToolInput / toolInput", () => {
    expect(isToolInput({})).toBe(true)
    expect(isToolInput({ a: 1 })).toBe(true)
    expect(isToolInput([])).toBe(false)
    expect(isToolInput(null)).toBe(false)
    expect(isToolInput("x")).toBe(false)
    expect(toolInput({ a: 1 })).toEqual({ a: 1 })
    expect(() => toolInput("x")).toThrow("tool input must be an object")
  })

  test("isResponseObject / isStringArray / isOptional / isString / isBoolean", () => {
    expect(isResponseObject({})).toBe(true)
    expect(isResponseObject([])).toBe(false)
    expect(isResponseObject(null)).toBe(false)
    expect(isStringArray(["a", "b"])).toBe(true)
    expect(isStringArray(["a", 1])).toBe(false)
    expect(isStringArray("a")).toBe(false)
    expect(isOptional(undefined, isString)).toBe(true)
    expect(isOptional("x", isString)).toBe(true)
    expect(isOptional(1, isString)).toBe(false)
    expect(isString("x")).toBe(true)
    expect(isString(1)).toBe(false)
    expect(isBoolean(true)).toBe(true)
    expect(isBoolean("true")).toBe(false)
  })

  test("orchestratorOnly", () => {
    const ORCHESTRATOR_ID = "__TAKT_ORCHESTRATOR_ID__"
    expect(() => orchestratorOnly({ agent: ORCHESTRATOR_ID }, "claim")).not.toThrow()
    expect(() => orchestratorOnly({ agent: "someone-else" }, "claim")).toThrow("claim belongs to the orchestrator")
    expect(() => orchestratorOnly({}, "claim")).toThrow("claim belongs to the orchestrator")
  })

  test("asString", () => {
    expect(asString("x", "fallback")).toBe("x")
    expect(asString(1, "fallback")).toBe("fallback")
    expect(asString(undefined, "fallback")).toBe("fallback")
  })
})

describe("responseObject", () => {
  test("passes an object through and rejects non-objects", () => {
    expect(responseObject({ a: 1 }, "vfs bind")).toEqual({ a: 1 })
    expect(() => responseObject("x", "vfs bind")).toThrow("takt-ai vfs bind returned a non-object response")
    expect(() => responseObject(null, "vfs bind")).toThrow("takt-ai vfs bind returned a non-object response")
    expect(() => responseObject([1], "vfs bind")).toThrow("takt-ai vfs bind returned a non-object response")
  })
})

describe("parseVFSShell", () => {
  test("accepts a well-formed shell decision with every optional field", () => {
    const raw = { decision: "ask", reason: "confirm", cwd: "/work", scratch: "/tmp/x", confirm: "run it?", capture: true, writable: ["a"], protected: ["b"], private: ["c"] }
    expect(parseVFSShell(raw)).toEqual({ decision: "ask", reason: "confirm", cwd: "/work", scratch: "/tmp/x", confirm: "run it?", capture: true, writable: ["a"], protected: ["b"], private: ["c"] })
  })
  test("accepts the minimal required shape", () => {
    expect(parseVFSShell({ decision: "deny", reason: "no" })).toEqual({ decision: "deny", reason: "no" })
  })
  test("rejects an invalid decision, a missing reason, or a wrongly typed optional field", () => {
    expect(parseVFSShell({ decision: "maybe", reason: "x" })).toBeUndefined()
    expect(parseVFSShell({ decision: "allow" })).toBeUndefined()
    expect(parseVFSShell({ decision: "allow", reason: "x", capture: "yes" })).toBeUndefined()
    expect(parseVFSShell({ decision: "allow", reason: "x", writable: ["a", 1] })).toBeUndefined()
    expect(parseVFSShell("not an object")).toBeUndefined()
  })
})

describe("parseClaim", () => {
  test("keeps a well-formed claim with its optional fields", () => {
    const raw = { key: "k", root_session_id: "r", work_unit_id: "u", agent_id: "a", target_instance: "t", pending: true, active: true, scope: ["x", 1], author_key: "auth" }
    expect(parseClaim(raw)).toEqual([{ key: "k", root_session_id: "r", work_unit_id: "u", agent_id: "a", target_instance: "t", pending: true, active: true, scope: ["x"], author_key: "auth" }])
  })
  test("drops anything missing a required field or not an object", () => {
    expect(parseClaim({ key: "k" })).toEqual([])
    expect(parseClaim("not an object")).toEqual([])
    expect(parseClaim(null)).toEqual([])
  })
})

describe("vfsResponse", () => {
  test("matches the real vfs_bind wire contract", () => {
    const { ok, ...rest } = contracts.vfs_bind
    expect(vfsResponse(rest, ok)).toEqual({ ok: true, key: "bind-opaque", attempt_id: "1", invariants_version: "inv1:abc" })
  })
  test("carries a shell decision and claims when present", () => {
    const parsed = { revision: 3, delta_hash: "h", shell: { decision: "allow", reason: "ok" }, claims: [{ key: "k", root_session_id: "r", work_unit_id: "u", agent_id: "a", target_instance: "t" }, "garbage"] }
    expect(vfsResponse(parsed, true)).toEqual({
      ok: true, revision: 3, delta_hash: "h",
      shell: { decision: "allow", reason: "ok" },
      claims: [{ key: "k", root_session_id: "r", work_unit_id: "u", agent_id: "a", target_instance: "t" }],
    })
  })
  test("omits every field the response does not set", () => {
    expect(vfsResponse({}, false)).toEqual({ ok: false })
  })
})

describe("gc coordination shapes", () => {
  const plan = { session_id: "s", cycle_id: "c", mandate_class: "complexity", delta: [], closure: ["a.go"], reachability: "codegraph" }
  const cycle = { phase: "collect", plan, scope: ["a.go"], sessions: { collector: "s1" }, report: null, started: "2024-01-01T00:00:00Z" }
  const stagedView = { revision: 1, delta_hash: "h", files: { "a.go": "content", "b.go": null } }
  const coordinator = { version: 1, units: 1, mutations: 0, cursor: 0, deferrals: 0, next_mandate: 1, requested: false, draining: false }

  test("isGCPlan / isGCCycle / isStagedView / isCoordinator accept well-formed shapes", () => {
    expect(isGCPlan(plan)).toBe(true)
    expect(isGCCycle(cycle)).toBe(true)
    expect(isStagedView(stagedView)).toBe(true)
    expect(isCoordinator(coordinator)).toBe(true)
    expect(isCoordinator({ ...coordinator, cycle })).toBe(true)
    expect(isCoordinator({ ...coordinator, history: [cycle] })).toBe(true)
  })

  test("isGCPlan / isGCCycle / isStagedView / isCoordinator reject malformed shapes", () => {
    expect(isGCPlan({ ...plan, closure: ["a.go", 1] })).toBe(false)
    expect(isGCCycle({ ...cycle, plan: { ...plan, session_id: 1 } })).toBe(false)
    expect(isStagedView({ ...stagedView, files: { "a.go": 1 } })).toBe(false)
    expect(isCoordinator({ ...coordinator, version: "1" })).toBe(false)
    expect(isCoordinator({ ...coordinator, cycle: { phase: "collect" } })).toBe(false)
  })

  test("gcCoordinateResponse dispatches by action", () => {
    expect(gcCoordinateResponse(plan, "prepare")).toEqual(plan)
    expect(gcCoordinateResponse({ notAPlan: true }, "prepare")).toBeUndefined()
    expect(gcCoordinateResponse(cycle, "findings")).toEqual(cycle)
    expect(gcCoordinateResponse(cycle, "investigate")).toEqual(cycle)
    expect(gcCoordinateResponse(cycle, "authorize")).toEqual(cycle)
    expect(gcCoordinateResponse(stagedView, "collected")).toEqual(stagedView)
    expect(gcCoordinateResponse(stagedView, "delta")).toEqual(stagedView)
    expect(gcCoordinateResponse(coordinator, "attach")).toEqual(coordinator)
    expect(gcCoordinateResponse({ not: "a coordinator" }, "attach")).toBeUndefined()
  })

  test("gcCoordinateResponse matches the real coordinator wire contract", () => {
    expect(isCoordinatorResponse(contracts.coordinator)).toBe(true)
    expect(isGCCycleResponse(contracts.coordinator.cycle)).toBe(true)
    expect(gcCoordinateResponse(contracts.coordinator, "attach")).toEqual(contracts.coordinator)
  })
})

describe("isHandoffEnvelope", () => {
  test("matches the real handoff and abort wire contracts", () => {
    expect(isHandoffEnvelope(contracts.handoff)).toBe(true)
    expect(isHandoffEnvelope(contracts.abort)).toBe(true)
  })
  test("rejects a malformed envelope", () => {
    expect(isHandoffEnvelope({ result: "Standard", additional_context: "x", extra_artifacts: [1], memory: [] })).toBe(false)
    expect(isHandoffEnvelope({ result: "Standard", additional_context: "x", extra_artifacts: [], memory: ["not a number"] })).toBe(false)
  })
})

describe("parseCoordinationResponse", () => {
  test("passes a null response through", () => {
    expect(parseCoordinationResponse(null, ["gc", "coordinate"], "prepare")).toBeNull()
  })
  test("recognizes a handoff envelope regardless of verb", () => {
    expect(parseCoordinationResponse(contracts.handoff, ["dispatch"], "handoff")).toEqual(contracts.handoff)
  })
  test("routes gc coordinate through gcCoordinateResponse", () => {
    const cycle = { phase: "collect", plan: { session_id: "s", cycle_id: "c", mandate_class: "complexity", delta: [], closure: [], reachability: "codegraph" }, scope: [], sessions: {}, report: null, started: "2024-01-01T00:00:00Z" }
    expect(parseCoordinationResponse(cycle, ["gc", "coordinate"], "findings")).toEqual(cycle)
  })
  test("treats an empty or ok dispatch switch/validate_results response as an acknowledgement", () => {
    expect(parseCoordinationResponse({}, ["dispatch"], "switch")).toBeNull()
    expect(parseCoordinationResponse({ ok: true }, ["dispatch"], "validate_results")).toBeNull()
  })
  test("accepts a coordinator response for a non-acknowledgement dispatch action", () => {
    expect(parseCoordinationResponse(contracts.coordinator, ["dispatch"], "handoff")).toEqual(contracts.coordinator)
  })
  test("rejects a response matching no known shape", () => {
    expect(() => parseCoordinationResponse({ garbage: true }, ["dispatch"], "handoff")).toThrow("dispatch handoff returned an unexpected response shape")
  })
  test("rejects a non-object response before shape matching", () => {
    expect(() => parseCoordinationResponse("not an object", ["vfs", "bind"], undefined)).toThrow("takt-ai vfs bind returned a non-object response")
  })
})
