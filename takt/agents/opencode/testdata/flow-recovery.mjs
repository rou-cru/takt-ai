import assert from 'node:assert/strict'
import {spawn,execFileSync} from 'node:child_process'
import {DatabaseSync} from 'node:sqlite'
import {Readable} from 'node:stream'
import {mkdir,writeFile,readFile,rm} from 'node:fs/promises'
import {resolve} from 'node:path'
import plugin from './takt-vfs.ts'
const bin=process.argv[2], workspace=resolve('workspace'),state=resolve('workspace/.takt-ai/vfs')
await mkdir(workspace,{recursive:true});await mkdir(process.env.HOME,{recursive:true})
const sleep=ms=>new Promise(r=>setTimeout(r,ms))
const call=(verb,request)=>JSON.parse(execFileSync(bin,[...verb,'--workspace',workspace,'--state',state,'--request',JSON.stringify(request)],{env:process.env,encoding:'utf8'}))
const db=resolve(state,'vfs.sqlite')
// One short-lived handle per access: the CLI under test writes the same file.
const withDB=f=>{const d=new DatabaseSync(db,{timeout:10000});try{return f(d)}finally{d.close()}}
const readState=()=>withDB(d=>JSON.parse(d.prepare('SELECT CAST(data AS TEXT) AS data FROM state WHERE id=1').get().data))
const writeState=s=>withDB(d=>d.prepare('UPDATE state SET data=? WHERE id=1').run(JSON.stringify(s)))
const events=[],hooks={},contexts=[],tools={},sessions=new Map([['root',{}],['other-root',{}]]),messages=new Map()
let wake,finished=false,interruptFails=false,beforeApproval=async()=>{},approvalCount=0,promptCount=0
const interrupted=[]
async function* stream(){
 for(;;){
  if(events.length){yield events.shift();continue}
  if(finished){return}
  await new Promise(r=>wake=r)
 }
}
const publish=e=>{events.push(e);wake?.()}
globalThis.createPermission=async r=>{
 approvalCount++;await beforeApproval(r)
 publish({id:`reply-${approvalCount}`,type:'permission.replied',data:{requestID:r.id,reply:'once'}})
 // Deliver the event before create resolves: this used to lose approvals.
 await sleep(5)
 return {id:r.id,effect:'ask'}
}
globalThis.Bun={spawn(argv,opts){
 if(argv[1]==='codegraph')return {stdout:'',stderr:'index unavailable',exited:Promise.resolve(1)}
 const p=spawn(argv[0],argv.slice(1),{cwd:opts.cwd,env:process.env,detached:opts.detached,stdio:[opts.stdin==='ignore'?'ignore':'pipe','pipe','pipe']})
 return {stdin:p.stdin,stdout:Readable.toWeb(p.stdout),stderr:Readable.toWeb(p.stderr),kill:signal=>p.kill(signal),exited:new Promise((r,j)=>{p.on('error',j);p.on('close',r)})}
}}
const cleanup=await plugin.setup({location:{directory:workspace},
 session:{get:async({sessionID})=>{if(!sessions.has(sessionID))throw {_tag:'SessionNotFoundError'};return sessions.get(sessionID)},context:async({sessionID})=>{if(!sessions.has(sessionID))throw {_tag:'SessionNotFoundError'};return messages.get(sessionID)??[]},hook:async(name,cb)=>{if(name==='context')contexts.push(cb);return {dispose(){}}},interrupt:async({sessionID})=>{if(interruptFails)throw Error('host interruption failed');interrupted.push(sessionID)},wait:async()=>{},prompt:async()=>{promptCount++},synthetic:async()=>{},create:async()=>{sessions.set('lent',{});return {id:'lent'}}},
 agent:{get:async()=>({data:{permissions:[{action:'*',resource:'*',effect:'allow'}]}})},
 tool:{hook:async(name,cb)=>{(hooks[name]??=[]).push(cb);return {dispose(){}}},transform:async cb=>{await cb({list:()=>[],get:()=>undefined,update(){},add:d=>tools[d.name]=d,namespace(){},remove(){}});return {dispose(){}}}},permission:{hook:async()=>({dispose(){}})},shell:{hook:async()=>({dispose(){}})},event:{subscribe:()=>({[Symbol.asyncIterator]:stream})}})
