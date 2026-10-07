# VDS: TUI Surface Profile — Takt AI

**Actual implementation progress: 95%** — 28 own requirements: 25 complete, 3 partial, 0 not integrated. `takt/tui/theme/theme.go` implements the full dark semantic token set with no per-screen hex values (verified: no hex literal anywhere under `takt/tui/install|uninstall|drift|models|modelpicker|diagnostics|ui`), `takt/tui/ui/shell.go` gives every screen the shared header/panel/footer layout with no key bar, a bounded and centered panel (`panelMaxWidth`, `PlaceHorizontal`) and lateral margins of 3 columns at 80 columns or more and 2 below, the home composes the logo beside or above the menu with no signature line (`takt/tui/tui.go` `menuBody`; `takt/tui/styles/logomode.go` probes the terminal for kitty graphics and falls back to quadrant blocks), `ui/list.go` renders checked options with `theme.Checked` (success color), `ui/status.go` shows done steps with `✓` and a real progress bar with the current file, `tui.go` takes the terminal's color profile (`tea.ColorProfileMsg`) through `theme.ModeFor`, with a verified 256-color theme and `NO_COLOR`/`TERM=dumb` selecting monochrome, and `ui/verification.go` + `takt/verify` implement the `[verified]`/`[not verified]`/`[not verifiable]` states verbatim (PR-UX-27). The three partial requirements: PR-UX-6, whose `✓`, `✗`, `┃` and panel borders have no ASCII variant and whose animation-free mode (`TAKT_NO_ANIMATION`) is documented only in a code comment; PR-UX-21, whose reconciliation reaches the review as a "Not installed" row with its reason but is never shown when toggling, and no manifest dependency triggers it today; and PR-UX-29, whose PgUp/PgDn scrolling works in install, drift, and diagnostics but not in uninstall or models. Two readings are counted as complete and could be partial: PR-UX-20 (only one capability is selectable, so there is nothing to group) and PR-UX-25 (cooperative cancellation is implemented, but the literal "Cancellation requested" text does not exist). Outside any single ID, the below-60×20 notice does not name an alternative.

**Surface/version:** installation and configuration TUI/CLI, profile 3.0.

## 1. Purpose and extension contract

Takt prepares the next session in OpenCode v2. The TUI is a reliable point for installing and adjusting that configuration, not a permanent crew monitor or a replacement harness. Users are engineers and power users, not necessarily familiar with Takt internals. The experience expresses calm competence through relevant information, durable choices, and clear recovery.

| Contract field | Surface application |
| --- | --- |
| Principles | B01 precision, B02 full capability with optional depth, B03 consistent semantics, B04 contextual confirmation, B05 present identity with clear operation, B06 recovery without reproach, B07 durable intention, B08 honest limits. |
| Patterns | L03 task menu; L04 focused configuration; L05 dependent first-install preparation; L06 model/capability comparison; L07 external-change conflict; L08 execution; L09 result/recovery. L01/L02 are not primary TUI task patterns. |
| Tokens and native equivalents | Semantic tokens mapped in §3; terminal font and cell spacing, not web typography or pixel geometry. |
| Navigation and commitment | §2 keyboard and component contracts; §4 task map and §5 state matrix. Flow PRDs own operation semantics. |
| Language | English TUI/CLI labels in this profile. Additional locales require complete labels/help and expansion tests, not partial translation. Technical identifiers preserve spelling. |
| Accessibility and environments | §3 capability policy; §6 size/theme/input/output evidence matrix. Terminal accessibility is not certified by web contrast calculations. |
| Evidence | §6 defines the required acceptance evidence for this surface. |
| Extensions | Native cell layout, capability fallback and keyboard grammar are local specializations of §§2–3. No additional meaning or palette is introduced beyond what is defined here. The implementation source is the Charm stack (Bubble Tea v2, Bubbles v2, Lip Gloss v2); library defaults such as key bars, list titles, or form chrome MUST NOT override §§2–3. |

