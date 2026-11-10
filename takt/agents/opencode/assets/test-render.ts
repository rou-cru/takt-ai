// Renders the deploy-time placeholders of the plugins, so tests import them as deployed from their own path
// and coverage is reported against it.
import { plugin } from "bun"
import { readFileSync } from "node:fs"

const MEMORY_TOOLS = { record: "takt_memory_record", read: "takt_memory_read" }
const MEMORY_WIRE = { tool_prefix: "takt_memory_", record: "record", bind: "bind", describe: "describe", admin_path: "/admin" }
const CREW_ROSTER = ["takt", "dev", "qa", "tpm", "verify"]

plugin({
  name: "render-takt-plugins",
  setup(build) {
    build.onLoad({ filter: /takt-vfs\.ts$/ }, ({ path }) => ({
      contents: readFileSync(path, "utf8").replace(`"__TAKT_MEMORY_TOOLS__"`, JSON.stringify(MEMORY_TOOLS)),
      loader: "ts",
    }))
    build.onLoad({ filter: /takt-memory\.ts$/ }, ({ path }) => ({
      contents: readFileSync(path, "utf8")
        .replace(`"__TAKT_MEMORY_WIRE__"`, JSON.stringify(MEMORY_WIRE))
        .replace(`"__TAKT_CREW_ROSTER__"`, JSON.stringify(CREW_ROSTER))
        .replace(`"__TAKT_AI_BINARY__"`, JSON.stringify("takt-ai")),
      loader: "ts",
    }))
  },
})
