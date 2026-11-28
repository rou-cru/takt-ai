#!/usr/bin/env node
// Takt's launcher for the Archify CLI it installs next to this skill. It runs
// the upstream CLI with update checks off, since Takt pins the version, and
// owns every output location of the read-only analysis: check, task and drift
// write into a fresh private directory under the temporary directory, never
// where an agent points them, and baseline changes are refused so they stay
// the user's decision. Commands check prints for follow-up are rewritten to
// this launcher, so an agent runs them exactly as printed.
import { spawnSync } from 'node:child_process';
import { existsSync, mkdtempSync, realpathSync } from 'node:fs';
import { homedir, tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const self = fileURLToPath(import.meta.url);
const upstream = path.resolve(path.dirname(self), '../../archify/bin/archify.mjs');
const env = { ...process.env, ARCHIFY_UPDATE_CHECK_DISABLED: '1' };
const runPrefix = 'takt-archify-';
const analyses = new Set(['check', 'task', 'drift']);
const baselineFlags = ['--update-baseline', '--accept', '--revoke'];
// task options that take no value; every other task option takes one.
const taskFlags = new Set(['--require-valid', '--json']);

const quote = (value) => (/^[A-Za-z0-9_./:=@+-]+$/.test(value) ? value : `'${value.replaceAll("'", `'\\''`)}'`);
const relative = path.relative(homedir(), self);
const launcher = relative.startsWith('..') || path.isAbsolute(relative) ? `node ${quote(self)}` : `node ~/${relative}`;
const named = (args, flag) => args.some((arg) => arg === flag || arg.startsWith(`${flag}=`));
const valueOf = (args, flag) => {
  const index = args.indexOf(flag);
  return index >= 0 ? args[index + 1] : undefined;
};

function refuse(message) {
  process.stderr.write(`Takt: ${message}\n`);
  process.exit(2);
}

function run(args, { capture = false } = {}) {
  const result = spawnSync(process.execPath, [upstream, ...args], {
    env,
    encoding: 'utf8',
    maxBuffer: 1 << 28,
    stdio: capture ? ['inherit', 'pipe', 'inherit'] : 'inherit',
  });
  if (result.error) refuse(`cannot run Archify at ${upstream}: ${result.error.message}`);
  return result;
}

const freshRun = () => mkdtempSync(path.join(tmpdir(), runPrefix));

// check writes the analysis a later task reads, so it always has a directory,
// answers in JSON, and reports findings without failing the command.
function checkArgs(args, out) {
  return [...args, '--out', out,
    ...(named(args, '--json') ? [] : ['--json']),
    ...(named(args, '--fail-on') ? [] : ['--fail-on', 'never'])];
}

// The follow-up commands check prints name the upstream CLI by its absolute
// path; they are rewritten to this launcher.
function rewrite(value, prefixes) {
  if (typeof value === 'string') {
    const prefix = prefixes.find((p) => value.startsWith(p));
    return prefix ? `${launcher} ${value.slice(prefix.length)}` : value;
  }
  if (Array.isArray(value)) return value.map((item) => rewrite(item, prefixes));
  if (value && typeof value === 'object') {
    return Object.fromEntries(Object.entries(value).map(([key, item]) => [key, rewrite(item, prefixes)]));
  }
  return value;
}

function check(args) {
  const result = run(['code-analysis', 'check', ...checkArgs(args, freshRun())], { capture: true });
  let report;
  try {
    report = JSON.parse(result.stdout);
  } catch {
    process.stdout.write(result.stdout);
    return result.status;
  }
  const clis = new Set([upstream]);
  try {
    clis.add(realpathSync(upstream));
  } catch {
    // The upstream path was already reported by the run itself.
  }
  const prefixes = [...clis].map((cli) => `node ${quote(cli)} `);
  process.stdout.write(`${JSON.stringify(rewrite(report, prefixes), null, 2)}\n`);
  return result.status;
}

// A sandboxed shell's temporary directory lasts one call, so the analysis a
// printed task command names can be gone: it is analyzed again.
function task(args) {
  const resolved = [...args];
  for (let i = 0; i < resolved.length; i++) {
    const arg = resolved[i];
    if (arg.startsWith('--')) {
      if (!taskFlags.has(arg)) i++;
      continue;
    }
    if (path.basename(arg).startsWith(runPrefix) && !existsSync(arg)) {
      const root = valueOf(resolved, '--root') ?? '.';
      const out = freshRun();
      process.stderr.write(`Takt: the analysis at ${arg} is gone; analyzing ${root} again.\n`);
      const analysis = run(['code-analysis', 'check', ...checkArgs([root], out)], { capture: true });
      if (analysis.status !== 0) {
        process.stdout.write(analysis.stdout);
        return analysis.status;
      }
      resolved[i] = out;
    }
    break;
  }
  return run(['code-analysis', 'task', ...resolved]).status;
}

function main(args) {
  const [command, sub, ...rest] = args;
  if (command !== 'code-analysis' || !analyses.has(sub)) return run(args).status;
  if (named(rest, '--out')) refuse(`the launcher chooses where ${sub} writes; run it again without --out.`);
  const change = baselineFlags.find((flag) => named(rest, flag));
  if (change) {
    refuse(`${change} changes the project's analysis baseline. Ask the user first; with their approval, run: `
      + ['node', upstream, 'code-analysis', sub, ...rest].map(quote).join(' '));
  }
  switch (sub) {
    case 'check':
      return check(rest);
    case 'task':
      return task(rest);
    default:
      return run(['code-analysis', 'drift', ...rest, '--out', path.join(freshRun(), 'drift')]).status;
  }
}

process.exitCode = main(process.argv.slice(2)) ?? 1;
