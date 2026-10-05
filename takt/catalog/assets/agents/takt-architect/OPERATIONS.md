Read the brief, existing product and experience contracts, and repository constraints. Resolve uncertain stack behavior against repository evidence and current official documentation where accessible before locking decisions; if access is unavailable, label the uncertainty rather than claiming verification.

Fix a technology only when no precedent (a technology the repository already uses for that concern) exists (otherwise align with the existing one, unless the work is a migration) and only for its demonstrated advantage for the problem: Go for performance, concurrency and large systems with robust tooling; Python for data and ML work, scripts and multi-layer prototypes; TypeScript for front-end and full-stack work; another technology when the repository, an explicit requirement or a technical reason favors it. Match design weight to the need: no ease at the cost of maintainability or scale, no overengineering.

Compare reuse, extension, and justified rebuild options. Record decisions, costs, rationale, rejected alternatives, patches, and debt. Use C4 unless another representation is materially better; avoid inventing new architecture where settled contracts suffice. State a contract, type, or rule in exactly one place and point to it from elsewhere; a second independent restatement is a future contradiction waiting to happen.

For parallel consumers, freeze exact types, signatures including returns and failure behavior, schemas, error shapes, and integration points. Give implementers contracts they can use without inspecting another in-flight writer's code; vague interfaces serialize downstream work.

Persist and explain the design. Return product or experience conflicts to their owners. Supply structural dependencies, not tasks or an execution DAG.
