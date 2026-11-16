import assert from "node:assert/strict"
import plugin from "./takt-vfs.ts"
import { historyFixture } from "./history-fixture.mjs"
const owner = historyFixture()
const claims = []

// Same mocking style as vfs-dispatch.mjs: only Bun.spawn is a double, the
// rendered artifact runs for real.
const calls = []
const coordinator = () => ({ version: 1, units: 0, mutations: 0, cursor: -1, deferrals: 0, next_mandate: 0, requested: false })
let respond = (_call) => undefined
globalThis.Bun = {
  spawn(argv, options) {
    assert.equal(argv[0], "/test/takt-ai")
    const call = { verb: argv[1], argv, options }
    calls.push(call)
    let cached
    const answer = () => {
      if (cached) return cached
      const custom = respond(call)
      if (custom?.code) { cached = custom; return cached }
      const fact = call.verb === "dispatch" ? owner.answer("dispatch",requestOf(call).action,requestOf(call),claims) : undefined
      cached = custom ?? (fact ? {stdout:fact.out??"",stderr:fact.err??"",code:fact.code} : {stdout:JSON.stringify(defaultAnswer(call)),stderr:"",code:0})
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
// defaultAnswer is what takt-ai prints on success when no scenario overrides it.
function defaultAnswer(call) {
  if (call.argv[1] === "gc" && call.argv[2] === "coordinate") return coordinator()
  if (call.argv[1] === "dispatch") {
    const action = requestOf(call).action
    return action === "validate_results" || action === "switch" ? null : coordinator()
  }
  if (call.argv[2] === "bind") return { ok: true, key: "k1", attempt_id: "1" }
  return { ok: true }
}
// requestOf is the coordination request a dispatch or gc call carried.
function requestOf(call) {
  const idx = call.argv.indexOf("--request")
  return JSON.parse(call.argv[idx + 1])
}

const scenario = process.env.TAKT_VFS_TEST_SCENARIO
const events = []
let wake
async function* stream() {
  while (true) {
    if (!events.length) await new Promise(resolve => { wake = resolve })
    while (events.length) yield events.shift()
  }
}
const nativeCalls = new Map()
// A verifier that is also a result producer judges an author bound earlier.
const author = { key: "author-1", agent: "dev", session: "root", dispatch: "dev-child", unit: "writer", revision: 1, deltaHash: "h1" }
const store = new Map(scenario === "dual_role" ? [["takt/vfs/bindings", [author]]] : [])
const titles = { "review-2": "review" }
const verifierGrants = ["vfs_bind", "vfs_read", "vfs_verify"].map(name => ({ action: name, resource: name, effect: "allow" }))
const tools = {}
const hooks = {}
const sessionHooks = {}
const promptedSessions = []
const waitedSessions = []
const idleSessions = new Set()
const idleWaiters = new Map()
let onWait = async sessionID => {
  if (!idleSessions.has(sessionID)) await new Promise(resolve => { idleWaiters.set(sessionID, resolve) })
}
await plugin.setup({
  location: { directory: "/workspace" },
  storage: { async get(key) { return store.get(key) }, async set(key, value) { store.set(key, value) } },
  session: {
    context: async () => [{type:"assistant",content:[...nativeCalls].map(([id,status])=>({type:"tool",name:"subagent",id,state:{status}}))}],
    // The host titles a child after its delegation's description.
    async get({ sessionID }) { return sessionID === "root" ? {} : { parentID: "root", title: titles[sessionID] ?? sessionID } },
    hook: async (name, callback) => { sessionHooks[name] ??= []; sessionHooks[name].push(callback); return { dispose() {} } },
    create: async () => ({ id: "lent" }),
    prompt: async ({ sessionID }) => { promptedSessions.push(sessionID) },
    wait: async ({ sessionID }) => { waitedSessions.push(sessionID); await onWait(sessionID) },
    synthetic: async () => {},
    interrupt: async () => {},
  },
  permission: { hook: async () => () => {} },
  agent: { get: async ({ agentID }) => ({ data: { permissions: agentID === "verify" ? verifierGrants : [] } }) },
  shell: { hook: async () => () => {} },
  tool: {
    hook: async (name, callback) => { hooks[name] ??= []; hooks[name].push(callback); return { dispose() {} } },
    transform: async (callback) => {
      callback({ add: (definition) => { tools[definition.name] = definition }, namespace() {}, list: () => [], get: () => undefined, update() {}, remove() {} })
      return { dispose() {} }
    },
  },
  event: { subscribe: () => ({ [Symbol.asyncIterator]: stream }) },
})

// delegate runs the subagent hooks of one phase for a delegation; extra adds
// fields to its input, and result is the host's reply the after hooks amend.
const delegate = async (phase, description, id, agent, status = "completed", extra = {}, result) => {
  if (phase === "execute.before" && description.trim()) await tools.dispatch_inputs.execute({ work_unit_id: description.trim(), result_ids: [] }, { sessionID: "root", agent: "takt" })
  const reply = result ?? { content: "Working", metadata: { sessionID: description, status: "running" } }
  const event = { tool: "subagent", sessionID: "root", agent: "takt", messageID: "m", id,
    input: { agent, description, prompt: "p", ...extra }, status, result: reply }
  nativeCalls.set(id, phase === "execute.before" ? "running" : status)
  for (const hook of hooks[phase]) await hook(event) // NOSONAR: hooks run in registration order
  return event
}
// dispatched lists the coordination requests sent so far.
const dispatched = () => calls.filter(c => c.verb === "dispatch").map(requestOf)

const spinUntil = async (ready) => {
  for (let i = 0; i < 100 && !ready(); i++) await new Promise(resolve => setTimeout(resolve, 2))
  assert.ok(ready(), "expected lifecycle observation never arrived")
}
const idle = async (sessionID) => {
  idleSessions.add(sessionID)
  idleWaiters.get(sessionID)?.()
  events.push({ id: `idle-${sessionID}`, created: 1, type: "session.idle", data: { sessionID } })
  wake?.()
  await spinUntil(() => dispatched().some(r => r.action === "finish" && owner.executions[r.dispatch]?.child === sessionID))
}
const deliver = (sessionID, ids, agent = "pm") => tools.deliver_result.execute({ result_ids: ids }, { sessionID, agent })
const finishes = () => dispatched().filter(r => r.action === "finish")
const launch = async (unit, id = unit, agent = "pm", extra = {}) => {
  await delegate("execute.before", unit, id, agent, "completed", extra)
  await delegate("execute.after", unit, id, agent)
  assert.equal(owner.executions[`root:${id}`].settled, undefined, "launch return settled active work")
}

if (scenario === "retry") {
  await launch("clean")
  await deliver("clean", [1])
  await idle("clean")
  await launch("retry")
  respond = call => call.verb === "dispatch" && requestOf(call).action === "deliver"
    ? { stdout: "", stderr: "handoff requires a memory entry ID", code: 1 } : undefined
  await assert.rejects(deliver("retry", []), /memory entry ID/)
  respond = () => undefined
  await deliver("retry", [2])
  await idle("retry")
  const byName = (a, b) => a.localeCompare(b)
  assert.deepEqual(Object.keys(finishes()[0]).sort(byName), Object.keys(finishes()[1]).sort(byName))
} else if (scenario === "async_delivery" || scenario === "async_missing" || scenario === "tpm") {
  const unit = "producer"
  const agent = scenario === "tpm" ? "tpm" : "pm"
  await launch(unit, unit, agent)
  assert.deepEqual(finishes(), [])
  if (scenario !== "async_missing") await deliver(unit, [2], agent)
  assert.deepEqual(finishes(), [], "delivery alone ended the child")
  await idle(unit)
  assert.deepEqual(owner.executions[`root:${unit}`].delivery ?? [], scenario === "async_missing" ? [] : [2])
} else if (scenario === "wait_error") {
  await launch("unreachable")
  onWait = async () => { throw new Error("host unavailable") }
  events.push({ id: "idle", created: 1, type: "session.idle", data: { sessionID: "unreachable" } }); wake?.()
  await spinUntil(() => waitedSessions.length > 0)
  await new Promise(resolve => setTimeout(resolve, 10))
  assert.deepEqual(finishes(), [], "transport failure is not termination")
  onWait = async () => {}
  await idle("unreachable")
} else if (scenario === "concurrent_delivery") {
  await launch("first")
  await launch("second")
  await deliver("second", [2])
  await deliver("first", [1])
  await idle("second")
  assert.deepEqual(finishes().map(r => r.event), ["second"])
  await idle("first")
  assert.deepEqual(finishes().map(r => r.event), ["second", "first"])
} else if (scenario === "repeated_callbacks") {
  await launch("repeat")
  await delegate("execute.after", "repeat", "repeat", "pm")
  await deliver("repeat", [3])
  await idle("repeat")
  await delegate("execute.after", "repeat", "repeat", "pm")
  await idle("repeat")
  assert.equal(finishes().length, 1)
} else if (scenario === "continued_child") {
  await launch("continued", "first")
  await deliver("continued", [1])
  await idle("continued")
  idleSessions.delete("continued")
  await launch("continued", "second", "pm", {sessionID:"continued"})
  await deliver("continued", [2])
  assert.deepEqual(owner.executions["root:first"].delivery, [1])
  assert.deepEqual(owner.executions["root:second"].delivery, [2])
} else if (scenario === "dual_role") {
  claims.push({ key: "author-1", root_session_id: "root", work_unit_id: "writer", agent_id: "dev", target_instance: "dev", active: true, scope: ["a.go"] })
  respond = call => {
    const reply = value => ({ stdout: JSON.stringify(value), stderr: "", code: 0 })
    if (call.argv[2] === "claims") return reply({ok:true,claims})
    if (call.argv[2] === "adopt-gates") {
      const gate = claims.find(c => c.agent_id === "verify" && c.pending)
      gate.pending = false; gate.active = true; gate.scope = ["a.go"]
      return reply({ok:true,gates:[{key:gate.key,author_key:"author-1",scope:gate.scope}]})
    }
  }
  await launch("review", "review", "verify", {author_keys:["author-1"]})
  const context = {agent:"verify",sessionID:"review",tools:{},system:[]}
  for (const callback of sessionHooks.context) await callback(context)
  assert.ok(context.system.some(part => part.text.includes("You judge the staged work")))
  assert.ok(context.system.some(part => part.text.includes("deliver_result")))
  await tools.vfs_verify.execute({author_key:"author-1",pass:true,finding:"reviewed"},{sessionID:"review",agent:"verify"})
  assert.deepEqual(owner.executions["root:review"].delivery ?? [], [], "verdict is not delivery")
  assert.deepEqual(finishes(), [], "verdict is not termination")
  await deliver("review", [55], "verify")
  await idle("review")
} else {
  throw new Error(`unknown scenario ${scenario}`)
}
assert.deepEqual(promptedSessions, [], "native launch must not nudge or reprompt the child")
