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
const sessionNotFound=()=>Object.assign(new Error('session not found'),{_tag:'SessionNotFoundError'})
const call=(verb,request)=>JSON.parse(execFileSync(bin,[...verb,'--workspace',workspace,'--state',state,'--request',JSON.stringify(request)],{env:process.env,encoding:'utf8'}))
const db=resolve(state,'vfs.sqlite')
// One short-lived handle per access: the CLI under test writes the same file.
const withDB=f=>{const d=new DatabaseSync(db,{timeout:10000});try{return f(d)}finally{d.close()}}
const readState=()=>withDB(d=>JSON.parse(d.prepare('SELECT CAST(data AS TEXT) AS data FROM state WHERE id=1').get().data))
const writeState=s=>withDB(d=>d.prepare('UPDATE state SET data=? WHERE id=1').run(JSON.stringify(s)))
const events=[],hooks={},contexts=[],tools={},sessions=new Map([['root',{}],['other-root',{}]]),messages=new Map()
let wake,finished=false,beforeApproval=async()=>{},approvalCount=0
const idleSessions=new Set(),idleWaiters=new Map()
const markIdle=child=>{idleSessions.add(child);idleWaiters.get(child)?.();idleWaiters.delete(child)}
const waitForIdle=async({sessionID})=>{if(sessions.get(sessionID)?.running&&!idleSessions.has(sessionID))await new Promise(resolve=>idleWaiters.set(sessionID,resolve))}
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
 session:{get:async({sessionID})=>{if(!sessions.has(sessionID)){throw sessionNotFound()}return sessions.get(sessionID)},context:async({sessionID})=>{if(!sessions.has(sessionID)){throw sessionNotFound()}return messages.get(sessionID)??[]},hook:async(name,cb)=>{if(name==='context'){contexts.push(cb)}return {dispose(){}}},interrupt:async({sessionID})=>{markIdle(sessionID)},wait:waitForIdle,prompt:async()=>{},synthetic:async()=>{},create:async()=>{sessions.set('lent',{});return {id:'lent'}}},
 agent:{get:async()=>({data:{permissions:[{action:'*',resource:'*',effect:'allow'}]}})},
 tool:{hook:async(name,cb)=>{hooks[name]??=[];hooks[name].push(cb);return {dispose(){}}},transform:async cb=>{await cb({list:()=>[],get:()=>undefined,update(){},add:d=>tools[d.name]=d,namespace(){},remove(){}});return {dispose(){}}}},permission:{hook:async()=>({dispose(){}})},shell:{hook:async()=>({dispose(){}})},event:{subscribe:()=>({[Symbol.asyncIterator]:stream})}})
