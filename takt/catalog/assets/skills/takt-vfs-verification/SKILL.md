---
name: takt-vfs-verification
description: "Use when reading a staged VFS delta and attaching an independent verification verdict."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.2"
---

# Takt VFS Verification

For a staged gate, each verifier assignment arrives bound, one per author_key of the phase,
with the files that author staged. The author owns the file paths; the verifier owns an
empty-scope, read-only gate. Read each author's listed files with `vfs_read` by path, judge
the phase as a whole against the applicable invariants, and attach one result per author_key
with `vfs_verify`: `pass` is true when no applicable requirement failed, and what could not
be checked is reported as unverified. If the assignment is unavailable, report the exact
binding error and the checks that remain unverified.
If `vfs_verify` reports that the author's staged work changed, read it again with `vfs_read`
and attach the verdict again.
Workspace checks do not prove changes that remain staged.
A failing verdict leaves the author's staged work untouched.