const root={sessionID:'root',agent:'takt',id:'root-tool',messageID:'root-message'},other={...root,sessionID:'other-root'}
const fire=async(name,e)=>{for(const cb of hooks[name]??[])await cb(e)}
const context=async(sessionID,agent)=>{for(const cb of contexts)await cb({sessionID,agent,system:[]})}
const base=content=>({present:true,content:Buffer.from(content).toString('base64'),mode:384})
try {
 // The real CLI issues the attempt, claim, binding, and staged work.
 await writeFile(resolve(workspace,'a.txt'),'base',{mode:0o600})
 await tools.dispatch_inputs.execute({work_unit_id:'author',none:true},root)
 const assigned=JSON.parse((await tools.claim_assign.execute({work_unit_id:'author',agent:'dev',scope:['a.txt']},root)).content)
 const key=assigned.key
 await fire('execute.before',{...root,id:'author-call',tool:'subagent',input:{description:'author',agent:'dev'}})
 sessions.set('child',{parentID:'root',title:'author',time:{created:Date.now()}})
 await context('child','dev')
 await tools.vfs_bind.execute({scope:['a.txt']},{sessionID:'child',agent:'dev'})
 await tools.vfs_write.execute({path:'a.txt',content:'candidate'},{sessionID:'child',agent:'dev'})
 await fire('execute.after',{...root,id:'author-call',tool:'subagent',status:'completed',result:{content:'done'}})
 const inject=async(content)=>{
  const s=readState();s.Recovery={Agent:key,Items:[{Path:'a.txt',Temp:'.takt-test-temp',Before:base('base'),After:base('candidate')}],Intent:1,Dirs:[]};writeState(s)
  await writeFile(resolve(workspace,'a.txt'),content,{mode:0o600})
 }
 await inject('candidate')
 // Specialist gets useful diagnostics, but no repair or consent is consumed.
 await assert.rejects(tools.vfs_read.execute({path:'a.txt'},{sessionID:'child',agent:'dev',id:'read',messageID:'m'}),/return this diagnostic to the orchestrator/)
 assert.equal(approvalCount,0);assert.ok(readState().Recovery)
 // A different root can approve the concrete restoration; ownership still
 // refuses consolidation of the previous root's work.
 await tools.claim_list.execute({},other)
 assert.equal(await readFile(resolve(workspace,'a.txt'),'utf8'),'base')
 assert.equal(readState().Recovery,null)
 await assert.rejects(tools.vfs_consolidate.execute({author_key:key},other),/another root|does not belong|identity/)
 // State changed during approval: no restoration of the new physical content.
 await inject('candidate')
 beforeApproval=async()=>writeFile(resolve(workspace,'a.txt'),'external')
 await assert.rejects(tools.claim_list.execute({},root),/approval is stale/)
 assert.equal(await readFile(resolve(workspace,'a.txt'),'utf8'),'external');assert.ok(readState().Recovery)
 beforeApproval=async()=>{}
 await tools.claim_list.execute({},root)
 assert.equal(readState().Recovery,null)
 assert.ok(Object.keys(readState().Interrupted).some(id=>id.startsWith('manifest/')))
 await assert.rejects(tools.vfs_consolidate.execute({author_key:key},root),/physical base changed/)
 assert.ok(readState().Deltas[key].Files['a.txt'])
 // A divergent cycle is yielded only after host termination. Its approval
 // preserves current files, retains evidence, and closes it as interrupted.
 let s=readState();s.Bindings[key].CycleID='audit-cycle'
 s.Cycles={'audit-cycle':{Agent:key,Items:{'a.txt':{Path:'a.txt',Before:base('base'),After:base('candidate')}},Dirs:[]}}
 s.Maintenance={version:1,units:0,mutations:0,cursor:-1,since:-1,deferrals:0,next_mandate:0,requested:false,session:'root',cycle:{phase:'acceptance',plan:{session_id:'root',cycle_id:'audit-cycle',mandate_class:'complexity',delta:[],closure:['a.txt'],reachability:'codegraph'},scope:['a.txt'],sessions:{},report:null,started:new Date().toISOString()}}
 writeState(s)
 call(['gc','coordinate'],{action:'attach',event:'audit-cycle',role:'verifier',child:'lane',owner:'test-owner'})
 sessions.set('lane',{})
 await tools.dispatch_inputs.execute({work_unit_id:'next',none:true},root)
 const admission={...root,id:'next-call',tool:'subagent',input:{description:'next',agent:'spec'}}
 interruptFails=true
 await assert.rejects(fire('execute.before',admission),/interruption failed/)
 assert.equal(readState().Maintenance.cycle.plan.cycle_id,'audit-cycle')
 assert.equal(call(['dispatch'],{action:'executions'}).executions['root:next-call'],undefined)
 const incompleteClosure=readState()
 interruptFails=false
 await fire('execute.before',admission)
 assert.ok(interrupted.includes('lane'));s=readState()
 assert.equal(s.Maintenance.cycle,undefined)
 assert.ok(s.Interrupted['cycle/audit-cycle'])
 assert.equal(s.Maintenance.history.at(-1).reason.includes('interrupted'),true)
 assert.equal(Object.values(s.Bindings).some(b=>b.CycleID==='audit-cycle'),false)
 assert.equal(await readFile(resolve(workspace,'a.txt'),'utf8'),'external')
 assert.equal(call(['dispatch'],{action:'execution',dispatch:'root:next-call'}).ready,true)
 // A crash after the archive committed but before staging/coordinator closure
 // must finish the same interruption without a new approval or acceptance.
 const approved=approvalCount
 s.Maintenance=incompleteClosure.Maintenance
 s.Bindings=incompleteClosure.Bindings
 s.Deltas=incompleteClosure.Deltas
 writeState(s)
 await tools.gc_request.execute({},other)
 for(let i=0;i<200 && readState().Maintenance.cycle;i++)await sleep(10)
 s=readState()
 assert.equal(s.Maintenance.cycle,undefined)
 assert.equal(Object.values(s.Bindings).some(b=>b.CycleID==='audit-cycle'),false)
 assert.ok(s.Interrupted['cycle/audit-cycle'])
 assert.equal(approvalCount,approved,'durable interrupted closure requested approval again')
 // Native orphan correlation checks the exact call even after its child vanished.
 await fire('execute.after',{...admission,status:'error',result:{content:'cancelled'}})
 await tools.dispatch_inputs.execute({work_unit_id:'orphan',none:true},root)
 call(['dispatch'],{action:'prepare',event:'orphan',session:'root',agent:'spec',dispatch:'root:orphan-call',caller:'root',owner:'dead',background:false})
 call(['dispatch'],{action:'attach_child',dispatch:'root:orphan-call',child:'deleted-child',agent:'spec'})
 messages.set('root',[{type:'assistant',content:[{type:'tool',name:'subagent',id:'orphan-call',state:{status:'error'}}]}])
 await tools.dispatch_inputs.execute({work_unit_id:'after-orphan',none:true},root)
 await tools.claim_assign.execute({work_unit_id:'after-orphan',agent:'dev',scope:['b.txt']},root)
 await fire('execute.before',{...root,id:'after-orphan',tool:'subagent',input:{description:'after-orphan',agent:'dev'}})
 assert.equal(call(['dispatch'],{action:'execution',dispatch:'root:orphan-call'}).settled,true)
 // Admission itself may have persisted before its execution/child record. It
 // remains visible only as a nonlaunchable diagnostic until this exact native
 // call ends, then is compensated before even a subsequently denied admission.
 call(['dispatch'],{action:'admit',event:'orphan',session:'root',agent:'spec',dispatch:'root:no-execution'})
 assert.notEqual(call(['dispatch'],{action:'executions'}).executions['root:no-execution'].ready,true)
 messages.set('root',[{type:'assistant',content:[{type:'tool',name:'subagent',id:'no-execution',state:{status:'error'}}]}])
 await assert.rejects(fire('execute.before',{...root,id:'missing-inputs',tool:'subagent',input:{description:'missing-inputs',agent:'spec'}}),/inputs absent/)
 assert.equal(call(['dispatch'],{action:'executions'}).executions['root:no-execution'],undefined)

 // Restart sees a durable prompt reservation with no submitted turn, or a lane
 // the host deleted. Neither permits a resend or an indefinite reservation.
 for(const scenario of ['lost-prompt','deleted-lane']) {
  const cycle=`audit-${scenario}`,child=`lane-${scenario}`
  s=readState();s.Maintenance={version:1,units:0,mutations:0,cursor:-1,since:-1,deferrals:0,next_mandate:0,requested:false,session:'root',cycle:{phase:'baseline',plan:{session_id:'root',cycle_id:cycle,mandate_class:'complexity',delta:[],closure:[],reachability:'codegraph'},scope:[],sessions:{},report:null,started:new Date().toISOString()}}
  writeState(s)
  call(['gc','coordinate'],{action:'attach',event:cycle,role:'verifier',child,owner:'dead-owner'})
  call(['gc','coordinate'],{action:'prompt',event:cycle,child,phase:'baseline',owner:'dead-owner'})
  if(scenario==='lost-prompt')sessions.set(child,{})
  const before=promptCount
  await tools.gc_request.execute({},root)
  for(let i=0;i<200 && readState().Maintenance.cycle;i++)await sleep(10)
  assert.equal(readState().Maintenance.cycle,undefined,`${scenario} never closed`)
  assert.equal(promptCount,before,'uncertain prompt was resent')
  const lane=call(['dispatch'],{action:'execution',dispatch:`${cycle}/verifier`})
  assert.equal(lane.terminated,true);assert.equal(lane.settled,true)
 }
 // Telemetry failure does not block repair or weaken VFS. Every decision still
 // uses the separate authoritative store and history.
 await rm(resolve(workspace,'.takt-ai/events.db'),{force:true})
 await mkdir(resolve(workspace,'.takt-ai/events.db'))
 sessions.set('telemetry-child',{parentID:'root',title:'after-orphan',time:{created:Date.now()}})
 await context('telemetry-child','dev')
 const writer={sessionID:'telemetry-child',agent:'dev'}
 await tools.vfs_bind.execute({scope:['b.txt']},writer)
 await tools.vfs_write.execute({path:'b.txt',content:'governed'},writer)
 await assert.rejects(tools.vfs_write.execute({path:'outside',content:'bypass'},writer),/scope|ownership/)
 assert.equal(Object.values(readState().Deltas).some(d=>d.Files['b.txt']),true)

 // A pending repair met by a plan declaration is asked of the user, and the
 // declaration continues. When the user cannot be asked, the refusal carries
 // the exact command that applies the same approval outside the session.
 await inject('candidate')
 beforeApproval=()=>{throw Error('approval service down')}
 const offline=await tools.dispatch_commit.execute({version:'v-repair',plan:[{unit:'planned',contract:'c'}]},root).then(()=>assert.fail('commit ran over a pending repair'),e=>e.message)
 const command=offline.slice(offline.lastIndexOf('run: ')+5)
 assert.ok(readState().Recovery)
 execFileSync('/bin/sh',['-c',command],{env:process.env})
 assert.equal(readState().Recovery,null)
 beforeApproval=async()=>{}
 await inject('candidate')
 const asked=approvalCount
 await tools.dispatch_commit.execute({version:'v-repair',plan:[{unit:'planned',contract:'c'},{unit:'probe',contract:'c'},{unit:'stuck',contract:'c'}]},root)
 assert.equal(approvalCount,asked+1);assert.equal(readState().Recovery,null)

 // A delegation whose calling session is gone can never return: the next
 // reconciliation ends it interrupted and frees its unit.
 call(['dispatch'],{action:'inputs',session:'gone-root',event:'lost',none:true})
 call(['dispatch'],{action:'prepare',event:'lost',session:'gone-root',agent:'spec',dispatch:'gone-root:lost-call',caller:'gone-root',owner:'dead',background:false})
 await tools.dispatch_inputs.execute({work_unit_id:'probe',none:true},root)
 await fire('execute.before',{...root,id:'probe',tool:'subagent',input:{description:'probe',agent:'spec'}})
 assert.equal(call(['dispatch'],{action:'execution',dispatch:'gone-root:lost-call'}).settled,true)
 await fire('execute.after',{...root,id:'probe',tool:'subagent',status:'error',result:{content:'cancelled'}})

 // One that will never return although its caller lives is ended on the
 // user's decision; the unit can then be delegated again.
 await tools.dispatch_inputs.execute({work_unit_id:'stuck',none:true},root)
 call(['dispatch'],{action:'prepare',event:'stuck',session:'root',agent:'spec',dispatch:'root:stuck-call',caller:'root',owner:'dead',background:false})
 call(['dispatch'],{action:'attach_child',dispatch:'root:stuck-call',child:'stuck-child',agent:'spec'})
 await assert.rejects(fire('execute.before',{...root,id:'stuck-retry',tool:'subagent',input:{description:'stuck',agent:'spec'}}),/root:stuck-call/)
 const before=approvalCount
 await tools.dispatch_abandon.execute({dispatch:'root:stuck-call'},root)
 assert.equal(approvalCount,before+1)
 assert.equal(call(['dispatch'],{action:'execution',dispatch:'root:stuck-call'}).settled,true)
 await fire('execute.before',{...root,id:'stuck-retry-2',tool:'subagent',input:{description:'stuck',agent:'spec'}})
 assert.equal(call(['dispatch'],{action:'execution',dispatch:'root:stuck-retry-2'}).ready,true)

 // While the interface is lent, its holder speaks with the user, so it asks
 // for a pending repair itself and hands the interface back.
 await tools.dispatch_switch.execute({target_agent:'pm',objective:'shape the brief'},root)
 await inject('candidate')
 const lentAsked=approvalCount
 await tools.dispatch_handoff.execute({result:'EarlyHandoff',additional_context:'stopping early',extra_artifacts:[],result_ids:[]},{sessionID:'lent',agent:'pm',id:'handoff',messageID:'lent-message'})
 assert.equal(approvalCount,lentAsked+1);assert.equal(readState().Recovery,null)
 assert.equal(call(['dispatch'],{action:'interlocutor',session:'root'}).holder,'')
} finally {cleanup();finished=true;wake?.();await sleep(100)}