const root={sessionID:'root',agent:'takt',id:'root-tool',messageID:'root-message'},other={...root,sessionID:'other-root'}
const fire=async(name,e)=>{for(const cb of hooks[name]??[])await cb(e)}
const context=async(sessionID,agent)=>{for(const cb of contexts)await cb({sessionID,agent,system:[]})}
const base=content=>({present:true,content:Buffer.from(content).toString('base64'),mode:384})
try {
 // The real CLI issues the attempt, claim, binding, and staged work.
 await writeFile(resolve(workspace,'a.txt'),'base',{mode:0o600})
 await tools.dispatch_inputs.execute({work_unit_id:'author',result_ids:[]},root)
 const assigned=JSON.parse((await tools.claim_assign.execute({work_unit_id:'author',agent:'dev',scope:['a.txt']},root)).content)
 const key=assigned.key
 await fire('execute.before',{...root,id:'author-call',tool:'subagent',input:{description:'author',agent:'dev'}})
 sessions.set('child',{parentID:'root',title:'author',time:{created:Date.now()}})
 await context('child','dev')
 await tools.vfs_bind.execute({scope:['a.txt']},{sessionID:'child',agent:'dev'})
 await tools.vfs_write.execute({path:'a.txt',content:'candidate'},{sessionID:'child',agent:'dev'})
 await fire('execute.after',{...root,id:'author-call',tool:'subagent',status:'completed',result:{content:'running',metadata:{sessionID:'child',status:'running'}}})
 markIdle('child');publish({id:'author-idle',type:'session.idle',data:{sessionID:'child'}})
 for(let i=0;i<100&&!call(['dispatch'],{action:'execution',dispatch:'root:author-call'}).settled;i++)await sleep(5)
 assert.ok(call(['dispatch'],{action:'execution',dispatch:'root:author-call'}).settled)
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
 await tools.dispatch_inputs.execute({work_unit_id:'next',result_ids:[]},root)
 const admission={...root,id:'next-call',tool:'subagent',input:{description:'next',agent:'spec'}}
 await fire('execute.before',admission)
 assert.equal(call(['dispatch'],{action:'execution',dispatch:'root:next-call'}).ready,true)
 // Native orphan correlation checks the exact call even after its child vanished.
 await fire('execute.after',{...admission,status:'error',result:{content:'cancelled'}})
 await tools.dispatch_inputs.execute({work_unit_id:'orphan',result_ids:[]},root)
 call(['dispatch'],{action:'prepare',event:'orphan',session:'root',agent:'spec',dispatch:'root:orphan-call',caller:'root',owner:'dead'})
 call(['dispatch'],{action:'attach_child',dispatch:'root:orphan-call',child:'deleted-child',agent:'spec'})
 messages.set('root',[{type:'assistant',content:[{type:'tool',name:'subagent',id:'orphan-call',state:{status:'error'}}]}])
 await tools.dispatch_inputs.execute({work_unit_id:'after-orphan',result_ids:[]},root)
 await tools.claim_assign.execute({work_unit_id:'after-orphan',agent:'dev',scope:['b.txt']},root)
 await fire('execute.before',{...root,id:'after-orphan',tool:'subagent',input:{description:'after-orphan',agent:'dev'}})
 assert.equal(call(['dispatch'],{action:'execution',dispatch:'root:orphan-call'}).settled,true)
 // Admission itself may have persisted before its execution/child record. It
 // remains visible only as a nonlaunchable diagnostic until this exact native
 // call ends, then is compensated before even a subsequently denied admission.
 call(['dispatch'],{action:'admit',event:'orphan',session:'root',agent:'spec',dispatch:'root:no-execution'})
 assert.notEqual(call(['dispatch'],{action:'executions'}).executions['root:no-execution'].ready,true)
 messages.set('root',[{type:'assistant',content:[{type:'tool',name:'subagent',id:'no-execution',state:{status:'error'}}]}])
 await assert.rejects(()=>fire('execute.before',{...root,id:'missing-inputs',tool:'subagent',input:{description:'missing-inputs',agent:'spec'}}),/inputs absent/)
 assert.equal(call(['dispatch'],{action:'executions'}).executions['root:no-execution'],undefined)

 // Telemetry failure does not block repair or weaken VFS. Every decision still
 // uses the separate authoritative store and history.
 // An ingest still in flight may recreate the store between the two calls.
 const store=resolve(workspace,'.takt-ai/events.db')
 for(let i=0;;i++){
  await rm(store,{recursive:true,force:true})
  try{await mkdir(store);break}catch(e){
   if(e.code!=='EEXIST'||i>=100)throw e
   await sleep(10)
  }
 }
 sessions.set('telemetry-child',{parentID:'root',title:'after-orphan',time:{created:Date.now()}})
 await context('telemetry-child','dev')
 const writer={sessionID:'telemetry-child',agent:'dev'}
 await tools.vfs_bind.execute({scope:['b.txt']},writer)
 await tools.vfs_write.execute({path:'b.txt',content:'governed'},writer)
 await assert.rejects(tools.vfs_write.execute({path:'outside',content:'bypass'},writer),/scope|ownership/)
 assert.equal(Object.values(readState().Deltas).some(d=>d.Files['b.txt']),true)

 // A pending repair met by a plan declaration is asked of the user, and the
 // declaration continues. When the user cannot be asked, the harness's own
 // error surfaces as is: no invented continuation, no command for the user.
 await inject('candidate')
 beforeApproval=()=>{throw new Error('approval service down')}
 const offline=await tools.dispatch_commit.execute({version:'v-repair',plan:[{unit:'planned',contract:'c'}]},root).then(()=>assert.fail('commit ran over a pending repair'),e=>e.message)
 assert.ok(/approval service down/.test(offline)&&!/run:|could not be asked/.test(offline),offline)
 assert.ok(readState().Recovery)
 beforeApproval=async()=>{}
 const asked=approvalCount
 await tools.dispatch_commit.execute({version:'v-repair',plan:[{unit:'planned',contract:'c'},{unit:'probe',contract:'c'},{unit:'stuck',contract:'c'}]},root)
 assert.equal(approvalCount,asked+1);assert.equal(readState().Recovery,null)

 // A delegation whose calling session is gone can never return: the next
 // reconciliation ends it interrupted and frees its unit.
 call(['dispatch'],{action:'inputs',session:'gone-root',event:'lost',result_ids:[]})
 call(['dispatch'],{action:'prepare',event:'lost',session:'gone-root',agent:'spec',dispatch:'gone-root:lost-call',caller:'gone-root',owner:'dead'})
 await tools.dispatch_inputs.execute({work_unit_id:'probe',result_ids:[]},root)
 await fire('execute.before',{...root,id:'probe',tool:'subagent',input:{description:'probe',agent:'spec'}})
 assert.equal(call(['dispatch'],{action:'execution',dispatch:'gone-root:lost-call'}).settled,true)
 await fire('execute.after',{...root,id:'probe',tool:'subagent',status:'error',result:{content:'cancelled'}})

 // One that will never return although its caller lives is ended on the
 // user's decision; the unit can then be delegated again.
 await tools.dispatch_inputs.execute({work_unit_id:'stuck',result_ids:[]},root)
 call(['dispatch'],{action:'prepare',event:'stuck',session:'root',agent:'spec',dispatch:'root:stuck-call',caller:'root',owner:'dead'})
 call(['dispatch'],{action:'attach_child',dispatch:'root:stuck-call',child:'stuck-child',agent:'spec'})
 sessions.set('stuck-child',{parentID:'root',title:'stuck',running:true})
 await assert.rejects(()=>fire('execute.before',{...root,id:'stuck-retry',tool:'subagent',input:{description:'stuck',agent:'spec'}}),/root:stuck-call/)
 const before=approvalCount
 await tools.dispatch_abandon.execute({dispatch:'root:stuck-call'},root)
 assert.equal(approvalCount,before+1)
 assert.equal(call(['dispatch'],{action:'execution',dispatch:'root:stuck-call'}).settled,true)
 await fire('execute.before',{...root,id:'stuck-retry-2',tool:'subagent',input:{description:'stuck',agent:'spec'}})
 assert.equal(call(['dispatch'],{action:'execution',dispatch:'root:stuck-retry-2'}).ready,true)

 // While the interface is lent, its holder speaks with the user, so it asks
 // for a pending repair itself and hands the interface back.
 await tools.dispatch_switch.execute({target_agent:'pm',objective:"shape the brief",requirements:[]},root)
 await inject('candidate')
 const lentAsked=approvalCount
 await tools.dispatch_handoff.execute({result:'EarlyHandoff',additional_context:'stopping early',extra_artifacts:[],result_ids:[]},{sessionID:'lent',agent:'pm',id:'handoff',messageID:'lent-message'})
 // The user accepts the handoff itself, then the pending repair.
 assert.equal(approvalCount,lentAsked+2);assert.equal(readState().Recovery,null)
 assert.equal(call(['dispatch'],{action:'interlocutor',session:'root'}).holder,'')
} finally {cleanup();finished=true;wake?.();await sleep(100)}
