# Planning DAG Examples

Each scenario shows the assessment, the DAG it produces, and why each edge exists. The
assessment drives the shape: the same lanes connect differently when different
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

## 7. Expanded designer lane

Assessment: raw intent for a game with art and music; pm's requirements name three design
focus areas (visual, music, experience) too broad for one designer brief at the required
detail. Structure to generate; behavior to generate.

```
                ┌──────────────┐
                │ P  pm        │                                    Round 1 · width 1
                └──────┬───────┘
        ┌──────────────┼──────────────┬─────────────────┐
        ▼              ▼              ▼                 │
 ┌────────────┐ ┌────────────┐ ┌────────────┐           │
 │ Dv visual  │ │ Dm music   │ │ Dx exper.  │           │           Round 2 · width 3
 └─────┬──────┘ └─────┬──────┘ └─────┬──────┘           │
       └──────────────┼──────────────┘                  │
                      ▼                                 ▼
                ┌────────────┐                    ┌────────────┐
                │ D  merge   │                    │ R architect│    Round 3 · width 2
                └─────┬──────┘                    └─────┬──────┘
                      └────────────────┬────────────────┘
                                       ▼
                                 ┌────────────┐
                                 │ S  spec    │                     Round 4 · width 1
                                 └────────────┘
```

- `P → Dv, Dm, Dx`: each designer instance owns one focus area pm named, so their briefs are
  disjoint and none guesses another's scope.
- `Dv, Dm, Dx → D`: the consolidating designer consumes the three partial results and removes
  contradictions, aligns details and fills gaps; the final experience invariant is its result.
- `P → R`: the architect consumes only pm's requirements; it runs in round 3 because the
  ceiling is full in round 2, not because of an edge.
- `D → S`, `R → S`: behavior is specified against the consolidated experience and the structure.
- Without a consolidator, the partial results go straight to their consumers (`Dv, Dx → R`)
  when those consumers can reconcile them within their own lane.

## 8. QA planned

Assessment: intent and structure settled; behavior to generate; pm's requirements name a
must-not-fail item and acceptance spans several units; no test policy in the workspace.

```
               ┌────────────┐
               │ S  spec    │                 Round 1 · width 1
               └─────┬──────┘
                     ▼
               ┌────────────┐
               │ Q  qa      │                 Round 2 · width 1
               └────────────┘
```

- `S → Q`: the test plan and traceability matrix are drawn from the specification's
  acceptance; Q also consumes pm's must-not-fail items and generates the test policy.
- Planning ends with S and Q; both feed TPM.
- With the specification already settled and no test plan, Q is the only lane.
