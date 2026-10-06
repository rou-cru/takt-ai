# Takt declarative catalog

This is the single editable source for agent and skill content. Go loaders live
outside this directory. `agent.yaml` declares identity, instances, role, native
settings, context files, and on-demand skill links; it must not contain prompt prose.

OpenCode installs this tree once at `~/.config/opencode/takt/`. Its native JSON
registers each instance with direct file references in this order:
BASELINE → PERSONA → SOUL → OPERATIONS, followed by a memory clause naming the
agent's memory skills. Only undeclared optional files are omitted.
This order does not introduce an authority hierarchy. Both judge instances share
one package. Skills are also deployed to `~/.opencode/skills/`, where OpenCode
discovers them; they are not injected into initial prompts. Resource bytes are
copied unchanged, except that a double-brace `policy.<name>` marker in Markdown prose is
replaced at install with the matching value of `takt/dispatch/admission_policy.yaml`;
an unknown marker fails the install.

Skill directories match their frontmatter `name`. References between skills are
relative to the containing skill directory. Declare executable scripts as a list
under frontmatter `metadata.executables` (for example `scripts/check.sh`); these
install as 0755. All other files install as 0644. Paths must stay inside packages.

The native registry is published after package dependencies. Editing an installed
Markdown file takes effect on a new OpenCode load, not necessarily an active
session. Missing declared context is an installation/diagnostic error. No second
compiled prompt is generated; the only global addition is the short "Takt Memory"
section Engram's wiring places in `~/.config/opencode/AGENTS.md`.

## VFS by role

| Role | VFS tools | How it changes the workspace |
| --- | --- | --- |
| orchestrator | claims, `vfs_discard`, `vfs_consolidate` | native edit outside claimed paths; assigns, discards and consolidates staged work |
| planning_author, direct_interlocutor | none | native edit outside claimed paths, delivering documents to the paths named |
| execution (dev, fix, simplify) | bind, write, read, delete | staged, never native; shell only under a bound identity; simplify also collects inside a GC cycle's authorized scope when attached |
| verification (verify, judge-a, judge-b) | bind, read, verify | none; reads staged work when assigned a gate, including GC deltas; shell and network run natively with native edit denied |

No role reads or writes the secret-bearing paths in `takt/model/sensitive_paths.go`;
native edits never land on a path an active VFS claim holds, and specialist shell
inspection cannot read those secrets either.

The permission rules live in `takt/agents/opencode/renderer.go`, the claim and
shell guards in the `takt-vfs` plugin; this table and the agents' `OPERATIONS.md`
and skills must agree with them.
