# Sensors

Every sensor in the GC loop. Content sensors observe the terrain; attribution sensors separate the crew's own thrust from disturbances; capability sensors observe the controlled variable; health sensors observe the actuator and the sensors themselves.

## Base sensor

The only sensor the loop can always count on (`PR-OBS-EST-2`). It depends on workspace content alone.

**Manifest.** One record per source file: path, size, modification time, content hash, verification state, and a mark for files with a crew-attributed change (set `T`, see [estimation.md](estimation.md#entropy)). The previous session's manifest is the starting reference of the next one.

| Part | Role | Acquisition | Measured cost | Error |
| :--- | :--- | :--- | :--- | :--- |
| Change scan (IMU) | Fast, frequent | Stat every source file; hash only files whose size or modification time changed | 25 ms here; 2.4 s at 248 k files | Drift: a same-size edit within the clock resolution, or a tool that preserves modification time, goes unseen |
| Full verification (GPS) | Slow, exact | Hash every source file | 31 ms here; 24 s at 248 k files, 4.3 GB (one thread) | None in content; only age since last pass |

A file stat-scanned but not hashed since its last verification is **unverified**. Unverified files are the drift; the next verification pass bounds it. Each session runs one full verification pass.

**Pacing** (`PR-OBS-EST-6`). Verification hashes in chunks of at most `C_chunk` bytes at low operating-system priority, at a rate set by phase:

| Phase | Starts when | Rate |
| :--- | :--- | :--- |
| Idle | Session opens | `ρ_idle` |
| Engaged | First user prompt | `ρ_engaged` |
| Mutating | First workspace mutation of the session | `ρ_full`, until the pass completes |

Files on the execution route (see [estimation.md](estimation.md#exposure)) are verified first, so precision arrives first where the crew works.

**Preflight** (`PR-OBS-EST-5`). The change scan runs against the last manifest, and the difference is the change accumulated between sessions. Verification starts in the idle phase.

[Preflight timeline](diagrams/preflight.html)

## Content metrics

Computed on content, only for files whose hash changed. Generated files and test trees are excluded. A language without an analyzer is a recorded coverage gap and counts as unverified.

| ID | Metric | Per-file value | Source | Measured | When |
| :--- | :--- | :--- | :--- | :--- | :--- |
| S0 | Size | `s_f`: source lines | Complexity analyzer | Exact | Each changed file |
| S1 | Complexity concentration | Excess mass `x_f = Σ_g max(0, CC_g − τ)·√SLOC_g` and total mass `c_f = Σ_g CC_g·√SLOC_g`, over the functions `g` of the file | lizard: per-function cyclomatic complexity and lines, multi-language (`.tsx` needs its own language flag) | 0.1 s per file, deterministic | Each changed file |
| S2 | Duplication | `d_f`: duplicated lines | jscpd | 4–10 s per repository, deterministic, no incremental mode; sees token clones only | Session close |
| S3 | Dead code | `z_f`: dead lines (see fusion below) | Screen: code graph. Confirmation: deadcode (Go), knip (TypeScript), vulture (Python) | Screen continuous; confirmation 3–6 s | Screen on each change; confirmation at session close |
| S4 | Documentation gap | `o_f ∈ {0, 1}`, only for files in `T`: 1 when an exported symbol of the file has no docstring | Code graph | Index lookup | Each changed file |
| S5 | Norm conformance | `n_f = Σ_findings (1 − φ_ρ)`: findings of the project's declared analyzers, each weighted by `1 − φ_ρ` for its rule `ρ` (effective false positives, see health sensors), plus newly introduced suppressions (`PR-MNT-29`) | Declared analyzers | Per package or file, under 1 s for this repository | Each changed file, or session close when the analyzer has no file mode |

**Why excess mass for S1.** Over the 80 commits of trunk, the share of mass held by functions above `τ = 10` grew from 0.093 to 0.280, while mean complexity moved from 3.66 to 3.94: the mean dilutes degradation and is not used. The binary share jumps by a function's whole mass when it crosses `τ`, with up to 32 % of the final value in one commit. Excess mass grows faster (from 0.025 to 0.101) and its largest single jump is 26 % of the final value.

**Dead-code fusion (S3).** The screen marks functions with zero incoming production edges in the code graph (edges from test code excluded, `PR-MNT-25`). Measured against deadcode on this repository: recall 18 of 19, precision 18 of 103. Confirmation runs less often and is exact for what it covers. The fused value is

`z_f = Σ_{confirmed} SLOC + π · Σ_{screened, unconfirmed} SLOC`

where `π` is the screen's precision at the last confirmation pass. Symbols younger than `a_pending` crew work are pending, not dead, and are excluded (`PR-MNT-26`).

**Rebaseline (S5).** A change in an analyzer's configuration hash or version starts a new baseline for that metric; it is not a change in entropy.

Comment verbosity and narrative comments are judged by the collector, not measured by a sensor (`PR-MNT-28`).

## Attribution sensors

They decide which content changes are the crew's thrust and which are disturbances (`PR-OBS-EST-4`).

| Sensor | Gives | Present when |
| :--- | :--- | :--- |
| Action Journal | Path, before and after hash, agent, work unit, revision for every VFS operation | A specialist works through the VFS |
| Tool hooks | Agent, work unit, and path hash for native edit, write, and patch tools | OpenCode runs the Takt plugin |

**Rule.** A file change observed by the base sensor is crew-attributed when an attribution sensor recorded an operation on that path whose resulting hash equals the observed one. Every other change is a disturbance. Changes made by shell commands are disturbances by this rule: they raise entropy, but not the crew's measured production rate.

## Capability sensors

They observe the controlled variable (`PR-OBS-EST-1`). Both are counts over work.

| Sensor | Count | Denominator |
| :--- | :--- | :--- |
| Problem signals | Previously passing checks that start failing, re-edits of files consolidated by crew work, discarded or redone units, repeated collisions (`PR-MNT-9`), discarded cycles (`PR-MNT-16`) | Work units dispatched, consolidations, attempts |
| Effort | Recorded actions | Units finished, per task type (`PR-OBS-PRG-4`) |

Counts are sparse and heavy-tailed: most sessions record none, and a few record many. They are filtered as counts, never as Gaussian values (see [estimation.md](estimation.md#capability)).

## Health sensors

| Sensor | Measures | Used by |
| :--- | :--- | :--- |
| Cycle reversions | Re-edits of what a cycle consolidated, by mandate class (`PR-MNT-31`); counted here, not as crew problem signals | Automatic demotion (`PR-MNT-32`) |
| Effective false positives | Per analyzer rule: fraction `φ_ρ` of findings refuted (`PR-MNT-24`) or reverted | Weight of the rule in `n_f` |
| Cycle effect | Region entropy before and after each cycle | Lockout release (see [gc-controller.md](gc-controller.md#law)) |
| Coverage | Languages and files without analyzer coverage | Uncertainty `U` (blind files `B`) |
| Stream liveness | Loss of a capture stream (`PR-OBS-BUS-5`) | Marks the affected sensor absent |

## Sampling

| Point | Runs |
| :--- | :--- |
| Preflight | Change scan; verification starts |
| Each consolidation, unit finish, activity finish | Change scan; S0, S1, S3 screen, S4, S5 on changed files |
| Session close | Change scan; S2; S3 confirmation; analyzers without file mode; decision (see [gc-controller.md](gc-controller.md)) |
| After a cycle consolidates | Change scan and verification of the cycle's region; cycle effect |
