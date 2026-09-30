import { describe, expect, mock, test } from "bun:test"
import { readFileSync } from "node:fs"
import { resolve } from "node:path"
import memoryPlugin, { closeResult, isObject, recordResult } from "./takt-memory"

const contracts = JSON.parse(readFileSync(resolve(import.meta.dir, "../testdata/contracts.json"), "utf8"))
type MemoryTool = { name: string; execute: (input: unknown, context: never) => Promise<{ content: string }> }

describe("plugin setup", () => {
  test("registers memory tools and sends valid requests to takt-ai", async () => {
    const runtime = globalThis as unknown as { Bun: { spawn: (...args: never[]) => unknown } }
    const originalSpawn = runtime.Bun.spawn
    const tools = new Map<string, { execute: (input: unknown, context: never) => Promise<{ content: string }> }>()
    const requests: Array<{ command: string; request: Record<string, unknown> }> = []
    runtime.Bun.spawn = ((argv: string[]) => {
      let request: Record<string, unknown> = {}
      const command = argv[2]
      return {
        stdin: { write(value: string) { request = JSON.parse(value) }, end() {} },
        get stdout() {
          requests.push({ command, request })
          const result = command === "record" ? { id: 42, deduplicated: false } : { end_anchor_id: 43, entries: 1 }
          return JSON.stringify({ ok: true, result })
        },
        stderr: "",
        exited: Promise.resolve(0),
      }
    }) as typeof originalSpawn

    try {
      await memoryPlugin.setup({
        location: { directory: "/workspace" },
        session: { async get({ sessionID }: { sessionID: string }) { return { id: sessionID } } },
        tool: { async transform(callback: (editor: { add: (definition: MemoryTool) => void }) => void) {
          callback({ add(definition) { tools.set(definition.name, definition) } })
          return { dispose() {} }
        } },
        event: { subscribe() { return { [Symbol.asyncIterator]() { return { next: () => new Promise<never>(() => {}) } } } } },
      } as never)

      const context = { sessionID: "root", agent: "takt", messageID: "message", id: "tool-call" } as never
      expect((await tools.get("memory_record")!.execute({ nature: "observation", scope: "project", title: "title", content: "content" }, context)).content).toBe("Recorded #42")
      expect((await tools.get("memory_continue_session")!.execute({ previous_session_id: "prior" }, context)).content).toBe("Continues prior")
      expect((await tools.get("memory_close_session")!.execute({ objective: "objective", state: "done" }, context)).content).toBe("Closed session as #43 with 1 entries")
      expect(requests.map(({ command }) => command)).toEqual(["record", "continue", "close"])
      expect(requests[0].request).toMatchObject({ author: "takt", session: "root", directory: "/workspace", title: "title" })
    } finally {
      runtime.Bun.spawn = originalSpawn
    }
  })
})

describe("isObject", () => {
  test("accepts plain objects only", () => {
    expect(isObject({})).toBe(true)
    expect(isObject([])).toBe(false)
    expect(isObject(null)).toBe(false)
    expect(isObject("x")).toBe(false)
  })
})

describe("recordResult", () => {
  test("matches the real memory_record wire contract", () => {
    expect(recordResult(contracts.memory_record.result)).toEqual({ id: 42, deduplicated: false })
  })
  test("rejects a non-integer, non-positive, or wrongly typed id", () => {
    expect(() => recordResult({ id: 0, deduplicated: false })).toThrow("takt-ai memory record returned an invalid result")
    expect(() => recordResult({ id: 1.5, deduplicated: false })).toThrow("takt-ai memory record returned an invalid result")
    expect(() => recordResult({ id: "1", deduplicated: false })).toThrow("takt-ai memory record returned an invalid result")
    expect(() => recordResult({ id: 1, deduplicated: "no" })).toThrow("takt-ai memory record returned an invalid result")
    expect(() => recordResult(null)).toThrow("takt-ai memory record returned an invalid result")
  })
})

