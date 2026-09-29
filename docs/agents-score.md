# Agentic Swarm Evaluation: Pacman Benchmark

**Goal**: a reproducible test that tells whether a harness/scaffold change made the swarm better, worse, or no different, judged on the final artifact.

## 1. Principle

> *"Grade what the agent produced, not the path it took."* — Anthropic, 2026

Instruction + container + tests on the end state (Terminal-Bench pattern). Every trial starts from a clean container. Tokens, cost and latency are logged separately for CLEAR (§6) and never enter the efficacy score.

## 2. What not to do

| Approach | Failure |
|---|---|
| Name/library assertions (`"ghost" in code`, `import pygame`) | False negatives on valid alternatives |
| Keyword counting as "behavior" | A keyword-stuffed docstring scores 100% |
| LLM judge on source code | Length/style bias; rewards broken code |
| Screenshot judge alone | Game can look right and break rules (GameGen-Verifier, 2026) |

Grade observable runtime behavior, add state keypoints when the task defines a contract, and use a human-calibrated visual judge.

## 3. Verification levels

```
Level 1  Hard invariants    GATE (0/1)
Level 2  Runtime behavior   50 pts   (+ optional 2b: state keypoints)
Level 3  Visual judge       50 pts
SCORE = gate × (L2 + L3)
```

**Runtime environment**
- Xvfb on a fixed `DISPLAY` passed to game, capture and input tools (not `xvfb-run -a`, whose random display the helpers can't see).
- Game runs in its own process group; kill the group on teardown.
- Game output goes to a log file, not a pipe (a full pipe blocks the game).
- Capture the game window by PID, falling back to the full screen. Set input focus before sending keys (no window manager).
- Terminal games run inside `xterm`. Entry point: conventional name or first file with `__main__`. Skip venvs, `site-packages`, `tests/`.

### Level 1 — Gate
All sources compile (`py_compile`, no imports: importing an unguarded module starts the game loop) and the entry point is still alive 3 s after launch. Failure → score 0. Being alive earns no points, so `sleep 60` scores 0 overall.

### Level 2 — Runtime behavior (50)
Frame change = fraction of pixels changing > 25 in any channel (compute with signed ints; `uint8` subtraction overflows).

| Metric | Weight | Rule |
|---|---|---|
| render | 0.3 | 0 if flat screen; else significant colors (≥0.05% of pixels): 3 → 0.25, ≥6 → 1.0 |
| dynamics | 0.3 | Change t1→t2 (1 s apart): 0.1–30% → 1.0; >30% → 0.5; <0.1% → 0 |
| input | 0.4 | Share of 3 rounds where change *with* keys clearly beats a *no-key* control over the same interval |

Keys go in one direction only (right-down-left-up returns the avatar to its start). If the process dies mid-test, L2 = 0.

### Level 2b — State keypoints (optional)
Keypoints as Hoare triples with the start state injected instead of played to (GameGen-Verifier: 92% accuracy vs 59% for free-play agents). This requires the task prompt to specify `--load-state` / `--state-file` with `score`, `lives`, `pellets_left` and avatar/ghost/wall positions.

| Keypoint | Pre | Action | Post |
|---|---|---|---|
| Eat pellet | Pellet right of avatar | Right | score ↑, pellets_left ↓ |
| Wall blocks | Wall right | Right | Position unchanged |
| Ghost kills | Ghost right | Right | lives ↓ |

With 2b active, L2 = 25 × runtime + 25 × keypoints passed. Fix the mode per campaign.

### Level 3 — Visual judge (50)
The judge sees three real frames (t1, t2, post-input) and scores avatar, maze, ghosts, pellets and HUD at 0 / 0.5 / 1. Motion is already covered by L2.
- Pinned model ID, temperature 0, 3 samples. The harness computes the median of the per-sample means × 50; the LLM does no arithmetic.
- Skip the judge if render = 0.
- **Calibrate** on ≥20 human-labeled frame sets and require ≥80% per-criterion agreement. Recalibrate whenever the judge changes; a new judge breaks comparability.

## 4. Bands

| Score | Meaning |
|---|---|
| 0 | Gate failed |
| 1–29 | Boots, barely renders or frozen |
| 30–59 | Partial prototype |
| 60–79 | Playable (**pass threshold = 60**) |
| 80–100 | Complete |

## 5. Comparing runs

Welch t + Cohen's d at N=5 is inadequate:
- It only detects d ≈ 2; d = 0.8 needs ~26 runs per arm.
- The gate makes scores bimodal.
- Zero variance yields NaN.

Runs per arm (α = 0.05, power 0.8) ≈ 2(z₀.₉₇₅ + z₀.₈)²/d² + 1, i.e. d = 2 → 5, d = 1.3 → 11, d = 0.8 → 26.

Protocol:
1. Pick N by power analysis (Miller 2024).
2. Report the mean difference with a 95% percentile bootstrap CI. Use Mann-Whitney as a secondary check (p = 1 if all values are identical).
3. Report pass rates with Wilson CIs (as Terminal-Bench does).
4. Apply a symmetric practical margin δ (e.g. 5 pts):
   - CI > 0 and Δ ≥ δ → improvement.
   - CI < 0 and −Δ ≥ δ → regression.
   - CI within ±δ → equivalent.
   - Anything else is inconclusive: add runs.
5. With several tasks, pair by task/seed and bootstrap over tasks, not trials.
6. Give the baseline the same compute budget (best-of-k or more retries). Many harness "gains" vanish against equal-compute test-time scaling (Wang et al. 2026).

## 6. CLEAR (Mehta 2025)

Cost (score/USD), Latency, Efficacy (score + CI), Assurance (lint errors, runtime warnings), Reliability (stdev, pass^k, Wilson CI). Agents drop from 60% on one run to 25% when 8-run consistency is required, so report pass^k alongside pass@k.

- pass@k = 1 − C(n−c, k)/C(n, k); pass^k = C(c, k)/C(n, k); requires n ≥ k.
- Example with n = 8, c = 6: pass@4 = 1.0, pass^4 = 0.21, Wilson 95% CI [0.41, 0.93].

## 7. Principles

1. Scaffold choice moves SWE-bench Verified by up to 11–15 pts for the same model (Epoch AI 2025). Pin the model and vary only the harness.
2. Use ephemeral, clean containers per trial.
3. Screenshots beat code-reading judges but miss mechanics: add state keypoints and calibrate every model judge.
4. Read a sample of transcripts regularly to catch broken tests, reward hacking and eval harness bugs.
5. Compare at equal compute.
6. Keep ≥2 held-out tasks (Snake, Tetris). Use them to confirm results, never to iterate.

## 8. References

- Anthropic, *Demystifying Evals for AI Agents*, 2026 — https://www.anthropic.com/engineering/demystifying-evals-for-ai-agents
- Merrill et al., *Terminal-Bench*, ICLR 2026 — arXiv:2601.11868
- Miller, *Adding Error Bars to Evals*, 2024 — arXiv:2411.00640
- Jia et al., *GameGen-Verifier*, 2026 — arXiv:2605.07442
- Zhang et al., *V-GameGym*, 2025 — arXiv:2509.20136
- Mehta, *Beyond Accuracy (CLEAR)*, 2025 — arXiv:2511.14136
- Wang et al., *Rethinking the Evaluation of Harness Evolution for Agents*, 2026 — arXiv:2607.12227
- Brand & Denain, *Why Benchmarking Is Hard*, Epoch AI, 2025 — https://epoch.ai/gradient-updates/why-benchmarking-is-hard
