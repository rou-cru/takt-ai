// The Go coordinator supplies private projection paths, never model arguments.
import { SandboxManager, getDefaultWritePaths } from '@anthropic-ai/sandbox-runtime';
import { isAbsolute } from 'node:path';

// SHELL_ESCAPED_DESCENDANTS is the status a supervised command exits with when a
// descendant outlived it: nothing confirms the projection is settled, so the
// delta must stay unimported.
export const SHELL_ESCAPED_DESCENDANTS = 121;

// DESCENDANT_SWEEPS bounds the wait for the process group to disappear, at one
// tenth of a second each.
const DESCENDANT_SWEEPS = 30;

export async function wrap(command, { writable, scratch, protectedPaths = [], privatePaths = [], callID }) {
  if (!['darwin', 'linux'].includes(process.platform) || process.arch !== 'arm64') {
    throw new Error('Takt sandbox: unsupported target; no direct-shell fallback');
  }
  const deps = await SandboxManager.checkDependenciesAsync();
  if (deps.errors.length) throw new Error(`Takt sandbox dependencies: ${deps.errors.join(', ')}`);
  if (!callID || !scratch || !writable?.length || !(protectedPaths.length || privatePaths.length) ||
      [...writable, scratch, ...protectedPaths, ...privatePaths].some(p => !isAbsolute(p) || /[*?\[\]]/.test(p))) {
    throw new Error('Takt sandbox: explicit absolute literal paths and call ID required');
  }
  // SRT grants shared temp/log directories by default. They are NOT private
  // scratch and must be carved out explicitly. Device stdio is retained.
  const denyDefaults = getDefaultWritePaths().filter(p => !p.startsWith('/dev/'));
  return SandboxManager.wrapWithSandbox(command, '/bin/sh', {
    filesystem: {
      allowWrite: [...writable, scratch],
      denyWrite: [...protectedPaths, ...privatePaths, ...denyDefaults],
      denyRead: privatePaths,
    },
    // Deliberately absent: network. [] would block network, contrary to product.
  }, undefined, { commandId: callID });
}

// supervise runs the wrapped command in its own process group, so a cancellation
// reaches every descendant, and records the status at confirmPath only once the
// group is gone. Without that record the coordinator refuses to import the
// delta: a surviving process could still be writing into the projection.
export function supervise(wrapped, confirmPath) {
  const confirm = `'${confirmPath.replaceAll("'", "'\\''")}'`;
  // Monitor mode is switched off again once the job exists: it is only needed to
  // give the command its own process group, and leaving it on would print job
  // notices into the command's own stderr.
  return `set -m; { ${wrapped}; } & __takt_group=$!; set +m; ` +
    `trap 'kill -TERM -$__takt_group 2>/dev/null' INT TERM HUP; ` +
    `wait $__takt_group; __takt_status=$?; ` +
    `kill -TERM -$__takt_group 2>/dev/null; __takt_sweep=0; ` +
    `while kill -0 -$__takt_group 2>/dev/null; do ` +
    `__takt_sweep=$((__takt_sweep+1)); ` +
    `[ $__takt_sweep -gt ${DESCENDANT_SWEEPS} ] && exit ${SHELL_ESCAPED_DESCENDANTS}; ` +
    `kill -KILL -$__takt_group 2>/dev/null; sleep 0.1; done; ` +
    `printf %s "$__takt_status" > ${confirm}; exit $__takt_status`;
}
