// Takt VFS — managed by takt-ai; edits are overwritten on sync.
// The harness, not the model, supplies author, session and directory. Every
// write goes through the durable VFS core: staging first, consolidation only
// after an independent verifier's verdict. One private store per workspace.
import { randomUUID } from "node:crypto"
import { mkdtemp, realpath, rm, writeFile } from "node:fs/promises"
import { homedir, tmpdir } from "node:os"
import type { OpenCode, OpenCodeEvent } from "@opencode/client"
import { join, resolve } from "node:path"
import { Plugin } from "@opencode/plugin"

// Short IPC has a transport deadline, never a deadline on a human decision.
// Drain both pipes together and terminate only the process this call created.
export async function ipcOutput(proc: ReturnType<typeof Bun.spawn>, label: string, timeout = 60_000): Promise<[string, string, number]> {
 let timer: ReturnType<typeof setTimeout> | undefined
 const output = Promise.all([new Response(proc.stdout as BodyInit).text(), new Response(proc.stderr as BodyInit).text(), proc.exited])
 if (!timeout) return output
 try {
  return await Promise.race([output, new Promise<never>((_, reject) => {
   timer = setTimeout(() => {
    try { process.kill(-proc.pid, "SIGKILL") }
    catch { try { proc.kill("SIGKILL") } catch { /* Already exited. */ } }
    reject(new Error(`${label} exceeded ${timeout / 1000}s; the owned IPC process was cancelled; inspect its recorded disposition before repeating the operation`))
   }, timeout)
  })])
 } finally { if (timer !== undefined) clearTimeout(timer) }
}

const TAKT_AI = "__TAKT_AI_BINARY__"
const ORCHESTRATOR_ID = "__TAKT_ORCHESTRATOR_ID__"
// Prompt templates owned by this plugin, rendered from takt/prompts/assets/*.yaml.
const PROMPTS: Record<string, string> = "__TAKT_PROMPTS__" as unknown as Record<string, string>
function renderPrompt(id: string, vars: Record<string, string> = {}): string {
  const template = PROMPTS[id]
  if (template === undefined) throw new Error(`missing Takt prompt template: ${id}`)
  return template.replace(/\{\{([a-z0-9_]+)\}\}/g, (_, key: string) => {
    if (!(key in vars)) throw new Error(`missing variable ${key} for Takt prompt ${id}`)
    return vars[key]
  })
}
const toolDesc = (name: string, vars?: Record<string, string>) => renderPrompt(`tool.${name}.description`, vars)
const paramDesc = (name: string, param: string, vars?: Record<string, string>) => renderPrompt(`tool.${name}.param.${param}`, vars)
const IPC_VERSION = 5
// HOST_EVENT_STREAM names the sensor stream whose loss leaves executions unmonitored.
const HOST_EVENT_STREAM = "host_events"
// STREAM_INACTIVITY_MS is the silence of the open host event stream after which
// one "inactive" observation is made. It is an observation, never an enforcement
// window or budget: silence proves neither closure nor failure of any agent.
export const STREAM_INACTIVITY_MS = 120_000
// INSPECT_PAGE_LIMIT is the largest page takt-ai vfs journal and history serve.
const INSPECT_PAGE_LIMIT = 500

async function sameDirectory(left: string, right: string): Promise<boolean> {
  // canonical resolves symlinks, falling back to the plain path when it cannot.
  const canonical = (path: string) => realpath(path).catch(() => resolve(path))
  const [leftPath, rightPath] = await Promise.all([canonical(left), canonical(right)])
  return leftPath === rightPath
}

type ToolInput = Record<string, unknown>
export function isToolInput(value: unknown): value is ToolInput {
  return typeof value === "object" && value !== null && !Array.isArray(value)
}
export function toolInput<T extends ToolInput = ToolInput>(value: unknown): T {
  if (!isToolInput(value)) throw new Error("tool input must be an object")
  return value as T
}

type VFSShellResponse = { decision: "allow" | "ask" | "deny"; reason: string; writable?: string[]; scratch?: string; protected?: string[]; private?: string[]; unreadable?: string[]; readable?: string[]; allowedDomains?: string[]; capture?: boolean; cwd?: string; confirm?: string }
// AdoptedGate is one gate the core adopted for the calling host session.
type AdoptedGate = { key: string; author_key: string; scope: string[]; error?: string }
type Repair = { kind: "manifest"; id: string; author: string; paths: string[]; state: string; preserve: boolean; reason: string }
type VFSResponse = {
 repair?: Repair
  ok: boolean; key?: string; content?: string; revision?: number; delta_hash?: string
  error?: string; attempt_id?: string; invariants_version?: string
  inspections?: Array<{call_id:string;session_id:string;caller_session:string;caller_agent:string;tool_call_id?:string}>
  shell?: VFSShellResponse; claims?: OwnershipClaim[]; outdated_paths?: string[]; gates?: AdoptedGate[]
}
// The IR-22 envelope: exactly these four fields.
type MemoryIDs = number[]
type HandoffEnvelope = { result: string; additional_context: string; extra_artifacts: string[]; memory: MemoryIDs }
// What the core delivers when the interface comes back: the envelope, with the
// declared document's check and the ended session beside it, never inside.
type InterlocutorReturn = { envelope: HandoffEnvelope; child?: string; root?: string; notice: string }
type NotHeld = { aborted: false }
type Inspection = NonNullable<VFSResponse["inspections"]>[number]
type Delegation = { dispatch: string; unit: string; root: string; attempt: string; agent: string; caller: string; child?: string; admitted_at: number; inputs: number[]; undelivered?: string[]; delivery?: number[]; terminated?: boolean; settled?: boolean; owner?: string; model?: string; substitute?: boolean; identity?: string[] }
// SEAT_UNPROVEN marks the core's refusal of an autonomous seat reassignment to
// a model this session has not seen complete work; the user may approve it.
const SEAT_UNPROVEN = "seat/unproven-model"
// modelName is the native model reference, including a selected variant.
export function modelName(value: unknown): string | undefined {
  if (typeof value === "string" && value) return value
  if (!isResponseObject(value) || typeof value.providerID !== "string" || typeof value.id !== "string") return undefined
  const variant = typeof value.variant === "string" && value.variant ? `#${value.variant}` : ""
  return `${value.providerID}/${value.id}${variant}`
}
// withoutIdentity removes a substitute's PERSONA and SOUL texts from its
// composed system prompt: it holds the seat without its identity or voice.
export function withoutIdentity<Part extends { type: string; text: string }>(system: Part[], identity: string[]): Part[] {
  return system.map(part => ({ ...part, text: identity.reduce((text, block) => text.split(block).join(""), part.text) }))
}
type DispatchResponse = InterlocutorReturn | NotHeld | Record<string, unknown> | null
const delegationOf = (value: unknown): Delegation | undefined =>
  value && isResponseObject(value) && typeof value.dispatch === "string" && typeof value.attempt === "string" && typeof value.root === "string" && typeof value.unit === "string" && typeof value.agent === "string" ? value as Delegation : undefined
export function isMemoryIDs(value: unknown): value is MemoryIDs {
  return Array.isArray(value) && value.every(item => typeof item === "number")
}
export function isHandoffEnvelope(value: unknown): value is HandoffEnvelope {
  return isResponseObject(value) && Object.keys(value).length === 4 && typeof value.result === "string" && typeof value.additional_context === "string" &&
    isStringArray(value.extra_artifacts) && isMemoryIDs(value.memory)
}
export function isInterlocutorReturn(value: Record<string, unknown>): value is InterlocutorReturn {
  return isHandoffEnvelope(value.envelope) && isString(value.notice) && isOptional(value.child, isString) && isOptional(value.root, isString)
}
type OwnershipClaim = { key?: string; author_key?: string; root_session_id: string; work_unit_id: string; agent_id: string; target_instance: string; pending?: boolean; active?: boolean; staged?: boolean; scope?: string[]; reads?: string[] }
export function isResponseObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value)
}
export function isStringArray(value: unknown): value is string[] {
  return Array.isArray(value) && value.every(item => typeof item === "string")
}
// isOptional accepts an absent field or one that passes check.
export function isOptional<T>(value: unknown, check: (value: unknown) => value is T): value is T | undefined {
  return value === undefined || check(value)
}
export const isString = (value: unknown): value is string => typeof value === "string"
export const isBoolean = (value: unknown): value is boolean => typeof value === "boolean"
export function parseVFSShell(value: unknown): VFSShellResponse | undefined {
  if (!isResponseObject(value) || (value.decision !== "allow" && value.decision !== "ask" && value.decision !== "deny") ||
      !isString(value.reason) || !isOptional(value.cwd, isString) || !isOptional(value.scratch, isString) ||
      !isOptional(value.confirm, isString) || !isOptional(value.capture, isBoolean) ||
      !isOptional(value.writable, isStringArray) || !isOptional(value.protected, isStringArray) || !isOptional(value.private, isStringArray) ||
      !isOptional(value.unreadable, isStringArray) || !isOptional(value.readable, isStringArray) ||
      !isOptional(value.allowedDomains, isStringArray)) return undefined
  const { decision, reason, cwd, scratch, confirm, capture, writable, protected: protectedPaths, private: privatePaths, unreadable, readable, allowedDomains } = value
  return { decision, reason, ...(isString(cwd) ? { cwd } : {}),
    ...(isString(scratch) ? { scratch } : {}), ...(isString(confirm) ? { confirm } : {}),
    ...(isBoolean(capture) ? { capture } : {}),
    ...(isStringArray(writable) ? { writable } : {}),
    ...(isStringArray(protectedPaths) ? { protected: protectedPaths } : {}), ...(isStringArray(privatePaths) ? { private: privatePaths } : {}) ,
    ...(isStringArray(unreadable) ? { unreadable } : {}), ...(isStringArray(readable) ? { readable } : {}),
    ...(isStringArray(allowedDomains) ? { allowedDomains } : {}) }
}
export function responseObject(value: unknown, command: string): Record<string, unknown> {
  if (!isResponseObject(value)) throw new Error(`takt-ai ${command} returned a non-object response`)
  return value
}
// parseClaim keeps a well-formed ownership claim and drops anything else.
export function parseClaim(item: unknown): OwnershipClaim[] {
  if (!isResponseObject(item)) return []
  const claim = item
  if (typeof claim.key !== "string" || typeof claim.root_session_id !== "string" || typeof claim.work_unit_id !== "string" || typeof claim.agent_id !== "string" || typeof claim.target_instance !== "string") return []
  return [{ key: claim.key, root_session_id: claim.root_session_id, work_unit_id: claim.work_unit_id, agent_id: claim.agent_id, target_instance: claim.target_instance, ...(claim.pending === true ? { pending: true } : {}), ...(claim.active === true ? { active: true } : {}), ...(claim.staged === true ? { staged: true } : {}), ...(Array.isArray(claim.scope) ? { scope: claim.scope.filter((path): path is string => typeof path === "string") } : {}), ...(isStringArray(claim.reads) && claim.reads.length > 0 ? { reads: claim.reads } : {}), ...(typeof claim.author_key === "string" ? { author_key: claim.author_key } : {}) }]
}
// parseGate keeps a well-formed adopted gate and drops anything else.
export function parseGate(item: unknown): AdoptedGate[] {
  if (!isResponseObject(item) || !isString(item.key) || !isString(item.author_key) || !isStringArray(item.scope) || !isOptional(item.error, isString)) return []
  return [{ key: item.key, author_key: item.author_key, scope: item.scope, ...(isString(item.error) ? { error: item.error } : {}) }]
}
function parseRepair(value: unknown): Repair | undefined {
 if (!isResponseObject(value) || value.kind !== "manifest" || !isString(value.id) || !isString(value.author) || !isStringArray(value.paths) || !isString(value.state) || !isBoolean(value.preserve) || !isString(value.reason)) return undefined
 return value as Repair
}
function parseInspections(value: unknown): VFSResponse["inspections"] | undefined {
  if (!Array.isArray(value)) return undefined
  return value.every(item => isResponseObject(item) && isString(item.call_id) && isString(item.session_id) && isString(item.caller_session) && isString(item.caller_agent) && isOptional(item.tool_call_id, isString)) ? value as VFSResponse["inspections"] : undefined
}
// vfsResponse keeps the fields of a `takt-ai vfs` answer the plugin knows, each only when well typed.
export function vfsResponse(parsed: Record<string, unknown>, ok: boolean): VFSResponse {
  const shell = parseVFSShell(parsed.shell)
  const repair = parseRepair(parsed.repair)
  const inspections = parseInspections(parsed.inspections)
  return {
    ok,
    ...(repair ? { repair } : {}),
    ...(typeof parsed.key === "string" ? { key: parsed.key } : {}),
    ...(typeof parsed.content === "string" ? { content: parsed.content } : {}),
    ...(inspections ? { inspections } : {}),
    ...(typeof parsed.revision === "number" ? { revision: parsed.revision } : {}),
    ...(typeof parsed.delta_hash === "string" ? { delta_hash: parsed.delta_hash } : {}),
    ...(typeof parsed.error === "string" ? { error: parsed.error } : {}),
    ...(typeof parsed.attempt_id === "string" ? { attempt_id: parsed.attempt_id } : {}),
    ...(typeof parsed.invariants_version === "string" ? { invariants_version: parsed.invariants_version } : {}),
    ...(Array.isArray(parsed.claims) ? { claims: parsed.claims.flatMap(parseClaim) } : {}),
    ...(isStringArray(parsed.outdated_paths) ? { outdated_paths: parsed.outdated_paths } : {}),
    ...(Array.isArray(parsed.gates) ? { gates: parsed.gates.flatMap(parseGate) } : {}),
    ...(shell ? { shell } : {}),
  }
}
// VFSRefusal is a refusal from `takt-ai vfs`, keeping the fields it answered with.
export class VFSRefusal extends Error {
  readonly response: VFSResponse
  constructor(message: string, response: VFSResponse) {
    super(message)
    this.response = response
  }
}
const DISPATCH_ANSWERS = new Set(["prepare", "execution", "executions", "attach_child", "resolve", "inputs", "deliver", "interlocutor"])
// dispatchResponse returns result when it is the shape `dispatch` answers action with; null acknowledges a switch or result validation.
function dispatchResponse(result: Record<string, unknown>, action: unknown): DispatchResponse | undefined {
  if (DISPATCH_ANSWERS.has(String(action))) return result
  if (action === "switch" || action === "validate_results") return Object.keys(result).length === 0 || result.ok === true ? null : undefined
  return result.ok === true ? result : undefined
}
function verbResponse(verb: string[], result: Record<string, unknown>, action: unknown): DispatchResponse | undefined {
  if (verb[0] === "dispatch") return dispatchResponse(result, action)
  return undefined
}
export function parseCoordinationResponse(value: unknown, verb: string[], action: unknown): DispatchResponse {
  if (value === null) return null
  const result = responseObject(value, verb.join(" "))
  if (isInterlocutorReturn(result)) return result
  if (action === "session_deleted" && result.aborted === false) return { aborted: false }
  const shaped = verbResponse(verb, result, action)
  if (shaped !== undefined) return shaped
  throw new Error(`${verb.join(" ")} ${asString(action, "unknown")} returned an unexpected response shape`)
}

