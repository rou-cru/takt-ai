---
name: takt-session-resume
description: "The user says this work continues earlier work, with or without a session id. Explains locating the previous session from memory, declaring continuity, and resuming from where that work stands."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Takt Session Resume

Two separate decisions. **Resuming** is reaching an objective that earlier work did not
reach: never start from zero, establish where things stand. **Declaring continuity**
(`memory_continue_session`) keeps the memory narrative unbroken. Resuming never waits on
the declaration, and failing to declare never stops the resume.

The declaration becomes a permanent link with this session's first new memory entry. Until
then it is replaced by a new call and withdrawn by an empty id. Declare once the user gave
the id or confirmed the candidate. That answer can be careless, so it is not the final
confirmation: reaching the first entry without the user objecting is. If the user signals
before that point that this is not the continuity they meant, replace or withdraw it.

## Locating the session

With an id, read that session's anchors. Without one, find candidates at the lowest cost:

1. Read only `session_anchor` entries — `[takt] Session <id> started` and `[takt] Session
   <id> closed`. Bodies of other entries wait until the user confirms.
2. Rank by match with what the user said, then recency, then workspace. The current
   workspace is the normal case; another workspace is rare but valid when one objective
   spans several repositories, and is searched when nothing here matches or the user points
   there.
3. A session with a start anchor and no closed anchor was interrupted. Its light
   reconstruction is its entry titles and its last entry: enough to ask, not to resume.

Present the strongest candidate in one or two lines: objective, date, workspace, closed or
interrupted.

## Scenarios

| Situation | Action |
|---|---|
| Id given, session exists and is closed | Declare; resume from its state at close |
| Id given, session exists and was interrupted | Do not declare; resume as an interrupted session |
| Id given, no such session | Say so; search as if no id was given |
| Candidate confirmed, closed | Declare; resume from its state at close |
| Candidate confirmed, interrupted | Do not declare; read its entries in order; resume confirming the cut-off point |
| Candidate rejected | Offer the next two or three strongest candidates |
| None recognized | Follow the user: a new hint means a new search; starting without the earlier context means withdrawing any declaration and starting |
| No recorded session matches anywhere | Say so; ask for a hint or start fresh |
| User signals, before the first entry, a different continuity | Replace or withdraw the declaration |
| Declaration rejected | Do not retry; resume without it |

## Resuming

- Start from the state at close, or from the interrupted session's entries.
- Check it against the current workspace and against any repository the memory names;
  verified repository state prevails.
- Confirm with the user, in order: whether the objective still stands, what memory claims
  that the repositories do not show (or the reverse), open disputes as open, and for an
  interrupted session whether its last step finished.
- Continue the work only after that.

## Anything else

Declare nothing doubtful. Show the user what you found and resume only once they confirm
which work it is and where it stands.
