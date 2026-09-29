# WBS

| Field | Holds |
| --- | --- |
| Task ID | A stable identifier for this task, unique within the breakdown |
| Scope | What this task does and its boundary — what is explicitly out of scope |
| Acceptance | The condition that proves this task done, checkable without re-reading the code |
| Consumed contracts | The other tasks' outputs or interfaces this task reads or relies on |
| Writable files | The exact paths this task is allowed to create or modify, nothing broader |
| Dependencies | Which other Task IDs this depends on and the shared contract or file that justifies it |
