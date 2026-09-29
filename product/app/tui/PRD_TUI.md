# PRD: TUI Architecture and State Machines

**Actual implementation progress: 98%** — 20 own requirements: 19 complete, 1 partial, 0 not integrated. Every flow (`takt/tui/install`, `uninstall`, `drift`, `models`, `diagnostics`) and the nested picker (`takt/tui/modelpicker/model_picker.go:141-172`) now route screen changes through the one generic `ui.Table[S,M]` mechanism (`takt/tui/ui/fsm.go:12-22`); the earlier claim that the models flow bypassed the shared table no longer holds — `models.go:113-146` and `model_picker.go` both build `ui.Table` values, and `grep -n '\.step = \|\.state = \|\.phase = '` across every flow shows direct assignment only inside constructors or `applyTransition`/`apply`, never a free-floating `switch`. The controller (`takt/tui/tui.go`) owns the model stack (`push`/`pop`/`active`, lines 122-141), replaces the stack on the install→models cross-flow jump (`routeTransitions()`, lines 164-168, PR-TUI-18), gates quit/back through a `Dirtier`-checked guard (`leave`, lines 319-331), and turns Ctrl+C during a running action into one cooperative `runtime.CancelRequest` (`cancel`, lines 342-354; `Run.Cancel`/`Result` ID-match in `takt/tui/runtime/run.go:36-60`, PR-TUI-20/21). Resize reaches every stacked model (`resize`, tui.go:286-292) and paste reaches only the foreground model (`paste`/`forward`, tui.go:271-284), matching PR-TUI-19's per-kind routing table. `takt/cli/main.go:50-55,127-132` checks `isInteractive` before ever constructing the Bubble Tea program, so the non-interactive contract in PR-TUI-11 is never bypassed by implicit terminal acquisition. The one partial is PR-TUI-19's clause on capability/environment reports: no code anywhere under `takt/tui/` handles `tea.KeyboardEnhancementsMsg`, `tea.ClientEnvironmentMsg`, terminal-version, capability, or terminal-color messages (confirmed by a repo-wide grep with zero matches), so that specific routing rule is unimplemented while every other clause of PR-TUI-19 (delivery order, guarded global intents, action-result routing, resize/paste targeting, discarded key releases) is verified in code.

## 1. Problem

Terminal navigation logic typically degrades into monolithic architectures where screen routing, message dispatch, input capture, and side-effect emission are mixed into large `switch` blocks and ad-hoc «back» mechanisms. Transitions become implicit, so adding a screen or event type does not mechanically surface unhandled combinations; and adding independent screens or sub-flows creates rigid coupling points and fragmented conventions.

Takt requires **maintainability, declarativity, and uniform composability** without introducing performance overhead or custom event loops: per-flow transition tables and a stack of independent models over the Bubble Tea v2 event loop, scalable from linear flows to nested and cross-flow navigation without re-architecture.

## 2. Functional Requirements

### 2.1 Navigation Architecture and Control Stack

| ID | Requirement |
| --- | --- |
| PR-TUI-1 | The TUI shell operates as a controller that manages a stack of independent flow models above the main menu. The main menu is the base of the stack: the controller owns its state and renders it itself when the stack is empty. |
| PR-TUI-2 | Opening an independent screen pushes a new model onto the stack, sizes it to the current terminal, and makes it the foreground model for rendering and input. |
| PR-TUI-3 | Returning from or closing an independent screen pops the active model via a uniform mechanism, restoring the immediately lower model; popping the last model restores the main menu. |
| PR-TUI-4 | Independent models communicate with the controller exclusively through typed navigation messages (such as back or open another flow) handled by the stack mechanism, with no direct references to sibling models or direct access to foreign internal structures. |
| PR-TUI-5 | Navigation between independent screens assumes no prior knowledge of destination or origin, enabling reuse and orthogonal composition. |
| PR-TUI-18 | A cross-flow jump, such as opening model assignment from the installation result, replaces the stack with the destination flow instead of pushing it, so closing the destination returns to the main menu rather than to the finished origin flow. |

```text
                    CONTROLLER-OWNED MODEL STACK

 Open independent screen                                  Close screen
         PUSH                                                  POP
           │                                                    │
           ▼                                                    ▼
┌────────────────────────────────────────────────────────────────────┐
│ Installation Flow Model                                [ACTIVE]    │
│   Targets → Setup → Components → Conflicts → Review → Result       │
│                                            local FSM transitions   │
├────────────────────────────────────────────────────────────────────┤
│ Main Menu (controller-owned)                            [BASE]     │
└────────────────────────────────────────────────────────────────────┘

The top model supplies the foreground view and receives input.
Flow-level `back` changes the local FSM state; on the flow's first
screen it asks the controller to pop. A cross-flow jump replaces the
stack.
```

### 2.2 Per-Flow Finite State Machines (FSM)

