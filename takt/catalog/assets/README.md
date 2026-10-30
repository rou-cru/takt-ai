# Takt declarative catalog

This is the single editable source for agent and skill content. Go loaders live
outside this directory. `agent.yaml` declares identity, instances, role, native
settings, context files, and on-demand skill links; it must not contain prompt prose.

OpenCode installs this tree once at `~/.config/opencode/takt/`. Its native JSON
registers each instance with direct file references in this order:
BASELINE → PERSONA → SOUL → OPERATIONS, followed by a memory clause naming the
memory contract and, for recording, the agent's memory skill. Only undeclared optional files are omitted.
This order does not introduce an authority hierarchy. PERSONA and SOUL carry identity and voice only: every working procedure, contract, and judgment criterion a role needs lives in BASELINE, OPERATIONS, or skills, so a substitute composed without them can hold an unavailable seat (`PR-CRW-13`). Both judge instances share
one package. Skills are also deployed to `~/.opencode/skills/`, where OpenCode
discovers them; they are not injected into initial prompts. `shared/GUIDELINES.md` is registered in `instructions`, so every session loads it. Resource bytes are
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

| Role | How it changes the workspace |
| --- | --- |
| orchestrator | native edit outside claimed paths; assigns, discards and consolidates staged work; reads the journal and execution history |
| planning_author, direct_interlocutor | native edit outside claimed paths, delivering documents to the paths named |
| execution | staged, never native; shell only under a bound identity; simplify also collects inside a GC cycle's authorized scope when attached |
| quality_assurance | none; native edit denied |
| verification | none; reads staged work when assigned a gate, including GC deltas; shell and network run natively with native edit denied |

Each instance's VFS grant is the `vfs_capabilities` list in its `agent.yaml`
(capability names from `takt/model/vfs_capability.go`); the role does not grant VFS access.

No role reads or writes the secret-bearing paths in `takt/model/sensitive_paths.go`;
native edits never land on a path an active VFS claim holds, and specialist shell
inspection cannot read those secrets either.

The permission rules live in `takt/agents/opencode/renderer.go`, the claim and
shell guards in the `takt-vfs` plugin; this table and the agents' `OPERATIONS.md`
and skills must agree with them.