### Decision rationale

| Adaptation | Principle | Why |
| --- | --- | --- |
| Canonical logo beside the task list on the home screen; compact header and untitled panels elsewhere | B05 | Keep the product recognizable at entry without turning operational screens into ornamental chrome. |
| One complete dark semantic theme, distinct focus/selection, no faint functional text | B03/B08 | Preserve meaning and verifiable legibility instead of approximating a local aesthetic. |
| Space selection, Enter submission of selection fields, visible commitment buttons and guarded exits | B01/B04 | Make action effects discoverable and prevent navigation from silently losing work. |
| Focused edits and durable exclusions | B02/B07 | Offer depth without repeating setup or resetting decisions. |
| State-based results and honest recovery | B06/B08 | Explain what can actually be done rather than blaming the user or promising undo. |

Apply the owning setup/installation requirement, then this profile, for any new menu or flow. New menus and known flows compose these patterns and states; they do not require a new visual identity. A genuinely new modality needs a documented reason and evidence before extending this profile.

## 2. Visual and Interactive Requirements

### 2.1 Visual Foundations

| ID | Requirement |
| --- | --- |
| PR-UX-1 | Each screen MUST read as a brief header, a dominant task body grouped in panels, and an optional row of action buttons below the panel. No key bar is rendered. Scrolling MUST preserve that reading order and keep focused controls reachable. No full-screen double frame or empty branding band is required. |
| PR-UX-2 | Operational screens MUST group their body in bordered panels without titles, centered with bounded width. Text inside a panel is left-aligned; the action button row is centered below it. List–detail is permitted only at 100 or more columns with at least 32 columns for list, 48 for detail, and room for margins/separation. Otherwise open detail as a nested view and preserve return context. |
| PR-UX-2A | The entry (home) screen MUST render the canonical logo (`docs/assets/brand/takt-ai.png`, or an approved terminal representation of it, including ASCII art) beside the task list when width allows, stacked above it otherwise. The home shows no `Takt AI` signature line: the logo alone identifies the product. Its terminal representation keeps the artwork's own colors and both rings, drawn as the artwork itself on a transparent background on terminals that accept the kitty graphics protocol when asked, and with quadrant block characters everywhere else. Support is established by querying the terminal, never by its name, and the mark is never left to font fallback. Operational screens (selection, edit, review, execution, result, conflict) MUST NOT reserve a mandatory logo band; the shell, panel composition, and accent carry continuity instead. |
| PR-UX-3 | The TUI has a single dark theme. It MUST consume the complete dark semantic token set, including painted canvas and panel backgrounds. There is no light or inherited-color theme. The token mapping in §3 is the only local adaptation; no separate palette or per-screen hex values are permitted. |
| PR-UX-4 | Color MUST express semantic roles, never decoration alone. Focus, persistent selection, information, success, warning, and failure MUST remain distinct through markers and labels without color. Petroleum identifies brand/orientation and focus, green verified success and checked options, amber uncertainty/warning, and red failure or destructive consequence. |
| PR-UX-5 | The host controls the monospace font. Use heading (`text.primary`, bold), content (`text.primary`), secondary (`text.secondary`), and supporting text (`text.muted`) roles with spacing and labels. Signature MAY use `brand.ink`; focus is an independent marker, not a heading role. No faint/opacity styling on functional text, imposed font, or simulated font sizes. |
| PR-UX-26 | Reduced color capability MUST NOT block startup or require a decision. Apply §3 capability policy: verified native mapping when available, otherwise monochrome. Nearest-color conversion alone is not contrast evidence. Preserve semantic names and markers in every mode. |
| PR-UX-6 | The baseline glyph vocabulary MUST work in ASCII: `>` focus, `[x]`/`[ ]` multi-selection, and the option under `>` as the single selection. The focused form field carries a vertical bar on its left (`┃`, ASCII `|`). Execution steps mark done with `✓` (ASCII `x`), the current step with a spinner (ASCII `*`; an animated spinner MAY supplement it where supported and disabled on request) and pending steps with no marker. Results state what happened in a sentence colored by its semantic role. Unicode glyphs MUST NOT replace labels or require Nerd Fonts. |

