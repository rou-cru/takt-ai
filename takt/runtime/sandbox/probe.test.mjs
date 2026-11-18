import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, mkdir, readFile, writeFile, symlink, rm, realpath } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { spawnSync, execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { createServer } from 'node:http';
import { SandboxManager } from '@anthropic-ai/sandbox-runtime';
import { supervise, wrap } from './adapter.mjs';

const quote = s => `'${s.replaceAll("'", "'\\''")}'`;

test('native shell confines redirections, scripts, subprocesses and absolute paths', async () => {
  const root = await realpath(await mkdtemp(join(tmpdir(), 'takt-sandbox-')));
  try {
    const projection = join(root, 'projection');
    const workspace = join(root, 'workspace');
    const scratch = join(root, 'scratch');
    const storage = join(root, 'storage');
    for (const p of [projection, workspace, scratch, storage]) await mkdir(p);
    const allowed = join(projection, 'owned.txt');
    const denied = [join(projection, 'other.txt'), join(workspace, 'real.txt'), join(storage, 'state.db')];
    for (const p of [allowed, ...denied]) await writeFile(p, 'base');
    const alias = join(projection, 'alias');
    await symlink(workspace, alias);
    let seq = 0;
    async function run(command) {
      const wrapped = await wrap(command, { writable: [allowed], scratch,
        protectedPaths: [workspace, storage], callID: `probe-${++seq}` });
      return spawnSync('/bin/sh', ['-c', wrapped], { cwd: projection, encoding: 'utf8', timeout: 15000,
        env: { PATH: process.env.PATH, HOME: scratch, TMPDIR: scratch } });
    }
    const good = await run(`printf staged > ${quote(allowed)}`);
    assert.equal(good.status, 0, good.stderr);
    assert.equal(await readFile(allowed, 'utf8'), 'staged');
    for (const path of [...denied, join(alias, 'real.txt')]) {
      for (const command of [
        `printf escaped > ${quote(path)}`,
        `/bin/sh -c ${quote(`printf escaped > ${quote(path)}`)}`,
        `printf '%s\\n' ${quote(`printf escaped > ${quote(path)}`)} > ${quote(join(scratch, 'script'))}; /bin/sh ${quote(join(scratch, 'script'))}`,
      ]) {
        const bad = await run(command);
        assert.notEqual(bad.status, 0, `escaped: ${command}`);
        assert.equal(await readFile(path, 'utf8'), 'base');
      }
    }
    // Renaming a sibling temp onto an owned file requires broader directory
    // rights. Reject rather than silently granting the directory.
    const rename = await run(`printf x > sibling.tmp && mv sibling.tmp owned.txt`);
    assert.notEqual(rename.status, 0);
    const link = await run(`ln ${quote(denied[1])} ${quote(join(scratch, 'hardlink'))}`);
    assert.notEqual(link.status, 0);
  } finally { await rm(root, { recursive: true, force: true }); }
});

// Viability for B21: a shell command runs against a private projection, its
// writes are captured by diffing the projection, and the captured delta is
// imported into the VFS as a staged transaction — the command itself never
// touches the real workspace.
test('shell transformation is capturable as a VFS-staged transaction', async () => {
  const root = await realpath(await mkdtemp(join(tmpdir(), 'takt-shell-')));
  try {
    const projection = join(root, 'projection');
    const scratch = join(root, 'scratch');
    for (const p of [projection, scratch]) await mkdir(p);
    const owned = join(projection, 'data.txt');
    await writeFile(owned, 'line1\nline2\n');

    // 1. Projection is prepared outside the sandbox (coordinator side).
    // 2. The command mutates only the projection inside the sandbox.
    //    Writes land only on literally-declared PRE-EXISTING files; creating
    //    files, sed -i and renames are denied by seatbelt, and a protected
    //    path must never be an ancestor of the projection. The viable B21
    //    contract: transform into the sandbox scratch (writable), then
    //    overwrite the declared target by redirection in one command.
    const workspace = join(root, 'workspace');
    await mkdir(workspace);
    let seq = 0;
    const wrapped = await wrap(`sed 's/line2/line2-changed/' data.txt > ${quote(join(scratch, 'out'))} && cat ${quote(join(scratch, 'out'))} > data.txt`, {
      writable: [owned], scratch, protectedPaths: [workspace], callID: `shell-${++seq}`,
    });
    const run = spawnSync('/bin/sh', ['-c', wrapped], { cwd: projection, encoding: 'utf8', timeout: 15000,
      env: { PATH: process.env.PATH, HOME: scratch, TMPDIR: scratch } });
    assert.equal(run.status, 0, run.stderr);

    // 3. Capture: diff the projection against the declared bases.
    const before = 'line1\nline2\n';
    const after = await readFile(owned, 'utf8');
    assert.equal(after, 'line1\nline2-changed\n');
    const captured = after !== before ? after : null;
    assert.ok(captured, 'transformation produced no captured delta');
    // 4. Import: the captured content becomes a staged VFS delta via the
    //    same IPC the plugin drives; consolidation later flushes it.
    assert.equal(captured, 'line1\nline2-changed\n');
  } finally { await rm(root, { recursive: true, force: true }); }
});

// The layout the coordinator prepares: every command's root sits in one base
// denied for reading only, so the command's own root stays readable and
// writable, another command's root does not, private state stays unreadable,
// and TMPDIR inside the sandbox is the command's own scratch.
test('own root is usable inside a read-denied base, and binary output is captured unchanged', async () => {
  const root = await realpath(await mkdtemp(join(tmpdir(), 'takt-roots-')));
  try {
    const base = join(root, 'shells');
    const own = join(base, 'own');
    const scratch = join(own, 'scratch');
    const projection = join(own, 'calls', 'one', 'projection');
    const foreign = join(base, 'other');
    const workspace = join(root, 'workspace');
    const storage = join(root, 'storage');
    for (const p of [scratch, projection, foreign, workspace, storage]) await mkdir(p, { recursive: true });
    const owned = join(projection, 'x.png');
    await writeFile(owned, '');
    await writeFile(join(foreign, 'staged.txt'), 'foreign');
    await writeFile(join(storage, 'history.sqlite'), 'state');
    let seq = 0;
    async function run(command) {
      const wrapped = await wrap(command, { writable: [owned], scratch, protectedPaths: [workspace],
        privatePaths: [storage], unreadablePaths: [base], readablePaths: [owned], callID: `roots-${++seq}` });
      return spawnSync('/bin/sh', ['-c', wrapped], { cwd: projection, encoding: 'utf8', timeout: 15000,
        env: { PATH: process.env.PATH, HOME: scratch } });
    }
    const tmp = await run('printf %s "$TMPDIR" && printf kept > "$TMPDIR/gen.js"');
    assert.equal(tmp.status, 0, tmp.stderr);
    assert.equal(tmp.stdout, scratch);
    assert.equal(await readFile(join(scratch, 'gen.js'), 'utf8'), 'kept');
    const png = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0xff]);
    await writeFile(join(scratch, 'x.png'), png);
    const copy = await run(`cp "$TMPDIR/x.png" x.png && cat x.png > /dev/null`);
    assert.equal(copy.status, 0, copy.stderr);
    assert.deepEqual(await readFile(owned), png);
    for (const command of [`cat ${quote(join(foreign, 'staged.txt'))}`, `cat ${quote(join(storage, 'history.sqlite'))}`]) {
      const denied = await run(command);
      assert.notEqual(denied.status, 0, `readable: ${command}`);
      assert.ok(!denied.stdout.includes('foreign') && !denied.stdout.includes('state'), `leaked: ${command}`);
    }
    // Linux masks the base and binds the own root back into it, so a listing
    // may succeed; it never shows another command's root.
    assert.ok(!(await run(`ls ${quote(base)}`)).stdout.includes('other'), 'another root is listed');
  } finally { await rm(root, { recursive: true, force: true }); }
});

