# Testing — Go

The Go form of [testing](./testing.md): the rules as the lint baseline and the test lanes
enforce them, and the cases that set them.

**The boundary is the external test package.** A test lives in `package foo_test` (the
pinned lint rejects an internal test package), so it reaches only what a consumer reaches;
a fake behind an interface or a function field that exists for a test is the smell the
[generic principle](./principles.md) names.

**The lanes.** `just do test go` runs the unit tests; `race` runs them under the race
detector (cgo forced, external linkmode, the host's C compiler by the named exception);
`fuzz` runs every `Fuzz*` target for a bounded time and passes where there is none; `cover`
reports coverage against the module's gate. A project's `test` aggregate names the lanes it
runs ([the shared recipes](./recipes.md)).

## The tests are one contract, checked within a bound

A package's tests state its contract once, as a comment at the top of
`contract_test.go`, from the sources that define it — the specification, the man page,
the platform's own documentation, the package's own docs — each rule cited or quoted.
The files beside it hold the code to that comment and to nothing else:

- `bounded_test.go` walks every input in a bound drawn from the contract's own edges:
  for each class the contract names, its boundary, one step past it, and a middle; for
  a stateful package, every sequence of calls up to a small length, held to a model after
  each. The bound is what makes the check exhaustive; the contract is what keeps it small.
- `fuzz_test.go` runs the same checks past the bound, seeded from it.
- a hand-written test remains only for a case no bounded walk reaches — a device path, a
  path past `MAX_PATH` — and its comment says which.
- a drop-in for a standard-library package (`xos` for `os`) is checked beside it, call
  for call, on the same filesystem state: what the caller saw and what was left on disk
  must match, on every platform.

An older suite goes only under a mutation score: single-point mutants of the package (a
flipped comparison, a dropped `!`, an off-by-one, a deleted statement, an `error` made
`nil`), each run against both suites, and the old suite is removed when the new one
catches every mutant the old one did. The score is the evidence; the line count is not.

Writing the contract down is where the bugs are found: stating a rule from its source
and holding the code to it is what turned up seven in primordium in a week, six in its
filesystem packages and one in a store, each a rule the code did not meet once written
(https://github.com/mycophonic/primordium/pull/138,
https://github.com/mycophonic/primordium/pull/139,
https://github.com/mycophonic/primordium/pull/140,
https://github.com/mycophonic/primordium/pull/143,
https://github.com/mycophonic/primordium/pull/146,
https://github.com/mycophonic/primordium/pull/147,
https://github.com/mycophonic/primordium/pull/153). The shape, package by package:
https://github.com/mycophonic/primordium/pull/141 (a path validator),
https://github.com/mycophonic/primordium/pull/144 (an `io.ReadSeeker` wrapper),
https://github.com/mycophonic/primordium/pull/145 (advisory locks, with blocking),
https://github.com/mycophonic/primordium/pull/151 (the `os` drop-in, differential),
https://github.com/mycophonic/primordium/pull/157 (XDG and platform directories), and
https://github.com/mycophonic/primordium/pull/165, where a 1,789-line port of Go's own
`os` tests went under the score.

## A test that only the standard library can fail is not a test

A test earns its place by a change to the package that would fail it. One that checks
what the standard library guarantees — that `errors.New` returns an error carrying its
message, that `errors.Is` finds a sentinel through `fmt.Errorf`'s `%w` once and twice,
that two sentinels are distinct — cannot fail short of a change to Go, and goes. A
package that is a set of sentinel errors and no logic needs no test file at all; the
coverage gate is on the module, not the package. The trap: such a file reads as coverage
of error handling and covers none of it, and it rots unnoticed — primordium's `fault`
carried 215 lines of it, with a sentinel missing from two of its own lists
(https://github.com/mycophonic/primordium/pull/160).