describe("closeResult", () => {
  test("matches the real memory_close wire contract", () => {
    expect(closeResult(contracts.memory_close.result)).toEqual({ end_anchor_id: 43, entries: 2 })
  })
  test("accepts zero entries but rejects a negative count or a non-positive anchor", () => {
    expect(closeResult({ end_anchor_id: 1, entries: 0 })).toEqual({ end_anchor_id: 1, entries: 0 })
    expect(() => closeResult({ end_anchor_id: 1, entries: -1 })).toThrow("takt-ai memory close returned an invalid result")
    expect(() => closeResult({ end_anchor_id: 0, entries: 1 })).toThrow("takt-ai memory close returned an invalid result")
    expect(() => closeResult({ end_anchor_id: 1.5, entries: 1 })).toThrow("takt-ai memory close returned an invalid result")
  })
})

// The confirmation channel is the local OpenCode service; these tests replace
// its discovery and client so no network or real service is ever touched.
type PermissionRequest = { sessionID: string; action: string; resources: string[]; metadata: Record<string, unknown>; agent: string; source: unknown }
type Effect = "allow" | "deny" | "ask"
type Reply = { out?: string; err?: string; code?: number }
type MemoryEvent = { type: string; data: Record<string, string> }

const REPLY_SETTLE_ATTEMPTS = 50
const context = { sessionID: "root", agent: "takt", messageID: "message", id: "tool-call" }
const sleep = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms))
const until = async (ready: () => boolean, attempts = REPLY_SETTLE_ATTEMPTS): Promise<void> => {
  if (ready() || attempts === 0) return
  await sleep(1)
  return until(ready, attempts - 1)
}

// startMemory wires the plugin to a scripted takt-ai, a scripted session tree,
// a scripted permission service and a controllable event stream.
async function startMemory(options: {
  respond?: (command: string, request: Record<string, unknown>) => Reply
  sessions?: Record<string, { parentID?: string; metadata?: Record<string, unknown> }>
  effects?: Effect[]
  endpoint?: { url: string } | undefined
} = {}) {
  const runtime = globalThis as unknown as { Bun: { spawn: (...args: never[]) => unknown } }
  const originalSpawn = runtime.Bun.spawn
  const tools = new Map<string, MemoryTool>()
  const requests: Array<{ command: string; request: Record<string, unknown> }> = []
  const created: PermissionRequest[] = []
  const events: MemoryEvent[] = []
  const effects = [...(options.effects ?? [])]
  let wake: (() => void) | undefined
  let closed = false
  let discoveries = 0
  const endpoint = "endpoint" in options ? options.endpoint : { url: "http://service.invalid" }
  mock.module("@opencode/client/service", () => ({
    Service: { discover: async () => { discoveries++; return endpoint }, headers: () => ({}) },
  }))
  mock.module("@opencode/client", () => ({
    OpenCode: { make: () => ({ permission: { create: async (request: PermissionRequest) => {
      created.push(request)
      return { effect: effects.shift() ?? "allow", id: `request-${created.length}` }
    } } }) },
  }))
  runtime.Bun.spawn = ((argv: string[]) => {
    let request: Record<string, unknown> = {}
    const command = argv[2]
    return {
      stdin: { write(value: string) { request = JSON.parse(value) }, end() {} },
      get stdout() {
        requests.push({ command, request })
        const reply = options.respond?.(command, request)
        if (reply) return reply.out ?? ""
        return JSON.stringify({ ok: true, result: command === "close" ? { end_anchor_id: 9, entries: 0 } : { id: 7, deduplicated: false } })
      },
      get stderr() { return options.respond?.(command, request)?.err ?? "" },
      exited: Promise.resolve(options.respond?.(command, request)?.code ?? 0),
    }
  }) as typeof originalSpawn
  async function* stream(): AsyncGenerator<MemoryEvent> {
    while (!closed) {
      while (events.length === 0 && !closed) await new Promise<void>((resolve) => { wake = resolve })
      const event = events.shift()
      if (event) yield event
    }
  }
  const cleanup = await memoryPlugin.setup({
    location: { directory: "/workspace" },
    session: { async get({ sessionID }: { sessionID: string }) { return { id: sessionID, ...options.sessions?.[sessionID] } } },
    tool: { async transform(callback: (editor: { add: (definition: MemoryTool) => void }) => void) {
      callback({ add(definition) { tools.set(definition.name, definition) } })
      return { dispose() {} }
    } },
    event: { subscribe({ signal }: { signal: AbortSignal }) {
      signal.addEventListener("abort", () => { closed = true; wake?.() }, { once: true })
      return { [Symbol.asyncIterator]: stream }
    } },
  } as never)
  return {
    requests, created, discoveries: () => discoveries,
    run: (name: string, input: unknown, ctx = context) => tools.get(name)!.execute(input, ctx as never),
    publish(event: MemoryEvent) { events.push(event); wake?.() },
    stop() { if (typeof cleanup === "function") cleanup(); runtime.Bun.spawn = originalSpawn },
  }
}

