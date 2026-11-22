// Renders the deploy-time placeholders of the plugins, so tests import them as deployed from their own path
// and coverage is reported against it.
import { plugin } from "bun"
import { readdirSync, readFileSync } from "node:fs"
import { resolve } from "node:path"

const MEMORY_TOOLS = { record: "takt_memory_record", read: "takt_memory_read" }
const MEMORY_WIRE = { tool_prefix: "takt_memory_", record: "record", bind: "bind", describe: "describe", admin_path: "/admin" }
const CREW_ROSTER = ["takt", "dev", "qa", "tpm", "verify"]

type PromptAssetFile = {
  entries?: Array<{ id: string; owner: string; text: string }>
  tools?: Array<{ name: string; owner: string; description?: string; params?: Record<string, string> }>
}

// loadPromptBundles reads the single canonical YAML registry directly, so tests
// exercise the exact templates deployed into plugins without duplicating them.
function loadPromptBundles(): Record<string, Record<string, string>> {
  const dir = resolve(import.meta.dir, "../../../prompts/assets")
  const bundles: Record<string, Record<string, string>> = {}
  const add = (owner: string, key: string, value: string) => {
    bundles[owner] ??= {}
    bundles[owner][key] = value
  }
  for (const name of readdirSync(dir).sort()) {
    if (!name.endsWith(".yaml")) continue
    const doc = Bun.YAML.parse(readFileSync(resolve(dir, name), "utf8")) as PromptAssetFile
    for (const entry of doc.entries ?? []) add(entry.owner, entry.id, entry.text)
    for (const tool of doc.tools ?? []) {
      if (tool.description) add(tool.owner, `tool.${tool.name}.description`, tool.description)
      for (const [param, desc] of Object.entries(tool.params ?? {})) {
        add(tool.owner, `tool.${tool.name}.param.${param}`, desc)
      }
    }
  }
  return bundles
}

const PROMPTS = loadPromptBundles()

plugin({
  name: "render-takt-plugins",
  setup(build) {
    build.onLoad({ filter: /takt-vfs\.ts$/ }, ({ path }) => ({
      contents: readFileSync(path, "utf8")
        .replace(`"__TAKT_MEMORY_TOOLS__"`, JSON.stringify(MEMORY_TOOLS))
        .replace(`"__TAKT_PROMPTS__"`, JSON.stringify(PROMPTS["plugin/takt-vfs"] ?? {})),
      loader: "ts",
    }))
    build.onLoad({ filter: /takt-memory\.ts$/ }, ({ path }) => ({
      contents: readFileSync(path, "utf8")
        .replace(`"__TAKT_MEMORY_WIRE__"`, JSON.stringify(MEMORY_WIRE))
        .replace(`"__TAKT_CREW_ROSTER__"`, JSON.stringify(CREW_ROSTER))
        .replace(`"__TAKT_AI_BINARY__"`, JSON.stringify("takt-ai"))
        .replace(`"__TAKT_PROMPTS__"`, JSON.stringify(PROMPTS["plugin/takt-memory"] ?? {})),
      loader: "ts",
    }))
    build.onLoad({ filter: /takt-dag\.tsx$/ }, ({ path }) => ({
      contents: readFileSync(path, "utf8")
        .replace(`"__TAKT_PROMPTS__"`, JSON.stringify(PROMPTS["plugin/takt-dag"] ?? {})),
      loader: "tsx",
    }))
  },
})
