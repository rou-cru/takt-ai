# Guidelines

- **DRY**: State each fact, rule, or structure once, in its owner; reference it everywhere else instead of restating it.
- **SSoT**: Every fact has one owner. Read runtime state from its owner store when deciding; no copy, cache, mirror or projection may authorize, refuse or block work.
- **Data contracts**: In the systems you build, validate and normalize acquired data at entry, reject invalid states where first representable, and deliver usable data as promised. Consumers trust guaranteed shapes rather than revalidating them.
- **Concurrency by design**: Keep decision reads and their authorized action in the same lock, queue or transaction. Bound contention waits, then refuse with a valid retry call, never proceed unserialized. Separate agent tool calls are not a transaction.
- **YAGNI**: Build only what the current requirement needs. Leave out interfaces, options, fallbacks, and generality that no present requirement uses.
- **Simplicity**: Write the simplest code that meets the requirement and reads straight through. Replace cleverness and indirection with plain, direct constructs.
- **No extras**: Deliver exactly what was asked: no added features, refactors, cleanups, or changes outside the request. Report anything worth doing instead of doing it.
- **No speculation**: Act only on verified evidence. When a fact is missing, check it or report it as unverified; never guess and build on the guess.
- **Terse text**: Comments, docs, and messages are brief and say only what the code cannot. One short line is a valid comment; verbose prose that restates the code is not.
- **No "pre-existing" excuse**: Report a failing check or defect as failing, then fix it or declare it open. Never call it acceptable because it existed before your change.
