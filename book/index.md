# Farcloser: the engineering book

This book covers everything related to developers tooling, style and
architecture that is common to all our projects.

It is both generic (providing high-level guidance on generic doctrine decisions)
and specific and opinionated (when it comes to our shared tooling).

The book is divided in many sections, each cleanly covering a specific aspect,
which can be read or refered to individually in relative isolation.

## Generic principles

We value above all:
- absolute correctness: no spaghetti, half baked abstractions, no warnings tolerated, no "should work for now". It just works.
- KISS: never over engineer for an hypothetical future expansion.
Either the use case is genuinely generic now, or by design, or it should be kept SIMPLE
- proper architecture and modularization: interfaces are *client-defined* to reduce hard dependencies, underlying details never leak into high level abstractions
- error handling is first-class citizen: use sentinel and wrap errors with a clean, reasonnably sized set of module specific errors
- logging: slog
-
