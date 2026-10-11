# Generic principles

What we value above all, whatever the language. Where a principle has a specific form, a
chapter of its own holds it; the links say where.

- **Absolute correctness.** No spaghetti, no half-baked abstraction, no warning tolerated, no
  "should work for now". It just works.
- **KISS.** Never over-engineer for a hypothetical future. Either the use case is generic
  now, by design, or the thing stays simple.
- **Architecture and modularization.** Interfaces are *client-defined*, to cut hard
  dependencies; underlying details never leak into a higher abstraction.
- **Consumers get the contract, and only the contract.** A consumer of a library or a system
  demands properties — that it behaves a certain way — and that contract is all it needs to
  know. A bug, or a violation of the contract, is the owner's to resolve, by clarifying the
  contract or fixing the implementation; how the owner tests its internals is not the
  consumer's business. A comment in package A states A's guarantee, never what B does with
  A's output. A wrapper with a stated contract (the same as `os`, plus one Windows
  difference) grows no export outside it because a caller wanted a home for a platform
  split: the caller keeps its helper private, or the thing goes where its general meaning
  lives.
- **Tests are black-box, never bought with indirection.** A test lives in the external test
  package (`package foo_test` in Go; the pinned lint enforces it) and reaches only what a
  consumer reaches. Production code is never given an interface, a function field or any
  other indirection whose only purpose is a test's fake: that is bad design, not
  testability; an interface is defined by a real client. When the fixed path is not
  reachable from outside, the question is how the test rig is modelled, never whether to
  open the code. A property no unit test can observe (a power loss, a filesystem failure)
  gets no unit test: a mock that asserts it was called proves only that the code calls
  itself. Correctness there is by construction and by reading; fault injection is a
  separate, larger rig ([testing](./testing.md)).
- **A package's tests are one contract, stated once, checked within a bound.** The contract
  is written at the top of the test package from the sources that define it; a bounded walk
  holds the code to every edge of it; a fuzz target runs past the bound; an older suite goes
  only when a mutation score shows the new one catches every mutant the old one did
  ([the tests are one contract](./testing-go.md#the-tests-are-one-contract-checked-within-a-bound)).
  Writing the contract down is where the bugs are found.
- **A type admits only its valid values.** A value that must not exist is removed by the
  type — sealed, unexported, returned by a function — not guarded at every call site, and
  not given a meaning by default (a zero value that silently stands for something). Nothing
  exported is mutable package state. The simplest shape that holds wins: no state kept
  twice, no helper that only forwards; a declaration moves when the reason it sat somewhere
  goes.
- **A size that comes from input is bounded before anything is allocated from it.** A
  dimension, a count or a length read from a file or a graph is checked against a named
  limit, with its own error, before the first allocation it drives, and a test runs at the
  limit. An unbounded size is a crash the input chooses: a WebP header misread by a library
  once asked for hundreds of millions of points a side.
- **Errors are first-class.** A module has a small set of sentinels, one per fault class a
  caller acts on; every error wraps one; a caller compares with `errors.Is`, never by text
  ([Go — errors](./errors-go.md)).
- **Logging.** A library writes nothing to standard error; a command with diagnostics uses
  `log/slog`, set up once in `main`; a command whose output is messages prints them
  ([Go — logging](./logging-go.md)).
- **Pinned means by digest.** Every image, action and tool is pinned to content, not to a
  tag — in code, in examples and in documentation alike, because examples are what gets
  copied.
- **Every linter finding is judged; none is ignored.** Fixed when the fix makes the code
  better; silenced inline when it does not — by its rule, never by its linter, with the
  reason the code is right as it is; raised against the baseline when the rule is wrong.
  A linter serves the code; working code is never bent to satisfy one
  ([judging a finding](./linting.md#judging-a-finding),
  [silencing a finding](./linting-go.md#silencing-a-finding)).
- **A comment names a trap, not a story.** The one non-obvious thing a future editor would
  get wrong at that spot — a working-directory constraint, an ordering that matters — and
  nothing else. Where a tool comes from is the hermetic `PATH`'s job and the book's to
  explain once; what the code does, why a pin is what it is, what the pull request was about
  belong in the commit message. One line beats five; none beats one when the code already
  says it. A comment is read against its code at every touch, since a wrong one is believed:
  a package doc that promised a function that never existed
  (https://github.com/mycophonic/primordium/pull/163) and a method doc that said "without a
  syscall" over code that made one on every call
  (https://github.com/mycophonic/primordium/pull/162) each stood for months.
