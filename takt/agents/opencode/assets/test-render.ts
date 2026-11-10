// Renders the deploy-time placeholders of takt-vfs.ts that hold structured values, so tests import the plugin as deployed.
import { plugin } from "bun"
import { readFileSync } from "node:fs"

const MEMORY_TOOLS = { record: "takt_memory_record", read: "takt_memory_read" }

plugin({
  name: "render-takt-vfs",
  setup(build) {
    build.onLoad({ filter: /takt-vfs\.ts$/ }, ({ path }) => ({
      contents: readFileSync(path, "utf8").replace(`"__TAKT_MEMORY_TOOLS__"`, JSON.stringify(MEMORY_TOOLS)),
      loader: "ts",
    }))
  },
})