const personalRecord = { nature: "decision", scope: "personal", title: "prefers tabs", content: "always tabs" }

describe("confirmation flow", () => {
  test("a personal record asks for approval with the entry as resource and records it confirmed", async () => {
    const memory = await startMemory()
    try {
      expect((await memory.run("memory_record", personalRecord)).content).toBe("Recorded #7")
      expect(memory.created).toEqual([{
        sessionID: "root", action: "memory_personal", resources: ["prefers tabs"],
        metadata: { title: "prefers tabs", content: "always tabs" }, agent: "takt",
        source: { type: "tool", messageID: "message", id: "tool-call" },
      }])
      expect(memory.requests[0].request).toMatchObject({ scope: "personal", confirmed: true, user_order: false })
    } finally { memory.stop() }
  })

  test("a user-ordered project record asks its own question and records the order", async () => {
    const memory = await startMemory()
    try {
      await memory.run("memory_record", { nature: "decision", scope: "project", title: "ship it", content: "c", user_order: true })
      expect(memory.created).toHaveLength(1)
      expect(memory.created[0]).toMatchObject({ action: "memory_user_order", metadata: { question: "Did you order recording this?" } })
      expect(memory.requests[0].request).toMatchObject({ confirmed: false, user_order: true })
    } finally { memory.stop() }
  })

  test("a personal user-ordered record is confirmed twice over one discovered client", async () => {
    const memory = await startMemory()
    try {
      await memory.run("memory_record", { ...personalRecord, user_order: true })
      expect(memory.created.map(({ action }) => action)).toEqual(["memory_personal", "memory_user_order"])
      expect(memory.discoveries()).toBe(1)
    } finally { memory.stop() }
  })

  test("a denial fails the call and nothing is recorded", async () => {
    const memory = await startMemory({ effects: ["deny"] })
    try {
      await expect(memory.run("memory_record", personalRecord)).rejects.toThrow("Permission denied: memory_personal")
      expect(memory.requests).toEqual([])
    } finally { memory.stop() }
  })

  test("an unreachable service fails the call before anything is recorded", async () => {
    const memory = await startMemory({ endpoint: undefined })
    try {
      await expect(memory.run("memory_record", personalRecord)).rejects.toThrow("cannot reach the local OpenCode service to ask for confirmation")
      expect(memory.requests).toEqual([])
    } finally { memory.stop() }
  })

  test("an asked confirmation parks until its reply event, then honors the answer", async () => {
    const memory = await startMemory({ effects: ["ask", "ask"] })
    try {
      const approved = memory.run("memory_record", personalRecord)
      await until(() => memory.created.length === 1)
      await sleep(1)
      memory.publish({ type: "permission.replied", data: { requestID: "request-1", reply: "once" } })
      expect((await approved).content).toBe("Recorded #7")

      const rejected = memory.run("memory_record", personalRecord)
      const outcome = rejected.then(() => "recorded", (error: Error) => error.message)
      await until(() => memory.created.length === 2)
      await sleep(1)
      memory.publish({ type: "permission.replied", data: { requestID: "request-2", reply: "reject" } })
      expect(await outcome).toBe("Permission denied: memory_personal")
    } finally { memory.stop() }
  })
})

describe("root session resolution", () => {
  test("follows parents and the takt_switch metadata up to the root", async () => {
    const memory = await startMemory({ sessions: { leaf: { parentID: "middle" }, middle: { metadata: { takt_switch: "origin" } } } })
    try {
      await memory.run("memory_continue_session", { previous_session_id: "prior" }, { ...context, sessionID: "leaf" })
      expect(memory.requests[0].request).toMatchObject({ session: "origin", previous_session: "prior" })
    } finally { memory.stop() }
  })

  test("a parent cycle is refused", async () => {
    const memory = await startMemory({ sessions: { a: { parentID: "b" }, b: { parentID: "a" } } })
    try {
      await expect(memory.run("memory_continue_session", { previous_session_id: "prior" }, { ...context, sessionID: "a" })).rejects.toThrow("session a has a parent cycle")
      expect(memory.requests).toEqual([])
    } finally { memory.stop() }
  })
})

