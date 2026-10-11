// A transport fixture for the current history IPC, not plugin-local storage.
// Production behavior is additionally exercised against the real CLI in ssot.
export function historyFixture(initial = {}, stageAgents = ["dev", "fix", "simplify"]) {
 const inputs = new Map(Object.entries(initial.inputs ?? {}))
 const executions = structuredClone(initial.executions ?? {})
 const holders = { ...initial.holders }
 // Instances that renewed a lease; an execution whose owner never did is orphaned.
 const leases = new Set()
 const reply = value => ({ out: JSON.stringify(value), code: 0 })
 const denied = err => ({ err, code: 1 })
 function activeChild(child, agent) { return Object.values(executions).find(d => !d.settled && d.child === child && (!agent || d.agent === agent)) }
 function answer(kind, command, r, claims = []) {
  if (kind !== "dispatch") return undefined
  if (command === "inputs") {
   inputs.set(`${r.session}\0${r.event}`, [...(r.result_ids ?? [])]);return reply({ result_ids: r.result_ids ?? [] })
  }
  if (command === "lease") { leases.add(r.owner); return reply(null) }
  if (command === "executions") return reply({ executions, orphaned: Object.values(executions).filter(d => !d.settled && !(d.owner && leases.has(d.owner))).map(d => d.dispatch) })
  if (command === "execution") return reply(executions[r.dispatch] ?? null)
  if (command === "resolve") return reply(activeChild(r.child,r.agent) ?? (holders[r.child] ? {root:holders[r.child]} : null))
  if (command === "prepare") {
   const ids=inputs.get(`${r.session}\0${r.event}`)
   if (ids === undefined) return denied(`launch refused: inputs absent for root ${r.session} unit ${r.event}; call dispatch_inputs before delegating`)
   if (stageAgents.includes(r.agent) && !claims.some(c => c.root_session_id===r.session && c.work_unit_id===r.event && c.agent_id===r.agent && c.pending)) return denied(`launch refused: no matching pending clean claim for session ${r.session}, work unit ${r.event}; call claim_assign`)
   if (r.author_keys !== undefined && (!Array.isArray(r.author_keys) || !r.author_keys.length || r.author_keys.some(x=>typeof x!=="string" || !x.trim()) || new Set(r.author_keys).size!==r.author_keys.length)) return denied("author_keys must list unique, nonempty staged author keys")
   if (executions[r.dispatch]) return reply({execution:executions[r.dispatch],launchable:false})
   if (Object.values(executions).some(d=>!d.settled && d.root===r.session && d.unit===r.event)) return denied("unit already in flight")
   const attempt=String(1+Object.values(executions).filter(d=>d.root===r.session && d.unit===r.event).length)
   const d={dispatch:r.dispatch,root:r.session,unit:r.event,attempt,agent:r.agent,caller:r.caller,background:r.background??false,admitted_at:Date.now(),inputs:[...ids],...(r.child?{child:r.child}:{}),...(r.owner?{owner:r.owner}:{})}
   executions[r.dispatch]=d;
   for(const author of r.author_keys??[])claims.push({key:`gate-${r.dispatch}-${author}`,root_session_id:r.session,work_unit_id:r.event,agent_id:r.agent,target_instance:r.agent,pending:true,scope:[],author_key:author})
   return reply({...d,launchable:true})
  }
  if (command === "attach_child") {
   const bound=activeChild(r.child,r.agent);if(bound)return reply(bound)
   if(Object.values(executions).some(d=>d.child===r.child))return reply(null)
   const d=r.dispatch?executions[r.dispatch]:Object.values(executions).find(d=>!d.settled&&!d.child&&d.root===r.session&&d.unit===r.event&&d.agent===r.agent&&d.caller===r.caller&&(!r.created_at||r.created_at>=d.admitted_at-1000))
   if(!d)return reply(null);d.child=r.child;d.launched=true;return reply(d)
  }
  if (command === "deliver") {
   const d=activeChild(r.child,r.agent)
   if(!d || (r.dispatch && d.dispatch!==r.dispatch) || (r.attempt && d.attempt!==r.attempt))return denied("deliver_result belongs to a running delegation; nothing was credited")
   d.delivery=[...new Set([...(d.delivery??[]),...r.result_ids])];return reply({result_ids:d.delivery})
  }
  if (command === "finish") {
   const d=executions[r.dispatch]
   if(d) d.settled=true
   return undefined
  }
  if (command === "switch") {holders[r.child]=r.session;return reply(null)}
  if (command === "session_deleted" || command === "handoff" || command === "abort_switch") {delete holders[r.child];return undefined}
  return undefined
 }
 return {inputs,executions,holders,answer,activeChild,reply,denied}
}
