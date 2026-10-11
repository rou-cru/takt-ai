import assert from 'node:assert/strict'
import { spawn, execFileSync } from 'node:child_process'
import { Readable } from 'node:stream'
import { mkdir, readFile } from 'node:fs/promises'
import { resolve } from 'node:path'
import { createServer } from 'node:http'
import plugin from './takt-vfs.ts'
const workspace=resolve('workspace'),state=resolve('workspace/.takt-ai/vfs'),bin=process.argv[2]
await mkdir(workspace,{recursive:true}); await mkdir(process.env.HOME,{recursive:true})
globalThis.Bun={spawn(argv,opts){
 if(argv[1]==='codegraph')return {stdout:'',stderr:'',exited:Promise.resolve(0)}
 const p=spawn(argv[0],argv.slice(1),{cwd:opts.cwd,env:process.env,detached:opts.detached,stdio:[opts.stdin==='ignore'?'ignore':'pipe','pipe','pipe']})
 return {stdin:p.stdin,stdout:Readable.toWeb(p.stdout),stderr:Readable.toWeb(p.stderr),exited:new Promise((r,j)=>{p.on('error',j);p.on('close',r)})}
}}
// The memory subsystem is real; only its remote Engram service is isolated.
const observations=new Map(); let sequence=0
const memoryServer=createServer(async(req,res)=>{
 let body=''; for await(const chunk of req)body+=chunk
 const data=body?JSON.parse(body):{}
 let reply={}
 if(req.method==='POST'&&req.url==='/observations'){const id=++sequence;observations.set(id,{...data,id});reply={id}}
 else if(req.url.startsWith('/observations/')){reply=observations.get(Number(req.url.split('/').at(-1)));if(!reply){res.writeHead(404);res.end('{}');return}}
 else if(req.url.startsWith('/project/current'))reply={project:'fixture',cwd:workspace}
 res.setHeader('Content-Type','application/json');res.end(JSON.stringify(reply))
})
await new Promise(r=>memoryServer.listen(0,'127.0.0.1',r))
process.env.ENGRAM_BASE_URL=`http://127.0.0.1:${memoryServer.address().port}`
async function recordResult(){
 const p=spawn(bin,['memory','record'],{env:process.env,stdio:['pipe','pipe','pipe']}); let out='',err=''
 p.stdout.on('data',c=>out+=c);p.stderr.on('data',c=>err+=c)
 p.stdin.end(JSON.stringify({author:'verify',session:'root-b',directory:workspace,nature:'observation',scope:'project',title:'Independent verdict',content:'All four staged authors checked',evidence:'Read all staged files'}))
 const code=await new Promise(r=>p.on('close',r));assert.equal(code,0,err);return JSON.parse(out).result.id
}
const history=r=>JSON.parse(execFileSync(bin,['dispatch','--workspace',workspace,'--state',state,'--request',JSON.stringify(r)],{env:process.env,encoding:'utf8'}))
const sessions=new Map(),waiting=new Map(),hostMessages=new Map()
const permissions=['vfs_bind','vfs_read','vfs_write','vfs_delete','vfs_verify'].map(action=>({action,resource:'*',effect:'allow'}))
async function start(){
 const tools={},hooks={},contexts=[]
 await plugin.setup({location:{directory:workspace},storage:{get(){throw Error('plugin authority read')},set(){throw Error('plugin authority write')}},
 session:{get:async({sessionID})=>sessions.get(sessionID)??{id:sessionID},context:async({sessionID})=>hostMessages.get(sessionID)??[],hook:async(name,cb)=>{if(name==='context')contexts.push(cb);return {dispose(){}}},wait:({sessionID})=>new Promise(r=>{waiting.set(sessionID,r)}),interrupt:async()=>{},prompt:async()=>{},synthetic:async()=>{}},
 agent:{get:async()=>({data:{permissions}})},tool:{hook:async(name,cb)=>{(hooks[name]??=[]).push(cb);return {dispose(){}}},transform:async(cb)=>{cb({list:()=>[],get:()=>undefined,update(){},add(d){tools[d.name]=d},namespace(){},remove(){}});return {dispose(){}}}},
 permission:{hook:async()=>({dispose(){}})},shell:{hook:async()=>({dispose(){}})},event:{subscribe:()=>({[Symbol.asyncIterator]:()=>({next:()=>new Promise(()=>{})})})}})
 return {tools,async fire(name,e){for(const cb of hooks[name]??[])await cb(e)},async context(sessionID,agent){const e={sessionID,agent,system:[]};for(const cb of contexts)await cb(e);return e}}
}
const a=await start(),rootA={sessionID:'root-a',agent:'takt'},rootB={sessionID:'root-b',agent:'takt'}
const units=Array.from({length:4},(_,i)=>`author-${i}`)
await a.tools.dispatch_commit.execute({version:'a',plan:units.map(unit=>({unit,contract:`stage ${unit}`}))},rootA)
async function launch(p,root,unit,agent,id,extra={}){
 const e={tool:'subagent',sessionID:root.sessionID,agent:'takt',id,input:{description:unit,agent,...extra}}
 await p.fire('execute.before',e)
 return e
}
async function done(p,e){await p.fire('execute.after',{...e,status:'completed',result:{content:'done'}})}
const authors=[]
for(const [i,unit] of units.entries()){
 await a.tools.dispatch_inputs.execute({work_unit_id:unit,none:true},rootA)
 const claim=JSON.parse((await a.tools.claim_assign.execute({work_unit_id:unit,agent:'dev',scope:[`${i}.txt`]},rootA)).content)
 const e=await launch(a,rootA,unit,'dev',`write-${i}`)
 const child=`dev-${i}`;sessions.set(child,{id:child,parentID:'root-a',title:unit,time:{created:Date.now()}})
 await a.context(child,'dev')
 await a.tools.vfs_bind.execute({}, {sessionID:child,agent:'dev'})
 await a.tools.vfs_write.execute({path:`${i}.txt`,content:`valid work ${i}`},{sessionID:child,agent:'dev'})
 await done(a,e)
 authors.push(claim.key)
}
// A new plugin instance has no stored authority. Rehome all four author keys,
// keeping their staged data, then deliver all gates in CodeMode (no tools map).
const b=await start()
await b.tools.dispatch_commit.execute({version:'b',plan:[...units.map(unit=>({unit,contract:'continue'})),{unit:'review',contract:'judge four authors'}]},rootB)
for(const [i,unit]of units.entries()){
 await b.tools.dispatch_inputs.execute({work_unit_id:unit,none:true},rootB)
 const r=JSON.parse((await b.tools.claim_assign.execute({work_unit_id:unit,agent:'dev',scope:[`${i}.txt`],author_key:authors[i]},rootB)).content)
 assert.equal(r.key,authors[i])
}
await b.tools.dispatch_inputs.execute({work_unit_id:'review',none:true},rootB)
const review=await launch(b,rootB,'review','verify','review-1',{author_keys:authors})
sessions.set('judge',{id:'judge',parentID:'root-b',title:'review',time:{created:Date.now()}})
const context=await b.context('judge','verify')
for(const key of authors)assert.ok(context.system.some(t=>t.text.includes(`author_key ${key}:`)),`missing gate ${key}`)
const c=await start() // current links/gates survive another process/plugin
for(const[i,key]of authors.entries()){
 const caller={sessionID:'judge',agent:'verify'}
 const content=await c.tools.vfs_read.execute({path:`${i}.txt`,author_key:key},caller)
 assert.ok(content.content.includes(`valid work ${i}`))
 await c.tools.vfs_verify.execute({author_key:key,pass:true,finding:'checked'},caller)
 await c.tools.vfs_consolidate.execute({author_key:key},rootB)
 assert.equal(await readFile(resolve(workspace,`${i}.txt`),'utf8'),`valid work ${i}`)
}
const resultID=await recordResult()
await c.tools.deliver_result.execute({result_ids:[resultID]},{sessionID:'judge',agent:'verify'})
await done(c,review)
// A delayed callback cannot finish the new attempt, and a duplicate hook cannot
// cancel an admission that might already have crossed the native launch boundary.
const retry=await launch(c,rootB,'review','verify','review-2',{sessionID:'judge',author_keys:authors})
await assert.rejects(()=>launch(c,rootB,'review','verify','review-2',{sessionID:'judge',author_keys:authors}),/already prepared/)
await done(a,review)
const d=history({action:'execution',dispatch:'root-b:review-2'})
assert.equal(d.attempt,'2');assert.ok(!d.settled)
await c.tools.deliver_result.execute({result_ids:[resultID]},{sessionID:'judge',agent:'verify'})
await done(c,retry)
// A plugin restart alone cannot settle a prepared call with no observed child.
// A terminal record of that exact native call does, allowing the existing retry.
const rootC={sessionID:'root-c',agent:'takt'}
await c.tools.dispatch_commit.execute({version:'c',plan:[{unit:'transport',contract:'recover interrupted native launch'}]},rootC)
await c.tools.dispatch_inputs.execute({work_unit_id:'transport',none:true},rootC)
await launch(c,rootC,'transport','pm','orphan')
const restarted=await start()
const hostCall=status=>[{type:'assistant',content:[{type:'tool',id:'orphan',name:'subagent',state:{status}}]}]
await assert.rejects(()=>launch(restarted,rootC,'transport','pm','retry-absent'))
assert.ok(!history({action:'execution',dispatch:'root-c:orphan'}).settled)
hostMessages.set('root-c',hostCall('running'))
await assert.rejects(()=>launch(restarted,rootC,'transport','pm','retry-running'))
assert.ok(!history({action:'execution',dispatch:'root-c:orphan'}).settled)
// A different tool call's error is not evidence about the orphan either.
hostMessages.set('root-c',[{type:'assistant',content:[{type:'tool',id:'other',name:'subagent',state:{status:'error'}}]}])
await assert.rejects(()=>launch(restarted,rootC,'transport','pm','retry-other'))
assert.ok(!history({action:'execution',dispatch:'root-c:orphan'}).settled)
hostMessages.set('root-c',hostCall('error'))
const recovered=await launch(restarted,rootC,'transport','pm','recovered')
assert.ok(history({action:'execution',dispatch:'root-c:orphan'}).settled)
assert.equal(history({action:'execution',dispatch:'root-c:recovered'}).attempt,'2')
await restarted.fire('execute.after',{...recovered,status:'error',result:{content:'native failure'}})
await new Promise(r=>memoryServer.close(r))
console.log('four authors retained across roots; all gates adopted; real core verdict/consolidation; restart/retry/late callback passed')
