# WBS

| Field | Holds |
| --- | --- |
| Assignment ID | A stable identifier for this assignment, unique within the breakdown |
| Slice | The vertical slice this assignment belongs to: the user story it delivers, by ID, and the objectives or sub-objective this assignment covers |
| Scope | What this assignment does and its boundary — what is explicitly out of scope |
| Acceptance | The condition that proves this assignment done, checkable without re-reading the code, as a bounded command |
| Allowed reads | The files this assignment may read; none is written by a concurrent assignment |
| Consumed contracts | The other assignments' outputs or interfaces this assignment reads or relies on |
| Writable files | The exact paths this assignment is allowed to create or modify, nothing broader |
| Dependencies | Which other Assignment IDs this depends on and the shared contract or file that justifies it |
