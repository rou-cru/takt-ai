# Planning DAG Examples

Each scenario shows the assessment, the DAG it produces, and why each edge exists. The
assessment drives the shape: the same five lanes connect differently when different
invariants are already settled.

## 1. Greenfield, raw intent

Assessment: intent, facts, experience, structure, and behavior all to generate. Empty
workspace; the user's request is the only input.

```
                ┌──────────────┐
                │ P  pm intent │                Round 1 · width 1
                └──────┬───────┘
                       ▼
                ┌──────────────┐
                │ A  analyst   │                Round 2 · width 1
                │ env + tools  │
                └──────┬───────┘
              ┌────────┴────────┐
              ▼                 ▼
       ┌────────────┐    ┌────────────┐
       │ D designer │    │ R architect│       Round 3 · width 2
       └─────┬──────┘    └─────┬──────┘
             └────────┬────────┘
                      ▼
               ┌────────────┐
               │ S  spec    │                 Round 4 · width 1
               └────────────┘
```

- `P → A`: the analyst investigates what the intent needs (the runtime for *this* product),
  not the environment in general.
- `A → D`, `A → R`: experience and structure decide against verified facts (available
  runtime, constraints), not assumed ones.
- `D → S`, `R → S`: behavior is specified against the settled experience and structure.

## 2. Settled intent, known environment

Assessment: an approved PRD and a validated stack in memory; experience, structure, and
behavior to generate.

```
       ┌────────────┐    ┌────────────┐
       │ D designer │    │ R architect│       Round 1 · width 2
       └─────┬──────┘    └─────┬──────┘
             └────────┬────────┘
                      ▼
               ┌────────────┐
               │ S  spec    │                 Round 2 · width 1
               └────────────┘
```

- No pm and no analyst: their invariants are settled, so their lanes and edges are absent.
- D and R both consume the PRD ID and run concurrently.

## 3. Experience constrains structure

Assessment: intent settled; a UI-heavy product whose interaction model decides the state
architecture (offline-first, realtime collaboration).

```
               ┌────────────┐
               │ D designer │                 Round 1 · width 1
               └─────┬──────┘
                     ▼
               ┌────────────┐
               │ R architect│                 Round 2 · width 1
               └─────┬──────┘
                     ▼
               ┌────────────┐
               │ S  spec    │                 Round 3 · width 1
               └────────────┘
```

- `D → R`: here the edge is real, because the architect's state lifecycle depends on the
  designer's interaction rules. Without that dependency, D and R stay concurrent as in
  scenario 2.

## 4. A fact blocks a structural decision

Assessment: intent and experience settled; the structure hinges on an unverified fact
(whether an existing service supports streaming).

```
               ┌────────────┐
               │ A  analyst │                 Round 1 · width 1
               │ 1 question │
               └─────┬──────┘
                     ▼
               ┌────────────┐
               │ R architect│                 Round 2 · width 1
               └─────┬──────┘
                     ▼
               ┌────────────┐
               │ S  spec    │                 Round 3 · width 1
               └────────────┘
```

- The analyst runs first only because it has a concrete question taken from the settled
  invariants; an analyst with no question has no place in the DAG.

## 5. Refinement of one invariant

Assessment: everything settled except behavior, which a new requirement changes.

```
               ┌────────────┐
               │ S  spec    │                 single lane
               └────────────┘
```

- One lane: bounded work, no planning DAG. The lane consumes the existing spec ID it refines.

## 6. Contradiction between lanes

Assessment: scenario 2 returned, and the designer's flow requires data the architect's
interfaces do not expose.

```
       ┌────────────┐    ┌────────────┐
       │ D designer │    │ R architect│       Round 1 (returned)
       └─────┬──────┘    └─────┬──────┘
             └──── conflict ───┘
                      ▼
               ┌──────────────┐
               │ R' architect │               re-dispatch: original document, its
               │   rewrite    │               result, standing D, the contradiction
               └──────┬───────┘
                      ▼
               ┌────────────┐
               │ S  spec    │
               └────────────┘
```

- D stands because the user approved its flow; only R rewrites. Spec waits until no
  contradiction remains open.
