# PRD: Setup Experience and Stack Selection

## 1. Functional Requirements

### 1.1 Setup Modes

| ID | Requirement |
| --- | --- |
| PR-SET-1 | Setup MUST offer the Takt default stack and optional customization over that same stack. Customization MUST preserve prepared choices and return to the same review, not create an incompatible “expert mode” or a capability-limited simple product. |
| PR-SET-2 | The Takt default stack MUST include specialist agent definitions, skills for proactive orchestration workflows, essential MCP integrations for memory, codebase exploration, and documentation access, and curated skills that keep work consistent across the models assigned to specialists. |
| PR-SET-3 | The Takt default stack MUST include the OpenCode v2 runtime extensions in the official integration scope, customization details, and permission settings for safe operation without unnecessary blocking. |
| PR-SET-4 | OpenCode v2 setup MUST offer an optional model-assignment tool. |

### 1.2 Detection and User Choice

| ID | Requirement |
| --- | --- |
| PR-SET-5 | Setup MUST detect whether OpenCode v2 is available and show the result before installation. Detection MUST NOT authorize installation or expand the chosen scope. |
| PR-SET-6 | If OpenCode v2 is unavailable, setup MUST explain the missing prerequisite before mutation. A deterministically incompatible composition MUST be blocked before mutation with a viable alternative or missing prerequisite identified. |

### 1.3 OpenCode v2 Model Assignment

| ID | Requirement |
| --- | --- |
| PR-SET-7 | Declining explicit model assignment in OpenCode v2 MUST NOT, by itself, block setup or require opening the assignment tool. Takt MUST defer to the default model resolved by OpenCode v2. |
| PR-SET-8 | In that unassigned case, the orchestrator and Takt-defined specialists MUST use the model resolved by OpenCode v2 until the user explicitly changes their assignments. Takt MUST NOT require specialist-by-specialist assignment to begin exploring the stack. |

### 1.4 Completion and Handoff

| ID | Requirement |
| --- | --- |
| PR-SET-9 | The ordinary default path MUST let the user review the OpenCode v2 scope, inspect the prepared setup, and authorize installation without choosing every capability. It SHOULD require only 2–3 meaningful decisions or confirmations; screens with no decision MUST NOT add “next” steps. Optional personalization returns to the review with choices intact. |
| PR-SET-10 | After setup, the application MUST provide a quick route to further configuration or exit to use the installed stack. For OpenCode v2, it MUST also offer immediate optional model assignment. |
| PR-SET-32 | After applying the plan, setup MUST verify the installed result rather than infer it from the files written: each installed MCP integration MUST be contacted to confirm it responds, and the Takt orchestrator MUST be confirmed selectable as the session agent. The result MUST report each affected capability as verified, not verified, or not verifiable, and MUST NOT present a not-verified result as ready to work. |
| PR-SET-11 | The intended successful OpenCode v2 handoff MUST have the Takt orchestrator selected as the session agent and the installed MCP integrations functional, with no additional Takt setup required. |

### 1.5 Custom Capability Composition

| ID | Requirement |
| --- | --- |
| PR-SET-12 | The Takt default stack is the mandatory core plus every curated optional capability the host supports; both modes select over the same catalog. Custom setup MUST retain the mandatory core, which includes the complete Takt crew (every specialist of the canonical definition), Engram memory, the base local inference assets of dreaming, CodeGraph codebase navigation, and core workflow skills. Core workflow skills MUST depend only on capabilities included in the mandatory core. |
| PR-SET-13 | Custom setup MUST allow the user to omit optional capabilities, including non-core skills and workflows. The crew is never omittable. Omitting optional capabilities MUST NOT leave the mandatory core dependent on absent tools or skills. |
| PR-SET-14 | Optional capabilities MUST be presented by their user-facing purpose, allowing the user to quickly read what each one does and adjust selections without manually assembling technical dependencies. |
| PR-SET-15 | For each selected capability, setup MUST compose the minimum supporting managed assets required for a functional result. A selection MUST NOT be considered a valid functional composition if it leaves a capability requiring an absent tool or another unresolved dependency. Required supporting assets MUST be reflected in the reviewable installation plan. |
| PR-SET-16 | Each installable capability MUST have a defined responsibility and support reuse and composition. Each MUST be self-sufficient or adapt to the available capabilities while fulfilling its offered function; customization MUST NOT silently produce a broken version of that function. |
| PR-SET-17 | Setup MUST resolve capability composition internally wherever possible. If a selection change would nevertheless break other selected optional capabilities, setup MUST reconcile the selection, removing capabilities that cannot remain functional, and briefly explain the affected capabilities and reason. It MUST NOT silently restore a dependency the user explicitly deselected. |
| PR-SET-31 | The curated catalog MUST be declared in a versioned manifest that, for each capability, states its user-facing purpose, whether it belongs to the mandatory core or is optional, and its dependencies on other capabilities and on supporting managed assets. Setup MUST resolve composition against that manifest. |
| PR-SET-34 | A capability MAY declare a hardware requirement in the manifest. Such a capability is always optional: the default stack includes it only when the host meets the requirement, and custom setup MAY omit it. When the host does not meet it, setup does not install it and shows it as unavailable with its reason in one line. Its absence MUST NOT break any other capability. |
| PR-SET-18 | Dependency reconciliation during selection MUST update the proposed configuration, not mutate the installed environment. The resulting selection and its consequences MUST remain visible for review before installation authorization. |

