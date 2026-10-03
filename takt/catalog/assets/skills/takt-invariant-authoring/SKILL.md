---
name: takt-invariant-authoring
description: "Before authoring a deliverable you will record in memory. The generic
  skeleton for a document that matches none of your role's recognized types, and how many
  documents one result needs — never an arbitrary length cut of one document."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Takt Invariant Authoring

Shapes the document(s) a planning result becomes, before `takt-memory-contract` and your own
`takt-memory-<role>` skill govern how you record them. Your own skill lists the document
types your role recognizes; this one covers what's shared across all of them.

## When nothing recognized fits

Still a real document, never improvisation: `references/generic-artifact.md` holds the
section skeleton to use.

## How many

One recognized type, or this generic skeleton, covers it → one document. Content spanning
two or more distinct types — an architecture decision and its interface contract, say — is
one document per type, each authored and recorded on its own. Never split one type's content
into pieces to keep it short; `takt-memory-contract` already says how to size it.
