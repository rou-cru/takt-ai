# Prepared GC analysis

`Findings(ctx, workspace, plan, preparation)` and `AnalyzePrepared` execute the
same pipeline. CLI findings/refutation reads `LoadPreparation(workspace, state)`
and requires the preparation's session to match. Ordinary project preparation
must install/cache tools first. Analysis neither downloads dependencies nor
initializes a CodeGraph index. CodeGraph computes causal closure in `Declare`;
it does not decide whether Go functions are live.

## Go dead code

Configure exactly one Go `dead-code` analyzer, with `tool: "deadcode"` and
`command: ["go", "tool", "deadcode", "-json", "./..."]`. The workspace's module
must pin the deadcode tool and its dependencies. A reviewed project-local
executable with arguments `-json ./...` is also supported. `version_command`
must report the pinned version.

RTA starts from production main/init roots, traverses callbacks/interface calls,
and reports unreachable chains and cycles, even if tests call them. No main
package, missing dependencies, tool failures and malformed output are errors.
Only findings in the declared closure are retained. Deleted files and test files
are excluded. Unknown file types produce gaps; supported languages without a
prepared analyzer fail explicitly. Coverage describes build-configuration and
dynamic-use limitations. Test-reference enrichment is not collected.

Findings are **candidates, not mutation authority**. Independent investigation
and `AuthorizedFinding` still apply. Go IDs retain `dead-code:path#Function` and
`dead-code:path#Type::Method`; line changes and pending-to-dead transitions do not
invalidate those refutations. The pre-session declaration snapshot determines
whether a symbol is new/pending; merely editing an old symbol's file does not
protect it. Findings that cannot be matched to a declaration are proposal-only.

## Execution budgets (version 1)

Optional top-level `.takt/gc.json` fields are positive Go duration strings:

| Field | Default | Applies to |
|---|---|---|
| `check_timeout` | `15m` | Each acceptance check |
| `analyzer_timeout` | `10m` | Each analyzer invocation |
| `version_timeout` | `30s` | Each tool version probe |
| `ast_timeout` | `30s` | Each external AST parser invocation |

An analyzer's optional `timeout` overrides `analyzer_timeout`. Empty/omitted
fields use these defaults; zero, negative
or malformed durations fail validation. A shorter caller context always wins.
`RunPrepared` uses the analyzer default; `RunChecks`
uses the configured check budget. There is no additional two-minute ceiling.

Execution disables Go proxy/checksum/toolchain downloads and sets npm, uv and
pip offline flags. These are package-manager safeguards, not a network sandbox
for arbitrary reviewed commands. A timeout/cancellation yields `completed:
false`, exit `-1`, and error text; deadlines additionally set `timed_out: true`.
Partial JSON stdout can never turn a timeout into successful empty analysis.
Unix cancellation kills the command's process group; Windows uses Go's direct
process cancellation and bounded output-drain wait.
