# PRD: Setup Experience and Stack Selection

**Actual implementation progress: 95%** — 31 own requirements: 28 complete, 3 partial, 0 not integrated. Gaps: the menu has no Upgrade entry; only one capability is omittable and the manifest does not declare supporting managed assets.

## 1. Problem

Users should be able to open Takt, prepare OpenCode v2 with a coherent Takt-defined stack, and begin working without designing or administering that stack themselves. Deliberate customization remains available within the same setup flow.

Terms such as *capability*, *managed asset*, *platform*, *Takt default stack*, and *mandatory core* carry the meanings defined in `product/GLOSSARY.md`. Every requirement below is a first-release obligation.

## 2. Functional Requirements

### 2.1 Setup Modes

| ID | Requirement |
| --- | --- |
| PR-SET-1 | Setup MUST offer a complete Takt default configuration and optional customization over that same configuration. Customization MUST preserve prepared choices and return to the same review, not create an incompatible “expert mode” or a capability-limited simple product. |
| PR-SET-2 | Takt default MUST include specialist agent definitions, skills for proactive orchestration workflows, essential MCP integrations for memory, codebase exploration, and documentation access, and curated skills intended to maintain consistent work across the models assigned to specialists. |
| PR-SET-3 | Takt default MUST include the OpenCode v2 runtime extensions in the official integration scope, customization details, and permission settings intended for safe operation without unnecessary blocking. |
| PR-SET-4 | OpenCode v2 setup MUST offer an optional model-assignment tool. |

### 2.2 Detection and User Choice

| ID | Requirement |
| --- | --- |
| PR-SET-5 | Setup MUST detect whether OpenCode v2 is available and show the result before installation. Detection MUST NOT authorize installation or expand the chosen scope. |
| PR-SET-6 | If OpenCode v2 is unavailable, setup MUST explain the missing prerequisite before mutation. A deterministically incompatible composition MUST be blocked before mutation with a viable alternative or missing prerequisite identified. |

### 2.3 OpenCode Model Assignment

| ID | Requirement |
| --- | --- |
| PR-SET-7 | Declining explicit model assignment in OpenCode v2 MUST NOT, by itself, block setup or require opening the assignment tool. Takt MUST defer to the default model resolved by OpenCode v2. |
| PR-SET-8 | In that unassigned case, the orchestrator and Takt-defined specialists MUST use the model resolved by OpenCode v2 until the user explicitly changes their assignments. Takt MUST NOT require specialist-by-specialist assignment to begin exploring the stack. |

### 2.4 Completion and Handoff

| ID | Requirement |
| --- | --- |
| PR-SET-9 | The ordinary default path MUST let the user review the OpenCode v2 scope, inspect the prepared setup, and authorize installation without choosing every capability. It SHOULD require only 2–3 meaningful decisions or confirmations; screens with no decision MUST NOT add “next” steps. Optional personalization returns to the review with choices intact. |
| PR-SET-10 | After setup, the application MUST provide a quick route to further configuration or exit to use the installed stack. For OpenCode, it MUST also offer immediate optional model assignment. |
| PR-SET-32 | After applying the plan, setup MUST verify the installed result rather than infer it from the files written: each installed MCP integration MUST be contacted to confirm it responds, and the Takt orchestrator MUST be confirmed selectable as the session agent. The result MUST report each affected capability as verified, not verified, or not verifiable, and MUST NOT present a not-verified result as ready to work. |
| PR-SET-11 | The intended successful OpenCode handoff MUST have the Takt orchestrator selected as the session agent and the installed MCP integrations functional, allowing the user to begin work without additional Takt setup. Merely placing configuration files MUST NOT be treated as evidence of this functional outcome. |

### 2.5 Custom Capability Composition

| ID | Requirement |
| --- | --- |
| PR-SET-12 | The mandatory core is a strict subset of the Takt default stack: the default stack is the mandatory core plus every curated optional capability, and both modes select over the same catalog. Custom setup MUST retain that mandatory core, which includes the complete Takt crew (every specialist of the canonical definition, so that every role class of `PRD_CREW` is present), Engram memory, CodeGraph codebase navigation, and core workflow skills. Core workflow skills MUST depend only on capabilities included in the mandatory core. |
| PR-SET-13 | Custom setup MUST allow the user to omit optional capabilities, including non-core skills and workflows. The crew is never omittable. Omitting optional capabilities MUST NOT leave the mandatory core dependent on absent tools or skills. |
| PR-SET-14 | Optional capabilities MUST be presented by their user-facing purpose, allowing the user to quickly read what each one does and adjust selections without manually assembling technical dependencies. |
| PR-SET-15 | For each selected capability, setup MUST compose the minimum supporting managed assets required for a functional result. A selection MUST NOT be considered a valid functional composition if it leaves a capability requiring an absent tool or another unresolved dependency. Required supporting assets MUST be reflected in the reviewable installation plan. |
| PR-SET-16 | Each installable capability MUST have a defined responsibility and support reuse and composition. Each MUST be self-sufficient or adapt to the available capabilities while fulfilling its offered function; customization MUST NOT silently produce a broken version of that function. |
| PR-SET-17 | Setup MUST resolve capability composition internally wherever possible, so dependency conflicts are exceptional rather than a routine task for the user. If a selection change would nevertheless break other selected optional capabilities, setup MUST reconcile the selection, removing capabilities that cannot remain functional, and briefly explain the affected capabilities and reason. It MUST NOT silently restore a dependency the user explicitly deselected. |
| PR-SET-31 | The curated catalog MUST be declared in a versioned manifest that, for each capability, states its user-facing purpose, whether it belongs to the mandatory core or is optional, and its dependencies on other capabilities and on supporting managed assets. Setup MUST resolve composition against that manifest. This document defines the rules the manifest must satisfy; it does not enumerate its content. |
| PR-SET-18 | Dependency reconciliation during selection MUST update the proposed configuration, not mutate the installed environment. The resulting selection and its consequences MUST remain visible for review before installation authorization. |

