# Control design

Engineering design of Takt's closed-loop control systems, ahead of implementation. `product/` states what the system must be; this set states how it is built.

The first system documented here is workspace GC.

## Vehicle model

The crew together with the harness is one vehicle. The model assumes it is inherently unstable: the entropy it adds to the workspace degrades it, and a degraded crew adds entropy faster. It operates over terrain it does not control. The loop holds the vehicle's capability, not the terrain.

| Vehicle | Takt | Product |
| :--- | :--- | :--- |
| Vehicle being stabilized | Crew and harness, as one system | `PR-OBS-EST-1` |
| Attitude to hold | Crew capability | `PR-OBS-EST-1`, `PR-MNT-9`, `PR-MNT-10` |
| Terrain | Workspace code | `PR-OBS-EST-1` |
| Terrain roughness | Workspace entropy | GLOSSARY |
| Own thrust: commanded, fully measured | VFS operations | `PR-OBS-EST-4` |
| Gusts and impacts | Disturbances: changes not attributed to the crew (manual edits, other tools, other projects, merges) | `PR-OBS-EST-4` |
| Flight plan | Execution route of the DAG: settled, in flight, planned | `PR-OBS-EST-7` |
| IMU | Change scan | `PR-OBS-EST-2` |
| GPS | Full content verification | `PR-OBS-EST-2`, `PR-OBS-EST-5` |
| Auxiliary sensors | Action Journal, tool hooks, code graph, workspace analyzers | `PR-OBS-EST-3` |
| Preflight | Session preflight | `PR-OBS-EST-5`, `PR-OBS-EST-6` |
| State estimator | Workspace estimate | `PR-OBS-EST-8`, `PR-OBS-EST-9` |
| Flight controller | GC control law | `PR-MNT-34`, `PR-MNT-35` |
| Corrective actuator | GC cycle | `PR-MNT-33`, `PR-MNT-36`, `PR-MNT-4` |
| Flight modes: manual, assisted, mission | Launch authority modes | `PR-MNT-37` |
| Actuator fault isolation | Automatic demotion | `PR-MNT-32` |

## Loop

[Loop block diagram](diagrams/gc-loop.html)

Without control, entropy compounds even with no disturbance at all, so a fixed cadence is open loop: it spends cycles on quiet periods and arrives late after bursts. The loop estimates entropy where the crew is about to work, projects it over the planned work, and launches the minimum sufficient cycle. Its quality criterion is disturbance rejection: after one or several impacts, return to the operating region quickly, without overshoot or oscillation against crew work, and keep working with any sensor lost except the base one.

## Documents

| Document | Content |
| :--- | :--- |
| [sensors.md](sensors.md) | Every sensor: what it measures, why it is in the loop, acquisition, cost, noise, conditioning |
| [estimation.md](estimation.md) | State, exposure weighting, attribution, the filters and their equations, initialization |
| [gc-controller.md](gc-controller.md) | Control law, scope selection, modes, degraded operation, policy parameters |

## Conventions

- **Work axis.** Every control rate and window is per unit of recorded work, never per unit of time (`PR-OBS-PRG-2`, `PR-OBS-DET-3`). Sample index `k`; `Δw_k` is the crew-attributed actions recorded between samples `k−1` and `k`.
- **Policy data.** Every symbol listed under policy parameters is data in the canonical policy, not a constant in code (`PR-OBS-BUS-4`).
- **Replay.** Each estimate and decision is a deterministic function of recorded measurements, policy data, and the previous estimate (`PR-OBS-EST-8`).
- **Measured values.** Costs quoted here were measured on this repository (40 k lines of Go and 8 k of TypeScript) and on a 248 k-file tree. They size the design; they are not guarantees.