### 2.2 Hierarchy, Spacing, and Focus

| ID | Requirement |
| --- | --- |
| PR-UX-7 | The entry screen MUST identify `Takt AI` through the canonical logo per PR-UX-2A; flow headers show the `Takt AI` signature on the left and the current step on the right in sentence case, without repeating the logo or the step inside the body. The body leads with the decision or actual state. No key bar, health, version, or movement hints are rendered. |
| PR-UX-8 | Use one column between closely associated items, two between horizontal groups, and one blank row between vertical groups. Lateral margins are three columns when they fit and two at reduced width. Labels/values MAY wrap or stack; descriptions MUST NOT be forced onto a single clipped line. Risks and recovery remain available in full. |
| PR-UX-9 | Only the active region MUST show focus: `>` beside the focused option, the vertical bar on the left of the focused form field, and a filled background on the focused action button. Focus uses `focus.ring`; a focused checked row keeps its `[x]` and color, with `>` in the neutral gutter. Do not highlight the entire outer frame as a substitute for local focus. The terminal text cursor is not a focus marker: hiding or showing it MUST NOT remove `>` or the field bar. |
| PR-UX-10 | Interactive states MUST distinguish focus, selection, pending work, success, warning, failure, and relevant unavailability. Omit tasks that do not apply to the environment; keep a relevant unavailable control discoverable with a textual reason and route to resolve it. Disabled controls never activate; unavailable does not mean invisible by default. A validation error appears below its field, prefixed with `*`, in danger colors. |

### 2.3 Keyboard Interaction

| ID | Requirement |
| --- | --- |
| PR-UX-11 | `Enter` MUST open a navigation destination, submit the focused selection field, open nested detail, or activate the focused action button. Merely moving focus MUST NOT advance or apply. In a single selector the option under the cursor is the value submitted with `Enter`. Multi-select uses `Space` to toggle; `Enter` submits the field and advances without applying. |
| PR-UX-12 | `Esc` MUST return one context or cancel the current edit scope, never silently exit with pending changes. Nested detail returns with parent choices and position intact. Leaving an unapplied scope offers `Keep editing` or `Discard changes`; leaving without changes needs no confirmation. During mutation, navigation is unavailable but interruption follows PR-UX-25. |
| PR-UX-13 | Arrows MUST move within the active list; `Tab`/`Shift+Tab` move between fields or regions in reading order. List navigation MAY retain end wrapping if consistent. Optional `j`/`k` aliases MUST NOT intercept printable input in a text field. Arrow navigation remains discoverable without aliases. |
| PR-UX-14 | `Space` MUST toggle multi-selection independently of `Enter` activation. Modified `Enter` is not required. Printable keys, including `q`, `j`, `k`, `?`, and space, MUST enter text when a text field owns focus rather than invoke global shortcuts. Pasted input MUST enter as text under the same condition. |
| PR-UX-15 | Selection fields (setup, components, existing files) have no `Continue` button: `Enter` submits and advances without applying. Review, result and blocked screens MUST expose labeled action buttons below the panel: `Install` commits first installation, `Apply changes` commits an edit, `Personalize` returns to the draft, and result actions name their destination. Review screens MUST have visible actions, not only `Enter`. A single-value nested picker may return directly to its draft parent after explicit selection; that is not installation authorization. |
| PR-UX-29 | The menu MUST end with `Quit`; `q` MAY alias it there. `Ctrl+B`, if retained, requests return to the menu through the same pending-change guard as other exits. Before mutation `Ctrl+C` requests safe cancellation/exit, offering keep/discard when edits would be lost; during mutation it requests cooperative interruption, never immediate exit. `PgUp`/`PgDn` scroll overflowing regions and keep focus visible; an overflowing region shows its position. |

