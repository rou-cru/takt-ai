import assert from "node:assert/strict"
import plugin from "./takt-vfs.ts"

let claimResponse = []
const storage = new Map()
const hooks = {}
const tools = {}
const claimsCalls = []
globalThis.Bun = {
  spawn(argv) {
    assert.equal(argv[0], "/test/takt-ai")
    const call = { argv }
    return {
      stdin: { write(json) { call.request = JSON.parse(json) }, end() {} },
      get stdout() {
        if (argv[2] === "claims") {
          claimsCalls.push(call.request)
          return JSON.stringify({ ok: true, claims: claimResponse })
        }
        return JSON.stringify({ ok: true })
      },
      stderr: "",
      exited: Promise.resolve(0),
    }
  },
}

await plugin.setup({
  location: { directory: "/workspace" },
  storage: { async get(key) { return storage.get(key) }, async set(key, value) { storage.set(key, value) } },
  session: {
    async get({ sessionID }) { return sessionID === "root" ? {} : { parentID: "root", title: "unit-1" } },
    hook: async (name, callback) => { hooks[name] ??= []; hooks[name].push(callback); return { dispose() {} } },
    create: async () => ({ id: "created" }), prompt: async () => {}, interrupt: async () => {},
  },
  agent: { async get({ agentID }) { return { id: agentID, permissions: agentID === "dev-none" ? [] : [
    { action: "vfs_read", resource: "*", effect: "allow" },
    { action: "vfs_write", resource: "*", effect: "allow" },
  ] } } },
  permission: { hook: async () => () => {} }, shell: { hook: async () => () => {} },
  tool: {
    hook: async () => ({ dispose() {} }),
    transform: async callback => {
      callback({
        add(definition) { tools[definition.name] = definition }, namespace() {}, list() { return [] },
        get() { return undefined }, update() {}, remove() {},
      })
      return { dispose() {} }
    },
  },
  event: { subscribe: () => ({ [Symbol.asyncIterator]: () => ({ next: () => new Promise(() => {}) }) }) },
})

const vfsNames = Object.keys(tools).filter(name => name.startsWith("vfs_"))
assert.ok(vfsNames.includes("vfs_read"), "fixture expects the registered VFS read tool")
const root = "root"
const matching = {
  key: "claim-1", root_session_id: root, work_unit_id: "unit-1", agent_id: "dev",
  target_instance: "dev", pending: true, active: false, scope: ["src/one.go"],
}

async function context(agent, claims) {
  claimResponse = claims
  const event = { agent, sessionID: agent === "dev-none" ? "child-none" : "child", tools: Object.fromEntries(vfsNames.map(name => [name, {}])), system: [] }
  for (const callback of hooks.context) await callback(event)
  return event
}

const byName = (a, b) => a.localeCompare(b)
const exact = await context("dev", [matching])
assert.deepEqual(Object.keys(exact.tools).sort(byName), ["vfs_read", "vfs_write"].sort(byName))
assert.equal(exact.system.length, 1)
assert.match(exact.system[0].text, /Work unit unit-1/)
assert.match(exact.system[0].text, /src\/one\.go/)
assert.doesNotMatch(exact.system[0].text, /verdict/)

// A verifier's gate claim carries the author key it judges and no scope.
const gate = await context("dev", [{ ...matching, scope: [], author_key: "author-1" }])
assert.equal(gate.system.length, 1)
assert.match(gate.system[0].text, /author_key author-1/)
assert.match(gate.system[0].text, /empty scope/)

const noClaim = await context("dev", [])
assert.deepEqual(Object.keys(noClaim.tools), [])
assert.deepEqual(noClaim.system, [])

for (const mismatch of [
  { ...matching, target_instance: "other" },
  { ...matching, work_unit_id: "unit-other" },
]) {
  const rejected = await context("dev", [mismatch])
  assert.deepEqual(Object.keys(rejected.tools), [])
  assert.deepEqual(rejected.system, [])
}

const noPermission = await context("dev-none", [{ ...matching, agent_id: "dev-none", target_instance: "dev-none" }])
assert.deepEqual(Object.keys(noPermission.tools), [])
assert.deepEqual(noPermission.system, [])
assert.ok(claimsCalls.length >= 4, "child contexts with permissions consult root claims")