describe("harness errors", () => {
  test("each tool refuses a non-object input", async () => {
    const memory = await startMemory()
    try {
      await Promise.all(["memory_record", "memory_continue_session", "memory_close_session"].map((name) =>
        expect(memory.run(name, "text")).rejects.toThrow(`${name} input must be an object`)))
    } finally { memory.stop() }
  })

  test("a deduplicated record is reported as already recorded", async () => {
    const memory = await startMemory({ respond: () => ({ out: JSON.stringify({ ok: true, result: { id: 5, deduplicated: true } }) }) })
    try {
      expect((await memory.run("memory_record", { nature: "observation", scope: "project", title: "t", content: "c" })).content).toBe("Already recorded #5")
    } finally { memory.stop() }
  })

  test("the reported error, or a generic failure, surfaces when takt-ai answers not ok", async () => {
    const withError = await startMemory({ respond: () => ({ out: JSON.stringify({ ok: false, error: "store locked" }), code: 1 }) })
    try {
      await expect(withError.run("memory_close_session", { objective: "o", state: "s" })).rejects.toThrow("store locked")
    } finally { withError.stop() }
    const bare = await startMemory({ respond: () => ({ out: JSON.stringify({ ok: false }), code: 1 }) })
    try {
      await expect(bare.run("memory_continue_session", { previous_session_id: "prior" })).rejects.toThrow("takt-ai memory continue failed")
    } finally { bare.stop() }
  })

  test("an ok flag that disagrees with the exit code is an invalid response", async () => {
    const memory = await startMemory({ respond: () => ({ out: JSON.stringify({ ok: true, result: {} }), code: 1 }) })
    try {
      await expect(memory.run("memory_continue_session", { previous_session_id: "prior" })).rejects.toThrow("takt-ai memory continue returned an invalid response")
    } finally { memory.stop() }
  })

  test("unparseable output reports the exit code with stdout, or stderr when stdout is empty", async () => {
    const withOutput = await startMemory({ respond: () => ({ out: "not json", code: 2 }) })
    try {
      await expect(withOutput.run("memory_continue_session", { previous_session_id: "prior" })).rejects.toThrow("takt-ai memory continue exited 2: not json")
    } finally { withOutput.stop() }
    const withStderr = await startMemory({ respond: () => ({ out: "", err: "panic: boom", code: 3 }) })
    try {
      await expect(withStderr.run("memory_continue_session", { previous_session_id: "prior" })).rejects.toThrow("takt-ai memory continue exited 3: panic: boom")
    } finally { withStderr.stop() }
  })
})

describe("session deletion", () => {
  test("a deleted root session that recorded is closed with the fallback, once", async () => {
    const memory = await startMemory()
    try {
      await memory.run("memory_continue_session", { previous_session_id: "prior" })
      memory.publish({ type: "session.deleted", data: { sessionID: "root" } })
      memory.publish({ type: "session.deleted", data: { sessionID: "root" } })
      await until(() => memory.requests.some(({ command }) => command === "close"))
      await sleep(5)
      const closes = memory.requests.filter(({ command }) => command === "close")
      expect(closes).toHaveLength(1)
      expect(closes[0].request).toEqual({ author: "takt", session: "root", directory: "/workspace", fallback: true })
    } finally { memory.stop() }
  })

  test("a session that was closed explicitly or never recorded triggers no fallback", async () => {
    const memory = await startMemory()
    try {
      await memory.run("memory_close_session", { objective: "o", state: "s" })
      memory.publish({ type: "session.deleted", data: { sessionID: "root" } })
      memory.publish({ type: "session.deleted", data: { sessionID: "stranger" } })
      memory.publish({ type: "session.created", data: { sessionID: "root" } })
      await sleep(10)
      expect(memory.requests.filter(({ command }) => command === "close")).toHaveLength(1)
    } finally { memory.stop() }
  })
})
