# PRD: Release Experience Validation

## 1. Requirements

### 1.1 First-Use Integration Image

| ID | Requirement |
| --- | --- |
| PR-REL-1 | Release validation MUST have a Docker integration-test image containing OpenCode v2 and the Takt application ready for its first configuration. |
| PR-REL-2 | The initial integration-test environment MUST NOT have Takt onboarding already completed. It MUST allow the tester to experience first-use navigation, installation, and configuration directly rather than only inspect a preinstalled stack. |
| PR-REL-3 | Installation and configuration under test MUST target the test environment, not the host's real OpenCode v2 installation or global configuration. Any repository made available for hands-on work MUST be an intentional test input, not an implicit expansion of setup scope. |

### 1.2 Final Manual Check

| ID | Requirement |
| --- | --- |
| PR-REL-4 | Manual integration validation MUST occur after the required automated checks, including applicable CI, unit, and end-to-end tests, pass. It MUST be a final release gate, not a substitute for those checks. |
| PR-REL-5 | The tester MUST configure Takt through its first-use experience, navigate and perform the adjustments under evaluation, then open OpenCode v2 and use the resulting Takt-defined stack for actual work. Merely completing installation or checking generated files MUST NOT satisfy this gate. |
| PR-REL-6 | Hands-on work MAY use a specific task or a repository made available to the test environment. The check MUST evaluate the transition from application setup to real harness use as one user journey while preserving their distinct responsibilities. |
| PR-REL-7 | A release MUST NOT be considered ready without completing this final manual experience check successfully. |

### 1.3 Required and Expected Compatibility

| ID | Requirement |
| --- | --- |
| PR-REL-8 | Release validation MUST cover the required support targets defined in PR-SET-26. A Linux container result alone MUST NOT be treated as evidence of native macOS compatibility. Validation MUST preserve the prohibition on using the host's real installation as the target for potentially broken setup changes. |
| PR-REL-9 | Results for the expected compatibility targets defined in PR-SET-26 MUST remain distinct from evidence for the required targets, and MUST NOT, by themselves, block release. |
| PR-REL-10 | Project tests and CI MUST NOT require a developer's real host installation or personal working configuration as a test target. Validation for supported operating systems and architectures MUST be designed around isolated test environments rather than depend on personal host use. |

### 1.4 Brand-derived surface validation

| ID | Requirement |
| --- | --- |
| PR-REL-11 | Release evidence MUST evaluate the applicable acceptance criteria of [VDS_TUI §§5–6](tui/VDS_TUI.md) using its task, state, and environment matrix. Record actual build, terminal/capability profile, dimensions, steps and results. Open gaps MUST be explicit, and upgrade scenarios MUST be evidenced. |
| PR-REL-12 | Validation MUST include default installation and personalization/return, a focused model adjustment, feasible external-change uncertainty versus a known incompatible composition, scoped restoration, and injected partial failure/cancellation with actual recovery limits. Repeat pertinent journeys without color, with long text, in 120×32, 80×24 and 60×20 terminals, and verify below-minimum handling and linear output. Hidden risk, silent loss of intent, impossible configurations presented as valid, inaccessible exits, illegible functional text, or advertised but absent recovery MUST block approval. |

### 1.5 Test Image Reuse and Public Image

| ID | Requirement |
| --- | --- |
| PR-REL-13 | The integration-test image MUST be reusable for automated end-to-end testing in addition to manual release validation. |
| PR-REL-14 | A public release image derived from the development/test image MUST ship with downloads and base setup prepared in advance. It does not replace the first-use test image. |

### 1.6 Crew Convergence Validation

| ID | Requirement |
| --- | --- |
| PR-REL-15 | Automated reference scenarios cover a bounded change and a new 2D-platformer MVP using two model families. Record reproducible control-bus-derived metrics: tokens per task, orchestrator context per dispatch, interactive requests per session, computationally validated features, deferred E2E failures, stagnation events, and DAG nodes per delivered feature. Pin scenario inputs, model identities, policy, and evidence scope. |
| PR-REL-16 | Reference scenarios verify that dispatch payload size does not grow with GC history, executable units have result evidence, unverified criteria never count as accepted, corrections terminate within configured bounds, and work inside the execution boundary requests no interaction. Report nodes per feature and actions to the first executable slice with raw counts and denominators. |
