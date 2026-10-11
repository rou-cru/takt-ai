import {expect,test} from "bun:test"
import {ipcOutput as vfsIPC} from "./takt-vfs"
import {ipcOutput as memoryIPC} from "./takt-memory"

for (const [name,read] of [["VFS",vfsIPC],["memory",memoryIPC]] as const) {
  test(`${name} drains stderr alongside stdout without backpressure`,async()=>{
    const proc=Bun.spawn(["/bin/sh","-c","i=0; while [ $i -lt 4096 ]; do printf 'xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx' >&2; i=$((i+1)); done; printf result"],{stdout:"pipe",stderr:"pipe"})
    const [out,err,code]=await read(proc,"test pipe",2000)
    expect(code).toBe(0);expect(out).toBe("result");expect(err.length).toBe(262144)
  })
  test(`${name} cancels an owned hung IPC process on its deadline`,async()=>{
    const proc=Bun.spawn(["/bin/sh","-c","exec sleep 30"],{stdout:"pipe",stderr:"pipe"})
    await expect(read(proc,"hung IPC",30)).rejects.toThrow("owned IPC process was cancelled")
    await proc.exited
    expect(proc.signalCode).toBe("SIGKILL")
  })
}
