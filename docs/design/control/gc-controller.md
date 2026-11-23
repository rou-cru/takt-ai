# GC controller

The law that turns the workspace estimate into GC cycles (`PR-MNT-33` to `PR-MNT-37`). State symbols are defined in [estimation.md](estimation.md#state).

## Why not a PID

The actuator is discrete: a cycle is a bounded intervention with dead time, cost, and its own risk, not a continuous force. The plant integrates by itself, since entropy accumulates. The law is therefore event-triggered hysteresis control on a projected state:

| PID term | Here |
| :--- | :--- |
| P: level | `E` |
| I: accumulation | Not needed: `E` is already accumulated entropy |
| D: trend | `r`, projected over the planned work |

## Projection

Feedforward from the execution route: the entropy expected once the planned work is done.

`Ê = E + r·W`

`W` is the planned work: the planned units on the execution route times the mean actions per unit from the execution history. Without a plan, `W` is the mean crew actions per session.

## Law

Evaluated at each session close. While another session of the workspace is active, the decision is held and evaluated again against the current estimate once none is (`PR-MNT-36`).

| Element | Rule |
| :--- | :--- |
| Launch | `Ê ≥ E_hi`, or any capability alarm `A_j` while `E ≥ E_lo` |
| Engaged | After a launch the law stays engaged and launches one bounded cycle per decision point until release |
| Release | `E ≤ E_lo`. The gap between `E_lo` and `E_hi` is the hysteresis that keeps the law from switching on noise |
| Deadband | The law re-evaluates only when `E` or `Ê` has moved at least `δ` since the last decision, or an alarm or the engaged state changed. `Ê` is recomputed at every sample, so a change of `r` or `W` alone re-evaluates the launch condition |
| Lockout | A region a cycle consolidated, with its closure, is not selected again until its post-cycle entropy has been measured: cycle effect `η = (E_R,before − E_R,after) / E_R,before` |
| Uncertainty | No autonomous launch while the estimate uncertainty `U` exceeds `U_max` (`PR-MNT-35`), nor while `U_R` of the selected region does |
| Record | The estimate and uncertainty read, policy version, region, mandate class, and mode (`PR-MNT-34`) |

A capability alarm only launches a cycle when entropy is already above `E_lo`: below it, entropy is not the likely cause, and the alarm is the business of the session control plane.

**Disturbance rejection.** Every decision reads the current estimate, never a queue of past triggers. A second or third impact during recovery raises `E` and keeps the law engaged; it does not stack cycles. Reversions of a cycle's work feed automatic demotion (`PR-MNT-32`).

## Scope selection

The minimum sufficient cycle (`PR-MNT-4`, `PR-MNT-35`):

1. **Region:** the non-locked file with the largest weighted contribution `w_f·Σ_i κ_i·(contribution of f to E_i)`.
2. **Mandate class:** the metric with the largest contribution in that region. Complexity, duplication, dead code, documentation, and norm conformance map to the complexity, duplication, dead-code, documentation, and analyzer-integrity mandates (`PR-MNT-17`, `PR-MNT-29`).
3. **Closure:** from the region along code-graph reachability outward, up to the policy limits on findings and files. Work beyond them waits for the next decision.

## Modes

<!-- Proposal to discuss: gate the autonomous mode on a workspace readiness level, as Factory's Agent Readiness does (binary criteria per pillar, five levels, 80 % of a level's criteria to unlock the next). Candidate criteria: declared analyzers and tests that preserve behavior (PR-MNT-14, PR-MNT-15, PR-MNT-17). -->

The estimate and the law are identical in every mode (`PR-MNT-37`).

| Mode | On a launch decision |
| :--- | :--- |
| Manual | Recorded. The user launches cycles at will; scope selection applies the same way |
| Assisted | Presented to the user with its evidence: `E`, `Ê`, `U`, `U_R`, alarms, region, mandate class |
| Autonomous | Launched by the harness, subject to the uncertainty gate |

## Degraded operation

| Lost | Effect | Loop |
| :--- | :--- | :--- |
| Verification pass incomplete | `U` stays high on unverified files | Assisted and manual act; autonomous waits for `U ≤ U_max` and `U_R ≤ U_max` on the chosen region |
| Action Journal and tool hooks | Every change is a disturbance; `r` gets no update, `P` grows, and `U` with it | `E` remains measured; the law acts on level, with the projection weakening |
| Code graph | No dead-code screen and `N` is empty; `ω_j²` enters `U` | Exposure falls back to `F`, `D`, and hotspots; dead code only from confirmation |
| An analyzer | Its metric has a coverage gap | Those files enter `B` and `U` rises |
| Capability counts | No alarms | The law acts on `Ê` only |
| Execution route | `W` falls back to the mean per session; exposure is hotspots only | Unchanged otherwise |

## Policy parameters

| Symbol | Meaning |
| :--- | :--- |
| `τ`, `τ_ι` | Complexity threshold of S1; indentation-depth threshold of B1 |
| `k` | Line-window length of B2 |
| `C_chunk`, `ρ_idle`, `ρ_engaged`, `ρ_full` | Verification chunk size and rates by phase |
| `a_F`, `a_P`, `a_D`, `a_N`, `a_H`, `ε` | Exposure weights and hotspot smoothing |
| `κ_i` | Metric scale weights |
| `q`, `σ_0²`, `σ_min²`, `ζ`, `g`, `P_0` | Production-rate filter: process noise, initial and minimum measurement noise, noise learning rate, gate width, initial variance |
| `ω²`, `ω_j²` | Prior variance a blind file adds to `E`; variance each absent fused sensor `j` adds to `U` |
| `ω_b²` | Initial calibration variance of the base proxies, until residuals against S1 and S2 replace it |
| `γ`, `γ_0`, `α_0`, `β_0`, `μ`, `H` | Capability filter: forgetting, baseline forgetting, priors, rise ratio, alarm threshold |
| `E_lo`, `E_hi`, `δ`, `U_max` | Release and launch levels, deadband, uncertainty gate on `U` |
| Demotion fraction and minimum registered work, per mandate class | Automatic demotion (`PR-MNT-32`) |
| Findings and files limits | Cycle bound (`PR-MNT-4`) |
