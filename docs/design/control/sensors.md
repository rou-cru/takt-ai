# Sensors

Every sensor in the GC loop. Content sensors observe the terrain; attribution sensors separate the crew's own thrust from disturbances; capability sensors observe the controlled variable; health sensors observe the actuator and the sensors themselves.

## Base sensor

The only sensor the loop can always count on (`PR-OBS-EST-2`). It depends on workspace content alone.

**Manifest.** One record per workspace file (every file not ignored by the workspace, the VCS directory excluded): path, size, non-blank line count, modification time, content hash, verification state, and a mark for files with a crew-attributed change (set `T`, see [estimation.md](estimation.md#entropy)). The previous session's manifest is the starting reference of the next one.

| Part | Role | Acquisition | Measured cost | Error |
| :--- | :--- | :--- | :--- | :--- |
| Change scan (IMU) | Fast, frequent | Walk the workspace under the manifest's ignore rules and stat every file found; hash files absent from the manifest and files whose size or modification time changed; drop manifest records whose path is gone | 25 ms here; 2.4 s at 248 k files | Drift: a same-size edit within the clock resolution, or a tool that preserves modification time, goes unseen. Created and deleted files are always seen |
| Full verification (GPS) | Slow, exact | Hash the complete content of every file the last walk listed | 31 ms here; 24 s at 248 k files, 4.3 GB (one thread) | None in content; only age since last pass |

A new file enters the manifest when the walk first finds it; a deleted file leaves it, and its contribution leaves `E`, in the same sample. A file stat-scanned but not hashed since its last verification is **unverified**. Unverified files are the drift; the next verification pass bounds it. Each session runs one full verification pass.

**Pacing** (`PR-OBS-EST-6`). Verification hashes in chunks of at most `C_chunk` bytes at low operating-system priority, at a rate set by phase:

| Phase | Starts when | Rate |
| :--- | :--- | :--- |
| Idle | Session opens | `ρ_idle` |
| Engaged | First user prompt | `ρ_engaged` |
| Mutating | First workspace mutation of the session | `ρ_full`, until the pass completes |

Files on the execution route (see [estimation.md](estimation.md#exposure)) are verified first, so precision arrives first where the crew works.

**Preflight** (`PR-OBS-EST-5`). The change scan runs against the last manifest, and the difference is the change accumulated between sessions. Verification starts in the idle phase.

[Preflight timeline](diagrams/preflight.html)

**Base entropy.** While it hashes, the base sensor also reads each source file once and derives the measurements below from its text alone, with no parser or optional tool (`PR-OBS-EST-2`). They are the floor every estimate stands on; the tool metrics of the next section replace them where present.

| ID | Metric | Per-file value | Method | When |
| :--- | :--- | :--- | :--- | :--- |
| B0 | Size | `s_f`: non-blank lines | Line count | Each changed file |
| B1 | Complexity concentration | Excess mass `x⁰_f = Σ_b max(0, ι_b − τ_ι)·ℓ_b` and total mass `c⁰_f = Σ_b ι_b·ℓ_b`, over the indentation blocks `b` of the file, `ι_b` its mean logical indentation depth and `ℓ_b` its non-blank lines | Indentation moments: deeper nesting tracks branching, and indentation statistics correlate linearly and by rank with McCabe complexity across languages (Hindle, Godfrey and Holt, ICPC 2008) | Each changed file |
| B2 | Duplication | `d⁰_f`: lines covered by a window of `k` whitespace-normalized lines whose hash recurs elsewhere in the workspace | Line-window hashing (Ducasse, Rieger and Demeyer, ICSM 1999), fingerprints selected by winnowing (Schleimer, Wilkerson and Aiken, SIGMOD 2003); sees exact and layout-only clones | Each changed file, against the workspace fingerprint index |

Dead code, documentation gaps, and norm conformance have no content-only measurement reliable enough to estimate on: without their source, a file enters `B` for that metric (`PR-OBS-EST-3`). Where S1 or S2 is present it replaces B1 or B2 for that file. Each pass that has both values for a file records their residual; its running variance `ω_b²` is the uncertainty a file measured only by the base sensor adds to `U` (see [estimation.md](estimation.md#entropy)).

## Content metrics

Computed on content of source files, for files whose hash changed and, for workspace-wide results (B2, S2, S3 screen, S3 confirmation), for every file whose contribution the new result changes, whether or not its own hash changed: a new or removed clone partner, or a change in the incoming production edges of its symbols. Generated files and test trees are excluded. A language without an analyzer is a recorded coverage gap for that analyzer's metrics.

| ID | Metric | Per-file value | Source | Measured | When |
| :--- | :--- | :--- | :--- | :--- | :--- |
| S1 | Complexity concentration | Excess mass `x_f = Σ_g max(0, CC_g − τ)·√SLOC_g` and total mass `c_f = Σ_g CC_g·√SLOC_g`, over the functions `g` of the file | lizard: per-function cyclomatic complexity and lines, multi-language (`.tsx` needs its own language flag) | 0.1 s per file, deterministic | Each changed file |
| S2 | Duplication | `d_f`: duplicated lines | jscpd | 4–10 s per repository, deterministic, no incremental mode; sees token clones only | Session close |
| S3 | Dead code | `z_f`: dead lines (see fusion below) | Screen: code graph. Confirmation: deadcode (Go), knip (TypeScript), vulture (Python) | Screen continuous; confirmation 3–6 s | Screen on each change; confirmation at session close |
| S4 | Documentation gap | `o_f ∈ {0, 1}`, only for files in `T`: 1 when an exported symbol of the file has no docstring | Code graph | Index lookup | Each changed file |
| S5 | Norm conformance | `n_f = Σ_findings (1 − φ_ρ)`: findings of the project's declared analyzers, each weighted by `1 − φ_ρ` for its rule `ρ` (effective false positives, see health sensors), plus newly introduced suppressions (`PR-MNT-29`) | Declared analyzers | Per package or file, under 1 s for this repository | Each changed file, or session close when the analyzer has no file mode |

**Why excess mass for S1.** Over the 80 commits of trunk, the share of mass held by functions above `τ = 10` grew from 0.093 to 0.280, while mean complexity moved from 3.66 to 3.94: the mean dilutes degradation and is not used. The binary share jumps by a function's whole mass when it crosses `τ`, with up to 32 % of the final value in one commit. Excess mass grows faster (from 0.025 to 0.101) and its largest single jump is 26 % of the final value.

**Dead-code fusion (S3).** The screen marks functions with zero incoming production edges in the code graph (edges from test code excluded, `PR-MNT-25`). Measured against deadcode on this repository: recall 18 of 19, precision 18 of 103. Confirmation runs less often and is exact for what it covers. The fused value is

`z_f = Σ_{confirmed} SLOC + π · Σ_{screened, unconfirmed} SLOC`

where `π` is the screen's precision at the last confirmation pass. A symbol without a consumer introduced by a unit whose objective the execution route has not yet settled is pending, not dead, and is excluded whatever its age (`PR-MNT-26`); once that objective is accepted and the symbol still has no consumer, it enters the screen, and its file is re-screened at that acceptance whether or not the file changed.

**Rebaseline (S5).** A change in an analyzer's configuration hash or version starts a new baseline for that metric; it is not a change in entropy.

Comment verbosity and narrative comments are judged by the collector, not measured by a sensor (`PR-MNT-28`).

## Attribution sensors

They decide which content changes are the crew's thrust and which are disturbances (`PR-OBS-EST-4`).

| Sensor | Gives | Present when |
| :--- | :--- | :--- |
| Action Journal | Path, before and after hash, agent, work unit, revision for every VFS operation | A specialist works through the VFS |
| Tool hooks | Agent, work unit, path, and resulting content hash for native edit, write, and patch tools, read after the tool completes | OpenCode runs the Takt plugin |

**Rule.** A file change observed by the base sensor is crew-attributed when an attribution sensor recorded an operation on that path whose resulting hash equals the observed one. Every other change is a disturbance. Changes made by shell commands are disturbances by this rule: they raise entropy, but not the crew's measured production rate.

A change in a file's contribution without a change in its own hash comes from a workspace-wide result (see [content metrics](#content-metrics)) and inherits the attribution of the changed files that caused it: its clone partners for B2 and S2, the files whose references added or removed its incoming edges for S3. It is crew-attributed when at least one of those causes is crew-attributed, and a disturbance otherwise. A symbol leaving pending status at objective acceptance is crew-attributed, since a crew unit introduced it. Such a change never adds the file to `T`, which marks the crew's own edits.

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
| Stream liveness | Loss of a capture stream (`PR-OBS-BUS-5`) | Marks the affected sensor absent, which raises `U` (see [estimation.md](estimation.md#entropy)) |

## Sampling

| Point | Runs |
| :--- | :--- |
| Preflight | Change scan; verification starts |
| Each consolidation, unit finish, activity finish | Change scan; B0, B1, B2, S1, S3 screen, S4, S5 on changed files; B2 and S3 screen on files whose clone partners or incoming edges those changes moved |
| Objective accepted | S3 screen on the files of symbols leaving pending status |
| Session close | Change scan; S2; S3 confirmation; analyzers without file mode; decision (see [gc-controller.md](gc-controller.md)) |
| After a cycle consolidates | Change scan and verification of the cycle's region; cycle effect |