| ID | Requirement |
| --- | --- |
| PR-TUI-6 | Each work flow, such as installation, drift correction, or uninstallation, is an independent FSM whose state corresponds to the active screen within the flow. |
| PR-TUI-7 | Every screen or nested sub-flow transition is defined through an explicit, declarative transition table keyed by (state, event), including confirm, back, discard, typed results, controller routing and pending-change guard outcomes. Message decoding and rendering MAY use `switch`; choosing or assigning a navigation destination outside a table is prohibited. |
| PR-TUI-8 | Each table entry names the event accepted in a state and a rule that returns the next state and the side-effect commands to emit. A rule MAY derive the next state from flow data, such as skipping the conflict step when no conflicts exist. Interaction that stays on the same screen (cursor movement, focus switching, toggling, text entry, scrolling) remains outside the table. Constructors MAY select the initial state. |
| PR-TUI-9 | Backward navigation («back») within a flow is an explicitly declared transition in its transition table, distinguished from popping independent screens. |
| PR-TUI-10 | Sub-flows or complex local interaction selectors operate as nested Sub-FSMs within their parent flow, without populating the controller's global stack except when they need to invoke an independent screen. |

### 2.3 Event Loop, Dispatch, and Side Effects

| ID | Requirement |
| --- | --- |
| PR-TUI-11 | TUI execution resides strictly within the canonical Bubble Tea v2 event loop (`Model`, `Cmd`, declarative `tea.View`), with no custom event loops, synchronous blocking, or untyped global buses. Programs pass explicit input/output handles and choose interactive vs linear output before constructing the program; implicit terminal acquisition MUST NOT substitute the non-interactive contract. |
| PR-TUI-12 | Every environment mutation, subprocess invocation, network access, or invocation of the execution layer (runtime/harness) is executed outside the event loop via commands (`Cmd`), returning its results as typed messages. Read-only inspection of local installation state, including plan and conflict preview, MAY run synchronously within `Update`. |
| PR-TUI-13 | View rendering (`View`) is a pure function of the active model's state returning a declarative view. The foreground model produces the content; the controller owns the terminal-level view fields (`AltScreen`, `MouseMode`, `ReportFocus`, `KeyboardEnhancements`) and no flow declares them. Content-level fields (`Cursor`, `ProgressBar`, `WindowTitle`, `BackgroundColor`, `ForegroundColor`) propagate from the foreground model; when the stack is empty the controller declares the main menu's view instead. |
| PR-TUI-19 | Messages are processed one at a time in event-loop delivery order. The controller routes global exit/close intents through the active flow's pending-change guard, never bypassing text-input ownership; interruption during mutation becomes an asynchronous cancellation request. Action requests/results pass through the runtime boundary then the foreground model. Resize reaches every stacked model; paste reaches the foreground model as text entry when it owns a text field; environment and capability reports (client environment, keyboard-enhancement, terminal-version, capability, and terminal-color messages) are consumed by the controller for surface capability, never by flows; key releases are discarded unless a flow explicitly owns that input. Other messages reach only the foreground model (or menu when empty). |
| PR-TUI-20 | While mutation executes, navigation and application exit MUST NOT take effect. Interruption MUST be dispatched as a cooperative cancellation request, not ignored or translated into immediate exit. The flow records and presents cancellation pending until a typed result confirms a stable stop or completion; a non-interruptible phase explains the wait. Repeated requests MUST NOT duplicate mutation or recovery. |
| PR-TUI-22 | Before commitment, edits remain draft state. Back from a nested editor preserves its parent context; leaving an unapplied edit scope offers keep editing or discard. Exit/close/interrupt paths MUST use this same guard. Discard MUST NOT mutate persistent installation state. |
| PR-TUI-23 | Terminal resize and rendering-mode changes MUST preserve draft values, focus, filters, and operation state. Insufficient display space MUST NOT authorize or cancel an operation implicitly. |
| PR-TUI-21 | A flow receiving an action result MUST confirm that it matches its own pending request and MUST ignore any result it did not request. |
| PR-TUI-24 | Pasted input delivered as a paste message reaches the foreground model and, when a text field owns focus, enters as text; it is never interpreted as control keys. |

```text
Bubble Tea message
        │
        ▼
   Controller ── global intent (guarded exit / close / interrupt) ──▶ handled
        │
        ├── ActionRequest / ActionResult ──▶ Runtime boundary ──▶ foreground model
        ├── WindowSize ──────────────────▶ every stacked model
        ├── Paste ───────────────────────▶ foreground model (text entry)
        ├── environment / capability report ──▶ controller (surface capability)
        └── any other message ───────────▶ foreground model (or main menu)
```

## 3. Failure and Anomalous Event Behavior

| Situation | Expected Behavior |
| --- | --- |
| Error or failure in a runtime async command | Emission of a typed error result and transition to the flow's result screen, which presents the failure and its available recovery. |
| Global interrupt while a mutation executes | Per PR-TUI-20, dispatch a cooperative cancellation request without exiting or implying that execution stopped; dispatch MUST NOT synchronously wait for operation stoppage, and typed results report when a stable stopping point or completion has actually been reached. |
| Attempt to access a model out of context | Strict isolation: models interact only via events and the control stack. |

## 4. Success Criteria

- **Total declarativity:** 100% of flow, nested sub-flow, controller-route, result, discard, and guard transitions are enumerable as table entries and verifiable without executing the TUI loop.
- **Zero cross-coupling:** No flow model or independent screen maintains pointers or direct dependencies on other sibling models.
- **Deterministic testability:** Unit test coverage over transition tables guaranteeing that every reachable (screen, event) pair has defined behavior.
- **Deterministic routing:** Every message reaches exactly the recipients PR-TUI-19 assigns to its kind, and no navigation or quit takes effect while a mutation executes.
