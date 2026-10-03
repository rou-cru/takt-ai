# Takt AI — Brand Identity and Design Foundations

**Actual implementation progress: 32%** — assessment of 14 numbered blocks (the brand does not use requirement IDs): 1 complete block, 7 partial, and 6 not integrated. The dark semantic palette reaches `takt/tui/theme/` and the logo has TUI generation/assets; some tone and interaction rules appear in copy and components. Web, documentation, and other applications have no verified surfaces in this repository, and several accessibility, layout, typography, and state contracts are documentary guidance, not implementation/acceptance evidence.

**Version:** 3.0 · **Reference language:** English

**Nature:** normative identity and design specification. It defines the target that implementations must conform to.

> **Precision without friction.**
>
> Takt lets you focus on the work, not on operating the tool. Its identity is that of a precise, sober, and reliable engineering tool: it resolves the routine, lets you control the particular, and helps you recover when the result is not useful.

**Takt MUST be recognizable and have its own character.** Logo, composition, and color are part of the TUI's visual contract; they require no added text foreign to the task.

## 0. How to use this document

This document is the common source for branding, the product website, documentation, graphical interfaces, terminal, and product communications. It is not a moodboard or a collection of screens. It establishes reusable decisions, their reasons, and how to verify them.

### 0.1 Authority and scope

- **MUST / MUST NOT:** requirement. Failure to comply prevents declaring conformity with this identity.
- **SHOULD:** default criterion. An exception needs a documented reason and evidence that it preserves the purpose.
- **MAY:** permitted alternative, neither a requirement nor an authorization to add unnecessary complexity.
- Numeric values in tables are the normative base, unless identified as indicative.
- Message examples illustrate design contracts; they do not fix definitive command names.

Authority order:

1. This identity and its common requirements.
2. A surface specification, which applies or extends those requirements.
3. Implementation components, patterns, and tokens.
4. Concrete screens and flows.

**Everything concerning TUI design in `product/` MUST be an application or extension of this document.** It may not introduce another identity, reassign chromatic meanings, or contradict its interaction contracts. In case of conflict over shared identity and design contracts, this document prevails; local presentation is governed by the relevant VDS (`INDEX.md` rules 6–7).

The file is portable. When moved, dependent specifications must update their reference and keep the identity version they implement. Its authority does not depend on being at the root of this repository.

### 0.2 Quick path by task

| Need | Sections to consult |
|---|---|
| Explain what Takt is | 1–3: strategy, character, and voice |
| Create a brand piece | 3–6 and 10: verbal expression, signature, color, typography, and images |
| Design a page or screen | 5–9: tokens, composition, behavior, and components |
| Add a known menu or flow | 7–9 and 13: pick a pattern, compose, and verify; do not reinvent |
| Adapt to web, documentation, or TUI | 11–12: accessibility and surface contracts |
| Review an implementation | 13–14: governance, criteria, and reproducible validation |

## 1. Product and brand strategy

### 1.1 What Takt AI is

Takt AI is a **meta harness**: it mounts on top of OpenCode v2 and the inference the user chooses. It contributes a team of specialized agents, a proactive orchestrator, and complementary capabilities such as persistent memory, documentation access, project navigation, skills, and deterministic reaction to events.

Its purpose is to produce a relatively consistent working experience across compatible models in OpenCode v2, without erasing their useful differences or promising identical results across models.

### 1.2 Who it exists for

For power users and engineers who know the value of a good tool and want results with agents without continuously administering the complexity around them.

They are not assumed to know Takt's implementation, its internal conventions, or its module names. Technical experience does not equal prior knowledge of the product or willingness to tolerate friction.

### 1.3 Positioning

Takt offers a complete default configuration and lets the user adjust the tools it comprises.

### 1.4 Promise and evidence

**Central promise:** "Your attention, on the work."

The promise MUST be sustained with adequate initial values, preservation of decisions, understandable consequences, pertinent information, accessible recovery, and explicit limits. Claims such as "never fails," "100% safe," or "works with any model" must not be used without a scope and evidence that truly sustain them.

## 2. Character and operating principles

**Personality: calm competence.** Professional without intimidating; accessible without becoming superficial; human without theatricality.

| ID | Principle | Design consequence | How to recognize a deviation |
|---|---|---|---|
| B01 | Precision without friction | Every action identifies its object, scope, and result. | Vague labels, steps with no real decision. |
| B02 | Power without clipping | The common is resolved; the particular stays available. | "Simple mode" that blocks capabilities. |
| B03 | Consistency without forced uniformity | A concept keeps its name and behavior across surfaces. | Switching Takt surfaces forces relearning the common. |
| B04 | Control without micromanagement | Automate the known; ask about new uncertainty. | Repeated confirmations or silent impactful decisions. |
| B05 | Present identity, clear operation | Keep the logo and visual character; adjust their hierarchy to the task. | Both an anonymous experience and branding that hides actions, hinders reading, or imposes waits. |
| B06 | Recovery without reproach | Explain state and exit before attributing causes. | "We warned you," blame, or dead ends. |
| B07 | Durable intention | An explicit decision does not disappear because of an upgrade. | Reinstalling something the user had excluded. |
| B08 | Functional honesty | Distinguish viable, uncertain, and unfeasible. | Offering a known-feasible configuration as impossible. |

Visual simplification does not justify erasing identity, hiding necessary information, reducing contrast, omitting labels, or turning a direct flow into a mysterious gesture. Visual presence is not excess by itself: restrictions must answer concrete interferences with reading, navigation, or execution.

## 3. Verbal identity

### 3.1 Name, descriptor, and messages

- Formal name: **Takt AI**. Short form: **Takt**, after establishing context.
- Do not alternate with "TAKT," "Takt.ai," or "TaktAI" in prose. Technical identifiers keep their real spelling.
- Technical descriptor: **Meta harness for agent teams**.
- First-contact explanation: "Takt coordinates specialized agents and working capabilities on top of OpenCode v2 and the models you choose."
- Positioning line: **Precision without friction.**
- Benefit line: **Your attention, on the work.**

The two lines must not be repeated together on every surface. They are communication resources, not mandatory product headers. Their use implies no commercial exclusivity or trademark registration.

In the TUI, no slogans, welcomes, descriptors, or closing messages will be added just to reinforce identity. The brand is expressed through the logo and the visual system; every additional text must contribute information useful to the task. This does not prevent identifying Takt AI where needed to understand context.

### 3.2 Voice and tone

The voice is direct, precise, considerate, and calm. Tone changes with the situation, not the personality.