// sequentially awaits step for each item in order. Steps are not concurrent:
// each reads what the previous one wrote, and an error ends the rest.
export async function sequentially<T>(items: readonly T[], step: (item: T) => Promise<unknown>): Promise<void> {
  await items.reduce<Promise<unknown>>((previous, item) => previous.then(() => step(item)), Promise.resolve())
}

// nativeReadPath is the first of filePath, path or directory a native reader was given as text.
export function nativeReadPath(args: Record<string, unknown>): string {
  for (const key of ["filePath", "path", "directory"]) {
    const value = args[key]
    if (typeof value === "string") return value
  }
  return ""
}
// allowsExplicitly reports whether the last permission rule naming toolName, or every tool, allows it.
function allowsExplicitly(rules: unknown, toolName: string): boolean {
  if (!Array.isArray(rules)) return false
  let decision: unknown
  for (const rule of rules) {
    if (isResponseObject(rule) && (rule.resource === toolName || rule.resource === "*") && (rule.action === "*" || rule.action === toolName)) decision = rule.effect
  }
  return decision === "allow"
}
// invariantsNotice tells a delegated unit which recorded invariants it consumes.
const invariantsNotice = (unit: string, consumed: number[], undelivered: string[] = []) => (consumed.length > 0
  ? renderPrompt("context.invariants_consumed", { unit, entries: entryRefs(consumed), read_tool: MEMORY_TOOLS.read })
  : renderPrompt("context.invariants_none", { unit }))
  + (undelivered.length ? renderPrompt("context.invariants_undelivered", { undelivered: undelivered.join(", ") }) : "")
// declaredInputs reports what a unit will consume, as dispatch_inputs recorded it.
const fromUnit = (unit?: string) => unit ? ` (from ${unit})` : ""
const declaredInputs = (unit: string, inputs: { id: number; title: string; unit?: string }[], undelivered: string[]) => {
  const lines = inputs.map(i => `#${i.id} ${i.title}${fromUnit(i.unit)}`)
  const absent = undelivered.length ? renderPrompt("directive.declared_inputs_absent", { undelivered: undelivered.join(", ") }) : ""
  return lines.length
    ? renderPrompt("directive.declared_inputs_some", { unit, lines: lines.join("; "), absent })
    : renderPrompt("directive.declared_inputs_none", { unit, absent })
}
// gateLine reports one adopted gate of a verifier's assignment.
const gateLine = (gate: AdoptedGate) => {
  const refusal = gate.error ? renderPrompt("context.gate_refusal_suffix", { error: gate.error }) : ""
  return renderPrompt("context.gate_line", { author_key: gate.author_key, scope: gate.scope.join(", "), refusal })
}
// gateNotice is the assignment of a verifier whose gates already arrived bound.
const gateNotice = (unit: string, allowed: string[], gates: AdoptedGate[]) => {
  const reading = allowed.includes("vfs_read") ? renderPrompt("context.gate_read_clause") : ""
  const verdict = allowed.includes("vfs_verify") ? renderPrompt("context.gate_verify_clause") : ""
  return renderPrompt("context.gate_notice", { unit, reading, verdict, gates: gates.map(gateLine).join("\n") })
}
// readsLabel names a reads selector the way its reader understands it.
const readsLabel = (selector: string) => selector === "all" ? "every path" : selector === "free" ? "every path no work in flight holds" : selector
// authorNotice is the assignment of an author: the paths it owns, what else its
// unit reads, and the VFS conduct it may use.
const authorNotice = (unit: string, scope: string[], reads: string[], allowed: string[], unbound: string) => {
  const conduct = [
    allowed.includes("vfs_write") ? renderPrompt("context.author_conduct_write") : "",
    allowed.includes("vfs_delete") ? renderPrompt("context.author_conduct_delete") : "",
    allowed.includes("vfs_read") ? renderPrompt("context.author_conduct_read") : "",
    allowed.includes("vfs_read") && reads.length > 0 ? renderPrompt("context.author_reads", { reads: reads.map(readsLabel).join(", ") }) : "",
  ].filter(Boolean).join(" ")
  return renderPrompt("context.author_notice", { unit, scope: scope.join(", "), unbound, conduct })
}

// orchestratorOnly refuses a claim operation from any agent but the orchestrator.
export const orchestratorOnly = (c: { agent?: string }, operation: string) => {
  if (c.agent !== ORCHESTRATOR_ID) throw new Error(`${operation} belongs to the orchestrator`)
}

// The native subagent result invites follow-ups by sessionID; a Takt
// delegation takes none, and a returned session continues only after it
// delivered a blocker (PR-ORQ-39, PR-HAR-38).
const NATIVE_SUBAGENT_FOLLOW_UP = "To send it follow-ups or change its instructions while it runs, call subagent again with this sessionID."
const SUBAGENT_FOLLOW_UP = renderPrompt("directive.subagent_follow_up")
const SUBAGENT_SESSION_DESCRIPTION = paramDesc("subagent", "sessionID")
type ToolContent = string | ReadonlyArray<{ readonly type: "text"; readonly text: string } | { readonly type: "file"; readonly uri: string; readonly mime: string; readonly name?: string }> | undefined
const truthfulSubagentContent = (content: ToolContent): ToolContent =>
  typeof content === "string"
    ? content.replaceAll(NATIVE_SUBAGENT_FOLLOW_UP, SUBAGENT_FOLLOW_UP)
    : content?.map(part => part.type === "text" ? { ...part, text: part.text.replaceAll(NATIVE_SUBAGENT_FOLLOW_UP, SUBAGENT_FOLLOW_UP) } : part)

// SANDBOX_ADAPTER is the sibling module takt-ai deploys next to this plugin. It
// wraps a command with @anthropic-ai/sandbox-runtime; without it no shell
// command runs at all, because running one unwrapped would be a way around the
// VFS (PR-HAR-15).
const SANDBOX_ADAPTER = "./takt-sandbox.mjs"

// QUOTED_QUOTE closes a single-quoted shell word, emits a literal quote and reopens it.
const QUOTED_QUOTE = String.raw`'\''`

// SHELL_ACTIONS are the permission actions shell execution resolves under; the
// command itself is the resource the rules and the approval match.
const SHELL_ACTIONS = new Set(["shell", "bash"])

// SHELL_NOT_ADMITTED is the status a command exits with when no admitted plan
// covers it. create.before cannot abort, so the command is replaced by its own
// refusal instead of reaching the workspace unwrapped.
const SHELL_NOT_ADMITTED = 122

// SHELL_MARKER_PREFIX opens the trailing shell comment that carries a shell
// call's token from execute.before to create.before; a comment leaves the
// command's meaning unchanged and create.before strips it before anything runs.
const SHELL_MARKER_PREFIX = "\n#takt-call:"
// SHELL_TOKEN_LENGTH is the hex length of a call token: 128 bits, unguessable.
const SHELL_TOKEN_LENGTH = 32
// LEASE_RENEW_INTERVAL_MS is how often this instance renews its lease on the
// executions it owns: a third of the core's lease TTL (90 s).
const LEASE_RENEW_INTERVAL_MS = 30_000
const SHELL_MARKER_PATTERN = new RegExp(`${SHELL_MARKER_PREFIX}([0-9a-f]{${SHELL_TOKEN_LENGTH}})$`)

// VFS_SHELL_ENFORCED decides whether specialist shell commands are admitted
// through the VFS and run in the sandbox. When false every shell command runs
// natively under its OpenCode permission profile; file edits stay governed.
const VFS_SHELL_ENFORCED: boolean = "__TAKT_VFS_SHELL_ENFORCED__" as unknown as boolean


// RESULT_AGENTS are the catalog instances that carry the takt-result-handoff
// skill. Those that stage no work deliver a memory artifact, so their
// delegation must call deliver_result before it ends; a VFS author calls it
// only to deliver a blocker.
const RESULT_AGENTS: string[] = "__TAKT_RESULT_AGENTS__" as unknown as string[]

// VFS_AUTHORS are the catalog instances granted VFS writes; only their native
// inspections reserve a read scope, so no other agent's read needs the core.
const VFS_AUTHORS: string[] = "__TAKT_VFS_AUTHORS__" as unknown as string[]

// deliversArtifact holds for an instance that delivers a memory artifact and
// stages no work: its shell runs natively, and its delegation is reminded to deliver.
const deliversArtifact = (agent?: string) => RESULT_AGENTS.includes(agent ?? "") && !VFS_AUTHORS.includes(agent ?? "")

// EXCEPTION_BOUNDS are the limits a user exception may raise, by the names the
// execution history records them under.
const EXCEPTION_BOUNDS = [
  "ceiling/concurrent-specialists", "budget/unplanned-units", "budget/contests",
  "budget/recovery-failures", "budget/recovery-actions", "budget/recovery-attempts", "budget/qa-strengthening-rounds",
]

// FAILING_VERDICT is how the core words a refusal to consolidate over a current
// failing verdict: only the user's acceptance lets the work through.
const FAILING_VERDICT = "a current verification verdict failed"
const VFS_TOOL_NAMES = ["vfs_bind", "vfs_write", "vfs_read", "vfs_delete", "vfs_discard", "vfs_verify", "vfs_consolidate"]

// entryRefs renders memory entry IDs the way agents cite them.
const entryRefs = (ids: number[]) => ids.map(id => `#${id}`).join(", ")
// str and obj declare the plugin tools' JSON Schema inputs.
const str = (description?: string) => (description ? { type: "string", description } : { type: "string" })
const obj = (properties: Record<string, unknown>, required: string[]) =>
  ({ type: "object", properties, required, additionalProperties: false })

// asString narrows an unknown protocol field to a string, falling back
// instead of letting a non-string value stringify to the useless
// "[object Object]" (dispatch/shell fields are caller-supplied and only
// typed unknown because they pass through a Record<string, unknown>).
export const asString = (v: unknown, fallback: string): string => (typeof v === "string" ? v : fallback)

// Prefixed names of the memory tools this plugin names, rendered from the Go constants that define them.
const MEMORY_TOOLS: { record: string; read: string } = "__TAKT_MEMORY_TOOLS__" as unknown as never

