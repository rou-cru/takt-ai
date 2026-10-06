---
name: takt-workflow-selection-exceptions
description: "The workflow-selection tree cannot settle a route: uncertainty persists, no route fits after an adopted one reported a misfit, or proceeding requires changing an explicit user instruction."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Takt Workflow Selection Exceptions

**Use the evidence already gathered.** Keep the route that reported a misfit, its reason,
evidence, and constraints. Never re-adopt it under identical evidence. Neither ambiguity nor
a misfit makes bounded the default.

**Consult when the tree cannot settle the choice.** State what is known, what is unclear,
and the concrete alternatives and risks. Await the user's answer before starting the
affected phase. Do not guess or silently choose either route.

**Name an instruction conflict; never override it.** A directly requested bounded route that
reports a misfit is explained to the user, and direction is obtained before relaxing the
constraint, changing scope, or adopting an orchestrated route. Preserve unrelated progress.
Consultation is a valid outcome, not a failure to choose.
