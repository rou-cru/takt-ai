---
name: takt-vfs-verification
description: "Use when reading a staged VFS delta and attaching an independent verification verdict."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Takt VFS Verification

For a staged gate, adopt the verifier assignment with `vfs_bind({scope: [], author_key})`.
The author owns the file paths; the verifier owns an empty-scope, read-only gate.
Read the author's staged files with `vfs_read`, assess the applicable invariants, and
attach the result with `vfs_verify`. Report what executable acceptance could be run
separately from the staged verdict. If the assignment is unavailable, report the exact
binding error and the checks that remain unverified.