### 1.6 Returning to an Existing Installation

| ID | Requirement |
| --- | --- |
| PR-SET-19 | The TUI MUST open on the task-selection menu whether or not Takt is already installed. Installation detection MAY change which entries the menu offers under `PR-SET-33`, but MUST NOT replace this entry point with an automatic onboarding or reconfiguration flow. |
| PR-SET-33 | The task-selection menu MUST offer `Install` when no installation exists. For an existing installation its task vocabulary is `Configure installation`, `Assign models`, `Check for drift` (PR-SET-22), `Upgrade`, `Uninstall`, and `Diagnostics`, followed by `Quit`. Tasks that do not apply to the current environment are omitted; a relevant but temporarily unavailable task remains discoverable with its reason. |
| PR-SET-20 | As the user navigates a configuration flow, the application MUST show the current installation state relevant to that flow and use it as the starting point for adjustments. Returning to configure an existing installation MUST NOT implicitly reset its selections to the Takt default stack. |
| PR-SET-21 | Adjusting an existing installation MUST start at the chosen object or scope, preserve unrelated configuration, and apply the proposed changes once without repeating onboarding or a redundant confirmation. The result MUST identify when the configuration takes effect in OpenCode v2; returning from a nested edit MUST preserve the parent choices and position. |

### 1.7 Correction Choices

| ID | Requirement |
| --- | --- |
| PR-SET-22 | The TUI MUST present correction of drift as an explicit user operation that restores the expected state of the installed version. A correction route MUST NOT implicitly select a full reset to default. Drift correction and upgrade MUST be presented as distinct operations. |

### 1.8 Installation Scope

| ID | Requirement |
| --- | --- |
| PR-SET-23 | The official integration MUST support global installation for the user of OpenCode v2. Project-scoped installation is not supported and MUST NOT be offered as a setup operation. |
| PR-SET-24 | Working with a repository from the Docker integration environment MUST NOT be represented as support for project-scoped Takt installation. |
| PR-SET-26 | Takt MUST support ARM64 on macOS and standard Linux distributions, using Ubuntu Server as the Linux reference environment. The minimum supported versions are the most recent Ubuntu LTS and the most recent generally available macOS major release. Support for a newer major release requires its own validation evidence; its publication alone does not establish support. AMD64, other Linux distributions such as Fedora, and WSL are expected compatibility targets, not required release-blocking support targets. Compatibility expectations MUST NOT be presented as verified support. Native Windows is unsupported. |

### 1.9 Acquisition Responsibility

| ID | Requirement |
| --- | --- |
| PR-SET-28 | Takt MUST acquire and configure the selected MCP integrations and other straightforward supporting assets needed for their OpenCode v2 integration, within the authorized installation plan. The curated capability catalog MUST NOT introduce complex dependency setup or substantial environment changes beyond those required to integrate with OpenCode v2. |
| PR-SET-29 | Completing Takt TUI operations MUST NOT require the user to provide credentials or API tokens, nor assume that such secrets are already available. Setup MUST NOT introduce a credential-entry or account-provisioning step as a prerequisite for completing those operations. |
| PR-SET-30 | LLM authentication MUST remain the responsibility of OpenCode v2 when agents are invoked. Takt setup MUST NOT require LLM authentication to be completed in order to configure the stack, and completion of setup MUST NOT be represented as proof that OpenCode v2 has authenticated successfully with an LLM provider. |

### 1.10 Execution-Policy Delivery

| ID | Requirement |
| --- | --- |
| PR-SET-35 | Setup installs the selected canonical admission policy and execution-boundary configuration as functional dependencies. Adjusted policy values reach both runtime enforcement and generated crew guidance. Hardware-conditional control sensors remain optional and do not remove deterministic control or result-sensing obligations. |
| PR-SET-36 | Every limit of the canonical admission policy MUST be adjustable from the TUI with the same override pattern as model assignments. |

## 2. Failure Behavior

| Situation | Expected Behavior |
| --- | --- |
| OpenCode v2 cannot resolve a usable model for the unassigned case of PR-SET-7 | Report the functional limitation as a warning distinct from the operation result. Setup MUST NOT be classified as a failure for this reason alone, and MUST NOT be presented as ready to work. |
| A prerequisite of a selected MCP integration is unavailable while the plan is being resolved | Fail visibly before mutation, identifying the affected capability and the missing prerequisite. |
| A prerequisite of a selected MCP integration becomes unavailable during execution | Report a visible partial result, identifying the affected capability and what remains functional. |
