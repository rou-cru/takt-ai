# PRD: TUI Architecture and State Machines

## 1. Functional Requirements

### 1.1 Navigation Architecture and Control Stack

| ID | Requirement |
| --- | --- |
| PR-TUI-1 | The TUI shell operates as a controller that manages a stack of independent flow models above the main menu. The main menu is the base of the stack: the controller owns its state and renders it itself when the stack is empty. |
| PR-TUI-2 | Opening an independent screen pushes a new model onto the stack, sizes it to the current terminal, and makes it the foreground model for rendering and input. |
| PR-TUI-3 | Returning from or closing an independent screen pops the active model via a uniform mechanism, restoring the immediately lower model; popping the last model restores the main menu. |
| PR-TUI-4 | Independent models communicate with the controller exclusively through typed navigation messages (such as back or open another flow) handled by the stack mechanism, with no direct references to sibling models or direct access to foreign internal structures. |
| PR-TUI-5 | Navigation between independent screens assumes no prior knowledge of destination or origin. |
| PR-TUI-18 | A cross-flow jump, such as opening model assignment from the installation result, replaces the stack with the destination flow instead of pushing it, so closing the destination returns to the main menu rather than to the finished origin flow. |

### 1.2 Per-Flow Finite State Machines (FSM)

| ID | Requirement |
| --- | --- |
| PR-TUI-6 | Each work flow, such as installation, drift correction, or uninstallation, is an independent FSM whose state corresponds to the active screen within the flow. |
| PR-TUI-7 | Every screen or nested sub-flow transition is defined through an explicit, declarative transition table keyed by (state, event), including confirm, back, discard, typed results, controller routing and pending-change guard outcomes. Message decoding and rendering MAY branch directly; choosing or assigning a navigation destination outside a table is prohibited. |
| PR-TUI-8 | Each table entry names the event accepted in a state and a rule that returns the next state and the side-effect commands to emit. A rule MAY derive the next state from flow data, such as skipping the conflict step when no conflicts exist. Interaction that stays on the same screen (cursor movement, focus switching, toggling, text entry, scrolling) remains outside the table. Constructors MAY select the initial state. |
| PR-TUI-9 | Backward navigation within a flow is an explicitly declared transition in its transition table. At the first local state it requests a controller pop; otherwise it changes the local FSM state. |
| PR-TUI-10 | Sub-flows or complex local interaction selectors operate as nested Sub-FSMs within their parent flow, without populating the controller's global stack except when they need to invoke an independent screen. |

### 1.3 Event Loop, Dispatch, and Side Effects

| ID | Requirement |
| --- | --- |
| PR-TUI-11 | TUI execution resides strictly within a single model–update–view event loop with declarative views, with no custom event loops, synchronous blocking, or untyped global buses. Programs pass explicit input/output handles and choose interactive vs linear output before constructing the program; implicit terminal acquisition MUST NOT substitute the non-interactive contract. |
| PR-TUI-12 | Every environment mutation, subprocess invocation, network access, or invocation of the execution layer (runtime/harness) is executed outside the event loop via asynchronous commands, returning its results as typed messages. Read-only inspection of local installation state, including plan and conflict preview, MAY run synchronously within the update step. |
| PR-TUI-13 | View rendering is a pure function of the active model's state returning a declarative view. The foreground model produces the content; the controller owns terminal-level view settings (alternate screen, mouse mode, focus reporting) and no flow declares them. Content-level settings (cursor, progress indicator, window title, background and foreground colors) propagate from the foreground model; when the stack is empty the controller declares the main menu's view instead. |
| PR-TUI-19 | Messages are processed one at a time in event-loop delivery order. The controller routes global exit/close intents through the active flow's pending-change guard, never bypassing text-input ownership; interruption during mutation becomes an asynchronous cancellation request. Action requests/results pass through the runtime boundary then the foreground model. Resize reaches every stacked model; paste reaches the foreground model as text entry when a text field owns focus, never as control keys; the terminal's color-profile report is consumed by the controller for surface capability, never by flows; key releases are discarded unless a flow explicitly owns that input. Other messages reach only the foreground model (or menu when empty). |
| PR-TUI-20 | While mutation executes, navigation and application exit MUST NOT take effect. Interruption MUST be dispatched as a cooperative cancellation request, not ignored or translated into immediate exit. The flow records and presents cancellation pending until a typed result confirms a stable stop or completion; a non-interruptible phase explains the wait. Repeated requests MUST NOT duplicate mutation or recovery. |
| PR-TUI-22 | Before commitment, edits remain draft state. Back from a nested editor preserves its parent context; leaving an unapplied edit scope offers keep editing or discard. Exit/close/interrupt paths MUST use this same guard. Discard MUST NOT mutate persistent installation state. |
| PR-TUI-23 | Terminal resize and rendering-mode changes MUST preserve draft values, focus, filters, and operation state. Insufficient display space MUST NOT authorize or cancel an operation implicitly. |
| PR-TUI-21 | A flow receiving an action result MUST confirm that it matches its own pending request and MUST ignore any result it did not request. |

## 2. Failure and Anomalous Event Behavior

| Situation | Expected Behavior |
| --- | --- |
| Error or failure in a runtime async command | Emission of a typed error result and transition to the flow's result screen, which presents the failure and its available recovery. |

## 3. Success Criteria

- **Total declarativity:** 100% of flow, nested sub-flow, controller-route, result, discard, and guard transitions are enumerable as table entries and verifiable without executing the TUI loop.
- **Zero cross-coupling:** No flow model or independent screen maintains pointers or direct dependencies on other sibling models.
- **Deterministic testability:** Unit test coverage over transition tables guaranteeing that every reachable (screen, event) pair has defined behavior.
- **Deterministic routing:** Every message reaches exactly the recipients PR-TUI-19 assigns to its kind, and no navigation or quit takes effect while a mutation executes.
