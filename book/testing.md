# Testing

What a test is for, what it may reach, and when a suite is replaced, in every language.
The Go form of each rule, with the lanes that enforce it, is [testing-go](./testing-go.md).

- **A test is black-box, and never bought with indirection.** It reaches what a consumer
  reaches, never internals or private state, and production code grows no interface,
  function field or other indirection whose only purpose is to let a test substitute a
  fake: that is bad design, not testability. An interface is defined by a real client,
  never by a test. When the path with the fix is not reachable from outside, the question
  is how the test rig is modelled, never whether to open the code up.
- **A property no test can observe gets no test.** A power loss, a filesystem failure,
  the kernel refusing a call: a unit test cannot see them, and a fake that asserts a mock
  was called proves only that the code calls itself. Correctness there is by construction
  and by reading; a real fault-injection rig is a separate, larger question.
- **A package's tests are one contract, stated once and checked within a bound.** The
  contract is written at the top of the test package from the sources that define it (the
  specification, the man page, the platform's own documentation), each rule cited; a
  bounded walk holds the code to every edge the contract names; a fuzz target runs past
  the bound, seeded from it; a hand-written test remains only for a case no walk reaches,
  and says which. Writing the contract down is where the bugs are found.
- **A drop-in for a platform or standard-library piece is checked beside the original**,
  call for call, on the same state: what the caller saw and what was left behind must
  match, on every platform the drop-in claims.
- **A suite is replaced only under a mutation score.** Single-point mutants of the package
  (a flipped comparison, a dropped negation, an off-by-one, a deleted statement, an error
  made nil), each run against both suites, and the old suite goes when the new one catches
  every mutant the old one did. The score is the evidence; the line count is not.
- **A test that only the platform can fail is not a test.** One that checks what the
  standard library or the runtime guarantees cannot fail short of a change to them, reads
  as coverage of the thing beside it, covers none of it, and rots. A test earns its place
  by a change to the package that would fail it.
- **A resource test asserts zero growth**, over enough rounds that one byte a round would
  show; a page of slack hid 49 bytes per instance for weeks
  ([forks](./forks.md), the pitfalls table).
- **A reference implementation is a differential oracle**, where upstream ships one: the
  fork's output over a corpus against the reference's, on every leg that can run the
  reference, and by hand before every tag where CI cannot
  ([forks](./forks.md), reviewing upstream before a release).
- **A flake is fixed when it is noticed.** A test that fails and then passes on a rerun is
  a bug that came with its reproducer; the rerun found it and did not fix it. The exceptions
  are the documented declined fixes ([coding agents as contributors](./agents.md),
  [known upstream bugs](./upstream.md)).
- **A generated artifact is tested by regenerating it**: built twice and compared byte for
  byte, in CI, on two platforms where the build has any ([forks](./forks.md)).

The race detector and other diagnostic builds run on the host's C toolchain by a named
exception ([the shared recipes](./recipes.md), the hermetic environment): a diagnostic is
never shipped, so a compiler difference there changes what a test finds, not what a consumer
runs.
