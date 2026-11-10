---
name: takt-session-resume
description: "The user asks about or continues earlier work, with or without a session id. Explains locating the previous session from Takt memory and resuming from where that work stands."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.1"
---

# Takt Session Resume

Resuming is reaching an objective that earlier work did not reach: never start from
zero, establish where things stand.

## Locating the session

Find candidates at the lowest cost, with `takt_memory_sessions`:

1. Query with the key words of the topic, not the whole sentence, and a small `limit`. Query an
   id by itself, exactly as listed. When nothing matches, retry with fewer words before
   concluding there is no session.
2. Rank by match with what the user said, then recency, then workspace. The current
   workspace is the normal case; another workspace is rare but valid when one objective
   spans several repositories, and is listed (`all_workspaces`) when nothing here matches or
   the user points there.
3. A session listed as open has entries no close covers: it was interrupted, unless it still
   runs in another conversation. In the message that presents it, ask whether that work
   stopped; if it still runs, leave it and follow the user. A session listed `current` is this
   conversation, never a candidate. The light reconstruction of an open session is its listed
   closes and entry titles, the last one included: enough to ask, not to resume.

Judge from the listing and stop at the first plausible candidate. Present it and ask; open no
body before the user confirms. Then read one entry at a time, only when the next action needs
its content.

Present the strongest candidate in one or two lines: objective, workspace, closed or
open.

## Scenarios

| Situation | Action |
|---|---|
| Id given, session exists and is closed | Resume from its state at close |
| Id given, session exists and is open | Ask whether that work stopped; if it did, resume as an interrupted session |
| Id given, no such session | Say so; search as if no id was given |
| Candidate confirmed, closed | Resume from its state at close |
| Candidate confirmed, open and stopped | Read its entries in order; resume confirming the cut-off point |
| Candidate confirmed, open and still running elsewhere | Leave it; follow the user |
| Candidate rejected | Offer the next two or three strongest candidates |
| None recognized | Follow the user: a new hint means a new search; starting without the earlier context means starting fresh |
| No recorded session matches anywhere | Say so; ask for a hint or start fresh |

## Resuming

- Start from the state at close, or from the interrupted session's entries.
- Declare continuity with a closed session you resume through `takt_memory_continue_session`.
- Check the claims the state names against the current workspace and any repository the
  Takt memory names; verified repository state prevails.
- Present in one message the objective, what Takt memory claims that the repositories do not show
  (or the reverse), open disputes as open, and for an interrupted session its last step. Ask
  only what that leaves in conflict or unknown; with nothing open, continue.

## Anything else

Show the user what you found and resume only once they confirm
which work it is and where it stands.
