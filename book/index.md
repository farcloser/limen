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
- consumers get the contract, and only the contract: a consumer of a library or a system gets to demand properties — that it behaves a certain way — and that contract is all it needs to know. A bug, or a violation of the contract, is the owner's to resolve, by clarifying the contract or fixing the implementation; anything else is off limits. How the owner tests its internals is not the consumer's business. A comment in package A does not narrate what package B does with A's output; it states A's own guarantee. A wrapper with a stated contract (the same as `os`, plus one Windows difference) does not grow an export outside that contract because a caller found it a convenient place for a platform split: the caller keeps its helper private, or the thing goes where its general meaning lives. The failure mode is spaghetti with leaky abstractions; what is wanted is clean, crisp, simple abstractions with no internal leakage
- tests are black-box, and never bought with indirection: a test lives in the external test package (`package foo_test` in Go — the pinned lint enforces the boundary) and reaches only what a consumer reaches, never internals or private state. Production code is never given an interface, a function field, or any other indirection whose only purpose is to let a test substitute a fake — that is bad design, not testability; an interface is defined by a real client, never by a test. When the code path with the fix is not reachable from the outside, the question is how the test rig is modelled, never whether to open the code up. Some properties — a power loss, a filesystem failure — are not observable from a unit test at all; then there is no unit test, and a fake one that asserts a mock was called is worse than none, because it proves that the code calls itself. Correctness there is by construction and by reading; a real fault-injection environment is a separate, larger question
- error handling is first-class citizen: use sentinel and wrap errors with a clean, reasonably sized set of module specific errors
- logging: slog
- pinned means by digest: every image, action, and tool is pinned to content, not to a tag — in code, in examples, and in documentation alike, because examples are what gets copied
- linters are law: no finding is ignored, and a finding that is silenced is silenced by its rule, never by its linter (see [per-language rules](./per-language.md#go--silencing-a-finding))
- a comment names a trap, not a story: it says the one non-obvious thing a future editor would get wrong at that spot — a working-directory constraint, an ordering that matters — and nothing else. Where a tool comes from or which version runs is the hermetic PATH's job and the book's to explain once; what the code does, why a pin is what it is, what the pull request was about belongs in the commit message. One line beats five; none beats one when the code already says it
