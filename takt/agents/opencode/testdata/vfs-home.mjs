import assert from "node:assert/strict"
import plugin from "./takt-vfs.ts"

const spawns = []
const tools = {}
globalThis.Bun = {
  spawn(argv, options) {
    spawns.push({ argv, options })
    return { stdin: { write() {}, end() {} }, stdout: "{}", stderr: "", exited: Promise.resolve(0) }
  },
}
const register = async () => ({ dispose() {} })

await plugin.setup({
  location: { directory: process.env.HOME },
  storage: { async get() {}, async set() {} },
  session: { hook: register, async get() { return {} } },
  permission: { hook: register },
  shell: { hook: register },
  tool: {
    hook: register,
    async transform(callback) {
      callback({ add: definition => { tools[definition.name] = definition }, namespace() {}, list: () => [], get: () => undefined, update() {}, remove() {} })
      return { dispose() {} }
    },
  },
  event: { subscribe: () => ({ [Symbol.asyncIterator]: () => ({ next: () => new Promise(() => {}) }) }) },
})

assert.equal(spawns.some(({ argv }) => argv[1] === "codegraph"), false, "home startup must not initialize CodeGraph")
assert.ok(tools.vfs_read, "the VFS plugin should remain available")
assert.ok(tools.gc_findings, "GC tools stay registered but report their unavailable prerequisite")
assert.throws(() => tools.gc_findings.execute({}, { sessionID: "root", agent: "takt" }), /CodeGraph is unavailable.*home directory/)