| Context | Tone | Example |
|---|---|---|
| Presentation | Clear, confident, demonstrable | "A team of agents on top of the tools you choose." |
| Configuration | Brief and decision-oriented | "We will use these models. You can change any of them." |
| Warning | Concrete, proportional | "This file has external changes. Keeping them may affect compatibility." |
| Error | Calm and resolutive | "The change could not be applied. Check the connection and try again." |
| Recovery | Useful, without judgment | "You can keep your version or restore Takt's." |
| Success | Sober, verifiable | "Configuration applied. It will be used in your next session." |

A message MUST match what the system knows and guarantees. "Your changes are saved" requires them to actually be saved; "nothing changed" requires verifying it. If the state is partial or unknown, say so.

### 3.3 Microcopy rules

1. Use concrete verbs and objects: "Apply changes," "Restore agent," "Keep my version." Avoid "OK," "Process," or "Continue" when they hide an important consequence.
2. Reserve "Continue" for advancing without applying changes. "Save" means persisting; "Apply" means making the configuration effective in the stated scope.
3. Use sentence case. Do not use sustained capitals, routine exclamations, emojis, or celebratory language for ordinary tasks.
4. One main sentence per message; technical details in a second layer. Brevity must not remove the affected object, the risk, or the recovery.
5. Errors: **what happened → what changed or did not change → what you can do**. Cause and diagnostics expandable when extensive.
6. Do not blame the user. Even if they ignored a warning, the next interaction starts by helping.
7. Do not hide a critical consequence in a tooltip, truncated text, external link, or closed panel.
8. Do not call a feature "smart," "magical," or "revolutionary" as a substitute for explaining its use.

### 3.4 Vocabulary and localization

`product/GLOSSARY.md` is the single source of truth for shared product vocabulary. This document does not redefine those terms; it specifies only how each surface presents them:

Each surface MUST declare the languages it supports. Do not mix languages in labels of the same experience except proper names, code, and technical terms with no useful translation. Documentation may introduce "restore (`sync`)" to connect human language and command, without inventing syntax. Translations preserve intent, not literal length; layout admits text expansion.

## 4. Signature and recognition

### 4.1 Logo and wordmark

The canonical asset indicated by product is `docs/assets/brand/takt-ai.png`. It MUST be preserved; `takt-ai-logo-source.png` is not its substitute. It must not be silently replaced by running text or treated as a future isotype. Changing it requires an explicit identity decision, with evaluation of recognition and of the affected surfaces.

**The logo MUST be visible and prominent on the TUI home. It is not mandatory to repeat it on every screen.** In selection, editing, review, execution, and recovery it may be omitted to use the workspace. Continuity is kept through the shell, composition, and shared tokens. Compact or monochrome logo variants must keep recognition; adapting the home does not authorize replacing it with running text.

The **Takt AI** wordmark identifies the product where appropriate, including an operating header without a logo; it does not replace the logo on the home. Its graphic reproduction follows these rules:

- IBM Plex Sans: "Takt" at weight 600 and "AI" at weight 400, at the same size and on the same baseline.
- Natural letter spacing of the font; one normal space between words. Do not condense, skew, or alter proportions.
- Single color: `text.primary` or `brand.ink`. On photographic backgrounds, first use an opaque and legible surface.
- Minimum clear space around: the height of the "T" in the signature. Do not include this margin inside a foreign button or control.
- Minimum graphic signature size on screen: 20 CSS px of font; in print, 12 pt. If it does not fit, use the name as normal text of the context, not an illegible shrunken graphic.
- In terminal, the textual signature uses the native font. The logo may span several rows through its terminal representation, including ASCII art; no font will be imposed on the user.

Graphic assets, the implemented site, and the component library are out of scope of this specification. This definition does not create a vector file. The surface profile identifies the canonical asset and the variants it uses, with visual evidence. Distinct symbols must not be improvised per surface.

### 4.2 Distinctive elements

Recognition comes from the logo, the petrol-blue family, mineral neutrals, and a consistent composition of header, separators, and selection. Clear typography and precise communication reinforce the whole, but do not replace the distinctive assets. In terminal, where font and color may vary, the home logo and the shared structure MUST sustain recognition, without requiring the symbol on every view.

Identity is continuous, not logo repetition. On the wide home, logo and actions form a balanced horizontal composition centered as a whole. In operating tasks, panels, selection, alignments, and a compact header identify the tool; no functionless graphic band is reserved. Do not repeat logos inside each panel, dialog, or notice.

Brand expression is not reserved to the commercial web. Each surface MUST document a recognizable composition and its adaptations, not limit itself to declaring it is "clean and sober." No welcome screens, additional steps, or waits are required to display identity.

## 5. Chromatic system

### 5.1 Foundation

The palette has a **petrol-blue brand family**, a **mineral neutral family**, and three functional families: success, warning, and error. Information uses petrol, not a sixth chromatic personality.

Petrol is chosen for its restrained character and visual distance from technological neon. Slightly cool neutrals keep kinship without turning every surface blue. Green, amber, and red provide functional discrimination; they are not presented as universal laws of color psychology.

Levels 50–950 order each family from light to dark. They are canonical sRGB values, not a promise of perceptually identical intervals. Their relative luminance strictly decreases. Components consume **semantic tokens**, never pick a tone that "looks good" on their own.

### 5.2 Complete primitive palette

| Level | Mineral `N` | Petrol `P` | Success `G` | Warning `A` | Error `R` |
|---|---|---|---|---|---|
| 50 | `#F5F7F7` | `#EFF8F9` | `#ECFDF5` | `#FFFBEB` | `#FEF2F2` |
| 100 | `#E8EDED` | `#D9EEF0` | `#D1FAE5` | `#FEF3C7` | `#FEE2E2` |
| 200 | `#D2DCDD` | `#B3DDE1` | `#A7F3D0` | `#FDE68A` | `#FECACA` |
| 300 | `#ADBDC0` | `#80C3CB` | `#6EE7B7` | `#FCD34D` | `#FCA5A5` |
| 400 | `#82979C` | `#4BA3B0` | `#34D399` | `#FBBF24` | `#F87171` |
| 500 | `#61777E` | `#2E8394` | `#10B981` | `#F59E0B` | `#EF4444` |
| 600 | `#485C63` | `#226A7D` | `#059669` | `#D97706` | `#DC2626` |
| 700 | `#35464D` | `#205566` | `#047857` | `#B45309` | `#B91C1C` |
| 800 | `#25343A` | `#204653` | `#065F46` | `#92400E` | `#991B1B` |
| 900 | `#18262C` | `#1E3B46` | `#064E3B` | `#78350F` | `#7F1D1D` |
| 950 | `#101B20` | `#102630` | `#022C22` | `#451A03` | `#450A0A` |

Auxiliaries: `white = #FFFFFF`, `black = #000000`. White is an intended surface and inverse text color; black is reserved for monochrome reproduction or accessibility adaptations, not a replacement for the default mineral background.

