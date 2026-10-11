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
const IPC_VERSION = 5

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

type VFSShellResponse = { decision: "allow" | "ask" | "deny"; reason: string; writable?: string[]; scratch?: string; protected?: string[]; private?: string[]; capture?: boolean; cwd?: string; confirm?: string }
// AdoptedGate is one gate the core adopted for the calling host session.
type AdoptedGate = { key: string; author_key: string; scope: string[]; error?: string }
type Repair = { kind: "manifest" | "cycle"; id: string; author: string; paths: string[]; state: string; preserve: boolean; reason: string }
type VFSResponse = {
 repair?: Repair
  stop_cycle?: { cycle:string; lanes:Array<{dispatch:string;attempt:string;child:string}> }
  ok: boolean; key?: string; content?: string; revision?: number; delta_hash?: string
  error?: string; attempt_id?: string; invariants_version?: string
  shell?: VFSShellResponse; claims?: OwnershipClaim[]; outdated_paths?: string[]; gates?: AdoptedGate[]
}
type GCPlan = Record<string, unknown> & { session_id: string; cycle_id: string; mandate_class: string; delta: unknown[]; closure: string[]; reachability: string }
type GCCycle = Record<string, unknown> & { phase: string; plan: GCPlan; scope: string[]; sessions: Record<string, string>; report: unknown; started: string; author_key?: string }
type StagedView = { revision: number; delta_hash: string; files: Record<string, string | null> }
type CoordinatorResponse = Record<string, unknown> & {
  version: number; units: number; mutations: number; cursor: number; deferrals: number
  next_mandate: number; requested: boolean; cycle?: GCCycle
  history?: GCCycle[]
}
type HandoffEnvelope = { result: string; additional_context: string; extra_artifacts: string[]; memory: number[]; child?: string; aborted?: boolean; root?: string }
type NotHeld = { aborted: false }
type Delegation = { dispatch: string; unit: string; root: string; attempt: string; agent: string; caller: string; background?: boolean; child?: string; admitted_at: number; inputs: number[]; delivery?: number[]; cycle?: string; role?: string; prompted?: string; terminated?: boolean; settled?: boolean; owner?: string }
type DispatchResponse = GCPlan | GCCycle | StagedView | CoordinatorResponse | HandoffEnvelope | NotHeld | Record<string, unknown> | null
const delegationOf = (value: unknown): Delegation | undefined =>
  value && isResponseObject(value) && typeof value.dispatch === "string" && typeof value.attempt === "string" && typeof value.root === "string" && typeof value.unit === "string" && typeof value.agent === "string" ? value as Delegation : undefined