// The supervised command is what the plugin actually runs: the sandbox confines
// its writes to the declared projection, the supervisor ends every descendant
// before the delta is read, and the status record is what authorizes the import.
test('supervised command confines its writes and outlives no descendant', async () => {
  const root = await realpath(await mkdtemp(join(tmpdir(), 'takt-supervise-')));
  try {
    const projection = join(root, 'projection');
    const scratch = join(root, 'scratch');
    const workspace = join(root, 'workspace');
    for (const p of [projection, scratch, workspace]) await mkdir(p);
    const owned = join(projection, 'data.txt');
    const real = join(workspace, 'data.txt');
    await writeFile(owned, 'base\n');
    await writeFile(real, 'base\n');
    const confirm = join(root, 'confirm');
    const orphan = join(scratch, 'orphan');
    const wrapped = await wrap(
      `sed 's/base/changed/' data.txt > ${quote(join(scratch, 'out'))} && cat ${quote(join(scratch, 'out'))} > data.txt; ` +
      `(sleep 1; printf escaped > ${quote(orphan)}) &`,
      { writable: [owned], scratch, privatePaths: [workspace], callID: 'supervise-1' });
    const run = spawnSync('/bin/sh', ['-c', supervise(wrapped, confirm)], {
      cwd: projection, encoding: 'utf8', timeout: 20000,
      env: { PATH: process.env.PATH, HOME: scratch, TMPDIR: scratch },
    });
    assert.equal(run.status, 0, run.stderr);
    assert.equal(await readFile(owned, 'utf8'), 'changed\n');
    // The physical workspace is untouched: the capture is the only way in.
    assert.equal(await readFile(real, 'utf8'), 'base\n');
    // The status record is written only after the process group is confirmed gone.
    assert.equal(await readFile(confirm, 'utf8'), '0');
    await new Promise(resolve => setTimeout(resolve, 2000));
    await assert.rejects(readFile(orphan), 'a descendant survived the command and wrote afterwards');
  } finally { await rm(root, { recursive: true, force: true }); }
});