### 5.3 Semantic tokens: light and dark themes

`N900` means Mineral level 900. The semantic name is the stable contract; the primitive reference defines this version of the theme.

| Token | Light | Dark |
|---|---|---|
| `bg.canvas` | `N50` | `N950` |
| `bg.surface` | `white` | `N900` |
| `bg.raised` | `white` | `N800` |
| `bg.sunken` | `N100` | `N950` |
| `bg.hover` | `N100` | `N800` |
| `text.primary` | `N900` | `N50` |
| `text.secondary` | `N700` | `N200` |
| `text.muted` | `N600` | `N300` |
| `border.subtle` | `N200` | `N700` |
| `border.control` | `N500` | `N400` |
| `brand.ink` | `P700` | `P300` |
| `link.default` | `P700` | `P300` |
| `link.hover` | `P800` | `P200` |
| `focus.ring` | `P600` | `P300` |
| `action.primary.bg` | `P700` | `P300` |
| `action.primary.hover` | `P600` | `P200` |
| `action.primary.pressed` | `P800` | `P400` |
| `action.primary.fg` | `white` | `P950` |
| `selection.bg` | `P100` | `P900` |
| `selection.fg` | `P900` | `P100` |
| `selection.border` | `P600` | `P300` |
| `disabled.bg` | `N100` | `N800` |
| `disabled.fg` | `N600` | `N300` |
| `info.bg` | `P50` | `P950` |
| `info.fg` | `P700` | `P300` |
| `success.bg` | `G50` | `G950` |
| `success.fg` | `G800` | `G300` |
| `warning.bg` | `A50` | `A950` |
| `warning.fg` | `A800` | `A300` |
| `danger.bg` | `R50` | `R950` |
| `danger.fg` | `R700` | `R300` |
| `action.danger.bg` | `R700` | `R300` |
| `action.danger.hover` | `R600` | `R200` |
| `action.danger.pressed` | `R800` | `R400` |
| `action.danger.fg` | `white` | `R950` |

Application rules:

- `border.subtle` only separates content already distinguishable by grouping. It does not by itself identify a field, focus, selection, or state.
- `border.control` identifies necessary control boundaries on neutral backgrounds. Purely decorative separators need not mimic controls.
- `text.muted` remains legible; it does not mean reduced opacity. Do not use `N500` as small light text on `N100`, nor `N400` as dark text on `N800`.
- Semantic surfaces use their own `*.fg`; do not place `text.muted` or another color on top without verifying the pair.
- Secondary buttons use `text.primary`, their container background, and `border.control`; hover `bg.hover`. Tertiary buttons use text or link without prominent fill.
- Focus, selection, and hover are different states. Focus adds an outline; selection persists with a marker and background; hover is transient and implies no selection.
- The web focus outline is 2 px with a 2 px gap from the control. The gap exposes the neutral background; it is not evaluated against the button fill alone.
- A disabled action keeps an accessible explanation. Do not use global opacity or faint color as the only signal of unavailability.
- Brand color may identify the logo, header, and structural regions of the product, not only commercial presentations. Its extent answers to composition and does not hide states or content. A validated text pair is required, e.g. `P100` on `P900`.
- Accent identifies brand as well as intent and orientation; it needs to represent no action to have function. Its structural use must be consistent and distinct from focus and selection. Green is not the general CTA color; red does not mean "an option Takt advises against."
- In the dark TUI, the signature uses Petrol `P400`, only on the terminal canvas (6.63:1); `brand.ink` keeps its shared value because `P400` falls below 4.5:1 on `N800`. `P300` is reserved for `focus.ring` so brand is not confused with focus. Single dark theme; no light variant exists.

### 5.4 Verified contrast and limits

The following values were computed with sRGB relative luminance; they are shown rounded to two decimals, but approval uses the unrounded value.

| Foreground / background | Ratio | Use |
|---|---|---|
| `N900` / `N50` | 14.44:1 | Primary light text |
| `N600` / `N100` | 5.95:1 | Muted text on the most demanding light neutral background |
| `N50` / `N800` | 11.97:1 | Primary text on raised dark surface |
| `P700` / `N50` | 7.65:1 | Link or signature on light |
| `P300` / `N800` | 6.49:1 | Link or signature on raised dark surface |
| `P400` / TUI canvas `#0D0D0D` | 6.63:1 | TUI signature |
| `white` / `P600` | 6.13:1 | Light primary action on hover |
| `P950` / `P400` | 5.34:1 | Dark primary action pressed |
| `P900` / `P100` | 9.86:1 | Light selection; the inverse pair serves on dark |
| `G800` / `G50` | 7.29:1 | Light success |
| `G300` / `G950` | 9.94:1 | Dark success |
| `A800` / `A50` | 6.84:1 | Light warning |
| `A300` / `A950` | 10.39:1 | Dark warning |
| `R700` / `R50` | 5.91:1 | Light error |
| `R300` / `R950` | 8.51:1 | Dark error |