### 2.4 Components and Nested Selection

| ID | Requirement |
| --- | --- |
| PR-UX-16 | Checklists MUST show `[x]`/`[ ]` independent of focus; checked labels use `success.fg`. Each option shows its name and, when useful, a muted description on the line below. A single selector has no separate value marker: the option under `>` is the value. A screen holding several single selectors (one per file) keeps each one's chosen value visible with the selection pair and a `•` marker, apart from the `>` cursor; `Enter` records the focused file's choice and moves to the next file, and after the last one advances unless a kept choice cannot work, whose reason stays shown. Overflowing lists keep focus visible and show position `n / total`; filtering preserves the chosen value and provides a distinct no-results state. |
| PR-UX-17 | Review MUST expose the flow-required scope and consequences, separating additions, modifications, preserved content, and removals while omitting empty categories. Destructive commitment MUST name affected objects and consequences before the explicit action, not add a second confirmation automatically. Execution shows a progress bar and the list of steps with done, current and pending state, with percentages only for real denominators. Results distinguish success, partial, failure and functional readiness, with real recovery actions and their limits. |
| PR-UX-28 | A nested selection (for example a model and effort for one specialist) MUST identify that object and keep parent draft state. `Esc` returns without losing parent selection, filter, or cursor; completing detail makes the new value visible in the parent. Unrelated assignments MUST NOT be rebuilt or applied by this edit. |

### 2.5 Capability Selection Clarity

| ID | Requirement |
| --- | --- |
| PR-UX-20 | Custom setup MUST group capabilities by user-facing purpose and place foundational options such as memory early when pertinent. Visual prominence MUST NOT imply an optional capability is mandatory; optional depth MUST NOT be framed as a separate expert product. |
| PR-UX-21 | Capability labels and reconciliation feedback MUST explain selection effects without requiring knowledge of technical dependency graphs. Reflect the resulting draft selection and briefly identify affected capabilities and reasons; no dependency change silently reinstates an explicit exclusion. |

### 2.6 Drift Visibility

| ID | Requirement |
| --- | --- |
| PR-UX-22 | Relevant external modification MUST carry a discreet textual indication, not color alone. Takt-managed choices are not drift. Drift unrelated to the operation MUST NOT interrupt it. |
| PR-UX-23 | A conflict view MUST explain source, affected scope and known versus uncertain compatibility before offering feasible retention/restoration choices with neutral decision hierarchy. Known deterministic incompatibility blocks the invalid composition and explains an alternative; unusual preference alone does not. A full diff is secondary, but decisive risk stays visible. |
| PR-UX-27 | Under `Capabilities`, list each capability assessed by PR-SET-32 once, by its user-facing name (memory, code navigation, library docs, orchestrator, default model, skills, plugins, reload), never by its check identifier. A marker that reads without color states the result: `✓` verified (success), `✗` not working (danger, a failed check), `?` not checked (warning, inability to assess); only the last two add their textual state and explanation. One verdict line precedes the list and is never repeated: not ready when a capability failed, unconfirmed when checks could not run. When every check passed, one line states what was not tested. Lack of evidence MUST NOT be presented as either verified success or a failed check. |
| PR-UX-24 | Results MUST distinguish changes applied from functional readiness. Completed changes with acknowledged uncertainty show operation success and a separate functional warning, not unqualified readiness or an invented execution failure. State when changes take effect without promising a live-session reload that is not supported. |

### 2.7 Cancellation Feedback

| ID | Requirement |
| --- | --- |
| PR-UX-25 | `Ctrl+C` during execution MUST request cooperative interruption. Show `Cancellation requested` and any phase that must finish until a typed result confirms stopped, partial, or already complete. A pending request is not a cancelled result. Offer retention and only supported recovery with scope and complete/partial/unavailable status; never imply automatic undo or promise a missing rollback. |

