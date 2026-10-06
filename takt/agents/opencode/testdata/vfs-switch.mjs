import assert from "node:assert/strict"
import plugin from "./takt-vfs.ts"

// order records the true call sequence across session lifecycle and Go IPC,
// so identity threading can be checked by position, not just by presence.
const order = []
const calls = []
const coordinator = () => ({ version: 1, units: 0, mutations: 0, cursor: -1, deferrals: 0, next_mandate: 0, requested: false })
const idleEvents = []
let wakeEvents
const sessionIdle = (sessionID) => { idleEvents.push({ id: `idle-${sessionID}`, created: 1, type: "session.idle", data: { sessionID } }); const wake = wakeEvents; wakeEvents = undefined; wake?.() }
let respond = (_call) => undefined
function requestOf(call) {
  const idx = call.argv.indexOf("--request")
  return JSON.parse(call.argv[idx + 1])
}
// defaultAnswer is what takt-ai prints on success when no scenario overrides it.
function defaultAnswer(call) {
  if (call.verb === "gc" && call.sub === "coordinate") return coordinator()
  if (call.verb !== "dispatch") return { ok: true }
  const action = requestOf(call).action
  return action === "switch" || action === "validate_results" ? null : coordinator()
}
globalThis.Bun = {
  spawn(argv, options) {
    assert.equal(argv[0], "/test/takt-ai")
    if (argv[1] === "codegraph") return { stdout: "", stderr: "", exited: Promise.resolve(0) }
    const call = { verb: argv[1], sub: argv[2], argv, options }
    calls.push(call)
    if (argv[1] === "dispatch") order.push(`dispatch:${requestOf(call).action}`)
    const answer = () => respond(call) ?? { stdout: JSON.stringify(defaultAnswer(call)), stderr: "", code: 0 }
    return {
      stdin: { write(json) { call.stdin = JSON.parse(json) }, end() {} },
      get stdout() { return answer().stdout },
      get stderr() { return answer().stderr },
      get exited() { return Promise.resolve(answer().code) },
    }
  },
}

const sessions = new Map([["root", {}]])
let nextChild = 0
const hooks = {}
const tools = {}
const prompted = []
const interrupted = []
const synthetic = []
await plugin.setup({
  location: { directory: "/workspace" },
  storage: { async get() { return undefined }, async set() {} },
  session: {
    async get({ sessionID }) {
      const info = sessions.get(sessionID)
      if (!info) throw new Error(`unknown session ${sessionID}`)
      return info
    },
    hook: async () => ({ dispose() {} }),
    create: async (opts) => {
      const id = `child-${++nextChild}`
      sessions.set(id, { metadata: opts.metadata })
      order.push(`create:${id}`)
      return { id }
    },
    prompt: async ({ sessionID }) => { order.push(`prompt:${sessionID}`); prompted.push(sessionID) },
    interrupt: async ({ sessionID }) => { order.push(`interrupt:${sessionID}`); interrupted.push(sessionID) },
    synthetic: async (message) => { synthetic.push(message) },
  },
  permission: { hook: async () => () => {} },
  agent: { get: async () => ({ data: { permissions: [] } }) },
  shell: { hook: async () => () => {} },
  tool: {
    hook: async (name, callback) => { hooks[name] ??= []; hooks[name].push(callback); return { dispose() {} } },
    transform: async (callback) => {
      callback({ add: (definition) => { tools[definition.name] = definition }, namespace() {}, list: () => [], get: () => undefined, update() {}, remove() {} })
      return { dispose() {} }
    },
  },
  event: { subscribe: () => ({ [Symbol.asyncIterator]: () => ({ next: () => idleEvents.length ? Promise.resolve({ value: idleEvents.shift(), done: false }) : new Promise(resolve => { wakeEvents = () => resolve({ value: idleEvents.shift(), done: false }) }) }) }) },
})

const root = { sessionID: "root", agent: "takt" }

// 1a. A successful switch registers the created child with Go before ever
// prompting it: order, not just presence, proves the fix.
let mark = order.length
await tools.dispatch_switch.execute({ target_agent: "pm", objective: "obj" }, root)
let segment = order.slice(mark)
const createdChild = segment.find(e => e.startsWith("create:")).split(":")[1]
assert.deepEqual(segment, [`create:${createdChild}`, "dispatch:switch", `prompt:${createdChild}`])
assert.equal(requestOf(calls.findLast(c => c.verb === "dispatch")).child, createdChild)

// 1b. A registration failure stops the never-prompted child instead of
// leaving it dangling.
respond = (call) => call.verb === "dispatch" && requestOf(call).action === "switch"
  ? { stdout: "", stderr: "harness: the interlocutor interface is already held; no chaining", code: 1 } : undefined
mark = order.length
await assert.rejects(tools.dispatch_switch.execute({ target_agent: "pm", objective: "obj2" }, root), /no chaining/)
segment = order.slice(mark)
const failedChild = segment.find(e => e.startsWith("create:")).split(":")[1]
assert.deepEqual(segment, [`create:${failedChild}`, "dispatch:switch", `interrupt:${failedChild}`])
assert.equal(prompted.includes(failedChild), false, "a never-registered switch child was prompted")
assert.equal(interrupted.includes(failedChild), true)
respond = () => undefined

// 2. rootSession() resolves a switch-created session (no parentID) through
// its metadata.takt_switch fallback: only the root orchestrator going idle asks
// whether maintenance is due, so the switch-created child's idle never does.
sessions.set("switch-root-child", { metadata: { takt_switch: "root" } })
const gcCoordinateCalls = () => calls.filter(c => c.verb === "gc" && c.sub === "coordinate")
const ticks = () => gcCoordinateCalls().filter(c => requestOf(c).action === "tick")
sessionIdle("switch-root-child")
sessionIdle("root")
for (let i = 0; i < 100 && ticks().length === 0; i++) await new Promise(resolve => setTimeout(resolve, 10))
assert.equal(ticks().length, 1, "exactly the root session's idle must reach gc coordinate")
assert.equal(requestOf(ticks()[0]).session, "root", "rootSession() did not resolve the switch-created session's root")

// 3. A dispatch_handoff from that same switch-created child reaches the
// resolved root, not the child's own raw session id.
const holder = { sessionID: "switch-root-child", agent: "pm" }
const envelope = { result: "Standard", additional_context: "ctx", extra_artifacts: [], memory: [] }
respond = (call) => call.verb === "dispatch" && requestOf(call).action === "handoff"
  ? { stdout: JSON.stringify(envelope), stderr: "", code: 0 } : undefined
mark = calls.filter(c => c.verb === "dispatch").length
await tools.dispatch_handoff.execute({ result: "Standard", additional_context: "ctx", extra_artifacts: [], result_ids: [1] }, holder)
const handoff = requestOf(calls.filter(c => c.verb === "dispatch").at(mark))
assert.equal(handoff.action, "handoff")
assert.equal(handoff.session, "root", "dispatch_handoff sent the child's raw session, not its resolved root")
assert.equal(handoff.child, "switch-root-child")
assert.equal(synthetic.at(-1).sessionID, "root")
respond = () => undefined
