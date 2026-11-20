# Estimation

How sensor readings become the workspace estimate (`PR-OBS-EST-8`, `PR-OBS-EST-9`). Per-file metric symbols are defined in [sensors.md](sensors.md#content-metrics).

## State

| Symbol | Meaning | Kind |
| :--- | :--- | :--- |
| `E` | Exposure-weighted workspace entropy | Measured from content |
| `u` | Share of weighted mass in blind files `B` | Measured from the manifest |
| `U` | Standard deviation of the projection `Ê` | Derived from `u` and the filter variance |
| `r` | Crew entropy production per action | Estimated: [Kalman filter](#production-rate) |
| `λ_j` | Capability count rate of sensor `j`, relative to its exposure | Estimated: [Gamma-Poisson filter](#capability) |
| `C_j` | Cumulative evidence that `λ_j` has risen | Estimated: [CUSUM](#capability) |

Content metrics have no measurement noise once a file is verified: the same content always yields the same value. Their uncertainty is coverage, captured by `u`. The stochastic quantities are the crew's production rate and its capability, which are seen only through noisy increments and sparse counts.

## Exposure

Entropy matters where the crew works (`PR-OBS-EST-7`). Each file gets a weight

`w_f = 1 + a_F·[f ∈ F] + a_D·[f ∈ D] + a_N·[f ∈ N] + a_H·h_f`

| Set or term | Content |
| :--- | :--- |
| `F` | Write scopes of in-flight units (claims, `PR-ORQ-18`) |
| `D` | Files changed by units the execution route records as settled |
| `N` | One-hop code-graph neighborhood of `F ∪ D`. Planned units record no file scope; their work is expected to land near the execution route |
| `h_f` | Change frequency: attributed changes of `f` per action, smoothed as `h_f ← (1 − ε)^{Δw}·h_f + [f changed]` |

Without an execution route, only the `1` and `a_H·h_f` terms remain. Without a code graph, `N` is empty.

## Entropy

Each metric is an exposure-weighted ratio, and `E` is their weighted sum:

| Metric | Ratio `E_i` |
| :--- | :--- |
| Complexity concentration | `E_1 = Σ w_f x_f / Σ w_f c_f` |
| Duplication | `E_2 = Σ w_f d_f / Σ w_f s_f` |
| Dead code | `E_3 = Σ w_f z_f / Σ w_f s_f` |
| Documentation gap | `E_4 = Σ_{f ∈ T} w_f o_f / Σ_{f ∈ T} w_f`, and 0 when `T` is empty |
| Norm conformance | `E_5 = Σ w_f n_f / Σ w_f s_f` |

`T` is the set of files with at least one crew-attributed change, marked in the manifest at the first one (scout rule: a file the crew touched is left documented). A disturbance never adds a file to `T`.

`E = Σ_i κ_i E_i`, with `κ_i` putting the metrics on one scale. A metric without a source for a file contributes nothing for it and puts the file in `B`.

**Uncertainty.** `B` holds unverified files and files for which a metric has no source. Their weighted share is `u = Σ_{f ∈ B} w_f s_f / Σ_f w_f s_f`. The projection `Ê` (see [gc-controller.md](gc-controller.md#projection)) carries two independent variances, one from blind files and one from the production rate:

`U² = ω²·u + W²·P`

`ω²` is the prior variance that a blind file adds to `E`, in the scale of `E`. `U_R` is the same expression with `u` restricted to a region `R`. A lost sensor raises `U` through the term it feeds: files left without a source enter `B`, and unattributed work stops updating `r`, so `P` grows (see [production rate](#production-rate)).

## Attribution

Every sample splits the change in `E` between the crew and disturbances (`PR-OBS-EST-4`). Each `E_i = M_i / Q_i` is a ratio. To first order, a set of changed files moves it by

`ΔE_i = (ΔM_i − E_i·ΔQ_i) / Q_i`

which is additive over files. The crew increment `ΔE^u` sums the files the [attribution rule](sensors.md#attribution-sensors) assigns to the crew; the disturbance `ΔE^d` sums the rest. Weights are held at their `k−1` values for the split, so a change of route moves neither term. `E` itself is always recomputed exactly.

## Production rate

The crew's entropy production per action, `r`, drifts as entropy accumulates (the self-acceleration). It is tracked by a scalar Kalman filter on the work axis with a random-walk model. Only crew increments feed it: disturbances move `E`, never `r`.

| Step | Equation |
| :--- | :--- |
| Predict | `r⁻ = r_{k−1}`, `P⁻ = P_{k−1} + q·Δw_k` |
| Measurement | `y_k = ΔE^u_k / Δw_k`, with variance `R_k = σ_k² / Δw_k` |
| Innovation | `ν = y_k − r⁻`, `V = P⁻ + R_k` |
| Gate | If `ν² > g²·V`: the reading is recorded as a sensor fault and excluded; `r_k = r⁻`, `P_k = P⁻` |
| Update | `K = P⁻ / V`, `r_k = r⁻ + K·ν`, `P_k = (1 − K)·P⁻` |
| Noise learning | `σ_{k+1}² = max(σ_min², (1 − ζ)·σ_k² + ζ·(ν² − P⁻)·Δw_k)`, starting at `σ_0²` |

The gate applies to the rate filter: content metrics are exact for verified files, and a capability count enters its filter as a count, where a large one raises the CUSUM instead of being excluded. A sample with `Δw_k = 0` changes nothing. The variance `P` grows only with recorded work that went unmeasured, so a lost attribution sensor shows up as a rising `P` (`PR-OBS-EST-3`).

## Capability

Each capability sensor `j` reports a count `n_k` over an exposure `m_k`:

| Sensor | `n_k` | `m_k` |
| :--- | :--- | :--- |
| Problem signals | Problem signals in the sample | Work units, consolidations, and attempts in the sample |
| Effort | Actions of the units finished in the sample | Sum of the baseline actions of their task types |

A Gamma-Poisson filter with forgetting per action estimates the rate. For effort, `λ = 1` means at baseline.

| Step | Equation |
| :--- | :--- |
| Update | `α_k = γ^{Δw_k}·α_{k−1} + n_k`, `β_k = γ^{Δw_k}·β_{k−1} + m_k` |
| Estimate | `λ_k = α_k / β_k`, with variance `α_k / β_k²` |
| Baseline | Same update with `γ_0 > γ`: the slow level `λ_0` against which stability is judged (`PR-MNT-10`) |

A rise is detected with a one-sided Poisson CUSUM against the rise ratio `μ`:

`C_k = max(0, C_{k−1} + n_k − m_k·ψ_j)`, where `ψ_j = λ_0·(μ − 1) / ln μ`

The capability alarm `A_j` holds while `C_k ≥ H`. A recorded change of model or harness resets `α`, `β`, the baseline, and `C` (`PR-MNT-11`).

## Initialization

| Situation | State |
| :--- | :--- |
| Workspace without an estimate | Every file unverified (`u = 1`); `r_0 = 0` with `P_0` large; capability priors `α_0`, `β_0`. Metrics fill in as the first verification pass reaches each file |
| Preflight | The last estimate loads. The change scan's difference against the last manifest is a disturbance `ΔE^d`, applied to `E` before the crew acts. Unverified files keep `u` above zero until verification reaches them |
| Storage | One owner store holds the estimate (`PR-OBS-EST-9`): the estimator writes it, preflight and the control law read it |

## Update order

Sensors arrive asynchronously. At each sample: predict once with `Δw_k`, then update with each reading present, in arrival order. A missing reading is skipped; its variance keeps growing. Every reading, gate decision, and resulting state is recorded, so replaying the records against the same policy reproduces the estimate (`PR-OBS-EST-8`).
