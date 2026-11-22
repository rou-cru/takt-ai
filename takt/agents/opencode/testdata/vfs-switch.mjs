import assert from "node:assert/strict"
import plugin from "./takt-vfs.ts"
import {historyFixture} from "./history-fixture.mjs"
const owner=historyFixture()
// The user accepts every transition the harness asks about.
globalThis.createPermission = async (r) => ({ id: r.id, effect: "allow" })

// order records the true call sequence across session lifecycle and Go IPC,
// so identity threading can be checked by position, not just by presence.
const order = []
const calls = []
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
  if (call.verb !== "dispatch") return { ok: true }
  const action = requestOf(call).action
  return action === "switch" || action === "validate_results" ? null : { ok: true }
}
globalThis.Bun = {
  spawn(argv, options) {
    assert.equal(argv[0], "/test/takt-ai")
    if (argv[1] === "codegraph") return { stdout: "", stderr: "", exited: Promise.resolve(0) }
    const call = { verb: argv[1], sub: argv[2], argv, options }
    calls.push(call)
    if (argv[1] === "dispatch" && !["resolve","execution","executions"].includes(requestOf(call).action)) order.push(`dispatch:${requestOf(call).action}`)
    let cached
    const answer=()=>{
      if(cached)return cached
      const custom=respond(call);if(custom?.code){ cached = custom; return cached }
      const fact=call.verb==="dispatch"?owner.answer("dispatch",requestOf(call).action,requestOf(call)):undefined
      cached = custom??(fact?{stdout:fact.out??"",stderr:fact.err??"",code:fact.code}:{stdout:JSON.stringify(defaultAnswer(call)),stderr:"",code:0})
      return cached
    }
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
let onPrompt=async()=>{}
const interrupted = []
const synthetic = []
await plugin.setup({
  location: { directory: "/workspace" },
  storage: { async get() { return undefined }, async set() {} },
  session: {
    context: async () => [],
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
    prompt: async ({ sessionID }) => { order.push(`prompt:${sessionID}`); prompted.push(sessionID);await onPrompt(sessionID) },
    wait: async () => {},
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
await tools.dispatch_switch.execute({ target_agent: "pm", objective: "obj", requirements: [] }, root)
let segment = order.slice(mark)
const createdChild = segment.find(e => e.startsWith("create:")).split(":")[1]
assert.deepEqual(segment, ["dispatch:validate_switch", `create:${createdChild}`, "dispatch:switch", `prompt:${createdChild}`])
assert.equal(requestOf(calls.findLast(c => c.verb === "dispatch" && requestOf(c).action === "switch")).child, createdChild)

// 1b. A registration failure stops the never-prompted child instead of
// leaving it dangling.
respond = (call) => call.verb === "dispatch" && requestOf(call).action === "switch"
  ? { stdout: "", stderr: "harness: the interlocutor interface is already held; no chaining", code: 1 } : undefined
mark = order.length
await assert.rejects(tools.dispatch_switch.execute({ target_agent: "pm", objective: "obj2", requirements: [] }, root), /no chaining/)
segment = order.slice(mark)
const failedChild = segment.find(e => e.startsWith("create:")).split(":")[1]
assert.deepEqual(segment, ["dispatch:validate_switch", `create:${failedChild}`, "dispatch:switch", `interrupt:${failedChild}`])
assert.equal(prompted.includes(failedChild), false, "a never-registered switch child was prompted")
assert.equal(interrupted.includes(failedChild), true)
respond = () => undefined

// 2. A switch-created session (no parentID) resolves its root through its
// persisted history link. No session going idle reaches GC.
sessions.set("switch-root-child", { metadata: { takt_switch: "not-authority" } })
owner.holders["switch-root-child"]="root"
const idledAt = calls.length
sessionIdle("switch-root-child")
sessionIdle("root")
// until polls cond every 10ms, at most tries times, without a loop of awaits,
// and reports whether cond held.
const until = async (cond, tries) => {
  if (cond()) return true
  if (tries === 0) return false
  await new Promise(resolve => setTimeout(resolve, 10))
  return until(cond, tries - 1)
}
// Each idle handler ends on resolving its session to no execution.
const resolved = (child) => calls.slice(idledAt).some(c => c.verb === "dispatch" && requestOf(c).action === "resolve" && requestOf(c).child === child)
assert.ok(await until(() => resolved("switch-root-child") && resolved("root"), 100), "an idle handler did not finish")
await new Promise(resolve => setTimeout(resolve, 20))
assert.equal(calls.some(c => c.verb === "gc"), false, "a session going idle reached GC")

// 3. A dispatch_handoff from that same switch-created child reaches the
// resolved root, not the child's own raw session id.
const holder = { sessionID: "switch-root-child", agent: "pm" }
const envelope = { result: "Standard", additional_context: "ctx", extra_artifacts: [], memory: [] }
respond = (call) => call.verb === "dispatch" && requestOf(call).action === "handoff"
  ? { stdout: JSON.stringify({ envelope, notice: "The interface is back from pm." }), stderr: "", code: 0 } : undefined
mark = calls.filter(c => c.verb === "dispatch").length
await tools.dispatch_handoff.execute({ result: "Standard", additional_context: "ctx", extra_artifacts: [], result_ids: [1] }, holder)
const handoff = requestOf(calls.filter(c => c.verb === "dispatch").slice(mark).find(c=>requestOf(c).action==="handoff"))
assert.equal(handoff.action, "handoff")
assert.equal(handoff.session, "root", "dispatch_handoff sent the child's raw session, not its resolved root")
assert.equal(handoff.child, "switch-root-child")
assert.equal(synthetic.at(-1).sessionID, "root")
respond = () => undefined