The internal target is **4.5:1 for all informative text**, without using the large-text exception to weaken headlines. WCAG 2.2 AA sets 4.5:1 for normal text and 3:1 for large text, with specific exceptions. [W3C reference: text contrast](https://www.w3.org/WAI/WCAG22/Understanding/contrast-minimum.html).

Necessary control boundaries and graphic indicators must reach at least 3:1 against the pertinent adjacent colors. Color does not replace labels, shapes, or markers. [W3C reference: non-text contrast](https://www.w3.org/WAI/WCAG22/Understanding/non-text-contrast.html).

These tests do not certify a real screen or terminal: transparencies, composite states, native selection, and external themes can change the result. No transparencies will be added to text, controls, or semantic backgrounds without recomputing their combinations.

### 5.5 Themes, print, and degradation

- Light and dark are equivalent brand expressions. On web, follow system preference and allow persistent choice; do not impose dark for being a technical tool.
- In terminal, respect the environment's capabilities and preferences per §12.3. Do not assume an ANSI palette equals these hexadecimals.
- Print: white background, dark text, no unnecessary background blocks. States keep words or symbols and remain distinguishable in grays.
- The brand MUST remain recognizable without color, keeping the logo on the home and composition in every context. Removing it must not prevent knowing focus, state, action, or hierarchy either.

## 6. Typography, shape, and rhythm

### 6.1 Type family

**IBM Plex Sans** for brand, navigation, titles, and reading. **IBM Plex Mono** for code, commands, paths, identifiers, and values where alignment matters. Sharing a family brings coherence without turning the whole experience into code. Families and their distribution are consulted in the [official IBM Plex repository](https://github.com/IBM/plex).

Web stacks:

```css
--font-sans: "IBM Plex Sans", system-ui, -apple-system, "Segoe UI", sans-serif;
--font-mono: "IBM Plex Mono", ui-monospace, "SFMono-Regular", Consolas, monospace;
```

Self-host the needed files, keep their license, and load only used weights/languages. Do not turn a remote font into a requirement for reading or using the product. The fallback must keep a functional layout. In TUI the user's font rules; these stacks are not imposed.

### 6.2 Web type scale

Reference values with a 16 px root. Implemented in `rem`; user zoom and preferences are not blocked.

| Token | Size / line height | Weight | Use |
|---|---|---|---|
| `type.caption` | 0.75rem / 1rem | 400 | Complementary metadata, never necessary instructions |
| `type.small` | 0.875rem / 1.25rem | 400 | Contextual help, compact table, code |
| `type.body` | 1rem / 1.5rem | 400 | Reading and standard controls |
| `type.lead` | 1.125rem / 1.75rem | 400 | Brief introduction |
| `type.h3` | 1.25rem / 1.75rem | 600 | Subsection |
| `type.h2` | 1.5rem / 2rem | 600 | Section |
| `type.h1` | 2rem / 2.5rem | 600 | Page title |
| `type.display` | 2.5–3.5rem / 1.1 | 600 | Main commercial message, fluid size |

Labels use weight 500; emphasis, 600. Do not add ultralight weights or whole paragraphs in bold. Only `type.display` may use −0.02em tracking; the rest uses natural tracking. Normal text aligned to start, never justified. Centering is limited to brief presentation messages, not forms, documentation, or errors.

Long reading: approximately 60–75 characters per line, container maximum `72ch`. Code keeps monospace font and allows copying the full content. Comparable numbers use tabular figures when the font allows.

### 6.3 Spacing and geometry

| Token | Web value | Base use |
|---|---|---|
| `space.0` | 0 | No separation |
| `space.1` | 4 px | Icon–text, inseparable elements |
| `space.2` | 8 px | Label–field, elements of one control |
| `space.3` | 12 px | Compact rows, related controls |
| `space.4` | 16 px | Compact padding, field separation |
| `space.6` | 24 px | Standard padding, internal groups |
| `space.8` | 32 px | Distinct groups |
| `space.12` | 48 px | Product or document sections |
| `space.16` | 64 px | Commercial sections |
| `space.24` | 96 px | Wide commercial separation |

Separation between groups MUST exceed internal separation. Do not introduce arbitrary distances per screen. Measures may be expressed in `rem` keeping the scale.

- Web radii: `radius.control = 4px`, `radius.panel = 8px`; pill only for compact tags and states, not for all buttons.
- Borders: 1 px; focus: 2 px. Do not nest more than two levels of visible container; prefer headers and space before adding another box.
- Shadows: only for real overlay. Light: `0 8px 24px rgb(16 27 32 / 12%)`; dark: `0 8px 24px rgb(0 0 0 / 24%)`. Accompany with border; do not use them as the only functional boundary.
- No glassmorphism, glow, bevels, metallic textures, or decorative gradients in operating interfaces.
- No pixel value translates literally to terminal cells; §12.3 defines the adaptation.

## 7. Layout grammar

### 7.1 Universal hierarchy

Every operating view MUST answer, in this reading order:

1. **Where I am and what I act on:** title, object, and scope when needed.
2. **What needs attention now:** decision, relevant state, or main content.
3. **What I can do:** pertinent actions, one primary intent per task region.
4. **What I can consult later:** explanation, details, history, or diagnostics.

"One primary intent" does not force highlighting a button where there is no action; a reading page may have none. If two actions have truly equal rank, present a neutral choice, not an arbitrary highlight of Takt's preferred one.

Visual, assistive-reading, and keyboard order MUST coincide. Identity frames location and task; it is not a mandatory vertical region or a prior step. This reading order does not require stacking everything in one column. Brand, global navigation, and aids must not hide decisive content or controls. Composition may center groups, distribute panels, and reserve the logo for the home.

### 7.2 Pattern selection by situation

| ID / situation | Base pattern | What is seen first | What is revealed later | Avoid |
|---|---|---|---|---|
| L01 · Explain the product | Vertical narrative | What it is, who it is for, and concrete benefit | Evidence, how it works, compatibility, and details | Empty hero, card grid with no argument |
| L02 · Read or learn | Reading column | Title, answer, or purpose | Example, steps, limits, and reference | Full-width text, banners inside each section |
| L03 · Choose destination | Navigation list | Task name and indispensable context | Description if it distinguishes options | Metrics dashboard or cards per entry |
| L04 · Configure an object | Grouped form | Current values and pertinent fields | Less frequent settings | Wizard for independent changes |
| L05 · Prepare a dependent set | Staged flow | Current decision and real progress | Final editable summary | Steps that only say "next" |
| L06 · Compare or reassign many items | Table or structured list | Identity and comparable attribute | Detail per item | Cards that prevent comparing columns |
| L07 · Resolve a conflict | Focused comparison | What differs and what decision is needed | Full diff and evidence | Dumping whole files before explaining the conflict |
| L08 · Execute an operation | Focused state | Running action and known phase | Details and consultable logs | Invented percentages, contextless spinner |
| L09 · Recover from a problem | Result and exit | What happened, current state, next action | Technical diagnostics | Exclusively red screen or error without recovery |

A new menu fitting L03 needs no new design: it reuses structure, components, navigation, states, and tokens. Only the options and their context change. If the task differs, pick another existing pattern before inventing one.

### 7.3 Web widths, columns, and adaptation

| Profile | Maximum width | Composition |
|---|---|---|
| Marketing | 1200 px | Grid up to 12 columns; narrative and demonstrations |
| Documentation | Shell 1440 px; article 72ch | Navigation 240 px, flexible article, optional 200 px index |
| Configuration | Form 640 px | One column; side summary only if it helps decide |
| Comparison / administration | 1440 px | Table or list; side detail when useful space exists |
| Brief dialog | 480 px, limited to viewport | One bounded decision, not an app inside another |

Outer margins: 16 px below 640 px, 24 px between 640–1024 px, 32 px from 1024 px. Standard gutter: 24 px; compact: 16 px. No maximum width prevents shrinking the container to its available space.

Base breakpoints: 640, 1024, and 1280 CSS px. Used for composition changes, not for assuming a device. If real content does not fit, collapse earlier with a documented rule; never shrink type to keep columns.

- Below 640 px: one column, collapsible secondary navigation, actions wide enough for their labels.
- Below 1024 px: forms and summaries stack; label–value pairs stay together.
- From 1280 px: documentation may show navigation, article, and index if all three fit without narrowing reading.
- When collapsing: preserve task, actions, and recognizable identity; adapt the logo and move index, navigation, and diagnostics to accessible controls. Do not erase decisive information.
- Tables and code blocks may have their own horizontal scroll when their structure requires it; do not cause whole-page horizontal scroll.
- Fixed bars only if they add continuity; they must reserve space and not hide content or focus.

### 7.4 Density, grouping, and progressive disclosure

Two permitted densities, not two products:

| Density | When | Spacing and reading |
|---|---|---|
| Standard | Forms, first install, reading | Body 16 px; padding 24 px; fields separated 16–24 px |
| Compact | Repeated comparisons and technical listings | Minimum text 14 px; padding 16 px; separation 8–12 px |

Compact density does not reduce touch targets, focus, or necessary aids. Do not alternate densities inside one list without functional reason.

Three information layers:

- **Always visible:** object, current value, selection, relevant consequence, state, and available action.
- **Contextual:** why something is recommended, pertinent limitations, and field help.
- **On demand:** diagnostics, logs, full diff, history, or extensive explanation.

"Personalize" opens depth without replacing the flow with an incompatible one. Prepared values stay present. Do not use labels like "expert mode" to implicitly warn that changing something is improper.

### 7.5 Composition sketches

Sketches show conceptual grouping, not a mandatory vertical layout or definitive text. For terminal, §12.3 and the surface wireframes define horizontal compositions and adaptations.

```text
Object configuration              Set preparation

Shared compact context            Shared compact context
Title + scope                     Title + current stage
Indispensable context             Current decision
                                  Prepared values
Field group                       [Personalize]
Field group
[Additional settings]             Final summary:
                                  added / changed / kept / removed
[Cancel] [Apply changes]          [Back] [Install]
```

Actions sit after content. On LTR web, the final group may align to the right end, secondary before primary; on narrow layouts preserve reading order and priority. A pinned primary button must not hide that unreviewed changes remain.

## 8. Interaction and trust contract

### 8.1 Common model

**Prepare → review when appropriate → apply → report result → allow recovery.**

These are responsibilities, not five mandatory screens. Confirmation is justified by a pending decision or new consequence, not by habit.

| Operation | Preparation and confirmation | What is preserved |
|---|---|---|
| First install | Detect, preselect, allow granularity; show resulting setup and confirm before executing. Habitual target path: 2–3 significant decisions or confirmations, not empty steps. | Explicit choices made during the flow. |
| Later adjustment | Edit the chosen scope and apply once. Report when it takes effect; do not restart the whole onboarding. | Configuration outside the edited scope. |
| Canonical upgrade | The request already authorizes the ordinary upgrade. Do not ask redundant confirmation if no new consequences appear. | Takt-managed configuration and explicit exclusions. |
| Upgrade with external changes | Show relevant differences, scope, and known or uncertain compatibility; ask how to proceed before replacing or preserving under uncertainty. | What the user decides to keep; do not promise compatibility without evidence. |
| Restore / sync | State which objects and which canonical version will be recovered. If it replaces personalizations, show it before commitment. | Everything outside the confirmed scope. |

The first install also presents the product through the logo and composition of the same functional view. That presence is not an "empty step": it adds no confirmations, identity texts, or a screen to pass through before working.

### 8.2 Autonomy and limits

- Detection and recommendation are not consent to widen scope. Choosing defaults does not authorize installs outside what was requested.
- Decisions made through Takt are first-class intent, not conflicts to ask about on every upgrade.
- Under uncertainty, explain what is not known and allow an informed choice if the combination remains viable.
- Under known, decisive incompatibility, do not apply a combination presented as valid. Explain the impediment and the alternatives or necessary requirements.
- "Not recommended" does not equal "forbidden." An unusual preference does not justify blocking the action.
- Do not re-ask an already resolved acceptance inside the same scope, unless new evidence changes the consequence.

### 8.3 Changes, exits, and recovery

- Before the commitment point, canceling must not modify persistent state. If an operation needs preparation with effects, it must declare them first; do not call it a harmless preview.
- Pending changes are identified locally and in the application scope. Exiting with no changes needs no confirmation; exiting with unapplied changes offers continuing to edit or discarding them.
- During execution, "Cancel" appears only if the system knows how to stop with an explainable result. Otherwise report that the phase must finish; do not fake cancellation.
- A partial result is distinguished from total success and failure: what was applied, what was not, and how to continue. Retrying must not hide possible repeated effects.
- Recovery requires explaining scope and consequences. Restoring the canonical is not the same as undoing exactly the last operation; do not swap those labels.
- Never promise backup, rollback, or reversibility outside the scope defined on the surface.
- When an action finishes, return to the pertinent context keeping selection and filters when still valid. Do not always send back to start.

### 8.4 Navigation and transferable learning

- A concept keeps name, relative location, and behavior in all its uses.
- Back returns to the previous level; cancel abandons the current operation; exit abandons the application. Do not confuse them.
- Menus navigate; selectors change values; buttons execute actions. Their appearance and semantics must not mix.
- Shortcuts accelerate visible or findable actions; they are not the only way to discover a capability.
- Navigating to detail preserves the return context. Search, filters, and pagination do not replace an understandable architecture.
- If a surface shows keyboard aids, they are contextual: the pertinent ones, not the full catalog on every screen. The TUI shows no key help bar.

## 9. Reusable component contracts

This section specifies behavior, not a library or framework. Each surface MUST map these contracts to its native controls or accessible equivalents.

| Component | Mandatory anatomy and behavior |
|---|---|
| View header | Single concrete title; scope if ambiguous; description only if it adds information. |
| Navigation list | Recognizable destination, focus marker independent of active state, stable order; description only to distinguish. |
| Field | Persistent label, value, pertinent help, and associated error; placeholder does not replace label. |
| Single selector | Unambiguous current value; comparable alternatives; moving focus neither confirms nor applies automatically. |
| Multiple selection | State of each option, scope of "select all," and explicit restrictions; selection is not confused with focus. |
| Primary action | Verb and object; one predominant goal per region; immediate activation feedback. |
| Destructive action | Visible scope and consequence; danger color only where the consequence is destructive. No type-to-confirm except exceptionally justified risk. |
| Notice | Named state, consequence, and action if needed; do not use for ordinary introductory text. |
| Change summary | Separate added, modified, kept, and removed; omit empty categories; allow returning to edit without losing the rest. |
| Comparison / diff | Identify versions and origin; human summary before detail; changes distinguishable without depending on red/green. |
| Table | Explicit headers, alignment by type, sorting indicated; recoverable truncation; do not hide decisive differences. |
| Progress | Verifiable operation and phase; percentage only with a real denominator; consultable details without stealing focus. |
| Result | Success, partial, or failure; real scope, temporal effect, and next step. No confetti or ephemeral notification as sole evidence. |
| Contextual help | Answers the doubt of the place; does not repeat the label; indispensable information is not locked in a tooltip. |
| Web dialog / overlay | One bounded decision; focus contained and restored on close; unambiguous close. Long flows use their own view. |

### 9.1 Minimum state matrix

Every interactive component specifies: rest, hover where it exists, focus, active/pressed, selected where applicable, disabled with reason, loading, and error. Not all states apply to every component; non-applicable ones are marked, not invented.

Every data view specifies: initial loading, content, initial empty, no filter results, failure, partial result when possible, and ongoing update. An empty state is not an error: it must explain what is missing and how to start, with no mandatory decorative illustrations.

If a new pattern does not define its states, it is not yet complete. No screen may resolve them with local styles foreign to these contracts.

## 10. Icons, images, data, and motion

### 10.1 Icons and images

- Linear web icons on a 24 px grid, nominal 1.5 px stroke; usage sizes 16, 20, or 24 px. Pick a single family per surface.
- An icon represents a recognizable action or category, not space filler. Less obvious actions carry text; an isolated icon needs an accessible name.
- States with icons also include word or textual meaning. Do not assume a triangle or a color is interpreted equally in every environment.
- Avoid emojis as functional iconography: style, width, and rendering depend on the environment.
- Commercial photographs and illustrations show real work, structure, or understandable results. Do not use robots, brains, glowing networks, or hardware photos as a substitute for explaining Takt.
- Captures must be legible, representative, and free of private information. Fictional demonstrations are identified as such. Do not present nonexistent features as available product.

### 10.2 Diagrams and data

Data only when it serves a decision. Do not add activity metrics to make a screen look powerful.

- Diagrams: consistent left-to-right or top-down reading; neutral nodes; petrol for the relevant path; label on ambiguous relations.
- Quantitative comparison: bars before ornaments; visible units, interval, and source; axes not manipulated to exaggerate differences.
- Single series: petrol. Ordered series: petrol ramp with labels and verified contrast. Categories: up to four strokes using `P700`, `N700`, `G800`, `A800` on light, and `P300`, `N300`, `G300`, `A300` on dark, accompanied by direct labels and distinct line patterns or markers.
- Categorical colors are not used in the same region to mean state. If that creates ambiguity, use small multiples or a table. More categories require another pattern, not a new improvised palette.
- In diffs: `+`/`−`, labels, or structure besides color. Keep enough order and context to understand the change.

### 10.3 Motion

| Token | Web duration | Purpose |
|---|---|---|
| `motion.feedback` | 100 ms | Hover or press transition |
| `motion.state` | 160 ms | Local change or help appearance |
| `motion.enter` | 200 ms | Panel or dialog entrance |
| `motion.exit` | 120 ms | Close without unnecessary perceptible delay |

Base curve: `cubic-bezier(0.2, 0, 0, 1)`. Functional response to an action is immediate; it does not wait for an animation to finish to acknowledge it. No bounces, parallax, simulated typing, mandatory scroll animations, or decorative loops.

Respect reduced motion: remove displacements and nonessential animations, keep state text. In terminal, avoid spatial transitions; discreet indicator only while real work exists. Never blinking as an urgency signal.

## 11. Accessibility and perceptual resilience

Accessibility is a quality requirement, not an optional visual topic. **The target for web surfaces is full WCAG 2.2 AA**; this document defines foundations and critical tests, not a substitute for evaluating every applicable criterion.

- Text and controls meet the contrast contracts of §5.4 in all their states.
- Every web operation can be completed with keyboard, with visible focus, logical order, and no traps. Use native semantics before reproducing controls with generic elements.
- Labels, errors, selection, and progress must be understandable with assistive technology. Announce relevant changes without reading every log line or moving focus for background updates.
- Internal target for standalone pointer controls: a 44 × 44 CSS px area, even if the drawing is smaller. That box is not required for links inside prose. WCAG 2.2 AA defines a 24 × 24 CSS px minimum with exceptions; the 44 px rule is a stricter Takt decision, not a quote of the AA minimum. [W3C reference: target size](https://www.w3.org/WAI/WCAG22/Understanding/target-size-minimum.html).
- Test web reflow at 320 CSS px width and 200% text zoom. Necessary two-dimensional structures, like some tables, are evaluated with their exceptions, not by forcing the whole page to overflow. [W3C reference: reflow](https://www.w3.org/WAI/WCAG22/Understanding/reflow.html).
- States are distinguished without color; controls do not depend on hover; important information does not depend on animation nor disappear on a timer before it can be read.
- Test long names, expanded translations, paths, Unicode characters, and absence of special icons. Truncate only if an accessible way exists to obtain the full value.
- Credentials and secrets are not shown in summaries, captures, errors, or logs by default. Copying a diagnostic requires knowing what is copied; do not promise redaction that is not performed.
- Web conformity does not automatically transfer to a TUI. Terminal accessibility requires testing with real environments and readers plus a linear-output alternative when interactive rendering is not accessible.

## 12. Identity applications

### 12.1 Product website

**Main job:** understand Takt, judge whether it fits, and find a real next step.

Base sequence L01: concrete promise → brief explanation → example or evidence → how it fits the user's tools → scope and limits → access to installation or documentation.

One predominant commercial intent per section. Do not repeat the CTA on every card. It may have more space and type scale than product, but no unevidenced claims, fictional testimonials, invented compatibility, third-party logos implying endorsement, or animations delaying reading.

The brand may lead at the entrance; when explaining details, the content leads, without losing product identification. This prominence is not web-exclusive: it also belongs to the TUI entrance. Captures must demonstrate clarity, not be used as illegible texture.

### 12.2 Technical documentation

**Main job:** find an answer and execute it correctly.

L02 pattern with navigation by user tasks and concepts. Each page identifies purpose and, when relevant, version, prerequisites, and scope. Procedures: expected result → steps → verification → recovery or known issues. Reference: exact definition → parameters or structure → examples → limits.

Code is copyable, contains no real secrets, and distinguishes commands from output. Navigation and search complement each other. Side index only for pages needing it; do not narrow the article to show an empty column. Notices are reserved for relevant consequences, not for decorating every paragraph.

### 12.3 TUI and CLI

The TUI installs and adjusts the Takt AI configuration. Its visual identity must accompany both first installation and later use. It is not a permanent swarm monitor or a harness replacement.

#### Brand presence and composition

The main context is a tool on PC/laptop, normally with horizontal space. A wide composition is designed first; adaptation uses the terminal's **real width and height**, not the monitor or an assumption about the device. A half-screen terminal may need another composition.

| Context | Composition |
|---|---|
| Wide home | Canonical logo on one side and action group on the other; balanced whole in the available area, no splash or presentation texts. |
| Narrow home | Logo above actions when they do not fit side by side; centered grouping, without turning the adaptation into the universal layout. |
| Selection and comparison | List and detail/summary in panels when useful space exists; visible selection and focus. When narrow, nested detail with preserved return. |
| Edit and review | Fields, values, and changes grouped spatially; differentiated actions. Do not write the setup as a paragraph report. |
| Execution and result | Execution: progress bar with real percentage and list of finished, running, and pending steps. Result: state sentence with its semantic color and next-action buttons. No logo required, no repeated known context. |
| Conflict and error | Focused comparison or decision; clear consequence and exit. No mandatory logo band. |

Continuity is expressed through shell, panels, selection, accent, and consistent alignments. The operating header shows the `Takt AI` signature on the left and the current step on the right; the body groups into borderless-titled panels, and actions go in a centered button row below the panel. Aligning text inside a control does not require left-aligning the whole screen. Centering a control group does not imply centering its paragraphs. The TUI profile MUST provide wide, medium, and narrow wireframes with cell budgets, actions, and focus state. Do not add texts just to give personality.

#### Visual adaptation

- Use the capabilities of Bubble Tea, Bubbles, and Lip Gloss: horizontal/vertical composition, bordered panels, semantic fills, tables, controls, action bars, and viewports. Do not mimic web shadows or font sizes; nor reduce the interface to a prose list by default.
- Each region has a function: pick, inspect, edit, review, or execute. Distribute space among them; do not add metrics or filler content to occupy width.
- Spacing base: one column between associated items; two or more between groups; one row between vertical groups. The shell may center a width-bounded composition. Avoid both piling everything in one corner and stretching text to fill the screen.
- The home uses a side logo if logo, actions, and separation fit without clipping; its threshold differs from a list–detail one. For lists, from 100 columns list–detail may be used with at least 32 columns of list, 48 of detail, plus margins/separation. With less space, open nested detail preserving focus and choices.
- Validate at minimum 120×32, 80×24, and 60×20, plus a wide low-height terminal. When narrow, adapt controls and paginate/scroll content without hiding actions, risks, or recovery. On the home, keep a recognizable logo variant. Below 60×20, legible notice and documented alternative route without mutating state.
- In operation there is no mandatorily reserved logo space. Use brief context and structured data; no key help bar is shown. Show indispensable risks; leave logs, extensive explanation, and diagnostics on demand. Brevity does not mean hiding consequences.
- Do not require Nerd Fonts, ligatures, or pictograms. ASCII fallback: focus `>`, multiple selection `[x]`/`[ ]`; in single selection the value is the option with `>`. The focused field carries a vertical bar on its left and the focused button goes filled with the focus color. Each option shows its name and, if helpful, a dim description on the next line. Unicode borders, `✓`, and spinner are verifiable enhancements, not dependencies.

#### Chromatic and accessible adaptation

- The TUI has a single theme: dark, with its complete tokens and painted backgrounds (canvas and panels). No light or inherited theme exists.
- With limited colors, map roles to available capabilities and verify the declared terminal matrix. Semantic identity prevails over hexadecimals.
- Support absence of color keeping the recognizable logo on the home and states on every view. Offer accessible linear output with textual Takt AI identification when graphic rendering is not accessible. In non-interactive CLI or redirected output, do not emit animations, decorative art, or control sequences contaminating content.
- The surface profile must define how to detect capabilities and pick alternative output; do not invent "reliable" detection without tests.

#### Interaction adaptation

- Arrows to traverse options, `Enter` to open or activate, `Space` for multiple selection, and `Tab`/`Shift+Tab` to switch region or field are the discoverability base. Additional shortcuts may accelerate, not replace it.
- `Esc` goes back or cancels the current context per its state; with pending changes, §8.3 applies. It does not silently exit the whole application with modifications.
- `Ctrl+C` means interruption: before applying, it cancels safely; during application, it requests interruption and communicates the possible result. No implicit reversion is promised. Detailed semantics and non-interruptible points are documented in the TUI profile.
- In text fields, printable keys write; a global shortcut like `q` cannot close the application while typing.
- Selection fields submit with `Enter`, with no `Continue` button. Review, result, and blocked screens expose visible buttons with concrete verbs. The base keys (arrows, `Enter`, `Space`, `Esc`) suffice to discover interaction with no help bar.
- Wizard for first install or dependent decisions; focused editing for customization; conflict resolution when uncertainty appears. Do not turn every task into a wizard.

These rules are the normative base. `product/` defines the complete navigation, state, capability, and component map without contradicting them.

### 12.4 Other surfaces

An editor extension, GUI, or printed material keeps recognizable identity, promise, voice, meanings, and hierarchy, but uses its environment's native conventions. Equivalence is conceptual, not a literal reproduction of pixels or keys. Adapting does not equal making the surface anonymous.

## 13. Governance and verification

### 13.1 What requires new design

**Needs no new design definition:** another L03 menu, another L04 form, an L06 table, or a flow composing already described patterns and states. It requires implementation and verification, not aesthetic improvisation.

**Extending the system is required for:** a new interaction modality, a nonexistent semantic meaning, a task fitting no pattern, or an accessibility requirement demanding another contract. Before extending, demonstrate why the available composition does not serve.

### 13.2 Surface extension contract

Each derived specification MUST declare:

```text
Surface and version:
Base identity: Takt AI / brand.md / 3.0
Tasks and users:
Canonical brand assets and approved variants:
Entry, operation, and reduced-size compositions:
Applied principles: Bxx
Used patterns: Lxx
Tokens and native equivalents:
Navigation, states, and commitment points:
Accessibility, sizes, and supported environments:
Tests and evidence:
Declared gaps:
Proposed extensions, if any:
```

Do not duplicate the palette to keep a local variant. Reference the source and map tokens. If a technical limitation prevents meeting a rule, record it as a gap with scope, mitigation, and closing condition; do not declare that the limitation redefines the brand.

### 13.3 Changes and exceptions

The product/design owner approves changes to the common contract. Every modification records: problem, affected rule, chosen alternative, reason, impacted surfaces, and evidence. An incompatible change in meanings or behavior raises a major version; a compatible extension, a minor one; an editorial fix with no contract change, a patch.

A local exception may not redefine state meanings, hide data loss, justify inaccessibility, or contradict intent preservation. If it needs to, the common contract must be reviewed, not the change hidden in a component.

Chromatic or typographic values are not changed to "refresh" an isolated screen. A token change is evaluated on light, dark, monochrome, and every affected surface.

### 13.4 Acceptance matrix

| ID | Verifiable criterion | Minimum evidence |
|---|---|---|
| V01 | The surface describes its task and uses an Lxx pattern. | Specification and main walkthrough. |
| V02 | The user can identify object, state, and next action without external explanation. | Task test with target-audience people; record doubts and detours. |
| V03 | No local colors, radii, or spacings foreign to the system without justification. | Token and style review. |
| V04 | Text, controls, and focus meet contrast in all their states. | Automatic computation and review on real rendering. |
| V05 | Focus, selection, error, and progress are understood without color. | Monochrome captures and colorless walkthrough. |
| V06 | All flows can be completed with the main input method without hidden shortcuts. | Keyboard walkthrough and applicable assistive evaluation. |
| V07 | Layout keeps decisive content at minimum sizes and with long texts. | Captures on responsive or terminal matrix; web zoom test. |
| V08 | Confirmations obey uncertainty and new consequences. | Install, custom, canonical upgrade, and external-conflict cases. |
| V09 | Upgrade preserves managed customizations and exclusions. | Before/after state test; a capture is not enough. |
| V10 | Uncertain risk and decisive incompatibility receive distinct treatments. | Informed-acceptance and explanatory-block cases. |
| V11 | Partial failure and recovery communicate real state without reproach. | Controlled failure injection or equivalent fixture; exit walkthrough. |
| V12 | Restore makes scope clear and promises no nonexistent undo. | Restore test and prior-summary review. |
| V13 | Components include pertinent empty, loading, error, and disabled states. | State catalog and visual tests. |
| V14 | Signature, voice, and demonstrations represent the product without exaggeration. | Editorial review and claim evidence. |
| V15 | Home with recognizable canonical logo: lateral on wide, adaptation on narrow; operation with no mandatory logo repetition. | Wide, medium, narrow, and monochrome captures; asset reference and approved variants. |
| V16 | The person recognizes Takt AI as a product, distinct from the base harness; composition keeps continuity across views. | Recognition test at entry and in operation with target audience; record attribution confusions. |
| V17 | The brand hides no actions, hinders no reading, and imposes no waits, steps, or texts added only for identity. | Functional walkthrough and visual/editorial review of entry, operation, and reduced size. |

For new functionality inside an existing pattern, review V01–V07, preservation of the approved brand composition, and the applicable behavior criteria. Do not demand new brand research per menu: recognition evidence may be reused while the approved pattern is kept. To declare a surface conformant, evaluate the full applicable matrix and justify the non-applicable; V15 fixes the home logo, it does not require repeating it on operating views.

**Exit gate:** do not approve silent loss of intent, hidden risks, combinations known to be unfeasible, navigation with no exit, illegible functional text, or announced-but-missing recovery. Nor an anonymous home, an unrecognizable adaptation, a narrow composition imposed on every size, or a presence obstructing work.

### 13.5 Minimum scenarios for the first TUI derivation

1. Install with available detection and defaults, no personalization, reviewing the result before applying.
2. Personalize from that same flow and return to the summary without losing choices.
3. Change one specialist's model without redoing foreign configuration.
4. Update a canonical installation with no redundant confirmation.
5. Update with managed exclusions and customizations, preserving them.
6. Resolve an external change by preserving it under informed uncertainty or restoring the canonical.
7. Reject a deterministically incompatible combination explaining how to make it viable.
8. Recover from a partial application with no blaming diagnostics or additional silent loss.
9. Restore an experimental agent with clear scope and return to work.
10. Repeat the pertinent tasks without color, with long text, and on a reduced terminal.
11. Check horizontal home with the canonical logo, narrow-terminal adaptation, and shell continuity in operation without repeating the logo; include resize and wide low-height terminal.
12. Verify Takt AI is recognized without adding slogans, welcomes, or presentation steps, and that the brand hides no decisive controls or information.

These scenarios guide design and verification.

## 14. Reproducible palette validation

This script uses only standard Python, reads the normative tables of this file, and verifies decreasing luminance, text contrast, actions, states, limits, and focus on the intended backgrounds. It does not duplicate the palette into another source of truth. Run from the directory containing `brand.md`.

```python
import re
from pathlib import Path

doc = Path("brand.md").read_text(encoding="utf-8")
palette = {"white": "#FFFFFF", "black": "#000000"}
tokens = {}
for line in doc.splitlines():
    cells = [c.strip().strip("`") for c in line.strip().strip("|").split("|")]
    if len(cells) == 6 and cells[0].isdigit():
        for family, value in zip("NPGAR", cells[1:]):
            assert re.fullmatch(r"#[0-9A-F]{6}", value), value
            palette[family + cells[0]] = value
    if len(cells) == 3 and re.fullmatch(r"[a-z]+(?:\.[a-z]+)+", cells[0]):
        tokens[cells[0]] = cells[1:]

def luminance(key):
    value = palette[key].lstrip("#")
    rgb = [int(value[i:i + 2], 16) / 255 for i in (0, 2, 4)]
    linear = [v / 12.92 if v <= 0.04045 else ((v + 0.055) / 1.055) ** 2.4
              for v in rgb]
    return sum(v * w for v, w in zip(linear, (0.2126, 0.7152, 0.0722)))

def contrast(a, b):
    low, high = sorted((luminance(a), luminance(b)))
    return (high + 0.05) / (low + 0.05)

checks = []
def check(fg, bg, minimum, label):
    actual = contrast(fg, bg)
    assert actual >= minimum, f"{label}: {fg}/{bg} = {actual:.4f}, requires {minimum}"
    checks.append(actual)

levels = (50, 100, 200, 300, 400, 500, 600, 700, 800, 900, 950)
for family in "NPGAR":
    ramp = [luminance(f"{family}{level}") for level in levels]
    assert all(a > b for a, b in zip(ramp, ramp[1:])), family

for theme in (0, 1):
    def value(name):
        return tokens[name][theme]

    backgrounds = ("bg.canvas", "bg.surface", "bg.raised", "bg.sunken", "bg.hover")
    for bg in backgrounds:
        for fg in ("text.primary", "text.secondary", "text.muted",
                   "brand.ink", "link.default", "link.hover"):
            check(value(fg), value(bg), 4.5, f"theme {theme}: {fg}/{bg}")
        for edge in ("border.control", "focus.ring"):
            check(value(edge), value(bg), 3, f"theme {theme}: {edge}/{bg}")
    for group in ("action.primary", "action.danger"):
        for state in ("bg", "hover", "pressed"):
            check(value(group + ".fg"), value(group + "." + state), 4.5, group)
    for group in ("info", "success", "warning", "danger", "selection", "disabled"):
        check(value(group + ".fg"), value(group + ".bg"), 4.5, group)
    check(value("selection.border"), value("selection.bg"), 3, "selection.border")
    check(value("focus.ring"), value("selection.bg"), 3, "focus on selection")

print(f"OK: 5 monotonic scales; {len(checks)} compliant pairs.")
```

The test excludes decorative separators and does not authorize arbitrary combinations of primitives. To add a new semantic pair or place controls on a colored background, extend its test cases. Categorical data combinations additionally require evaluating distinction by shape, label, and context on the real chart.
