# Testing

What a test is for, what a package's tests must establish, when a test goes, and where the
tests run, in every language. The Go form, with the files, the lanes, the lint and the cases
that set the rules, is [testing — Go](./testing-go.md). The reviewer's side is
[reviewing code](./review.md); a fork's baseline and differential checks are in
[forks](./forks.md).

Tests are where discipline slips, not where quality comes from. A package is judged on its
usefulness, its design and its correctness first; its tests prove what those decided.

## What a test is for

- **A test holds the code to its contract, never to itself.** The contract comes from the
  sources that define it: the specification, the man page, the platform's documentation,
  the package's own docs, the consumers' needs. It is written down before the code is read;
  a table read off the code enshrines the code's bugs and passes by construction. Writing
  the contract down is where the bugs are found.
- **A test is black-box, and never bought with indirection.** It reaches what a consumer
  reaches: no private state, and no interface, function field or other seam in production
  code whose only purpose is a test's fake. An interface is defined by a real client. When
  the path is not reachable from outside, the question is how the test rig is modelled,
  never whether to open the code. A format the package documents is part of its contract: a
  test may write it, at the documented offsets, to plant the state a case needs; it may not
  read an unexported layout.
- **A test earns its place by a change to the package that would fail it.** One that checks
  what the standard library or the runtime guarantees cannot fail short of a change to them,
  reads as coverage of the thing beside it, covers none of it, and rots. The same holds for
  a change under review: a test that passes before and after proves nothing about it
  ([reading the change](./review.md#reading-the-change)).
- **A property no test can observe gets no test.** A fake that asserts a mock was called
  proves only that the code calls itself. A crash is observable: the nearest testable
  neighbour of a power loss is a process killed inside the window the test is about, and the
  test owns that process. Only what no process boundary reaches, a torn sector, the kernel
  refusing a call, goes untested; correctness there is by construction and by reading, and
  fault injection is a separate, larger rig.

## What a package's tests establish

The full pass, by default. Each item is done, or named as skipped with its reason; "done"
with an unnamed gap is not done.

1. **The contract, stated once.** Written from the documented intent and the consumers'
   needs, then checked against the sources, each rule cited. Every exported behaviour, and
   every error class a caller can act on.
2. **Every edge within a bound.** For each class the contract names, its boundary, one step
   past it and a member between; every option combination; sizes around every buffer and
   block boundary, zero, one, the largest; every platform branch; for a stateful package,
   every sequence of calls up to a small length, held to a model after each. The bound makes
   the check exhaustive; the contract keeps it small.
3. **Past the bound, fuzzed**, wherever there is an input space: bytes, names, text. The
   oracle is the contract: refused or accepted, never a panic, and what is accepted
   round-trips. A crasher is kept as a regression case.
4. **The test runs where the contract holds.** A promise across processes is tested with a
   real second process; across platforms, on every leg the package claims; against a
   service, with a local stand-in that records what arrived. A boundary runs for real on
   the leg with the tightest limit, where skipping it tests nothing. A bound on time is a
   contract rule like any other: "the first call after open returns within a limit" is how
   a lock left held is caught, since nothing else observes it from outside. A test skips
   only for a capability the host lacks, never for a limit the host has, and says which.
5. **A drop-in is checked beside the original**, call for call, on the same state: what the
   caller saw and what was left behind must match, on every platform. Where upstream ships
   a reference implementation, the fork's output over a corpus is compared with it
   ([forks](./forks.md)). A generated artifact is built twice and compared byte for byte.
6. **The oracle is exact.** A truth computed in the code's own arithmetic is the code again:
   an exact result comes from a wider type, and a tolerance is derived from the arithmetic,
   never picked.
7. **A resource test asserts zero growth**, over enough rounds that one unit a round would
   show; a page of slack hid 49 bytes per instance for weeks ([forks](./forks.md), the
   pitfalls table).
8. **Speed is guarded where the package exists for it**: a benchmark kept beside the tests,
   on the shape the real consumer runs, read for the regression a green test cannot show,
   and run before and after a change to the hot path. A buffer size or a pool is chosen by
   that measurement, never by reflex.

## When a test goes

- **A suite is replaced under a mutation score.** Single-point mutants of the package (a
  flipped comparison, a dropped negation, an off-by-one, a deleted statement, an error made
  nil), each run against both suites; the old suite goes when the new one catches every
  mutant the old one did, and an example stays when it catches what a property cannot. The
  score pins; it never grades. A survivor is read by where it sits: in a log field, a
  message string or a branch arm that does what the other does, it is equivalent and left;
  in a comparison, a bound or an error path, it is a missing edge in the walk, added before
  the old suite goes.
- **A test only the platform can fail goes.** A package that is a set of sentinels and no
  logic needs no test file; the coverage gate is on the module, not the package.

## Flakes

- **A flake is fixed when it is noticed.** A test that fails and then passes on a rerun is a
  bug that came with its reproducer; the rerun found it and did not fix it. The usual cause
  is a sleep standing in for synchronisation: a test waits on something observable, a
  readiness line, a file's existence, a channel, inside a bounded wait, so a hang fails the
  test instead of stalling CI. The exceptions are the documented declined fixes
  ([coding agents as contributors](./agents.md), [known upstream bugs](./upstream.md)).

## Where the tests run

- **`just test` is the project's aggregate**, named in the root `.justfile` with the lanes
  it runs, and what CI runs on every leg ([the shared recipes](./recipes.md)).
- **Fuzzing is CI's own job**, on one leg, with the corpus cached between runs, feeding the
  gate like a failing test ([github](./github.md#fuzz)); it is not in the aggregate.
- **Benchmarks and profiles are `perf`**: reports that judge nothing, run by hand, never in
  CI.
- **The race detector runs on the host's C toolchain** by a named exception: a diagnostic is
  never shipped, so a compiler difference there changes what a test finds, never what a
  consumer runs.
- **The mutation score has no lane yet.** It is run by hand with a single-point mutant
  harness, before and after, and its numbers go in the pull request.

## Reporting

A test pass is reported as the list above: each item with its evidence (the file, the lane,
the score, the command that ran) or skipped with its reason. On a pull request that is the
🧪 section ([the shape of a pull request](./agents.md#the-shape-of-a-pull-request)).
