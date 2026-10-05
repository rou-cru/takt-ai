---
name: takt-vfs-verification
description: "Use when reading a staged VFS delta and attaching an independent verification verdict."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.1"
---

# Takt VFS Verification

For a staged gate, adopt each verifier assignment with `vfs_bind({scope: [], author_key})`,
once per author_key of the phase. The author owns the file paths; the verifier owns an
empty-scope, read-only gate. Read each author's staged files with `vfs_read` naming that
author_key, judge the phase as a whole against the applicable invariants, and attach one
result per author_key with `vfs_verify`: `pass` is true when no applicable requirement failed,
and what could not be checked is reported as unverified. If the assignment is unavailable, report the exact
binding error and the checks that remain unverified.
A failing verdict leaves the author's staged work untouched.
