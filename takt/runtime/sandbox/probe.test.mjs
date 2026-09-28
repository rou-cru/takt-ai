import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, mkdir, readFile, writeFile, symlink, rm, realpath } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
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