### 2.6 Returning to an Existing Installation

| ID | Requirement |
| --- | --- |
| PR-SET-19 | The TUI MUST open on the task-selection menu whether or not Takt is already installed. Installation detection MAY change which entries the menu offers, per PR-SET-33, but MUST NOT replace this entry point with an automatic onboarding or reconfiguration flow. |
| PR-SET-33 | In the first release the task-selection menu MUST offer `Install` when no installation exists. For an existing installation its task vocabulary is `Configure installation`, `Assign models`, `Check for drift` (PR-SET-22), `Upgrade`, `Uninstall`, and `Diagnostics`, followed by `Quit`. Tasks that do not apply to the current environment are omitted; a relevant but temporarily unavailable task remains discoverable with its reason, per PR-UX-10. |
| PR-SET-20 | As the user navigates a configuration flow, the application MUST show the current installation state relevant to that flow and use it as the starting point for adjustments. Returning to configure an existing installation MUST NOT implicitly reset its selections to the default preset. |
| PR-SET-21 | Adjusting an existing installation MUST start at the chosen object or scope, preserve unrelated configuration, and apply the proposed changes once without repeating onboarding or a redundant confirmation. The result MUST identify when the configuration takes effect in OpenCode v2; returning from a nested edit MUST preserve the parent choices and position. |

### 2.7 Correction Choices

| ID | Requirement |
| --- | --- |
| PR-SET-22 | The TUI MUST present correction of drift as an explicit user operation that restores the expected state of the installed version. A correction route MUST NOT implicitly select a full reset to default. Drift correction and upgrade MUST be presented as distinct operations per PR-INS-28. |

### 2.8 First Release Installation Scope

| ID | Requirement |
| --- | --- |
| PR-SET-23 | The official integration MUST support global installation for the user of OpenCode v2. Project-scoped installation is not supported and MUST NOT be offered as a setup operation. |
| PR-SET-24 | Working with a repository from the Docker integration environment MUST NOT be represented as support for project-scoped Takt installation. |
| PR-SET-26 | This requirement is the single source of the first release's platform support scope. The first release MUST support ARM64 on macOS and standard Linux distributions, using Ubuntu Server as the Linux reference environment. The minimum supported versions are the most recent Ubuntu LTS and the most recent generally available macOS major release. Adopting a newer major release is a deliberate decision that requires its own validation evidence; it MUST NOT be assumed to follow automatically from its publication. AMD64, other Linux distributions such as Fedora, and WSL are expected compatibility targets, not required release-blocking support targets. Compatibility expectations MUST NOT be presented as verified support. Native Windows is unsupported. |

### 2.9 Acquisition Responsibility

| ID | Requirement |
| --- | --- |
| PR-SET-28 | Takt MUST acquire and configure the selected MCP integrations and other straightforward supporting assets needed for their OpenCode v2 integration, within the authorized installation plan. The curated capability catalog MUST NOT introduce complex dependency setup or substantial environment changes beyond those required to integrate with OpenCode v2. |
| PR-SET-29 | Completing Takt TUI operations MUST NOT require the user to provide credentials or API tokens, nor assume that such secrets are already available. Setup MUST NOT introduce a credential-entry or account-provisioning step as a prerequisite for completing those operations. |
| PR-SET-30 | LLM authentication MUST remain the responsibility of OpenCode v2 when agents are invoked. Takt setup MUST NOT require LLM authentication to be completed in order to configure the stack, and completion of setup MUST NOT be represented as proof that OpenCode v2 has authenticated successfully with an LLM provider. |

## 3. Failure Behavior

| Situation | Expected Behavior |
| --- | --- |
| OpenCode v2 cannot resolve a usable model for the unassigned case of PR-SET-7 | Report the functional limitation as a warning distinct from the operation result, per PR-INS-24. Setup MUST NOT be classified as a failure for this reason alone, and MUST NOT be presented as ready to work. |
| A prerequisite of a selected MCP integration is unavailable while the plan is being resolved | Fail visibly before mutation, per PR-INS-6, identifying the affected capability and the missing prerequisite. |
| A prerequisite of a selected MCP integration becomes unavailable during execution | Report a visible partial result per PR-INS-10, identifying the affected capability and what remains functional. |

## 4. Acceptance Scenarios

- A user choosing default can install the curated Takt stack into OpenCode v2 without choosing each agent, skill, or MCP integration separately.
- A proposed composition with unresolved dependencies is not presented as functional merely because its individual files can be installed.
- If deselecting a non-core skill or workflow leaves an optional capability unable to function, setup removes that capability from the proposed selection and explains the consequence concisely, without modifying the installed environment or silently reselecting the dependency.
- A returning user sees the task-selection menu, enters the desired configuration flow, and finds the current installation reflected there without repeating onboarding or implicitly restoring default selections.
- A user correcting a modified capability can distinguish that action from upgrading it; the correction restores its version-specific expected state while retaining unrelated custom choices.
- A completed setup reports each installed MCP integration as verified, not verified, or not verifiable, rather than declaring the stack ready because its files were written.
- A user can complete Takt TUI setup without entering credentials or API tokens; any LLM authentication needed when invoking an agent remains an OpenCode v2 concern.
