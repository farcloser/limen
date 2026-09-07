# Farcloser: the engineering book

This book covers everything related to developers tooling, style and
architecture that is common to all our projects.

It is both generic (providing high-level guidance on generic doctrine decisions)
and specific and opinionated (when it comes to our shared tooling).

The book is divided in many sections, each cleanly covering a specific aspect,
which can be read or referred to individually in relative isolation.

## Generic principles

We value above all:
- absolute correctness: no spaghetti, half baked abstractions, no warnings tolerated, no "should work for now". It just works.
- KISS: never over engineer for an hypothetical future expansion.
Either the use case is genuinely generic now, or by design, or it should be kept SIMPLE
- proper architecture and modularization: interfaces are *client-defined* to reduce hard dependencies, underlying details never leak into high level abstractions
- error handling is first-class citizen: use sentinel and wrap errors with a clean, reasonably sized set of module specific errors
- logging: slog
- pinned means by digest: every image, action, and tool is pinned to content, not to a tag — in code, in examples, and in documentation alike, because examples are what gets copied
- linters are law: no finding is ignored, and a finding that is silenced is silenced by its rule, never by its linter (see [per-language rules](./per-language.md#go--silencing-a-finding))
- a comment names a trap, not a story: it says the one non-obvious thing a future editor would get wrong at that spot — a working-directory constraint, an ordering that matters — and nothing else. Where a tool comes from or which version runs is the hermetic PATH's job and the book's to explain once; what the code does, why a pin is what it is, what the pull request was about belongs in the commit message. One line beats five; none beats one when the code already says it