export function isCoordinatorResponse(value: DispatchResponse): value is CoordinatorResponse {
  return value !== null && "version" in value && "units" in value
}
export function isGCCycleResponse(value: DispatchResponse): value is GCCycle {
  return value !== null && isResponseObject(value) && isGCCycle(value)
}
export function isHandoffEnvelope(value: Record<string, unknown>): value is HandoffEnvelope {
  return typeof value.result === "string" && typeof value.additional_context === "string" &&
    Array.isArray(value.extra_artifacts) && value.extra_artifacts.every(item => typeof item === "string") &&
    Array.isArray(value.memory) && value.memory.every(item => typeof item === "number")
}
type OwnershipClaim = { key?: string; author_key?: string; root_session_id: string; work_unit_id: string; agent_id: string; target_instance: string; pending?: boolean; active?: boolean; staged?: boolean; scope?: string[] }
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
      !isOptional(value.writable, isStringArray) || !isOptional(value.protected, isStringArray) || !isOptional(value.private, isStringArray)) return undefined
  const { decision, reason, cwd, scratch, confirm, capture, writable, protected: protectedPaths, private: privatePaths } = value
  return { decision, reason, ...(isString(cwd) ? { cwd } : {}),
    ...(isString(scratch) ? { scratch } : {}), ...(isString(confirm) ? { confirm } : {}),
    ...(isBoolean(capture) ? { capture } : {}),
    ...(isStringArray(writable) ? { writable } : {}),
    ...(isStringArray(protectedPaths) ? { protected: protectedPaths } : {}), ...(isStringArray(privatePaths) ? { private: privatePaths } : {}) }
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
  return [{ key: claim.key, root_session_id: claim.root_session_id, work_unit_id: claim.work_unit_id, agent_id: claim.agent_id, target_instance: claim.target_instance, ...(claim.pending === true ? { pending: true } : {}), ...(claim.active === true ? { active: true } : {}), ...(claim.staged === true ? { staged: true } : {}), ...(Array.isArray(claim.scope) ? { scope: claim.scope.filter((path): path is string => typeof path === "string") } : {}), ...(typeof claim.author_key === "string" ? { author_key: claim.author_key } : {}) }]
}
// parseGate keeps a well-formed adopted gate and drops anything else.
export function parseGate(item: unknown): AdoptedGate[] {
  if (!isResponseObject(item) || !isString(item.key) || !isString(item.author_key) || !isStringArray(item.scope) || !isOptional(item.error, isString)) return []
  return [{ key: item.key, author_key: item.author_key, scope: item.scope, ...(isString(item.error) ? { error: item.error } : {}) }]
}
function parseRepair(value: unknown): Repair | undefined {
 if (!isResponseObject(value) || !["manifest", "cycle"].includes(String(value.kind)) || !isString(value.id) || !isString(value.author) || !isStringArray(value.paths) || !isString(value.state) || !isBoolean(value.preserve) || !isString(value.reason)) return undefined
 return value as Repair
}
// vfsResponse keeps the fields of a `takt-ai vfs` answer the plugin knows, each only when well typed.
export function vfsResponse(parsed: Record<string, unknown>, ok: boolean): VFSResponse {
  const shell = parseVFSShell(parsed.shell)
  const repair = parseRepair(parsed.repair)
  return {
    ok,
    ...(repair ? { repair } : {}),
    ...(isResponseObject(parsed.stop_cycle) && isString(parsed.stop_cycle.cycle) && Array.isArray(parsed.stop_cycle.lanes) && parsed.stop_cycle.lanes.every(lane => isResponseObject(lane) && isString(lane.dispatch) && isString(lane.attempt) && isString(lane.child)) ? { stop_cycle: parsed.stop_cycle as VFSResponse["stop_cycle"] } : {}),
    ...(typeof parsed.key === "string" ? { key: parsed.key } : {}),
    ...(typeof parsed.content === "string" ? { content: parsed.content } : {}),
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
export function isGCPlan(value: Record<string, unknown>): value is GCPlan {
  return typeof value.session_id === "string" && typeof value.cycle_id === "string" && typeof value.mandate_class === "string" &&
    Array.isArray(value.delta) && Array.isArray(value.closure) && value.closure.every((path): path is string => typeof path === "string") && typeof value.reachability === "string"
}
export function isGCCycle(value: Record<string, unknown>): value is GCCycle {
  return typeof value.phase === "string" && isResponseObject(value.plan) && isGCPlan(value.plan) &&
    Array.isArray(value.scope) && value.scope.every((path): path is string => typeof path === "string") &&
    isResponseObject(value.sessions) && Object.values(value.sessions).every((session): session is string => typeof session === "string") &&
    Object.hasOwn(value, "report") && typeof value.started === "string" &&
    (value.author_key === undefined || typeof value.author_key === "string")
}
export function isStagedView(value: Record<string, unknown>): value is StagedView {
  return typeof value.revision === "number" && typeof value.delta_hash === "string" && isResponseObject(value.files) &&
    Object.values(value.files).every(file => file === null || typeof file === "string")
}
export function isCoordinator(value: Record<string, unknown>): value is CoordinatorResponse {
  return typeof value.version === "number" && typeof value.units === "number" && typeof value.mutations === "number" &&
    typeof value.cursor === "number" && typeof value.deferrals === "number" && typeof value.next_mandate === "number" &&
    typeof value.requested === "boolean" &&
    (value.cycle === undefined || isResponseObject(value.cycle) && isGCCycle(value.cycle)) &&
    (value.history === undefined || Array.isArray(value.history) && value.history.every(item => isResponseObject(item) && isGCCycle(item)))
}
// gcCoordinateResponse returns result when it is the shape `gc coordinate` answers action with.
export function gcCoordinateResponse(result: Record<string, unknown>, action: unknown): DispatchResponse | undefined {
  switch (action) {
    case "prepare": return isGCPlan(result) ? result : undefined
    case "findings": case "investigate": case "authorize": return isGCCycle(result) ? result : undefined
    case "collected": case "delta": return isStagedView(result) ? result : undefined
    default: return isCoordinator(result) ? result : undefined
  }
}
export function parseCoordinationResponse(value: unknown, verb: string[], action: unknown): DispatchResponse {
  if (value === null) return null
  const result = responseObject(value, verb.join(" "))
  if (isHandoffEnvelope(result)) return result
  if (verb[0] === "dispatch" && ["prepare", "execution", "executions", "attach_child", "resolve", "inputs", "deliver", "interlocutor"].includes(String(action))) return result
  if (verb[0] === "gc" && action === "prompt" && typeof result.allowed === "boolean") return result
  if (action === "session_deleted" && result.aborted === false) return { aborted: false }
  const coordinated = verb[0] === "gc" && verb[1] === "coordinate" ? gcCoordinateResponse(result, action) : undefined
  if (coordinated !== undefined) return coordinated
  if (verb[0] === "dispatch") {
    const acknowledgement = action === "switch" || action === "validate_results"
    if (acknowledgement && (Object.keys(result).length === 0 || result.ok === true)) return null
    if (!acknowledgement && isCoordinator(result)) return result
  }
  throw new Error(`${verb.join(" ")} ${asString(action, "unknown")} returned an unexpected response shape`)
}

// orchestratorOnly refuses a claim operation from any agent but the orchestrator.
export const orchestratorOnly = (c: { agent?: string }, operation: string) => {
  if (c.agent !== ORCHESTRATOR_ID) throw new Error(`${operation} belongs to the orchestrator`)
}

// Notices the orchestrator receives around a maintenance cycle: conduct only.
const MAINTENANCE_DUE =
  "A cleanup cycle is running while you are idle. It ends the moment you delegate, so delegate whenever the work calls for it; changes it already consolidated were not requested by the user and are not a regression."
const MAINTENANCE_DONE = "The cleanup cycle concluded. Delegation and normal work continue."

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


// RESULT_AGENTS are the catalog producers that carry the takt-result-handoff
// skill: their delegation must call deliver_result before it ends.
const RESULT_AGENTS: string[] = "__TAKT_RESULT_AGENTS__" as unknown as string[]

// EXCEPTION_BOUNDS are the limits a user exception may raise, by the names the
// execution history records them under.
const EXCEPTION_BOUNDS = [
  "ceiling/concurrent-specialists", "budget/unplanned-units", "budget/contests",
  "budget/recovery-failures", "budget/recovery-actions", "budget/recovery-attempts",
]

// FAILING_VERDICT is how the core words a refusal to consolidate over a current
// failing verdict: only the user's acceptance lets the work through.
const FAILING_VERDICT = "a current verification verdict failed"
const VFS_TOOL_NAMES = ["vfs_bind", "vfs_write", "vfs_read", "vfs_delete", "vfs_discard", "vfs_verify", "vfs_consolidate"]

// str and obj keep the JSON Schema inputs readable; V2 takes plain JSON Schema
// rather than the V1 zod-shaped tool.schema helper.
// withText appends one text block to a tool result's content, in either of its shapes.
function withText<C>(content: string | ReadonlyArray<C> | undefined, text: string): string | ReadonlyArray<C | { readonly type: "text"; readonly text: string }> {
  if (content === undefined) return text
  if (typeof content === "string") return `${content}\n\n${text}`
  return [...content, { type: "text" as const, text }]
}
// engramRefs renders Engram entry IDs the way agents cite them.
const engramRefs = (ids: number[]) => ids.map(id => `Engram #${id}`).join(", ")
const str = (description?: string) => (description ? { type: "string", description } : { type: "string" })
const obj = (properties: Record<string, unknown>, required: string[]) =>
  ({ type: "object", properties, required, additionalProperties: false })

// asString narrows an unknown protocol field to a string, falling back
// instead of letting a non-string value stringify to the useless
// "[object Object]" (dispatch/shell fields are caller-supplied and only
// typed unknown because they pass through a Record<string, unknown>).
export const asString = (v: unknown, fallback: string): string => (typeof v === "string" ? v : fallback)

// once runs work until it succeeds a single time. Concurrent callers share the
// run in flight, and a failed run is attempted again by the next caller.
function once(work: () => Promise<void>): () => Promise<void> {
  let done = false
  let running: Promise<void> | undefined
  return async () => {
    if (done) return
    running ??= work().then(() => { done = true }).finally(() => { running = undefined })
    return running
  }
}

// SENSITIVE_READ_GLOBS are the secret-bearing paths no agent reads, injected
// from the same list the global read rules deny.
const SENSITIVE_READ_GLOBS: string[] = "__TAKT_SENSITIVE_READ_GLOBS__" as unknown as string[]

// GC_LANE_TOOLS are the tools each GC lane keeps besides its gc_* tools: both
// read the workspace natively to investigate or judge, and the verifier keeps
// everything an ordinary verification uses (shell, web, Engram reads); only the
// collector stages its authorized change.
const GC_LANE_TOOLS: Record<string, string[]> = {
  collector: ["gc_findings", "gc_investigate", "gc_authorize", "gc_collected", "gc_no_change", "read", "glob", "grep", "vfs_read", "vfs_write", "vfs_delete", "memory_record"],
  verifier: ["gc_baseline", "gc_delta", "gc_verdict", "gc_acceptance", "read", "glob", "grep", "shell", "bash", "webfetch", "websearch", "engram_mem_current_project", "engram_mem_search", "engram_mem_get_observation", "engram_mem_context", "memory_record"],
}

// gcSessionPermissions is the ruleset a GC child session is created with: only
// its lane's tools survive. A tool whose last matching rule is a blanket deny
// is removed from the session's tool snapshot entirely.
const gcSessionPermissions = (role: string) => [
  { action: "*", resource: "*", effect: "deny" as const },
  ...GC_LANE_TOOLS[role].map(action => ({ action, resource: "*", effect: "allow" as const })),
  ...SENSITIVE_READ_GLOBS.map(glob => ({ action: "read", resource: `**/${glob}`, effect: "deny" as const })),
]

export default Plugin.define({
  id: "takt.vfs",
  async setup(ctx) {
    const workspace = ctx.location.directory
    const abort = new AbortController()
    let hostWaits = new AbortController()
    const home = homedir()
    let codegraphReady = !(await sameDirectory(workspace, home))
    let codegraphProblem = `CodeGraph is unavailable because ${workspace} is the home directory; open a specific project directory first.`
    if (!codegraphReady) {
      console.warn(`Takt: CodeGraph was not initialized because OpenCode is open in the home directory (${home}). Open a specific project to enable CodeGraph tools.`)
    } else {
      // MCP tools are catalogued during OpenCode startup. Prepare the index
      // synchronously here rather than from a later session event, so CodeGraph
      // is active by the time OpenCode asks its MCP server for tools.
      try {
        const codegraph = Bun.spawn([TAKT_AI, "codegraph", "ensure-index"], { detached:true, cwd: workspace, stdout: "pipe", stderr: "pipe" })
        const [out, err, code] = await ipcOutput(codegraph, "CodeGraph preparation")
        if (code !== 0) throw new Error(err.trim() || out.trim() || `exit ${code}`)
      } catch (error) {
        codegraphReady = false
        codegraphProblem = `CodeGraph preparation failed in ${workspace}: ${error instanceof Error ? error.message : error}; repair its index prerequisites and restart the host; protection hooks remain active.`
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
    const invocations = new Map<string,Pick<Delegation,"dispatch"|"root"|"attempt"|"cycle">>()
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
      scheduleGC()
    }
    const reconcileStoredDelegations = async () => {
      for (const d of await unsettledExecutions()) {
        if (d.cycle) continue
        let backgroundLaunched = false
        // Every exact call is checked, including our own lost callbacks and
        // executions whose linked child disappeared. Lease expiry proves nothing.
        try {
          const state = await callState(d)
          if (state === "gone" || state === "error" || (!d.background && state === "completed")) {
            await settle(d, state)
            continue
          }
          backgroundLaunched = state === "completed"
        } catch (error) { console.error("Takt native call reconciliation", d.dispatch, error) }
        if (d.background && backgroundLaunched && d.child) {
          try {
            await ctx.session.get({ sessionID:d.child }, { signal:AbortSignal.any([abort.signal,AbortSignal.timeout(60_000)]) })
          } catch (error) {
            // Exact launch completion plus verified child deletion is terminal;
            // a transport failure or an expired owner lease is not.
            if (notFound(error)) {
              await endDelegation(d.dispatch,d)
              continue
            }
            console.error("Takt background child reconciliation",d.dispatch,error)
          }
        }
        if (watching.has(d.dispatch) || !d.child || !d.mine || (d.background && !backgroundLaunched)) continue
        watching.add(d.dispatch)
        void ctx.session.wait({ sessionID:d.child }, { signal:AbortSignal.any([abort.signal,hostWaits.signal]) }).then(async () => {
          // A child idle before a foreground call ends is not that call's
          // termination. Background launch completion plus host idle is terminal.
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
    }
    function stateDir(): string {
      return resolve(workspace, ".takt-ai", "vfs")
    }

    async function delegatedUnit(sessionID: string): Promise<string> {
      const d = await executionFor(sessionID)
      if (!d) throw new Error(`session ${sessionID} has no running delegation; return to the orchestrator to resume its exact sessionID`)
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
    // offlineCommand is the exact takt-ai invocation the user can run outside
    // the session to take a decision the session could not ask for.
    const offlineCommand = (verb: string[], request: Request) => {
      const quote = (s: string) => `'${s.replaceAll("'", QUOTED_QUOTE)}'`
      return `${TAKT_AI} ${verb.join(" ")} --workspace ${quote(workspace)} --state ${quote(stateDir())} --request ${quote(JSON.stringify(request))}`
    }
    async function repairOperation<T>(run: () => Promise<T>, resume: (approval: Request) => Promise<T>, c: Caller): Promise<T> {
      try { return await run() }
      catch (error) {
        const repair = error instanceof VFSRefusal ? error.response.repair : undefined
        if (!repair) throw error
        const diagnostic = `${repair.reason}; ${repair.kind} ${repair.id}, author ${repair.author}, files ${repair.paths.join(", ")}, state ${repair.state}`
        // The user approves a repair; whoever speaks with the user asks.
        const interlocutor = c.agent === ORCHESTRATOR_ID || await holdsInterface(c.sessionID)
        if (!interlocutor) throw new VFSRefusal(`${diagnostic}; return this diagnostic to the orchestrator and invoke the original normal operation there`, (error as VFSRefusal).response)
        if (!c.agent || !c.id || !c.messageID) {
          throw new VFSRefusal(`${diagnostic}; ${c.agent === ORCHESTRATOR_ID ? "call claim_list" : "make your next Takt tool call"} to ask the user for this repair, then retry`, (error as VFSRefusal).response)
        }
        const approval = { kind: repair.kind, id: repair.id, state: repair.state, preserve: repair.preserve }
        const offline = repair.kind === "cycle"
          ? offlineCommand(["gc", "coordinate"], { action: "abort", event: repair.id, evidence: "repair approved by the user", repair: approval })
          : offlineCommand(["dispatch"], { action: "tick", session: await rootSession(c.sessionID), repair: approval })
        await confirmWithUser(`vfs_repair_${repair.kind}`, repair.id, { reason: repair.reason, author: repair.author, files: repair.paths.join(", "), state: repair.state, preserve_current_files: String(repair.preserve) }, { sessionID:c.sessionID, agent:c.agent, id:c.id, messageID:c.messageID }, `ask the user to run: ${offline}`)
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
    type ShellPlan = { decision: "allow" | "ask" | "deny"; reason: string; cwd: string; scratch: string; confirm: string; writable: string[]; protected: string[]; private: string[]; capture: boolean }
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
    // The orchestrator's and RESULT_AGENTS' native-permission admission, by call
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
          throw refused("git_mutation_denied", agent ?? "", sessionID, new Error(`Takt refused this shell command: ${e instanceof Error ? e.message : e}`))
        }
        entry.ready = true
      }
    }
    // shellPlanOf reads the plan shell-prepare admitted, refusing a denial or
    // a plan missing a field.
    const shellPlanOf = (shell: VFSShellResponse | undefined, agent: string | undefined, sessionID: string): ShellPlan => {
      if (!shell) throw new Error("takt-ai vfs shell-prepare response omitted shell plan")
      if (shell.decision === "deny") throw refused("shell_denied", agent ?? "", sessionID, new Error(`Takt refused this shell command: ${shell.reason}`))
      if (typeof shell.cwd !== "string" || typeof shell.scratch !== "string" || typeof shell.confirm !== "string" || typeof shell.capture !== "boolean" || !shell.writable || !shell.protected || !shell.private) throw new Error("takt-ai vfs shell-prepare response omitted an allowed shell plan field")
      return { decision: shell.decision, reason: shell.reason, cwd: shell.cwd, scratch: shell.scratch, confirm: shell.confirm, capture: shell.capture, writable: shell.writable, protected: shell.protected, private: shell.private }
    }

    // The first execute.before pins the tool call to the execution its session
    // runs, before any other hook (the shell's included) acts for that call,
    // and keeps a maintenance lane to the tools its role uses.
    const toolStartedAt = new Map<string, number>()
    await ctx.tool.hook("execute.before", async (event) => {
      toolStartedAt.set(toolKeyOf(event), Date.now())
      if (event.agent === ORCHESTRATOR_ID) return
      const d = await executionFor(event.sessionID, event.agent)
      if (!d) return
      if (d.cycle && !(GC_LANE_TOOLS[d.role ?? ""] ?? []).includes(event.tool)) throw refused("gc_lane_tool", event.agent ?? "", event.sessionID, new Error("This maintenance cycle does not use that tool"))
      invocations.set(toolKeyOf(event), { dispatch: d.dispatch, root: d.root, attempt: d.attempt, cycle: d.cycle })
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
        if (event.agent === ORCHESTRATOR_ID || RESULT_AGENTS.includes(event.agent ?? "")) {
          await admitDirect(event.agent, event.sessionID, command, toolKey, token)
          return
        }
        const callID = randomUUID()
        const entry: AdmittedShell = { callID, session: event.sessionID, agent: event.agent, toolKey, started: false, wrapped: false, evaluated: false }
        admittedShell.set(token, entry)
        try {
          // The core plans from the binding this session holds; an unbound caller
          // may still inspect, but it may not mutate.
          const res = await takt("shell-prepare", event, { call_id: callID, command })
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
        if (!admitted?.ready || admitted.started) return refuse("this shell command was not admitted; issue it again as a new tool call")
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
            protectedPaths: plan.protected, privatePaths: plan.private, callID,
          }), plan.confirm)
          // The command text stays as admitted: OpenCode parses it for its own
          // permission check after this hook. The sandbox lives in the shell
          // executable instead, which ignores the arguments it is handed and runs
          // the supervised command against its projection. A command that
          // mutates gets the sandbox scratch as home and temporary directory;
          // inspection runs in the workspace and keeps the real home.
          const quote = (s: string) => `'${s.replaceAll("'", QUOTED_QUOTE)}'`
          const dir = await mkdtemp(join(tmpdir(), "takt-shell-"))
          admitted.wrapper = dir
          const wrapper = join(dir, "sh")
          await writeFile(wrapper, [
            "#!/bin/sh",
            `cd ${quote(plan.cwd)} || exit ${SHELL_NOT_ADMITTED}`,
            `export TMPDIR=${quote(plan.scratch)}`,
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
        if (event.effect === "deny" || event.agent === ORCHESTRATOR_ID || RESULT_AGENTS.includes(event.agent ?? "")) return
        // OpenCode asks over the commands it parsed out of the call, not the call
        // itself, so the admission is found by the tool call the request names.
        // The decision is resolved again for every execution, so a persistent
        // approval cannot silently broaden the command, the resource or the
        // role: an approval-gated command is asked once per run (PR-HAR-9).
        const key = event.source ? toolKeyOf({ sessionID: event.sessionID, id: event.source.id }) : undefined
        const mine = key ? [...admittedShell.values()].find((a) => a.wrapped && !a.evaluated && a.toolKey === key && a.ready) : undefined
        if (!mine) {
          event.effect = "deny"
          event.message = "Takt refused this shell command: it was not admitted; issue it again as a new tool call"
          return
        }
        mine.evaluated = true
        try {
          const plan = shellPlanOf((await takt("shell-continue",{sessionID:mine.session,agent:mine.agent},{call_id:mine.callID,phase:"evaluate"})).shell,mine.agent,mine.session)
          event.effect = plan.decision
          event.message = plan.reason
        } catch (error) {
          event.effect = "deny"
          event.message = `Takt refused this shell command: ${error instanceof Error ? error.message : error}; issue it again as a new tool call`
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
        for (const [token, d] of [...directShell]) if (d.toolKey === toolKey) directShell.delete(token)
        const ended = [...admittedShell].filter(([, a]) => a.session === event.sessionID && a.toolKey === toolKey)
        for (const [token, admitted] of ended) await finishShell(token, admitted, event.sessionID, true)
      })

      // A call whose execute.after never arrives ends with its session: an idle
      // session has no call in flight (its delta is still imported), a deleted one
      // has no binding left to import into.
      releaseShellSession = async (sessionID, importDelta) => {
        for (const [token, d] of [...directShell]) if (d.toolKey.startsWith(`${sessionID}:`)) directShell.delete(token)
        for (const [token, admitted] of [...admittedShell]) if (admitted.session === sessionID) await finishShell(token, admitted, sessionID, importDelta)
      }
    }

    // GC policy and lifecycle live in Go. This queue only transports host events
    // and API-created child identities; no event handler waits for model work.
    let gcQueue: Promise<void> = Promise.resolve()
    // GC lane authority and prompt dispositions live in history/core.
    // phases and trigger bookkeeping, `dispatch` for ordinary crew admission
    // and lifecycle. Neither is the model's to choose; each call site below
    // names the one its action belongs to.
    async function spawnCoordination(verb: string[], request: Record<string, unknown>): Promise<DispatchResponse> {
      const proc = Bun.spawn([TAKT_AI, ...verb, "--workspace", workspace,
        "--state", stateDir(), "--request", JSON.stringify(request)], {
        detached:true, stdin: "ignore", stdout: "pipe", stderr: "pipe", cwd: workspace,
      })
      const [out, err, code] = await ipcOutput(proc, verb.join(" "), verb[0] === "gc" && ["prepare", "request", "tick", "baseline", "acceptance", "verdict"].includes(String(request.action)) ? 0 : 60_000)
      if (code !== 0) throw new Error(`${verb.join(" ")} ${asString(request.action, "unknown")}: ${err || out}`)
      const parsed: unknown = JSON.parse(out)
      if (isResponseObject(parsed) && parsed.ok === false) throw new VFSRefusal(asString(parsed.error, "coordination refused"), vfsResponse(parsed, false))
      return parseCoordinationResponse(parsed, verb, request.action)
    }
    const coordinate = (request: Record<string, unknown>): Promise<DispatchResponse> => {
      return spawnCoordination(["gc", "coordinate"], request)
    }
    // dispatchAction admits and drives ordinary crew work: the same execution
    // history and admission protocol `gc coordinate` uses to gate its own
    // cycle, reached under its own name rather than under GC's.
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
    // stopChild interrupts a GC child and reports whether it still exists, so a
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
    // stopLanes interrupts a cycle's lane sessions together; one that fails
    // does not keep the others running.
    async function stopLanes(cycleID: string): Promise<Array<{ dispatch: string; attempt: string; child: string }>> {
      const lanes = (await unsettledExecutions()).filter(d => d.cycle === cycleID)
      const results = await Promise.allSettled(lanes.map(async d => {
        if (!d.child) throw new Error(`GC lane ${d.dispatch} has no host session to prove terminated; no resources released`)
        if (!d.terminated) await stopChild(d.child)
        return { dispatch:d.dispatch, attempt:d.attempt, child:d.child }
      }))
      const failed = results.flatMap(r => r.status === "rejected" ? [r.reason] : [])
      if (failed.length) throw new AggregateError(failed, `GC lane interruption failed; no admission or resource release; retry the same call, which stops the lanes again: ${failed.map(String).join("; ")}`)
      return results.flatMap(r => r.status === "fulfilled" ? [r.value] : [])
    }
    // createChildSession and promptChild are the generic child-session trio:
    // GC's own pump and the interlocutor switch (below) both spin up a lane
    // session and seed it the same way, so neither owns this pair.
    async function createChildSession(opts: Parameters<typeof ctx.session.create>[0]): Promise<string> {
      const created = await ctx.session.create(opts)
      return created.id
    }
    async function promptChild(sessionID: string, text: string): Promise<void> {
      await ctx.session.prompt({ sessionID, text }, { signal:AbortSignal.any([abort.signal,AbortSignal.timeout(60_000)]) })
    }
    // returnInterface hands the interlocutor envelope to the root orchestrator,
    // which holds the interface again; the lent session gets nothing back.
    async function returnInterface(root: string, from: string, envelope: unknown): Promise<void> {
      await ctx.session.synthetic({ sessionID: root, text: `The interface is back from ${from}. Envelope:\n${JSON.stringify(envelope, null, 2)}` })
    }
    const restoreGCCycle = once(async () => { await reconcileStoredDelegations() })
    function scheduleGC() {
      // setTimeout releases tool/event hooks before the SDK is asked to prompt.
      setTimeout(() => {
        gcQueue = gcQueue.then(pumpGC).catch(async error => {
          console.error("Takt GC", error)
        })
      }, 0)
    }
    // Whether a cycle is due is only asked when the root orchestrator goes idle:
    // never mid-turn, never from a delegated specialist's or a lane's own idle.
    async function startCycleWhenIdle(sessionID: string) {
      try {
        if ((await executionFor(sessionID))?.cycle || await rootSession(sessionID) !== sessionID) return
        await restoreGCCycle()
        await coordinate({ action: "tick", session: sessionID })
        scheduleGC()
      } catch (error) {
        console.error("Takt GC", error)
      }
    }
    // pumpGC prompts the lane the in-flight cycle's phase needs, once per phase.
    async function pumpGC() {
      await restoreGCCycle()
      const state = await coordinate({ action: "status" })
      const cycle = isCoordinatorResponse(state) ? state.cycle : undefined
      if (!cycle?.plan) return
      const cycleID=cycle.plan.cycle_id
      if (["aborting", "blocked", "consolidating"].includes(cycle.phase)) {
        const stopped = await stopLanes(cycleID)
        await coordinate({ action:"abort", event:cycleID, stopped, evidence:"incomplete cycle closure" })
        return
      }
      // Another live instance drives this cycle's lanes until its lease lapses.
      if ((await unsettledExecutions()).some(d => d.cycle === cycleID && !d.mine)) return
      try {
      let role: "verifier" | "collector" | undefined
      if (cycle.phase === "baseline" || cycle.phase === "verify" || cycle.phase === "acceptance") role = "verifier"
      else if (cycle.phase === "investigate") role = "collector"
      if (!role) return
      let child = cycle.sessions?.[role]
      const agent = role === "collector" ? "simplify" : "verify"
      if (!child) {
        // V2 sessions have no settable parent, so a GC lane is its own session,
        // marked and permission-bounded at creation. The cycle's ordinary root
        // stays in cycle.plan.session_id; the lane names it as its root.
        child = await createChildSession({
          title: `Takt GC ${cycle.plan.cycle_id} ${role}`,
          agent,
          metadata: { takt_gc: cycle.plan.cycle_id },
          permissions: gcSessionPermissions(role),
        })
        try { await coordinate({ action: "attach", event:cycleID, role, child, owner: INSTANCE }) }
        catch {
          // A concurrent pump may have attached its child, or the cycle may
          // have changed while the host created ours. Only the owner's current
          // link can decide whether this native session should be retained.
          const current = await coordinate({action:"status"})
          if (!isCoordinatorResponse(current) || current.cycle?.plan.cycle_id!==cycleID || current.cycle.sessions?.[role]!==child) {
            await stopChild(child)
            scheduleGC()
            return
          }
        }
      }
      // A deleted lane is an incomplete cycle, never an invitation to resend.
      await ctx.session.get({ sessionID:child })
      const permit = await coordinate({ action: "prompt", event:cycleID, child, phase: cycle.phase, owner: INSTANCE })
      if (!permit || !isResponseObject(permit) || responseObject(permit,"gc prompt").allowed !== true) {
        watchIncompletePrompt(cycleID, cycle.phase, child)
        return
      }
      const instructions: Record<string, string> = {
        baseline: "Execute gc_baseline to record the acceptance baseline before any mutation. Then stop.",
        investigate: "Use gc_findings. Investigate each finding and record evidence with gc_investigate. Absence of refutation is not authorization. Public contracts, framework entries, new symbols and semantic documentation are protected. If only proposals/no changes, call gc_no_change. Otherwise call gc_authorize; change only its authorized files through vfs_write/vfs_delete and call gc_collected. Never consolidate yourself.",
        verify: "Use gc_delta to inspect the exact staged delta. Independently verify it preserves behavior and stays within the declared mandate/scope. Call gc_verdict with your explicit evidence. Do not author changes or verify your own work.",
        acceptance: "Call gc_acceptance to execute the same prepared checks on the materialized workspace. Then stop.",
      }
      // A failed transport does not prove the host never ran this prompt.
      // Keep the reservation until scoped recovery interrupts the native lane.
      await promptChild(child, `Maintenance cycle ${cycle.plan.cycle_id}; mandate ${cycle.plan.mandate_class}. ${instructions[cycle.phase]}`)
      watchIncompletePrompt(cycleID, cycle.phase, child)
      } catch(error) {
        // Recovery is scoped to the cycle this pump actually observed. A late
        // failure must never stop or abort a different, newly admitted cycle.
        const current = await coordinate({action:"status"})
        if(isCoordinatorResponse(current) && current.cycle?.plan.cycle_id===cycleID) {
          const stopped = await stopLanes(cycleID)
          await coordinate({action:"abort",event:cycleID,stopped,evidence:error instanceof Error?error.message:"unknown error"})
        }
        throw error
      }
    }
    const incompletePrompts = new Set<string>()
    function watchIncompletePrompt(cycle: string, phase: string, child: string) {
      const key = `${cycle}:${phase}:${child}`
      if (incompletePrompts.has(key)) return
      incompletePrompts.add(key)
      const finishIncomplete = async () => {
        const state = await coordinate({ action:"status" })
        if (!isCoordinatorResponse(state) || state.cycle?.plan.cycle_id !== cycle || state.cycle.phase !== phase) return
        const stopped = await stopLanes(cycle)
        await coordinate({ action:"abort", event:cycle, stopped, evidence:`lane ended without completing reserved phase ${phase}; prompt is never resent` })
      }
      void ctx.session.wait({ sessionID:child }, { signal:AbortSignal.any([abort.signal,hostWaits.signal]) })
        .then(finishIncomplete, async error => {
          if (notFound(error)) return finishIncomplete()
          throw error
        }).catch(error => console.error("Takt incomplete GC prompt", error)).finally(() => incompletePrompts.delete(key))
    }

    // gcTool runs one maintenance action for the lane attached to its role.
    async function gcTool(action: string, c: { sessionID: string; agent: string }, fields: Record<string, unknown> = {}) {
      const role = ["baseline", "delta", "verdict", "acceptance"].includes(action) ? "verifier" : "collector"
      const expected = role === "verifier" ? "verify" : "simplify"
      if (c.agent !== expected) throw new Error(`GC ${action} requires the attached ${role} session`)
      try {
        const result = await coordinate({ ...fields, action, session: c.sessionID })
        return { content: JSON.stringify(result) }
      } finally { scheduleGC() }
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

    await ctx.tool.hook("execute.before", async (event) => {
      if (event.tool !== "subagent" || invocations.get(toolKeyOf(event))?.cycle) return
      await reconcileStoredDelegations()
      const root = await rootSession(event.sessionID)
      const input = isResponseObject(event.input) ? event.input : {}
      // The delegation's description names the work unit it executes: a planned
      // unit by its committed identity, and a retry by the same name again. The
      // host call identity tells a transport repetition apart from a retry.
      const unit = asString(input.description, "").trim()
      const specialist = asString(input.agent, "")
      if (!unit) throw refused("missing_unit", specialist, root, new Error("Name the delegation after the work unit it executes: set its description to the unit identity"))
      if (!specialist) throw refused("missing_specialist", "", root, new Error("Takt VFS refused launch: the requested specialist identity is missing"), unit)
      const delegation = `${event.sessionID}:${event.id}`
      try {
        const request: Request = { action: "prepare", event: unit, dispatch: delegation, session: root,
          agent: specialist, caller: event.sessionID, background: input.background === true, owner: INSTANCE,
          ...(typeof input.sessionID === "string" && input.sessionID ? { child: input.sessionID } : {}),
          ...(input.author_keys === undefined ? {} : { author_keys: input.author_keys }) }
        const prepare = async () => {
          try { return await dispatchAction(request) }
          catch (error) {
            const stop = error instanceof VFSRefusal ? error.response.stop_cycle : undefined
            if (!stop) throw error
            // Only an otherwise admissible operation asks to yield these lanes.
            request.stopped = await stopLanes(stop.cycle)
            return dispatchAction(request)
          }
        }
        const prepared = await repairOperation(prepare, approval => dispatchAction({ ...request, repair:approval }), event)
        if (!prepared || !isResponseObject(prepared)) throw new Error(`dispatch ${delegation} preparation returned no disposition; retry the same subagent call`)
        if (responseObject(prepared,"dispatch prepare").launchable !== true) throw new Error(`dispatch ${delegation} was already prepared; do not repeat its native launch; wait for its session, or retry the unit with a new subagent call after it settles`)
      } catch (error) {
        const cause = error instanceof Error ? error : new Error(String(error))
        const held = (await unsettledExecutions().catch(() => [])).filter(d => !d.cycle)
        if (held.length) cause.message += `; unsettled delegations: ${held.map(d => `dispatch ${d.dispatch} (unit ${d.unit}, session ${d.child ?? d.caller}, attempt ${d.attempt})`).join(", ")}. The next admission checks their termination again; one that will never return is ended, with the user's agreement, by dispatch_abandon on its dispatch, and its staged work is kept`
        throw refused(cause.message.includes("claim_assign") ? "missing_claim" : "admission_denied", specialist, root, cause, unit)
      }
      // This instance already owns the host callback for this native call.
      // Do not start a second wait on it during transport reconciliation.
      watching.add(delegation)
      // Native subagent execution follows this hook. All subsequent observations
      // carry this exact dispatch, including callbacks that arrive after retry.
      scheduleGC()
    })

    // collectDelivery returns the Engram IDs a completed producer delivered for
    // its unit. Bounded to one retry: a producer that forgot deliver_result gets
    // a single nudge, never an unbounded prompt loop.
    async function collectDelivery(delegation: string, child: string | undefined): Promise<number[]> {
      let d = await execution(delegation)
      if (!d?.delivery?.length && child) {
        await promptChild(child, "Call deliver_result with this delegation's completed Engram entry IDs before ending your turn.")
        await ctx.session.wait({ sessionID: child }, { signal:AbortSignal.any([abort.signal,hostWaits.signal]) })
        d = await execution(delegation)
      }
      if (!d?.delivery?.length) throw new Error("the specialist ended without delivering its result; resume its session to ask for it")
      return d.delivery
    }

    /**
     * Settles tracked delegations and advances GC after ordinary VFS tool calls.
     * A completed result producer with no delivery gets one reminder if its child
     * is known; the hook waits for that turn before rejecting a missing result.
     * Delivery checks clear the child and result tracking and attempt to finish
     * the delegation even on failure. Storage, session, and coordination errors
     * propagate; a failed finish can replace a delivery or session error.
     * GC children and untracked delegations are ignored.
     */
    await ctx.tool.hook("execute.after", async (event) => {
      if (event.tool === "subagent") {
        const delegation = `${event.sessionID}:${event.id}`
        const d = await execution(delegation)
        if (d === undefined || d.settled || d.cycle) { watching.delete(delegation); return }
        // A background launch returns at once: the specialist is still running.
        if (d.background && event.status === "completed") { watching.delete(delegation); return }
        try {
          if (event.status === "completed" && RESULT_AGENTS.includes(d.agent ?? "")) {
            const delivered = await collectDelivery(delegation, d.child)
            // The orchestrator receives the delivered IDs with the result itself.
            event.result = { ...event.result, content: withText(event.result.content, `Delivered results: ${engramRefs(delivered)}`) }
          }
        } finally {
          try { await endDelegation(delegation, d, event) }
          finally { watching.delete(delegation) }
        }
      } else if (event.tool.startsWith("vfs_")) {
        // Maintenance observation cannot replace a successful VFS result with
        // a context/query failure. Admission still enforces its core barrier.
        void (async () => {
          if ((await executionFor(event.sessionID))?.cycle) return
          await restoreGCCycle()
          scheduleGC()
        })().catch(error => console.error("Takt post-result maintenance context unavailable",error))
      }
    })

    // A background delegation ends when its child goes idle. There is no tool
    // result to amend: whatever the child said, a question or a missing delivery
    // included, reaches the orchestrator as the child's own notification.
    async function settleBackground(child: string) {
      const d = await executionFor(child)
      if (d?.background) await reconcileStoredDelegations()
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

    // endDelegation records d's termination; given the caller that observed it,
    // a pending repair is asked of the user instead of failing that call.
    async function endDelegation(delegation: string, d: Delegation, c?: Caller) {
      const request = { action: "finish", event: d.unit, dispatch: delegation, attempt: d.attempt, session: d.root }
      await (c ? governed(c, request) : dispatchAction(request))
      scheduleGC()
    }

    // A native edit never lands on a path a VFS claim holds: that path belongs
    // to the staged work of its unit until it is consolidated or discarded, so
    // editing it natively would race the gate. Only ever tightens a decision.
    await ctx.permission.hook("evaluate", async (event) => {
      if (event.action !== "edit" || event.effect === "deny" || !event.resources.some(resource => resource.length > 0)) return
      try { await takt("edit-guard",event,{resources:event.resources}) }
      catch (error) {
        event.effect = "deny"
        event.message = `Takt refused this edit: ${error instanceof Error ? error.message : error}; retry the edit after ownership is released`
        refused("claimed_path", event.agent ?? "", event.sessionID,new Error(event.message))
      }
    })

    // Before delegation, surface prior-root ownership alongside maintenance
    // notices. Neither context read introduces a startup gate.
    let maintenanceAnnounced = false
    await ctx.session.hook("context", async (event) => {
      if (event.agent !== ORCHESTRATOR_ID) return
      const root = await rootSession(event.sessionID).catch(error => { console.error("Takt root context unavailable", error); return event.sessionID })
      const claims = await takt("claims", event).then(r => r.claims ?? [], () => undefined)
      if (claims !== undefined) {
        const prior = claims.filter(claim => claim.root_session_id !== root)
        if (prior.length) event.system.push({ type: "text", text: `Previous-session VFS claims (keys, files and staged status): ${JSON.stringify(prior)}. Before delegating: claim_assign without author_key replaces overlapping claims from other roots completely, discarding their staged work; with author_key it continues the existing work. Same-root collisions are refused.` })
      }
      // A failed status read must never break the orchestrator's own request.
      const state = await coordinate({ action: "status" }).catch(() => undefined)
      if (state === undefined) return
      const active = isCoordinatorResponse(state) && state.cycle != null
      if (active) {
        maintenanceAnnounced = true
        event.system.push({ type: "text", text: MAINTENANCE_DUE })
      } else if (maintenanceAnnounced) {
        maintenanceAnnounced = false
        event.system.push({ type: "text", text: MAINTENANCE_DONE })
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
    function endApprovals(reason: string, session?: string) {
      for (const [id, waiter] of pendingReplies) {
        if (session && waiter.session !== session) continue
        pendingReplies.delete(id)
        waiter.controller.abort()
        waiter.reject(new Error(`${reason}; no approval was granted; request approval again through the original operation`))
      }
    }
    // confirmWithUser asks the user through OpenCode and throws on a refusal.
    // fallback names how to proceed when the user cannot be asked here.
    async function confirmWithUser(action: string, resource: string, metadata: Record<string, string>, c: { sessionID: string; agent: string; messageID: string; id: string }, fallback: string) {
      try { await askUser(action, resource, metadata, c) }
      catch (error) {
        if (error instanceof Error && !error.message.startsWith("Permission denied")) error.message += `; the user could not be asked here: ${fallback}`
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
      const id = randomUUID()
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
      void (async () => {
        for await (const event of ctx.event.subscribe({ signal: abort.signal })) {
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
              await settleBackground(session)
              await releaseShellSession(session, true)
              await startCycleWhenIdle(session)
              scheduleGC()
            })().catch(error => console.error("Takt session idle", error))
          } else if (event.type === "session.deleted") {
            endApprovals("session deleted", event.data.sessionID)
            void onSessionDeleted(event.data.sessionID).catch(error => console.error("Takt session deleted", error))
          }
        }
      })().catch(error => console.error("Takt event stream disconnected", error)).finally(() => {
        eventsRunning = false; confirmations = undefined
        hostWaits.abort(); hostWaits = new AbortController()
        endApprovals("confirmation event stream ended")
      })
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
      if (ended && !ended.cycle) await reconcileStoredDelegations()
      if (ended?.cycle) {
        const stopped = await stopLanes(ended.cycle)
        await coordinate({ action:"abort", event:ended.cycle, stopped, evidence:"native lane session deleted" })
      }
      // The core decides from its history whether the session held a root.
      await dispatchAction({ action: "session_deleted", child: sessionID })
        .then(reply => {
          const { aborted, root } = reply as { aborted?: boolean; root?: string }
          if (aborted && root) return returnInterface(root, "a lent session that ended without a handoff", reply)
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
            author_keys: { type: "array", items: str(), minItems: 1, uniqueItems: true,
              description: "Staged author keys to review in this delegation; access is ready automatically, including retries." },
          } }
        })
      }
      const gc = (name: string, description: string, action: string, input = obj({}, [])) =>
        editor.add({ name, description, input, execute: (value: unknown, c) => {
          if (!codegraphReady) throw new Error(codegraphProblem)
          return gcTool(action, c, toolInput(value))
        } })

      gc("gc_baseline", "Verifier: run and record prepared acceptance baseline before mutation.", "baseline")
      gc("gc_findings", "Collector: read persisted findings and cycle declaration.", "findings")
      gc("gc_investigate", "Collector: record refuted, confirmed or proposal with explicit investigation evidence.", "investigate",
        obj({ finding: str(), outcome: str(), evidence: str() }, ["finding", "outcome", "evidence"]))
      gc("gc_authorize", "Collector: request a bounded VFS scope after investigation.", "authorize")
      gc("gc_collected", "Collector: submit the staged delta for independent verification.", "collected")
      gc("gc_delta", "Verifier: read the exact staged candidate delta.", "delta")
      gc("gc_verdict", "Verifier: deliver your independent verdict; an approval consolidates the delta.", "verdict",
        obj({ pass: { type: "boolean" }, evidence: str() }, ["pass", "evidence"]))
      gc("gc_acceptance", "Verifier: run prepared materialized acceptance; regression discards the entire cycle.", "acceptance")
      gc("gc_no_change", "Collector: explicitly close without changes, including proposals/refutations, without inventing acceptance.", "no-change")

      editor.add({
        name: "gc_prepare",
        description: "Ordinary preparation: validate reviewed project GC analyzer dependencies and acceptance commands. Never installs during GC.",
        input: obj({}, []),
        async execute(_args, c) {
          if (c.sessionID !== await rootSession(c.sessionID)) throw new Error("GC preparation belongs to the root ordinary workflow")
          return { content: JSON.stringify(await governed(c, { action: "prepare", session: c.sessionID }, coordinate)) }
        },
      })
      editor.add({
        name: "gc_request",
        description: "Request a GC cycle on the user's behalf.",
        input: obj({}, []),
        async execute(_args, c) {
          if (c.sessionID !== await rootSession(c.sessionID)) throw new Error("GC requests belong to the root interlocutor")
          await restoreGCCycle()
          const result = await governed(c, { action: "request", session: c.sessionID }, coordinate)
          scheduleGC()
          return { content: JSON.stringify(result) }
        },
      })

      // dispatch is (name, description, action, input) for the ordinary crew
      // dispatch tools below: each is the orchestrator's own declaration, never
      // a specialist's, so every one requires the calling session to be the
      // root interlocutor.
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

      dispatch("dispatch_commit", "Commit a plan baseline: every listed unit becomes planned, with its contract and explicit prerequisites. A planned unit is executed by delegating it with the unit identity as the subagent description; delegating that name again retries the unit. Commit before delegating more work than may run without a plan, or when the active workflow calls for one. Once a plan stands, change it by naming its current version as base_version: list only the units the revision adds or replaces, withdrawn units leave the plan, and units already delegated stand as committed.",
        "commit",
        obj({
          version: str("Identifier of this baseline version"),
          base_version: str("The standing plan version this declaration revises; omit only for the first commitment"),
          plan: { type: "array", items: obj({
            unit: str("Work unit identity"), contract: str("Frozen contract this unit is dispatched against"),
            prerequisites: { type: "array", items: { type: "string" }, description: "Prerequisite work unit identities" },
          }, ["unit", "contract"]), description: "Every unit this baseline covers, or that a revision adds or replaces" },
          withdraw: { type: "array", items: { type: "string" }, description: "Still-planned unit identities a revision removes" },
        }, ["version", "plan"]),
        (args: { version: string; base_version?: string; withdraw?: string[]; plan: { unit: string; contract: string; prerequisites?: string[] }[] }) =>
          ({
            version: args.version,
            ...(args.base_version ? { base_version: args.base_version } : {}),
            plan: args.plan.map(u => ({ unit: u.unit, contract: u.contract, prerequisites: u.prerequisites ?? [] })),
            ...(args.withdraw?.length ? { withdrawals: args.withdraw } : {}),
          }))

      editor.add({
        name: "dispatch_inputs",
        description: "Before delegating a unit, declare the Engram IDs of the invariants it consumes, or declare that none exists yet. The specialist receives them with its delegation; redeclaring replaces the declaration.",
        input: obj({
          work_unit_id: str("The unit the delegation will be named after"),
          result_ids: { type: "array", items: { type: "integer" }, description: "Existing Engram entry IDs of the invariants this unit consumes" },
          none: { type: "boolean", description: "True only when no invariant this unit consumes has been recorded yet" },
        }, ["work_unit_id"]),
        async execute(value: unknown, c) {
          const args = toolInput<{ work_unit_id: string; result_ids?: number[]; none?: boolean }>(value)
          const session = await rootSession(c.sessionID)
          orchestratorOnly(c, "dispatch_inputs")
          if (c.sessionID !== session) throw new Error("dispatch_inputs belongs to the root orchestrator")
          const unit = asString(args.work_unit_id, "").trim()
          const ids = Array.isArray(args.result_ids) ? args.result_ids : []
          if (!unit) throw new Error("Name the unit the delegation will be named after")
          if ((ids.length > 0) === (args.none === true)) throw new Error("Declare either the consumed result_ids or none: true, exactly one of them")
          await governed(c, { action: "inputs", session, event: unit, result_ids: ids, none: args.none === true })
          return { content: ids.length > 0 ? `Declared ${ids.length} consumed invariant(s) for ${unit}` : `Declared that ${unit} consumes no recorded invariant yet` }
        },
      })

      editor.add({ name: "claim_list", description: "List ownership claims for the current root session.", input: obj({}, []), async execute(value: unknown, c) {
        toolInput(value)
        orchestratorOnly(c, "VFS claims")
        return { content: JSON.stringify(await takt("claims", c)) }
      } })
      // The target instance is named once and serves as both the binding agent
      // and the catalog specialist, exactly as the launched subagent binds.
      editor.add({ name: "claim_assign", description: "Before delegating implementation, reserve the unit's exact file set for the specialist that will stage it. The returned key is that work's author_key. Never infer or expand scope. Without author_key, overlapping claims from other root sessions are discarded completely, including staged work; same-root collisions are refused. With author_key, continue existing work. To retry or correct work that already has staged changes, pass its author_key with the new exact file set: the staged work is kept, only added paths are checked for collisions, and the set must include every path already staged.", input: obj({
        work_unit_id: str("The unit the delegation will be named after"),
        agent: str("Catalog instance id of the specialist you will launch (for example `dev`); parallel lanes of one specialty share it and are told apart by `work_unit_id`"),
        scope: { type: "array", items: str(), description: "Exact workspace-relative file paths, existing or to be created; no directories or globs" },
        author_key: str("The author_key of this unit's existing staged work to keep; omit for new work"),
      }, ["work_unit_id", "agent", "scope"]), async execute(input: unknown, c) {
        const args = toolInput(input)
        orchestratorOnly(c, "VFS claim assignment")
        const unit = asString(args.work_unit_id, "").trim()
        return { content: JSON.stringify(await takt("assign", c, { work_unit_id: unit, agent_id: args.agent, specialist: args.agent, scope: args.scope, author_key: args.author_key })) }
      } })
      editor.add({ name: "claim_release", description: "Use claim_list to get the exact claim_key. A claim that holds staged work is not released: consolidate it, reassign it with claim_assign and its author_key, or discard it. A claim with no staged work is released directly, except one an agent is actively working under in the current root session: ask the user through the orchestrator's native question mechanism first and set confirmed true once they agree.", input: obj({ claim_key: str("Exact key returned by claim_list; never infer this key"), confirmed: { type: "boolean", description: "Set true once the user agreed to release a claim in this root session" } }, ["claim_key"]), async execute(value: unknown, c) {
        const args = toolInput<{ claim_key: string; confirmed?: boolean }>(value)
        orchestratorOnly(c, "VFS claim release")
        // The core decides under its lock: staged work and an active claim of
        // this root without the user's confirmation are refused there.
        const released = await takt("release", c, { key: args.claim_key, confirmed: args.confirmed === true })
        return { content: JSON.stringify(released) }
      } })

      dispatch("dispatch_activity_start", "Record direct orchestrator work activity (not a delegated unit).", "activity_start", obj({ activity_id: str(), node_kind: { type: "string", enum: ["orchestrator"] } }, ["activity_id", "node_kind"]), (args: { activity_id: string; node_kind: string }) => args)
      dispatch("dispatch_activity_finish", "Finish direct orchestrator work activity with its outcome.", "activity_finish", obj({ activity_id: str(), node_kind: { type: "string", enum: ["orchestrator"] }, outcome: { type: "string", enum: ["completed", "failed", "interrupted"] } }, ["activity_id", "node_kind", "outcome"]), (args: { activity_id: string; node_kind: string; outcome: string }) => args)

      dispatch("dispatch_declare_recovery", "Declare bounded autonomous recovery of an objective before uncertain work begins: a binary expected result, a prior recoverable point, explicit scope, and both budgets. The scope cannot overlap a recovery that is still open or abandoned and not restored. After repeated failed recoveries of the same objective, present evidence and alternatives to the user and await their decision.",
        "recovery",
        obj({
          objective: str("Identity of the objective being recovered"),
          result: str("The binary expected result that would demonstrate recovery"),
          point: str("Reference to the prior recoverable point"),
          scope: { type: "array", items: { type: "string" }, description: "Work unit identities this recovery's scope covers" },
          actions: { type: "number", description: "Action budget: the governed calls, reads included, that the scope's units may make" },
          attempts: { type: "number", description: "Attempt budget shared by every unit of the scope: each attempt admitted for any of them after this declaration spends one" },
        }, ["objective", "result", "point", "scope", "actions", "attempts"]),
        /** Copies the recovery declaration, using its objective as the history event's work unit. */
        (args: { objective: string; result: string; point: string; scope: string[]; actions: number; attempts: number }) => ({ ...args, event: args.objective }))

      dispatch("dispatch_close_recovery", "Close an open declared recovery: record whether its result was demonstrated and link the evidence. A scope whose budget ran out is already abandoned: continue it with dispatch_exception or drop it with dispatch_restore.",
        "recovered",
        obj({
          objective: str("Identity of the recovered objective"),
          evidence: str("Evidence supporting the claimed result"),
          demonstrated: { type: "boolean", description: "Whether the declared result was actually demonstrated" },
        }, ["objective", "evidence", "demonstrated"]),
        /** Maps the recovery result to history fields, using the objective as the work unit and demonstrated as pass. */
        (args: { objective: string; evidence: string; demonstrated: boolean }) => ({ event: args.objective, objective: args.objective, evidence: args.evidence, pass: args.demonstrated }))

      dispatch("dispatch_restore", "Drop an abandoned recovery scope: reverts all the staged work of its units, after which a new recovery can be declared. It waits until the scope's delegations have returned; call it again then.",
        "restore",
        obj({
          objective: str("Identity of the abandoned recovery's objective"),
        }, ["objective"]),
        (args: { objective: string }) => ({ objective: args.objective }))

      dispatch("dispatch_exception", "Raise one bound by the finite allowance the user explicitly granted. A recovery bound needs the objective. Granting attempts or actions of an abandoned recovery reopens it with its staged work intact.",
        "exception",
        obj({
          event: str("Optional label for the record"),
          bound: { type: "string", enum: EXCEPTION_BOUNDS, description: "The bound being raised, one of: ceiling/concurrent-specialists (concurrent delegations), budget/unplanned-units (units delegated without a committed plan), budget/contests, budget/recovery-failures, budget/recovery-actions, budget/recovery-attempts" },
          objective: str("Recovery objective the exception applies to; required for the recovery bounds"),
          allowance: { type: "number", description: "Finite additional allowance in that bound's own unit" },
        }, ["bound", "allowance"]),
        (args: { event?: string; bound: string; objective?: string; allowance: number }) => args)

      dispatch("dispatch_contest", "Contest the recorded terminal failure of a unit attempt, one that ended without a result: it records the contest and spends one of the contests. Judging the contested work is a Verify delegation of yours.",
        "contest",
        obj({
          event: str("The work unit whose recorded terminal failure is contested"),
          attempt: str("The attempt identity the failure belongs to; defaults to the unit's current attempt"),
        }, ["event"]),
        (args: { event: string; attempt?: string }) => ({ event: args.event, attempt: args.attempt ?? "" }))

      editor.add({
        name: "dispatch_abandon",
        description: "End a delegation that will never return, because its session is gone or will not end: its unit can be delegated again and its slot is freed, and its staged work is kept. Name the dispatch exactly as it was reported. The user is asked to agree first.",
        input: obj({ dispatch: str("Dispatch identity of the delegation, as reported") }, ["dispatch"]),
        async execute(value: unknown, c) {
          const args = toolInput<{ dispatch: string }>(value)
          orchestratorOnly(c, "dispatch_abandon")
          if (c.sessionID !== await rootSession(c.sessionID)) throw new Error("dispatch_abandon belongs to the root orchestrator")
          const d = await execution(asString(args.dispatch, ""))
          if (!d || d.cycle) throw new Error(`dispatch ${args.dispatch} names no ordinary delegation; use a dispatch exactly as a refusal reported it`)
          if (d.settled) return { content: `Dispatch ${d.dispatch} already returned` }
          const request = { action: "abandon", session: d.root, dispatch: d.dispatch, event: d.unit }
          await confirmWithUser("dispatch_abandon", d.dispatch, { unit: d.unit, agent: d.agent, root: d.root, session: d.child ?? d.caller }, c,
            `ask the user to stop session ${d.child ?? d.caller} and run: ${offlineCommand(["dispatch"], request)}`)
          // The user's decision stands even when the host cannot confirm the stop.
          const unstopped = d.child ? await stopChild(d.child).then(() => "", error => `; its session ${d.child} could not be confirmed stopped: ${error instanceof Error ? error.message : error}`) : ""
          await governed(c, request)
          watching.delete(d.dispatch)
          scheduleGC()
          return { content: `Abandoned unit ${d.unit} (dispatch ${d.dispatch}); its staged work is kept${unstopped}` }
        },
      })

      // dispatch_switch and dispatch_abort_switch cannot ride the dispatch()
      // factory above unmodified: both need to create or stop the temporary
      // interlocutor's own session around the call to Go, and fields() only
      // ever contributes a plain object into that one call. Each repeats
      // dispatch()'s own root-only guard instead (IR-19/IR-24: a switch or
      // its abort is only ever originated by the base or the user, never by
      // a temporary holder).
      //
      // OpenCode's plugin context has no programmatic way to raise the native
      // `question` confirmation (no ctx.question domain, and unlike
      // ctx.permission, there is no question.create on the local service's
      // own client either — verified against the installed @opencode/plugin
      // 2.0.16 and @opencode/client packages). `question` is a tool only the
      // model itself can invoke, so these descriptions instruct the model to
      // call it first and only proceed here after an accepted confirmation.
      editor.add({
        name: "dispatch_switch",
        description: "Switch the active interlocutor role to another specialist for the rest of this conversation. Before calling this tool, invoke the native `question` tool to confirm the target specialist and objective with the user. Call this tool only after an accepted confirmation; on rejection or a freeform answer, do not call this tool — reevaluate instead.",
        input: obj({
          target_agent: str("Catalog id of the specialist to switch the interlocutor role to"),
          objective: str("Objective for the incoming interlocutor's session"),
          requirements: str("Requirements context for the incoming interlocutor"),
          client: str("Client-facing context for the incoming interlocutor"),
          expected_artifact: str("Optional filesystem copy; the result's Engram ID is required at handoff"),
        }, ["target_agent", "objective"]),
        async execute(value: unknown, c) {
          const args = toolInput<{ target_agent: string; objective: string; requirements?: string; client?: string; expected_artifact?: string }>(value)
          const session = await rootSession(c.sessionID)
          if (c.sessionID !== session) throw new Error("dispatch_switch belongs to the root orchestrator")
          const text = [
            "You are taking over as the active interlocutor for this workspace.",
            `Objective: ${args.objective}`,
            args.requirements ? `Requirements: ${args.requirements}` : undefined,
            args.client ? `Client: ${args.client}` : undefined,
          ].filter(Boolean).join("\n")
          const child = await createChildSession({
            title: `Takt switch: ${args.target_agent}`,
            agent: args.target_agent,
            metadata: { takt_switch: session },
          })
          let result: DispatchResponse
          try {
            result = await governed(c, { action: "switch", session, child, agent: args.target_agent, artifact: args.expected_artifact })
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
        name: "dispatch_abort_switch",
        description: "Abort the current interlocutor switch and return the interlocutor role to the base. No confirmation required.",
        input: obj({ reason: str("Evidence for aborting the switch") }, ["reason"]),
        async execute(value: unknown, c) {
          const args = toolInput<{ reason: string }>(value)
          const session = await rootSession(c.sessionID)
          if (c.sessionID !== session) throw new Error("dispatch_abort_switch belongs to the root orchestrator")
          // No child is named: the core aborts the current holder or refuses.
          const result = await governed(c, { action: "abort_switch", session, child: "", evidence: args.reason, origin: "user" })
          const aborted = result !== null && "child" in result && typeof result.child === "string" ? result.child : ""
          if (aborted) await stopChild(aborted)
          return { content: JSON.stringify(result) }
        },
      })
      editor.add({
        name: "dispatch_handoff",
        description: "Hand the interlocutor role back to the base after a switch. Only the temporary holder that a switch created may call this. Before calling this tool, invoke the native `question` tool to confirm with the user what is being handed back (result, additional context, extra artifacts). Call this tool only after an accepted confirmation.",
        input: obj({
          result: { type: "string", enum: ["Standard", "EarlyHandoff", "TechFault", "Outraged"], description: "Outcome of this temporary interlocutor's turn" },
          additional_context: str("Context to carry back to the base"),
          extra_artifacts: { type: "array", items: { type: "string" }, description: "Optional filesystem copies" },
          result_ids: { type: "array", items: { type: "integer" }, description: "One existing Engram entry ID per delivered result; required for Standard handoff" },
        }, ["result", "additional_context", "extra_artifacts", "result_ids"]),
        // Not built on dispatch(): the caller here is the temporary holder,
        // never the root, so dispatch()'s root-only guard does not apply.
        async execute(value: unknown, c) {
          const args = toolInput<{ result: string; additional_context: string; extra_artifacts: string[]; result_ids: number[] }>(value)
          const session = await rootSession(c.sessionID)
          // A refused handoff throws here, and its reason is all the holder
          // receives: it corrects and retries.
          const envelope = await governed(c, {
            action: "handoff", session, child: c.sessionID, agent: c.agent,
            result: args.result, additional_context: args.additional_context, extra_artifacts: args.extra_artifacts, result_ids: args.result_ids,
          })
          await returnInterface(session, c.agent, envelope)
          // The holder has no further active work; its turn ends once this call returns.
          const holder = c.sessionID
          setTimeout(() => void stopChild(holder).catch(() => undefined), 0)
          return { content: "" }
        },
      })

      editor.add({
        name: "vfs_bind",
        description: "Bind to the assignment named in your context, before any other VFS tool.",
        input: obj({
          scope: { type: "array", items: { type: "string" }, description: "The exact paths your assignment names" },
        }, ["scope"]),
        async execute(input: unknown, c) {
          const args = toolInput<{ scope: string[] }>(input)
          if (isResponseObject(input) && input.author_key !== undefined) throw new Error("vfs_bind only binds implementation scope; verifier access is automatic")
          const who = { ...await callerBlock(c), work_unit_id: await delegatedUnit(c.sessionID) }
          const consumed = (await executionFor(c.sessionID, c.agent))?.inputs ?? []
          const res = await taktUnlocked("bind", { ...who, scope: args.scope })
          const key = res.key ?? (() => { throw new Error("vfs bind response omitted key") })()
          return { content: `Bound as ${c.agent} to scope ${JSON.stringify(args.scope)}; unit ${who.work_unit_id}, attempt ${res.attempt_id}; invariants ${engramRefs(consumed) || "none"} at version ${res.invariants_version}; author_key ${key}` }
        },
      })
      // authorKey passes a model-named author_key through only when it is a string.
      const authorKey = (value: unknown) => typeof value === "string" ? { author_key: value } : {}
      editor.add({
        name: "vfs_write",
        description: "Create or update a file inside your declared scope (create if absent, patch if present). Staged virtually; not written to disk until verified and consolidated.",
        input: obj({
          path: str(),
          content: str(),
          author_key: str("Assigned binding key; defaults to your own latest binding"),
        }, ["path", "content"]),
        async execute(input: unknown, c) {
          const args = toolInput<{ path: string; content: string; author_key?: string }>(input)
          const res = mutated(await takt("op", c, { call_id: randomUUID(), action: "create", path: args.path, content: args.content, ...authorKey(args.author_key) }))
          return { content: `Staged ${args.path} at revision ${res.revision} (delta_hash ${res.delta_hash})` }
        },
      })
      editor.add({
        name: "vfs_read",
        description: "Read a file of your assignment as staged: your own staged view, or the staged work you judge.",
        input: obj({ path: str("Workspace-relative path within your assignment"), author_key: str("Verifier only, optional: the author_key of the staged work whose files you read; omitted, the path's owner is used") }, ["path"]),
        async execute(input: unknown, c) {
          const args = toolInput<{ path: string; author_key?: string }>(input)
          const res = await takt("op", c, { call_id: randomUUID(), action: "read", path: args.path, ...authorKey(args.author_key) })
          // The verdict covers the state of the last read. Files read under an
          // earlier state may be outdated, so the read that moved it names them.
          const outdated = res.outdated_paths ?? []
          const note = outdated.length > 0
            ? `The author's staged work changed after you read: ${outdated.join(", ")}. What you read there may be outdated; read those files again before attaching the verdict.\n\n`
            : ""
          return { content: `${note}${res.content ?? ""}` }
        },
      })
      editor.add({
        name: "vfs_delete",
        description: "Stage the deletion of a file inside your declared scope. Staged virtually; the workspace copy is untouched until verified and consolidated.",
        input: obj({
          path: str(),
          author_key: str("Assigned binding key; defaults to your own latest binding"),
        }, ["path"]),
        async execute(input: unknown, c) {
          const args = toolInput<{ path: string; author_key?: string }>(input)
          const res = mutated(await takt("op", c, { call_id: randomUUID(), action: "delete", path: args.path, ...authorKey(args.author_key) }))
          return { content: `Staged deletion of ${args.path} at revision ${res.revision} (delta_hash ${res.delta_hash})` }
        },
      })
      editor.add({
        name: "vfs_discard",
        description: "Discard an author's entire staged work without touching the workspace, releasing the paths it owns. Name the author by its author_key.",
        input: obj({
          author_key: str("The author_key of the staged work to discard"),
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
        description: "As an independent verifier: attach a pass/fail verdict to the author's staged delta, assigned by your delegation to that author_key. The verdict covers exactly the revision staged when you judged it.",
        input: obj({
          author_key: str("The author_key the author reported"),
          pass: { type: "boolean" },
          finding: str("What the verdict rests on; required when pass is false"),
        }, ["author_key", "pass", "finding"]),
        async execute(input: unknown, c) {
          const args = toolInput<{ author_key: string; pass: boolean; finding: string }>(input)
          if (typeof args.author_key !== "string") throw new Error("author_key must be a string")
          // The core judges through the caller's gate over this author, at the
          // state that gate last read.
          await takt("verify", c, { call_id: randomUUID(), author_key: args.author_key, pass: args.pass, finding: args.finding })
          return { content: args.pass ? "Verdict: passed" : `Verdict: rejected — ${args.finding}` }
        },
      })
      editor.add({
        name: "vfs_consolidate",
        description: "Move authorized staged work into the workspace by author_key. Verdicts inform your decision; if a current one failed, the user is asked to accept consolidating anyway and a refusal keeps the work staged. Ownership and physical-base checks always apply.",
        input: obj({
          author_key: str("The author_key of the staged work to consolidate"),
          checkpoint: str("Optional short label hashed into the journal; defaults to the author key and revision"),
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
                `correct the work instead: claim_assign with author_key ${args.author_key}, delegate the fix, then delegate Verify again`)
              // The acceptance covers exactly the staged state the refusal named.
              await consolidate({ accept_failing: true, accepted_revision: revision, accepted_hash: hash })
            }
          } catch (error) {
            if (error instanceof Error && /physical base changed|recovery required/.test(error.message))
              error.message += "; inspect the named author and files; use claim_assign with author_key to continue staged work from the current physical base, or vfs_discard to explicitly abandon that staged work"
            throw error
          }
          return { content: "Consolidated to workspace" }
        },
      })
      editor.add({
        name: "deliver_result",
        description: "Deliver this delegation's completed result: the Engram entry IDs already recorded for it. Call this before your turn ends.",
        input: obj({
          result_ids: { type: "array", items: { type: "integer" }, minItems: 1, description: "Existing Engram entry IDs this delegation delivers" },
        }, ["result_ids"]),
        async execute(input: unknown, c) {
          const args = toolInput<{ result_ids: number[] }>(input)
          const root = await rootSession(c.sessionID)
          // Only the session a running delegation runs in delivers for it: an
          // earlier attempt's session of the same unit never credits this one.
          const call=c.id ? invocations.get(`${c.sessionID}:${c.id}`) : undefined
          const running = await adoptChild(c.sessionID, c.agent,call)
          const count = Array.isArray(args.result_ids) ? args.result_ids.length : 0
          if (!running) {
            await governed(c, { action: "validate_results", session: root, agent: c.agent, result_ids: args.result_ids })
            // A delegated session that runs no delegation has nobody to deliver
            // to; a top-level session (an interlocutor) hands off its results itself.
            if ((await ctx.session.get({ sessionID: c.sessionID })).parentID) {
              throw new Error("deliver_result belongs to a running delegation, and this session runs none: nothing was credited; return the result IDs in your reply instead")
            }
            return { content: `Validated ${count} result id(s); no delegation to credit them to` }
          }
          await governed(c, { action: "deliver", child: c.sessionID, agent: c.agent, dispatch:call?.dispatch ?? running.dispatch,attempt:call?.attempt ?? running.attempt,result_ids: args.result_ids })
          return { content: `Delivered ${count} result id(s)` }
        },
      })
    })

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
        const agent = await ctx.agent.get({ agentID: event.agent })
        const rules = agent.data.permissions
        const explicitlyAllows = (toolName: string) => {
          if (!Array.isArray(rules)) return false
          let decision: string | undefined
          for (const rule of rules) {
            if (rule && (rule.resource === toolName || rule.resource === "*") && ["*", toolName].includes(rule.action)) decision = rule.effect
          }
          return decision === "allow"
        }
        const allowed = VFS_TOOL_NAMES.filter(name => explicitlyAllows(name))
        const root = await rootSession(event.sessionID)
        const unit = await delegatedUnit(event.sessionID)
        const consumed = delegation?.inputs
        if (consumed) event.system.push({ type: "text", text: consumed.length > 0
          ? `Work unit ${unit} consumes these invariants: ${engramRefs(consumed)}. Read each with mem_get_observation; they bind as read-only and prevail over any restatement in the brief.`
          : `Work unit ${unit} consumes no recorded invariant yet; author it from the brief.` })
        const claim = (await findClaims(event, root, unit, event.agent, ["pending", "active"]))[0]
        if (!claim || allowed.length === 0) { removeVFS(); return }
        for (const name of VFS_TOOL_NAMES) if (!allowed.includes(name)) delete event.tools?.[name]
        if (claim.author_key) {
          // A gate arrives bound: adopting each preassigned gate and naming the
          // author's files is the delivery of the assignment, never the verifier's task.
          delete event.tools?.vfs_bind
          const { gates = [] } = await takt("adopt-gates", event)
          const lines = gates.map(gate => `author_key ${gate.author_key}: ${gate.scope.join(", ")}${gate.error ? ` (gate not adopted: ${gate.error}; report it as unverified)` : ""}`)
          event.system.push({ type: "text", text: `You judge the staged work of these authors for work unit ${unit}; every gate is already bound. ${allowed.includes("vfs_read") ? "Read staged files with vfs_read naming their author_key;" : ""} read project context with native read tools and run validation checks without editing the project.${allowed.includes("vfs_verify") ? " Attach a pass or fail verdict per author_key with vfs_verify, naming the finding it rests on." : ""}\n${lines.join("\n")}` })
          return
        }
        const conduct = [
          allowed.includes("vfs_bind") ? "Bind with vfs_bind using exactly this scope." : "",
          allowed.includes("vfs_write") ? "Stage changes with vfs_write." : "",
          allowed.includes("vfs_delete") ? "Stage deletions with vfs_delete." : "",
          allowed.includes("vfs_read") ? "Read your staged view with vfs_read." : "",
        ].filter(Boolean).join(" ")
        event.system.push({ type: "text", text: `Work unit ${unit} owns exactly these workspace-relative paths: ${(claim.scope ?? []).join(", ")}. ${conduct} Return the author_key in your handoff.` })
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
    return () => { clearInterval(leaseTimer); disposed = true; endApprovals("VFS plugin stopped"); abort.abort() }
  },
})