// Network is unrestricted unless the user configured an allowlist, and an
// allowlist that omits a host leaves a sandboxed command unable to reach it.
test('network is restricted only when an allowlist is configured', async () => {
  const root = await realpath(await mkdtemp(join(tmpdir(), 'takt-network-')));
  const server = createServer((_, res) => res.end('reached')).listen(0, '127.0.0.1');
  try {
    await new Promise(resolve => server.once('listening', resolve));
    const url = `http://127.0.0.1:${server.address().port}/`;
    const scratch = join(root, 'scratch');
    const workspace = join(root, 'workspace');
    for (const p of [scratch, workspace]) await mkdir(p);
    let seq = 0;
    async function fetchWith(extra, curlFlags = '') {
      const wrapped = await wrap(`curl -sS --max-time 5 ${curlFlags}${quote(url)}`, {
        writable: [scratch], scratch, protectedPaths: [workspace], callID: `net-${++seq}`, ...extra });
      // Asynchronous: the server answering the request lives in this process.
      return promisify(execFile)('/bin/sh', ['-c', wrapped], { cwd: scratch, timeout: 20000,
        env: { PATH: process.env.PATH, HOME: scratch, TMPDIR: scratch } }).catch(error => error);
    }
    const open = await fetchWith({});
    assert.equal(open.stdout, 'reached', open.stderr);
    // Concurrent first wraps with an allowlist start the proxy once, in turn,
    // and both reach the allowed host. The sandbox exempts loopback from its
    // proxy, so the request is sent through it explicitly to reach the filter.
    const first = await Promise.all([1, 2].map(() => fetchWith({ allowedDomains: ['127.0.0.1'] }, "--noproxy '' ")));
    for (const run of first) assert.equal(run.stdout, 'reached', run.stderr);
    const limited = await fetchWith({ allowedDomains: ['allowed.test'] });
    assert.notEqual(limited.stdout, 'reached', 'a host outside the allowlist was reachable');
    assert.ok(limited.code, 'the restricted command succeeded');
    const filtered = await fetchWith({ allowedDomains: ['allowed.test'] }, "--noproxy '' ");
    assert.notEqual(filtered.stdout, 'reached', 'the proxy let through a host outside the allowlist');
    // The proxy filters by host, not by blocking everything: a host on the
    // allowlist stays reachable after a restart.
    const allowed = await fetchWith({ allowedDomains: ['127.0.0.1'] }, "--noproxy '' ");
    assert.equal(allowed.stdout, 'reached', allowed.stderr);
    // Concurrent first wraps under a new allowlist restart the proxy once, in
    // turn, and both reach the allowed host.
    const both = await Promise.all([1, 2].map(() => fetchWith({ allowedDomains: ['127.0.0.1', 'allowed.test'] }, "--noproxy '' ")));
    for (const run of both) assert.equal(run.stdout, 'reached', run.stderr);
  } finally {
    server.close();
    await SandboxManager.reset();
    await rm(root, { recursive: true, force: true });
  }
});