## 3. Native token and capability mapping

This table maps terminal roles to the shared semantic token names; it does not define a second light/dark palette.

| Terminal use | Token(s) | Non-color equivalent |
| --- | --- | --- |
| Painted canvas and panels | `#0d0d0d` for the terminal canvas (neutral black, the TUI's native canvas — darker than `bg.canvas`); `bg.surface` (`N900`) inside panels; `border.control` for the panel border | Untitled panels; no shadows |
| Heading/content/help | `text.primary`, `text.secondary`, `text.muted` | Order, bold when supported, explicit labels |
| Home logo | The canonical artwork's own flat colors on the canvas; logo beside the task list, per PR-UX-2A | Canonical logo per PR-UX-2A; no signature line on the home |
| Focus | `focus.ring` for `>` and the focused field bar | `>` beside the focused option; bar beside the focused field |
| Checked option | `success.fg` label; `text.muted` marker | `[x]`, independent of focus; single selection is the option under `>` |
| Focused action button | `bg.canvas` text on `focus.ring` fill | Concrete verb/object label; fill marks focus |
| Other action buttons | `text.secondary`, no fill; only the focused button carries a fill | Concrete verb/object label |
| Disabled control | `disabled.bg` / `.fg` | `Unavailable: <reason>`; no activation |
| Information and result state | `info`, `success`, `warning`, `danger` matching `.bg` / `.fg` pairs | Textual state and explanation |
| Destructive action | `action.danger` background/foreground pair | Destructive verb, scope, consequence |
| Nonfunctional separator | `border.subtle` | Optional; never sole control boundary |

Hover and pointer behavior are not required in this keyboard profile. If added, use the shared hover tokens without conflating hover, focus and selection. Tokens such as web link hover have no invented terminal equivalent when no corresponding control exists.

### Capability selection policy

1. A non-TTY or redirected invocation uses the non-interactive linear CLI contract (PR-INS-42–44), never an alternate-screen renderer. It fails before mutation if required decisions cannot be supplied through that supported interface.
2. An explicit user choice of linear output takes priority over interactive rendering. That route must be documented; this profile does not invent command flags. An undocumented route is not an available recovery claim.
3. In an interactive TTY the TUI renders its single dark theme. Respect `NO_COLOR` for no-color rendering and provide an explicit monochrome choice.
4. The dark theme requires true-color capability and paints the owned backgrounds with the full theme. Capability hints such as `TERM`/`COLORTERM` are fallible; the controller takes the color profile the terminal reports through the event loop instead of certifying contrast from hints, and `NO_COLOR` (any non-empty value) and `TERM=dumb` select the monochrome mode over any reported profile. The terminal background is not queried and never selects a theme or palette: the single dark theme stands regardless. Record the reported configuration in tests, fixing color profile and window size with explicit program options.
5. Limited-color mode preserves roles through a tested mapping on declared terminals. Unverified mappings fall back to monochrome/native default text, not approximate colors declared compliant by distance. User-controlled colors require actual environment validation for a contrast claim.
   Declared matrix: the color profile reported by the event loop selects the mode. True color renders the full theme (iTerm2, Ghostty, Kitty, WezTerm). 256-color renders a declared token-to-xterm-256 table whose every §5.4 pair is recomputed on the mapped values (macOS Terminal.app). 16-color and below render monochrome.
6. ASCII and nonanimated textual progress must remain available and is the baseline: terminal-native progress indicators and animated spinners MAY supplement it when the terminal supports them, with a documented animation-free mode. No-color mode must preserve hierarchy, focus, selection, warnings, and recovery. Linear output never emits spinners, cursor movement, or ANSI styling; it does not claim interactive tasks requiring TUI-only risk decisions are automatically accessible.

## 4. Task composition and semantic prototypes

These examples illustrate the required design. Brackets denote illustrative content. Boxes denote untitled panels. `█ Label █` is the focused, filled button and `░ Label ░` another button. No key bar is shown.

| Task | Pattern and commitment | Return/context |
| --- | --- | --- |
| Choose task | L03; Enter opens destination, no mutation | Preserve menu position |
| First install | Opens on the review of the prepared plan (L05), with the files needing a decision (L07) first when any; `Personalize` opens the component checklist (L04) and Enter returns to review; `Install` → L08 → L09. No setup-choice step | Personalize and return without losing prepared choices |
| Adjust installation or one specialist | L04/L06 → L08 → L09; apply scoped changes once | Preserve unrelated values and filters; no onboarding restart |
| Resolve external modification | L07; human summary, viable choices, then scoped application | Preserve chosen content; do not require repair for unrelated work |
| Restore canonical asset / uninstall | L07 or focused review; identify objects/version and loss before commitment | Restore is not exact undo; show actual recovery availability |
| Upgrade | L08 directly after canonical request unless L07 decision is newly required | Preserve managed choices/exclusions; no redundant confirmation |
| Inspect diagnostics | L06/L09, read-only unless a separate explicit action exists | Return to caller; do not build a permanent crew dashboard |

### Menu and multi-selection

```text
                                     > Configure installation
                                       Assign models
            [Canonical logo]           Check for drift
                                       Uninstall
                                       Diagnostics
                                       Quit

                                       Change components or reapply Takt's files
```

The home has no header row and no signature line: the logo (its terminal representation) alone carries the identity. Logo and menu form one block centered in the screen; the menu block is centered vertically against the logo's ring and stacks below it when that draws a larger logo. Every option has a muted description, but only the one under the cursor is shown, in a single slot below the list whose line and width stay reserved, so moving the cursor never shifts the menu. The logo takes the largest generated variant that fits; below the smallest one the menu stands alone (PR-UX-2A).

### Review and nested edit

```text
  Takt AI                                           Review

  ┌──────────────────────────────────────────────────────┐
  │ Install into ~/.config/opencode                      │
  │ for every project                                    │
  │                                                      │
  │ WHAT YOU GET                                         │
  │ [n] agents       1 orchestrator · [n] specialists    │
  │                  [n] on [provider/model]             │
  │                  [n] on OpenCode's default model     │
  │ [n] skills                                           │
  │ [n] MCP servers  codegraph · context7 · engram       │
  │ [n] integrations DAG panel · memory · sandbox · VFS  │
  │                                                      │
  │ WHAT CHANGES ON DISK                                 │
  │ [n] new files · [n] updated                          │
  │ opencode.json    merged — your settings are kept     │
  │ Kept             [path (your version is kept)]       │
  └──────────────────────────────────────────────────────┘

               ░ Personalize ░  █ Install █
```

Review states what the install sets up in the user's terms, never as file categories or internal tier labels. A title names the destination, then two groups follow: what the install sets up and what it changes on disk. In each group the fact leads (a count aligned on its last digit, or a file name) and its qualifier follows muted in one aligned column; empty groups, rows and zero counts are omitted. Agents are counted by role and grouped by the model they run on. A configuration file the user owned before Takt is merged and says so; one Takt owns is updated.

```text
  Takt AI                                           Agents

  ┌──────────────────────────────────────────────────────┐
  │   All agents          [shared model | different models]
  │   takt                [provider/model]               │
  │ > analyst             [provider/model]               │
  │   dev               • [newly chosen provider/model]  │
  │   judge-a             inherits OpenCode default      │
  └──────────────────────────────────────────────────────┘

                   ░ Apply changes ░
```

`All agents` opens the same model picker and records its choice for every agent as pending; individual rows stay editable before applying. `•` marks agents whose model changes when applied. The table keeps one width whatever row is visible. Choosing a model returns to this draft; `Apply changes` commits every pending assignment in one change. When search owns focus, printable keys write instead of acting as shortcuts.

### Execution

```text
  Takt AI                                       Installing

  ┌──────────────────────────────────────────────────────┐
  │ ✓ Checking OpenCode connection                       │
  │ ✓ Preparing managed files                            │
  │ ⠋ Applying installation files                        │
  │   ████████████████████░░░░░░░░░░  67%                │
  │   [current file]                                     │
  └──────────────────────────────────────────────────────┘

                       █ Cancel █
```

Finished phases carry `✓`; the current phase carries the activity marker and, when its total is known, a bar with the real percentage and the file in progress. Unknown future phases are not invented. `Cancel` is the action while work runs; once requested it stays visible but unavailable, saying the current phase must finish.

### Conflict and partial result

```text
  Takt AI                                   Existing files

  ┌──────────────────────────────────────────────────────┐
   │ [asset path]                                       │
   │ Source: [human summary]                            │
   │ Affects: [scope]                                   │
   │ [Known consequence / explicit uncertainty]         │
   │ ┃ > Keep my version                                │
   │   Restore Takt version                             │
  └──────────────────────────────────────────────────────┘
```

Selection of a conflict choice updates the plan; if that row is itself the commitment action, its full scope and consequence must already be displayed. Do not add a second confirmation merely to repeat an acceptance.

```text
  Takt AI                                           Result

  ┌──────────────────────────────────────────────────────┐
  │  [Partial: completed work; work not applied.]        │
  │  [Current state, recovery and its limits]            │
  └──────────────────────────────────────────────────────┘

       █ [Available next action] █  ░ Back to menu ░  ░ Quit ░
```

```text
  ┌──────────────────────────────────────────────────────┐
  │ Success: Takt AI is installed in OpenCode.           │
  │ Warning: Some checks could not run, so those         │
  │ capabilities are unconfirmed, not failed.            │
  │                                                      │
  │ Capabilities                                         │
  │ ✓ Memory                                             │
  │ ✗ Code navigation — not working                      │
  │   [why]                                              │
  │ ? Skills — not checked                               │
  │   [why]                                              │
  └──────────────────────────────────────────────────────┘
```

The verdict line appears once, above the capabilities (PR-UX-27). The take-effect sentence appears only when OpenCode was not reloaded. Diagnostics shows the same block and adds `Repair`, which opens the configure flow, when a capability failed its check.

Result text follows what happened → what changed → what can be done. Do not call `Restore` an undo, hide partial work behind an error, or show a rollback action without its implementation guarantee.

## 5. Component state contract

N/A states below must not be fabricated to fill a catalog.

| Component/view | Required states and behavior | Not applicable in baseline |
| --- | --- | --- |
| Navigation list | Idle, focus, relevant unavailable with reason; long list scroll/position; no destinations explains why and provides exit | Persistent value selection, hover |
| Single/multiple selector | Checked options distinct from focus; single selection is the focused option; unavailable with reason; dirty draft; loading catalog; empty catalog; no filter results; load failure with retry if supported | Hover |
| Field/search | Label, value, focus, dirty, validation error tied to field; pending asynchronous validation if used; preserve text on error | Checked state, hover |
| Action | Idle, focus, activation feedback, pending, unavailable with reason; duplicate activation prevented while executing | Checked state, hover |
| Review/conflict | Draft changes by category; nothing to change; unknown origin/reference; uncertain vs known-incompatible outcomes; detail accessible; keep/discard on leaving drafts | Percentage, decorative loading |
| Progress | Progress bar and step list with done, current and pending steps; pending; cancellation requested; stable stop or actual completion; readable nonanimated equivalent; terminal-native indicator only as a supplement where available | Selection and hover; percentage without denominator |
| Result/diagnostic | Success, partial, failed, unavailable verification; actual recovery; diagnostic loading/error/no entries where applicable; no secret values in default output | Automatic timed dismissal |

Unavailable references never silently substitute a newer canonical version. Loading does not erase choices, and background updates never steal focus. Long paths/identifiers wrap or have a reachable full-value detail; decisive differences cannot be hidden behind truncation. Diagnostic disclosure must state what is shown or copied and must not promise redaction that is not performed.

## 6. Verification and release evidence

For every test record build/revision, OS/architecture, terminal/version, font/glyph mode, capability hints/overrides, theme, dimensions, input sequence, observed state, and evidence location. Render captures are committed fixtures with color profile and window size fixed by program options, not ad-hoc dumps. The matrix below declares required validation profiles.

Fixtures live in each package's `testdata/` as golden files, one per screen state, size (120×32, 80×24, 60×20, 160×24) and mode (true color, 256-color, monochrome), and regenerate with `go test ./takt/tui/... -update`. A regenerated fixture is reviewed visually before it is committed.

| Axis | Minimum evidence |
| --- | --- |
| Dimensions | 120×32, 80×24, 60×20; resize during editing/execution; below 60×20 readable size notice and documented alternative, no layout-triggered mutation |
| Layout | Single column at reduced width; list–detail only with the stated thresholds; long names/paths and expanded labels; scroll reaches every risk/action/recovery item |
| Color | Dark theme with true color; queried terminal background/version recorded; declared 256/16-color mappings or verified monochrome fallback; `NO_COLOR`; no ANSI in redirected output |
| Glyph/motion | ASCII without Nerd Fonts; optional Unicode only on tested configurations; progress with animation disabled; animated supplements only with a documented disable |
| Keyboard | Arrows, Space, Enter, Tab/Shift+Tab, Esc, Ctrl+C; text containing shortcut letters; paste entry; dirty exits, busy interruption, repeated interrupt and completion race |
| Accessibility | Keyboard-only task completion; actual terminal/screen-reader combination and linear-output exercise; distinguish accessible task coverage from TUI-only decisions still lacking an accessible route |
| Environment | Required OS/architecture scope of PR-SET-26 in isolated environments; record actual terminal versions. No macOS claim from a Linux container run |

### Acceptance criteria mapping

| Criterion | Required proof |
| --- | --- |
| Pattern match and task self-evidence | Pattern/task map and task observation with intended users: identify object, state, next action without external explanation |
| Token, contrast, and no-color conformity | Token review, palette validation, real-render contrast, monochrome focus/selection/state captures |
| Keyboard and assistive completion | Keyboard and assistive task runs on real terminals; captures in each minimum size and long-text fixtures; verify one shared back message and a controller-owned pending-change guard across Esc/Ctrl+B/Ctrl+C, including dirty exits, repeated interrupt, completion races, and paste entry |
| Confirmation matches uncertainty and consequence | First-install review, scoped custom apply, canonical upgrade without repeated approval, new external-conflict decision; upgrade cases are evidenced, not passed over |
| Managed choices survive upgrade | Before/after managed choices and exclusions on upgrade; required when upgrade ships, not a screenshot test |
| Uncertain risk vs. deterministic incompatibility | Separate feasible uncertainty acceptance from deterministic incompatibility block |
| Partial failure and recovery honesty | Injected failure/partial cancellation; actual state, scoped restore and honest recovery limits |
| Component state coverage | Component fixtures for the applicable states in §5, with justified N/A states |
| Editorial honesty | Editorial review of name, sentence case, restrained signature, honest capability claims and nonblaming recovery |

Run the applicable release scenarios: default install; personalize/return; single-specialist edit; canonical upgrade; managed-choice upgrade; external-change decision; known incompatibility; partial recovery; scoped restoration; reduced/no-color repeat. Upgrade scenarios are first-release evidence under PR-INS-46. Full surface conformity requires every applicable criterion and a documented disposition of gaps, not merely a palette calculation or passing unit tests.

A new flow within the existing patterns supplies its state fixtures and applicable acceptance evidence. Record implementation gaps outside this specification, with scope and supporting evidence; a limitation does not change the acceptance criteria.