export default Plugin.define({
  id: "takt.vfs",
  async setup(ctx) {
    const workspace = ctx.location.directory
    const abort = new AbortController()
    let hostWaits = new AbortController()
    const home = homedir()
    if (await sameDirectory(workspace, home)) {
      console.warn(`Takt: CodeGraph was not initialized because OpenCode is open in the home directory (${home}). Open a specific project to enable CodeGraph tools.`)
    } else {
      // MCP tools are catalogued during OpenCode startup. Prepare the index
      // synchronously here rather than from a later session event, so CodeGraph
      // is active by the time OpenCode asks its MCP server for tools.
      try {
        const codegraph = Bun.spawn([TAKT_AI, "codegraph", "ensure-index"], { detached: true, cwd: workspace, stdin: "ignore", stdout: "pipe", stderr: "pipe" })
        const [out, err, code] = await ipcOutput(codegraph, "CodeGraph preparation")
        if (code !== 0) throw new Error(err.trim() || out.trim() || `exit ${code}`)
      } catch (error) {
        console.error("Takt CodeGraph preparation unavailable; protection hooks remain active", error)
      }
    }

    // Execution facts live in history. The plugin retains only outstanding host
    // wait promises: transport correlation with no power to authorize a call.
    const watching = new Set<string>()
    // INSTANCE names this harness instance in the execution history, which
    // records the executions it owns; the lease it renews keeps them its own.
    const INSTANCE = randomUUID()

    // Exact host invocation correlation only. Every owner checks these IDs
    // against its live state; this map cannot grant or extend an execution.
    const invocations = new Map<string,Pick<Delegation,"dispatch"|"root"|"attempt">>()
    const execution = async (dispatch: string) => delegationOf(await dispatchAction({ action: "execution", dispatch }))
    const executionFor = async (child: string, agent?: string) => delegationOf(await dispatchAction({ action: "resolve", child, agent }))
    // unsettledExecutions lists the unsettled executions, each marked mine when
    // this instance may act on it: its own, or one the core reports orphaned
    // because its owner's lease lapsed.
    async function unsettledExecutions(): Promise<Array<Delegation & { mine: boolean }>> {
      const result = responseObject(await dispatchAction({ action: "executions" }), "dispatch executions")
      if (!result || !isResponseObject(result) || !isResponseObject(result.executions)) return []
      const orphaned = isStringArray(result.orphaned) ? result.orphaned : []
      return Object.values(result.executions).flatMap(d => {
        const parsed = delegationOf(d as DispatchResponse)
        return parsed && !parsed.settled ? [{ ...parsed, mine: parsed.owner === INSTANCE || orphaned.includes(parsed.dispatch) }] : []
      })
    }
    // unsettledActivities lists the direct orchestrator activities in flight,
    // each with whether this instance captures it and whether the core reports
    // it orphaned because its capturing instance's lease lapsed.
    async function unsettledActivities(): Promise<Array<{ id: string; root: string; own: boolean; orphaned: boolean; unmonitored: boolean }>> {
      const result = responseObject(await dispatchAction({ action: "executions" }), "dispatch executions")
      if (!result || !isResponseObject(result) || !isResponseObject(result.activities)) return []
      const orphaned = isStringArray(result.orphaned_activities) ? result.orphaned_activities : []
      return Object.entries(result.activities).flatMap(([id, a]) => isResponseObject(a) && typeof a.session_id === "string"
        ? [{ id, root: a.session_id, own: a.owner === INSTANCE, orphaned: orphaned.includes(id), unmonitored: a.flight === "capture unmonitored" }]
        : [])
    }
    // markActivitiesUnmonitored records lost capture for each listed activity.
    // The core keeps the activity in flight and ignores a repeat.
    async function markActivitiesUnmonitored(activities: Array<{ id: string; root: string }>) {
      await Promise.all(activities.map(a =>
        dispatchAction({ action: "activity_unmonitored", session: a.root, activity_id: a.id, node_kind: "orchestrator" })
          .catch(error => console.error("Takt unmonitored activity unrecorded", a.id, error))))
    }
    // A terminal host record of the exact native call settles any execution,
    // whoever owns it; only the owner, or any instance once the owner's lease
    // lapsed, starts a host wait on a child.
    async function nativeCall(d: Delegation) {
      const messages = await ctx.session.context({ sessionID:d.caller }, { signal:AbortSignal.any([abort.signal,AbortSignal.timeout(60_000)]) })
      const callID = d.dispatch.startsWith(`${d.caller}:`) ? d.dispatch.slice(d.caller.length + 1) : undefined
      return messages.flatMap(message => message.type === "assistant" ? message.content : [])
        .find(part => part.type === "tool" && part.name === "subagent" && part.id === callID)
    }
    const notFound = (error: unknown) => isResponseObject(error) && error._tag === "SessionNotFoundError"
    // callState reads d's exact native call: "gone" once its caller session no
    // longer exists, so the call can never complete.
    async function callState(d: Delegation): Promise<"gone" | "error" | "completed" | "open"> {
      try {
        const call = await nativeCall(d)
        // A compaction drops older calls from the caller's context. An async
        // launch whose child is attached already returned, so the missing call is
        // not an open one: the child's idle or deletion is its termination.
        if (call === undefined && d.child) return "completed"
        return call?.type === "tool" && (call.state.status === "error" || call.state.status === "completed") ? call.state.status : "open"
      } catch (error) {
        if (notFound(error)) return "gone"
        throw error
      }
    }
    // settle ends d by its observed termination; one whose caller is gone ends
    // interrupted, after its child is stopped.
    async function settle(d: Delegation, state: "gone" | "error" | "completed") {
      if (state !== "gone") return endDelegation(d.dispatch, d)
      if (d.child) await stopChild(d.child).catch(error => console.error("Takt callerless child stop", d.dispatch, error))
      await dispatchAction({ action: "abandon", session: d.root, dispatch: d.dispatch, event: d.unit })
    }
    const reconcileNativeInspection = async (call: Inspection) => {
      try {
        // A deleted caller session can never complete the call: it is terminal.
        let gone = false
        const messages = await ctx.session.context({sessionID:call.caller_session},{signal:AbortSignal.any([abort.signal,AbortSignal.timeout(60_000)])})
          .catch((error: unknown) => { if (notFound(error)) { gone = true; return [] } throw error })
        const observed = messages.flatMap(message => message.type === "assistant" ? message.content : [])
          .find(part => part.type === "tool" && part.id === (call.tool_call_id || call.call_id))
        if (!gone && (observed?.type !== "tool" || (observed.state.status !== "completed" && observed.state.status !== "error"))) return
        await taktUnlocked("native-inspection-end", {
          ipc_version: IPC_VERSION, session_id: call.session_id, caller_session: call.caller_session,
          caller_agent: call.caller_agent, caller_dispatch: "", work_unit_id: "", call_id: call.call_id,
        })
      } catch (error) { console.error("Takt native inspection reconciliation",call.call_id,error) }
    }
    // Inspections are reconciled one at a time: each one ends a call the host reports on.
    const reconcileNativeInspections = async () => {
      const response = await taktUnlocked("native-inspections", { session_id:"reconcile", caller_session:"reconcile", caller_agent:"harness" })
      await sequentially(response.inspections ?? [], reconcileNativeInspection)
    }
    // backgroundChildEnded reports whether the child of a background launch was deleted, ending the delegation.
    // Exact launch completion plus verified child deletion is terminal;
    // a transport failure or an expired owner lease is not.
    const backgroundChildEnded = async (d: Delegation, child: string): Promise<boolean> => {
      try {
        await ctx.session.get({ sessionID:child }, { signal:AbortSignal.any([abort.signal,AbortSignal.timeout(60_000)]) })
      } catch (error) {
        if (notFound(error)) {
          await endDelegation(d.dispatch,d)
          return true
        }
        console.error("Takt background child reconciliation",d.dispatch,error)
      }
      return false
    }
    const watchChild = (d: Delegation, child: string) => {
      watching.add(d.dispatch)
      void ctx.session.wait({ sessionID:child }, { signal:AbortSignal.any([abort.signal,hostWaits.signal]) }).then(async () => {
        // Launch completion plus host idle is termination, not proof of
        // delivery or verification.
        const state = await callState(d)
        if (state !== "open") await settle(d, state)
      }).catch(async error => {
        if (notFound(error)) {
          const state = await callState(d)
          if (state !== "open") {
            await settle(d, state)
            return
          }
        }
        console.error("Takt native delegation reconciliation",d.dispatch,error)
      }).catch(error => console.error("Takt native delegation reconciliation",d.dispatch,error))
        .finally(() => watching.delete(d.dispatch))
    }
    // recoverChild links a launched delegation whose attach failed: the host
    // recorded the child on the exact completed native call.
    async function recoverChild(d: Delegation): Promise<string | undefined> {
      const call = await nativeCall(d)
      const child = call?.type === "tool" && call.state.status === "completed" ? call.state.metadata?.sessionID : undefined
      if (typeof child !== "string" || !child) return undefined
      await adoptChild(child, d.agent, d)
      return child
    }
    // Every exact call is checked, including our own lost callbacks and
    // executions whose linked child disappeared. Lease expiry proves nothing.
    const reconcileDelegation = async (d: Delegation & { mine: boolean }) => {
      let launched = false
      let child = d.child
      try {
        const state = await callState(d)
        if (state === "gone" || state === "error") {
          await settle(d, state)
          return
        }
        launched = state === "completed"
        if (launched && !child) child = await recoverChild(d)
      } catch (error) { console.error("Takt native call reconciliation", d.dispatch, error) }
      if (launched && child && await backgroundChildEnded(d, child)) return
      if (watching.has(d.dispatch) || !child || !d.mine || !launched) return
      watchChild(d, child)
    }
    const reconcileStoredDelegations = async () => {
      await reconcileNativeInspections().catch(error => console.error("Takt native inspection reconciliation",error))
      // While the host stream is down, the lost capture of what this instance
      // watches is recorded again, so a write that failed is retried. Otherwise
      // only an instance that stopped renewing its lease lost the capture of the
      // activities it ran; whichever instance notices records that gap.
      if (!eventsRunning && !disposed) {
        await unsettledExecutions().then(executions => recordLostCapture(executions.filter(d => d.mine)))
          .catch(error => console.error("Takt lost capture reconciliation", error))
      } else {
        await unsettledActivities()
          .then(activities => markActivitiesUnmonitored(activities.filter(a => a.orphaned && !a.own && !a.unmonitored)))
          .catch(error => console.error("Takt orphaned activity reconciliation", error))
      }
      await sequentially(await unsettledExecutions(), reconcileDelegation)
    }
    function stateDir(): string {
      return resolve(workspace, ".takt-ai", "vfs")
    }

    async function delegatedUnit(sessionID: string): Promise<string> {
      const d = await executionFor(sessionID)
      if (!d) throw new Error(renderPrompt("refusal.vfs.no_running_delegation", { session_id: sessionID }))
      return d.unit
    }

    async function rootSession(id: string): Promise<string> {
      const seen = new Set<string>()
      let current = id
      while (!seen.has(current)) {
        seen.add(current)
        const link:unknown = await dispatchAction({ action: "resolve", child: current })
        if (link && isResponseObject(link) && typeof link.root === "string") return link.root
        const info = await ctx.session.get({ sessionID: current })
        if (!info.parentID) return current
        current = info.parentID
      }
      throw new Error(`session ${id} has a parent cycle`)
    }

    // Caller is the host-delivered context of whoever runs a VFS command.
    type Caller = { sessionID: string; agent?: string; id?: string; messageID?: string }
    type Request = Record<string, unknown>
    // callerBlock is the caller's identity as the harness delivers it, derived
    // per call and never stored: the core resolves what the caller holds.
    async function callerBlock(c: Caller): Promise<Request> {
      const call = c.id ? invocations.get(`${c.sessionID}:${c.id}`) : undefined
      const root = call?.root ?? await rootSession(c.sessionID)
      return { session_id: root, caller_session: c.sessionID, caller_agent: c.agent ?? "", ...(call ? {caller_dispatch:call.dispatch,caller_attempt:call.attempt} : {}) }
    }
    // The core workspace lock, not a process-global queue, serializes effects.
    // takt runs one VFS command for c; fields may name a claim's target unit.
    const takt = async (command: string, c: Caller, fields: Request = {}) => {
      const unit = command === "bind" || command === "adopt-gates"
        ? { work_unit_id: await delegatedUnit(c.sessionID) } : {}
      const request = { ...await callerBlock(c), ...unit, ...fields }
      return repairOperation(() => taktUnlocked(command, request), approval => taktUnlocked(command, { ...request, repair: approval, accept_failing: false }), c)
    }
    // holdsInterface reports whether session is the temporary interlocutor its
    // root lent the interface to, as the history records it.
    async function holdsInterface(session: string): Promise<boolean> {
      const root = await rootSession(session)
      if (root === session) return false
      const holder: unknown = await dispatchAction({ action: "interlocutor", session: root })
      return isResponseObject(holder) && holder.holder === session
    }
    async function repairOperation<T>(run: () => Promise<T>, resume: (approval: Request) => Promise<T>, c: Caller): Promise<T> {
      try { return await run() }
      catch (error) {
        const repair = error instanceof VFSRefusal ? error.response.repair : undefined
        if (!repair) throw error
        const diagnostic = `${repair.reason}; ${repair.kind} ${repair.id}, author ${repair.author}, files ${repair.paths.join(", ")}, state ${repair.state}`
        // The user approves a repair; whoever speaks with the user asks.
        const interlocutor = c.agent === ORCHESTRATOR_ID || await holdsInterface(c.sessionID)
        if (!interlocutor) throw new VFSRefusal(renderPrompt("refusal.vfs.repair_non_interlocutor", { diagnostic }), (error as VFSRefusal).response)
        if (!c.agent || !c.id || !c.messageID) {
          throw new VFSRefusal(renderPrompt(c.agent === ORCHESTRATOR_ID ? "refusal.vfs.repair_retry_orchestrator" : "refusal.vfs.repair_retry_interlocutor", { diagnostic }), (error as VFSRefusal).response)
        }
        const approval = { kind: repair.kind, id: repair.id, state: repair.state, preserve: repair.preserve }
        await confirmWithUser(`vfs_repair_${repair.kind}`, repair.id, { reason: repair.reason, author: repair.author, files: repair.paths.join(", "), state: repair.state, preserve_current_files: String(repair.preserve) }, { sessionID:c.sessionID, agent:c.agent, id:c.id, messageID:c.messageID })
        // Re-entry evaluates the original operation; this consent covers no
        // ownership, verification, failing-verdict or native shell decision.
        return resume(approval)
      }
    }
    // governed runs an interlocutor's coordination request; a pending repair is
    // asked of the user exactly as for every VFS command.
    const governed = (c: Caller, request: Request, run: (request: Request) => Promise<DispatchResponse> = dispatchAction) =>
      repairOperation(() => run(request), approval => run({ ...request, repair: approval }), c)
    // mutated checks that a staging answer reports the state it left.
    const mutated = (res: VFSResponse) => {
      if (typeof res.revision !== "number" || typeof res.delta_hash !== "string") throw new Error("takt-ai VFS mutation response omitted revision or delta_hash")
      return res
    }
    // findClaims returns the claims held for exactly this root session, work unit and
    // agent instance while they are in one of the given states.
    const findClaims = async (c: Caller, root: string, unit: string, agent: string, states: ("pending" | "active")[]): Promise<OwnershipClaim[]> =>
      ((await takt("claims", c)).claims ?? []).filter((x: OwnershipClaim) =>
        x.root_session_id === root && x.work_unit_id === unit && x.agent_id === agent && x.target_instance === agent && states.some(state => x[state] === true))

    // Observability is deliberately a side channel: telemetry failure must never
    // change the outcome of the governed operation. The Go CLI validates the
    // envelope and persists it in the workspace-local event store.
    function observe(eventClass: string, source: string, agent: string, sessionID: string, attributes: Record<string, unknown>, workUnitID = "", attemptID = "") {
      const payload = JSON.stringify({
        ipc_version: 1, session_id: sessionID, work_unit_id: workUnitID,
        attempt_id: attemptID, agent, source, event_class: eventClass, attributes,
      })
      // Defer the subprocess until the governed hook has returned. This keeps
      // telemetry strictly non-blocking even when OpenCode is under load.
      setTimeout(() => { void (async () => {
        try {
          const proc = Bun.spawn([TAKT_AI, "obs", "ingest", "--workspace", workspace], {
            detached:true, stdin: "pipe", stdout: "pipe", stderr: "pipe", cwd: workspace,
          })
          proc.stdin.write(payload)
          proc.stdin.end()
          // Drain both streams so a telemetry process cannot remain blocked on
          // pipe backpressure. Its exit status is intentionally non-fatal.
          await ipcOutput(proc, "telemetry")
        } catch (error) {
          console.error("Takt observability unavailable", error)
        }
      })() }, 0)
    }

    // refused records a governed refusal before it reaches the caller: the
    // denials are the signal of whether conduct and controls agree, so they are
    // observed like any other dispatch decision, then thrown unchanged.
    function refused(reasonCode: string, agent: string, sessionID: string, error: Error, workUnitID = ""): Error {
      observe("dispatch", "takt.orchestration", agent || "harness", sessionID, {
        decision: "refused", specialist: agent, reason_code: reasonCode, dispatch_id: workUnitID,
      }, workUnitID)
      return error
    }

    async function taktUnlocked(command: string, request: Record<string, unknown>): Promise<VFSResponse> {
      const proc = Bun.spawn([TAKT_AI, "vfs", command, "--workspace", workspace, "--state", stateDir()], {
        detached:true, stdin: "pipe", stdout: "pipe", stderr: "pipe", cwd: workspace,
      })
      proc.stdin.write(JSON.stringify({ ipc_version: IPC_VERSION, ...request }))
      proc.stdin.end()
      // takt-ai reports its failures on stderr, so a refusal is read from there.
      const [out, err, code] = await ipcOutput(proc, `vfs ${command}`)
      try {
        const output: unknown = JSON.parse(out)
        const parsed = responseObject(output, `vfs ${command}`)
        if (code !== 0 || parsed.ok === false) throw new VFSRefusal(typeof parsed.error === "string" ? parsed.error : `takt-ai vfs ${command} exited ${code}: ${err}`, vfsResponse(parsed, false))
        if (typeof parsed.ok !== "boolean") throw new Error(`takt-ai vfs ${command} returned a response without ok`)
        return vfsResponse(parsed, parsed.ok)
      } catch (e) {
        // A failed command reports on stderr and leaves stdout empty; only
        // unparseable output from a successful exit is a parse failure.
        if (e instanceof SyntaxError) throw new Error(`takt-ai vfs ${command} exited ${code}: ${err || out}`, code === 0 ? { cause: e } : undefined)
        throw e
      }
    }

    // releaseShellSession is armed with the shell hooks below.
    let releaseShellSession: (sessionID: string, importDelta: boolean) => Promise<void> = () => Promise.resolve()

    // ─── shell capture ────────────────────────────────────────────────────────
    // Specialist shell execution is governed by VFS: the harness resolves a
    // sandbox plan from the binding's own scope, then imports workspace mutations
    // as a staged delta. The orchestrator instead uses OpenCode's native shell
    // permissions and is never required to bind or route through VFS.
    //
    // OpenCode runs a shell tool call as: tool execute.before (session and
    // agent known, input rewritable) → shell create.before (command only) →
    // permission evaluate (carries the tool call it belongs to). execute.before
    // admits the call and appends an unguessable per-call token to the command as
    // a trailing shell comment; create.before finds the call by that token,
    // strips it and swaps the shell executable for a sandboxed one without
    // touching the command text; evaluate finds the call by its tool call id and
    // only applies the decision admission already took.
    type ShellPlan = { decision: "allow" | "ask" | "deny"; reason: string; cwd: string; scratch: string; confirm: string; writable: string[]; protected: string[]; private: string[]; unreadable: string[]; readable: string[]; allowedDomains?: string[]; capture: boolean }
    // ready is false while the transport request is still running.
    // started claims the call for create.before; wrapped says the sandbox is armed.
    type AdmittedShell = {
      ready?: boolean; callID: string; session: string; agent?: string; toolKey: string
      started: boolean; wrapped: boolean; evaluated: boolean; wrapper?: string
    }
    type DirectShell = { toolKey: string; ready: boolean }
    // Admission is one synchronous critical section per call token: the entry is
    // reserved before any await and released on failure, and create.before claims
    // it before its first await, so no check spans an await (STR-16).
    const admittedShell = new Map<string, AdmittedShell>()
    // The orchestrator's and artifact producers' native-permission admission, by call
    // token, until create. Each reservation is bound to its tool call and expires
    // with it.
    const directShell = new Map<string, DirectShell>()
    const toolKeyOf = (event: { sessionID: string; id?: string }) => `${event.sessionID}:${event.id}`
    const markShell = (command: string, token: string) => `${command}${SHELL_MARKER_PREFIX}${token}`
    // tokenOf splits an executed command into its text and the token it carries.
    const tokenOf = (command: string): { text: string; token?: string } => {
      const found = SHELL_MARKER_PATTERN.exec(command)
      return found ? { text: command.slice(0, found.index), token: found[1] } : { text: command }
    }
    // admitDirect lets the orchestrator or a result agent run a command on the
    // native shell, reserving it for create.before before its guard awaits.
    const admitDirect = async (agent: string | undefined, sessionID: string, command: string, toolKey: string, token: string) => {
      const entry: DirectShell = { toolKey, ready: agent === ORCHESTRATOR_ID }
      directShell.set(token, entry)
      // A result agent's native shell has no sandbox; its Git mutation is
      // refused by the same classifier the shell plan applies (PR-CRW-5).
      if (agent !== ORCHESTRATOR_ID) {
        try {
          await takt("shell-guard", { sessionID, agent }, { command })
        } catch (e) {
          directShell.delete(token)
          throw refused("git_mutation_denied", agent ?? "", sessionID, new Error(renderPrompt("refusal.vfs.shell_refused", { reason: e instanceof Error ? e.message : String(e) })))
        }
        entry.ready = true
      }
    }
    // shellPlanOf reads the plan shell-prepare admitted, refusing a denial or
    // a plan missing a field.
    const shellPlanOf = (shell: VFSShellResponse | undefined, agent: string | undefined, sessionID: string): ShellPlan => {
      if (!shell) throw new Error("takt-ai vfs shell-prepare response omitted shell plan")
      if (shell.decision === "deny") throw refused("shell_denied", agent ?? "", sessionID, new Error(renderPrompt("refusal.vfs.shell_refused", { reason: shell.reason })))
      if (typeof shell.cwd !== "string" || typeof shell.scratch !== "string" || typeof shell.confirm !== "string" || typeof shell.capture !== "boolean" || !shell.writable || !shell.protected || !shell.private || !shell.unreadable) throw new Error("takt-ai vfs shell-prepare response omitted an allowed shell plan field")
      // readable is empty when nothing inside a denied path needs re-allowing.
      return { decision: shell.decision, reason: shell.reason, cwd: shell.cwd, scratch: shell.scratch, confirm: shell.confirm, capture: shell.capture, writable: shell.writable, protected: shell.protected, private: shell.private, unreadable: shell.unreadable, readable: shell.readable ?? [], ...(shell.allowedDomains ? { allowedDomains: shell.allowedDomains } : {}) }
    }

    // The first execute.before pins the tool call to the execution its session
    // runs, before any other hook (the shell's included) acts for that call.
    const toolStartedAt = new Map<string, number>()
    await ctx.tool.hook("execute.before", async (event) => {
      toolStartedAt.set(toolKeyOf(event), Date.now())
      if (event.agent === ORCHESTRATOR_ID) return
      const d = await executionFor(event.sessionID, event.agent)
      if (!d) return
      invocations.set(toolKeyOf(event), { dispatch: d.dispatch, root: d.root, attempt: d.attempt })
    })

    if (VFS_SHELL_ENFORCED) {
      await ctx.tool.hook("execute.before", async (event) => {
        if (!SHELL_ACTIONS.has(event.tool)) return
        const input = isResponseObject(event.input) ? event.input : {}
        const command = asString(input.command, "")
        // The call is correlated into create.before by its token, so every
        // reservation is made before the first await.
        const toolKey = toolKeyOf(event)
        const token = randomUUID().replaceAll("-", "").slice(0, SHELL_TOKEN_LENGTH)
        event.input = { ...input, command: markShell(command, token) }
        if (event.agent === ORCHESTRATOR_ID || deliversArtifact(event.agent)) {
          await admitDirect(event.agent, event.sessionID, command, toolKey, token)
          return
        }
        const callID = randomUUID()
        const entry: AdmittedShell = { callID, session: event.sessionID, agent: event.agent, toolKey, started: false, wrapped: false, evaluated: false }
        admittedShell.set(token, entry)
        try {
          // The core plans from the binding this session holds; an unbound caller
          // may still inspect, but it may not mutate.
          const res = await takt("shell-prepare", event, { call_id: callID, command, tool_call_id:event.id })
          shellPlanOf(res.shell, event.agent, event.sessionID)
          entry.ready = true
        } catch (e) {
          admittedShell.delete(token)
          throw e
        }
      })

      await ctx.shell.hook("create.before", async (input) => {
        const refuse = (reason: string) => {
          const message = JSON.stringify(`Takt: ${reason}`)
          input.command = `echo ${message} >&2; exit ${SHELL_NOT_ADMITTED}`
        }
        const { text, token } = tokenOf(input.command)
        const direct = token ? directShell.get(token) : undefined
        if (token && direct?.ready) {
          directShell.delete(token)
          input.command = text
          return // The orchestrator uses OpenCode's native shell permission, not VFS.
        }
        // create.before cannot abort, so an unadmitted command is replaced by its
        // own refusal. A shell without an admitted token, such as one the user
        // opens outside a dispatch, is refused too.
        const admitted = token ? admittedShell.get(token) : undefined
        if (!admitted?.ready || admitted.started) return refuse(renderPrompt("refusal.vfs.shell_not_admitted_command"))
        // Claim the call before the first await: a second create.before for the
        // same token finds it started.
        admitted.started = true
        input.command = text
        const { callID } = admitted
        try {
          const plan = shellPlanOf((await takt("shell-continue",{sessionID:admitted.session,agent:admitted.agent},{call_id:callID,phase:"start"})).shell,admitted.agent,admitted.session)
          const { wrap, supervise } = await import(SANDBOX_ADAPTER)
          const run = supervise(await wrap(text, {
            writable: plan.writable, scratch: plan.scratch,
            protectedPaths: plan.protected, privatePaths: plan.private, unreadablePaths: plan.unreadable, readablePaths: plan.readable,
            ...(plan.allowedDomains ? { allowedDomains: plan.allowedDomains } : {}), callID,
          }), plan.confirm)
          // The command text stays as admitted: OpenCode parses it for its own
          // permission check after this hook. The sandbox lives in the shell
          // executable instead, which ignores the arguments it is handed and runs
          // the supervised command against its projection. Every command gets the
          // scratch as its temporary directory, set by the adapter inside the
          // sandbox; a command that mutates also gets it as home, while
          // inspection runs in the workspace and keeps the real home.
          const quote = (s: string) => `'${s.replaceAll("'", QUOTED_QUOTE)}'`
          const dir = await mkdtemp(join(tmpdir(), "takt-shell-"))
          admitted.wrapper = dir
          const wrapper = join(dir, "sh")
          await writeFile(wrapper, [
            "#!/bin/sh",
            `cd ${quote(plan.cwd)} || exit ${SHELL_NOT_ADMITTED}`,
            ...(plan.cwd === workspace ? [] : [`export HOME=${quote(plan.scratch)}`]),
            `exec /bin/sh -c ${quote(run)}`,
            "",
          ].join("\n"), { mode: 0o700 })
          input.shell = wrapper
          admitted.wrapped = true
        } catch (e) {
          refuse(`sandbox unavailable, shell denied: ${e}`)
        }
      })

      await ctx.permission.hook("evaluate", async (event) => {
        if (!SHELL_ACTIONS.has(event.action)) return
        // A configured denial is never weakened: the harness may only tighten what
        // the ruleset already resolved, and a denied command is never admitted.
        if (event.effect === "deny" || event.agent === ORCHESTRATOR_ID || deliversArtifact(event.agent)) return
        // OpenCode asks over the commands it parsed out of the call, not the call
        // itself, so the admission is found by the tool call the request names.
        // The decision is resolved again for every execution, so a persistent
        // approval cannot silently broaden the command, the resource or the
        // role: an approval-gated command is asked once per run (PR-HAR-9).
        const key = event.source ? toolKeyOf({ sessionID: event.sessionID, id: event.source.id }) : undefined
        const mine = key ? [...admittedShell.values()].find((a) => a.wrapped && !a.evaluated && a.toolKey === key && a.ready) : undefined
        if (!mine) {
          event.effect = "deny"
          event.message = renderPrompt("refusal.vfs.shell_not_admitted_permission")
          return
        }
        mine.evaluated = true
        try {
          const plan = shellPlanOf((await takt("shell-continue",{sessionID:mine.session,agent:mine.agent},{call_id:mine.callID,phase:"evaluate"})).shell,mine.agent,mine.session)
          event.effect = plan.decision
          event.message = plan.reason
        } catch (error) {
          event.effect = "deny"
          event.message = renderPrompt("refusal.vfs.shell_eval_error", { reason: error instanceof Error ? error.message : String(error) })
        }
      })

      // The entry of an ended call is always released; only an armed one imports,
      // and only when the call can still import (its session is not gone).
      async function finishShell(token: string, admitted: AdmittedShell, sessionID: string, importDelta: boolean): Promise<void> {
        admittedShell.delete(token)
        if (admitted.wrapper) await rm(admitted.wrapper, { recursive: true, force: true })
        if (!importDelta || !admitted.wrapped) {
          await takt("shell-release",{sessionID:admitted.session,agent:admitted.agent},{call_id:admitted.callID})
          return
        }
        // A failing command keeps its delta: it is imported, retained, and left
        // unverified for the gate to judge.
        mutated(await takt("shell-complete", { sessionID, agent: admitted.agent }, { call_id: admitted.callID }))
      }

      // The command's result is admitted as one VFS transaction once it has ended,
      // completed or in error. A direct reservation that never reached
      // create.before expires with its call.
      // Entries finish sequentially: each import can advance the binding's
      // revision, and an error must leave later admissions untouched.
      await ctx.tool.hook("execute.after", async (event) => {
        const toolKey = toolKeyOf(event)
        for (const [token, d] of directShell) if (d.toolKey === toolKey) directShell.delete(token)
        const ended = [...admittedShell].filter(([, a]) => a.session === event.sessionID && a.toolKey === toolKey)
        await sequentially(ended, ([token, admitted]) => finishShell(token, admitted, event.sessionID, true))
      })

      // A call whose execute.after never arrives ends with its session: an idle
      // session has no call in flight (its delta is still imported), a deleted one
      // has no binding left to import into.
      releaseShellSession = async (sessionID, importDelta) => {
        for (const [token, d] of directShell) if (d.toolKey.startsWith(`${sessionID}:`)) directShell.delete(token)
        const ended = [...admittedShell].filter(([, admitted]) => admitted.session === sessionID)
        await sequentially(ended, ([token, admitted]) => finishShell(token, admitted, sessionID, importDelta))
      }
    }

    async function spawnCoordination(verb: string[], request: Record<string, unknown>): Promise<DispatchResponse> {
      const proc = Bun.spawn([TAKT_AI, ...verb, "--workspace", workspace,
        "--state", stateDir(), "--request", JSON.stringify(request)], {
        detached:true, stdin: "ignore", stdout: "pipe", stderr: "pipe", cwd: workspace,
      })
      const [out, err, code] = await ipcOutput(proc, verb.join(" "), 60_000)
      if (code !== 0) throw new Error(`${verb.join(" ")} ${asString(request.action, "unknown")}: ${err || out}`)
      const parsed: unknown = JSON.parse(out)
      if (isResponseObject(parsed) && parsed.ok === false) throw new VFSRefusal(asString(parsed.error, "coordination refused"), vfsResponse(parsed, false))
      return parseCoordinationResponse(parsed, verb, request.action)
    }
    // dispatchAction admits and drives ordinary crew work through the execution history.
    // A declaration, so the hooks registered earlier in setup can reach it.
    async function dispatchAction(request: Record<string, unknown>): Promise<DispatchResponse> {
      const action = asString(request.action, "unknown")
      const session = asString(request.session, "")
      const event = asString(request.event, "")
      const agent = asString(request.agent, "harness")
      let result: DispatchResponse
      try {
        result = await spawnCoordination(["dispatch"], request)
      } catch (e) {
        throw refused("dispatch_refused", agent, session, e instanceof Error ? e : new Error(String(e)), event)
      }
      // Only a unit transition carries the slots held and the states it moved
      // between; other actions report none rather than an invented value.
      const transition: Record<string, unknown> = result !== null && isResponseObject(result) ? result : {}
      observe("dispatch", "takt.orchestration", agent, session, {
        decision: action, dispatch_id: event, reason_code: "dispatch_completed",
        ...(typeof transition.in_flight === "number" ? { parallelism: transition.in_flight } : {}),
      }, event)
      if (["admit", "launch", "finish"].includes(action)) {
        observe("unit_lifecycle", "takt.orchestration", agent, session, {
          transition: action, from_state: asString(transition.from_state, ""), to_state: asString(transition.to_state, ""), specialist: agent, reason_code: "dispatch_completed",
        }, event)
      }
      return result
    }
    // stopChild interrupts a child and reports whether it still exists, so a
    // session removed behind the plugin's back is not mistaken for a live turn.
    async function stopChild(id: string): Promise<boolean> {
      const signal = AbortSignal.any([abort.signal, hostWaits.signal, AbortSignal.timeout(60_000)])
      try {
        await ctx.session.get({ sessionID: id }, { signal })
      } catch (error) {
        if (notFound(error)) return false
        throw error
      }
      await ctx.session.interrupt({ sessionID: id }, { signal })
      // An interrupt request is not proof that a lane stopped.
      await ctx.session.wait({ sessionID: id }, { signal })
      return true
    }
    // createChildSession and promptChild spin up and seed the interlocutor
    // switch's lent session.
    async function createChildSession(opts: Parameters<typeof ctx.session.create>[0]): Promise<string> {
      const created = await ctx.session.create(opts)
      return created.id
    }
    async function promptChild(sessionID: string, text: string): Promise<void> {
      await ctx.session.prompt({ sessionID, text }, { signal:AbortSignal.any([abort.signal,AbortSignal.timeout(60_000)]) })
    }
    // returnInterface hands the core's notice of the returned interface to the
    // root orchestrator, which holds the interface again; the lent session gets
    // nothing back.
    async function returnInterface(root: string, ret: InterlocutorReturn): Promise<void> {
      await ctx.session.synthetic({ sessionID: root, text: ret.notice })
    }
    await ctx.tool.hook("execute.after", async (event) => {
      const key = `${event.sessionID}:${event.id}`
      const started = toolStartedAt.get(key)
      toolStartedAt.delete(key)
      const root = invocations.get(key)?.root ?? event.sessionID
      invocations.delete(key)
      observe("tool_activity", "takt.platform", event.agent ?? "harness", root, {
        platform: "opencode", tool: event.tool, phase: "after",
        outcome: asString(event.status, "completed"),
        ...(started === undefined ? {} : { duration_ms: Date.now() - started }),
      })
    })

    // admissionRequest is the `prepare` request admitting one subagent call as a delegation.
    function admissionRequest(a: { delegation: string; root: string; unit: string; specialist: string; caller: string; input: Record<string, unknown>; model?: string; previous?: string }): Request {
      const { input } = a
      return { action: "prepare", event: a.unit, dispatch: a.delegation, session: a.root,
        agent: a.specialist, caller: a.caller, owner: INSTANCE,
        ...(typeof input.sessionID === "string" && input.sessionID ? { child: input.sessionID } : {}),
        ...(input.author_keys === undefined ? {} : { author_keys: input.author_keys }),
        ...(a.model ? { model: a.model } : {}), ...(a.previous ? { previous_model: a.previous } : {}),
        ...(typeof input.seat_reason === "string" && input.seat_reason ? { seat_reason: input.seat_reason } : {}),
        ...(input.seat_substitute === true ? { substitute: true } : {}) }
    }
    // unsettledNotice tells a refused admission which delegations still hold their unit.
    const unsettledNotice = (held: Delegation[]) => {
      const named = held.map(d => `dispatch ${d.dispatch} (unit ${d.unit}, session ${d.child ?? d.caller}, attempt ${d.attempt})`).join(", ")
      return renderPrompt("refusal.vfs.unsettled_delegations", { named })
    }

    // effectiveModel is the model a native launch would run before any override,
    // matching native create/follow-up semantics: the same agent retains its
    // child's model; switching agents uses its configuration if present.
    async function effectiveModel(input: Record<string, unknown>, specialist: string, caller: string, configured: string | undefined) {
      if (typeof input.sessionID !== "string" || !input.sessionID) return configured ?? modelName((await ctx.session.get({ sessionID: caller })).model)
      const existing = await ctx.session.get({ sessionID: input.sessionID })
      return (existing.agent === specialist ? undefined : configured) ?? modelName(existing.model)
    }

    await ctx.tool.hook("execute.before", async (event) => {
      if (event.tool !== "subagent") return
      await reconcileStoredDelegations()
      const root = await rootSession(event.sessionID)
      const input = isResponseObject(event.input) ? event.input : {}
      // The delegation's description names the work unit it executes: a planned
      // unit by its committed identity, and a retry by the same name again. The
      // host call identity tells a transport repetition apart from a retry.
      const unit = asString(input.description, "").trim()
      const specialist = asString(input.agent, "")
      if (!unit) throw refused("missing_unit", specialist, root, new Error(renderPrompt("refusal.vfs.missing_unit")))
      if (!specialist) throw refused("missing_specialist", "", root, new Error(renderPrompt("refusal.vfs.missing_specialist")), unit)
      const delegation = `${event.sessionID}:${event.id}`
      try {
        const configured = modelName((await ctx.agent.get({ agentID: specialist })).data.model)
        const previous = await effectiveModel(input, specialist, event.sessionID, configured)
        const model = modelName(input.model) ?? previous
        const request = admissionRequest({ delegation, root, unit, specialist, caller: event.sessionID, input, model, previous })
        const prepare = async (): Promise<DispatchResponse> => {
          try { return await dispatchAction(request) }
          catch (error) {
            // PR-CRW-13: an unproven model restores the seat only with the user's approval.
            if (error instanceof Error && error.message.includes(SEAT_UNPROVEN) && request.user_confirmed !== true) {
              await confirmWithUser("seat_restore", specialist, { model: model ?? "", reason: asString(input.seat_reason, ""), substitute: String(input.seat_substitute === true) }, event)
              request.user_confirmed = true
              return prepare()
            }
            throw error
          }
        }
        const prepared = await repairOperation(prepare, approval => dispatchAction({ ...request, repair:approval }), event)
        if (!prepared || !isResponseObject(prepared)) throw new Error(`dispatch ${delegation} preparation returned no disposition; retry the same subagent call`)
        if (responseObject(prepared,"dispatch prepare").launchable !== true) throw new Error(renderPrompt("refusal.vfs.already_prepared", { delegation }))
      } catch (error) {
        const cause = error instanceof Error ? error : new Error(String(error))
        const held = await unsettledExecutions().catch(() => [])
        if (held.length) cause.message += unsettledNotice(held)
        throw refused(cause.message.includes("claim_assign") ? "missing_claim" : "admission_denied", specialist, root, cause, unit)
      }
      // This instance already owns the host callback for this native call.
      // Do not start a second wait on it during transport reconciliation.
      watching.add(delegation)
      // Native subagent execution follows this hook. All subsequent observations
      // carry this exact dispatch, including callbacks that arrive after retry.
    })

    // Launch completion is not child termination, delivery, or verification.
    // Bind the native result to this exact attempt; lifecycle reconciliation
    // observes the child's eventual idle/deletion using the existing owner.
    await ctx.tool.hook("execute.after", async (event) => {
      if (event.tool === "subagent") {
        if (event.status === "completed") event.result = { ...event.result, content: truthfulSubagentContent(event.result.content) }
        const delegation = `${event.sessionID}:${event.id}`
        try {
          const d = await execution(delegation)
          if (d === undefined || d.settled) return
          if (event.status === "completed") {
            const child = event.result.metadata?.sessionID
            if (typeof child !== "string" || !child) throw new Error("native subagent result has no sessionID; launch remains unsettled")
            await adoptChild(child, d.agent, d)
          } else {
            await endDelegation(delegation, d)
          }
        } catch (error) {
          // execute.after cannot refuse a launch that already ran. Keep failed
          // observations unsettled for the existing reconciliation to retry.
          console.error("Takt native launch observation", delegation, error)
        } finally { watching.delete(delegation) }
      }
    })

    // An async delegation ends when its child goes idle. There is no tool
    // result to amend: whatever the child said, a question or a missing delivery
    // included, reaches the orchestrator as the child's own notification.
    async function onChildIdle(child: string) {
      const d = await executionFor(child)
      if (!d) return
      await reconcileStoredDelegations()
    }

    async function adoptChild(session: string, agent: string, invocation?: Pick<Delegation,"dispatch"|"attempt">): Promise<Delegation | undefined> {
      if (invocation) return delegationOf(await dispatchAction({action:"attach_child",dispatch:invocation.dispatch,attempt:invocation.attempt,child:session,agent}))
      const current = await executionFor(session, agent)
      if (current) return delegationOf(await dispatchAction({action:"attach_child",dispatch:current.dispatch,child:session,agent}))
      const info = await ctx.session.get({ sessionID: session })
      if (!info.parentID) return undefined
      return delegationOf(await dispatchAction({ action: "attach_child", child: session, agent,
        session: await rootSession(info.parentID), event: (info.title ?? "").trim(),
        caller: info.parentID, created_at: info.time?.created }))
    }

    // endDelegation records the exact attempt's observed termination.
    async function endDelegation(delegation: string, d: Delegation) {
      const request = { action: "finish", event: d.unit, dispatch: delegation, attempt: d.attempt, session: d.root }
      await dispatchAction(request)
    }

    // A native edit never lands on a path a VFS claim holds: that path belongs
    // to the staged work of its unit until it is consolidated or discarded, so
    // editing it natively would race the gate. Only ever tightens a decision.
    await ctx.permission.hook("evaluate", async (event) => {
      if (event.action !== "edit" || event.effect === "deny" || !event.resources.some(resource => resource.length > 0)) return
      try { await takt("edit-guard",event,{resources:event.resources}) }
      catch (error) {
        event.effect = "deny"
        event.message = renderPrompt("refusal.vfs.edit_claimed_path", { reason: error instanceof Error ? error.message : String(error) })
        refused("claimed_path", event.agent ?? "", event.sessionID,new Error(event.message))
      }
    })

    // Before delegation, surface prior-root ownership. The context read
    // introduces no startup gate.
    await ctx.session.hook("context", async (event) => {
      if (event.agent !== ORCHESTRATOR_ID) return
      const root = await rootSession(event.sessionID).catch(error => { console.error("Takt root context unavailable", error); return event.sessionID })
      const claims = await takt("claims", event).then(r => r.claims ?? [], () => undefined)
      if (claims !== undefined) {
        const prior = claims.filter(claim => claim.root_session_id !== root)
        if (prior.length) event.system.push({ type: "text", text: renderPrompt("context.prior_claims", { claims: JSON.stringify(prior) }) })
      }
    })

    // OpenCode reports provider usage on session.usage.updated. Keep only numeric
    // accounting fields; message text, prompts and tool output never cross into
    // Takt telemetry. Event IDs prevent duplicate delivery from counting twice.
    const observedUsageEvents = new Set<string>()
    // observeModelUsage records one usage event against who ran in its session.
    async function observeModelUsage(event: OpenCodeEvent) {
      if (event.type !== "session.usage.updated") return
      if (observedUsageEvents.has(event.id)) return
      observedUsageEvents.add(event.id)
      const { sessionID, cost, tokens } = event.data
      // Usage belongs to the delegation that session runs; a root session's own
      // usage is the harness's.
      const delegation = await executionFor(sessionID).catch(error => { observedUsageEvents.delete(event.id); throw error })
      const attributes = {
        usage_event_id: event.id,
        input_tokens: tokens.input, output_tokens: tokens.output,
        reasoning_tokens: tokens.reasoning, cache_read_tokens: tokens.cache.read,
        cache_write_tokens: tokens.cache.write, cost_usd: cost,
      }
      observe("model_usage", "takt.platform", delegation?.agent ?? "harness", sessionID,
        attributes, delegation?.unit ?? "")
    }

    // A user's acceptance is asked through the local OpenCode service, the
    // endpoint every built-in approval rides on, and answered as an event. The
    // model never supplies it.
    let confirmations: ReturnType<(typeof OpenCode)["make"]> | undefined
    const pendingReplies = new Map<string, { session: string; controller: AbortController; resolve: (reply: string) => void; reject: (error: Error) => void }>()
    let eventsRunning = false
    let disposed = false
    // inactivity fires once per silence of the open host stream; the next host event arms it again.
    let inactivity: ReturnType<typeof setTimeout> | undefined
    const stopInactivity = () => { clearTimeout(inactivity); inactivity = undefined }
    const armInactivity = () => {
      stopInactivity()
      inactivity = setTimeout(() => { if (eventsRunning) void observeStream("inactive") }, STREAM_INACTIVITY_MS)
      inactivity.unref?.()
    }
    function endApprovals(reason: string, session?: string) {
      for (const [id, waiter] of pendingReplies) {
        if (session && waiter.session !== session) continue
        pendingReplies.delete(id)
        waiter.controller.abort()
        waiter.reject(new Error(`${reason}; no approval was granted; request approval again through the original operation`))
      }
    }
    // confirmWithUser asks the user through OpenCode and throws on a refusal.
    // fallback names a real tool call that proceeds when the user cannot be asked
    // here. Without one, the failure to ask is the harness's own error, reported as is.
    async function confirmWithUser(action: string, resource: string, metadata: Record<string, string>, c: { sessionID: string; agent: string; messageID: string; id: string }, fallback?: string) {
      try { await askUser(action, resource, metadata, c) }
      catch (error) {
        if (fallback && error instanceof Error && !error.message.startsWith("Permission denied")) error.message += `; the user could not be asked here: ${fallback}`
        throw error
      }
    }
    async function askUser(action: string, resource: string, metadata: Record<string, string>, c: { sessionID: string; agent: string; messageID: string; id: string }) {
      if (!confirmations) {
        // Loaded only when a user is actually asked: no other path needs the client.
        const [{ OpenCode }, { Service }] = await Promise.all([import("@opencode/client"), import("@opencode/client/service")])
        const endpoint = await Service.discover()
        if (!endpoint) throw new Error("cannot reach the local OpenCode service to ask for confirmation")
        confirmations = OpenCode.make({ baseUrl: endpoint.url, headers: Service.headers(endpoint) })
      }
      startEvents()
      if (disposed) throw new Error("VFS plugin stopped; no approval was granted")
      // The permission service accepts only its own `per` identifier prefix.
      const id = `per_${randomUUID()}`
      const controller = new AbortController()
      const reply = new Promise<string>((resolve, reject) => pendingReplies.set(id, { session: c.sessionID, controller, resolve, reject }))
      void reply.catch(() => undefined)
      try {
        const request = await Promise.race([confirmations.permission.create({ id,
          sessionID: c.sessionID, action, resources: [resource], metadata, agent: c.agent,
          source: { type: "tool", messageID: c.messageID, id: c.id } }, { signal: AbortSignal.any([abort.signal, controller.signal, AbortSignal.timeout(60_000)]) }), reply.then(() => new Promise<never>(() => {}))])
        if (disposed || !eventsRunning || !pendingReplies.has(id)) throw new Error("confirmation channel ended; no approval granted; request approval again")
        if (request.id !== id) throw new Error("permission service changed the confirmation correlation")
        if (request.effect === "deny") throw new Error(`Permission denied: ${action}`)
        if (request.effect !== "allow" && await reply === "reject") throw new Error(`Permission denied: ${action}`)
      } finally { controller.abort(); pendingReplies.delete(id) }
    }
    function startEvents() {
      if (eventsRunning || disposed) return
      eventsRunning = true
      let failed = false
      armInactivity()
      void (async () => {
        for await (const event of ctx.event.subscribe({ signal: abort.signal })) {
          armInactivity()
          // Replies never queue behind context lookups, telemetry or lifecycle IPC.
          if (event.type === "permission.replied") {
            pendingReplies.get(event.data.requestID)?.resolve(event.data.reply)
            continue
          }
          void observeModelUsage(event).catch(error => console.error("Takt usage context unavailable", error))
          if (event.type === "session.created") {
            void onSessionCreated(event.data.sessionID, event.data.agent).catch(error => console.error("Takt session created", error))
          } else if (event.type === "session.idle") {
            const session = event.data.sessionID
            void (async () => {
              // The captured shell delta is imported before the child's finish
              // can cancel its pending shell preparation.
              await releaseShellSession(session, true)
              await onChildIdle(session)
            })().catch(error => console.error("Takt session idle", error))
          } else if (event.type === "session.deleted") {
            endApprovals("session deleted", event.data.sessionID)
            void onSessionDeleted(event.data.sessionID).catch(error => console.error("Takt session deleted", error))
          }
        }
      })().catch(error => { failed = true; console.error("Takt event stream disconnected", error) }).finally(() => {
        eventsRunning = false; confirmations = undefined
        stopInactivity()
        hostWaits.abort(); hostWaits = new AbortController()
        endApprovals("confirmation event stream ended")
        // The plugin's own disposal is closure, a stream error a disconnection,
        // and a stream that just ended EOF: three distinct observations (PR-OBS-BUS-6).
        let cause: "closed" | "disconnected" | "eof" = "eof"
        if (disposed) cause = "closed"
        else if (failed) cause = "disconnected"
        void streamLost(cause)
      })
    }
    // STREAM_READ_ATTEMPTS bounds how often a stream-loss report rereads what is
    // in flight before it gives up: a transient read failure must not drop the
    // unmonitored record of known work.
    const STREAM_READ_ATTEMPTS = 3
    async function reread<T>(read: () => Promise<T>, attempt = 1): Promise<T> {
      try {
        return await read()
      } catch (error) {
        if (attempt >= STREAM_READ_ATTEMPTS) throw error
        return reread(read, attempt + 1)
      }
    }
    // observeStream records the host stream's state once per execution this instance
    // watches (PR-OBS-BUS-5/6), and says which ones. The observation proves nothing
    // about the agent.
    async function observeStream(state: "eof" | "inactive" | "disconnected" | "closed") {
      try {
        const watched = (await reread(unsettledExecutions)).filter(d => d.mine)
        for (const d of watched) observe("stream_liveness", "takt.platform", d.agent, d.root, { stream: HOST_EVENT_STREAM, state }, d.unit, d.attempt)
        return watched
      } catch (error) { console.error("Takt stream state unreported", error); return [] }
    }
    // recordLostCapture marks unmonitored, neither completed nor failed, each
    // execution in watched and each direct activity this instance captures or
    // whose capturing instance is gone. The core ignores a repeat.
    async function recordLostCapture(watched: Delegation[]) {
      await Promise.all(watched.map(d =>
        dispatchAction({ action: "unmonitored", session: d.root, event: d.unit, dispatch: d.dispatch })
          .catch(error => console.error("Takt unmonitored execution unrecorded", d.dispatch, error))))
      const activities = await reread(unsettledActivities).catch(error => { console.error("Takt unmonitored activities unread", error); return [] })
      await markActivitiesUnmonitored(activities.filter(a => (a.own || a.orphaned) && !a.unmonitored))
    }
    // streamLost reports the loss of the host event stream for what this
    // instance watches. Telemetry loss proves nothing about the agent.
    async function streamLost(state: "eof" | "disconnected" | "closed") {
      await recordLostCapture(await observeStream(state))
    }
    startEvents()

    // onSessionCreated learns a child as soon as the host creates it, so one
    // that ends before its first turn still ends its delegation.
    async function onSessionCreated(sessionID: string, agent: string | undefined) {
      if (agent) await adoptChild(sessionID, agent).catch(error => console.error("Takt child adoption", sessionID, error))
    }

    // onSessionDeleted ends what the deleted session held. A deleted child never
    // goes idle, so its background delegation ends here. A temporary
    // interlocutor that died without handing off or being explicitly aborted is
    // reported by the harness itself, so the stack never points at a session
    // that no longer exists.
    async function onSessionDeleted(sessionID: string) {
      await releaseShellSession(sessionID, false).catch(error => console.error("Takt shell release", error))
      const ended = await executionFor(sessionID)
      if (ended) await reconcileStoredDelegations()
      // The core decides from its history whether the session held a root.
      await dispatchAction({ action: "session_deleted", child: sessionID })
        .then(reply => {
          if (reply !== null && isInterlocutorReturn(reply) && reply.root) return returnInterface(reply.root, reply)
        })
        .catch(error => console.error("Takt session_deleted", error))
    }

    await ctx.tool.transform(async (editor) => {
      const subagent = editor.get("subagent")
      if (subagent) {
        // Native tools use Effect schemas; plugin tools may use plain JSON Schema.
        // Extend the native declaration without replacing its execution.
        let schema: ToolInput
        if (typeof subagent.input === "function") {
          const { Schema } = await import("effect")
          if (!Schema.isSchema(subagent.input)) throw new Error("subagent input is not a schema")
          const document = Schema.toJsonSchemaDocument(subagent.input)
          schema = { ...document.schema, $defs: document.definitions }
        } else {
          schema = toolInput(subagent.input)
        }
        editor.update("subagent", (tool) => {
          tool.input = { ...schema, properties: {
            ...toolInput(schema.properties),
            ...("sessionID" in toolInput(schema.properties) ? { sessionID: { ...toolInput(toolInput(schema.properties).sessionID), description: SUBAGENT_SESSION_DESCRIPTION } } : {}),
            model: { ...toolInput(toolInput(schema.properties).model), description: paramDesc("subagent", "model") },
            author_keys: { type: "array", items: str(), minItems: 1, uniqueItems: true,
              description: paramDesc("subagent", "author_keys") },
            seat_reason: str(paramDesc("subagent", "seat_reason")),
            seat_substitute: { type: "boolean", description: paramDesc("subagent", "seat_substitute") },
          } }
        })
      }
      // dispatch is (name, description, action, input) for the ordinary crew
      // dispatch tools below: each is the orchestrator's own declaration, never
      // a specialist's, so every one requires the calling session to be the
      // root interlocutor.
      // after runs once the history accepted the request, with the root session it named.
      const dispatch = <Args extends ToolInput>(name: string, description: string, action: string, input: ReturnType<typeof obj>, fields: (args: Args) => Record<string, unknown>) =>
        editor.add({
          name, description, input,
          async execute(value: unknown, c) {
            const args = toolInput<Args>(value)
            const session = await rootSession(c.sessionID)
            if (c.sessionID !== session) throw new Error(`${name} belongs to the root orchestrator`)
            const result = await governed(c, { action, session, ...fields(args) })
            return { content: JSON.stringify(result) }
          },
        })

      dispatch("dispatch_commit", toolDesc("dispatch_commit"),
        "commit",
        obj({
          version: str(paramDesc("dispatch_commit", "version")),
          base_version: str(paramDesc("dispatch_commit", "base_version")),
          plan: { type: "array", items: obj({
            unit: str(paramDesc("dispatch_commit", "plan_unit")), contract: str(paramDesc("dispatch_commit", "plan_contract")),
            prerequisites: { type: "array", items: { type: "string" }, description: paramDesc("dispatch_commit", "plan_prerequisites") },
          }, ["unit", "contract"]), description: paramDesc("dispatch_commit", "plan") },
          qa_round: obj({ objective: str(paramDesc("dispatch_commit", "qa_round_objective")),
            units: { type: "array", items: str(), minItems: 1, uniqueItems: true, description: paramDesc("dispatch_commit", "qa_round_units") },
          }, ["objective", "units"]),
          withdraw: { type: "array", items: { type: "string" }, description: paramDesc("dispatch_commit", "withdraw") },
        }, ["version", "plan"]),
        (args: { version: string; base_version?: string; withdraw?: string[]; qa_round?: { objective: string; units: string[] }; plan: { unit: string; contract: string; prerequisites?: string[] }[] }) =>
          ({
            version: args.version,
            ...(args.qa_round ? { qa_round: args.qa_round } : {}),
            ...(args.base_version ? { base_version: args.base_version } : {}),
            plan: args.plan.map(u => ({ unit: u.unit, contract: u.contract, prerequisites: u.prerequisites ?? [] })),
            ...(args.withdraw?.length ? { withdrawals: args.withdraw } : {}),
          }))

      editor.add({
        name: "dispatch_inputs",
        description: toolDesc("dispatch_inputs"),
        input: obj({
          work_unit_id: str(paramDesc("dispatch_inputs", "work_unit_id")),
          result_ids: { type: "array", items: { type: "integer" }, description: paramDesc("dispatch_inputs", "result_ids") },
          units: { type: "array", items: str(), description: paramDesc("dispatch_inputs", "units") },
        }, ["work_unit_id", "result_ids"]),
        async execute(value: unknown, c) {
          const args = toolInput<{ work_unit_id: string; result_ids?: number[]; units?: string[] }>(value)
          const session = await rootSession(c.sessionID)
          orchestratorOnly(c, "dispatch_inputs")
          if (c.sessionID !== session) throw new Error("dispatch_inputs belongs to the root orchestrator")
          const unit = asString(args.work_unit_id, "").trim()
          const ids = Array.isArray(args.result_ids) ? args.result_ids : []
          const units = Array.isArray(args.units) ? args.units.map(u => asString(u, "").trim()).filter(Boolean) : []
          if (!unit) throw new Error("Name the unit the delegation will be named after")
          const declared = toolInput<{ inputs: { id: number; title: string; unit?: string }[]; undelivered?: string[] }>(
            await governed(c, { action: "inputs", session, event: unit, result_ids: ids, units }))
          return { content: declaredInputs(unit, declared.inputs, declared.undelivered ?? []) }
        },
      })

      editor.add({ name: "claim_list", description: toolDesc("claim_list"), input: obj({}, []), async execute(value: unknown, c) {
        toolInput(value)
        orchestratorOnly(c, "VFS claims")
        return { content: JSON.stringify(await takt("claims", c)) }
      } })
      // The target instance is named once and serves as both the binding agent
      // and the catalog specialist, exactly as the launched subagent binds.
      editor.add({ name: "claim_assign", description: toolDesc("claim_assign"), input: obj({
        work_unit_id: str(paramDesc("claim_assign", "work_unit_id")),
        agent: str(paramDesc("claim_assign", "agent")),
        scope: { type: "array", items: str(), description: paramDesc("claim_assign", "scope") },
        author_key: str(paramDesc("claim_assign", "author_key")),
        reads: { type: "array", items: str(), description: paramDesc("claim_assign", "reads") },
      }, ["work_unit_id", "agent", "scope"]), async execute(input: unknown, c) {
        const args = toolInput(input)
        orchestratorOnly(c, "VFS claim assignment")
        const unit = asString(args.work_unit_id, "").trim()
        return { content: JSON.stringify(await takt("assign", c, { work_unit_id: unit, agent_id: args.agent, specialist: args.agent, scope: args.scope, reads: args.reads, author_key: args.author_key })) }
      } })
      editor.add({ name: "claim_release", description: toolDesc("claim_release"), input: obj({ claim_key: str(paramDesc("claim_release", "claim_key")) }, ["claim_key"]), async execute(value: unknown, c) {
        const args = toolInput<{ claim_key: string }>(value)
        orchestratorOnly(c, "VFS claim release")
        // The core decides under its lock: staged work and a claim whose unit
        // is still running are refused there, each naming its exit.
        const released = await takt("release", c, { key: args.claim_key })
        return { content: JSON.stringify(released) }
      } })

      dispatch("dispatch_activity_start", toolDesc("dispatch_activity_start"), "activity_start", obj({ activity_id: str(paramDesc("dispatch_activity_start", "activity_id")), node_kind: { type: "string", enum: ["orchestrator"], description: paramDesc("dispatch_activity_start", "node_kind") } }, ["activity_id", "node_kind"]), (args: { activity_id: string; node_kind: string }) => ({ ...args, owner: INSTANCE }))
      dispatch("dispatch_activity_finish", toolDesc("dispatch_activity_finish"), "activity_finish", obj({ activity_id: str(paramDesc("dispatch_activity_finish", "activity_id")), node_kind: { type: "string", enum: ["orchestrator"], description: paramDesc("dispatch_activity_finish", "node_kind") }, outcome: { type: "string", enum: ["completed", "failed", "interrupted"], description: paramDesc("dispatch_activity_finish", "outcome") } }, ["activity_id", "node_kind", "outcome"]), (args: { activity_id: string; node_kind: string; outcome: string }) => args)

      dispatch("dispatch_declare_recovery", toolDesc("dispatch_declare_recovery"),
        "recovery",
        obj({
          objective: str(paramDesc("dispatch_declare_recovery", "objective")),
          result: str(paramDesc("dispatch_declare_recovery", "result")),
          scope: { type: "array", items: { type: "string" }, description: paramDesc("dispatch_declare_recovery", "scope") },
          actions: { type: "number", description: paramDesc("dispatch_declare_recovery", "actions") },
          attempts: { type: "number", description: paramDesc("dispatch_declare_recovery", "attempts") },
        }, ["objective", "result", "scope", "actions", "attempts"]),
        /** Copies the recovery declaration, using its objective as the history event's work unit. */
        (args: { objective: string; result: string; scope: string[]; actions: number; attempts: number }) => ({ ...args, event: args.objective }))

      dispatch("dispatch_close_recovery", toolDesc("dispatch_close_recovery"),
        "recovered",
        obj({
          objective: str(paramDesc("dispatch_close_recovery", "objective")),
          evidence: str(paramDesc("dispatch_close_recovery", "evidence")),
          demonstrated: { type: "boolean", description: paramDesc("dispatch_close_recovery", "demonstrated") },
        }, ["objective", "evidence", "demonstrated"]),
        /** Maps the recovery result to history fields, using the objective as the work unit and demonstrated as pass. */
        (args: { objective: string; evidence: string; demonstrated: boolean }) => ({ event: args.objective, objective: args.objective, evidence: args.evidence, pass: args.demonstrated }))

      dispatch("dispatch_restore", toolDesc("dispatch_restore"),
        "restore",
        obj({
          objective: str(paramDesc("dispatch_restore", "objective")),
        }, ["objective"]),
        (args: { objective: string }) => ({ objective: args.objective }))

      // A limit exception is the user's decision: the harness asks them itself and
      // records it only on their agreement (PR-HAR-22).
      editor.add({
        name: "dispatch_exception",
        description: toolDesc("dispatch_exception"),
        input: obj({
          event: str(paramDesc("dispatch_exception", "event")),
          bound: { type: "string", enum: EXCEPTION_BOUNDS, description: paramDesc("dispatch_exception", "bound") },
          objective: str(paramDesc("dispatch_exception", "objective")),
          allowance: { type: "number", description: paramDesc("dispatch_exception", "allowance") },
          evidence: str(paramDesc("dispatch_exception", "evidence")),
        }, ["bound", "allowance", "evidence"]),
        async execute(value: unknown, c) {
          const args = toolInput<{ event?: string; bound: string; objective?: string; allowance: number; evidence: string }>(value)
          const session = await rootSession(c.sessionID)
          if (c.sessionID !== session) throw new Error("dispatch_exception belongs to the root orchestrator")
          await confirmWithUser("dispatch_exception", args.bound, { bound: args.bound, objective: args.objective ?? "", allowance: String(args.allowance), evidence: args.evidence }, c)
          const result = await governed(c, { action: "exception", session, event: args.event, bound: args.bound, objective: args.objective, allowance: args.allowance })
          return { content: JSON.stringify(result) }
        },
      })

      dispatch("dispatch_contest", toolDesc("dispatch_contest"),
        "contest",
        obj({
          event: str(paramDesc("dispatch_contest", "event")),
          attempt: str(paramDesc("dispatch_contest", "attempt")),
        }, ["event"]),
        (args: { event: string; attempt?: string }) => ({ event: args.event, attempt: args.attempt ?? "" }))

      editor.add({
        name: "dispatch_abandon",
        description: toolDesc("dispatch_abandon"),
        input: obj({ dispatch: str(paramDesc("dispatch_abandon", "dispatch")) }, ["dispatch"]),
        async execute(value: unknown, c) {
          const args = toolInput<{ dispatch: string }>(value)
          orchestratorOnly(c, "dispatch_abandon")
          if (c.sessionID !== await rootSession(c.sessionID)) throw new Error("dispatch_abandon belongs to the root orchestrator")
          const d = await execution(asString(args.dispatch, ""))
          if (!d) throw new Error(renderPrompt("refusal.vfs.dispatch_abandon_unknown", { dispatch: args.dispatch }))
          if (d.settled) return { content: `Dispatch ${d.dispatch} already returned` }
          const request = { action: "abandon", session: d.root, dispatch: d.dispatch, event: d.unit }
          await confirmWithUser("dispatch_abandon", d.dispatch, { unit: d.unit, agent: d.agent, root: d.root, session: d.child ?? d.caller }, c)
          // The user's decision stands even when the host cannot confirm the stop.
          const unstopped = d.child ? await stopChild(d.child).then(() => "", error => `; its session ${d.child} could not be confirmed stopped: ${error instanceof Error ? error.message : error}`) : ""
          await governed(c, request)
          watching.delete(d.dispatch)
          return { content: `Abandoned unit ${d.unit} (dispatch ${d.dispatch}); its staged work is kept${unstopped}` }
        },
      })

      // dispatch_switch cannot ride the dispatch() factory above unmodified: it
      // creates the temporary interlocutor's own session around the call to
      // Go, and fields() only ever contributes a plain object into that one
      // call. It repeats dispatch()'s own root-only guard instead (IR-19). No
      // agent holds an abort: the user's /takt-abort command and the harness
      // originate one (IR-24).
      //
      // The user's decision is the harness's to obtain, not the model's word
      // (IR-20, IR-24): each transition asks through the permission service
      // before anything changes. The confirmation actions are named apart from
      // the tools, so no tool grant in the agent's rules answers them.
      editor.add({
        name: "dispatch_switch",
        description: toolDesc("dispatch_switch"),
        input: obj({
          target_agent: str(paramDesc("dispatch_switch", "target_agent")),
          objective: str(paramDesc("dispatch_switch", "objective")),
          requirements: { type: "array", items: { type: "integer" }, description: paramDesc("dispatch_switch", "requirements") },
          client: str(paramDesc("dispatch_switch", "client")),
        }, ["target_agent", "objective", "requirements", "client"]),
        async execute(value: unknown, c) {
          const args = toolInput<{ target_agent: string; objective: string; requirements: number[]; client: string }>(value)
          const session = await rootSession(c.sessionID)
          if (c.sessionID !== session) throw new Error("dispatch_switch belongs to the root orchestrator")
          // A switch that would be refused is returned to the agent before the user is asked.
          await dispatchAction({ action: "validate_switch", session, agent: args.target_agent, requirements: args.requirements })
          await confirmWithUser("interlocutor_switch", args.target_agent, { target_agent: args.target_agent, objective: args.objective, requirements: entryRefs(args.requirements), client: args.client }, c)
          const requirements = args.requirements.length
            ? renderPrompt("interlocutor.switch_requirements_some", { entries: entryRefs(args.requirements), read_tool: MEMORY_TOOLS.read })
            : renderPrompt("interlocutor.switch_requirements_none")
          const text = renderPrompt("interlocutor.switch_opening", { objective: args.objective, requirements, client: args.client })
          const child = await createChildSession({
            title: `Takt switch: ${args.target_agent}`,
            agent: args.target_agent,
            metadata: { takt_switch: session },
          })
          let result: DispatchResponse
          try {
            result = await governed(c, { action: "switch", session, child, agent: args.target_agent, requirements: args.requirements })
          } catch (error) {
            // The switch never registered, so the child is stopped rather than
            // left dangling unprompted.
            await stopChild(child)
            throw error
          }
          try {
            await promptChild(child, text)
          } catch (error) {
            // The switch is recorded, so the failed child is aborted to clear the holder.
            await dispatchAction({ action: "abort_switch", session, child, evidence: "the switched session could not be prompted", origin: "harness" })
              .catch(abortError => console.error("Takt interlocutor abort_switch", abortError))
            await stopChild(child).catch(() => undefined)
            throw error
          }
          return { content: JSON.stringify(result) }
        },
      })
      editor.add({
        name: "dispatch_handoff",
        description: toolDesc("dispatch_handoff"),
        input: obj({
          result: { type: "string", enum: ["Standard", "EarlyHandoff", "TechFault", "Outraged"], description: paramDesc("dispatch_handoff", "result") },
          additional_context: str(paramDesc("dispatch_handoff", "additional_context")),
          extra_artifacts: { type: "array", items: { type: "string" }, description: paramDesc("dispatch_handoff", "extra_artifacts") },
          result_ids: { type: "array", items: { type: "integer" }, description: paramDesc("dispatch_handoff", "result_ids") },
        }, ["result", "additional_context", "extra_artifacts", "result_ids"]),
        // Not built on dispatch(): the caller here is the temporary holder,
        // never the root, so dispatch()'s root-only guard does not apply.
        async execute(value: unknown, c) {
          const args = toolInput<{ result: string; additional_context: string; extra_artifacts: string[]; result_ids: number[] }>(value)
          const session = await rootSession(c.sessionID)
          // A handoff that would be refused is returned to the holder before the user is asked.
          await dispatchAction({ action: "validate_handoff", session, child: c.sessionID, result: args.result, extra_artifacts: args.extra_artifacts, result_ids: args.result_ids })
          await confirmWithUser("interlocutor_handoff", c.agent, { result: args.result, additional_context: args.additional_context, extra_artifacts: (args.extra_artifacts ?? []).join(", "), result_ids: (args.result_ids ?? []).join(", ") }, c)
          // A refused handoff throws here, and its reason is all the holder
          // receives: it corrects and retries.
          const envelope = await governed(c, {
            action: "handoff", session, child: c.sessionID, agent: c.agent,
            result: args.result, additional_context: args.additional_context, extra_artifacts: args.extra_artifacts, result_ids: args.result_ids,
          })
          if (envelope === null || !isInterlocutorReturn(envelope)) throw new Error("dispatch_handoff: the core returned no envelope")
          await returnInterface(session, envelope)
          // The holder has no further active work; its turn ends once this call returns.
          const holder = c.sessionID
          setTimeout(() => void stopChild(holder).catch(() => undefined), 0)
          return { content: "" }
        },
      })

      // The recorded journal and execution history are read from their stores
      // without the workspace lock; neither holds file content.
      editor.add({
        name: "vfs_inspect",
        description: toolDesc("vfs_inspect"),
        input: obj({
          source: { type: "string", enum: ["journal", "history"], description: paramDesc("vfs_inspect", "source") },
          session: str(paramDesc("vfs_inspect", "session")),
          unit: str(paramDesc("vfs_inspect", "unit")),
          after: { type: "integer", minimum: -1, description: paramDesc("vfs_inspect", "after") },
          limit: { type: "integer", minimum: 1, maximum: INSPECT_PAGE_LIMIT, description: paramDesc("vfs_inspect", "limit", { max_limit: String(INSPECT_PAGE_LIMIT) }) },
        }, ["source"]),
        async execute(input: unknown) {
          const args = toolInput<{ source: "journal" | "history"; session?: string; unit?: string; after?: number; limit?: number }>(input)
          if (args.source !== "journal" && args.source !== "history") throw new Error("source must be journal or history")
          const flags = [
            ...args.session ? ["--session", args.session] : [],
            ...args.unit ? ["--unit", args.unit] : [],
            ...args.after !== undefined ? ["--after", String(args.after)] : [],
            ...args.limit !== undefined ? ["--limit", String(args.limit)] : [],
          ]
          const proc = Bun.spawn([TAKT_AI, "vfs", args.source, "--workspace", workspace, "--state", stateDir(), ...flags], {
            detached: true, stdin: "ignore", stdout: "pipe", stderr: "pipe", cwd: workspace,
          })
          const [out, err, code] = await ipcOutput(proc, `vfs ${args.source}`)
          if (code !== 0) throw new Error(`takt-ai vfs ${args.source} exited ${code}: ${err || out}`)
          return { content: out }
        },
      })
      editor.add({
        name: "vfs_bind",
        description: toolDesc("vfs_bind"),
        input: obj({
          scope: { type: "array", items: { type: "string" }, description: paramDesc("vfs_bind", "scope") },
        }, ["scope"]),
        async execute(input: unknown, c) {
          const args = toolInput<{ scope: string[] }>(input)
          if (isResponseObject(input) && input.author_key !== undefined) throw new Error(renderPrompt("refusal.vfs.vfs_bind_verifier"))
          const who = { ...await callerBlock(c), work_unit_id: await delegatedUnit(c.sessionID) }
          const consumed = (await executionFor(c.sessionID, c.agent))?.inputs ?? []
          const res = await taktUnlocked("bind", { ...who, scope: args.scope })
          const key = res.key ?? (() => { throw new Error("vfs bind response omitted key") })()
          return { content: `Bound as ${c.agent} to scope ${JSON.stringify(args.scope)}; unit ${who.work_unit_id}, attempt ${res.attempt_id}; invariants ${entryRefs(consumed) || "none"} at version ${res.invariants_version}; author_key ${key}` }
        },
      })
      // authorKey passes a model-named author_key through only when it is a string.
      const authorKey = (value: unknown) => typeof value === "string" ? { author_key: value } : {}
      editor.add({
        name: "vfs_write",
        description: toolDesc("vfs_write"),
        input: obj({
          path: str(),
          content: str(paramDesc("vfs_write", "content")),
          author_key: str(paramDesc("vfs_write", "author_key")),
        }, ["path", "content"]),
        async execute(input: unknown, c) {
          const args = toolInput<{ path: string; content: string; author_key?: string }>(input)
          const res = mutated(await takt("op", c, { call_id: randomUUID(), action: "create", path: args.path, content: args.content, ...authorKey(args.author_key) }))
          return { content: `Staged ${args.path} at revision ${res.revision} (delta_hash ${res.delta_hash})` }
        },
      })
      editor.add({
        name: "vfs_read",
        description: toolDesc("vfs_read"),
        input: obj({ path: str(paramDesc("vfs_read", "path")), author_key: str(paramDesc("vfs_read", "author_key")) }, ["path"]),
        async execute(input: unknown, c) {
          const args = toolInput<{ path: string; author_key?: string }>(input)
          const res = await takt("op", c, { call_id: randomUUID(), action: "read", path: args.path, ...authorKey(args.author_key) })
          // The verdict covers the state of the last read. Files read under an
          // earlier state may be outdated, so the read that moved it names them.
          const outdated = res.outdated_paths ?? []
          const note = outdated.length > 0
            ? renderPrompt("directive.vfs_read_outdated", { paths: outdated.join(", ") })
            : ""
          // A staged file exists nowhere else for the reader to open, so the host
          // must hand it over whole instead of cutting it at its tool-output cap.
          return { content: `${note}${res.content ?? ""}`, metadata: { truncated: false } }
        },
      })
      editor.add({
        name: "vfs_delete",
        description: toolDesc("vfs_delete"),
        input: obj({
          path: str(),
          author_key: str(paramDesc("vfs_delete", "author_key")),
        }, ["path"]),
        async execute(input: unknown, c) {
          const args = toolInput<{ path: string; author_key?: string }>(input)
          const res = mutated(await takt("op", c, { call_id: randomUUID(), action: "delete", path: args.path, ...authorKey(args.author_key) }))
          return { content: `Staged deletion of ${args.path} at revision ${res.revision} (delta_hash ${res.delta_hash})` }
        },
      })
      editor.add({
        name: "vfs_discard",
        description: toolDesc("vfs_discard"),
        input: obj({
          author_key: str(paramDesc("vfs_discard", "author_key")),
        }, ["author_key"]),
        async execute(input: unknown, c) {
          const args = toolInput<{ author_key: string }>(input)
          // Discarding is the orchestrator's backtracking decision (PR-VFS-CSL-5).
          orchestratorOnly(c, "vfs_discard")
          if (typeof args.author_key !== "string") throw new Error("author_key must be a string")
          await takt("discard", c, { author_key: args.author_key })
          return { content: "Discarded staged delta" }
        },
      })
      editor.add({
        name: "vfs_verify",
        description: toolDesc("vfs_verify"),
        input: obj({
          author_key: str(paramDesc("vfs_verify", "author_key")),
          pass: { type: "boolean", description: paramDesc("vfs_verify", "pass") },
          finding: str(paramDesc("vfs_verify", "finding")),
        }, ["author_key", "pass"]),
        async execute(input: unknown, c) {
          const args = toolInput<{ author_key: string; pass: boolean; finding?: string }>(input)
          if (typeof args.author_key !== "string") throw new Error("author_key must be a string")
          // The core judges through the caller's gate over this author, at the
          // state that gate last read.
          await takt("verify", c, { call_id: randomUUID(), author_key: args.author_key, pass: args.pass, finding: args.finding ?? "" })
          return { content: args.pass ? "Verdict: passed" : `Verdict: rejected — ${args.finding}` }
        },
      })
      editor.add({
        name: "vfs_consolidate",
        description: toolDesc("vfs_consolidate"),
        input: obj({
          author_key: str(paramDesc("vfs_consolidate", "author_key")),
          checkpoint: str(paramDesc("vfs_consolidate", "checkpoint")),
        }, ["author_key"]),
        async execute(input: unknown, c) {
          const args = toolInput<{ author_key: string; checkpoint?: string }>(input)
          if (typeof args.author_key !== "string") throw new Error("author_key must be a string")
          // Consolidation is the orchestrator's act over work its own delegations
          // staged (PR-DAG-REP-5).
          if (c.agent !== ORCHESTRATOR_ID) throw new Error("vfs_consolidate belongs to the orchestrator")
          // consolidate asks the core, passing the user's acceptance when given.
          const consolidate = (accepted: Request = {}) => takt("consolidate", c, {
            author_key: args.author_key, checkpoint: args.checkpoint, ...accepted,
          })
          try {
            try {
              await consolidate()
            } catch (error) {
              if (!(error instanceof VFSRefusal) || !error.message.includes(FAILING_VERDICT)) throw error
              const { revision, delta_hash: hash } = error.response
              if (typeof revision !== "number" || typeof hash !== "string") throw new Error(`${error.message}; the refusal omitted the staged state it refused`, { cause: error })
              // The core's refusal lists every failing finding; the user sees them.
              await confirmWithUser("vfs_consolidate_failing", args.author_key, { author_key: args.author_key, findings: error.message }, c,
                renderPrompt("refusal.vfs.consolidate_failing_fallback", { author_key: args.author_key }))
              // The acceptance covers exactly the staged state the refusal named.
              await consolidate({ accept_failing: true, accepted_revision: revision, accepted_hash: hash })
            }
          } catch (error) {
            if (error instanceof Error && /physical base changed|recovery required/.test(error.message))
              error.message += renderPrompt("refusal.vfs.consolidate_base_changed")
            throw error
          }
          return { content: "Consolidated to workspace" }
        },
      })
      editor.add({
        name: "deliver_result",
        description: toolDesc("deliver_result", { record_tool: MEMORY_TOOLS.record }),
        input: obj({
          result_ids: { type: "array", items: { type: "integer" }, minItems: 1, description: paramDesc("deliver_result", "result_ids") },
          blocker: { type: "boolean", description: paramDesc("deliver_result", "blocker") },
        }, ["result_ids"]),
        async execute(input: unknown, c) {
          const args = toolInput<{ result_ids: number[]; blocker?: boolean }>(input)
          // Only the session a running delegation runs in delivers for it: an
          // earlier attempt's session of the same unit never credits this one.
          const call=c.id ? invocations.get(`${c.sessionID}:${c.id}`) : undefined
          const running = await adoptChild(c.sessionID, c.agent,call)
          const count = Array.isArray(args.result_ids) ? args.result_ids.length : 0
          if (!running) {
            await governed(c, { action: "validate_results", session: c.sessionID, agent: c.agent, result_ids: args.result_ids })
            // A delegated session that runs no delegation has nobody to deliver
            // to; a top-level session (an interlocutor) hands off its results itself.
            if ((await ctx.session.get({ sessionID: c.sessionID })).parentID) {
              throw new Error(renderPrompt("refusal.vfs.deliver_result_no_delegation"))
            }
            return { content: renderPrompt("directive.deliver_result_lent_interface", { count: String(count) }) }
          }
          await governed(c, { action: "deliver", child: c.sessionID, agent: c.agent, dispatch:call?.dispatch ?? running.dispatch,attempt:call?.attempt ?? running.attempt,result_ids: args.result_ids, blocker: args.blocker === true })
          return { content: `Delivered ${count} result id(s)` }
        },
      })
      // Keep OpenCode's native readers as the execution path. The VFS reserves
      // their exact read scope across the complete native call; it never
      // reconstructs their output or transports file contents through IPC.
      for (const item of editor.list()) {
        const name = item.id
        if (!["read", "grep", "glob"].includes(name) && !/codegraph/i.test(name)) continue
        editor.update(name, (tool) => {
          const native = tool.execute
          tool.execute = async (value, c) => {
            if (!VFS_AUTHORS.includes(c.agent)) return native(value,c)
            const args = toolInput(value)
            const pathValue = nativeReadPath(args)
            // Reconciliation finds a reservation only by the host's own call id.
            if (typeof c.id !== "string" || !c.id) throw new Error(`${name} call has no host call id, so its read reservation could never be reconciled; issue the call again`)
            const callID = c.id
            await takt("native-inspection-start", c, {
              call_id: callID, tool: name, path: /codegraph/i.test(name) ? "" : pathValue,
            })
            let result: Awaited<ReturnType<typeof native>> | undefined
            let failure: unknown
            try { result = await native(value,c) } catch (error) { failure = error }
            try { await takt("native-inspection-end", c, { call_id: callID }) }
            catch (error) {
              if (failure !== undefined) throw new AggregateError([failure,error], "native inspection and reservation release failed", { cause: error })
              throw error
            }
            if (failure !== undefined) throw failure
            return result!
          }
        })
      }
    })

    // bindAuthor adopts the claim the harness assigned. It returns what an agent
    // needs to bind itself when adoption was refused, and nothing when it succeeded.
    async function bindAuthor(event: Caller, scope: string[]): Promise<string> {
      try { await takt("bind", event, { scope }) }
      catch (error) { return renderPrompt("context.author_bind_failed", { error: error instanceof Error ? error.message : String(error) }) }
      return ""
    }
    // Delegated VFS context is granted per exact agent instance and only while
    // its exact root/unit/identity claim remains pending or active. Permission
    // rules are read from that instance; no role defaults are inferred here.
    await ctx.session.hook("context", async (event) => {
      if (!event.sessionID || !event.agent) return
      const removeVFS = () => { for (const name of VFS_TOOL_NAMES) delete event.tools?.[name] }
      try {
        const info = await ctx.session.get({ sessionID: event.sessionID })
        if (!info.parentID) return
        // Learned first: nothing this context does later may keep it from its delegation.
        const delegation = await adoptChild(event.sessionID, event.agent)
        if (delegation?.identity?.length) event.system = withoutIdentity(event.system, delegation.identity)
        const agent = await ctx.agent.get({ agentID: event.agent })
        const rules = agent.data.permissions
        const allowed = VFS_TOOL_NAMES.filter(name => allowsExplicitly(rules, name))
        const root = await rootSession(event.sessionID)
        const unit = await delegatedUnit(event.sessionID)
        const consumed = delegation?.inputs
        if (consumed) event.system.push({ type: "text", text: invariantsNotice(unit, consumed, delegation?.undelivered) })
        if (delegation && deliversArtifact(event.agent)) event.system.push({ type: "text", text: renderPrompt("context.deliver_result_reminder", { unit }) })
        const claim = (await findClaims(event, root, unit, event.agent, ["pending", "active"]))[0]
        if (!claim || allowed.length === 0) { removeVFS(); return }
        for (const name of VFS_TOOL_NAMES) if (!allowed.includes(name)) delete event.tools?.[name]
        if (claim.author_key) {
          // A gate arrives bound: adopting each preassigned gate and naming the
          // author's files is the delivery of the assignment, never the verifier's task.
          delete event.tools?.vfs_bind
          const { gates = [] } = await takt("adopt-gates", event)
          event.system.push({ type: "text", text: gateNotice(unit, allowed, gates) })
          return
        }
        // An author arrives bound too: the harness adopts the claim it assigned,
        // so the agent never echoes a scope the harness already holds. A refused
        // adoption leaves vfs_bind to the agent, with the reason.
        const unbound = claim.pending && allowed.includes("vfs_bind") ? await bindAuthor(event, claim.scope ?? []) : ""
        if (!unbound) delete event.tools?.vfs_bind
        event.system.push({ type: "text", text: authorNotice(unit, claim.scope ?? [], claim.reads ?? [], allowed, unbound) })
      } catch (error) { console.error("Takt delegated VFS context",error); removeVFS() }
    })

    // Renew this instance's lease now and on every interval. Each later renewal
    // also reconciles what this instance owns or what an expired owner left
    // behind; at startup the first idle, pump or launch reconciles, as before.
    const renewLease = () => dispatchAction({ action: "lease", owner: INSTANCE })
    const leaseTimer = setInterval(() => renewLease().then(reconcileStoredDelegations)
      .catch(error => console.error("Takt lease renewal", error)), LEASE_RENEW_INTERVAL_MS)
    // The renewal never keeps the host process alive on its own.
    leaseTimer.unref?.()
    void renewLease().catch(error => console.error("Takt lease renewal", error))
    return () => { clearInterval(leaseTimer); stopInactivity(); disposed = true; endApprovals("VFS plugin stopped"); abort.abort() }
  },
})
